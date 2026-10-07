package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// .deb と .rpm の固定値（設計書 13 編 §13.7）。
const (
	packageMaintainer = "Takaaki Mizuno"
	packageHomepage   = "https://github.com/takaaki-mizuno/shogun-emulator"
	packageSummary    = "NES (Famicom) emulator with debugging tools and an interface for AI agents"
)

// 依存するパッケージ。実行ファイルが動的に結び付く OpenGL・X11・ALSA の
// ライブラリを、それぞれの系統のパッケージ名で並べる。
var (
	debDepends = []string{"libgl1", "libx11-6", "libxcursor1", "libxi6", "libxinerama1", "libxrandr2",
		"libxxf86vm1", "libasound2 | libasound2t64"}
	rpmDepends = []string{"mesa-libGL", "libX11", "libXcursor", "libXi", "libXinerama", "libXrandr",
		"libXxf86vm", "alsa-lib"}
)

// nfpmContent は nfpm の設定の contents の 1 項目。
type nfpmContent struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Packager string `json:"packager,omitempty"`
	// FileInfo の mode は 10 進の整数で書く。JSON には 8 進の表記が無い。
	FileInfo map[string]int `json:"file_info"`
}

// buildLinuxPackages は .deb と .rpm を作る。
//
// nfpm の設定を JSON で書く。JSON は YAML として読めるため、nfpm がそのまま扱える。
func buildLinuxPackages(root, version, arch string, exe []byte, share []entry, docs []docFile, out string) ([]string, error) {
	stage, err := filepath.Abs(filepath.Join(out, "nfpm-"+arch))
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(stage); err != nil {
		return nil, err
	}
	files := filepath.Join(stage, "files")
	es := []entry{{name: "usr/bin/shogun", data: exe, mode: 0o755}}
	for _, e := range share {
		e.name = "usr/" + e.name
		es = append(es, e)
	}
	es = append(es, docEntries("usr/share/doc/"+appID+"/", docs)...)
	if err := writeTree(files, es); err != nil {
		return nil, err
	}

	var contents []nfpmContent
	for _, e := range es {
		contents = append(contents, nfpmContent{
			Src: filepath.Join(files, e.name), Dst: "/" + e.name,
			FileInfo: map[string]int{"mode": int(e.mode)},
		})
	}
	// Debian の方針に合わせ、ライセンスを copyright の名前でも置く。
	contents = append(contents, nfpmContent{
		Src: filepath.Join(files, "usr/share/doc", appID, licenseFileName), Dst: "/usr/share/doc/" + appID + "/copyright",
		Packager: "deb", FileInfo: map[string]int{"mode": 0o644},
	})

	config := map[string]any{
		"name":           appID,
		"arch":           arch,
		"platform":       "linux",
		"version":        versionNumber(version),
		"version_schema": "none",
		"release":        "1",
		"section":        "games",
		"priority":       "optional",
		"maintainer":     packageMaintainer,
		"vendor":         packageMaintainer,
		"homepage":       packageHomepage,
		"license":        "MIT",
		"description":    packageSummary,
		"contents":       contents,
		"rpm":            map[string]any{"group": "Amusements/Games"},
		"overrides": map[string]any{
			"deb": map[string]any{"depends": debDepends},
			"rpm": map[string]any{"depends": rpmDepends},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(stage, "nfpm.yaml")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return nil, err
	}

	absOut, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range []string{"deb", "rpm"} {
		cmd := exec.Command("go", "tool", "nfpm", "package", "-f", configPath, "-p", p, "-t", absOut)
		cmd.Dir = root
		var stdout strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return paths, fmt.Errorf("nfpm（%s）: %w", p, err)
		}
		paths = append(paths, filepath.Join(absOut, linuxPackageName(p, version, arch)))
	}
	return paths, nil
}

// rpmArch は Go のアーキテクチャ名を rpm の名前に直す。
var rpmArch = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}

// linuxPackageName は nfpm が付ける .deb と .rpm のファイル名を返す。
func linuxPackageName(packager, version, arch string) string {
	v := versionNumber(version)
	if packager == "deb" {
		return fmt.Sprintf("%s_%s-1_%s.deb", appID, v, arch)
	}
	return fmt.Sprintf("%s-%s-1.%s.rpm", appID, v, rpmArch[arch])
}
