package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// options はコマンドラインで指定できる値（設計書 11 編 §11.5.1）。
type options struct {
	showVersion bool
	configPath  string
	portable    bool
	romPath     string

	// 表示と音声
	region      string
	scale       int
	fullscreen  bool
	noAudio     bool
	audioBuffer int
	sampleRate  int
	speed       float64

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

	// デバッグ
	debug         bool
	logOutput     string
	logDir        string
	logCategories string
	traceLog      string
	breakAt       string

	// headless 実行
	headless   bool
	frames     int
	screenshot string
}

// flagGroups は --help でオプションを並べる分類。
var flagGroups = []struct {
	title string
	names []string
}{
	{"全般", []string{"help", "version", "config", "portable"}},
	{"表示と音声", []string{"region", "scale", "fullscreen", "no-audio", "audio-buffer", "sample-rate", "speed"}},
	{"保存先", []string{"save-dir", "state-dir"}},
	{"ステートとムービー", []string{"load-state", "save-state-on-exit", "movie", "record-movie", "movie-verify", "no-movie-verify"}},
	{"決定論", []string{"ram-init", "ram-seed", "deterministic"}},
	{"デバッグ", []string{"debug", "log", "log-dir", "log-categories", "trace-log", "break-at"}},
	{"headless", []string{"headless", "frames", "screenshot"}},
}

// parseArgs はコマンドライン引数を解釈する。
//
// 戻り値の ok が false のときは code で終了する。--help は 0、不正な引数は 2。
func parseArgs(args []string, stderr io.Writer) (options, int, bool) {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts options
	var help bool
	fs.BoolVar(&help, "help", false, "このヘルプを表示する（-h も同じ）")
	fs.BoolVar(&opts.showVersion, "version", false, "バージョン・コミット・ビルド日時・Go のバージョンを表示する")
	fs.StringVar(&opts.configPath, "config", "", "設定ファイルのパス")
	fs.BoolVar(&opts.portable, "portable", false, "実行ファイルのディレクトリを保存先にする")
	fs.StringVar(&opts.region, "region", "", "リージョン（auto, ntsc, pal, dendy）")
	fs.IntVar(&opts.scale, "scale", 0, "拡大率（1 から 8）")
	fs.BoolVar(&opts.fullscreen, "fullscreen", false, "フルスクリーンで起動する")
	fs.BoolVar(&opts.noAudio, "no-audio", false, "音声を出さない")
	fs.IntVar(&opts.audioBuffer, "audio-buffer", 0, "オーディオバッファの長さ（ミリ秒、5 から 200）")
	fs.IntVar(&opts.sampleRate, "sample-rate", 0, "サンプリングレート（48000 だけを受け付ける）")
	fs.Float64Var(&opts.speed, "speed", 0, "実行速度の倍率（0.25 から 8）")
	fs.StringVar(&opts.saveDir, "save-dir", "", "バッテリーバックアップの保存先")
	fs.StringVar(&opts.stateDir, "state-dir", "", "セーブステートの保存先")
	fs.StringVar(&opts.loadState, "load-state", "", "起動時にセーブステートを読み込む")
	fs.StringVar(&opts.saveStateOnExit, "save-state-on-exit", "", "終了時にセーブステートを保存する")
	fs.StringVar(&opts.moviePath, "movie", "", "入力ムービーを再生する")
	fs.StringVar(&opts.recordMovie, "record-movie", "", "入力ムービーを記録する")
	fs.BoolVar(&opts.movieVerify, "movie-verify", false, "ムービー再生時にチェックサムを検証する")
	fs.BoolVar(&opts.noMovieVerify, "no-movie-verify", false, "ムービー再生時にチェックサムを検証しない")
	fs.StringVar(&opts.ramInit, "ram-init", "", "RAM の初期化パターン（zero, ff, pattern, random）")
	fs.Uint64Var(&opts.ramSeed, "ram-seed", 0, "RAM 初期化の乱数シード")
	fs.BoolVar(&opts.deterministic, "deterministic", false, "値が定まらない状態をすべて固定値にする")
	fs.BoolVar(&opts.debug, "debug", false, "CPU デバッガを開き、一時停止した状態で起動する")
	fs.StringVar(&opts.logOutput, "log", "", "ログの出力先（stdout, stderr, file）")
	fs.StringVar(&opts.logDir, "log-dir", "", "ログファイルの保存先")
	fs.StringVar(&opts.logCategories, "log-categories", "", "ログカテゴリ（カンマ区切り）")
	fs.StringVar(&opts.traceLog, "trace-log", "", "CPU トレースをこのファイルへ出力する")
	fs.StringVar(&opts.breakAt, "break-at", "", "起動時に実行ブレークポイントを置くアドレス（16 進）")
	fs.BoolVar(&opts.headless, "headless", false, "GUI を起動せずに実行する")
	fs.IntVar(&opts.frames, "frames", 0, "指定したフレーム数だけ実行して終了する")
	fs.StringVar(&opts.screenshot, "screenshot", "", "終了時にスクリーンショットを PNG で保存する")
	fs.Usage = func() { printUsage(fs, stderr) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, exitOK, false
		}
		return opts, exitBadArgs, false
	}
	if help {
		printUsage(fs, stderr)
		return opts, exitOK, false
	}
	switch fs.NArg() {
	case 0:
	case 1:
		opts.romPath = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "%s: ROM のファイルは 1 つだけ指定する\n", appName)
		return opts, exitBadArgs, false
	}
	return opts, exitOK, true
}

// printUsage は分類ごとにオプションを並べる。
func printUsage(fs *flag.FlagSet, w io.Writer) {
	fmt.Fprintf(w, "使い方: %s [オプション] [ROM ファイル]\n", appName)
	for _, g := range flagGroups {
		fmt.Fprintf(w, "\n%s:\n", g.title)
		for _, name := range g.names {
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			arg, usage := flag.UnquoteUsage(f)
			head := "  --" + name
			if arg != "" {
				head += " " + arg
			}
			fmt.Fprintf(w, "%-28s %s\n", head, usage)
		}
	}
}

// optionOverrides は引数による設定の上書きを作る。受け付けられない値はエラーにする。
//
// 上書きは設定ファイルへ書き戻さない（設計書 11 編 §11.1）。
func optionOverrides(opts options) ([]config.Override, []string, error) {
	var out []config.Override
	var warnings []string
	add := func(src string, fn func(c *config.Config)) {
		out = append(out, config.Override{Source: src, Apply: fn})
	}

	if opts.region != "" {
		if !slices.Contains([]string{config.RegionAuto, config.RegionNTSC, config.RegionPAL, config.RegionDendy}, opts.region) {
			return nil, nil, fmt.Errorf("知らないリージョン %q", opts.region)
		}
		add("--region", func(c *config.Config) { c.Emulation.Region = opts.region })
	}
	if opts.scale != 0 {
		if opts.scale < config.MinScale || opts.scale > config.MaxScale {
			return nil, nil, fmt.Errorf("拡大率は %d から %d の範囲で指定する", config.MinScale, config.MaxScale)
		}
		add("--scale", func(c *config.Config) { c.Video.Scale = opts.scale })
	}
	if opts.fullscreen {
		add("--fullscreen", func(c *config.Config) { c.Video.Fullscreen = true })
	}
	if opts.noAudio {
		add("--no-audio", func(c *config.Config) { c.Audio.Enabled = false })
	}
	if opts.audioBuffer != 0 {
		if opts.audioBuffer < 5 || opts.audioBuffer > 200 {
			return nil, nil, fmt.Errorf("オーディオバッファは 5 から 200 ミリ秒の範囲で指定する")
		}
		add("--audio-buffer", func(c *config.Config) { c.Audio.BufferMilliseconds = opts.audioBuffer })
	}
	if opts.sampleRate != 0 && opts.sampleRate != config.FixedSampleRate {
		warnings = append(warnings, fmt.Sprintf("サンプリングレートは %d に固定しているため --sample-rate %d を無視する",
			config.FixedSampleRate, opts.sampleRate))
	}
	if opts.speed != 0 && (opts.speed < 0.25 || opts.speed > 8) {
		return nil, nil, fmt.Errorf("実行速度は 0.25 から 8 の範囲で指定する")
	}
	if opts.saveDir != "" {
		add("--save-dir", func(c *config.Config) { c.Paths.SaveDir = opts.saveDir })
	}
	if opts.stateDir != "" {
		add("--state-dir", func(c *config.Config) { c.Paths.StateDir = opts.stateDir })
	}
	if opts.ramInit != "" {
		if _, ok := state.ParsePattern(opts.ramInit); !ok {
			return nil, nil, fmt.Errorf("知らない RAM 初期化パターン %q", opts.ramInit)
		}
		add("--ram-init", func(c *config.Config) { c.Emulation.RAMInitPattern = opts.ramInit })
	}
	if opts.ramSeed != 0 {
		add("--ram-seed", func(c *config.Config) { c.Emulation.RAMSeed = opts.ramSeed })
	}
	if opts.deterministic {
		// 値が定まらない状態をすべて固定値にする（設計書 08 編 §8.5.2）。
		add("--deterministic", func(c *config.Config) {
			c.Emulation.RAMInitPattern = state.PatternZero.String()
			c.Emulation.RAMSeed = 0
			c.Emulation.CPUPPUAlignment = 0
			c.Emulation.DMAGetPutPhase = 0
			c.Emulation.PPUVBlankFlag = false
		})
	}
	if opts.movieVerify && opts.noMovieVerify {
		return nil, nil, fmt.Errorf("--movie-verify と --no-movie-verify は同時に指定できない")
	}
	if opts.movieVerify {
		add("--movie-verify", func(c *config.Config) { c.Movie.VerifyChecksums = true })
	}
	if opts.noMovieVerify {
		add("--no-movie-verify", func(c *config.Config) { c.Movie.VerifyChecksums = false })
	}
	if opts.logOutput != "" {
		if !slices.Contains([]string{config.LogStdout, config.LogStderr, config.LogFile}, opts.logOutput) {
			return nil, nil, fmt.Errorf("ログの出力先は stdout・stderr・file のいずれかを指定する（%q）", opts.logOutput)
		}
		add("--log", func(c *config.Config) { c.Debug.LogOutput = opts.logOutput })
	}
	if opts.logDir != "" {
		add("--log-dir", func(c *config.Config) { c.Paths.LogDir = opts.logDir })
	}
	if opts.logCategories != "" {
		cats := config.SplitList(opts.logCategories)
		for _, name := range cats {
			if !slices.Contains(config.LogCategoryNames(), name) {
				return nil, nil, fmt.Errorf("知らないログカテゴリ %q（%s）", name, strings.Join(config.LogCategoryNames(), ", "))
			}
		}
		add("--log-categories", func(c *config.Config) { c.Debug.LogCategories = cats })
	}
	if opts.breakAt != "" {
		if _, err := parseBreakAddrs(opts.breakAt); err != nil {
			return nil, nil, err
		}
	}
	if opts.headless && opts.romPath == "" {
		return nil, nil, fmt.Errorf("--headless には ROM のファイルを指定する")
	}
	return out, warnings, nil
}

// applyOptions は引数による上書きを cfg へ当てる。
func applyOptions(cfg *config.Config, opts options) error {
	overrides, _, err := optionOverrides(opts)
	if err != nil {
		return err
	}
	for _, o := range overrides {
		o.Apply(cfg)
	}
	return nil
}

// parseBreakAddrs は --break-at のアドレスを読む。カンマで複数を指定できる。
func parseBreakAddrs(s string) ([]uint16, error) {
	var out []uint16
	for _, part := range config.SplitList(s) {
		t := strings.TrimPrefix(strings.TrimPrefix(part, "$"), "0x")
		v, err := strconv.ParseUint(t, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("--break-at のアドレス %q を 16 進として読めない", part)
		}
		out = append(out, uint16(v))
	}
	return out, nil
}
