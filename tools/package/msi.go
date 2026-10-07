package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// .msi の固定値（設計書 13 編 §13.6）。
const (
	// msiUpgradeCode は版を通して変えない。変えると、新しい版が古い版を
	// 置き換えられなくなる。
	msiUpgradeCode   = "{14B52A47-548C-4E19-A477-92EAB4B3ABA7}"
	msiManufacturer  = "Takaaki Mizuno"
	msiProductName   = "Shogun Emulator"
	msiHelpLink      = "https://github.com/takaaki-mizuno/shogun-emulator"
	msiCulture       = "ja-JP"
	msiUIExtension   = "WixToolset.UI.wixext"
	icoSource        = "assets/icon/generated/icon.ico"
	wxsFileName      = "shogun.wxs"
	licenseRTFName   = "license.rtf"
	bannerBMPName    = "banner.bmp"
	dialogBMPName    = "dialog.bmp"
	msiIconFileName  = "icon.ico"
	msiExecutableExe = "shogun.exe"
)

// msiArch は Go のアーキテクチャ名を WiX のプラットフォーム名に直す。
var msiArch = map[string]string{"amd64": "x64", "arm64": "arm64"}

// msiName は .msi のファイル名を返す。
func msiName(version, arch string) string {
	return fmt.Sprintf("Shogun_Emulator-%s-windows-%s.msi", version, msiArch[arch])
}

// buildMSI は .msi の材料を out の下にそろえ、wix build で .msi を作る。
func buildMSI(root, version, arch string, exe []byte, docs []docFile, out string) (string, error) {
	platform, ok := msiArch[arch]
	if !ok {
		return "", fmt.Errorf("知らないアーキテクチャ %q", arch)
	}
	wix, err := findWix()
	if err != nil {
		return "", err
	}
	stage, err := stageMSI(root, version, exe, docs, filepath.Join(out, "msi-"+arch))
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(out, msiName(version, arch)))
	if err != nil {
		return "", err
	}
	cmd := exec.Command(wix, "build", "-arch", platform, "-culture", msiCulture,
		"-ext", msiUIExtension, "-o", path, wxsFileName)
	cmd.Dir = stage
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("wix build: %w", err)
	}
	return path, nil
}

// findWix は wix の場所を返す。環境変数 WIX を優先し、無いときは PATH から探す。
func findWix() (string, error) {
	if p := os.Getenv("WIX"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("wix")
	if err != nil {
		return "", errors.New("wix が見つからない。WiX Toolset v5 を入れるか、環境変数 WIX に場所を入れる（-msi=false で .msi を作らない）")
	}
	return p, nil
}

// stageMSI は .wxs と、それが参照するファイルを dir に書く。
func stageMSI(root, version string, exe []byte, docs []docFile, dir string) (string, error) {
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	ico, err := os.ReadFile(filepath.Join(root, icoSource))
	if err != nil {
		return "", err
	}
	logo, err := readLogo(root)
	if err != nil {
		return "", err
	}
	license, err := os.ReadFile(filepath.Join(root, licenseFileName))
	if err != nil {
		return "", err
	}
	wxs, err := wxsSource(versionNumber(version), docs)
	if err != nil {
		return "", err
	}
	es := []entry{
		{name: wxsFileName, data: []byte(wxs), mode: 0o644},
		{name: msiExecutableExe, data: exe, mode: 0o755},
		{name: msiIconFileName, data: ico, mode: 0o644},
		{name: licenseRTFName, data: []byte(licenseRTF(string(license))), mode: 0o644},
		{name: bannerBMPName, data: wixBanner(logo), mode: 0o644},
		{name: dialogBMPName, data: wixDialog(logo), mode: 0o644},
	}
	es = append(es, docEntries("", docs)...)
	return dir, writeTree(dir, es)
}

// wxsTemplate は .msi の定義（WiX v5 の書式）。
var wxsTemplate = template.Must(template.New("wxs").Parse(`<?xml version="1.0" encoding="utf-8"?>
<Wix xmlns="http://wixtoolset.org/schemas/v4/wxs" xmlns:ui="http://wixtoolset.org/schemas/v4/wxs/ui">
  <Package Name="{{.Name}}" Manufacturer="{{.Manufacturer}}" Version="{{.Version}}"
           UpgradeCode="{{.UpgradeCode}}" Language="1041" Codepage="932" Scope="perMachine">
    <MajorUpgrade DowngradeErrorMessage="より新しい版の {{.Name}} がすでにインストールされています。" />
    <MediaTemplate EmbedCab="yes" />

    <Icon Id="ShogunIcon" SourceFile="{{.Icon}}" />
    <Property Id="ARPPRODUCTICON" Value="ShogunIcon" />
    <Property Id="ARPHELPLINK" Value="{{.HelpLink}}" />

    <StandardDirectory Id="ProgramFiles64Folder">
      <Directory Id="INSTALLFOLDER" Name="{{.Name}}">
        <Component Id="MainExecutable">
          <File Id="ShogunExe" Source="{{.Exe}}" KeyPath="yes" />
          <ProgId Id="ShogunEmulator.nes" Description="NES ROM" Icon="ShogunExe" IconIndex="0">
            <Extension Id="nes" ContentType="application/x-nes-rom">
              <Verb Id="open" TargetFile="ShogunExe" Argument="&quot;%1&quot;" />
            </Extension>
          </ProgId>
        </Component>
        <Component Id="Documents">
{{- range $i, $d := .Docs}}
          <File Id="Doc{{$i}}" Source="{{$d}}"{{if eq $i 0}} KeyPath="yes"{{end}} />
{{- end}}
        </Component>
      </Directory>
    </StandardDirectory>

    <StandardDirectory Id="ProgramMenuFolder">
      <Component Id="StartMenuShortcut">
        <Shortcut Id="ShogunShortcut" Name="{{.Name}}" Target="[#ShogunExe]" WorkingDirectory="INSTALLFOLDER" />
        <RegistryValue Root="HKMU" Key="Software\{{.Name}}" Name="StartMenuShortcut" Type="integer" Value="1" KeyPath="yes" />
      </Component>
    </StandardDirectory>

    <Feature Id="Main" Title="{{.Name}}" Level="1">
      <ComponentRef Id="MainExecutable" />
      <ComponentRef Id="Documents" />
      <ComponentRef Id="StartMenuShortcut" />
    </Feature>

    <ui:WixUI Id="WixUI_InstallDir" InstallDirectory="INSTALLFOLDER" />
    <WixVariable Id="WixUILicenseRtf" Value="{{.LicenseRTF}}" />
    <WixVariable Id="WixUIBannerBmp" Value="{{.Banner}}" />
    <WixVariable Id="WixUIDialogBmp" Value="{{.Dialog}}" />
  </Package>
</Wix>
`))

// wxsSource は版と同梱する文書から .wxs を作る。
func wxsSource(version string, docs []docFile) (string, error) {
	var names []string
	for _, d := range docs {
		names = append(names, d.name)
	}
	var b bytes.Buffer
	err := wxsTemplate.Execute(&b, map[string]any{
		"Name":         msiProductName,
		"Manufacturer": msiManufacturer,
		"Version":      version,
		"UpgradeCode":  msiUpgradeCode,
		"HelpLink":     msiHelpLink,
		"Icon":         msiIconFileName,
		"Exe":          msiExecutableExe,
		"Docs":         names,
		"LicenseRTF":   licenseRTFName,
		"Banner":       bannerBMPName,
		"Dialog":       dialogBMPName,
	})
	return b.String(), err
}

// licenseRTF はライセンスの文面を、WiX のライセンスの画面に出す RTF にする。
//
// 等幅のフォントで 1 行ずつ段落にする。RTF の特殊文字（\ { }）は逃がし、
// ASCII 以外の文字は \uN? の形にする。
func licenseRTF(text string) string {
	var b strings.Builder
	b.WriteString(`{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0\fmodern Consolas;}}\f0\fs16` + "\n")
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		for _, r := range line {
			switch {
			case r == '\\' || r == '{' || r == '}':
				b.WriteByte('\\')
				b.WriteRune(r)
			case r < 0x80:
				b.WriteRune(r)
			default:
				fmt.Fprintf(&b, `\u%d?`, int16(r))
			}
		}
		b.WriteString("\\par\n")
	}
	b.WriteString("}\n")
	return b.String()
}
