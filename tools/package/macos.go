package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// macOS の配布物の名前（設計書 13 編 §13.5）。
const (
	appBundleName  = "Shogun Emulator.app"
	bundleID       = "com.shogunemulator.app"
	icnsSource     = "assets/icon/generated/icon.icns"
	minimumMacOS   = "11.0"
	nesDocumentUTI = "com.shogunemulator.nes-rom"
	dmgVolumeName  = "Shogun Emulator"
)

// .dmg のウィンドウの見た目（設計書 13 編 §13.5）。座標はウィンドウ内の
// 位置で、アイコンの中心を指す。
const (
	dmgWindowWidth   = 640
	dmgWindowHeight  = 480
	dmgIconSize      = 96
	dmgIconY         = 180
	dmgAppX          = 160
	dmgApplicationsX = 480
	dmgDocsY         = 370
	// dmgArrowGap は矢印の端からアイコンの中心までの距離。
	dmgArrowGap = 80
	// dmgLayoutTimeout は Finder の AppleScript を待つ時間。
	dmgLayoutTimeout = 2 * time.Minute
)

// packageMacOS は arm64 と amd64 の実行ファイルからユニバーサルバイナリを作り、
// .app と .dmg を作る。片方だけ渡したときはそれを使う。
//
// layout が true のとき、背景画像と配置を Finder の AppleScript で設定する。
// 設定に失敗したときは警告を出し、見た目の設定の無い .dmg を作る。
func packageMacOS(root, version, arm64, amd64, out string, layout bool) (string, error) {
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
	if err := writeDMGBackground(filepath.Join(staging, ".background"), out); err != nil {
		return "", err
	}
	if err := os.Symlink("/Applications", filepath.Join(staging, "Applications")); err != nil {
		return "", err
	}

	dmg := filepath.Join(out, fmt.Sprintf("Shogun_Emulator-%s-macos.dmg", version))
	rw := filepath.Join(out, "shogun-rw.dmg")
	os.Remove(dmg)
	os.Remove(rw)
	if err := runTool("hdiutil", "create", "-volname", dmgVolumeName, "-srcfolder", staging,
		"-fs", "HFS+", "-format", "UDRW", "-ov", rw); err != nil {
		return "", err
	}
	defer os.Remove(rw)
	if err := decorateDMG(rw, icns, docs, layout); err != nil {
		return "", err
	}
	err = runTool("hdiutil", "convert", rw, "-format", "UDZO", "-imagekey", "zlib-level=9", "-o", dmg)
	return dmg, err
}

// writeDMGBackground は背景画像を 1x と 2x で描き、tiffutil で 1 つの TIFF に
// まとめて dir/background.tiff に置く。
func writeDMGBackground(dir, work string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var pngs []string
	for _, scale := range []int{1, 2} {
		data, err := encodePNG(dmgBackground(scale))
		if err != nil {
			return err
		}
		p := filepath.Join(work, fmt.Sprintf("background-%dx.png", scale))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
		defer os.Remove(p)
		pngs = append(pngs, p)
	}
	return runTool("tiffutil", "-cathidpicheck", pngs[0], pngs[1], "-out", filepath.Join(dir, "background.tiff"))
}

// decorateDMG は読み書きできる .dmg をマウントし、layout のときは Finder で
// ウィンドウの見た目を設定し、ボリュームのアイコンを置く。
func decorateDMG(rw string, icns []byte, docs []docFile, layout bool) error {
	// Finder は /Volumes の下にマウントしたボリュームだけを disk として扱うため、
	// マウント先を指定せずに付け、hdiutil の出力からマウント先を読む。
	cmd := exec.Command("hdiutil", "attach", rw, "-readwrite", "-noverify", "-noautoopen", "-plist")
	cmd.Stderr = os.Stderr
	plist, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("hdiutil attach: %w", err)
	}
	mnt := mountPoint(string(plist))
	if mnt == "" {
		return errors.New("hdiutil attach の出力にマウント先が無い")
	}
	detached := false
	defer func() {
		if !detached {
			runTool("hdiutil", "detach", mnt, "-force")
		}
	}()

	if layout {
		if err := runAppleScript(dmgLayoutScript(docs)); err != nil {
			fmt.Fprintf(os.Stderr, "警告: .dmg のウィンドウの見た目を設定できなかった。設定の無い .dmg を作る: %v\n", err)
		}
	}
	// ボリュームのアイコンは Finder の操作の後に置く。先に置くと、Finder が
	// ウィンドウを開いている間に取り除かれる。
	if err := os.WriteFile(filepath.Join(mnt, ".VolumeIcon.icns"), icns, 0o644); err != nil {
		return err
	}
	if err := setCustomIconFlag(mnt); err != nil {
		return err
	}
	runTool("sync")
	if err := runTool("hdiutil", "detach", mnt); err != nil {
		return err
	}
	detached = true
	return nil
}

// mountPointPattern は hdiutil attach -plist の出力からマウント先を取り出す。
var mountPointPattern = regexp.MustCompile(`<key>mount-point</key>\s*<string>([^<]+)</string>`)

// mountPoint は hdiutil attach -plist の出力からマウント先を返す。無いときは空。
func mountPoint(plist string) string {
	m := mountPointPattern.FindStringSubmatch(plist)
	if m == nil {
		return ""
	}
	return m[1]
}

// dmgLayoutScript は .dmg のウィンドウの見た目を設定する AppleScript を作る。
func dmgLayoutScript(docs []docFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, `tell application "Finder"
	tell disk %q
		open
		set current view of container window to icon view
		set toolbar visible of container window to false
		set statusbar visible of container window to false
		set the bounds of container window to {120, 120, %d, %d}
		set viewOptions to the icon view options of container window
		set arrangement of viewOptions to not arranged
		set icon size of viewOptions to %d
		set text size of viewOptions to 12
		set background picture of viewOptions to file ".background:background.tiff"
		set position of item %q of container window to {%d, %d}
		set position of item "Applications" of container window to {%d, %d}
`, dmgVolumeName, 120+dmgWindowWidth, 120+dmgWindowHeight, dmgIconSize,
		appBundleName, dmgAppX, dmgIconY, dmgApplicationsX, dmgIconY)
	for i, d := range docs {
		x := dmgAppX + i*(dmgApplicationsX-dmgAppX)/max(len(docs)-1, 1)
		fmt.Fprintf(&b, "\t\tset position of item %q of container window to {%d, %d}\n", d.name, x, dmgDocsY)
	}
	b.WriteString(`		close
		open
		update without registering applications
		delay 2
		close
	end tell
end tell
`)
	return b.String()
}

// runAppleScript は AppleScript を実行する。Finder が応答しないときに備えて
// 時間を区切る。
func runAppleScript(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dmgLayoutTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
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
