package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// packageWindows は Windows 向けの .zip を作り、msi のとき .msi も作る
// （設計書 13 編 §13.6）。
func packageWindows(root, version, arch, bin, out string, msi bool) ([]string, error) {
	if bin == "" {
		return nil, errors.New("-bin に shogun.exe のパスを指定する")
	}
	exe, err := os.ReadFile(bin)
	if err != nil {
		return nil, err
	}
	docs, err := bundledDocs(root)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("shogun-emulator-%s-windows-%s", version, arch)
	es := append(docEntries(name+"/", docs), entry{name: name + "/shogun.exe", data: exe, mode: 0o755})
	zipPath := filepath.Join(out, name+".zip")
	if err := writeZip(zipPath, es); err != nil {
		return nil, err
	}
	paths := []string{zipPath}
	if !msi {
		return paths, nil
	}
	msiPath, err := buildMSI(root, version, arch, exe, docs, out)
	if err != nil {
		return paths, err
	}
	return append(paths, msiPath), nil
}
