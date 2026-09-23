package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// アプリケーション識別子（設計書 11 編 §11.2）。
const (
	// AppDirName は macOS と Windows のディレクトリ名。
	AppDirName = "ShogunEmulator"
	// AppDirNameUnix は Linux のディレクトリ名。小文字とハイフンで書く慣習に合わせる。
	AppDirNameUnix = "shogun-emulator"
	// fallbackDirName は OS の標準のディレクトリを求められないときにホーム
	// ディレクトリ直下へ置くディレクトリ名。
	fallbackDirName = ".shogun-emulator"
	// PortableMarker は実行ファイルの横に置くとポータブルモードになるファイル。
	PortableMarker = "portable.txt"
)

// データディレクトリの下のディレクトリ名。
const (
	SaveDirName    = "saves"
	StateDirName   = "states"
	MovieDirName   = "movies"
	SymbolsDirName = "symbols"
	TraceDirName   = "traces"
	PatchesDirName = "patches"
)

// ファイル名。
const (
	ConfigFileName      = "config.json"
	KeybindingsFileName = "keybindings.json"
)

// Paths は保存先のディレクトリ（設計書 11 編 §11.2）。
//
// 各パッケージは os.UserConfigDir などを直接呼ばず、ResolvePaths の結果を
// 受け取る。ポータブルモードと上書きを 1 か所で扱うためである。
type Paths struct {
	Config      string // 設定ファイルのディレクトリ
	Data        string // セーブデータ、ステート、ムービー
	Cache       string
	Logs        string
	Screenshots string
}

// Overrides は環境変数と引数で指定された保存先。空の項目は既定の場所を使う。
type Overrides struct {
	Config, Data, Logs, Screenshots string
}

// platform は保存先の決定に使う OS の情報。テストで差し替える。
type platform struct {
	goos          string
	getenv        func(string) string
	home          func() (string, error)
	userConfigDir func() (string, error)
	userCacheDir  func() (string, error)
	executableDir func() (string, error)
	mkdirAll      func(string, os.FileMode) error
}

// hostPlatform は実行中の OS の情報を返す。
func hostPlatform() platform {
	return platform{
		goos:          runtime.GOOS,
		getenv:        os.Getenv,
		home:          os.UserHomeDir,
		userConfigDir: os.UserConfigDir,
		userCacheDir:  os.UserCacheDir,
		executableDir: func() (string, error) {
			exe, err := os.Executable()
			if err != nil {
				return "", err
			}
			return filepath.Dir(exe), nil
		},
		mkdirAll: os.MkdirAll,
	}
}

// ResolvePaths は保存先を決め、設定・データ・ログのディレクトリを作る。
func ResolvePaths(portable bool, o Overrides) (Paths, error) {
	return resolvePaths(hostPlatform(), portable, o, true)
}

// DefaultPaths はポータブルモードでも上書きでもない既定の保存先を返す。
// ディレクトリは作らない。保存先を与えられなかった部品が使う。
func DefaultPaths() (Paths, error) {
	return resolvePaths(hostPlatform(), false, Overrides{}, false)
}

// PortableRequested は実行ファイルの横に portable.txt があるかを返す。
func PortableRequested() bool {
	dir, err := hostPlatform().executableDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, PortableMarker))
	return err == nil
}

// resolvePaths は platform に従って保存先を決める。
func resolvePaths(pf platform, portable bool, o Overrides, create bool) (Paths, error) {
	var p Paths
	var err error
	if portable {
		p, err = portablePaths(pf)
	} else {
		p, err = standardPaths(pf)
	}
	if err != nil {
		return Paths{}, err
	}
	if o.Config != "" {
		p.Config = o.Config
	}
	if o.Data != "" {
		p.Data = o.Data
	}
	if o.Logs != "" {
		p.Logs = o.Logs
	}
	if o.Screenshots != "" {
		p.Screenshots = o.Screenshots
	}
	if create {
		for _, dir := range []string{p.Config, p.Data, p.Logs} {
			if err := pf.mkdirAll(dir, 0o755); err != nil {
				return Paths{}, err
			}
		}
	}
	return p, nil
}

// portablePaths は実行ファイルのディレクトリを保存先にする（設計書 11 編 §11.2.1）。
func portablePaths(pf platform) (Paths, error) {
	dir, err := pf.executableDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Config:      dir,
		Data:        dir,
		Cache:       filepath.Join(dir, "cache"),
		Logs:        filepath.Join(dir, "logs"),
		Screenshots: filepath.Join(dir, "screenshots"),
	}, nil
}

// standardPaths は OS ごとの標準の保存先を返す（設計書 11 編 §11.2 の表）。
func standardPaths(pf platform) (Paths, error) {
	home, homeErr := pf.home()
	fallback := func() (string, error) {
		if homeErr != nil {
			return "", errors.New("config: 保存先のディレクトリを決められない（ホームディレクトリが分からない）")
		}
		return filepath.Join(home, fallbackDirName), nil
	}
	orFallback := func(dir string, err error, name string) (string, error) {
		if err != nil || dir == "" {
			fb, ferr := fallback()
			if ferr != nil {
				return "", ferr
			}
			return filepath.Join(fb, name), nil
		}
		return dir, nil
	}
	appName := AppDirName
	if pf.goos == "linux" {
		appName = AppDirNameUnix
	}

	cfgBase, cfgErr := pf.userConfigDir()
	configDir, err := orFallback(join(cfgBase, appName), cfgErr, "config")
	if err != nil {
		return Paths{}, err
	}
	cacheBase, cacheErr := pf.userCacheDir()
	cacheDir, err := orFallback(join(cacheBase, appName), cacheErr, "cache")
	if err != nil {
		return Paths{}, err
	}

	var p Paths
	p.Config = configDir
	switch pf.goos {
	case "linux":
		p.Cache = cacheDir
		data := pf.getenv("XDG_DATA_HOME")
		if data == "" && homeErr == nil {
			data = filepath.Join(home, ".local", "share")
		}
		state := pf.getenv("XDG_STATE_HOME")
		if state == "" && homeErr == nil {
			state = filepath.Join(home, ".local", "state")
		}
		if data == "" || state == "" {
			return Paths{}, errors.New("config: 保存先のディレクトリを決められない（ホームディレクトリが分からない）")
		}
		p.Data = filepath.Join(data, appName)
		p.Logs = filepath.Join(state, appName, "logs")
	case "windows":
		p.Data = configDir
		// %LocalAppData%\ShogunEmulator の下にキャッシュとログを置く。
		local, lerr := orFallback(cacheBase, cacheErr, "local")
		if lerr != nil {
			return Paths{}, lerr
		}
		p.Cache = filepath.Join(local, appName, "cache")
		p.Logs = filepath.Join(local, appName, "logs")
	default: // darwin とその他
		p.Data = configDir
		p.Cache = cacheDir
		if homeErr == nil {
			p.Logs = filepath.Join(home, "Library", "Logs", appName)
		} else {
			p.Logs = filepath.Join(configDir, "logs")
		}
	}
	if homeErr == nil {
		p.Screenshots = filepath.Join(home, "Pictures", AppDirName)
	} else {
		p.Screenshots = filepath.Join(p.Data, "screenshots")
	}
	return p, nil
}

// join は base が空のとき空を返す filepath.Join。
func join(base, name string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(base, name)
}

// under は override が空でなければそれを、空なら base/name を返す。
func under(override, base, name string) string {
	if override != "" {
		return override
	}
	return filepath.Join(base, name)
}

// ConfigFile は設定ファイルのパスを返す。
func (p Paths) ConfigFile() string { return filepath.Join(p.Config, ConfigFileName) }

// KeybindingsFile はキーバインドファイルのパスを返す。
func (p Paths) KeybindingsFile() string { return filepath.Join(p.Config, KeybindingsFileName) }

// SaveDir はバッテリーバックアップの保存先を返す。override が空でなければそれを使う。
func (p Paths) SaveDir(override string) string { return under(override, p.Data, SaveDirName) }

// StateDir はセーブステートの保存先を返す。
func (p Paths) StateDir(override string) string { return under(override, p.Data, StateDirName) }

// MovieDir は入力ムービーの保存先を返す。
func (p Paths) MovieDir(override string) string { return under(override, p.Data, MovieDirName) }

// SymbolsDir は名前とブレークポイントの保存先を返す。
func (p Paths) SymbolsDir(override string) string { return under(override, p.Data, SymbolsDirName) }

// TraceDir はトレースの書き出し先を返す。
func (p Paths) TraceDir(override string) string { return under(override, p.Data, TraceDirName) }

// PatchesDir はオーバーレイの保存先を返す。
func (p Paths) PatchesDir(override string) string { return under(override, p.Data, PatchesDirName) }

// ScreenshotDir はスクリーンショットの保存先を返す。
func (p Paths) ScreenshotDir(override string) string {
	if override != "" {
		return override
	}
	return p.Screenshots
}

// LogDir はログファイルの保存先を返す。
func (p Paths) LogDir(override string) string {
	if override != "" {
		return override
	}
	return p.Logs
}
