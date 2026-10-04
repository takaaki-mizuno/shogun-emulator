package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// packageWindows は Windows 向けの .zip を作る（設計書 13 編 §13.6）。
func packageWindows(root, version, arch, bin, out string) (string, error) {
	if bin == "" {
		return "", errors.New("-bin に shogun.exe のパスを指定する")
	}
	exe, err := os.ReadFile(bin)
	if err != nil {
		return "", err
	}
	docs, err := bundledDocs(root)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("shogun-emulator-%s-windows-%s", version, arch)
	es := append(docEntries(name+"/", docs), entry{name: name + "/shogun.exe", data: exe, mode: 0o755})
	path := filepath.Join(out, name+".zip")
	return path, writeZip(path, es)
}
