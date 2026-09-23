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

各 OS 向けの形式は生成してリポジトリへ格納する。ビルド環境にツールを要求しないためである。

| OS | 形式 | 含めるサイズ |
|---|---|---|
| macOS | `icon.icns` | 16, 32, 64, 128, 256, 512, 1024（各 @1x と @2x） |
| Windows | `icon.ico` | 16, 24, 32, 48, 64, 128, 256 |
| Linux | `icon-<size>.png` | 16, 22, 24, 32, 48, 64, 128, 256 |
| 実行時 | `icon.png` | 256 |

生成手順を `tools/gen-icons.sh` に置く。macOS では `iconutil`、Windows 向けの `.ico` は Go のプログラムで生成する。

Windows の `.exe` へのアイコン埋め込みとバージョン情報リソースの生成に `github.com/josephspurrier/goversioninfo` を用いる。

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

`CFBundleDocumentTypes` を設定することで、Finder から `.nes` ファイルを本アプリケーションで開ける。

配布形式を `.dmg` とする。

署名と公証を行わない場合、Gatekeeper が初回起動を妨げる。この場合の起動手順を配布物に添付する文書へ記載する。

## 13.6 Windows の配布

`.zip` で配布する。同梱する内容を次に示す。

| 内容 |
|---|
| `shogun.exe` |
| ライセンス |
| 使い方を記した文書 |

インストーラを作らない。単一の実行ファイルで動作する。

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

## 13.8 CI

```yaml
# .github/workflows/ci.yml の構成
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - テスト ROM の取得
      - go vet
      - go test ./...
      - 静的検査（「12 テスト設計」§12.7）
      - 決定論ハッシュの出力を成果物として保存

  determinism:
    needs: test
    steps:
      - 3 OS の決定論ハッシュを比較する

  build:
    needs: test
    steps:
      - 各 OS でビルドする
      - 成果物を保存する
```

`test` ジョブで実行する内容を次に示す。

| 内容 |
|---|
| 単体テスト |
| テスト ROM の判定 |
| 往復テスト（`-short` で 60 フレーム） |
| ムービー再現テスト |
| 静的検査 |

タグを打ったときに `build` の成果物をリリースへ添付する。

## 13.9 依存モジュール

| モジュール | 用途 |
|---|---|
| `fyne.io/fyne/v2` | GUI |
| `github.com/ebitengine/oto/v3` | 音声出力 |
| `github.com/ncruces/zenity` | ネイティブダイアログ |
| `github.com/josephspurrier/goversioninfo` | Windows のリソース生成。ビルド時のみ |
| `golang.org/x/sys` | Windows のコンソール接続 |
| `golang.org/x/tools` | 静的検査。テスト時のみ |
| `golang.org/x/image` | アイコン生成ツールの字形の取り出しとラスタライズ。`tools/gen-icon` のみ |

バージョンを `go.mod` で固定する。更新は個別に検証してから行う。

静的検査は `go list` と `go/ast` で実装し、外部モジュールを必要としない（「12 テスト設計」§12.7）。

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
tools/
    fetch-test-roms.go
    gen-icons.sh
    spikes/           技術検証コード。独立モジュール
.github/workflows/
go.mod
go.sum
```

`testdata/roms/` を `.gitignore` に含める。
