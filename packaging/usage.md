# 将軍エミュレータ（Shogun Emulator）の使い方

NES（ファミリーコンピュータ）のエミュレータです。ROM ファイル（`.nes`）を開いて遊べるほか、パターンテーブル・ネームテーブル・スプライト・パレットのビューアとエディタ、メモリの 16 進ダンプ、逆アセンブラ、ブレークポイントとステップ実行を備えています。

## 動作環境

| OS | 必要なもの |
|---|---|
| macOS | macOS 11（Big Sur）以降。Apple シリコンと Intel の両方で動くユニバーサルバイナリです |
| Windows | Windows 10 以降（64 bit。x64 と Arm64） |
| Linux | x86-64 または arm64。OpenGL、X11 または Wayland、PulseAudio または ALSA（`libasound.so.2`） |

Linux で必要なものは、一般的なデスクトップ環境には最初から入っています。音が出ないときは PulseAudio（または PipeWire の PulseAudio 互換）が動いているか、`libasound.so.2` があるかを確かめてください。

## 起動

### macOS

1. `.dmg` を開き、`Shogun Emulator.app` を「アプリケーション」フォルダへドラッグします。
2. 初回だけ次の手順で開きます。署名と公証をしていないため、そのままダブルクリックすると「開発元を検証できない」と表示されて起動しません。
   - Finder で `Shogun Emulator.app` を Control キーを押しながらクリックし、「開く」を選び、表示されたダイアログで「開く」を押します。
   - 開けないときは「システム設定」→「プライバシーとセキュリティ」の下にある「このまま開く」を押します。
   - 端末からは `xattr -dr com.apple.quarantine "/Applications/Shogun Emulator.app"` でも開けるようになります。
3. 2 回目からは普通にダブルクリックで起動します。Finder で `.nes` ファイルを開くこともできます。

### Windows

1. `.zip` を展開し、`shogun.exe` をダブルクリックします。インストールは不要です。
2. 初回に「Windows によって PC が保護されました」（SmartScreen）と表示されることがあります。署名をしていないためです。「詳細情報」を押し、「実行」を押すと起動します。
3. コマンドプロンプトや PowerShell から `shogun.exe --version` のように引数付きで起動すると、出力がその画面に表示されます。

### Linux

- AppImage：ファイルに実行の許可を付けて（`chmod +x Shogun_Emulator-*.AppImage`）、ダブルクリックまたは端末から起動します。
- 実行ファイル単体（`.tar.gz`）：展開した `shogun` を起動します。デスクトップの一覧とファイルの関連付けに登録するときは、`shogun` を `PATH` の通った場所（`~/.local/bin` など）へ置き、展開した `share/` の中身を `~/.local/share/` へ写してから `update-mime-database ~/.local/share/mime` を実行します。

## 基本の操作

メニューの「ファイル」→「ROM を開く…」で ROM を開きます。コマンドラインから `shogun ゲーム.nes` のように ROM を渡しても開けます。

### キーの既定の割り当て

設定メニューの「キーバインドを開く…」で変えられます。

| ボタン | プレイヤー 1 | プレイヤー 2 |
|---|---|---|
| 上・下・左・右 | 矢印キー | W・S・A・D |
| A | X | G |
| B | Z | F |
| Start | Enter | R |
| Select | 右 Shift | T |

| 操作 | キー |
|---|---|
| 一時停止 | Space |
| コマ送り | .（ピリオド） |
| 早送り（押している間） | Tab |
| スロー（押している間） | `（バッククォート） |
| 巻き戻し（押している間） | Backspace |
| リセット | F1 |
| セーブステート・ロードステート | F5・F7 |
| スロットを前へ・次へ | F4・F6 |
| スクリーンショット | F12 |
| フルスクリーンの切り替え | F11 |

## 設定とセーブデータの保存先

| 内容 | macOS | Windows | Linux |
|---|---|---|---|
| 設定（`config.json`・`keybindings.json`） | `~/Library/Application Support/ShogunEmulator/` | `%AppData%\ShogunEmulator\` | `~/.config/shogun-emulator/` |
| セーブデータ・ステート・ムービー | 同上 | 同上 | `~/.local/share/shogun-emulator/` |
| ログ | `~/Library/Logs/ShogunEmulator/` | `%LocalAppData%\ShogunEmulator\logs\` | `~/.local/state/shogun-emulator/logs/` |
| スクリーンショット | `~/Pictures/ShogunEmulator/` | `%UserProfile%\Pictures\ShogunEmulator\` | `~/Pictures/ShogunEmulator/` |

実行ファイルと同じ場所に `portable.txt` という名前のファイルを置くか、`--portable` を付けて起動すると、実行ファイルの場所に設定とデータを保存します（USB メモリなどに入れて持ち運ぶとき）。

設定は設定メニューの「設定を開く…」で変えられます。

## コマンドライン

`shogun --help` で全オプションを表示します。よく使うものを挙げます。

| オプション | 内容 |
|---|---|
| `--scale N` | 拡大率（1〜8） |
| `--fullscreen` | フルスクリーンで起動する |
| `--no-audio` | 音声を出さない |
| `--load-state PATH` | 起動時にセーブステートを読み込む |
| `--debug` | CPU デバッガを開き、一時停止した状態で起動する |
| `--log file --log-categories mapper,ppu.register` | 指定したカテゴリのログをファイルへ出す |
| `--headless --frames 60 --screenshot out.png` | 画面を出さずに 60 フレーム実行し、画面を保存する |

## 開発者向け：テスト ROM の取得

ソースコードからテストを実行するときは、テスト ROM を次のコマンドで `testdata/roms/` へ取得します。ROM はリポジトリに含めていません。

```
go run ./tools/fetch-test-roms
go test ./...
```

## ライセンス

本体は MIT License です（`LICENSE`）。実行ファイルに含まれる他のソフトウェアのライセンスは `THIRD_PARTY_LICENSES.txt` にあります。
