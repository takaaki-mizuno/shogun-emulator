package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// macOS の配布物の名前（設計書 13 編 §13.5）。
const (
	appBundleName  = "Shogun Emulator.app"
	bundleID       = "com.shogunemulator.app"
	icnsSource     = "assets/icon/generated/icon.icns"
	minimumMacOS   = "11.0"
	nesDocumentUTI = "com.shogunemulator.nes-rom"
)

// packageMacOS は arm64 と amd64 の実行ファイルからユニバーサルバイナリを作り、
// .app と .dmg を作る。片方だけ渡したときはそれを使う。
func packageMacOS(root, version, arm64, amd64, out string) (string, error) {
	var bins []string
	for _, b := range []string{arm64, amd64} {
		if b != "" {
			bins = append(bins, b)
		}
	}
	if len(bins) == 0 {
		return "", errors.New("-arm64 と -amd64 の少なくとも一方に実行ファイルを指定する")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	universal := filepath.Join(out, "shogun-universal")
	if len(bins) == 2 {
		if err := runTool("lipo", append([]string{"-create", "-output", universal}, bins...)...); err != nil {
			return "", err
		}
	} else {
		data, err := os.ReadFile(bins[0])
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(universal, data, 0o755); err != nil {
			return "", err
		}
	}
	exe, err := os.ReadFile(universal)
	if err != nil {
		return "", err
	}
	icns, err := os.ReadFile(filepath.Join(root, icnsSource))
	if err != nil {
		return "", err
	}
	docs, err := bundledDocs(root)
	if err != nil {
		return "", err
	}

	staging := filepath.Join(out, "dmg-staging")
	if err := os.RemoveAll(staging); err != nil {
		return "", err
	}
	app := appBundleName + "/Contents/"
	es := []entry{
		{name: app + "Info.plist", data: []byte(infoPlist(version)), mode: 0o644},
		{name: app + "MacOS/shogun", data: exe, mode: 0o755},
		{name: app + "Resources/icon.icns", data: icns, mode: 0o644},
	}
	es = append(es, docEntries("", docs)...)
	if err := writeTree(staging, es); err != nil {
		return "", err
	}
	if err := os.Symlink("/Applications", filepath.Join(staging, "Applications")); err != nil {
		return "", err
	}

	dmg := filepath.Join(out, fmt.Sprintf("Shogun_Emulator-%s-macos.dmg", version))
	os.Remove(dmg)
	err = runTool("hdiutil", "create", "-volname", "Shogun Emulator "+version,
		"-srcfolder", staging, "-ov", "-format", "UDZO", dmg)
	return dmg, err
}

// runTool は外部のコマンドを実行する。
func runTool(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// infoPlist は .app の Info.plist を作る（設計書 13 編 §13.5 の表）。
//
// .nes の種類を UTImportedTypeDeclarations で public.data に準じるものとして
// 宣言し、CFBundleDocumentTypes でその種類を開けることを示す。これにより
// Finder で .nes ファイルを本アプリケーションで開ける。
func infoPlist(version string) string {
	v := versionNumber(version)
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>ja</string>
	<key>CFBundleExecutable</key>
	<string>shogun</string>
	<key>CFBundleIdentifier</key>
	<string>` + bundleID + `</string>
	<key>CFBundleName</key>
	<string>Shogun Emulator</string>
	<key>CFBundleDisplayName</key>
	<string>Shogun Emulator</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + v + `</string>
	<key>CFBundleVersion</key>
	<string>` + v + `</string>
	<key>CFBundleIconFile</key>
	<string>icon</string>
	<key>LSMinimumSystemVersion</key>
	<string>` + minimumMacOS + `</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.games</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>UTImportedTypeDeclarations</key>
	<array>
		<dict>
			<key>UTTypeIdentifier</key>
			<string>` + nesDocumentUTI + `</string>
			<key>UTTypeDescription</key>
			<string>NES ROM</string>
			<key>UTTypeConformsTo</key>
			<array>
				<string>public.data</string>
			</array>
			<key>UTTypeTagSpecification</key>
			<dict>
				<key>public.filename-extension</key>
				<array>
					<string>nes</string>
				</array>
			</dict>
		</dict>
	</array>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeName</key>
			<string>NES ROM</string>
			<key>CFBundleTypeRole</key>
			<string>Viewer</string>
			<key>LSHandlerRank</key>
			<string>Owner</string>
			<key>LSItemContentTypes</key>
			<array>
				<string>` + nesDocumentUTI + `</string>
			</array>
			<key>CFBundleTypeExtensions</key>
			<array>
				<string>nes</string>
			</array>
		</dict>
	</array>
</dict>
</plist>
`
}
