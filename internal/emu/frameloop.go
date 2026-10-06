package emu

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// beginFrame はフレームの開始時の処理を行う。
//
// 入力のラッチ・ムービーの再生と記録・巻き戻しの記録を、この 1 か所で
// 同じ順序で行う。記録した入力と再生に使う入力が同じ値になるように
// するためである。
func (e *Emulator) beginFrame() {
	if e.machine == nil {
		return
	}
	frame := e.machine.Frames()
	e.journal.frameStart = e.machine.Cycles()
	// 常時記録は、再生が終わったフレームも含めてすべてのフレームで行う。
	// 再生した介入は、このフレームの記録の後に受け取って適用する。
	defer func() {
		e.journalFrame()
		in := e.incoming
		e.incoming = nil
		if e.player != nil || len(in) > 0 {
			e.takeInterventions(in)
		}
	}()

	// 再生中はムービーの入力を使う。再生していないときは UI から来た
	// 押下状態を取り込む。
	if e.player != nil {
		if !e.playMovieFrame(frame) {
			return
		}
	} else if e.agentInput != nil {
		// エージェントの入力をそのまま使う。キーボードと連射は使わない
		// （設計書 14 編 §14.4.2）。
		e.latch.set(*e.agentInput)
	} else {
		e.latch.set(e.turbo.apply(frame, e.currentButtons()))
	}

	if e.recorder != nil {
		e.recorder.BeginFrame(e.latch.get(), e.machine.StateHash())
		e.setMovieStatus()
	}
	if e.rewind != nil && !e.silent {
		e.rewind.record(frame, e.latch.get(), e.machine.SaveState)
	}
}

// currentButtons は UI から来た押下状態を取り込む。
func (e *Emulator) currentButtons() [input.PortCount]uint8 {
	var out [input.PortCount]uint8
	for i := range out {
		out[i] = e.Input.Buttons(i)
	}
	return out
}

// playMovieFrame はムービーの 1 フレーム分を適用する。
//
// 再生を続けるとき true を返す。ムービーが終わったときと desync で
// 停止したときは false を返す。
func (e *Emulator) playMovieFrame(frame uint64) bool {
	f, ok := e.player.BeginFrame()
	if !ok {
		// 記録の終わり。残った介入（フレームの終わりのものを含む）を適用する。
		e.flushInterventions()
		if e.journal.pauseAtEnd {
			// Repro は最後のフレームで止める（設計書 14 編 §14.16.3）。
			e.paused = true
			e.setPaused(true)
		}
		e.stopPlayback("ムービーの再生が終わった")
		return false
	}
	switch {
	case f.HardReset:
		init, err := e.initState()
		if err == nil {
			e.machine.PowerOn(init)
			e.machine.APU.SetOutput(e.apuOutput())
		}
	case f.Reset:
		e.machine.Reset()
	}
	e.latch.set(f.Buttons)
	e.incoming = f.Interventions

	// Re-Reach の再生は別の ROM で行うため、チェックサムを比べない。
	if f.Checksum != nil && e.cfg.Movie.VerifyChecksums && !e.journal.replaying {
		got := e.machine.StateHash()
		if got != *f.Checksum {
			e.reportDesync(&movie.DesyncError{Frame: frame, Want: *f.Checksum, Got: got})
			if e.cfg.Movie.StopOnDesync {
				return false
			}
		}
	}
	e.setMovieStatus()
	return true
}

// reportDesync は状態の食い違いを知らせる。
func (e *Emulator) reportDesync(err *movie.DesyncError) {
	e.desyncMu.Lock()
	e.desyncErr = err
	e.desyncMu.Unlock()
	e.notifyError(err)
	if o := e.observer.Load(); o != nil && o.OnDesync != nil {
		o.OnDesync(err, e.machine.Frames())
	}
}

// DesyncError は再生中に検出した状態の食い違いを返す。無いとき nil。
func (e *Emulator) DesyncError() error {
	e.desyncMu.Lock()
	defer e.desyncMu.Unlock()
	if e.desyncErr == nil {
		return nil
	}
	return e.desyncErr
}

// stopPlayback は再生を止め、通常の入力へ戻す。
func (e *Emulator) stopPlayback(reason string) {
	if e.player == nil {
		return
	}
	e.player = nil
	e.journal.replaying = false
	e.journal.pauseAtEnd = false
	e.journal.pending = nil
	e.latch.set([input.PortCount]uint8{})
	e.setMovieStatus()
	if reason != "" {
		e.notifyMessage(reason)
	}
}

// stopRecording は記録を止め、ファイルへ書き出す。
func (e *Emulator) stopRecording() error {
	if e.recorder == nil {
		return nil
	}
	data := e.recorder.Encode()
	path := e.recordPath
	e.recorder = nil
	e.recordPath = ""
	e.setMovieStatus()
	if path == "" {
		return nil
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("emu: ムービーを書き出せない: %w", err)
	}
	e.notifyMessage(fmt.Sprintf("%s へムービーを書き出しました", path))
	return nil
}

// notifyMessage は利用者へ知らせる。
func (e *Emulator) notifyMessage(msg string) {
	if e.cfg.Notify == nil {
		return
	}
	e.cfg.Notify(msg)
}

// setMovieStatus はムービーの状態を Status へ反映する。
func (e *Emulator) setMovieStatus() {
	var st MovieStatus
	switch {
	case e.recorder != nil:
		st = MovieStatus{
			Recording: true,
			Frame:     e.recorder.Frames(),
			Total:     e.recorder.Frames(),
			Rerecords: e.recorder.Rerecords(),
		}
	case e.player != nil:
		st = MovieStatus{
			Playing: true,
			Frame:   e.player.Frame(),
			Total:   e.player.TotalFrames(),
		}
	}
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Movie = st
}
