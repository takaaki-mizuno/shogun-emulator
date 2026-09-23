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
// 可能な速度で実行する（設計書 11 編 §11.5.2）。
func runHeadless(cfg *config.Config, opts options, stdout, stderr io.Writer) int {
	cfg.Audio.Enabled = false

	e := emu.New(emu.Config{
		Emulation: cfg.Emulation,
		Input:     cfg.Input,
		Audio:     cfg.Audio,
		Paths:     cfg.Paths,
		State:     cfg.State,
		Movie:     cfg.Movie,
		AppName:   appTitle,
		Version:   version,
		Commit:    commit,
		NewPacer:  func(*region.Region) emu.Pacer { return emu.NewNoPacer() },
		Warn: func(format string, args ...any) {
			fmt.Fprintf(stderr, "%s: "+format+"\n", append([]any{appName}, args...)...)
		},
		Notify: func(msg string) { fmt.Fprintf(stdout, "%s\n", msg) },
	})
	e.Start()
	defer e.Stop()

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
