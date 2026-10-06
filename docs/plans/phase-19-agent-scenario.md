# フェーズ 19: Scenario とテスト実行

- 作成日: 2026-10-04
- 前提フェーズ: 18
- 完了条件: `shogun run` が合格する Scenario で終了コード 0、失敗する Scenario で終了コード 5 と失敗箇所つきの JUnit XML と Repro を出し、`scenario.export` で書き出した Scenario を `shogun run` で再実行すると同じ結果になる

## 1. 背景

Agent Interface の 2 番目の用途は自動テストである（設計書 14 編 §14.1）。「120 フレーム後に `mode` が `play` になる」「この場面の画面がお手本と一致する」といった確認を headless で CI に流したい。

フェーズ 14〜18 で Agent Command・Game State・式・常時記録を作ってある。Scenario（§14.17）は、Agent Command の並びとアサーションを書いたファイルである。Agent Command の名前と引数を JSON-RPC と同じにするので、エージェントが対話中に試した操作をそのまま Scenario に書き写せる。`scenario.export`（§14.17.4）はこの書き写しを自動で行う。

## 2. 方針とその理由

### 2.1 Scenario の各ステップを Agent Command と同じ名前と引数にする

Scenario 専用の命令を作らない。アサーション（`assert` など）だけを Scenario 固有の要素とする。

理由: 対話中の操作をそのまま保存できる。Agent Command を足せば Scenario でも使える。登録簿（§14.7.1）で引数を検証できる。

### 2.2 Scenario 実行器もプロセス内の Host と `net.Pipe` を通す

`shogun run` は JSON-RPC のクライアントとして、プロセス内の Host に `net.Pipe` で接続する（§14.2.1）。

理由: MCP や `shogun ctl` と同じ経路を通るので、Scenario で合格した操作は他の Transport でも同じ結果になる。

### 2.3 画面の比較をパレットを適用する前の値で行う

お手本の PNG を既定のパレットで書き、比較の前に同じパレットでパレットインデックスとエンファシスへ逆に引いてから比べる（§14.17.2）。

理由: 利用者がパレットの設定を変えてもテストが壊れない。

### 2.4 Scenario の Instance は既定で決定論的にする

`deterministic` の既定値を `true` とする。`timeout_ms` を見つけたら警告する（§14.8.3）。

理由: CI で毎回同じ結果を得る。`timeout_ms` による停止位置は実行環境の速さで変わる。

### 2.5 YAML の読み込みに goccy/go-yaml を使う

`github.com/goccy/go-yaml` を用いる。実装を始める時点で最新版を確認し、`go.mod` に固定する。

理由: 標準ライブラリは YAML を読めない。保守が続いているライブラリを選ぶ。

### 2.6 失敗した Scenario ごとに Repro を書き出す

失敗した時点の Instance で `repro.export` を行い、パスを出力する（§14.17.3）。

理由: CI で失敗したとき、人間が GUI で失敗の場面をそのまま再生できる。

## 3. タスク

### 3.1 依存の追加

- [x] `github.com/goccy/go-yaml` の最新版を確認する（context7 または公式リポジトリ）
- [x] `go.mod` に固定する
- [x] ライセンスを同梱ライセンス一覧（フェーズ 13）に加える
- [x] `internal/arch` の静的検査で、YAML ライブラリを参照するのが `internal/agent/scenario` だけであることを検査する

### 3.2 Scenario ファイルの読み込み

- [x] `internal/agent/scenario` パッケージを作る
- [x] 拡張子 `.yaml`・`.yml`・`.json` で形式を判別する
- [x] `name`・`rom`・`start`・`init`・`symbols`・`gamestate`・`diagnostics`・`steps` を読む（設計書 14 編 §14.17.1）
- [x] パスを Scenario ファイルからの相対パスとして解決する
- [x] `start` に `power-on` と `{state: <パス>}` を受け付ける
- [x] `init` に `ram_init`・`ram_seed`・`deterministic` を受け付け、`deterministic` の既定値を `true` とする
- [x] `symbols` を省略したとき、ROM と同じ名前の `.dbg` を探す
- [x] `diagnostics` の `enable`（`all` または種類の列）と `fail_on` を読む
- [x] `steps` の各要素が、Agent Command の名前をキーとする要素かアサーションかを判別する
- [x] Agent Command の引数を登録簿のスキーマで検証する
- [x] 登録簿に無い名前、`ClassSession` の名前（`session.hello` など）をエラーにする
- [x] エラーにファイル名・行番号・ステップの番号を含める
- [x] `timeout_ms` を含むステップを見つけたら警告する

### 3.3 アサーション

- [x] `assert: <式>` を実装する（設計書 14 編 §14.11.4 の式）
- [x] `assert_stop: <Stop Reason>` を実装する。直前の進行の Stop Reason と比べる
- [x] 直前に進行が無いときの `assert_stop` をエラーにする
- [x] `assert_screen: {golden, max_diff_pixels}` を実装する
- [x] お手本の PNG を既定のパレットでパレットインデックスとエンファシスへ逆に引く
- [x] 逆に引けない色（既定のパレットに無い RGB）を含むお手本をエラーにする
- [x] 一致しなかった画素の数と、最初の画素の位置を失敗の内容に含める
- [x] 失敗したとき、実際の画面を `<golden 名>.actual.png` として書く
- [x] `assert_mem: {loc, equals}` を実装する
- [x] `assert_no_diagnostics: [種類...]` を実装する。Scenario の開始からの Diagnostic を数える
- [x] 種類を省略したときすべての種類を対象にする
- [x] `diagnostics.fail_on` の種類を検知したら、その時点で失敗にする
- [x] 失敗の内容に、式・実際の値・Observation の要約を含める

### 3.4 shogun run

- [x] `cmd/shogun` に `run` サブコマンドを加える（§14.5.3）
- [x] 複数の Scenario ファイルを順に実行する
- [x] Scenario ごとに Instance を作り、終わったら閉じる
- [x] プロセス内の Host に `net.Pipe` で接続する（方針 2.2）
- [x] 標準出力に Scenario ごとの成否と所要時間を出す
- [x] 失敗したとき、ステップの番号・内容・式・実際の値・Observation の要約を出す
- [x] `--junit PATH` で JUnit XML を書く
- [x] Scenario ファイル 1 つを `testsuite`、Scenario を `testcase` とする
- [x] 失敗時に `failure` へステップの番号と内容を入れる
- [x] 不正なファイルは `error` として JUnit XML に入れる
- [x] 失敗した Scenario ごとに `repro.export` を行い、パスを出力する
- [x] `--update-golden` で、お手本の PNG が無いか一致しないとき現在の画面で上書きする
- [x] `--update-golden` で書き換えたファイルの一覧を出力する
- [x] 終了コードを決める（0: 合格、5: アサーションの失敗、6: Scenario ファイルの不正、1: ROM の読み込み失敗）
- [x] 設計書 11 編 §11.5.2 の終了コードの表に 5 と 6 が追記されていることを確認する（無ければ先に設計書を直す）
- [x] `--help` に `run` の使い方を加える

### 3.5 scenario.mark と scenario.export

- [x] `scenario.mark` を登録簿に加える（`ClassConfig`、§14.17.4）
- [x] 印を付けた時点のセーブステートを Instance の中に保持する（最新の 1 つだけ）
- [x] 印を付けた位置を Agent Command の記録（フェーズ 18 の 3.8）に残す
- [x] `scenario.export` を登録簿に加える（`ClassObserve`）
- [x] Agent Command の記録から `ClassAdvance` と `ClassMutate` のものを順に並べる
- [x] `from: start` のとき Instance の開始から書き出す
- [x] `from: mark` のとき印の位置から書き出す
- [x] 開始が電源投入でないとき、その時点のセーブステートを Scenario と同じディレクトリに書き、`start: {state: …}` とする
- [x] `rom`・`init`・`symbols`・`gamestate` を Instance の設定から埋める
- [x] パスを書き出し先からの相対パスにする
- [x] YAML で書き出す（拡張子が `.json` なら JSON）
- [x] アサーションを含めない

### 3.6 テスト

- [x] テスト用の ROM（フェーズ 16 の `testdata/agent/`）で合格する Scenario を書く
- [x] 終了コード 0 になることを確認する
- [x] 式のアサーションが失敗する Scenario を書き、終了コード 5 と失敗の内容を確認する
- [x] 画面のアサーションが失敗する Scenario を書き、`.actual.png` が書かれることを確認する
- [x] Diagnostic で失敗する Scenario（`fail_on`）を書く
- [x] 不正な Scenario（存在しない Agent Command、引数の型の誤り）で終了コード 6 になることを確認する
- [x] JUnit XML を XML として読み直し、`testsuite`・`testcase`・`failure` の構成を確認する
- [x] 失敗時に Repro が書き出され、その SHGM を再生できることを確認する
- [x] `--update-golden` でお手本が書かれ、2 回目の実行で合格することを確認する
- [x] パレットの設定を変えても画面のアサーションが合格することを確認する
- [x] Instance で操作した後に `scenario.export` で書き出し、`shogun run` で再実行して同じ Observation になることを確認する
- [x] `from: mark` で書き出した Scenario が印の時点のセーブステートから始まることを確認する
- [x] 同じ Scenario を 2 回実行し、結果が一致することを確認する（決定論）

### 3.7 CI での使い方の文書化

- [x] 設計書 12 編に Scenario によるテストの節が追記されていることを確認する（無ければ先に設計書を直す）
- [x] 利用者向けの文書に `shogun run` の使い方を書く（ファイル例・アサーション・終了コード・JUnit XML）
- [x] GitHub Actions で `shogun run tests/*.yaml --junit report.xml` を実行し、結果を表示する例を書く
- [x] お手本の PNG を `--update-golden` で作り、リポジトリに含める手順を書く
- [x] 失敗時の Repro を CI の成果物として保存する例を書く
- [x] エージェントが `scenario.export` で書き出した Scenario にアサーションを足す手順を書く

### 3.8 フェーズの締め

- [ ] `go test ./...` が 3 OS で通ることを確認する（Git を使わない方針のため CI を持たない。ローカルの macOS でのみ確認した）
- [x] 決定論テストが引き続き合格することを確認する
- [x] 実装と設計書 11 編・12 編・14 編の食い違いが無いことを確認する
- [x] `docs/plans/README.md` のフェーズ 19 の状態を「完了」にする

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| 合格 | 合格する Scenario で終了コード 0 |
| 失敗 | 失敗する Scenario で終了コード 5、失敗のステップと実際の値が出力されること |
| JUnit XML | 失敗箇所を含む JUnit XML が XML として正しいこと |
| 失敗時の Repro | 失敗した Scenario の Repro を GUI で再生できること |
| 不正なファイル | 終了コード 6 とファイル名・行番号つきのエラー |
| scenario.export | 書き出した Scenario を再実行して同じ Observation になること |
| 決定論 | 同じ Scenario を 2 回実行して結果が一致すること |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| トレース・プロファイル・Diagnostic の検知の仕組み | フェーズ 20（このフェーズでは既存の Diagnostic の報告を使う） |
| 組み込みスクリプト言語（Lua・JavaScript） | 扱わない（設計書 14 編 §14.1） |
| 画面の部分一致・あいまい比較 | 扱わない。`max_diff_pixels` で許容する |
| Scenario の並列実行 | 扱わない。必要になれば `shogun run` を複数起動する |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| 画面のアサーションが環境によって失敗する | RGB で比べている。パレットインデックスでの比較を確認する |
| パレットを変えるとお手本が逆に引けない | お手本を既定のパレットで書いているか確認する |
| CI でだけ失敗する | `timeout_ms` を使っている。警告を確認する。`deterministic` が `false` になっていないか確認する |
| 書き出した Scenario が再実行で違う場面に着く | 観測だけの Agent Command で状態が変わっている。`ClassObserve` が Machine State を変えないことを確認する |
| `scenario.export` に余計なステップが入る | 分類の誤り。登録簿の `Class` を確認する |
| Scenario ファイルのパスが解決できない | 作業ディレクトリからの相対パスで解決している。Scenario ファイルからの相対パスを確認する |

## 7. 記録

| 判断 | 内容 | 理由 |
|---|---|---|
| goccy/go-yaml の版 | v1.19.2（2026-10-05 時点の最新。`go list -m -versions` で確認） | 方針 2.5。ライセンス一覧は `tools/package` が依存から自動で作るため、手で加えるものは無い |
| `assert` の評価 | Agent Command `expr.eval`（Observe）を加え、実行器はそれを JSON-RPC で呼ぶ | 方針 2.2（実行器も JSON-RPC だけを通す）。エージェントが条件を試すのにも使える |
| `gamestate` の読み込み | Agent Command `gamestate.load`（Config）を加える | Scenario の `gamestate` に対応する Agent Command が無かった |
| 画面の比較の実装 | `obs.screenshot`（拡大率 1、常に既定のパレット）の RGB とお手本の RGB を比べ、お手本の色が既定のパレットの 512 色に含まれることを確かめる | パレットインデックスで比べることと同じ結果になり、emu に新しい口が要らない。既定のパレットで同じ色になる値（黒）は同じとみなす |
| 失敗の実際の値 | `assert` の式を `&&`・`||` と比較演算子で分け、リテラルでない側を `expr.eval` で評価して示す | 式全体の値（0）だけでは何が違うか分からない |
| `ram_init: random` の決定論 | `deterministic` が真でシードを省いたときは 1 にする。`scenario.export` はシードが無いと知らせる | 省くと実行のたびに時刻からシードを作っていた（テストで見つけた） |
| `scenario.export` の開始 | 作成（`state` や Fork なら作った時点のセーブステート）、`state.load`・`rom.load`・`rom.reload` の直後を開始に置き直す | 名前付きの保管場所や前の ROM は Scenario から参照できない |
| ブレークポイントの書き出し | `debug.bp.add`・`remove`・`enable` は `ClassConfig` だが書き出す | 進行の止まる位置を変える |
| `scenario.export`・`scenario.mark` の GUI | headless だけで使える | GUI では人間の入力が Agent Command の記録に残らない |
| `exec.run`・`exec.pause` | 書き出しで特別に扱わない | GUI 版だけの Agent Command で、headless の記録に現れない |
| 終了コードの優先 | 6 → 1 → 5 → 0。不正なファイルがあっても残りを実行する | CI で一度にすべての問題を見せる |
| Diagnostic | 検知はフェーズ 20。実行器は `diagnostic` イベントを集めて `fail_on` と `assert_no_diagnostics` に使う。`diag.configure` が登録されていれば `enable` を渡す | フェーズ 20 で実装を変えずに使えるようにする。`fail_on` はイベントを直接積むテストで確かめた |
| `--repro-dir` | 追加（設計書 11 編・14 編に追記） | CI の成果物として Repro を集める |

| 確認 | 結果 |
|---|---|
| `internal/agent/scenario` のテスト | 合格（合格・失敗・画面・不正なファイル・JUnit・fail_on・決定論・export の往復と from: mark・state.load の後の export） |
| `cmd/shogun` の `shogun run` のテスト | 合格（終了コード 0・5・6、JUnit XML、`--update-golden`、Repro、パレットの設定を変えても合格） |

| 残り | 内容 |
|---|---|
| 3 OS の CI | Git を使わない方針のため、macOS でだけ確認した |
| Diagnostic で実際に失敗する Scenario | フェーズ 20 で検知を作ってから、テスト ROM で確かめる |
| Repro の GUI での再生の目視 | 通常のエミュレータでムービーを再生できることはテストで確かめた。GUI での目視はしていない |
