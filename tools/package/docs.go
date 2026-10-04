package main

import (
	"os"
	"path/filepath"
	"strings"
)

// 配布物に同梱する文書（設計書 13 編 §13.7.1）。
const (
	licensesFileName = "THIRD_PARTY_LICENSES.txt"
	usageSource      = "packaging/usage.md"
	usageFileName    = "USAGE.md"
	licenseFileName  = "LICENSE"
)

// docFile は配布物に入れる 1 つのファイル。
type docFile struct {
	// name は配布物の中の名前。
	name string
	data []byte
}

// bundledDocs は使い方・ライセンス・依存のライセンス一覧を読む。
//
// 依存のライセンス一覧は go list の結果から作る。
func bundledDocs(root string) ([]docFile, error) {
	usage, err := os.ReadFile(filepath.Join(root, usageSource))
	if err != nil {
		return nil, err
	}
	license, err := os.ReadFile(filepath.Join(root, licenseFileName))
	if err != nil {
		return nil, err
	}
	third, err := thirdPartyLicenses(root)
	if err != nil {
		return nil, err
	}
	return []docFile{
		{usageFileName, usage},
		{licenseFileName, license},
		{licensesFileName, []byte(third)},
	}, nil
}

// versionNumber はタグの名前から "1.2.3" の形の版を取り出す。
// 数字で始まらないときは "0.0.0" を返す。
func versionNumber(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	for _, p := range parts[:3] {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return "0.0.0"
		}
	}
	return strings.Join(parts[:3], ".")
}
