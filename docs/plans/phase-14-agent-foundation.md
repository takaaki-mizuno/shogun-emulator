# フェーズ 14: Agent Interface の基盤

- 作成日: 2026-10-04
- 前提フェーズ: 13（フェーズ 13 の残項目（Windows と Linux の実機確認、CI とリリースの実行、人に確認する 5 点）とは独立に進められる）
- 完了条件: `shogun serve` が Unix ドメインソケットで待ち受け、`shogun ctl` から `session.hello`・`instance.create`・`instance.fork`・`instance.list`・`instance.close`・`control.*` が動作する。トークンなし・誤ったトークンの接続を断るテストと、`internal/arch` の新しい依存規則の検査が合格する

## 1. 背景

AI エージェントやプログラムからエミュレータを操作・観測・デバッグする窓口として、Agent Interface（設計書 14 編）を設ける。このフェーズは、その土台となる部分を作る。

Agent Command の実装は JSON-RPC 層の 1 か所に置き、MCP と CLI はその前段の変換とする（`docs/adr/0001-jsonrpc-core-with-mcp-bridge.md`）。そのため、最初に作るのは JSON-RPC のサーバとクライアント、Agent Command の登録簿、Instance を管理する Host である。進行と観測の Agent Command はフェーズ 15、MCP はフェーズ 17 で載せる。

現在の `emu.Emulator` は GUI から使われることを前提にしている。`StepFrames` などの進行の操作はコマンドキューへ送るだけで、完了を待つ手段が無い（`internal/emu/command.go` の `cmdStep` は `done` を持たない）。エージェントは進行の結果を受け取ってから次の判断をするため、完了の通知が要る。このフェーズでその仕組みを加える。

## 2. 方針とその理由

### 2.1 Agent Command を登録簿 1 か所で定義する

`CommandSpec`（設計書 14 編 §14.7.1）の一覧から、JSON-RPC の振り分け、`session.commands` の結果、CLI のヘルプと引数の変換を作る。フェーズ 17 で MCP のツール定義、フェーズ 19 で Scenario の検証も同じ一覧から作る。

理由: Transport ごとに Agent Command の一覧を持つと、追加のたびに 3 か所を直すことになり、必ずずれる。

### 2.2 JSON-RPC は標準ライブラリで実装する

`encoding/json` と `net` だけで、改行区切りの JSON-RPC 2.0 を実装する。

理由: MCP SDK の内部の JSON-RPC 実装は公開 API ではなく、依存すると SDK の更新で壊れる（設計書 14 編 §14.2.3）。改行区切りにすれば `nc` で手で試せる。

### 2.3 進行の操作に完了通知を加える

`cmdStep` に `done chan StepResult` を加える。`StepResult` は停止の理由（フレーム数に達した、ブレークポイント、停止要求など）と停止した位置を持つ。既存の GUI からの呼び出し（`Step`・`StepFrames`・`FrameAdvance`）は `done` を `nil` のまま送り、動作を変えない。

理由: エージェントは進行の結果（Stop Reason と Observation）を受け取ってから次の判断をする（設計書 14 編 §14.8）。完了をポーリングで待つと、待ち時間の分だけ遅くなり、止まった理由も分からない。

### 2.4 Instance は `emu.Emulator` を 1 つずつ包む

headless の Host は Instance ごとに `emu.Emulator` を作り、それぞれがエミュレーションゴルーチンを 1 本持つ。オーディオデバイスは開かず、`NoPacer` で動かす。

理由: `emu.Emulator` はすでに 1 台分のエミュレータとして閉じており、エミュレーション状態を触るのはそのゴルーチンだけという規約（設計書 01 編 §1.5）をそのまま保てる。Instance のために `internal/nes` を変えずに済む。

### 2.5 Fork はセーブステートを経由する

`SaveState` で取り出した状態を、新しい `emu.Emulator` の `LoadState` に渡す。

理由: セーブステートの往復テスト（設計書 12 編 §12.4）で、状態の複製が完全であることがすでに確かめられている。Machine State を直接コピーする経路を新たに作ると、往復テストの保証の外に出る。

### 2.6 Unix ドメインソケットでもトークンを要求する

`session.hello` のトークンを、待ち受けの方式によらず必ず確かめる。

理由: 方式によって認証の有無が変わると、TCP へ切り替えたときに穴が開く（設計書 14 編 §14.5.1）。

### 2.7 `internal/agent` は `internal/ui` を参照しない

依存規則を `internal/arch` の静的検査に加える（設計書 14 編 §14.2.2）。

理由: headless の Host と GUI 版の Host で同じコードを使うためである。この規則を最初のフェーズで検査に入れておかないと、GUI との統合（フェーズ 17）で破れても気付かない。

## 3. タスク

### 3.1 パッケージと依存規則

- [x] `internal/agent/`・`internal/agent/rpc/` を作り、それぞれに `doc.go` を置く（設計書 14 編 §14.2.2）
- [x] `internal/agent/mcpbridge/`・`internal/agent/scenario/` は、使うフェーズ（17・19）で作る。このフェーズでは作らない
- [x] `internal/arch` の依存規則に次を加える
  - [x] `internal/agent` は `internal/ui` を参照しない
  - [x] `internal/agent/rpc` は `internal/ui` を参照しない
  - [x] `internal/agent/mcpbridge` は `internal/agent` を直接参照しない（`internal/agent/rpc` だけを使う）
  - [x] MCP SDK（`github.com/modelcontextprotocol/go-sdk`）を参照するのは `internal/agent/mcpbridge` だけ
  - [x] `internal/nes` は `internal/agent` 以下を参照しない
- [x] 規則を破るコードを置いたときに検査が失敗することを、既存の `detect_test.go` と同じ方法で確かめる
- [x] 設計書 01 編 §1.3 のパッケージ構成と §1.4 の依存の図が 14 編 §14.2.2 と一致していることを確かめる（食い違えば先に設計書を直す）

### 3.2 進行の完了通知

- [x] `internal/emu` に `StepResult` を定義する。停止の理由（`StopFramesDone`・`StopStepDone`・`StopBreakpoint`・`StopCancelled`・`StopCPUHalted`）と、フレーム番号・PC・命令の途中かどうかを持つ
- [x] `cmdStep` に `done chan StepResult` を加える
- [x] フレーム・命令・サイクルの単位（コマンドの処理の中で終わるもの）で、終わったときに `done` へ送る
- [x] オーバー・アウト・スキャンライン・カーソルまでの単位（停止条件を置いて再開するもの）で、止まったときに `done` へ送る
- [x] ブレークポイントで止まったとき、`StopBreakpoint` とブレークポイントの情報を送る
- [x] ROM の取り外しやリセットで進行が打ち切られたとき、`StopCancelled` を送る（`done` を送らずに放置しない）
- [x] `done` が `nil` のときは何も送らず、既存の動作を変えない
- [x] 待つ側の API として `StepAndWait(ctx, kind, count) (StepResult, error)` を加える。`ctx` が取り消されたら停止を要求し、止まった結果を返す
- [x] 既存の GUI からの呼び出し（`Step`・`StepFrames`・`FrameAdvance`・`RunTo`）の挙動が変わらないことを、既存のテストで確かめる
- [x] 完了通知を加えても決定論テスト 4 ムービーのハッシュが一致することを確かめる（`TestDeterminismHashes` が合格）

### 3.3 Agent Command の登録簿

- [x] `internal/agent/command.go` に `CommandSpec` と `CommandClass`（`ClassObserve`・`ClassConfig`・`ClassAdvance`・`ClassMutate`・`ClassSession`）を定義する（設計書 14 編 §14.7.1）
- [x] `Registry` を実装する。名前の重複を登録時に検出して `panic` する（起動時の誤りであり、エミュレーション中ではない）
- [x] 名前が小文字の英字・数字・アンダースコア・ドットだけからなることを登録時に検査する（§14.5.4）
- [x] `Params` の構造体から JSON Schema を作る関数を実装する
  - [x] 整数・文字列・真偽値・配列・構造体・省略可能（`omitempty`）を扱う
  - [x] 既定値と説明をタグ（`desc:"..."`・`default:"..."`）から取る
  - [x] フィールドの順を構造体の定義順に保つ（`map` の反復を使わない）
- [x] `Context` を定義する。接続の ID、対象の Instance、Host を持つ
- [x] 引数 `instance` の解決を実装する。省略時に Instance が 1 つならそれ、0 個なら `instance_not_found`、2 つ以上なら `instance_required`（§14.3.1）
- [x] 分類が `ClassAdvance`・`ClassMutate` の Agent Command を、Control を持たない接続から呼んだとき `control_required` を返す処理を、振り分けの共通部分に置く
- [x] `GUI: false` の Agent Command を GUI 版の Host で呼んだとき `unsupported_in_gui` を返す処理を、振り分けの共通部分に置く
- [x] `session.commands` を実装する。登録簿の全 Agent Command の名前・分類・説明（日本語と英語）・引数のスキーマを返す

### 3.4 名前の変換

- [x] `internal/agent/names.go` に変換関数を実装する（§14.5.4）
  - [x] JSON-RPC → MCP（ドットをアンダースコアへ）
  - [x] MCP → JSON-RPC（登録簿を引いて戻す。名前にアンダースコアを含むため、単純な置き換えでは戻せない）
  - [x] JSON-RPC → CLI（ドットを空白へ、アンダースコアをハイフンへ）
  - [x] CLI → JSON-RPC
- [x] 登録簿のすべての名前が 3 方向で往復できることを確かめるテストを書く
- [x] MCP の名前が衝突する組み合わせ（`a.b_c` と `a_b.c`）を登録時に検出する

### 3.5 Host と Instance

- [x] `internal/agent/host.go` に `Host`・`Instance`・`romShared` を定義する（§14.3.1）
- [x] Instance の一覧をスライスで持ち、作成順を保つ
- [x] Instance ID を `i1`・`i2`… の形で振る。閉じた ID は再利用しない
- [x] `romShared` を ROM ハッシュごとに 1 つ作り、同じ ROM の Instance で共有する。この時点では `symbols` に既存の `*debug.Symbols` を入れ、`gamestate` は空にしておく（フェーズ 16 で埋める）
- [x] 最後の Instance が閉じたとき `romShared` を捨てる
- [x] headless の Host で `emu.Emulator` を作る関数を実装する
  - [x] オーディオデバイスを開かない
  - [x] `NoPacer` を使う
  - [x] 一時停止した状態で ROM を読み込む（設計書 11 編 §11.5.2 と同じ理由）
  - [x] 引数 `deterministic`・`ram_init`・`ram_seed` を `InitState` に反映する
- [x] GUI 版の Host を作る関数を実装する。既存の `emu.Emulator` を受け取り、ID `i1` の Instance として登録する
- [x] `agent.maxInstances`（既定値 16）を超える作成で `limit_exceeded` を返す
- [x] Host の終了時に全 Instance の `emu.Emulator` を止める

### 3.6 Fork

- [x] `instance.fork` を実装する（§14.3.2）
- [x] 元の Instance で `SaveState` を取り、新しい `emu.Emulator` に同じ ROM の内容を読み込んでから `LoadState` する
- [x] オーバーレイを複製する
- [x] ブレークポイントとウォッチを複製し、以降は別々に持つ
- [x] Symbol は `romShared` を共有する
- [x] Freeze・Diagnostic・トレース・プロファイルの設定を複製する処理の置き場所を作る（中身は各フェーズで埋める。この時点で複製の関数に項目のコメントを並べておく）
- [x] 常時記録の複製の置き場所を作る（フェーズ 18 で埋める）
- [x] 新しい Instance の Control を、Fork を要求した接続に持たせる
- [x] 新しい Instance のイベントキューを空で始める
- [x] 命令の途中で止まっている Instance を Fork するとき、命令を完了させてから複製し、応答の `notes` にその旨を入れる
- [x] Fork した 2 つの Instance を同じだけ進めて、状態のハッシュが一致することを確かめるテストを書く
- [x] 一方だけを進めて、もう一方が変わらないことを確かめるテストを書く

### 3.7 Control

- [x] `internal/agent/control.go` に `controlState` を定義する。持ち主（なし・人間・接続 ID）と進行モード（Agent-Paced・Real-Time）を持つ（§14.4）
- [x] `control.acquire` を実装する
  - [x] 他の接続が持っているとき `control_held`
  - [x] 人間が持っているとき（GUI 版）は奪い、Instance を一時停止して Agent-Paced にする
  - [x] 同じ接続が 2 回呼んでもエラーにしない
- [x] `control.release` を実装する。GUI 版では人間に戻して Real-Time にする（取り返す直前の一時停止の状態は保たない。§14.4.2）
- [x] `control.status` を実装する。持ち主・進行モード・接続名（`session.hello` の `client`）を返す
- [x] 接続が切れたとき、その接続が持っていた Control を返す
- [x] headless の Instance を作った接続に Control を持たせる
- [x] 人間の取り返し（GUI のバナー）を受ける関数の口を用意する。GUI 側の実装はフェーズ 17 で行う

### 3.8 JSON-RPC サーバ

- [x] `internal/agent/rpc/conn.go` に改行区切りの読み書きを実装する（§14.5.1）
  - [x] 1 行が 16 MiB を超えたら、エラーを返して接続を切る
  - [x] 書き込みを接続ごとに直列化する（応答と通知が混ざらない）
- [x] JSON-RPC 2.0 の要求・応答・通知・エラーの型を定義する
- [x] バッチ要求（配列）は受け付けず、`-32600` を返す（使う理由が無く、順序の規則を複雑にするため）
- [x] 標準のエラーコード（`-32700`・`-32600`・`-32601`・`-32602`・`-32603`）を返す
- [x] 設計書 14 編 §14.5.1 の表のエラーコード（`-32001`〜`-32013`）と `error.data.kind` を定義する
- [x] `internal/agent/rpc/server.go` にサーバを実装する
  - [x] 受け付けゴルーチンを Transport ごとに 1 本、接続ゴルーチンを接続ごとに 1 本置く（§14.2.4）
  - [x] 1 つの接続の要求を届いた順に処理する
  - [x] `exec.cancel` と `events.poll` は先行する要求の完了を待たずに処理する（別のゴルーチンで受け付ける）
  - [x] 同じ Instance への進行の要求を、接続をまたいで 1 つずつ処理する
- [x] Agent Command の結果に画像（PNG のバイト列）を含められる `Result` 型を定義する。JSON-RPC では Base64 の文字列にする
- [x] `session.hello` を実装する
  - [x] `token` が一致しなければ `unauthorized` を返して接続を切る
  - [x] `session.hello` の前の要求に `unauthorized` を返して接続を切る
  - [x] `api_version` が 1 でなければエラーを返す
  - [x] 結果に `api_version`・`server`（バージョン文字列）・`kind`・`instances` を入れる
- [x] トークンの比較を `crypto/subtle.ConstantTimeCompare` で行う

### 3.9 待ち受けと発見ファイル

- [x] `internal/agent/rpc/listen.go` に待ち受けを実装する
  - [x] `unix`: キャッシュディレクトリの `agent/<pid>.sock` で待ち受ける
  - [x] パスが 104 バイトを超えるときはエラーにして理由を示す
  - [x] 同じパスに古いソケットファイルが残っていれば消してから待ち受ける
  - [x] `tcp:127.0.0.1:PORT` と `tcp:[::1]:PORT`: `PORT` が 0 なら自動で選ぶ
  - [x] `127.0.0.1` と `::1` 以外のアドレスを指定されたら起動を断る
- [x] トークンを 32 バイトの乱数（`crypto/rand`）から 16 進 64 文字で作る
- [x] 発見ファイル `agent/<pid>.json` を書く（`pid`・`endpoint`・`token`・`kind`・`rom`・`started`）
  - [x] パーミッションを 0600 にする
  - [ ] Windows では所有者だけに読み書きを許す ACL を付ける（未実装。Windows の実機で確かめられないため、0600 を指定するだけにしている。Windows 未確認）
  - [x] 一時ファイルに書いてから名前を変える（`internal/emu/atomicfile.go` は非公開の関数のため、`internal/agent/rpc/discover.go` に 0600 で書く同じ手順を置いた）
- [x] ROM を読み込むたびに発見ファイルの `rom` を書き直す
- [x] 正常終了時に発見ファイルとソケットファイルを消す
- [x] `internal/agent/rpc/discover.go` にクライアント側の発見を実装する
  - [x] `agent/*.json` を読み、`pid` のプロセスが無いファイルを消して無視する
  - [x] `--pid` の指定が無いときの選び方を決める（`ctl` は `kind` が `gui` で最も新しいもの、無ければ最も新しいもの）
- [x] Windows で Unix ドメインソケットが使えることをローカルで確認できないため、確認できるまで計画の該当項目に「Windows 未確認」と記す（発見ファイルの ACL と、`TestStartWritesDiscovery` を Windows で飛ばしていることが該当する）

### 3.10 JSON-RPC クライアント

- [x] `internal/agent/rpc/client.go` にクライアントを実装する
  - [x] 接続して `session.hello` を送る
  - [x] 要求と応答を `id` で対応付ける（同時に複数の要求を出せる）
  - [x] 通知を受け取るコールバックを持つ
  - [x] `net.Conn` を受け取って作れるようにする（フェーズ 17 で `net.Pipe` を渡すため）
- [x] プロセス内の Host に `net.Pipe` でつないだクライアントを作る関数を実装する（`shogun run` と `shogun mcp` で使う）

### 3.11 instance と session の Agent Command

- [x] `instance.list` を実装する（ID、ROM 名、フレーム番号、Control の持ち主、進行モード）
- [x] `instance.create` を実装する。引数 `rom`・`state`・`deterministic`・`ram_init`・`ram_seed`
- [x] `instance.close` を実装する。閉じた Instance の Control とイベントキューを捨てる
- [x] GUI 版の Host で `instance.create`・`instance.fork`・`instance.close` が `unsupported_in_gui` を返すことを確かめる

### 3.12 shogun serve

- [x] `cmd/shogun` で第 1 引数が `serve`・`mcp`・`ctl`・`run` のときサブコマンドとして扱う（§14.5.3）。このフェーズでは `serve` と `ctl` を実装し、`mcp` と `run` は「未実装」と表示して終了コード 2 で終える
- [x] 同じ名前の ROM を開くには `./serve` のようにパスで渡すことを `--help` に書く
- [x] `shogun serve [--listen ADDR]` を実装する。headless の Host を作り、待ち受け、シグナル（`SIGINT`・`SIGTERM`）で終える
- [x] 待ち受け先とトークンの発見ファイルのパスを標準エラーに 1 行表示する
- [x] 設定 `agent.listen`・`agent.maxInstances` を設定ファイルに加え、`--listen` で上書きできるようにする（設計書 14 編 §14.23）

### 3.13 shogun ctl

- [x] `shogun ctl [--pid PID] <名前空間> <操作> [引数...]` を実装する（§14.5.3）
- [x] 引数を `params` に変換する
  - [x] `--name value`: 数値に見えるものは数値、`true`/`false` は真偽値、それ以外は文字列
  - [x] `--json '{...}'`: そのまま使い、他の引数で上書きする
  - [x] 位置引数: 登録簿の `Positional` の順に入れる
  - [x] ハイフンを含む引数名（`--max-frames`）をアンダースコア（`max_frames`）に直す
- [x] 名前空間と操作を §14.5.4 の規則で JSON-RPC の名前に直す。登録簿に無ければ候補を示して終了コード 2
- [x] 結果を整形した JSON で出力する。`--text` で人が読む形にする
- [x] 結果の画像を `--out PATH` のときだけファイルに書く。指定がなければ大きさだけ表示する
- [x] エラーを標準エラーに出し、終了コード 1 で終える
- [x] `shogun ctl --help` と `shogun ctl <名前空間> --help` で、登録簿から作った一覧と引数を表示する

### 3.14 テスト

- [x] 登録簿の単体テスト（重複、名前の文字、JSON Schema の生成）
- [x] JSON-RPC の読み書きのテスト（改行区切り、上限超過、不正な JSON、バッチ要求の拒否）
- [x] 振り分けのテスト（未知のメソッド、引数の型の誤り、`control_required`、`instance_required`、`unsupported_in_gui`）
- [x] セキュリティのテスト（§14.24）
  - [x] トークンなしの接続を断る
  - [x] 誤ったトークンの接続を断る
  - [x] `session.hello` の前の要求を断る
  - [x] 発見ファイルのパーミッションが 0600 である（macOS・Linux）
  - [x] `127.0.0.1` と `::1` 以外での待ち受けを断る
- [x] `shogun serve` を起動し、`shogun ctl` から `instance.create` → `instance.fork` → `instance.list` → `instance.close` を行う結合テストを書く（`cmd/shogun` のテストで実行ファイルを作らず、`run` 関数を呼ぶ）
- [x] 接続を切ったときに Control が返ることを確かめるテストを書く
- [x] 2 つの接続が同時に `control.acquire` したとき、一方だけが得ることを確かめるテストを書く

### 3.15 フェーズの締め

- [x] `go test ./...` がローカルの macOS で通ることを確認する（Git を使わない方針のため CI の 3 OS 実行は確認できない。その旨を記す）（macOS で全パッケージが合格。`internal/testrom` は単独で 613 秒かかり、全体を並列に流すと既定の 10 分の上限を超えるため `-timeout 60m` で流した。この所要時間はこのフェーズの変更とは関係なく、CI は `-short` で回している。CI の 3 OS 実行は Git を使わない方針のため未確認）
- [x] 静的検査（`internal/arch` を含む）が通ることを確認する
- [x] 決定論テストが引き続き合格することを確認する（`TestDeterminismHashes` が合格）
- [x] 実装が設計書 14 編と食い違った箇所があれば、先に設計書を直したことを確認する
- [x] `docs/plans/README.md` のフェーズ 14 の状態を「完了」にする

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| 待ち受け | `shogun serve` が `agent/<pid>.sock` で待ち受け、発見ファイルを 0600 で書く |
| 認証 | トークンなし・誤ったトークン・`session.hello` 前の要求がすべて `-32001` で断られる |
| Instance の管理 | `shogun ctl` から `instance.create`・`fork`・`list`・`close` が動作する |
| Fork | Fork した 2 つの Instance を同じだけ進めると状態のハッシュが一致する |
| Control | 2 つの接続が同時に取ろうとすると一方が `control_held` になる。切断で Control が返る |
| 名前の変換 | 登録簿のすべての名前が JSON-RPC・MCP・CLI の間で往復できる |
| 完了通知 | `StepAndWait` が Stop Reason を返し、既存の GUI の操作の挙動が変わらない |
| 依存規則 | `internal/arch` の検査が新しい規則を含めて合格する |
| 決定論 | 決定論テスト 4 ムービーのハッシュが一致する |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| `exec.*`・`obs.*`・`mem.*`・`cpu.*`・`debug.*`・`state.*`・`rom.load` | フェーズ 15 |
| Observation | フェーズ 15 |
| Symbol の再構成、`.dbg`、Game State | フェーズ 16 |
| `shogun mcp`、GUI 版での有効化、Control のバナー、イベント | フェーズ 17 |
| 常時記録、`rom.reload`、Re-Reach、Repro | フェーズ 18 |
| `shogun run`（Scenario） | フェーズ 19 |
| トレースの絞り込み、プロファイル、Diagnostic | フェーズ 20 |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| `StepAndWait` が返ってこない | 進行が打ち切られる経路（ROM の取り外し、リセット、ロード）で `done` へ送っているか確認する |
| GUI のステップ操作が変わった | `done` が `nil` のときに何もしないか確認する |
| macOS でソケットを作れない | パスが 104 バイトを超えている。キャッシュディレクトリの下に置いているか確認する |
| 前回の異常終了の後に待ち受けられない | 古いソケットファイルを消しているか確認する |
| MCP の名前から JSON-RPC の名前に戻せない | アンダースコアを含む名前がある。単純な置き換えでなく登録簿を引く |
| 一覧の順序が実行ごとに変わる | `map` を反復している。スライスを使う |
| Fork した Instance が元と違う動きをする | オーバーレイや `InitState` を複製しているか確認する。往復テストの対象外の状態が無いか確認する |
| 接続ゴルーチンから Machine State に触れてしまう | `WithMachine` かコマンドキューを通す。`go test -race` で確認する |
