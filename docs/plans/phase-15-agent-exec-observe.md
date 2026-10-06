# フェーズ 15: 進行と観測

- 作成日: 2026-10-04
- 前提フェーズ: 14
- 完了条件: `exec.step`・`exec.input_sequence`・`exec.run_until`・`exec.step_unit` が Stop Reason と Observation を返し、`obs.*`・`mem.*`・`cpu.*`・`debug.*`・`state.*`・`rom.load` が動作する。同じ Agent Command の列を 2 回実行したときと、Fork した 2 つの Instance に同じ列を与えたときに、すべての Observation（画像を含む）が一致する

## 1. 背景

フェーズ 14 で、Agent Command を届ける経路（JSON-RPC、`shogun ctl`）と Instance の管理ができた。このフェーズでは、エージェントがエミュレータを実際に動かし、様子を見て、書き換えるための Agent Command を載せる。

エージェントは LLM であることが多く、1 往復に数秒かかり、1 回の応答に含められる情報の量にも限りがある。そのため、進行の要求は「止まった理由と、前回から何が変わったか」を 1 回の応答で返す（設計書 14 編 §14.8、§14.9）。画面の画像は必要なときだけ付け、PPU の状態は数値と格子で渡す（§14.10）。LLM は画像からピクセルの位置と色を正確に読めないためである。

Game State（§14.12）はフェーズ 16 で作る。このフェーズの Observation はウォッチの値の差分までを扱い、Game State の差分を差し込む口だけを用意しておく。

## 2. 方針とその理由

### 2.1 進行の Agent Command は既存の実行制御の上に作る

`exec.step` と `exec.input_sequence` はフレーム単位の `StepAndWait`（フェーズ 14）を繰り返して作る。`exec.step_unit` は既存の `StepKind`（設計書 09 編 §9.5）をそのまま使う。

理由: ブレークポイントでの停止、サイクル単位ステップ、命令の途中での停止の扱いは、フェーズ 10 で作り込んで検証済みである。新しい実行経路を作ると、これらの検証をやり直すことになる。

### 2.2 入力はムービーと同じラッチ点で与える

`exec.step` の入力は、各フレームの開始時（ムービーのフレームと同じ位置）に取り込む。キーボードの入力は Agent-Paced の間は使わない。

理由: 入力の取り込み位置をムービーと揃えておけば、フェーズ 18 の常時記録で同じ入力を同じ位置に再生でき、決定論が保たれる（設計書 14 編 §14.22）。

### 2.3 `run_until` の判定は既定でフレームの開始ごとに行う

`check: instruction` を指定したときだけ、条件付きブレークポイントと同じ仕組みで命令ごとに判定する。

理由: 命令ごとの判定は 1 秒あたり 30 万回の式の評価になり、遅い。エージェントが待つ場面の多くは画面の切り替わりなどフレーム単位の変化であり、フレームごとの判定で足りる。

### 2.4 Observation の差分を接続ごとに持つ

前回の観測値を `Instance.observed[connID]` に持つ。

理由: 人間が `shogun ctl` で覗いただけでエージェントの差分が消えると、エージェントは変化を見落とす（設計書 14 編 §14.9.2）。

### 2.5 構造化された観測は格子を文字列で返す

ネームテーブルの `tiles` は 1 行を 16 進 2 桁と空白で並べた文字列とし、30 行の配列にする（§14.10.2）。

理由: JSON の配列の配列にすると、LLM が格子として読みにくく、トークンも数倍になる。

### 2.6 PPU 書き込みの記録とフックは必要なときだけ有効にする

`obs.ppu_writes` が要求されたときに `OnCPUWrite` フックを設定し、600 フレームのあいだ要求が無ければ外す（§14.10.5）。Freeze のフックも Freeze が 1 件以上あるときだけ設定する。

理由: `OnCPUWrite` は毎秒 180 万回呼ばれる。使わないときにフックを `nil` に保つ方針（設計書 09 編 §9.6）を Agent Interface でも守る。

### 2.7 Freeze の書き戻しは書き込みの直後に行う

Freeze した位置への CPU の書き込みを `OnCPUWrite` で検知し、その命令が終わった命令境界で値を書き戻す（§14.13.2）。

理由: フレームの開始時にだけ書き戻す方法では、フレームの途中で書かれた値をプログラムが読めてしまい、固定にならない。

## 3. タスク

### 3.1 入力の書き方

- [x] `internal/agent/input.go` に入力の文字列を解釈する関数を実装する（設計書 14 編 §14.8.5）
  - [x] `""` を何も押さない状態にする
  - [x] `A`・`B`・`Select`・`Start`・`Up`・`Down`・`Left`・`Right` を解釈する
  - [x] `U`・`D`・`L`・`R` の略記を解釈する
  - [x] `+` でつないだ同時押しを解釈する
  - [x] `$81` のビットマスクを解釈する（bit 0 から A・B・Select・Start・Up・Down・Left・Right）
  - [x] 大文字・小文字を区別しない
  - [x] 上下・左右の同時押しをそのまま通す（`input.allowOpposingDirections` に従わない）
  - [x] 不正な文字列で、位置を含むエラーを返す
- [x] 各書き方のテストを書く

### 3.2 Agent-Paced の入力経路

- [x] Agent-Paced の Instance で、キーボードの入力（`InputState`）を無視する経路を `internal/emu` に加える
- [x] 進行の要求ごとにポート 1 と 2 の入力を設定し、フレームの開始時（設計書 08 編 §8.7.1 のラッチ点）に取り込む
- [x] 連射（設計書 07 編 §7.5）を Agent-Paced では適用しない
- [x] Agent-Paced から Real-Time に戻ったとき、キーボードの入力経路に戻す

### 3.3 exec.step と exec.input_sequence

- [x] `exec.step` を実装する（§14.8.1）
  - [x] 引数 `frames`（1–36000）・`input`・`input2`・`observe`・`break`
  - [x] `frames` を範囲外にしたら `-32602` を返す
  - [x] `break: false` のとき、ブレークポイントと Diagnostic で止まらない
  - [x] 応答に `stop_reason` と Observation を含める
- [x] `exec.input_sequence` を実装する（§14.8.2）
  - [x] 要素を順に流す
  - [x] 途中で止まったら、そこで終えて `completed_steps` を返す
  - [x] `observe_each: true` のとき、各要素の終わりの要約（画像を除く）を `per_step` に並べる
  - [x] 全要素を流し終えたら `stop_reason: sequence_done`
- [x] 1 フレームの定義が「次のフレームの開始（scanline 0 / dot 0 を過ぎた最初の命令境界）まで」であることをテストで確かめる

### 3.4 exec.run_until

- [x] `exec.run_until` を実装する（§14.8.3）
  - [x] 引数 `condition`・`max_frames`（既定 600、1–216000）・`check`・`input`・`input2`・`timeout_ms`
  - [x] 条件式は既存の条件式（設計書 09 編 §9.6.1）で解析する。式の拡張はフェーズ 16 で行う
  - [x] 解析エラーで `invalid_expression` と `error.data.position` を返す
  - [x] `check: frame` でフレームの開始ごとに判定する
  - [x] `check: instruction` で条件付きブレークポイントと同じ仕組みで判定し、終わったら外す
  - [x] 開始時点ですでに条件が成り立っていても、少なくとも 1 フレーム（`check: instruction` なら 1 命令）進めてから判定する
  - [x] 成り立ったら `stop_reason: condition` と式を返す
  - [x] `max_frames` に達したら `stop_reason: max_frames`
  - [x] `timeout_ms` に達したら `stop_reason: timeout`
- [x] 条件が成り立つフレームで正しく止まることを確かめるテストを書く（テスト用 ROM の RAM の値が変わるフレームを使う）

### 3.5 exec.step_unit・exec.reset・exec.cancel

- [x] `exec.step_unit` を実装する。`unit` に `cycle`・`instruction`・`over`・`out`・`scanline`、`count` に回数、`to` に `RunToCursor` の位置を渡せるようにする
- [x] 終わったら `stop_reason: step_done`
- [x] `exec.reset` を実装する。`hard: true` で電源の入れ直し
- [x] `exec.cancel` を実装する
  - [x] 実行中の進行要求の `ctx` を取り消す
  - [x] 取り消された要求は `stop_reason: cancelled` の結果を返す（エラー `cancelled` にしない。止まった位置の Observation をエージェントが受け取れるようにするため）
  - [x] 実行中の要求が無ければ何もせず成功を返す
- [x] `run_until` の実行中に別の要求として `exec.cancel` を送り、止まることを確かめるテストを書く

### 3.6 Stop Reason

- [x] `internal/agent/stop.go` に Stop Reason の名前（§14.8.4 の表の全 11 種）を定義する
- [x] `emu.StepResult` の停止理由から Stop Reason への変換を実装する
- [x] `breakpoint` にブレークポイントの ID・種別・位置を添える
- [x] `cpu_halted` を STP の実行で返す
- [x] `control_lost` を返す口を作る（人間の取り返しはフェーズ 17 で実装する）
- [x] `diagnostic` を返す口を作る（Diagnostic はフェーズ 20 で実装する）

### 3.7 Observation の要約

- [x] `internal/agent/observe.go` に Observation の型を定義する（§14.9.1）
  - [x] `instance`・`frame`・`stop_reason`・`stop_detail`・`cpu`・`gamestate_changed`・`watch`・`diagnostics`・`events_pending`・`notes`
  - [x] 空のフィールド（ウォッチが無い、Diagnostic が無い）を省く
- [x] `cpu` に PC と、命令の途中で止まっているかを入れる。Symbol とソース行は、この時点では既存のラベルだけを添える（ソース行はフェーズ 16）
- [x] Observation を作る処理をエミュレーションゴルーチンの `WithMachine` の中で行い、値のコピーだけを取り出す
- [x] `obs.get` を実装する。`full: true` で差分ではなく全項目の現在値を返す

### 3.8 差分

- [x] `observedValues` を定義する。ウォッチと Game State の前回値を持つ
- [x] 接続 ID と Instance の組ごとに前回値を持つ（§14.9.2）
- [x] 初めての Observation では、すべての値を `{"from": null, "to": …}` で返す
- [x] ウォッチの値の差分を返す
- [x] Game State の差分を差し込む口を用意する（`gamestate_changed` を作る関数を空で置く。フェーズ 16 で埋める）
- [x] 接続が切れたら、その接続の前回値を捨てる
- [x] 接続 A の観測が接続 B の差分を変えないことを確かめるテストを書く

### 3.9 付ける内容の選択

- [x] `observe.include` を実装する（§14.9.3）
  - [x] `image`: 画面の PNG。`observe.scale` 1–4（既定は `agent.observeImageScale`、その既定値 2）
  - [x] `gamestate`: この時点では空のオブジェクトを返す（フェーズ 16 で埋める）（この時点では値が無いため省く）
  - [x] `cpu_full`: `cpu.get` の結果
  - [x] `sprites`・`nametable`・`ppu_writes`: 各 `obs.*` の結果
- [x] `observe: false` で `frame` と `stop_reason` だけを返す
- [x] 画像の拡大は最近傍法で行う（ぼかすとタイルの境界が読めなくなる）
- [x] 画像はパレットを適用した RGB で作る（既存の `internal/video/png.go` を使う）

### 3.10 構造化された観測

- [x] `obs.screenshot` を実装する。`scale` 1–4
- [x] `obs.sprites` を実装する（§14.10.1）
  - [x] `size`（`8x8`・`8x16`）
  - [x] 各スプライトの `index`・`x`・`y`（OAM の値）・`screen_y`・`tile`・`palette`・`priority`・`flip_h`・`flip_v`・`sprite0`・`drawn`
  - [x] `lines_over_8`
  - [x] Y が `$EF` 以上のスプライトを既定で省き、`hidden_count` に数を入れる。`all: true` で 64 個すべて
  - [x] 値は既存のスナップショット（設計書 09 編 §9.3）から取る
- [x] `obs.nametable` を実装する（§14.10.2）
  - [x] `table` 0–3 を指定したとき、その面の 30 行 × 32 タイルの文字列と 15 行 × 16 区画の属性
  - [x] `base`・`mirroring`・`scroll`
  - [x] `table` を省略したとき、フレームの開始時のスクロール位置から見える範囲（2 面にまたがればつなぐ）
  - [x] フレームの途中でスクロールが変わったことを PPU 書き込みの記録から検知できたときは `notes` に入れる
- [x] `obs.patterns` を実装する（§14.10.3）
  - [x] `format: hash`（既定）: 各タイルの 16 バイトの SHA-1 の先頭 8 桁。全 0 のタイルは `null`
  - [x] `format: image`: 2 面を並べた PNG。`palette` で適用するパレット
  - [x] `format: pixels`: `tiles` で指定したタイルの 8 行の色番号の文字列
- [x] `obs.palette` を実装する（§14.10.4）。背景 4 組とスプライト 4 組、各エントリに色番号と RGB
- [x] `obs.apu` を実装する。既存の `Inspect`（設計書 05 編 §5.9）の値を返す

### 3.11 PPU 書き込みの記録

- [x] `internal/debug/ppulog.go` に記録を実装する（§14.10.5）
  - [x] 対象を `$2000`・`$2001`・`$2003`・`$2005`・`$2006`・`$4014` とする（ミラーを畳んだアドレスで判定する）
  - [x] 各書き込みに `reg`・`value`・`scanline`・`dot`・`pc` を記録する
  - [x] 1 フレームにつき 512 件までとし、超えたら `truncated: true`
  - [x] フレームの完成時に記録を入れ替え、直前に完成したフレームの記録を返す
- [x] `obs.ppu_writes` を実装する
  - [x] 要求されたときにフックを設定する
  - [x] 最後の要求から 600 フレームたったらフックを外す
  - [x] 有効にした直後のフレームの記録に `complete: false` を付ける
- [x] 既存の `Hooks` の設定（`updateHooks()`）に PPU 書き込みの記録の要否を加える
- [x] 記録を有効にしても決定論テストのハッシュが変わらないことを確かめる

### 3.12 mem と cpu

- [x] この時点の位置の指定は、CPU アドレス（`$0300`・`0x0300`・`768`）、既存のラベル、空間の接頭辞（`ppu:`・`oam:`・`pal:`・`chr:`・`prg:`）を受け付ける。Symbol の式と `bankN:` はフェーズ 16 で加える
- [x] `mem.read` を実装する。`loc` と `length`（1–4096）。`Bus.Peek` などの副作用のない読み出しを使う
- [x] 結果を 16 進の文字列（16 バイトごとに区切る）と数値の配列の両方で返す
- [x] `mem.write` を実装する
  - [x] CPU アドレス空間には `Bus.Poke`、`side_effects: true` で `Bus.Write`
  - [x] 他の空間には既存の `Emulator.Poke`（設計書 09 編 §9.4.8）
  - [x] 値に数値とバイト列を受け付ける
  - [x] 命令境界で書く
- [x] `mem.find` を実装する。バイト列を探し、最大 100 件の位置を返す
- [x] `cpu.get` を実装する。レジスタ、P の `NV-BDIZC` 表記、PPU の位置、累積サイクル数、保留中の割り込み
- [x] `cpu.set` を実装する。命令境界でレジスタを書き換える
- [x] `cpu.disasm` を実装する。既存の逆アセンブラ（設計書 09 編 §9.4.6）を使い、推定かどうか、実効アドレスの注記、ラベルを添える
- [x] `cpu.callstack` を実装する。既存のコールスタックを返し、推定である旨を含める

### 3.13 Freeze

- [x] `internal/agent/freeze.go` に `Freeze` を定義する（§14.13.2）（フックを持つ `internal/debug/freeze.go` に置いた。設計書 14 編 §14.13.2 に追記）
- [x] `mem.freeze` を実装する
  - [x] 対象を `$0000`–`$07FF` と `$6000`–`$7FFF` に限る。それ以外は `invalid_location`
  - [x] `size` 1–4 バイト
  - [x] 設定した時点で値を書く
  - [x] Instance あたり 64 件を上限とし、超えたら `limit_exceeded`
- [x] `OnCPUWrite` で Freeze した位置への書き込みを検知し、その命令が終わった命令境界で値を書き戻す（書き込みの直後に書き戻す方式にした。命令境界まで待つと、書き込んだ命令の直後に止まったときに固定していない値が観測される。設計書 14 編 §14.13.2 を更新）
- [ ] RMW 命令の中では書いた値が見えることをテストで確かめる（書き込みの直後に書き戻す方式にしたため、命令の中で書いた値を外から観測する手段が無い。RMW 命令は読んだ値を CPU の中で持つため計算には影響しない。`INC $10` を Freeze した位置に行う NMI で値が固定されることは `TestFreeze` で確かめた）
- [x] `mem.unfreeze` と `mem.freezes` を実装する
- [x] Freeze が 0 件のとき、Freeze のためのフックが `nil` であることを確かめる
- [x] Freeze をセーブステートに含めないことを確かめる
- [x] Fork で Freeze を複製する（フェーズ 14 で作った複製の関数を埋める）

### 3.14 debug.bp と debug.watch

- [x] `debug.bp.add` を実装する。種別（`exec`・`read`・`write`・`ppu`・`event`）、位置、範囲、条件、有効
- [x] ブレークポイントに Instance 内で一意な ID を振り、結果で返す
- [x] `debug.bp.remove`・`debug.bp.list`・`debug.bp.enable` を実装する
- [x] `debug.watch.add`・`debug.watch.remove`・`debug.watch.list` を実装する
- [x] ウォッチの値を Observation の `watch` に入れる
- [x] Agent Command で追加したブレークポイントとウォッチを、既存の `symbols/<rom-hash>.json` に保存する（GUI で追加したものと同じ扱い）

### 3.15 state と rom.load

- [x] `state.save` を実装する
  - [x] `path` を指定したらファイルに書く
  - [x] `name` を指定したら Instance 内の名前付きの保管場所に置く
  - [x] どちらも無ければ `auto-<フレーム番号>` の名前で保管する
  - [x] 命令の途中で止まっているときは、命令を完了させてから保存し、`notes` に入れる（設計書 08 編 §8.3.2）
- [x] `state.load` を実装する。`path` または `name`
- [x] `state.list` を実装する。名前・フレーム番号・作った時刻
- [x] 名前付きの保管場所は Instance を閉じたら捨てる（ファイルに書かない）
- [x] `rom.load` を実装する。読み込んだ後は一時停止した状態にする
- [x] ROM の読み込みに失敗したら `io_error` と理由を返す

### 3.16 ムービーとの関係

- [x] Instance がムービーを再生している間、`exec.step` などの入力指定と `ClassMutate` の Agent Command を `movie_conflict` で断る（§14.7.3）
- [x] 入力を指定しない進行と観測はできることを確かめる

### 3.17 決定論テスト

- [x] テスト用の Agent Command の列を作る（`exec.step`・`exec.input_sequence`・`exec.run_until`・`mem.write`・`cpu.set`・`mem.freeze`・`state.save`・`state.load` を含む）
- [x] 同じ列を 2 回実行し、各 Observation（画像を含む）が一致することを確かめる
- [x] Fork した 2 つの Instance に同じ列を与え、各 Observation が一致することを確かめる
- [x] ブレークポイント・ウォッチ・PPU 書き込みの記録・Freeze を有効にしても、既存の決定論テスト 4 ムービーのハッシュが変わらないことを確かめる（ブレークポイントとトレースは既存の `TestMovieUnchangedByDebugger` が有効にしている。PPU 書き込みの記録をそこへ加えた。ウォッチはフックを使わない。Freeze は値を書き換える機能であり、設計書 12 編 §12.8.3 のとおり含めない）

### 3.18 性能の確認

- [x] headless で `exec.step {frames: 3600}` の所要時間を計測する
- [x] `observe.include: [image]` を毎フレーム付けたときの 1 往復の所要時間を計測する
- [x] ウォッチ 32 件と Freeze 16 件を置いたときの `exec.step` の所要時間を計測する
- [x] 計測した値を、このファイルの末尾に記録する（§7）

### 3.19 フェーズの締め

- [x] `go test ./...` がローカルの macOS で通ることを確認する（Git を使わない方針のため CI の 3 OS 実行は確認できない。その旨を記す）
- [x] 静的検査が通ることを確認する
- [x] 決定論テストが引き続き合格することを確認する
- [x] 実装が設計書 14 編と食い違った箇所があれば、先に設計書を直したことを確認する
- [x] `docs/plans/README.md` のフェーズ 15 の状態を「完了」にする

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| 進行 | `exec.step`・`input_sequence`・`run_until`・`step_unit` が §14.8.4 の Stop Reason を返す |
| 条件で止まる | `run_until` がテスト用 ROM の RAM の値が変わるフレームで止まる |
| キャンセル | `run_until` の実行中に `exec.cancel` を送ると `stop_reason: cancelled` で返る |
| 差分 | 2 つの接続の差分が互いに影響しない |
| 構造化された観測 | `obs.sprites`・`nametable`・`patterns`・`palette`・`ppu_writes`・`apu` が §14.10 の形で返る |
| Freeze | フレームの途中の書き込みの後も値が固定される。0 件のときフックが `nil` |
| ムービーとの関係 | 再生中に入力指定と書き込みが `movie_conflict` で断られる |
| 決定論 | 同じ列の 2 回の実行と、Fork した 2 つの Instance で、すべての Observation が一致する |
| 既存の決定論 | 決定論テスト 4 ムービーのハッシュが一致する |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| Symbol の式、`bankN:`、`.dbg`、ソース行 | フェーズ 16 |
| Game State と `gamestate_changed` | フェーズ 16 |
| MCP、GUI 版の Control のバナー、`exec.run`・`exec.pause`、イベント | フェーズ 17 |
| 介入の常時記録、`rom.reload` | フェーズ 18 |
| Scenario | フェーズ 19 |
| `trace.*`・`profile.*`・`diag.*` | フェーズ 20 |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| `exec.step {frames: 1}` で進むフレーム数がずれる | フレームの数え方がムービーのフレームと一致しているか確認する（設計書 08 編 §8.7.1） |
| 同じ列で Observation が一致しない | キーボードの入力や連射が Agent-Paced で混ざっていないか確認する |
| `run_until` が開始直後に止まる | 開始時点で条件が成り立っている。少なくとも 1 フレーム進めてから判定しているか確認する |
| Freeze しても値が変わる | フレームの開始時にだけ書き戻している。書き込みのフックで書き戻しているか確認する |
| ネームテーブルの表示が画面とずれる | フレームの途中でスクロールを変えている。PPU 書き込みの記録を見る |
| メモリを読むとゲームの挙動が変わる | `Bus.Read` を呼んでいる。`Peek` を使う |
| 画像のタイルの境界がぼける | 拡大に補間を使っている。最近傍法にする |
| 毎フレーム画像を付けると遅い | 必要なときだけ付ける。既定では付けない |

## 7. 性能の計測結果

2026-10-05、macOS（Apple Silicon）、テスト用 ROM（NMI ごとにコントローラを読み OAM DMA を行うもの）で計測した。`internal/agent/bench_test.go`。

| 計測 | 結果 | 補足 |
|---|---|---|
| `exec.step {frames: 3600}`（観測なし） | 6.70 秒（約 537 fps） | `shogun --headless --frames 3600` も 6.68 秒であり、Agent Interface による上乗せは無い。速さはエミュレーションコアで決まる |
| `exec.step {frames: 1}` に `include: [image]`（拡大率 2） | 1 往復 4.5 ミリ秒 | PNG の作成を含む |
| ウォッチ 32 件と Freeze 16 件で `exec.step {frames: 60}` | 109 ミリ秒 | 何も置かないとき 132 ミリ秒。差は計測の揺れの範囲であり、Freeze の書き込みフックによる遅れは見えない |
