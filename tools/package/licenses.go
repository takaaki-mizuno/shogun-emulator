package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// licenseTargets は依存を調べるプラットフォーム。OS ごとに結び付く依存が
// 違うため、3 OS の和を一覧にする。
var licenseTargets = []string{"darwin", "windows", "linux"}

// module は go list が返すモジュールの情報。
type module struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

// listedPackage は go list -json が返すパッケージの情報のうち使うもの。
type listedPackage struct {
	Standard bool
	Module   *module
}

// writeLicenses は依存のライセンス一覧をファイルへ書く。
func writeLicenses(root, path string) error {
	text, err := thirdPartyLicenses(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// thirdPartyLicenses は実行ファイルに含まれる依存モジュールのライセンスを
// 集める（設計書 13 編 §13.7.1）。Go のランタイムと標準ライブラリも含める。
func thirdPartyLicenses(root string) (string, error) {
	mods, err := dependencyModules(root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("将軍エミュレータ（Shogun Emulator）の実行ファイルに含まれるソフトウェアのライセンス\n")
	b.WriteString("Third-party software licenses included in the Shogun Emulator executable\n")

	goroot, err := goEnv("GOROOT")
	if err != nil {
		return "", err
	}
	goLicense, err := readGoLicense(goroot)
	if err != nil {
		return "", err
	}
	section(&b, "Go（ランタイムと標準ライブラリ）", goLicense)

	for _, m := range mods {
		files, err := licenseFiles(m.Dir)
		if err != nil {
			return "", err
		}
		if len(files) == 0 {
			return "", fmt.Errorf("%s@%s にライセンスのファイルが無い", m.Path, m.Version)
		}
		var text []byte
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			text = append(text, data...)
			text = append(text, '\n')
		}
		section(&b, m.Path+" "+m.Version, bytes.TrimSpace(text))
	}
	return b.String(), nil
}

// section は 1 つのライセンスを見出し付きで書く。
func section(b *strings.Builder, title string, text []byte) {
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 78))
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 78))
	b.WriteString("\n\n")
	b.Write(bytes.TrimSpace(text))
	b.WriteString("\n")
}

// dependencyModules は cmd/shogun が 3 OS で依存するモジュールを、パスの順に返す。
// 本体のモジュールは除く。
func dependencyModules(root string) ([]module, error) {
	seen := map[string]module{}
	for _, goos := range licenseTargets {
		cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/shogun")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=1")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list（%s）: %w", goos, err)
		}
		dec := json.NewDecoder(bytes.NewReader(out))
		for {
			var p listedPackage
			if err := dec.Decode(&p); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, err
			}
			if p.Standard || p.Module == nil || p.Module.Main {
				continue
			}
			seen[p.Module.Path] = *p.Module
		}
	}
	mods := make([]module, 0, len(seen))
	for _, m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return mods, nil
}

// licenseFiles はモジュールのディレクトリの直下にあるライセンスのファイルを返す。
func licenseFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if e.IsDir() {
			continue
		}
		for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE"} {
			if strings.HasPrefix(name, prefix) {
				out = append(out, filepath.Join(dir, e.Name()))
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// goEnv は go env の値を返す。
func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// readGoLicense は Go のライセンスを読む。GOROOT の直下に無い配布形態
// （Homebrew は libexec を GOROOT とし、LICENSE をその親に置く）では親も探す。
func readGoLicense(goroot string) ([]byte, error) {
	var firstErr error
	for _, p := range []string{filepath.Join(goroot, "LICENSE"), filepath.Join(goroot, "..", "LICENSE")} {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, fmt.Errorf("Go のライセンスのファイルが見つからない: %w", firstErr)
}
