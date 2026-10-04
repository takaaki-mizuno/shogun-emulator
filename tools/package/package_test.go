package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testRoot はモジュールのルートを返す。
func testRoot(t *testing.T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fakeBinary は仮の実行ファイルを書く。
func fakeBinary(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestVersionNumber はタグから数字の版を取り出すことを確かめる。
func TestVersionNumber(t *testing.T) {
	for tag, want := range map[string]string{
		"v1.2.3": "1.2.3", "v1.2.3-rc.1": "1.2.3", "1.4": "1.4.0", "dev": "0.0.0", "v1.x.0": "0.0.0",
	} {
		if got := versionNumber(tag); got != want {
			t.Errorf("versionNumber(%q) = %q, 期待 %q", tag, got, want)
		}
	}
}

// TestPackageWindowsZip は .zip に実行ファイルと同梱の文書が入ることを確かめる（設計書 13 編 §13.6）。
func TestPackageWindowsZip(t *testing.T) {
	out := t.TempDir()
	path, err := packageWindows(testRoot(t), "v1.0.0", "amd64", fakeBinary(t, "shogun.exe"), out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "shogun-emulator-v1.0.0-windows-amd64.zip" {
		t.Errorf("名前 = %s", filepath.Base(path))
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	prefix := "shogun-emulator-v1.0.0-windows-amd64/"
	for _, want := range []string{"shogun.exe", "USAGE.md", "LICENSE", "THIRD_PARTY_LICENSES.txt"} {
		if !slices.Contains(names, prefix+want) {
			t.Errorf("%s が無い: %v", want, names)
		}
	}
}

// TestPackageLinuxTarGz は .tar.gz の中身と、AppDir の構成を確かめる（設計書 13 編 §13.7）。
func TestPackageLinuxTarGz(t *testing.T) {
	root := testRoot(t)
	out := t.TempDir()
	paths, err := packageLinux(root, "v1.0.0", "arm64", fakeBinary(t, "shogun"), out, false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	modes := map[string]int64{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		modes[h.Name] = h.Mode
	}
	prefix := "shogun-emulator-v1.0.0-linux-arm64/"
	if modes[prefix+"shogun"] != 0o755 {
		t.Errorf("実行ファイルの許可 = %o", modes[prefix+"shogun"])
	}
	for _, want := range []string{"share/applications/shogun-emulator.desktop", "share/mime/packages/shogun-emulator.xml",
		"share/icons/hicolor/16x16/apps/shogun-emulator.png", "share/icons/hicolor/512x512/apps/shogun-emulator.png",
		"USAGE.md", "LICENSE", "THIRD_PARTY_LICENSES.txt"} {
		if _, ok := modes[prefix+want]; !ok {
			t.Errorf("%s が無い", want)
		}
	}

	// AppDir は appimagetool が無くても組み立てられる。
	share, err := linuxShareEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := bundledDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(out, "AppDir")
	if err := buildAppDir(dir, []byte("binary"), share, docs); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "AppRun")); err != nil || target != "usr/bin/shogun" {
		t.Errorf("AppRun = %q, %v", target, err)
	}
	for _, want := range []string{"shogun-emulator.desktop", "shogun-emulator.png", "usr/bin/shogun",
		"usr/share/icons/hicolor/256x256/apps/shogun-emulator.png"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("AppDir に %s が無い", want)
		}
	}
	if _, err := packageLinux(root, "v1.0.0", "arm64", fakeBinary(t, "shogun"), out, true); err == nil && os.Getenv("APPIMAGETOOL") == "" {
		t.Error("APPIMAGETOOL が無いのに AppImage を作れたことになっている")
	}
}

// TestInfoPlist は Info.plist が設計書 13 編 §13.5 の項目を持つことを確かめる。
func TestInfoPlist(t *testing.T) {
	p := infoPlist("v2.3.4")
	for _, want := range []string{"<string>com.shogunemulator.app</string>", "<string>Shogun Emulator</string>",
		"<key>CFBundleShortVersionString</key>\n\t<string>2.3.4</string>", "<key>CFBundleIconFile</key>\n\t<string>icon</string>",
		"<string>11.0</string>", "<key>NSHighResolutionCapable</key>\n\t<true/>", "<string>nes</string>", "<string>public.data</string>",
		"<key>CFBundleExecutable</key>\n\t<string>shogun</string>"} {
		if !strings.Contains(p, want) {
			t.Errorf("Info.plist に %q が無い", want)
		}
	}
}

// TestThirdPartyLicenses はライセンス一覧に主な依存と Go が入ることを確かめる。
func TestThirdPartyLicenses(t *testing.T) {
	text, err := thirdPartyLicenses(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Go（ランタイムと標準ライブラリ）", "fyne.io/fyne/v2", "github.com/ebitengine/oto/v3",
		"github.com/ncruces/zenity", "golang.org/x/sys"} {
		if !strings.Contains(text, want) {
			t.Errorf("一覧に %s が無い", want)
		}
	}
	if strings.Contains(text, "goversioninfo") {
		t.Error("ビルド時だけの goversioninfo が一覧に入っている")
	}
}
