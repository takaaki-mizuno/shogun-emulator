package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Linux の配布物に入れるファイル（設計書 13 編 §13.7）。
const (
	desktopSource = "packaging/linux/shogun-emulator.desktop"
	mimeSource    = "packaging/linux/shogun-emulator.xml"
	iconDir       = "assets/icon/generated/linux"
	appID         = "shogun-emulator"
)

// linuxIconSizes は hicolor へ置くアイコンの大きさ。
var linuxIconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// appImageArch は Go のアーキテクチャ名を AppImage の名前に直す。
var appImageArch = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}

// packageLinux は .tar.gz を作り、packages のとき .deb と .rpm を、appimage の
// とき AppImage も作る（設計書 13 編 §13.7）。
func packageLinux(root, version, arch, bin, out string, packages, appimage bool) ([]string, error) {
	if bin == "" {
		return nil, errors.New("-bin に shogun のパスを指定する")
	}
	exe, err := os.ReadFile(bin)
	if err != nil {
		return nil, err
	}
	docs, err := bundledDocs(root)
	if err != nil {
		return nil, err
	}
	share, err := linuxShareEntries(root)
	if err != nil {
		return nil, err
	}

	name := fmt.Sprintf("shogun-emulator-%s-linux-%s", version, arch)
	es := docEntries(name+"/", docs)
	es = append(es, entry{name: name + "/shogun", data: exe, mode: 0o755})
	for _, e := range share {
		e.name = name + "/" + e.name
		es = append(es, e)
	}
	tarPath := filepath.Join(out, name+".tar.gz")
	if err := writeTarGz(tarPath, es); err != nil {
		return nil, err
	}
	paths := []string{tarPath}
	if packages {
		pkgs, err := buildLinuxPackages(root, version, arch, exe, share, docs, out)
		paths = append(paths, pkgs...)
		if err != nil {
			return paths, err
		}
	}
	if !appimage {
		return paths, nil
	}

	appDir := filepath.Join(out, "AppDir-"+arch)
	if err := buildAppDir(appDir, exe, share, docs); err != nil {
		return paths, err
	}
	tool := os.Getenv("APPIMAGETOOL")
	if tool == "" {
		return paths, errors.New("環境変数 APPIMAGETOOL に appimagetool の場所を入れる")
	}
	img := filepath.Join(out, fmt.Sprintf("Shogun_Emulator-%s-%s.AppImage", version, appImageArch[arch]))
	cmd := exec.Command(tool, appDir, img)
	cmd.Env = append(os.Environ(), "ARCH="+appImageArch[arch])
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return paths, fmt.Errorf("appimagetool: %w", err)
	}
	return append(paths, img), nil
}

// linuxShareEntries は .desktop・MIME の定義・hicolor のアイコンを、
// usr/share 以下の構成で並べる。
func linuxShareEntries(root string) ([]entry, error) {
	var es []entry
	read := func(src, dst string) error {
		data, err := os.ReadFile(filepath.Join(root, src))
		if err != nil {
			return err
		}
		es = append(es, entry{name: dst, data: data, mode: 0o644})
		return nil
	}
	if err := read(desktopSource, "share/applications/"+appID+".desktop"); err != nil {
		return nil, err
	}
	if err := read(mimeSource, "share/mime/packages/"+appID+".xml"); err != nil {
		return nil, err
	}
	for _, size := range linuxIconSizes {
		src := filepath.Join(iconDir, fmt.Sprintf("icon-%d.png", size))
		dst := fmt.Sprintf("share/icons/hicolor/%dx%d/apps/%s.png", size, size, appID)
		if err := read(src, dst); err != nil {
			return nil, err
		}
	}
	return es, nil
}

// buildAppDir は AppImage の元になる AppDir を組み立てる（設計書 13 編 §13.7）。
func buildAppDir(dir string, exe []byte, share []entry, docs []docFile) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	es := []entry{{name: "usr/bin/shogun", data: exe, mode: 0o755}}
	var desktop, icon []byte
	for _, e := range share {
		e.name = "usr/" + e.name
		es = append(es, e)
		switch e.name {
		case "usr/share/applications/" + appID + ".desktop":
			desktop = e.data
		case "usr/share/icons/hicolor/256x256/apps/" + appID + ".png":
			icon = e.data
		}
	}
	es = append(es,
		entry{name: appID + ".desktop", data: desktop, mode: 0o644},
		entry{name: appID + ".png", data: icon, mode: 0o644},
	)
	es = append(es, docEntries("usr/share/doc/"+appID+"/", docs)...)
	if err := writeTree(dir, es); err != nil {
		return err
	}
	return os.Symlink("usr/bin/shogun", filepath.Join(dir, "AppRun"))
}
