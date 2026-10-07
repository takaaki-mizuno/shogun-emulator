package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

// TestWXSSource は .wxs が XML として読め、設計書 13 編 §13.6 の項目を持つことを確かめる。
func TestWXSSource(t *testing.T) {
	docs := []docFile{{name: "USAGE.md"}, {name: "LICENSE"}, {name: "THIRD_PARTY_LICENSES.txt"}}
	src, err := wxsSource("0.9.0", docs)
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(strings.NewReader(src))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("XML として読めない: %v\n%s", err, src)
		}
	}
	for _, want := range []string{`Version="0.9.0"`, `UpgradeCode="` + msiUpgradeCode + `"`, `Scope="perMachine"`,
		`<MajorUpgrade`, `ProgramFiles64Folder`, `<Extension Id="nes" ContentType="application/x-nes-rom">`,
		`Argument="&quot;%1&quot;"`, `ProgramMenuFolder`, `Id="ARPPRODUCTICON"`, `WixUI_InstallDir`,
		`Source="USAGE.md" KeyPath="yes"`, `Source="LICENSE"`, `Source="THIRD_PARTY_LICENSES.txt"`,
		`Id="WixUILicenseRtf" Value="license.rtf"`} {
		if !strings.Contains(src, want) {
			t.Errorf(".wxs に %q が無い", want)
		}
	}
	if got := msiName("v0.9.0", "arm64"); got != "Shogun_Emulator-v0.9.0-windows-arm64.msi" {
		t.Errorf("msiName = %s", got)
	}
}

// TestLicenseRTF は RTF の特殊文字を逃がすことを確かめる。
func TestLicenseRTF(t *testing.T) {
	rtf := licenseRTF("a\\b {c}\r\né")
	for _, want := range []string{`{\rtf1`, `a\\b \{c\}\par`, `\u233?\par`} {
		if !strings.Contains(rtf, want) {
			t.Errorf("RTF に %q が無い:\n%s", want, rtf)
		}
	}
	if !strings.HasSuffix(rtf, "}\n") {
		t.Error("RTF が閉じていない")
	}
}

// TestInstallerImages は WiX の画像と .dmg の背景の大きさを確かめる。
func TestInstallerImages(t *testing.T) {
	logo, err := readLogo(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		data []byte
		w, h int
	}{
		"banner": {wixBanner(logo), wixBannerWidth, wixBannerHeight},
		"dialog": {wixDialog(logo), wixDialogWidth, wixDialogHeight},
	} {
		img, err := bmp.Decode(bytes.NewReader(c.data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if img.Bounds() != image.Rect(0, 0, c.w, c.h) {
			t.Errorf("%s の大きさ = %v", name, img.Bounds())
		}
	}
	for _, scale := range []int{1, 2} {
		bg := dmgBackground(scale)
		if bg.Bounds() != image.Rect(0, 0, dmgWindowWidth*scale, dmgWindowHeight*scale) {
			t.Errorf("背景（%dx）の大きさ = %v", scale, bg.Bounds())
		}
		// 2 つのアイコンの中ほどに矢印がある。
		mid := bg.NRGBAAt((dmgAppX+dmgApplicationsX)/2*scale, dmgIconY*scale)
		if mid != colorArrow {
			t.Errorf("背景（%dx）の中央が矢印の色でない: %v", scale, mid)
		}
	}
}

// TestDMGLayoutScript は AppleScript が各項目の位置を設定することを確かめる。
func TestDMGLayoutScript(t *testing.T) {
	script := dmgLayoutScript([]docFile{{name: "USAGE.md"}, {name: "LICENSE"}, {name: "THIRD_PARTY_LICENSES.txt"}})
	for _, want := range []string{`tell disk "Shogun Emulator"`, `set icon size of viewOptions to 96`,
		`file ".background:background.tiff"`, `item "Shogun Emulator.app" of container window to {160, 180}`,
		`item "Applications" of container window to {480, 180}`, `item "USAGE.md" of container window to {160, 370}`,
		`item "LICENSE" of container window to {320, 370}`, `item "THIRD_PARTY_LICENSES.txt" of container window to {480, 370}`} {
		if !strings.Contains(script, want) {
			t.Errorf("AppleScript に %q が無い:\n%s", want, script)
		}
	}
	plist := "<dict>\n\t<key>mount-point</key>\n\t<string>/Volumes/Shogun Emulator</string>\n</dict>"
	if got := mountPoint(plist); got != "/Volumes/Shogun Emulator" {
		t.Errorf("mountPoint = %q", got)
	}
}

// TestLinuxPackages は nfpm で .deb と .rpm を作り、.deb の中身と依存を確かめる
// （設計書 13 編 §13.7）。go tool nfpm を使うため、-short では飛ばす。
func TestLinuxPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("go tool nfpm のビルドに時間がかかる")
	}
	out := t.TempDir()
	paths, err := packageLinux(testRoot(t), "v0.9.0", "amd64", fakeBinary(t, "shogun"), out, true, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s が無い", p)
		}
	}
	for _, want := range []string{"shogun-emulator_0.9.0-1_amd64.deb", "shogun-emulator-0.9.0-1.x86_64.rpm"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s が無い: %v", want, names)
		}
	}

	members := readAr(t, filepath.Join(out, "shogun-emulator_0.9.0-1_amd64.deb"))
	control := tarFiles(t, members["control.tar.gz"])["./control"]
	for _, want := range []string{"Package: shogun-emulator", "Version: 0.9.0-1", "Architecture: amd64",
		"Section: games", "libasound2 | libasound2t64"} {
		if !strings.Contains(control, want) {
			t.Errorf("control に %q が無い:\n%s", want, control)
		}
	}
	data := tarFiles(t, members["data.tar.gz"])
	for _, want := range []string{"./usr/bin/shogun", "./usr/share/applications/shogun-emulator.desktop",
		"./usr/share/mime/packages/shogun-emulator.xml", "./usr/share/icons/hicolor/256x256/apps/shogun-emulator.png",
		"./usr/share/doc/shogun-emulator/copyright", "./usr/share/doc/shogun-emulator/USAGE.md"} {
		if _, ok := data[want]; !ok {
			t.Errorf(".deb に %s が無い", want)
		}
	}
}

// readAr は ar 書庫（.deb）の各メンバーを読む。
func readAr(t *testing.T, path string) map[string][]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("!<arch>\n")) {
		t.Fatal("ar 書庫でない")
	}
	out := map[string][]byte{}
	for p := 8; p+60 <= len(b); {
		h := b[p : p+60]
		name := strings.TrimSuffix(strings.TrimSpace(string(h[:16])), "/")
		var size int
		for _, c := range strings.TrimSpace(string(h[48:58])) {
			size = size*10 + int(c-'0')
		}
		out[name] = b[p+60 : p+60+size]
		p += 60 + size + size%2
	}
	return out
}

// tarFiles は gzip の tar の各ファイルの中身を読む。
func tarFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
}
