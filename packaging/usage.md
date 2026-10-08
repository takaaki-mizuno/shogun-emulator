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

インストーラ（`.msi`）を使う場合:

1. お使いの PC に合った `.msi` をダブルクリックします。通常の PC は `windows-x64`、Arm の PC（Snapdragon など）は `windows-arm64` です。
2. 「Windows によって PC が保護されました」（SmartScreen）と表示されたときは、「詳細情報」を押し、「実行」を押します。署名をしていないためです。
3. 画面の指示に従ってインストールします。管理者の権限を求められます。スタートメニューに「Shogun Emulator」が登録され、`.nes` ファイルを開けるようになります。
4. アンインストールは「設定」→「アプリ」→「インストールされているアプリ」から行います。新しい版の `.msi` を実行すると、古い版を置き換えます。

インストールせずに使う場合:

1. `.zip` を展開し、`shogun.exe` をダブルクリックします。
2. 初回に SmartScreen が表示されたときは、上と同じ手順で起動します。

コマンドプロンプトや PowerShell から `shogun.exe --version` のように引数付きで起動すると、出力がその画面に表示されます。インストーラで入れた場合、実行ファイルは `C:\Program Files\Shogun Emulator\shogun.exe` にあります。

### Linux

- Debian・Ubuntu（`.deb`）：`sudo apt install ./shogun-emulator_<版>-1_amd64.deb` で入れます。Arm の PC では `arm64` のファイルを使います。アプリの一覧に「Shogun Emulator」が出て、端末からは `shogun` で起動します。
- Fedora・RHEL・openSUSE（`.rpm`）：`sudo dnf install ./shogun-emulator-<版>-1.x86_64.rpm`（openSUSE は `sudo zypper install`）で入れます。
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

## 録画と操作の記録

「記録」メニューに 3 つの機能があります。

| 機能 | 内容 |
|---|---|
| 操作の記録・操作の再生 | ボタンの操作だけを記録したファイル（`.movie`）を作り、再生します。ファイルは小さく、このエミュレータと同じ ROM で再生すると、画も音もそのとおりに再現します。動画プレイヤーでは開けません |
| 録画 | 画面と音を動画（MP4）に保存します。QuickTime Player などで再生できます。拡大率は設定の「録画の拡大率」（1〜3 倍、既定 2 倍）で変えられます |
| 操作の記録から書き出す | 保存した操作の記録を、実時間より速く動画にします。記録したときと同じ ROM を開いてから使います |

動画は Motion JPEG という形式で、ファイルが大きくなります（2 倍で 1 分あたり数十 MB）。早送りやスローで遊んだ部分も、動画では等速になります。

コマンドラインからは `shogun --headless --movie 記録.movie --record-video 出力.mp4 ゲーム.nes` で、画面を出さずに操作の記録から動画を作れます。

## 設定とセーブデータの保存先

| 内容 | macOS | Windows | Linux |
|---|---|---|---|
| 設定（`config.json`・`keybindings.json`） | `~/Library/Application Support/ShogunEmulator/` | `%AppData%\ShogunEmulator\` | `~/.config/shogun-emulator/` |
| セーブデータ・ステート・操作の記録 | 同上 | 同上 | `~/.local/share/shogun-emulator/` |
| ログ | `~/Library/Logs/ShogunEmulator/` | `%LocalAppData%\ShogunEmulator\logs\` | `~/.local/state/shogun-emulator/logs/` |
| スクリーンショット | `~/Pictures/ShogunEmulator/` | `%UserProfile%\Pictures\ShogunEmulator\` | `~/Pictures/ShogunEmulator/` |
| 録画した動画 | `~/Movies/ShogunEmulator/` | `%UserProfile%\Videos\ShogunEmulator\` | `~/Videos/ShogunEmulator/` |

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
| `--record-video PATH` | 動画（MP4）を録画する |

## AI から使う

将軍エミュレータは、AI エージェント（Claude Code などの MCP クライアント）やプログラムから操作・観測・デバッグできます（Agent Interface）。AI は ROM を動かし、画面・メモリ・スプライト・ネームテーブルを見て、ブレークポイントやメモリの書き換えでデバッグできます。

### Claude Code に登録する

AI 専用の、画面を出さないエミュレータを使う場合:

```
claude mcp add shogun -- shogun mcp
```

ROM を開いた状態で始める場合は `--rom` を付けます。

```
claude mcp add shogun -- shogun mcp --rom /path/to/game.nes
```

プロジェクトで共有するときは `--scope project` を付けるか、プロジェクトの `.mcp.json` に次のように書きます。

```json
{
  "mcpServers": {
    "shogun": { "command": "shogun", "args": ["mcp"] }
  }
}
```

macOS の `.app` から使うときは、`command` に `"/Applications/Shogun Emulator.app/Contents/MacOS/shogun"` のように実行ファイルのパスを書きます。

### 画面を見ながら AI と共有する

人が見ている画面を AI と共有するときは、次の手順にします。

1. 将軍エミュレータを起動し、「AI」メニューの「AI からの接続を許可」を選ぶ（起動時に `--agent` を付けても同じ）
2. Claude Code に `--attach` 付きで登録する

```
claude mcp add shogun-gui -- shogun mcp --attach
```

AI が操作している間は、画面の上に「AI が操作中」と表示されます。「取り返す」を押すか、ゲームの入力キーを押すと、操作が人に戻ります。AI が操作している間は、一時停止やステートの読み込みなどのホットキーは効きません。

### AI がよく使うツール

| ツール | 内容 |
|---|---|
| `instance_create` | ROM を読み込んで AI 専用のエミュレータを作る |
| `exec_step` | 入力を押したまま N フレーム進め、何が変わったかを返す。`observe` に `{"include": ["image"]}` を付けると画面も返す |
| `exec_run_until` | 条件（`game.mode == 'play'`、`[score].w >= 100` など）が成り立つまで進める |
| `obs_get` | 今の様子を返す |
| `mem_read`・`mem_write` | メモリを読み書きする。`$0300` のほか、ROM と一緒に置いた ca65 の `.dbg` の名前も使える |
| `debug_bp_add` | ブレークポイントを置く |
| `gamestate_define`・`gamestate_get` | 「自機の X 座標」のような意味のある値を定義して読む |

ROM と同じ場所に ca65/ld65 の `.dbg`（`ld65 --dbgfile`）を置くと、AI は変数やラベルを名前で扱え、ソースの行も分かります。

なぜ動かないかを調べるためのツールもあります。

| ツール | 内容 |
|---|---|
| `diag_list`・`diag_configure` | ファミコン特有の誤りの疑い（描画中の VRAM アクセス、PPU の起動前の書き込み、未初期化 RAM の読み出し、スタックのあふれ、NMI の入れ子、データの実行など 11 種類）を、ソースの行つきで一覧にする。AI 用のエミュレータでは最初から有効です |
| `trace_enable`・`trace_query`・`trace_summary` | 実行した命令を記録し、フレーム・位置・JSR や書き込みなどで絞り込む。関数ごとの呼び出し回数とサイクル数に要約する |
| `profile_start`・`profile_report` | フレームごとの CPU の忙しさ、NMI 処理の長さ、VBlank の余裕、処理落ちのフレームを測る |

### コマンドラインから

`shogun serve` で待ち受け、`shogun ctl` で 1 つずつ操作できます。

```
shogun ctl instance create game.nes
shogun ctl exec step --frames 60 --input R+A
shogun ctl mem read player_x
shogun ctl events watch
```

`shogun ctl --help` で全コマンドの一覧が出ます。

### 自動テスト（Scenario）

`shogun run` は、操作とアサーションを書いた Scenario ファイル（YAML または JSON）を画面を出さずに実行します。ゲームの開発で「120 フレーム後にタイトルが出る」「この場面の画面がお手本と一致する」といった確認を CI に流せます。

```yaml
# tests/title.yaml
name: タイトルからゲーム開始まで
rom: ../build/game.nes           # Scenario ファイルからの相対パス
steps:
  - exec.step: {frames: 120}
  - assert: "game.mode == 'title'"
  - exec.input_sequence:
      steps: [{frames: 2, input: Start}, {frames: 10, input: ""}]
  - exec.run_until: {condition: "game.mode == 'play'", max_frames: 300}
  - assert_stop: condition
  - assert_screen: {golden: golden/play-start.png}
  - assert_mem: {loc: lives, equals: [3]}
```

ステップには AI のツールと同じ名前（`.` 区切り）と引数を書きます。アサーションは次の 5 つです。

| アサーション | 内容 |
|---|---|
| `assert: <式>` | 式が真であること（`game.mode == 'play'`、`[score].w >= 100` など） |
| `assert_stop: <理由>` | 直前の進行の止まった理由（`frames_done`・`condition`・`max_frames`・`breakpoint` など） |
| `assert_screen: {golden: PATH, max_diff_pixels: N}` | 画面がお手本の PNG と一致すること |
| `assert_mem: {loc: L, equals: [..]}` | メモリの内容 |
| `assert_no_diagnostics: [種類...]` | ファミコン特有の誤りの疑いが無いこと |

Scenario は既定で決定論的に実行します（`init: {deterministic: true}`）。毎回同じ結果になるように、止める条件には時間（`timeout_ms`）ではなくフレーム数（`max_frames`）を使ってください。

```
shogun run tests/*.yaml --junit report.xml --repro-dir repros
```

| 終了コード | 意味 |
|---|---|
| 0 | すべて合格 |
| 1 | ROM などを読み込めない |
| 5 | アサーションが失敗した |
| 6 | Scenario ファイルが正しくない（ファイル名・行番号・ステップの番号を出します） |

失敗すると、失敗したステップ・式・実際の値を表示し、その場面を再現する Repro（`repro.shgm` など）を書き出します。Repro は「記録 → 操作の再生 → 再生…」で開くと失敗の場面で止まります。`--junit` は JUnit XML を書きます。

**お手本の画像**: 初めは `shogun run tests/title.yaml --update-golden` で今の画面からお手本を作り、内容を確かめてからリポジトリに含めます。一致しないときは `<お手本>.actual.png` に実際の画面を書きます。お手本は常に既定のパレットで書くため、パレットの設定を変えてもテストは壊れません。

**AI の操作から作る**: AI が試した操作は `scenario_export` で Scenario として書き出せます（`scenario_mark` で印を付けると、その場面から書き出せます）。書き出したファイルには操作だけが入るので、確かめたいことを `assert` などで足してください。

GitHub Actions での例:

```yaml
- name: Scenario テスト
  run: shogun run tests/*.yaml --junit report.xml --repro-dir repros
- name: 結果を表示
  if: always()
  uses: mikepenz/action-junit-report@v5
  with:
    report_paths: report.xml
- name: 失敗の Repro を保存
  if: failure()
  uses: actions/upload-artifact@v4
  with:
    name: repros
    path: repros/
```

### 安全のための注意

AI からの接続を許可すると、同じ利用者で動くプログラムがこのエミュレータを操作し、メモリを読み書きできます。待ち受けは同じコンピュータの中（Unix ドメインソケット、または 127.0.0.1）だけで、接続にはキャッシュディレクトリの発見ファイルに書かれたトークンが要ります。使わないときは「AI からの接続を許可」を外してください。

## 開発者向け：テスト ROM の取得

ソースコードからテストを実行するときは、テスト ROM を次のコマンドで `testdata/roms/` へ取得します。ROM はリポジトリに含めていません。

```
go run ./tools/fetch-test-roms
go test ./...
```

## ライセンス

本体は MIT License です（`LICENSE`）。実行ファイルに含まれる他のソフトウェアのライセンスは `THIRD_PARTY_LICENSES.txt` にあります。
