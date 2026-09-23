package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// AppDirName はスクリーンショットの保存先に使うディレクトリ名。
//
// 3 つの OS で同じ名前を使う。スクリーンショットは利用者が直接開く
// 場所に置くため、OS ごとに違う名前にすると探しにくい。
const AppDirName = "ShogunEmulator"

// ScreenshotDir はスクリーンショットの保存先を返す。
//
// override が空でないときはそれを使う。空のときはホームディレクトリの
// Pictures の下を使う。
func ScreenshotDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Pictures", AppDirName), nil
}

// AppDirNameUnix は Linux で使うディレクトリ名。
//
// Linux では小文字とハイフンで書く慣習に合わせる。設定とデータは
// 利用者が直接開く場所ではないため、OS ごとの慣習を優先する。
const AppDirNameUnix = "shogun-emulator"

// DataDir はセーブデータ・ステート・ムービーを置く場所を返す。
//
// override が空でないときはそれを使う。
func DataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if runtime.GOOS == "linux" {
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, AppDirNameUnix), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", AppDirNameUnix), nil
	}
	// macOS と Windows では設定と同じ場所に置く。
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppDirName), nil
}

// SaveDirName はバッテリーバックアップを置くディレクトリの名前。
const SaveDirName = "saves"

// SaveDir はバッテリーバックアップの保存先を返す。
//
// override が空でないときはそれをそのまま使う。空のときはデータ
// ディレクトリの下の saves を使う。
func SaveDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SaveDirName), nil
}

// StateDirName はセーブステートを置くディレクトリの名前。
const StateDirName = "states"

// StateDir はセーブステートの保存先を返す。
//
// override が空でないときはそれをそのまま使う。
func StateDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StateDirName), nil
}

// MovieDirName は入力ムービーを置くディレクトリの名前。
const MovieDirName = "movies"

// MovieDir は入力ムービーの保存先を返す。
func MovieDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, MovieDirName), nil
}

// SymbolsDirName は名前とブレークポイントを置くディレクトリの名前。
const SymbolsDirName = "symbols"

// SymbolsDir は名前とブレークポイントの保存先を返す（設計書 11 編 §11.2）。
func SymbolsDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SymbolsDirName), nil
}

// TraceDirName はトレースの書き出し先のディレクトリの名前。
const TraceDirName = "traces"

// TraceDir はトレースの書き出し先を返す。
func TraceDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, TraceDirName), nil
}

// PatchesDirName は CHR と PRG のオーバーレイを置くディレクトリの名前。
const PatchesDirName = "patches"

// PatchesDir はオーバーレイの保存先を返す（設計書 11 編 §11.2）。
func PatchesDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := DataDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, PatchesDirName), nil
}
