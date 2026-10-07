// Command gen-icon は原画からアプリケーションのアイコンとロゴを生成する。
//
// 原画 assets/icon.png（正方形）を唯一の原本とし、各 OS 向けの形式・
// 実行時のアイコン・アプリ内のロゴを書く（設計書 13 編 §13.4）。
//
// 使い方:
//
//	go run ./tools/gen-icon
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-icon: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	src, err := readPNG(filepath.Join(root, sourcePath))
	if err != nil {
		return err
	}
	return derive(src, root)
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
			return "", fmt.Errorf("go.mod が見つからない")
		}
		dir = parent
	}
}
