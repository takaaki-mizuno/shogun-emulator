package emu

import (
	"errors"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// run はエミュレーションゴルーチンの本体。
//
// コマンドの処理を命令境界に限る。命令の途中では CPU の状態がホストの
// コールスタックにあり、状態の保存も差し替えもできない。
func (e *Emulator) run() {
	defer close(e.done)
	// 終了時に書き出していないセーブデータを残さない。
	defer e.closeBattery()
	defer e.saveSymbols()
	defer e.closeTraceFile()
	defer e.storeOverlay()

	for {
		if !e.drainCommands() {
			return
		}
		if e.rewinding && e.machine != nil {
			e.stepRewind()
			continue
		}
		if !e.canAdvance() {
			// 進めないときはコマンドが来るまで待つ。空回りで CPU を
			// 使い切らないためである。
			if !e.waitCommand() {
				return
			}
			continue
		}
		e.advance()
	}
}

// drainCommands は溜まっているコマンドをすべて処理する。
// 停止を指示されたとき false を返す。
func (e *Emulator) drainCommands() bool {
	for {
		select {
		case <-e.stop:
			return false
		case c := <-e.cmds:
			e.handle(c)
		default:
			return true
		}
	}
}

// waitCommand はコマンドが 1 つ来るまで待って処理する。
func (e *Emulator) waitCommand() bool {
	select {
	case <-e.stop:
		return false
	case c := <-e.cmds:
		e.handle(c)
		return true
	}
}

// canAdvance は今エミュレーションを進めてよいかを返す。
func (e *Emulator) canAdvance() bool {
	return e.machine != nil && !e.paused
}

// advance は 1 命令進め、フレームが完成したところで待つ。
//
// フレーム単位でまとめて進めないのは、コマンドを受け付ける間隔を
// 短く保つためである。1 フレームは約 3 万サイクルあり、まとめて進めると
// 一時停止の反映が 1 フレーム遅れる。
func (e *Emulator) advance() {
	done := e.stepOnce()
	if e.dbg.HasHit() {
		e.takeBreak()
	}
	e.checkUntil()
	if done {
		e.beginFrame()
		e.pollBattery()
		e.pacer.WaitFrame(e.speed)
		e.updateStatus()
	}
}

// stepOnce は 1 命令進め、フレームが完成したかを返す。
func (e *Emulator) stepOnce() bool {
	before := e.machine.Frames()
	e.machine.StepInstruction()
	if e.machine.Frames() == before {
		return false
	}
	e.publishFrame()
	return true
}

// runSteps は一時停止したまま指定の単位だけ進める。
//
// コマンドの処理の中で進める。コマンドを溜めて後で進める形にすると、
// 「1 命令進めてから状態を読む」という並びで、進める前の状態が読まれる。
func (e *Emulator) runSteps(kind StepKind, count int) {
	if e.machine == nil {
		return
	}
	for range count {
		if kind == StepInstruction {
			if e.stepOnce() {
				e.beginFrame()
			}
			if e.takeBreak() {
				break
			}
			continue
		}
		// フレームが完成するまで命令を進める。STP を実行した後も
		// バスは進むため、この繰り返しは必ず終わる。ブレークポイントに
		// 当たったときはそこで止める。
		for !e.stepOnce() && !e.dbg.HasHit() {
		}
		if e.takeBreak() {
			break
		}
		e.beginFrame()
	}
	e.updateStatus()
}

// apuOutput は APU の出力先を返す。音声が無効のとき nil を返す。
//
// nil を返すために型を明示する。*audioPipeline の nil をそのまま
// interface に入れると、nil でない interface になってしまう。
func (e *Emulator) apuOutput() apu.Output {
	if e.audio == nil {
		return nil
	}
	return e.audio
}

// notifyError は続行できる不具合を知らせる。
func (e *Emulator) notifyError(err error) {
	if err == nil {
		return
	}
	e.warn("%v", err)
}

// warn は続行できる不具合を知らせる。
func (e *Emulator) warn(format string, args ...any) {
	if e.cfg.Warn == nil {
		return
	}
	e.cfg.Warn(format, args...)
}

// closeBattery は残っているセーブデータを書き出して保存先を閉じる。
//
// 書き出しに失敗しても続ける。ここで止めると、ROM を取り外せなくなる。
func (e *Emulator) closeBattery() {
	if e.battery == nil {
		return
	}
	if err := e.battery.close(); err != nil {
		e.notifyError(err)
	}
	e.battery = nil
}

// pollBattery は不揮発メモリの変化を調べ、落ち着いたところで書き出す。
func (e *Emulator) pollBattery() {
	if e.battery == nil {
		return
	}
	if err := e.battery.poll(); err != nil {
		e.notifyError(err)
	}
}

// publishFrame は完成したフレームを表示側へ渡す。
//
// 巻き戻しの前方再生中は渡さない。遡る途中の画面が出ると、早送りの
// ように見えてしまう（設計書 08 編 §8.6）。
func (e *Emulator) publishFrame() {
	f := e.machine.TakeFrame()
	if f == nil || e.silent {
		return
	}
	e.lastFrame.CopyFrom(f)
	e.hasFrame = true
	e.Frames.Put(f)
}

// handle はコマンド 1 つを処理する。
func (e *Emulator) handle(c command) {
	switch v := c.(type) {
	case cmdLoadMachine:
		e.saveSymbols()
		e.storeOverlay()
		e.closeBattery()
		e.machine = v.machine
		e.machine.APU.SetMute(e.apuMute)
		e.battery = v.battery
		e.pacer = e.cfg.NewPacer(v.machine.Region)
		e.pacer.Reset()
		e.paused = e.cfg.StartPaused
		if e.audio != nil {
			e.audio.setRegion(v.machine.Region)
			e.audio.setSpeed(e.speed)
			e.audio.setPaused(false)
			v.machine.APU.SetOutput(e.apuOutput())
			e.audio.reset()
			e.audio.start()
		}
		e.setLoaded(v.name)
		e.attachDebugger(v.machine)
		e.powerOnCycles = v.machine.Cycles()
		e.hasFrame = false
		e.resetRewind()
		e.stopPlayback("")
		e.recorder = nil
		e.setMovieStatus()
		// 最初のフレームの入力をラッチする。
		e.beginFrame()
		v.done <- nil

	case cmdUnload:
		e.saveSymbols()
		e.storeOverlay()
		e.dbg.Attach(nil, nil)
		e.closeBattery()
		if err := e.stopRecording(); err != nil {
			e.notifyError(err)
		}
		e.stopPlayback("")
		e.resetRewind()
		e.machine = nil
		e.pacer = NewNoPacer()
		if e.audio != nil {
			// 取り外している間は待たない。次の ROM を待つあいだ
			// エミュレーションゴルーチンが止まらないようにする。
			e.audio.setPaused(true)
		}
		e.setUnloaded()
		v.done <- nil

	case cmdReset:
		if e.machine == nil {
			v.done <- errNoROM
			break
		}
		if v.hard {
			init, err := e.initState()
			if err != nil {
				v.done <- err
				break
			}
			e.machine.PowerOn(init)
			e.machine.APU.SetOutput(e.apuOutput())
			e.powerOnCycles = e.machine.Cycles()
		} else {
			e.machine.Reset()
		}
		e.pacer.Reset()
		if e.audio != nil {
			e.audio.reset()
		}
		e.updateStatus()
		v.done <- nil

	case cmdSetInput:
		e.Input.Set(v.port, v.buttons)

	case cmdSetSpeed:
		if v.factor > 0 {
			e.speed = v.factor
			e.pacer.Reset()
			if e.audio != nil {
				e.audio.setSpeed(v.factor)
			}
			e.setSpeed(v.factor)
		}

	case cmdSetMuted:
		if e.audio != nil {
			e.audio.setMuted(v.muted)
		}
		e.setMuted(v.muted)

	case cmdPause:
		e.paused = v.paused
		if !v.paused {
			e.pacer.Reset()
		}
		if e.audio != nil {
			e.audio.setPaused(v.paused)
		}
		e.setPaused(v.paused)

	case cmdStep:
		e.startStep(v)

	case cmdSaveState:
		if e.machine == nil {
			v.out <- saveResult{err: errNoROM}
			break
		}
		v.out <- saveResult{
			data: e.machine.SaveStateWithScreenshot(e.screenshotPNG(v.screenshot)),
			pc:   e.machine.CPU.PC,
		}

	case cmdLoadState:
		if e.machine == nil {
			v.done <- errNoROM
			break
		}
		err := e.machine.LoadState(v.data)
		e.afterLoadState()
		if err == nil {
			// ステートのロードで記録を止める。ロードの前後で
			// フレームの並びが続かないためである（設計書 08 編 §8.7.5）。
			if rerr := e.stopRecording(); rerr != nil {
				e.notifyError(rerr)
			}
			e.stopPlayback("")
			e.resetRewind()
			e.beginFrame()
		}
		e.updateStatus()
		v.done <- err

	case cmdRewind:
		v.done <- e.rewindFrames(v.frames)

	case cmdStartRecording:
		v.done <- e.startRecording(v.path)

	case cmdStopRecording:
		v.done <- e.stopRecording()

	case cmdPlayMovie:
		v.done <- e.startPlayback(v.m)

	case cmdSetRewinding:
		e.rewinding = v.on && e.rewind != nil
		e.setRewinding(e.rewinding)

	case cmdStopMovie:
		err := e.stopRecording()
		e.stopPlayback("")
		v.done <- err

	case cmdFunc:
		if v.fn != nil {
			v.fn(e.machine)
		}
		close(v.done)
	}
}

// setLoaded は ROM を読み込んだ状態を反映する。
func (e *Emulator) setLoaded(name string) {
	info := e.machine.Cart.Info()
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Loaded = true
	e.status.Paused = false
	e.status.ROMName = name
	e.status.ROMKey = cart.ROMKeyString(e.machine.ROM.Hash[:])
	e.status.MapperName = info.MapperName
	e.status.MapperNumber = info.MapperNumber
	e.status.RegionName = e.machine.Region.Name
	e.status.PictureHeight = e.machine.Region.PictureHeight
	e.status.Frames = e.machine.Frames()
	e.status.Cycles = e.machine.Cycles()
}

// setUnloaded は ROM を取り外した状態を反映する。
func (e *Emulator) setUnloaded() {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	speed := e.status.Speed
	e.status = Status{Speed: speed}
}

// setPaused は一時停止の状態を反映する。
func (e *Emulator) setPaused(p bool) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Paused = p
}

// setMuted は消音の状態を反映する。
func (e *Emulator) setMuted(v bool) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Muted = v
}

// setSpeed は速度倍率を反映する。
func (e *Emulator) setSpeed(f float64) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Speed = f
}

// updateStatus は累積の値を反映する。
func (e *Emulator) updateStatus() {
	var frames, cycles uint64
	if e.machine != nil {
		frames, cycles = e.machine.Frames(), e.machine.Cycles()
	}
	var fill, high int
	var underruns uint64
	if e.audio != nil {
		fill, high = e.audio.ring.Fill()
		_, underruns, _ = e.audio.ring.Stats()
	}

	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Frames = frames
	e.status.Cycles = cycles
	e.status.AudioFill = fill
	e.status.AudioHigh = high
	e.status.AudioUnderruns = underruns
}

// stepRewind は押している間の巻き戻しを 1 フレーム分進める。
//
// 遡る速さを通常の再生と同じにする。待たずに遡ると、キーを押した瞬間に
// 記録の先頭まで戻ってしまう。
func (e *Emulator) stepRewind() {
	if err := e.rewindFrames(1); err != nil {
		// 限界に達した。これ以上遡らない。
		e.rewinding = false
		e.setRewinding(false)
		return
	}
	e.pacer.WaitFrame(e.speed)
	e.updateStatus()
}

// setRewinding は巻き戻し中であることを Status へ反映する。
func (e *Emulator) setRewinding(v bool) {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Rewinding = v
}

// afterLoadState はステートを復元した後の後始末を行う。
//
// 音声のフィルタとリングを作り直す。復元前の波形が残っていると、
// つながらない音が出る。フレームバッファは次のフレームが完成するまで
// 前の内容のままにする（設計書 08 編 §8.4）。
func (e *Emulator) afterLoadState() {
	e.pacer.Reset()
	if e.audio != nil {
		e.audio.reset()
	}
}

// screenshotPNG は直前に完成したフレームを PNG にする。
//
// 画面をキャプチャせずフレームバッファから作る。表示の更新は非同期で
// あり、画面に出ている内容とフレームの内容が一致するとは限らない。
func (e *Emulator) screenshotPNG(want bool) []uint8 {
	if !want || !e.hasFrame || e.machine == nil {
		return nil
	}
	png, err := video.EncodePNG(e.lastFrame, video.DefaultPalette(), video.Overscan{},
		e.machine.Region.PictureHeight)
	if err != nil {
		e.notifyError(err)
		return nil
	}
	return png
}

// SaveScreenshot は直前に完成したフレームを PNG で path へ保存する
// （引数 --screenshot）。オーバースキャンで隠さず、全体を保存する。
func (e *Emulator) SaveScreenshot(path string) error {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil || !e.hasFrame {
			err = errors.New("emu: 保存するフレームが無い")
			return
		}
		err = video.SavePNG(e.lastFrame, video.DefaultPalette(), video.Overscan{}, m.Region.PictureHeight, path)
	})
	if !ok {
		return errors.New("emu: エミュレーションが停止している")
	}
	return err
}
