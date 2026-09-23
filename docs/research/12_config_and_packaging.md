# 設定ファイル・CLI・配布パッケージングの調査

> 調査日: 2026-09-21 / Go 1.25.3（macOS arm64）で実測確認

## 1. 背景・目的

project.md の要求:

| ID | 要求 |
|---|---|
| C1 | 設定などは、**各 OS で最も適切な場所に JSON の設定ファイルとして保存する** |
| C2 | **コマンドラインでも起動が可能。その際には設定を各種引数で更新できる** |
| C3 | **シングルバイナリとして配布でき、macOS / Windows / Linux で動作する** |
| C4 | **アイコンは後で付けるので、いったんプレースホルダで** |
| C5 | キーバインディングを設定ファイルとして保存する |
| C6 | デバッグログを Config で指定したフォルダに保存する |

## 2. 設定ファイルの場所（C1）

### 2.1 Go 標準ライブラリの挙動

`os.UserConfigDir()` と `os.UserCacheDir()` の返す値（Go 1.25.3 のドキュメントと実測）:

| OS | `os.UserConfigDir()` | `os.UserCacheDir()` |
|---|---|---|
| macOS (darwin) | `$HOME/Library/Application Support` | `$HOME/Library/Caches` |
| Windows | `%AppData%`（= `C:\Users\<user>\AppData\Roaming`） | `%LocalAppData%`（= `...\AppData\Local`） |
| Linux / Unix | `$XDG_CONFIG_HOME`、未設定なら `$HOME/.config` | `$XDG_CACHE_HOME`、未設定なら `$HOME/.cache` |

実測（macOS）:

```
GOOS: darwin GOARCH: arm64
UserConfigDir: /Users/takaaki/Library/Application Support
UserCacheDir: /Users/takaaki/Library/Caches
```

いずれも「アプリ固有のサブディレクトリを自分で作って使うこと」がドキュメントで指示されている。`$HOME` が未定義、または `$XDG_CONFIG_HOME` が相対パスだとエラーを返す。

### 2.2 データディレクトリ（セーブデータ等）の注意

**Go には `UserDataDir()` がない。** セーブデータ（バッテリーバックアップ SRAM）、セーブステート、スクリーンショットは「設定」ではないので、OS の慣習に従うと別の場所になる。

| OS | 慣習的なデータ置き場 | Go での取得方法 |
|---|---|---|
| macOS | `$HOME/Library/Application Support/<App>`（設定と同じ） | `os.UserConfigDir()` |
| Windows | `%AppData%\<App>`（設定と同じ）または `%LocalAppData%\<App>` | `os.UserConfigDir()` |
| Linux | **`$XDG_DATA_HOME`、未設定なら `$HOME/.local/share`** | **自前で実装する** |

### 2.3 採用するディレクトリ構成

アプリ識別子: `ShogunEmulator`（macOS / Windows）、`shogun-emulator`（Linux。XDG の慣習に合わせて小文字ハイフン）。

| 用途 | macOS | Windows | Linux |
|---|---|---|---|
| 設定 | `~/Library/Application Support/ShogunEmulator/config.json` | `%AppData%\ShogunEmulator\config.json` | `~/.config/shogun-emulator/config.json` |
| キーバインド | 同ディレクトリの `keybindings.json` | 同 | 同 |
| セーブデータ（SRAM） | `~/Library/Application Support/ShogunEmulator/saves/<rom-hash>.sav` | `%AppData%\ShogunEmulator\saves\` | `~/.local/share/shogun-emulator/saves/` |
| セーブステート（スロット） | `.../states/<rom-hash>/<slot>.state` | 同 | 同 |
| 入力ムービー | `.../movies/<rom-hash>/<name>.movie` | 同 | 同 |
| デバッグシンボル・ラベル | `.../symbols/<rom-hash>.json` | 同 | 同 |
| CHR/PRG オーバーレイ（`11_debugger_features.md` §6） | `.../patches/<rom-hash>.json` | 同 | 同 |
| スクリーンショット | `~/Pictures/ShogunEmulator/`（設定で変更可） | `%UserProfile%\Pictures\ShogunEmulator\` | `$XDG_PICTURES_DIR` または `~/Pictures/` |
| デバッグログ | `~/Library/Logs/ShogunEmulator/` | `%LocalAppData%\ShogunEmulator\logs\` | `~/.local/state/shogun-emulator/logs/`（未設定なら `~/.cache/shogun-emulator/logs/`） |
| キャッシュ（サムネイル等） | `~/Library/Caches/ShogunEmulator/` | `%LocalAppData%\ShogunEmulator\cache\` | `~/.cache/shogun-emulator/` |

> **`<rom-hash>` は何にするか**: ROM ファイルのパスではなく**内容のハッシュ**にする。理由: (1) ファイルを移動してもセーブが追従する、(2) 同じ ROM のコピーが別名であっても同じセーブを使える、(3) ROM が書き換わったら別扱いになる。
> **ハッシュは「ヘッダを除いた PRG-ROM + CHR-ROM の SHA-1」**にする。理由: iNES ヘッダはリッパーによって異なる（`05_cartridge_and_mappers.md` §2.4 の "DiskDude!" 問題）ため、ヘッダを含めると同じゲームが別扱いになる。短縮表示には先頭 16 桁を使う。

> **ポータブルモード**: 実行ファイルと同じディレクトリに `portable.txt`（または `config.json`）があれば、そこを設定ディレクトリとして使う。USB メモリに入れて持ち歩く用途と、テストの再現性のために有用。

### 2.4 設定ファイルの形式

**JSON**（project.md の明示要件）。

設計方針:

- **すべてのフィールドにデフォルト値を持たせ、`omitempty` は使わない。** 設定ファイルを読んだユーザーが「何が設定できるか」を一覧できるようにする
- **未知のフィールドは無視するが警告ログを出す**（バージョン間の互換性）
- **`"$schema"` と `"version"` フィールドを持つ**。将来のマイグレーションのため
- コメントが書けないので、**代わりに `docs/` に設定リファレンスを置き、GUI にツールチップを出す**
- 保存は「一時ファイルに書いて rename」（クラッシュで設定を失わない）

構造の概略:

```json
{
  "version": 1,
  "emulation": {
    "region": "auto",
    "ramInitPattern": "random",
    "mmc3IrqVariant": "sharp",
    "busConflicts": "auto",
    "dmcDmaRegisterConflicts": true,
    "cpuPpuAlignment": 0
  },
  "video": {
    "scale": 3,
    "integerScale": true,
    "aspectRatioCorrection": false,
    "filter": "nearest",
    "overscanTop": 8,
    "overscanBottom": 8,
    "overscanLeft": 0,
    "overscanRight": 0,
    "paletteFile": "",
    "syncMode": "audio"
  },
  "audio": {
    "enabled": true,
    "sampleRate": 48000,
    "bufferMilliseconds": 25,
    "ringHighWaterMultiplier": 2,
    "masterVolume": 1.0,
    "channelVolumes": { "pulse1": 1.0, "pulse2": 1.0, "triangle": 1.0, "noise": 1.0, "dmc": 1.0 },
    "filterProfile": "nes",
    "muteOnFastForward": true
  },
  "input": {
    "keybindingsFile": "keybindings.json"
  },
  "paths": {
    "romDirectory": "",
    "savesDirectory": "",
    "statesDirectory": "",
    "screenshotDirectory": "",
    "logDirectory": ""
  },
  "debug": {
    "enabled": false,
    "logCategories": ["warn.compat", "error"],
    "logDestination": "file",
    "logRotationMegabytes": 64,
    "logRotationKeep": 5,
    "traceRingBufferInstructions": 1000000,
    "breakOnUninitializedRamRead": false
  },
  "state": {
    "slotCount": 10,
    "screenshotInSlot": true,
    "rewindEnabled": true,
    "rewindSeconds": 60,
    "rewindIntervalFrames": 10,
    "rewindCompression": true
  },
  "movie": {
    "checksumIntervalFrames": 60,
    "stopOnDesync": true,
    "recordTurboAsPressed": true
  },
  "ui": {
    "theme": "system",
    "language": "ja",
    "recentRoms": [],
    "windows": {
      "main": { "x": 100, "y": 100, "width": 768, "height": 720, "visible": true },
      "cpuDebugger": { "x": 900, "y": 100, "width": 600, "height": 800, "visible": false }
    }
  }
}
```

キーバインドは別ファイル（`keybindings.json`）にする。理由: 設定 GUI のリセット操作を分離したい、他人と共有しやすい。

```json
{
  "version": 1,
  "players": [
    {
      "player": 1,
      "device": "standard",
      "bindings": {
        "up":     [{ "type": "key", "code": "ArrowUp" }],
        "down":   [{ "type": "key", "code": "ArrowDown" }],
        "left":   [{ "type": "key", "code": "ArrowLeft" }],
        "right":  [{ "type": "key", "code": "ArrowRight" }],
        "a":      [{ "type": "key", "code": "KeyX" }],
        "b":      [{ "type": "key", "code": "KeyZ" }],
        "start":  [{ "type": "key", "code": "Enter" }],
        "select": [{ "type": "key", "code": "ShiftRight" }]
      },
      "turbo": { "a": 0, "b": 0 }
    }
  ],
  "hotkeys": {
    "pause":        [{ "type": "key", "code": "Space" }],
    "frameAdvance": [{ "type": "key", "code": "Period" }],
    "fastForward":  [{ "type": "key", "code": "Tab" }],
    "reset":        [{ "type": "key", "code": "F1" }],
    "saveState":    [{ "type": "key", "code": "F5" }],
    "loadState":    [{ "type": "key", "code": "F7" }],
    "screenshot":   [{ "type": "key", "code": "F12" }]
  }
}
```

**キーコードの表現**: 物理キーの識別子を**プラットフォーム非依存の名前**（`KeyZ`、`ArrowUp`、`ShiftRight`）で保存する。W3C の `KeyboardEvent.code` の命名に合わせる理由: (1) 標準として広く知られている、(2) キーボードレイアウト（JIS / US / AZERTY）に依存しない物理位置を表す、(3) Wails を選んだ場合はそのまま使える。フレームワーク固有のキーコードとの変換テーブルを 1 箇所に置く。

1 アクションに複数のバインドを許す（配列）。キーボードとゲームパッドの併用、複数キーの割り当てに対応。

## 3. CLI（C2）

### 3.1 基本方針

```
shogun [オプション] [ROMファイル]
```

- **ROM ファイルを引数に渡すと、それを開いて GUI を起動する**（ファイルマネージャからの「このアプリで開く」にも対応）
- 引数なしで起動すると GUI が空の状態で起動する
- **すべてのオプションは設定ファイルの値を上書きする**（設定ファイルは書き換えない = 一時的な上書き）

### 3.2 オプション設計

| オプション | 説明 |
|---|---|
| `--help`, `-h` | ヘルプ |
| `--version` | バージョン、ビルド情報（コミットハッシュ、ビルド日時、Go バージョン） |
| `--config PATH` | 設定ファイルのパスを指定 |
| `--portable` | 実行ファイルのディレクトリを設定ディレクトリとして使う |
| `--region {auto,ntsc,pal,dendy}` | リージョン |
| `--scale N` | 拡大率 |
| `--fullscreen` | フルスクリーンで起動 |
| `--no-audio` | 音を出さない |
| `--audio-buffer MS` | オーディオバッファ（ms） |
| `--sample-rate HZ` | サンプリングレート |
| `--speed FACTOR` | 実行速度（例 `2.0`, `0.5`） |
| `--save-dir PATH` | セーブデータのディレクトリ |
| `--state-dir PATH` | セーブステートのディレクトリ |
| `--debug` | デバッグモードを有効化 |
| `--log {stdout,stderr,file}` | **ログ出力先**（project.md の要件） |
| `--log-dir PATH` | ログの保存先ディレクトリ |
| `--log-categories LIST` | ログカテゴリ（カンマ区切り。例 `trace.cpu,ppu.timing`） |
| `--break-at ADDR` | 起動時にブレークポイントを設定 |
| `--trace-log PATH` | CPU トレースをこのファイルに出す（nestest.log 形式） |
| `--headless` | **GUI を起動せずに実行する**（テスト ROM の自動実行用） |
| `--frames N` | N フレーム実行して終了（`--headless` と併用） |
| `--screenshot PATH` | 終了時にスクリーンショットを保存 |
| `--movie PATH` | 入力ムービーを再生（`14_savestate_and_movie.md` §7） |
| `--record-movie PATH` | 入力ムービーを記録 |
| `--movie-verify` / `--no-movie-verify` | 再生時に desync チェックサムを検証（デフォルト有効。`14` §7.4） |
| `--load-state PATH` | 起動時にセーブステートをロード |
| `--save-state-on-exit PATH` | 終了時にセーブステートを保存 |
| `--deterministic` | 決定論モード。RAM 初期値・CPU/PPU アライメント・DMA 位相を固定値にする（**テストで使う**。`14` §6.2） |
| `--ram-init {zero,ff,pattern,random}` / `--ram-seed N` | RAM 初期化パターンとシード |

**`--headless` + `--frames` + `--screenshot` + `--trace-log` の組み合わせが、テスト自動化の基盤になる。** これは開発の初期から必要。

### 3.3 実装

Go 標準の `flag` パッケージで足りる。外部依存（cobra / urfave-cli）を入れる理由が現時点ではない。

- サブコマンドは不要（`shogun` 単体 + オプション）
- ただし将来 `shogun test <rom>` のようなサブコマンドを足す可能性があるので、第 1 引数がサブコマンド名かどうかの判定だけ入れておく

### 3.4 設定の優先順位

```
コマンドライン引数 > 環境変数 > 設定ファイル > 組み込みデフォルト
```

環境変数は `SHOGUN_` プレフィックス（例 `SHOGUN_LOG_DIR`）。CI やコンテナでの利用を考えて入れておく。

## 4. シングルバイナリ配布（C3）

### 4.1 Go のクロスコンパイル

Go は `GOOS` / `GOARCH` を指定するだけでクロスコンパイルできる。**ただし cgo を使うライブラリがあると成立しない。**

| ライブラリ | cgo | クロスコンパイル |
|---|---|---|
| `ebitengine/oto/v3` | **不要**（Windows / macOS / Linux / BSD / Wasm） | ○ |
| Ebitengine | 一部プラットフォームで purego を使う。詳細は選定後に確認 | ○（実績多数） |
| Fyne | OpenGL バインディングで cgo を使う | △ `fyne-cross`（Docker ベース）が必要 |
| Gio | cgo を使う | △ |
| cimgui-go | cgo（C++） | ✗ ツールチェーンが必要 |
| Wails v3 | cgo（WebView） | ✗ 各 OS でビルドする |

**現実的な方針: CI（GitHub Actions）で macOS / Windows / Linux のランナーを使い、各 OS でネイティブビルドする。** クロスコンパイルに頼らない。これがもっとも確実で、AI/人間の開発者が再現できる。

対象:

| OS | アーキテクチャ | 備考 |
|---|---|---|
| macOS | arm64, amd64 | `lipo` で universal binary にまとめる |
| Windows | amd64, arm64 | |
| Linux | amd64, arm64 | glibc 版。musl（Alpine）は当面対象外 |

### 4.2 アセットの埋め込み

Go 1.16 以降の `embed` パッケージでアイコン・デフォルトパレット・フォント等をバイナリに埋め込む。

```go
import "embed"

//go:embed assets/icon.png assets/palettes/*.pal
var assets embed.FS
```

これで「シングルバイナリ」が成立する（外部ファイル不要）。

### 4.3 アイコン（C4）

**プレースホルダの作り方**: 「将」の 1 文字を白抜きで、単色背景に置いた正方形の PNG。SVG から各サイズを生成する。project.md が「いったんプレースホルダで」と言っているので、**デザインには時間をかけず、差し替え手順を整備することに時間をかける。**

必要なサイズと形式:

| OS | 形式 | サイズ |
|---|---|---|
| macOS（.app） | `.icns` | 16, 32, 64, 128, 256, 512, 1024（@1x/@2x） |
| Windows（.exe） | `.ico` | 16, 24, 32, 48, 64, 128, 256 |
| Linux | `.png` + `.desktop` | 16, 22, 24, 32, 48, 64, 128, 256 |
| ウィンドウアイコン（実行時） | `.png` | 任意（256 程度） |

生成:

- **元データは 1024×1024 の PNG と SVG** を `assets/icon/` に置く
- `.icns` / `.ico` はビルドスクリプトで生成する（macOS は `iconutil`、Windows は Go の `github.com/akavel/rsrc` や `github.com/josephspurrier/goversioninfo`）
- **生成物もリポジトリにコミットする**（ビルド環境に依存しないため）

Windows の `.exe` へのアイコン埋め込みは `goversioninfo` が一般的。バージョン情報リソース（会社名、製品名、バージョン）も同時に埋め込める。

### 4.4 macOS の .app バンドル

macOS では「シングルバイナリ」をそのままダブルクリックしても GUI アプリとして扱われない（Dock に出ない、メニューバーが出ない）。**`.app` バンドルが必要。**

```
Shogun Emulator.app/
  Contents/
    Info.plist          ← バンドル ID、バージョン、アイコン名、ドキュメントタイプ（.nes）
    MacOS/
      shogun            ← 実行ファイル（これ自体は単一バイナリ）
    Resources/
      icon.icns
```

`Info.plist` に `CFBundleDocumentTypes` を書けば、**Finder で .nes ファイルを「このアプリで開く」できる。**

配布形式:

- `.dmg`（ドラッグ&ドロップでインストール）
- **署名と公証（notarization）**: 署名しないと Gatekeeper が「開発元を確認できません」と出す。Apple Developer Program（年 $99）が必要。**当面は署名なしで配布し、README に「右クリック → 開く」の手順を書く。** 本格配布の段になったら判断する（人間の判断が必要）

### 4.5 Windows

- `.exe` 単体で配布可能
- **コード署名**がないと SmartScreen が警告を出す。EV 証明書は年数万円。**当面は署名なしで、README に手順を書く**
- `.zip` で配布。インストーラ（MSI / NSIS）は当面不要
- **コンソールウィンドウの問題**: `go build` した Windows バイナリはコンソールアプリとしてビルドされ、GUI 起動時に黒いコンソールウィンドウが出る。`-ldflags="-H windowsgui"` を付けると消えるが、**そうすると CLI モードで stdout が使えなくなる**（project.md の C2 と衝突）

  → **解決策**: `-H windowsgui` でビルドし、CLI 起動時（コマンドラインオプションが与えられた場合、または `--log=stdout` の場合）に `AttachConsole(ATTACH_PARENT_PROCESS)` を呼んで親のコンソールに接続する。これが Windows で「GUI と CLI 両対応」を実現する標準的な方法。`golang.org/x/sys/windows` で実装できる

### 4.6 Linux

- 単一バイナリ + `.desktop` ファイル + アイコンで配布
- `AppImage` にまとめると「1 ファイルでダブルクリック起動」になる。**これが project.md の「シングルバイナリ」の精神に最も近い**
- Flatpak / Snap は将来
- **実行時依存**: oto が PulseAudio または `libasound.so.2` を要求する（`10_audio_and_video_output.md` §2.1）。GUI フレームワークによって X11 / Wayland / OpenGL のライブラリも必要。**README に明記する**

### 4.7 バージョン情報の埋め込み

```bash
go build -ldflags "\
  -X main.version=$(git describe --tags --always) \
  -X main.commit=$(git rev-parse --short HEAD) \
  -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

`--version` でこれらを表示する。**バグ報告のときにどのビルドか分かることが重要。**

Go 1.24 以降は `debug.ReadBuildInfo()` で VCS 情報が自動的に埋まるので、そちらも併用する。

## 5. 実装方針のまとめ

| 項目 | 決定 |
|---|---|
| 設定ファイル | JSON。`os.UserConfigDir()` + アプリ名のサブディレクトリ |
| データディレクトリ | Linux のみ `$XDG_DATA_HOME` を自前実装。他は設定と同じ |
| ROM の識別 | **ヘッダを除いた PRG+CHR の SHA-1** |
| ポータブルモード | 実行ファイル横に `portable.txt` があれば有効 |
| キーバインド | 別ファイル。W3C `KeyboardEvent.code` 互換の物理キー名 |
| CLI | 標準 `flag`。引数 > 環境変数 > 設定ファイル > デフォルト |
| headless モード | `--headless --frames N --screenshot --trace-log` でテスト自動化 |
| ビルド | **CI で各 OS ネイティブビルド。クロスコンパイルに依存しない** |
| アセット | `embed` でバイナリに埋め込み |
| アイコン | 1024px PNG + SVG を原本に、`.icns` / `.ico` を生成してコミット |
| macOS | `.app` バンドル + `.dmg`。署名は後回し |
| Windows | `-H windowsgui` + `AttachConsole` で GUI/CLI 両対応 |
| Linux | 単一バイナリ + `.desktop` + AppImage |

## 6. 未解決・後続調査

- [ ] Fyne を選んだ場合の `fyne package` コマンドとの役割分担（`.app` / `.ico` 生成を任せるか自前でやるか）
- [ ] macOS の署名・公証の費用と手順（人間の判断が必要）
- [ ] AppImage の作り方（`linuxdeploy` など）
- [ ] Windows ARM64 のサポート状況
- [ ] 設定のマイグレーション（`version` フィールドが上がったときの処理）

## 7. 参考資料

| 資料 | URL / 確認方法 | 参照日 |
|---|---|---|
| `os.UserConfigDir` のドキュメント | `go doc os.UserConfigDir`（Go 1.25.3） | 2026-09-21 |
| `os.UserCacheDir` のドキュメント | `go doc os.UserCacheDir`（Go 1.25.3） | 2026-09-21 |
| XDG Base Directory Specification | https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html | 2026-09-21 |
| oto README（Linux の実行時依存） | https://github.com/ebitengine/oto | 2026-09-21 |
| W3C UI Events KeyboardEvent code values | https://www.w3.org/TR/uievents-code/ | 2026-09-21 |
