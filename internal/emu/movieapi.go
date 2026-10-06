package emu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// StartRecordingMovie はムービーの記録を始める。
//
// すでに進んでいる状態から始めるときは、その時点のステートをムービーへ
// 埋め込む。再生側が同じ状態から始められるようにするためである。
func (e *Emulator) StartRecordingMovie(path string) error {
	if path == "" {
		return errors.New("emu: ムービーの書き出し先が空である")
	}
	return e.apply(cmdStartRecording{path: path, done: make(chan error, 1)})
}

// StopRecordingMovie は記録を止めてファイルへ書き出す。
func (e *Emulator) StopRecordingMovie() error {
	return e.apply(cmdStopRecording{done: make(chan error, 1)})
}

// PlayMovieFile はファイルからムービーを読み込んで再生する。
//
// ファイルの読み込みと解析を呼び出し側のゴルーチンで行う。実行中の
// エミュレーションを止めないためである。
func (e *Emulator) PlayMovieFile(path string) error {
	// Repro のディレクトリ（<名前>.repro）を渡されたら中の repro.shgm を使う。
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		p := filepath.Join(path, "repro.shgm")
		if _, err := os.Stat(p); err != nil {
			return errReproDir
		}
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := movie.Decode(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return e.PlayMovie(m)
}

// PlayMovie は読み込み済みのムービーを再生する。
func (e *Emulator) PlayMovie(m *movie.Movie) error {
	return e.apply(cmdPlayMovie{m: m, done: make(chan error, 1)})
}

// StopMovie は記録と再生を止める。
func (e *Emulator) StopMovie() error {
	return e.apply(cmdStopMovie{done: make(chan error, 1)})
}

// Rewind は frames フレーム分だけ遡る。
func (e *Emulator) Rewind(frames int) error {
	return e.apply(cmdRewind{frames: frames, done: make(chan error, 1)})
}

// SetRewinding は押している間の巻き戻しを切り替える。
//
// 有効な間、エミュレーションは進まずに 1 フレームずつ遡る。遡れる
// 限界に達したところで止まる。
func (e *Emulator) SetRewinding(on bool) { e.send(cmdSetRewinding{on: on}) }

// startRecording はエミュレーションゴルーチンで記録を始める。
func (e *Emulator) startRecording(path string) error {
	if e.machine == nil {
		return errNoROM
	}
	if err := e.stopRecording(); err != nil {
		return err
	}
	e.stopPlayback("")

	h := e.movieHeader()
	if !e.atPowerOn() {
		// 途中から記録する。再生側が同じ状態から始められるように、
		// その時点のステートを埋め込む。電源投入の直後でない限り、
		// 電源を入れ直しただけでは同じ状態にならない。
		h.Start = movie.StartSaveState
		h.StateBlob = e.machine.SaveState()
	}
	e.recorder = movie.NewRecorder(h)
	e.recordPath = path
	// このフレームの入力から記録する。
	e.recorder.BeginFrame(e.latch.get(), e.machine.StateHash())
	e.setMovieStatus()
	return nil
}

// atPowerOn は電源を入れた直後の状態かを返す。
//
// フレーム数だけでは判定できない。フレーム 0 の途中でも命令は進んで
// いるためである。
func (e *Emulator) atPowerOn() bool {
	return e.machine != nil && e.machine.Frames() == 0 && e.machine.Cycles() == e.powerOnCycles
}

// movieHeader は現在の本体からムービーのヘッダを作る。
func (e *Emulator) movieHeader() movie.Header {
	h := e.machine.Header()
	name := e.Status().ROMName
	interval := e.cfg.Movie.ChecksumIntervalFrames
	return movie.Header{
		Version:          h.Version,
		Commit:           h.Commit,
		ROMHash:          h.ROMHash,
		ROMName:          name,
		Region:           h.Region,
		Mapper:           h.Mapper,
		Submapper:        h.Submapper,
		Init:             h.Init,
		Start:            movie.StartPowerOn,
		Ports:            e.portKinds(),
		ChecksumInterval: interval,
	}
}

// portKinds はポートに接続しているデバイスの種別を返す。
func (e *Emulator) portKinds() [2]string {
	var out [2]string
	for i := range out {
		out[i] = config.DeviceNone
		if e.machine != nil && i < len(e.machine.Ports) {
			out[i] = e.machine.Ports[i].Kind()
		}
	}
	return out
}

// startPlayback はエミュレーションゴルーチンで再生を始める。
func (e *Emulator) startPlayback(m *movie.Movie) error {
	if e.machine == nil {
		return errNoROM
	}
	if err := e.stopRecording(); err != nil {
		return err
	}

	p := movie.NewPlayer(m)
	want := e.movieHeader()
	warning, err := p.VerifyHeader(&want)
	if err != nil {
		return err
	}
	if warning != "" {
		e.warn("%s", warning)
	}

	// ムービーの初期状態で電源を入れ直す。記録時と同じ値でないと、
	// 未初期化 RAM に依存するプログラムで結果が変わる。
	e.machine.PowerOn(nes.InitState(m.Header.Init))
	e.machine.APU.SetOutput(e.apuOutput())
	if m.Header.Start == movie.StartSaveState {
		if err := e.machine.LoadState(m.Header.StateBlob); err != nil {
			return fmt.Errorf("emu: ムービーに埋め込まれたステートを復元できない: %w", err)
		}
		e.dbg.StateLoaded()
	}
	e.afterLoadState()
	e.resetRewind()
	e.desyncMu.Lock()
	e.desyncErr = nil
	e.desyncMu.Unlock()

	e.player = p
	// 常時記録を再生するムービーと同じ始まりから記録し直す。
	if m.Header.Start == movie.StartSaveState {
		e.startJournal(movie.StartSaveState)
	} else {
		e.startJournal(movie.StartPowerOn)
	}
	e.journal.pauseAtEnd = m.Header.Author == ReproAuthor
	e.journal.replaying = false
	// Agent-Paced の Instance はエージェントの進行の要求の間だけ進む
	// （設計書 14 編 §14.4.2）。再生を始めても走り出さない。
	e.paused = e.agentInput != nil
	e.setPaused(e.paused)
	e.hasFrame = false
	e.framePending = false
	e.frameBoundary()
	// フレームの開始の処理を遅らせたときも、再生中であることを見せる。
	e.setMovieStatus()
	e.updateStatus()
	return nil
}
