// Command shogun は NES エミュレータ「将軍エミュレータ」の実行ファイル。
//
// GUI アプリケーションとして起動し、コマンドラインから設定を上書きできる。
// 引数に ROM のファイルを渡すと、起動と同時に読み込む。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/ui"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

func main() {
	attachConsoleIfNeeded()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// logFileName はログファイルの名前。
const logFileName = "shogun.log"

// run は引数を解釈して終了コードを返す（設計書 11 編 §11.6）。
//
// main から分けてあるのは、終了コードと出力をテストから確かめられるように
// するためである。
func run(args []string, stdout, stderr io.Writer) int {
	if isSubcommand(args) {
		return runSubcommand(args, stdout, stderr)
	}
	opts, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}
	if opts.showVersion {
		printVersion(stdout)
		return exitOK
	}
	cliOverrides, cliWarnings, err := optionOverrides(opts)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return exitBadArgs
	}

	store, warnings, err := loadStore(opts, cliOverrides, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return exitROMError
	}
	for _, w := range append(warnings, cliWarnings...) {
		fmt.Fprintf(stderr, "%s: %s\n", appName, w)
	}

	cfg := store.Config()
	logOut, err := openLog(cfg, store.Paths)
	if err != nil {
		fmt.Fprintf(stderr, "%s: ログを開けないため標準エラー出力へ出す: %v\n", appName, err)
		logOut = nil
	}
	if logOut != nil {
		defer logOut.Close()
	}

	if opts.headless {
		return runHeadless(store, opts, logWriter(logOut), stdout, stderr)
	}
	startGUI(store, opts, logWriter(logOut), stderr)
	return exitOK
}

// loadStore は保存先を決め、設定ファイルとキーバインドを読み、環境変数と
// 引数の上書きを重ねる。
func loadStore(opts options, cliOverrides []config.Override, getenv func(string) string) (*config.Store, []string, error) {
	portable := opts.portable || config.EnvBool(getenv, config.EnvPortable) || config.PortableRequested()
	paths, err := config.ResolvePaths(portable, config.Overrides{})
	if err != nil {
		return nil, nil, err
	}
	cfgPath := paths.ConfigFile()
	keysPath := paths.KeybindingsFile()
	if p := firstNonEmpty(opts.configPath, getenv(config.EnvConfig)); p != "" {
		cfgPath = p
		keysPath = filepath.Join(filepath.Dir(p), config.KeybindingsFileName)
	}

	base, warnings, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}
	keys, kw, err := config.LoadKeybindings(keysPath, ui.KnownKeyCode)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, kw...)
	envOverrides, ew := config.EnvOverrides(getenv)
	warnings = append(warnings, ew...)

	overrides := append(envOverrides, cliOverrides...)
	return config.NewStore(paths, cfgPath, keysPath, base, overrides, keys), warnings, nil
}

// firstNonEmpty は空でない最初の値を返す。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// openLog は設定に従ってログの出力先を開く。none のとき nil を返す。
func openLog(cfg *config.Config, paths config.Paths) (io.WriteCloser, error) {
	if cfg.Debug.LogOutput == config.LogNone {
		return nil, nil
	}
	path := filepath.Join(paths.LogDir(cfg.Paths.LogDir), logFileName)
	return debug.OpenLogOutput(cfg.Debug.LogOutput, path, cfg.Debug.LogMaxBytes, cfg.Debug.LogGenerations)
}

// logWriter は nil の WriteCloser を nil の io.Writer にする。
func logWriter(w io.WriteCloser) io.Writer {
	if w == nil {
		return nil
	}
	return w
}

// emuConfig は設定からエミュレータの設定を作る。
func emuConfig(cfg *config.Config, paths config.Paths, logOut io.Writer, stderr io.Writer) emu.Config {
	return emu.Config{
		Emulation: cfg.Emulation,
		Input:     cfg.Input,
		Audio:     cfg.Audio,
		Paths:     cfg.Paths,
		State:     cfg.State,
		Movie:     cfg.Movie,
		Debug:     cfg.Debug,
		Dirs:      paths,
		LogWriter: logOut,
		AppName:   appTitle,
		Version:   version,
		Commit:    commit,
		Warn: func(format string, args ...any) {
			fmt.Fprintf(stderr, "%s: "+format+"\n", append([]any{appName}, args...)...)
		},
	}
}

// recordPalette は録画に使うパレットを返す。設定のパレットを読めないときは
// 既定のパレットを使う（画面の表示と同じ扱い）。
func recordPalette(cfg *config.Config) *video.Palette {
	if cfg.Video.PaletteFile != "" {
		if p, err := video.LoadPaletteFile(cfg.Video.PaletteFile); err == nil {
			return p
		}
	}
	return video.DefaultPalette()
}

// videoOverscan は設定のオーバースキャンを返す。
func videoOverscan(cfg *config.Config) video.Overscan {
	v := cfg.Video
	return video.Overscan{Top: v.OverscanTop, Bottom: v.OverscanBottom, Left: v.OverscanLeft, Right: v.OverscanRight}
}

// recordVideoDecision は GUI 起動時に --record-video を始めてよいかを判定する。
//
// ROM を指定していない、または読み込みに失敗しているときは、Emulator が
// 始まっていない（StartRecordingVideo の apply が戻らない）ため録画を始め
// ない。そのときは利用者へ理由を知らせる warning を返す（recordVideo が
// 空のときは知らせる必要が無いので warning も空文字になる）。
func recordVideoDecision(recordVideo string, romLoaded bool) (start bool, warning string) {
	if recordVideo == "" {
		return false, ""
	}
	if !romLoaded {
		return false, fmt.Sprintf("%s: --record-video には ROM を指定して読み込む必要がある", appName)
	}
	return true, ""
}

// startGUI は画面を開き、閉じられるまで戻らない。
func startGUI(store *config.Store, opts options, logOut io.Writer, stderr io.Writer) {
	cfg := store.Config()
	e := emu.New(emuConfig(cfg, store.Paths, logOut, stderr))
	if err := e.AudioError(); err != nil {
		// 音が出ないことで起動しない状態を作らない。
		fmt.Fprintf(stderr, "%s: 音声を初期化できないため無効にする: %v\n", appName, err)
	}
	if opts.breakAt != "" {
		addrs, _ := parseBreakAddrs(opts.breakAt)
		e.SetStartupBreakpoints(addrs)
	}
	// Finder で開いた .nes を受け取れるよう、アプリケーションを作る前に登録する。
	ui.InstallOpenFileHandler()
	u := ui.New(e, store, version)
	u.SetBuildDetails(commit, buildDate)
	if opts.speed != 0 {
		u.SetSpeed(opts.speed)
	}
	romLoaded := false
	if opts.romPath != "" {
		// 画面を開く前に読み込む。読み込めないときは知らせて続ける。
		// ROM を開けないことでアプリケーションが起動しない状態を作らない。
		e.Start()
		if err := e.LoadROM(opts.romPath); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		} else {
			romLoaded = true
			u.NotifyROMLoaded(opts.romPath)
			applyStartupOptions(e, opts, stderr)
			if opts.traceLog != "" {
				if err := u.StartTraceLog(opts.traceLog); err != nil {
					fmt.Fprintf(stderr, "%s: %v\n", appName, err)
				}
			}
		}
	}
	if opts.debug {
		u.OpenDebuggerOnStart()
	}
	// ROM を指定して読み込めたときだけ録画を始める。Emulator が始まって
	// いない（e.Start() を呼んでいない）状態で StartRecordingVideo を呼ぶと
	// apply が戻らずアプリケーションが起動しなくなる。
	if start, warning := recordVideoDecision(opts.recordVideo, romLoaded); start {
		if err := e.StartRecordingVideo(opts.recordVideo, recordPalette(cfg), cfg.Video.RecordScale, videoOverscan(cfg)); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	} else if warning != "" {
		fmt.Fprintln(stderr, warning)
	}
	u.Run()

	if opts.saveStateOnExit != "" {
		if err := e.SaveToFile(opts.saveStateOnExit); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
	if opts.screenshot != "" {
		if err := e.SaveScreenshot(opts.screenshot); err != nil {
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
