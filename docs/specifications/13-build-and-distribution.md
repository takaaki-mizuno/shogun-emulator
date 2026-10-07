# 13 ビルドと配布設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 13.1 ビルド方式

各 OS のネイティブ環境でビルドする。クロスコンパイルに依存しない。Fyne が OpenGL のバインディングで cgo を用いるためである。

CI で macOS・Windows・Linux のランナーを使い、それぞれでビルドする。

| OS | アーキテクチャ | 成果物 |
|---|---|---|
| macOS | arm64, amd64 | `lipo` で 1 つのユニバーサルバイナリにまとめ、`.app` バンドルへ収める |
| Windows | amd64, arm64 | `.exe` を `.msi` と `.zip` に収める |
| Linux | amd64, arm64 | 実行ファイルを `.deb`・`.rpm`・AppImage・`.tar.gz` に収める |

## 13.2 ビルドフラグ

```
go build -trimpath \
  -ldflags "-s -w \
    -X main.version=$(git describe --tags --always) \
    -X main.commit=$(git rev-parse --short HEAD) \
    -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o shogun ./cmd/shogun
```

| フラグ | 目的 |
|---|---|
| `-trimpath` | ビルド環境のパスをバイナリに含めない |
| `-s -w` | シンボルテーブルと DWARF を除く |
| `-X` | バージョン情報を埋め込む |

Windows では `-ldflags "-H windowsgui"` を追加する。コンソールウィンドウが表示されない。コマンドラインからの起動時は「11 設定と CLI 設計」§11.5.3 の処理でコンソールへ接続する。

デバッグビルドでは `-s -w` を付けない。

`tools/build.sh` がこれらのフラグでビルドし、`dist/<GOOS>_<GOARCH>/` へ出力する。版・コミット・ビルド日時は環境変数 `SHOGUN_VERSION`・`SHOGUN_COMMIT`・`SHOGUN_BUILD_DATE` を優先し、無いときに `git describe` と `git rev-parse` から求める。版とコミットの両方を環境変数で渡したときは git を呼ばない。ソースの書庫からのビルドなど、git の無い環境でもビルドが成り立つ。Windows 向けのときは、版から `1.2.3.0` の形の値を作って `go generate ./cmd/shogun` を実行し、リソースを作ってからビルドする（§13.4）。

## 13.3 アセットの埋め込み

```go
package assets

import _ "embed"

//go:embed palettes/default.pal
var DefaultPalette []byte

//go:embed icon/icon.png
var Icon []byte

//go:embed icon/icon-macos.png
var IconMacOS []byte

//go:embed icon/logo.png
var Logo []byte
```

埋め込む対象を次に示す。

| 対象 | 変数 | 用途 |
|---|---|---|
| `palettes/default.pal` | `DefaultPalette` | 既定のパレット |
| `icon/icon.png` | `Icon` | 実行時のウィンドウアイコン（Windows・Linux） |
| `icon/icon-macos.png` | `IconMacOS` | 実行時のアプリケーションアイコン（macOS。`.app` に入れずに起動したときの Dock の表示） |
| `icon/logo.png` | `Logo` | ROM を開いていないときの画面と「バージョン情報」のロゴ（「10 GUI 設計」§10.4・§10.5） |

`embed.FS` に 1 つにまとめず、対象ごとに変数を持つ。呼び出し側がパスの文字列を書かずに済み、ファイル名を変えたときに参照漏れがコンパイルで分かる。

`assets` はデータだけを持ち、`embed` 以外を参照しない（「01 全体アーキテクチャ設計」§1.4）。

埋め込みにより、実行時に外部ファイルを必要としない。

## 13.4 アイコン

原本を `assets/icon.png`（正方形の原画）とする。原本はこの 1 枚だけであり、各 OS 向けの形式とロゴはすべてここから生成する。

`go run ./tools/gen-icon` が原本から次を作る。生成物はリポジトリに含める。ビルド環境にツールを要求しないためである。`.icns` と `.ico` も Go のプログラムで書く。`iconutil` を使わないのは、macOS 以外のランナーでも生成と検証ができるようにするためである。

| 用途 | ファイル | 形 | 含めるサイズ |
|---|---|---|---|
| macOS の `.app` | `assets/icon/generated/icon.icns` | タイル | 16・32・128・256・512 の @1x（`icp4`・`icp5`・`ic07`・`ic08`・`ic09`）と @2x（`ic11`・`ic12`・`ic13`・`ic14`・`ic10`）。`iconutil` の iconset と同じ 10 要素 |
| Windows | `assets/icon/generated/icon.ico` | 正方形 | 16, 24, 32, 48, 64, 128, 256（各要素を PNG で格納する） |
| Linux | `assets/icon/generated/linux/icon-<size>.png` | 正方形 | 16, 22, 24, 32, 48, 64, 128, 256, 512 |
| 実行時（Windows・Linux） | `assets/icon/icon.png` | 正方形 | 256 |
| 実行時（macOS） | `assets/icon/icon-macos.png` | タイル | 512 |
| ロゴ | `assets/icon/logo.png` | 正方形 | 512 |

「正方形」は原本をそのまま縮小した形である。「タイル」は 1024 px の枠の中央に、一辺 824 px・角の半径 185 px の角丸の正方形として原本を置き、外側を透明にした形である。macOS 11 以降のアイコンの外形と余白に合わせる。Windows と Linux には決まった外形が無く、余白を取ると小さい大きさで図柄がつぶれるため、正方形とする。

縮小は Catmull-Rom 補間で行う。タイルの角の透明度は、画素を 4×4 に分けた点のうち角丸の内側にある割合とする。生成物が原本から作ったものと一致することをテストで確かめ、原本を変えて再生成し忘れたことを検出する。縮小は浮動小数点で計算し、arm64 では積和の融合により amd64 と下位の桁が違うことがあるため、復号した画素の各成分の差が 2 以下であれば一致とみなす。

Windows の `.exe` へのアイコン埋め込みとバージョン情報リソースの生成に `github.com/josephspurrier/goversioninfo` を用いる。`go.mod` の `tool` 指令で版を固定し、`cmd/shogun/versioninfo.json` を入力として `go generate ./cmd/shogun` で `resource_windows_<arch>.syso` を作る。`.syso` はビルドのたびに作り、リポジトリに含めない。Windows 以外のビルドでは名前の `_windows_` により無視される。

## 13.5 macOS の `.app` バンドル

```
Shogun Emulator.app/
  Contents/
    Info.plist
    MacOS/
      shogun
    Resources/
      icon.icns
```

`Info.plist` に含める項目を次に示す。

| キー | 値 |
|---|---|
| `CFBundleIdentifier` | `com.shogunemulator.app` |
| `CFBundleName` | `Shogun Emulator` |
| `CFBundleShortVersionString` | バージョン |
| `CFBundleVersion` | ビルド番号 |
| `CFBundleIconFile` | `icon` |
| `CFBundleDocumentTypes` | `.nes` 拡張子と `public.data` |
| `LSMinimumSystemVersion` | `11.0` |
| `NSHighResolutionCapable` | `true` |

バンドルに入れずに実行ファイルを直接起動したときもメニューバーの名前が `Shogun Emulator` になるよう、`CFBundleName` などを書いた `internal/ui/info_darwin.plist` をリンク時に実行ファイルの `__TEXT,__info_plist` セクションへ埋め込む（cgo の `-Wl,-sectcreate`）。

アプリケーションメニュー（「Shogun Emulatorについて」「Shogun Emulatorを終了」など）とウインドウメニューの項目は GLFW が英語の固定の文言で作る。起動後に UI スレッドで、項目の action を手がかりに日本語の文言へ差し替える。

`CFBundleDocumentTypes` と、`.nes` を `public.data` に準じる種類として宣言する `UTImportedTypeDeclarations` を設定することで、Finder から `.nes` ファイルを本アプリケーションで開ける。

Finder で開いたファイルは起動引数ではなく、「書類を開く」の Apple イベント（`kAEOpenDocuments`）で届く。Fyne はこのイベントを扱わないため、`internal/ui` の macOS 専用のコード（cgo と Objective-C）で受け取る。受け取る処理は `NSApplicationWillFinishLaunchingNotification` の通知の中で `NSAppleEventManager` に登録する。`NSApplication` は起動の完了の前に既定の受け取り口を登録し、この通知の時点で登録したものがそれを上書きするためである。届いたパスは溜めておき、画面の更新のときに UI スレッドで開く。起動中にアプリケーションがすでに動いているときに届いたファイルも同じ経路で開く。

`.app` と `.dmg` は `go run ./tools/package macos` で作る。arm64 と amd64 のバイナリを `lipo` でユニバーサルバイナリにまとめ、`hdiutil` で `.dmg` を作る。これらの手順は macOS のツールを使うため、macOS で実行する。

配布形式を `.dmg` とする。`.dmg` の中身を次に示す。

| 中身 | 内容 |
|---|---|
| `Shogun Emulator.app` | アプリケーション本体 |
| `Applications` | `/Applications` へのシンボリックリンク。`.app` をここへドラッグしてインストールする |
| `USAGE.md`・`LICENSE`・`THIRD_PARTY_LICENSES.txt` | 同梱する文書（§13.7.1） |
| `.background/background.tiff` | ウィンドウの背景画像。1x（640×480）と 2x（1280×960）を `tiffutil -cathidpicheck` で 1 つにまとめる |
| `.VolumeIcon.icns` | ボリュームのアイコン。`.app` と同じ `icon.icns` を使う |

背景画像は `tools/package` が描く。明るい背景に、`.app` の位置から `Applications` の位置へ向かう矢印を置く。背景を明るくするのは、Finder がアイコンの名前を黒い文字で描くためである。

`.dmg` を開いたときのウィンドウの見た目とボリュームのアイコンは、次の手順で設定する。

1. 中身を読み書きできる形式（`UDRW`）の `.dmg` にする
2. マウント先を指定せずに `hdiutil attach` でマウントし、出力（`-plist`）からマウント先を読む。Finder は `/Volumes` の下のボリュームだけを AppleScript の `disk` として扱う
3. Finder の AppleScript でアイコン表示・ツールバーとステータスバーの非表示・ウィンドウの大きさ（640×480）・アイコンの大きさ（96）・背景画像・各項目の位置を設定する
4. `.VolumeIcon.icns` を置き、ボリュームのルートの Finder 情報（拡張属性 `com.apple.FinderInfo`）にカスタムアイコンの印を立てる。`.VolumeIcon.icns` を Finder の操作の前に置くと、操作の間に取り除かれる。拡張属性はシステムコールで書く
5. マウントを外し、圧縮した形式（`UDZO`）へ変換する

| 項目 | 位置（ウィンドウ内の座標） |
|---|---|
| `Shogun Emulator.app` | (160, 180) |
| `Applications` | (480, 180) |
| `USAGE.md`・`LICENSE`・`THIRD_PARTY_LICENSES.txt` | (160, 370)・(320, 370)・(480, 370) |

AppleScript が失敗したときは警告を表示し、見た目の設定の無い `.dmg` を作る。Finder の自動操作は、画面の無い環境や自動操作の許可が無い環境で失敗する。見た目は使い方に影響しないため、配布物を作ることを優先する。`-layout=false` で見た目の設定を飛ばす。

macOS の署名と公証、Windows のコード署名は行わない。そのため Gatekeeper と SmartScreen が初回起動を妨げる。起動の手順を配布物に添付する文書へ記載する。リリースの公開先は GitHub Releases とする。

## 13.6 Windows の配布

インストーラ（`.msi`）と `.zip` を配布する。`.zip` はインストールせずに使う場合（ポータブルモード、「11 設定と CLI 設計」§11.2.1）のためにある。両方とも `go run ./tools/package windows` で作る。

| 成果物 | 内容 |
|---|---|
| `Shogun_Emulator-<版>-windows-<x64 または arm64>.msi` | インストーラ |
| `shogun-emulator-<版>-windows-<amd64 または arm64>.zip` | `shogun.exe`、ライセンス、使い方を記した文書 |

`.msi` は WiX Toolset v5 で作る。`tools/package` が版とアーキテクチャから `.wxs` を作り、`wix build -arch <x64 または arm64> -culture ja-JP -ext WixToolset.UI.wixext` を実行する。WiX v6 以降を使わないのは、利用条件に保守費用（Open Source Maintenance Fee）の規定が加わったためである。`wix` の場所は環境変数 `WIX` で渡し、無いときは `PATH` から探す。`wix` は Windows でだけ動くため、`.msi` は Windows で作る。`.zip` は Go の標準ライブラリで書くため、どの OS でも作れる。`-msi=false` で `.msi` を作らない。

`.msi` の内容を次に示す。

| 項目 | 内容 |
|---|---|
| 製品名・製造元 | `Shogun Emulator`・`Takaaki Mizuno` |
| `UpgradeCode` | `{14B52A47-548C-4E19-A477-92EAB4B3ABA7}`（固定）。`MajorUpgrade` により、新しい版を入れると古い版を置き換える。古い版を新しい版の上に入れることは断る |
| `ProductVersion` | 版の数字部分（`0.9.0` など） |
| インストールの範囲 | マシン全体（`Scope="perMachine"`）。管理者の権限を求める |
| インストール先 | `Program Files\Shogun Emulator`（変更できる） |
| ファイル | `shogun.exe`、`USAGE.md`、`LICENSE`、`THIRD_PARTY_LICENSES.txt` |
| スタートメニュー | `Shogun Emulator` のショートカット |
| 関連付け | `.nes` を `ShogunEmulator.nes`（MIME タイプ `application/x-nes-rom`）として登録し、開く動作で `shogun.exe "%1"` を起動する |
| 「アプリと機能」の表示 | アイコンを `icon.ico` とする |
| 画面 | `WixUI_InstallDir`（ようこそ、ライセンス、インストール先、確認、進行、完了）。言語は日本語 |
| ライセンスの画面 | `LICENSE` から作った RTF |
| 画像 | 上部の帯（493×58）とようこそ・完了の画面の左側（493×312）を BMP で作る。ロゴを置く |

RTF と BMP は `tools/package` が作る。BMP は `golang.org/x/image/bmp` で書く。

## 13.7 Linux の配布

`.deb`・`.rpm`・AppImage・`.tar.gz` を配布する。`.deb` と `.rpm` はパッケージ管理（`apt`・`dnf` など）で入れる場合、AppImage は 1 ファイルで動かす場合、`.tar.gz` は手で置く場合のためにある。

`.desktop` ファイルの内容を次に示す。

```
[Desktop Entry]
Type=Application
Name=Shogun Emulator
Exec=shogun %f
Icon=shogun-emulator
Categories=Game;Emulator;
MimeType=application/x-nes-rom;
Terminal=false
```

実行時に必要なライブラリを次に示す。

| ライブラリ | 用途 |
|---|---|
| OpenGL | Fyne の描画 |
| X11 または Wayland | Fyne のウィンドウ |
| PulseAudio または `libasound.so.2` | oto の音声出力 |

これらを配布物に添付する文書へ記載する。

MIME タイプ `application/x-nes-rom` の定義（拡張子 `.nes`）を `packaging/linux/shogun-emulator.xml` に置く。

`go run ./tools/package linux` が次を作る。

| 成果物 | 内容 |
|---|---|
| `.tar.gz` | 実行ファイル、`.desktop`、MIME の定義、各サイズのアイコン、ライセンス、使い方の文書 |
| AppImage | AppDir を組み立て、`appimagetool` で 1 ファイルにする |
| `.deb`・`.rpm` | nfpm で作る。`/usr` の下に置くファイルは下の表のとおり |

`.deb` と `.rpm` は nfpm で作る。`tools/package` が nfpm の設定（YAML）を作り、`go tool nfpm package -p deb`・`-p rpm` を実行する。nfpm の版は `go.mod` の `tool` 指令で固定する。どの OS でも作れる。`-packages=false` で作らない。

| 置き場所 | 内容 |
|---|---|
| `/usr/bin/shogun` | 実行ファイル |
| `/usr/share/applications/shogun-emulator.desktop` | `.desktop` |
| `/usr/share/mime/packages/shogun-emulator.xml` | MIME の定義 |
| `/usr/share/icons/hicolor/<size>x<size>/apps/shogun-emulator.png` | 各サイズのアイコン |
| `/usr/share/doc/shogun-emulator/` | `USAGE.md`、`LICENSE`、`THIRD_PARTY_LICENSES.txt`。`.deb` では `LICENSE` を `copyright` の名前でも置く |

| 項目 | `.deb` | `.rpm` |
|---|---|---|
| パッケージ名 | `shogun-emulator` | `shogun-emulator` |
| 依存 | `libgl1`, `libx11-6`, `libxcursor1`, `libxi6`, `libxinerama1`, `libxrandr2`, `libxxf86vm1`, `libasound2 \| libasound2t64` | `mesa-libGL`, `libX11`, `libXcursor`, `libXi`, `libXinerama`, `libXrandr`, `libXxf86vm`, `alsa-lib` |
| 区分 | `games` | `Amusements/Games` |
| ファイル名 | `shogun-emulator_<版>-1_<amd64 または arm64>.deb` | `shogun-emulator-<版>-1.<x86_64 または aarch64>.rpm` |

MIME・`.desktop`・アイコンのキャッシュの更新は、パッケージにスクリプトを持たせない。Debian 系と Fedora 系のパッケージ管理が、これらのディレクトリへの追加を検知して更新する仕組み（トリガ）を持つためである。

AppDir は `AppRun`（`usr/bin/shogun` を指すシンボリックリンク）、`shogun-emulator.desktop`、`shogun-emulator.png`（256 px）、`usr/bin/shogun`、`usr/share/applications/shogun-emulator.desktop`、`usr/share/icons/hicolor/<size>x<size>/apps/shogun-emulator.png`、`usr/share/mime/packages/shogun-emulator.xml` で構成する。

OpenGL・X11・Wayland・PulseAudio・ALSA のライブラリは AppImage に収めない。ホストのドライバと音声サーバに合わせる必要があるためである。実行ファイルが動的に結び付くのはこれらと libc だけであり、収めるライブラリが無いため、ライブラリを集める `linuxdeploy` を使わない。`appimagetool` の場所は環境変数 `APPIMAGETOOL` で渡す。AppImage の生成は Linux で行う。

### 13.7.1 配布物に同梱する文書

| 文書 | 内容 |
|---|---|
| `USAGE.md`（`packaging/usage.md`） | 動作環境、起動の仕方、Gatekeeper と SmartScreen の回避手順、設定とセーブデータの保存先、キーバインドの既定値、AI から使う手順（Claude Code への登録、GUI との共有、安全のための注意）、テスト ROM の取得方法（開発者向け） |
| `LICENSE` | 本体のライセンス |
| `THIRD_PARTY_LICENSES.txt` | 実行ファイルに含まれる依存モジュールのライセンス。`go run ./tools/package licenses` が `go list -deps` の結果から各モジュールのライセンスファイルを集めて作る |

## 13.8 CI

`.github/workflows/ci.yml` はプッシュとプルリクエストで動く。

| ジョブ | 内容 |
|---|---|
| `test` | 3 OS で、テスト ROM の取得、gofmt、`go vet`、`go build`、`go test -short ./...`、`go test -race`（`internal/testrom` を除く）。決定論ハッシュ（`TestDeterminismHashes` の出力）をファイルへ保存し、成果物として上げる |
| `determinism` | `test` の後に、3 OS の決定論ハッシュを比べる。違えば失敗し、どの OS のどのムービーが違ったかを表示する |
| `build` | `test` の後に、3 OS で `tools/build.sh` を実行し、成果物を保存する |

`test` ジョブで実行する内容を次に示す。

| 内容 |
|---|
| 単体テスト |
| テスト ROM の判定 |
| 往復テスト（`-short` で 60 フレーム） |
| ムービー再現テスト |
| 静的検査 |

`.github/workflows/release.yml` は `v` で始まるタグで動く。

| ジョブ | 内容 |
|---|---|
| `test` | 3 OS でテストを実行する |
| `macos` | arm64 と amd64 をビルドし、`.app` と `.dmg` を作る |
| `windows` | `go generate` でリソースを作ってから amd64 と arm64 をビルドし、それぞれの `.msi` と `.zip` を作る。WiX v5 は `dotnet tool install` で入れる |
| `linux` | amd64 と arm64 のランナーでビルドし、それぞれの `.deb`・`.rpm`・AppImage・`.tar.gz` を作る |
| `release` | 成果物の SHA-256 を `SHA256SUMS.txt` にまとめ、リリースノートとともに GitHub Releases の下書きへ添付する。`-` を含むタグはプレリリースとする |

リリースノートは `docs/releases/<タグ>.md` に置く。`release` ジョブはこのファイルの後ろにダウンロードの表と初回起動の注意を足して下書きの本文にする。ファイルが無いときは変更点の欄を空けた雛形を使う。

## 13.9 依存モジュール

| モジュール | 用途 |
|---|---|
| `fyne.io/fyne/v2` | GUI |
| `github.com/ebitengine/oto/v3` | 音声出力 |
| `github.com/ncruces/zenity` | ネイティブダイアログ |
| `github.com/josephspurrier/goversioninfo` | Windows のリソース生成。ビルド時のみ |
| `golang.org/x/sys` | Windows のコンソール接続 |
| `golang.org/x/tools` | 静的検査。テスト時のみ |
| `golang.org/x/image` | アイコンの縮小（`tools/gen-icon`）とインストーラの BMP の書き出し（`tools/package`） |
| `github.com/goreleaser/nfpm/v2` | `.deb` と `.rpm` の生成。`tool` 指令で参照し、実行ファイルには含まれない |

バージョンを `go.mod` で固定する。更新は個別に検証してから行う。

静的検査は `go list` と `go/ast` で実装し、外部モジュールを必要としない（「12 テスト設計」§12.7）。そのため `golang.org/x/tools` は依存に現れない。

表のモジュールが間接的に必要とするモジュールは `go.mod` の `indirect` として現れる。`goversioninfo` と nfpm は `tool` 指令で参照し、実行ファイルには含まれない。

Go のモジュールではない外部ツールを次に示す。

| ツール | 用途 | 入れ方 |
|---|---|---|
| WiX Toolset v5 と `WixToolset.UI.wixext` | `.msi` の生成 | `dotnet tool install --global wix --version 5.0.2` と `wix extension add --global WixToolset.UI.wixext/5.0.2` |
| `appimagetool` | AppImage の生成 | リリースの配布物を取得する |

`tools/spikes` は独立したモジュールとし、本体の依存に含めない。

## 13.10 リポジトリの構成

```
cmd/shogun/
internal/
assets/
docs/
    research/         調査結果
    specifications/   設計書
    plans/            開発計画
    releases/         リリースノート
testdata/
    golden/           期待値
    roms/             テスト ROM。バージョン管理の対象外
packaging/
    usage.md          配布物に同梱する使い方の文書
    linux/            .desktop と MIME の定義
tools/
    build.sh          1 つのプラットフォーム向けのビルド
    fetch-test-roms/  テスト ROM の取得
    gen-icon/         原画から各 OS 向けの形式とロゴを生成
    gen-palette/      既定のパレットの生成
    package/          .app・.dmg・.msi・.zip・.deb・.rpm・.tar.gz・AppImage・ライセンス一覧の生成
    spikes/           技術検証コード。独立モジュール
.github/workflows/
go.mod
go.sum
```

`testdata/roms/` を `.gitignore` に含める。
