// Command package は配布物を作る（設計書 13 編 §13.5〜§13.7）。
//
// 使い方:
//
//	go run ./tools/package licenses -o dist/THIRD_PARTY_LICENSES.txt
//	go run ./tools/package macos   -version v1.2.3 -arm64 dist/darwin_arm64/shogun -amd64 dist/darwin_amd64/shogun -out dist
//	go run ./tools/package windows -version v1.2.3 -arch amd64 -bin dist/windows_amd64/shogun.exe -out dist
//	go run ./tools/package linux   -version v1.2.3 -arch amd64 -bin dist/linux_amd64/shogun -out dist [-appimage]
//
// macos は lipo と hdiutil を使うため macOS で、linux の -appimage は
// appimagetool を使うため Linux で実行する。windows と linux の書庫は
// Go の標準ライブラリで作るため、どの OS でも作れる。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "package: %v\n", err)
		os.Exit(1)
	}
}

// run はサブコマンドを振り分ける。
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("サブコマンド（licenses・macos・windows・linux）を指定する")
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	version := fs.String("version", "dev", "版（タグの名前）")
	out := fs.String("out", "dist", "出力先のディレクトリ")
	switch args[0] {
	case "licenses":
		o := fs.String("o", filepath.Join("dist", licensesFileName), "出力するファイル")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return writeLicenses(root, *o)
	case "macos":
		arm := fs.String("arm64", "", "arm64 の実行ファイル")
		amd := fs.String("amd64", "", "amd64 の実行ファイル")
		layout := fs.Bool("layout", true, ".dmg のウィンドウの見た目を Finder で設定する")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path, err := packageMacOS(root, *version, *arm, *amd, *out, *layout)
		report(path)
		return err
	case "windows":
		arch := fs.String("arch", "amd64", "アーキテクチャ")
		bin := fs.String("bin", "", "shogun.exe のパス")
		msi := fs.Bool("msi", true, ".msi も作る（WiX Toolset v5 が要る。環境変数 WIX に wix の場所を入れられる）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		paths, err := packageWindows(root, *version, *arch, *bin, *out, *msi)
		for _, p := range paths {
			report(p)
		}
		return err
	case "linux":
		arch := fs.String("arch", "amd64", "アーキテクチャ")
		bin := fs.String("bin", "", "shogun のパス")
		packages := fs.Bool("packages", true, ".deb と .rpm も作る（go tool nfpm を使う）")
		appimage := fs.Bool("appimage", false, "AppImage も作る（環境変数 APPIMAGETOOL に appimagetool の場所を入れる）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		paths, err := packageLinux(root, *version, *arch, *bin, *out, *packages, *appimage)
		for _, p := range paths {
			report(p)
		}
		return err
	}
	return fmt.Errorf("知らないサブコマンド %q", args[0])
}

// report は作ったファイルを表示する。
func report(path string) {
	if path != "" {
		fmt.Println(path)
	}
}

// moduleRoot は go.mod のあるディレクトリを上へたどって探す。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod が見つからない")
		}
		dir = parent
	}
}
