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
| Windows | amd64, arm64 | `.exe` |
| Linux | amd64, arm64 | 実行ファイルと AppImage |

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
```

埋め込む対象を次に示す。

| 対象 | 変数 | 用途 |
|---|---|---|
| `palettes/default.pal` | `DefaultPalette` | 既定のパレット |
| `icon/icon.png` | `Icon` | 実行時のウィンドウアイコン |

`embed.FS` に 1 つにまとめず、対象ごとに変数を持つ。呼び出し側がパスの文字列を書かずに済み、ファイル名を変えたときに参照漏れがコンパイルで分かる。

`assets` はデータだけを持ち、`embed` 以外を参照しない（「01 全体アーキテクチャ設計」§1.4）。

埋め込みにより、実行時に外部ファイルを必要としない。

## 13.4 アイコン

原本を `assets/icon/icon.svg` と `assets/icon/icon-1024.png` とする。図柄は「将」の 1 文字を白抜きで単色の角丸背景に置いたものとする。

原本は `go run ./tools/gen-icon` で生成する。字形は SIL Open Font License のフォントから輪郭として取り出し、パスとして書き出す。フォントそのものは配布物に含めない。図柄を画像編集ソフトの操作として残さず、生成する手続きとして残すことで、形や色を変えたときに再生成できる。

各 OS 向けの形式も同じプログラムで 1024 px の原本から生成し、`assets/icon/generated/` へ格納してリポジトリに含める。ビルド環境にツールを要求しないためである。`.icns` と `.ico` も Go のプログラムで書く。`iconutil` を使わないのは、macOS 以外のランナーでも生成と検証ができるようにするためである。

| OS | 形式 | 含めるサイズ |
|---|---|---|
| macOS | `generated/icon.icns` | 16・32・128・256・512 の @1x（`icp4`・`icp5`・`ic07`・`ic08`・`ic09`）と @2x（`ic11`・`ic12`・`ic13`・`ic14`・`ic10`）。`iconutil` の iconset と同じ 10 要素 |
| Windows | `generated/icon.ico` | 16, 24, 32, 48, 64, 128, 256（各要素を PNG で格納する） |
| Linux | `generated/linux/icon-<size>.png` | 16, 22, 24, 32, 48, 64, 128, 256, 512 |
| 実行時 | `icon.png` | 256 |

縮小は Catmull-Rom 補間で行う。`go run ./tools/gen-icon -derive` は原本の SVG を作り直さず、`icon-1024.png` から各形式だけを作る。字形を取り出すフォントが無い環境でも生成物を更新できる。生成物が原本から作ったものと一致することをテストで確かめ、原本を変えて再生成し忘れたことを検出する。縮小は浮動小数点で計算し、arm64 では積和の融合により amd64 と下位の桁が違うことがあるため、復号した画素の各成分の差が 2 以下であれば一致とみなす。

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

`CFBundleDocumentTypes` と、`.nes` を `public.data` に準じる種類として宣言する `UTImportedTypeDeclarations` を設定することで、Finder から `.nes` ファイルを本アプリケーションで開ける。

Finder で開いたファイルは起動引数ではなく、「書類を開く」の Apple イベント（`kAEOpenDocuments`）で届く。Fyne はこのイベントを扱わないため、`internal/ui` の macOS 専用のコード（cgo と Objective-C）で受け取る。受け取る処理は `NSApplicationWillFinishLaunchingNotification` の通知の中で `NSAppleEventManager` に登録する。`NSApplication` は起動の完了の前に既定の受け取り口を登録し、この通知の時点で登録したものがそれを上書きするためである。届いたパスは溜めておき、画面の更新のときに UI スレッドで開く。起動中にアプリケーションがすでに動いているときに届いたファイルも同じ経路で開く。

`.app` と `.dmg` は `go run ./tools/package macos` で作る。arm64 と amd64 のバイナリを `lipo` でユニバーサルバイナリにまとめ、`hdiutil` で `.dmg` を作る。`.dmg` には `.app`、`/Applications` へのシンボリックリンク、使い方の文書を入れる。これらの手順は macOS のツールを使うため、macOS で実行する。

配布形式を `.dmg` とする。

macOS の署名と公証、Windows のコード署名は行わない。そのため Gatekeeper と SmartScreen が初回起動を妨げる。起動の手順を配布物に添付する文書へ記載する。リリースの公開先は GitHub Releases とする。

## 13.6 Windows の配布

`.zip` で配布する。同梱する内容を次に示す。

| 内容 |
|---|
| `shogun.exe` |
| ライセンス |
| 使い方を記した文書 |

インストーラを作らない。単一の実行ファイルで動作する。`.zip` は `go run ./tools/package windows` で作る。Go の標準ライブラリで書くため、どの OS でも作れる。

## 13.7 Linux の配布

実行ファイルと AppImage の両方を配布する。

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

AppDir は `AppRun`（`usr/bin/shogun` を指すシンボリックリンク）、`shogun-emulator.desktop`、`shogun-emulator.png`（256 px）、`usr/bin/shogun`、`usr/share/applications/shogun-emulator.desktop`、`usr/share/icons/hicolor/<size>x<size>/apps/shogun-emulator.png`、`usr/share/mime/packages/shogun-emulator.xml` で構成する。

OpenGL・X11・Wayland・PulseAudio・ALSA のライブラリは AppImage に収めない。ホストのドライバと音声サーバに合わせる必要があるためである。実行ファイルが動的に結び付くのはこれらと libc だけであり、収めるライブラリが無いため、ライブラリを集める `linuxdeploy` を使わない。`appimagetool` の場所は環境変数 `APPIMAGETOOL` で渡す。AppImage の生成は Linux で行う。

### 13.7.1 配布物に同梱する文書

| 文書 | 内容 |
|---|---|
| `USAGE.md`（`packaging/usage.md`） | 動作環境、起動の仕方、Gatekeeper と SmartScreen の回避手順、設定とセーブデータの保存先、キーバインドの既定値、テスト ROM の取得方法（開発者向け） |
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
| `windows` | `go generate` でリソースを作ってから amd64 と arm64 をビルドし、それぞれの `.zip` を作る |
| `linux` | amd64 と arm64 のランナーでビルドし、それぞれの `.tar.gz` と AppImage を作る |
| `release` | 成果物の SHA-256 を `SHA256SUMS.txt` にまとめ、リリースノートの雛形とともに GitHub Releases の下書きへ添付する。`-` を含むタグはプレリリースとする |

## 13.9 依存モジュール

| モジュール | 用途 |
|---|---|
| `fyne.io/fyne/v2` | GUI |
| `github.com/ebitengine/oto/v3` | 音声出力 |
| `github.com/ncruces/zenity` | ネイティブダイアログ |
| `github.com/josephspurrier/goversioninfo` | Windows のリソース生成。ビルド時のみ |
| `golang.org/x/sys` | Windows のコンソール接続 |
| `golang.org/x/tools` | 静的検査。テスト時のみ |
| `golang.org/x/image` | アイコン生成ツールの字形の取り出し・ラスタライズ・縮小。`tools/gen-icon` のみ |

バージョンを `go.mod` で固定する。更新は個別に検証してから行う。

静的検査は `go list` と `go/ast` で実装し、外部モジュールを必要としない（「12 テスト設計」§12.7）。そのため `golang.org/x/tools` は依存に現れない。

表のモジュールが間接的に必要とするモジュールは `go.mod` の `indirect` として現れる。`goversioninfo` は `tool` 指令で参照し、実行ファイルには含まれない。

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
testdata/
    golden/           期待値
    roms/             テスト ROM。バージョン管理の対象外
packaging/
    usage.md          配布物に同梱する使い方の文書
    linux/            .desktop と MIME の定義
tools/
    build.sh          1 つのプラットフォーム向けのビルド
    fetch-test-roms/  テスト ROM の取得
    gen-icon/         アイコンの原本と各 OS 向けの形式の生成
    gen-palette/      既定のパレットの生成
    package/          .app・.dmg・.zip・.tar.gz・AppImage・ライセンス一覧の生成
    spikes/           技術検証コード。独立モジュール
.github/workflows/
go.mod
go.sum
```

`testdata/roms/` を `.gitignore` に含める。
