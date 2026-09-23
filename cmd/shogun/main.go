// Command shogun は NES エミュレータ「将軍エミュレータ」の実行ファイル。
//
// GUI アプリケーションとして起動し、コマンドラインから設定を上書きできる。
// 引数に ROM のファイルを渡すと、起動と同時に読み込む。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
	"github.com/takaakimizuno/shogun-emulator/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// options はコマンドラインで指定できる値。
type options struct {
	showVersion bool
	region      string
	scale       int
	fullscreen  bool
	noAudio     bool
	romPath     string

	// 保存先
	saveDir  string
	stateDir string

	// ステートとムービー
	loadState       string
	saveStateOnExit string
	moviePath       string
	recordMovie     string
	movieVerify     bool
	noMovieVerify   bool

	// 決定論
	deterministic bool
	ramInit       string
	ramSeed       uint64

	// headless 実行
	headless bool
	frames   int
}

// run は引数を解釈して終了コードを返す。
//
// main から分けてあるのは、終了コードと出力をテストから確かめられるように
// するためである。
func run(args []string, stdout, stderr io.Writer) int {
	opts, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}
	if opts.showVersion {
		printVersion(stdout)
		return 0
	}

	cfg := config.Default()
	if err := applyOptions(cfg, opts); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return 2
	}

	if opts.headless {
		return runHeadless(cfg, opts, stdout, stderr)
	}

	startGUI(cfg, opts, stderr)
	return 0
}

// parseArgs はコマンドライン引数を解釈する。
func parseArgs(args []string, stderr io.Writer) (options, int, bool) {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "使い方: %s [オプション] [ROM ファイル]\n\nオプション:\n", appName)
		fs.PrintDefaults()
	}

	var opts options
	fs.BoolVar(&opts.showVersion, "version", false, "バージョン情報を表示して終了する")
	fs.StringVar(&opts.region, "region", "", "リージョン（auto, ntsc, pal, dendy）")
	fs.IntVar(&opts.scale, "scale", 0, "拡大率（1 から 8）")
	fs.BoolVar(&opts.fullscreen, "fullscreen", false, "フルスクリーンで起動する")
	fs.BoolVar(&opts.noAudio, "no-audio", false, "音声を出さない")
	fs.StringVar(&opts.saveDir, "save-dir", "", "バッテリーバックアップの保存先")
	fs.StringVar(&opts.stateDir, "state-dir", "", "セーブステートの保存先")
	fs.StringVar(&opts.loadState, "load-state", "", "起動時にセーブステートを読み込む")
	fs.StringVar(&opts.saveStateOnExit, "save-state-on-exit", "", "終了時にセーブステートを保存する")
	fs.StringVar(&opts.moviePath, "movie", "", "入力ムービーを再生する")
	fs.StringVar(&opts.recordMovie, "record-movie", "", "入力ムービーを記録する")
	fs.BoolVar(&opts.movieVerify, "movie-verify", false, "ムービー再生時にチェックサムを検証する")
	fs.BoolVar(&opts.noMovieVerify, "no-movie-verify", false, "ムービー再生時にチェックサムを検証しない")
	fs.BoolVar(&opts.deterministic, "deterministic", false, "値が定まらない状態をすべて固定値にする")
	fs.StringVar(&opts.ramInit, "ram-init", "", "RAM の初期化パターン（zero, ff, pattern, random）")
	fs.Uint64Var(&opts.ramSeed, "ram-seed", 0, "RAM 初期化の乱数シード")
	fs.BoolVar(&opts.headless, "headless", false, "GUI を起動せずに実行する")
	fs.IntVar(&opts.frames, "frames", 0, "指定したフレーム数だけ実行して終了する")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, 0, false
		}
		return opts, 2, false
	}
	if fs.NArg() > 0 {
		opts.romPath = fs.Arg(0)
	}
	return opts, 0, true
}

// applyOptions はコマンドラインの指定を設定へ反映する。
func applyOptions(cfg *config.Config, opts options) error {
	if opts.region != "" {
		switch opts.region {
		case config.RegionAuto, config.RegionNTSC, config.RegionPAL, config.RegionDendy:
			cfg.Emulation.Region = opts.region
		default:
			return fmt.Errorf("知らないリージョン %q", opts.region)
		}
	}
	if opts.scale != 0 {
		if opts.scale < config.MinScale || opts.scale > config.MaxScale {
			return fmt.Errorf("拡大率は %d から %d の範囲で指定する", config.MinScale, config.MaxScale)
		}
		cfg.Video.Scale = opts.scale
	}
	if opts.fullscreen {
		cfg.Video.Fullscreen = true
	}
	if opts.noAudio {
		cfg.Audio.Enabled = false
	}
	if opts.saveDir != "" {
		cfg.Paths.SaveDir = opts.saveDir
	}
	if opts.stateDir != "" {
		cfg.Paths.StateDir = opts.stateDir
	}
	if opts.ramInit != "" {
		if _, ok := state.ParsePattern(opts.ramInit); !ok {
			return fmt.Errorf("知らない RAM 初期化パターン %q", opts.ramInit)
		}
		cfg.Emulation.RAMInitPattern = opts.ramInit
	}
	if opts.ramSeed != 0 {
		cfg.Emulation.RAMSeed = opts.ramSeed
	}
	if opts.deterministic {
		// 値が定まらない状態をすべて固定値にする（設計書 08 編 §8.5.2）。
		cfg.Emulation.RAMInitPattern = state.PatternZero.String()
		cfg.Emulation.RAMSeed = 0
		cfg.Emulation.CPUPPUAlignment = 0
		cfg.Emulation.DMAGetPutPhase = 0
		cfg.Emulation.PPUVBlankFlag = false
	}
	if opts.movieVerify && opts.noMovieVerify {
		return fmt.Errorf("--movie-verify と --no-movie-verify は同時に指定できない")
	}
	if opts.movieVerify {
		cfg.Movie.VerifyChecksums = true
	}
	if opts.noMovieVerify {
		cfg.Movie.VerifyChecksums = false
	}
	if opts.headless && opts.romPath == "" {
		return fmt.Errorf("--headless には ROM のファイルを指定する")
	}
	return nil
}

// startGUI は画面を開き、閉じられるまで戻らない。
func startGUI(cfg *config.Config, opts options, stderr io.Writer) {
	e := emu.New(emu.Config{
		Emulation: cfg.Emulation,
		Input:     cfg.Input,
		Audio:     cfg.Audio,
		Paths:     cfg.Paths,
		State:     cfg.State,
		Movie:     cfg.Movie,
		Debug:     cfg.Debug,
		AppName:   appTitle,
		Version:   version,
		Commit:    commit,
		Warn: func(format string, args ...any) {
			fmt.Fprintf(stderr, "%s: "+format+"\n", append([]any{appName}, args...)...)
		},
	})
	if err := e.AudioError(); err != nil {
		// 音が出ないことで起動しない状態を作らない。
		fmt.Fprintf(stderr, "%s: 音声を初期化できないため無効にする: %v\n", appName, err)
	}
	u := ui.New(e, cfg, config.DefaultKeybindings(), version)
	if opts.romPath != "" {
		// 画面を開く前に読み込む。読み込めないときは知らせて続ける。
		// ROM を開けないことでアプリケーションが起動しない状態を作らない。
		e.Start()
		if err := e.LoadROM(opts.romPath); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		} else {
			u.NotifyROMLoaded(opts.romPath)
			applyStartupOptions(e, opts, stderr)
		}
	}
	u.Run()

	if opts.saveStateOnExit != "" {
		if err := e.SaveToFile(opts.saveStateOnExit); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
}

// applyStartupOptions は起動時に指定されたステートとムービーの操作を行う。
func applyStartupOptions(e *emu.Emulator, opts options, stderr io.Writer) {
	if opts.loadState != "" {
		if err := e.LoadFromFile(opts.loadState); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.moviePath != "" {
		if err := e.PlayMovieFile(opts.moviePath); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.recordMovie != "" {
		if err := e.StartRecordingMovie(opts.recordMovie); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
}
