package main

import (
	"fmt"
	"io"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// 終了コード（設計書 11 編 §11.5.2）。
const (
	exitOK           = 0
	exitROMError     = 1
	exitBadArgs      = 2
	exitMovieDesync  = 3
	exitTestROMError = 4
)

// headlessDefaultFrames は --frames を指定しなかったときに進める上限。
//
// ムービーを再生するときはムービーの長さで終わる。上限はムービーが
// 終わらない場合に備えた保険である。
const headlessDefaultFrames = 60 * 60 * 60

// runHeadless は画面を作らずにエミュレーションだけを実行する。
//
// オーディオデバイスを開かない。進行の駆動をオーディオに依存させず、
// 可能な速度で実行する（設計書 11 編 §11.5.2）。設定ファイルへは書かない。
func runHeadless(store *config.Store, opts options, logOut io.Writer, stdout, stderr io.Writer) int {
	cfg := store.Config().Clone()
	cfg.Audio.Enabled = false

	ec := emuConfig(cfg, store.Paths, logOut, stderr)
	ec.NewPacer = func(*region.Region) emu.Pacer { return emu.NewNoPacer() }
	// 読み込んだ直後から進めない。--frames の数とトレースの先頭を正確にする。
	ec.StartPaused = true
	ec.Notify = func(msg string) { fmt.Fprintf(stdout, "%s\n", msg) }
	e := emu.New(ec)
	e.Start()
	defer e.Stop()

	if opts.breakAt != "" {
		addrs, _ := parseBreakAddrs(opts.breakAt)
		e.SetStartupBreakpoints(addrs)
	}
	if err := e.LoadROM(opts.romPath); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return exitROMError
	}
	if opts.loadState != "" {
		if err := e.LoadFromFile(opts.loadState); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
	}
	if opts.traceLog != "" {
		if err := e.StartTraceLog(opts.traceLog); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitBadArgs
		}
	}

	frames := opts.frames
	if opts.moviePath != "" {
		if err := e.PlayMovieFile(opts.moviePath); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
		if frames == 0 {
			frames = int(e.Status().Movie.Total)
		}
	}
	if opts.recordMovie != "" {
		if err := e.StartRecordingMovie(opts.recordMovie); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
	}
	if frames <= 0 {
		frames = headlessDefaultFrames
	}

	code := runHeadlessFrames(e, opts, frames, stdout)

	if opts.traceLog != "" {
		if err := e.StopTraceFile(); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.recordMovie != "" {
		if err := e.StopRecordingMovie(); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.saveStateOnExit != "" {
		if err := e.SaveToFile(opts.saveStateOnExit); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.screenshot != "" {
		if err := e.SaveScreenshot(opts.screenshot); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
	}
	return code
}

// headlessChunk は 1 回に進めるフレーム数。
//
// まとめて進めるのは、コマンドの往復の費用を抑えるためである。
// 途中で desync を見つけたときは、この単位で気づく。
const headlessChunk = 60

// runHeadlessFrames は指定したフレーム数を進め、終了コードを返す。
func runHeadlessFrames(e *emu.Emulator, opts options, frames int, stdout io.Writer) int {
	e.SetPaused(true)
	for done := 0; done < frames; done += headlessChunk {
		n := min(headlessChunk, frames-done)
		e.StepFrames(n)
		// 進み終わるまで待つ。命令境界で処理されるコマンドを 1 つ送る。
		e.WithMachine(func(*nes.NES) {})

		if err := e.DesyncError(); err != nil {
			fmt.Fprintf(stdout, "%v\n", err)
			return exitMovieDesync
		}
		if reason := e.Status().Break; reason != "" {
			// --break-at のブレークポイントで止まった。理由を出して終える。
			fmt.Fprintf(stdout, "停止: %s\n", reason)
			return exitOK
		}
		if opts.moviePath != "" && !e.Status().Movie.Playing {
			break
		}
	}
	if err := e.DesyncError(); err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
		return exitMovieDesync
	}
	return exitOK
}
