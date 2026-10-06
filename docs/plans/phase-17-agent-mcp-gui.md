# フェーズ 17: MCP ブリッジと GUI 共有

- 作成日: 2026-10-04
- 前提フェーズ: 15（フェーズ 16 とは独立に進められる）
- 完了条件: `shogun mcp` を MCP クライアントの設定に登録して、全 Agent Command をツールとして呼べる。同じ要求を JSON-RPC・MCP ブリッジ・`shogun ctl` で送り、同じ結果を得る往復テストが合格する。GUI 版で Agent Interface を有効にすると、エージェントの Control の取得でバナーが出て Agent-Paced になり、人間の取り返しで Real-Time に戻り、`control_changed` イベントが届く

## 1. 背景

フェーズ 14・15 で、JSON-RPC と `shogun ctl` からエミュレータを動かし観測できるようになった。このフェーズでは、Claude Code などの MCP クライアントから使えるようにし、人間が見ている GUI をエージェントと共有できるようにする。

MCP は stdio のブリッジ（`shogun mcp`）として作り、JSON-RPC の前段に置く（`docs/adr/0001-jsonrpc-core-with-mcp-bridge.md`）。ブリッジは、プロセス内に headless の Host を持つか、`--attach` で起動中の GUI へ接続する。

GUI を共有するときは、人間のキー入力とエージェントの入力がぶつかる。入力の権利（Control）を一度に 1 人だけが持ち、エージェントが持つ間は GUI にバナーを出す（設計書 14 編 §14.4）。人間が遊んでいる最中にブレークポイントで止まったことなどは、エージェントが要求していない時点で起きるため、イベント（§14.14）で知らせる。

## 2. 方針とその理由

### 2.1 ブリッジは JSON-RPC のクライアントだけを使う

`internal/agent/mcpbridge` は `internal/agent` を参照せず、`internal/agent/rpc` のクライアントを通す。プロセス内に Host を持つときも `net.Pipe` でつなぐ（§14.2.1）。

理由: MCP の経路と JSON-RPC の経路で挙動が分かれないようにするためである。依存規則はフェーズ 14 で `internal/arch` に入れてある。

### 2.2 MCP のツール定義は登録簿から作る

ツールの名前・説明・引数のスキーマを、フェーズ 14 の登録簿から作る。ブリッジは起動時に `session.commands` を呼んで一覧を得る。

理由: ツールの一覧を手で書くと、Agent Command を足すたびにずれる。`session.commands` から作れば、`--attach` 先のプロセスの版と一致する。

### 2.3 MCP のツールの説明は英語にする

登録簿の `DescEN` を MCP に渡す。

理由: LLM は英語の説明のほうがツールを選ぶ精度が高い（§14.5.2）。

### 2.4 MCP SDK は実装を始める時点の最新版に固定する

公式の Go SDK `github.com/modelcontextprotocol/go-sdk` を使う。2026-10 時点の最新は v1.8.0 である。実装を始める時点で最新版と変更履歴を確認し、`go.mod` に固定する。

理由: MCP の仕様と SDK の更新は速く、計画を書いた時点の版が実装の時点で古い可能性がある。

### 2.5 MCP ブリッジはイベントを購読せず、`events.poll` を使う

JSON-RPC の通知 `events.event` は `events.subscribe` を送った接続にだけ送る。MCP ブリッジは購読しない。

理由: MCP クライアントの多くはサーバからの通知をエージェントに渡さない（§14.14）。

### 2.6 GUI 版の Agent Interface は既定で無効にする

設定 `agent.enabled`、引数 `--agent`、「AI」メニューで有効にする（§14.21）。

理由: GUI はゲームを遊ぶためにも使う。利用者が意図しないまま、ローカルの他のプロセスからメモリを書き換えられる状態にしない。

### 2.7 人間がゲームの入力キーを押したら Control を取り返す

バナーの「取り返す」ボタンに加え、ゲームの入力キーを押したときも人間に戻す（§14.4.1）。

理由: 人間がゲームに触ろうとした時点で、操作したい意図は明らかである。ボタンを探させない。

## 3. タスク

### 3.1 MCP SDK の導入

- [x] 公式の Go SDK の最新版を確認する（リリースノート、対応する MCP の仕様の版、stdio Transport の API）
- [x] 確認した版を `go.mod` に固定し、版と確認した日をこのファイルの末尾に記録する（§7）
- [x] SDK のライセンスを、設計書 13 編の同梱ライセンス一覧に加える（`THIRD_PARTY_LICENSES.txt` は `go list -deps` から作るため、SDK は自動で含まれることを確かめた。SDK は MIT から Apache-2.0 へ移行中）
- [x] `internal/agent/mcpbridge/` を作り、`doc.go` を置く
- [x] `internal/arch` の検査で、SDK を参照するのが `mcpbridge` だけであることを確かめる

### 3.2 ブリッジの本体

- [x] `internal/agent/mcpbridge/bridge.go` にブリッジを実装する
  - [x] JSON-RPC のクライアント（`rpc.Client`）を受け取る
  - [x] 起動時に `session.commands` を呼び、ツールの一覧を作る
  - [x] MCP のツール名を JSON-RPC の名前に戻すときは、フェーズ 14 の変換関数（登録簿を引くもの）を使う
- [x] ツール定義を作る
  - [x] 名前を §14.5.4 の規則で変換する
  - [x] 説明に `DescEN` を使う
  - [x] 引数の JSON Schema を `session.commands` の結果から渡す
- [x] 結果を変換する（§14.5.2）
  - [x] JSON の値を `TextContent`（JSON の文字列）と `structuredContent` に入れる
  - [x] 画像（Observation の `image`、`obs.screenshot`、`obs.patterns` の画像）を `ImageContent`（`image/png`）にする
  - [x] 画像を `TextContent` の JSON から取り除き、代わりに大きさを入れる（Base64 を二重に送らない）
  - [x] エラーを `isError: true` の結果にし、`TextContent` に `kind` と説明を入れる
- [x] MCP のキャンセル通知（`notifications/cancelled`）を受けたら、対応する要求について `exec.cancel` を送る
- [x] 1 つの MCP セッションを 1 つの JSON-RPC 接続に対応させる（Control と差分の単位を揃えるため）

### 3.3 shogun mcp

- [x] `shogun mcp` を実装する（§14.5.2）
  - [x] 引数なし: プロセス内に headless の Host を持ち、`net.Pipe` でブリッジとつなぐ
  - [x] `--rom PATH`: Instance を 1 つ作って読み込む
  - [x] `--attach`: 発見ファイルから起動中のプロセスを選んで接続する（`kind` が `gui` で最も新しいもの）
  - [x] `--attach PID`: 指定したプロセスへ接続する
  - [x] 接続先が見つからないときは、理由を標準エラーに出して終了コード 1
  - [x] headless 向けのオプション（`--deterministic`・`--ram-init`・`--ram-seed`）を受け付ける
- [x] stdio を MCP 専用にする。ログは標準エラーかファイルに出し、標準出力に何も書かない
- [x] `--attach` 先のプロセスが終了したとき、MCP クライアントへエラーを返してから終える

### 3.4 GUI 版での有効化

- [x] 設定 `agent.enabled`（既定 `false`）を加える（§14.23）
- [x] 引数 `--agent` と `--agent-listen ADDR` を加える
- [x] 「AI」メニューを加える
  - [x] 「AI からの接続を許可」（チェック。`agent.enabled` に反映する）
  - [x] 「接続先をコピー」（発見ファイルのパスをクリップボードへ）
  - [x] 「接続中のクライアント」（接続名と Control の持ち主の一覧）
  - [x] 「すべての接続を切る」
- [x] 有効にしたとき GUI 版の Host を作り、表示中の `emu.Emulator` を ID `i1` の Instance として登録し、待ち受けを始める
- [x] 無効にしたとき、すべての接続を切り、待ち受けを止め、発見ファイルを消す
- [x] ROM を開き直したとき、Instance の `i1` を保ったまま中身を差し替え、`rom_loaded` イベントを積む
- [x] 文言を既存の文言の集約（設計書 10 編 §10.9）に加える
- [x] 設定画面に `agent.enabled`・`agent.listen`・`agent.observeImageScale` を加える

### 3.5 Control のバナーと取り返し

- [x] エージェントが Control を持つ間、メインウィンドウの上端にバナーを出す（§14.4.3）。表示は「AI が操作中: <client>（接続 #N）」と「取り返す」ボタン
- [x] Control を持たない接続があるとき、ステータスバーに「AI 接続中（N）」を出す
- [x] 「取り返す」ボタンで人間に戻す
- [x] ゲームの入力キーを押したら人間に戻す（ホットキーでは戻さない）
- [x] 取り返したら Real-Time にして走らせる（取り返す直前の一時停止の状態は保たない。§14.4.2）
- [x] 取り返したとき、進行中の要求を `stop_reason: control_lost` で終える
- [x] 取り返したとき `control_changed` イベントを積む
- [x] UI の操作をフェーズ 14 で用意した口から行い、`fyne.Do` の呼び出しを `internal/ui/dispatch.go` に限る規則（設計書 10 編 §10.3）を守る
- [x] エージェントが Control を持たずにブレークポイントを追加したとき、ステータスバーに「AI がブレークポイントを追加」と出す（§14.7.1）

### 3.6 exec.run と exec.pause

- [x] `exec.run` を実装する。Control を持ったまま Real-Time で走らせる（ゲームの様子を人間に見せるため）
- [x] `exec.run` の間もキーボードの入力を使わない（Control はエージェントにある）
- [x] `exec.pause` を実装する。一時停止して Agent-Paced に戻す
- [x] headless の Host では `exec.run` と `exec.pause` をエラー `unsupported_in_headless`（-32014）で断る（設計書 14 編 §14.5.1）

### 3.7 イベント

- [x] `internal/agent/events.go` にイベントの型とキューを実装する（§14.14）
  - [x] 種類: `breakpoint_hit`・`diagnostic`・`control_changed`・`rom_loaded`・`rom_changed`・`movie_desync`・`instance_closed`
  - [x] 各イベントに `seq`（Instance ごとの通し番号）・`frame`・`time`（RFC 3339）を付ける
  - [x] Instance ごとに最新 1000 件を保持する
- [x] `events.poll` を実装する。`since` より後を最大 `max`（既定 100）件返し、あふれて捨てた数を `dropped` に入れる
- [x] `events.subscribe` と `events.unsubscribe` を実装する。購読した接続に JSON-RPC の通知 `events.event` を送る
- [x] 通知の送信で接続の書き込みが詰まっても、エミュレーションゴルーチンを止めない（接続ごとの送信キューを置き、あふれたら古い通知を捨てて `dropped` を数える）
- [x] Observation の `events_pending` にキューの未読の数を入れる（接続ごとの `since` で数える）
- [x] ブレークポイントでの停止、ROM の読み込み、ムービーの desync、Instance の終了でイベントを積む
- [x] `diagnostic` を積む口を作る（Diagnostic はフェーズ 20）
- [x] `rom_changed` を積む口を作る（ファイルの監視はフェーズ 18）
- [x] Agent-Paced の進行中に起きたことも同じくイベントに積むことを確かめる

### 3.8 shogun ctl events watch

- [x] `shogun ctl events watch` を実装する。`events.subscribe` を送り、届いた通知を 1 行ずつ表示し続ける
- [x] `--kinds` で種類を絞り込む
- [x] `SIGINT` で `events.unsubscribe` を送って終える

### 3.9 Claude Code への登録手順

- [x] 利用者向けの文書（同梱文書。設計書 13 編）に「AI から使う」の節を加える
  - [x] Claude Code に `shogun mcp` を登録する手順（`claude mcp add` の例と、設定ファイルに書く例）
  - [x] GUI と共有する手順（GUI で「AI からの接続を許可」→ `shogun mcp --attach` を登録）
  - [x] よく使うツールの例（`exec_step`・`obs_get`・`mem_read`・`exec_run_until`）
  - [x] セキュリティの注意（Agent Interface を有効にすると、同じ利用者のプロセスからメモリを読み書きできる）
- [x] 登録手順の `claude mcp add` の書き方を、実装を始める時点の Claude Code の文書で確認する

### 3.10 Transport の往復テスト

- [x] 同じ要求を JSON-RPC・MCP ブリッジ・`shogun ctl` で送り、同じ結果を得ることを確かめるテストを書く（§14.24）
  - [x] 対象: `instance.create`・`exec.step`（画像あり）・`mem.read`・`mem.write`・`exec.run_until`・エラーになる要求
  - [x] MCP の結果の `structuredContent` と JSON-RPC の結果を比べる
  - [x] 画像のバイト列が一致することを確かめる
- [x] MCP のキャンセル通知で `run_until` が `stop_reason: cancelled` で終わることを確かめるテストを書く
- [x] MCP のテストは SDK のクライアントをプロセス内で使い、stdio の代わりにパイプでつなぐ

### 3.11 GUI 共有のテスト

- [x] GUI 版の Host をテストで作り（Fyne のテスト用ドライバを使う）、Control の取得で Agent-Paced になり、バナーが出ることを確かめる
- [x] 人間の取り返しで Real-Time になり、`control_changed` イベントが届くことを確かめる
- [x] 進行中の取り返しで `stop_reason: control_lost` が返ることを確かめる
- [x] Agent Interface を無効にしたとき、発見ファイルとソケットファイルが消えることを確かめる
- [ ] 目視の確認項目を挙げる（バナーの表示、「取り返す」ボタン、キー入力での取り返し、ステータスバー）。目視の確認は人が行い、結果をこのファイルに記す（項目は §7 に挙げた。目視の確認は未実施）

### 3.12 フェーズの締め

- [x] `go test ./...` がローカルの macOS で通ることを確認する（Git を使わない方針のため CI の 3 OS 実行は確認できない。その旨を記す）（macOS で合格。CI の 3 OS 実行は未確認）
- [x] 静的検査（`internal/arch` を含む）が通ることを確認する
- [x] 決定論テストが引き続き合格することを確認する
- [x] 実装が設計書 10 編・14 編と食い違った箇所があれば、先に設計書を直したことを確認する
- [x] `docs/plans/README.md` のフェーズ 17 の状態を「完了」にする

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| MCP | Claude Code に `shogun mcp` を登録し、ツールの一覧に全 Agent Command が出る |
| 画像 | `exec_step` に画像を付けると `ImageContent` で返る |
| 往復 | 同じ要求を 3 つの Transport で送り、結果が一致する |
| キャンセル | MCP のキャンセル通知で `run_until` が止まる |
| GUI の有効化 | 既定では待ち受けず、「AI からの接続を許可」で待ち受けを始める |
| Control | 取得でバナーが出て Agent-Paced になり、取り返しで Real-Time に戻り、`control_changed` が届く |
| イベント | `events.poll` と `events.subscribe` の両方でブレークポイントでの停止を受け取れる |
| 依存規則 | SDK を参照するのが `mcpbridge` だけであることを検査が確かめる |
| 決定論 | 決定論テスト 4 ムービーのハッシュが一致する |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| Symbol・Game State（ツールとしては 16 の完了後に自動で現れる） | フェーズ 16 |
| ROM ファイルの監視と `rom_changed` の発生 | フェーズ 18 |
| `agent.romWatchAction` と GUI の自動読み直し | フェーズ 18 |
| `shogun run`（Scenario） | フェーズ 19 |
| Diagnostic と `diagnostic` イベントの発生 | フェーズ 20 |
| MCP の HTTP Transport | 扱わない（ADR 0001） |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| MCP クライアントがサーバを認識しない | 標準出力にログを書いている。stdio を MCP 専用にする |
| MCP のツール名から元の名前に戻らない | アンダースコアを含む名前がある。登録簿を引く変換を使う |
| 画像が二重に送られて応答が大きい | `TextContent` の JSON から画像を取り除いているか確認する |
| `--attach` 先が見つからない | GUI で「AI からの接続を許可」を有効にしたか、古い発見ファイルが残っていないか確認する |
| 取り返したのにエージェントの進行が続く | 進行中の要求を `control_lost` で終えているか確認する |
| ホットキーで Control が戻ってしまう | ゲームの入力キーとホットキーを区別しているか確認する |
| 通知の送信でゲームがカクつく | エミュレーションゴルーチンから直接書いている。接続ごとの送信キューを通す |
| GUI のバナーの更新でクラッシュする | UI スレッド以外から Fyne を触っている。`dispatch.go` を通す |
| SDK の更新で API が変わった | `go.mod` に固定した版を使っているか確認する |

## 7. 記録

### 7.1 MCP SDK

| 項目 | 内容 |
|---|---|
| モジュール | `github.com/modelcontextprotocol/go-sdk` |
| 版 | v1.8.0（2026-10-05 に `go list -m -versions` で最新を確認） |
| 対応する MCP の仕様の最新 | 2026-07-28 |
| 使った API | `mcp.NewServer`・`Server.AddTool`（JSON Schema をそのまま渡す低水準の形）・`StdioTransport`・テストで `NewInMemoryTransports` と `Client` |
| キャンセル | SDK がツールのハンドラの `ctx` を取り消す形で届く。ブリッジは `exec.cancel`（`id` つき）を送る |
| ライセンス | MIT から Apache-2.0 へ移行中。`THIRD_PARTY_LICENSES.txt` に自動で含まれる |

`shogun mcp` を実際のバイナリで stdio に流し、`initialize`・`tools/list`（54 個）・`tools/call`（`exec_step` に画像を付けると `image` の内容が返る）を確かめた。Claude Code への登録の書き方（`claude mcp add <名前> -- <コマンド> [引数...]`、`--scope`）は、手元の `claude mcp add --help` で確かめた。

### 7.2 実装で決めたこと

| 判断 | 内容 | 理由 |
|---|---|---|
| `exec.cancel` の `id` | 指定した要求だけを取り消す | MCP のキャンセルは特定の要求に対するもの。すべてを取り消すと、直後に送った別の要求まで巻き込んだ（テストで見つけた） |
| headless の Control の自動の取得 | 誰も持っていなければ、進行・書き換えを要求した接続が得る | `shogun ctl` は要求ごとに接続し直すため、毎回 `control_required` になっていた |
| AI の操作中のホットキー | 進行と状態を変えるホットキーを無視する | 人間の一時停止などが AI の進行と混ざる。画面と音だけのもの（スクリーンショット、フルスクリーン、消音）は受け付ける |
| ブレークポイントのイベントの順 | 進行の結果を返す前に積む | 結果の直後の `events_pending` に数えるため（race 検出器付きのテストで見つけた） |
| GUI の表示 | 画面の更新で Agent の状態を読む | 他のゴルーチンから Fyne を触らない（設計書 10 編 §10.3） |

### 7.3 目視の確認項目（人が行う。未実施）

| 項目 | 確かめること |
|---|---|
| AI メニュー | 「AI からの接続を許可」のチェックで待ち受けが始まり、ステータスバーに「AI 待受中」が出る |
| 接続 | `shogun ctl --pid <GUI の PID> instance list` が通り、ステータスバーが「AI 接続中（1）」になる |
| バナー | `shogun ctl control acquire` で上端に「AI が操作中: shogun-ctl（接続 #N）」と「取り返す」が出て、ゲームが止まる |
| 取り返し | 「取り返す」とゲームの入力キー（既定の X など）のどちらでもバナーが消え、ゲームが走る |
| ホットキー | AI の操作中に Space（一時停止）を押すと、無視した旨がステータスバーに出る |
| Claude Code | `claude mcp add shogun-gui -- shogun mcp --attach` で登録し、Claude Code から `obs_screenshot` などを呼べる |
