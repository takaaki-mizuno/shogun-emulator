# フェーズ 7: DMA の精密化

- 作成日: 2026-09-21
- 前提フェーズ: 6
- 完了条件: `read_joy3`・`dmc_dma_during_read4/dma_2007_write`・`read_write_2007`・`cpu_interrupts_v2/4-irq_and_dma`・`dpcmletterbox` が合格する

## 1. 背景

OAM DMA と DMC DMA は CPU を停止させる。フェーズ 5 までは `$4014` への書き込みを無視していたため、OAM DMA を使うゲーム（ほとんどのゲーム）でスプライトが表示されない。ここで実装する。

DMC DMA には実機由来の副作用がある。CPU が停止している間、停止時に読んでいたアドレスを繰り返し読む。`$4016` を読んでいた場合、コントローラのシフトレジスタが余分にクロックされてレポートから 1 bit が失われる。症状は「Right が勝手に押される」である。**この挙動を実装しないと、回避策を持つゲームが正しく動かない。**

## 2. 方針とその理由

### 2.1 DMA を CPU の外側に置く

`bus.DMA` として実装し、`Bus.Read` の先頭で処理する。CPU のコードに DMA の知識を入れない。

理由: DMA は CPU を停止させる外部の仕組みである。CPU の中に入れると、命令ごとのサイクル列に DMA の分岐が混ざり、フェーズ 1・2 で通したテストが読みにくくなる。

### 2.2 get / put の位相を APU のクロックから取る

APU クロックの前半を get、後半を put とする。バスが別にカウンタを持たない。

理由: get/put は APU のクロックそのものである。別に数えると、同じクロックのはずの 2 つが食い違う。判定は「これから実行するサイクル」について行う。APU の位相は直前に進めたサイクルのものであるため反転して使う。ここを 1 サイクル間違えると DMC DMA の長さが 3 と 4 で入れ替わり、`read_joy3/thorough_test` と `dmc_dma_during_read4/dma_2007_write` が通らなくなる。

### 2.3 レジスタ競合を「停止中に再度 `read` を呼ぶ」形で実装する

コントローラのデバイス側に特別な処理を置かない。`Bus.read` を再度呼ぶことで、`StandardController.Read` が呼ばれた回数だけシフトする。

理由: 実機で起きているのは「CPU が同じリードサイクルを繰り返す」ことである。これをそのまま表現すると、`$2007`・`$2002`・`$4015`・`$4016`・`$4017` のすべてで正しい副作用が自然に発生する。デバイス側に個別の対応を入れると、対象レジスタを 1 つ見落としたときに気づかない。

### 2.4 リージョンで挙動を分ける

`region.DMCDMARegisterConflict` が false のとき再読み出しを行わない。設定 `emulation.dmcDmaRegisterConflicts` でも無効にできる。

理由: 2A07（PAL）ではこの問題が修正されている。加えて、この挙動を無効にしたほうが動くゲームがある場合に切り替えられるようにする。

## 3. タスク

### 3.1 DMA の型と位相

- [x] `internal/nes/bus/dma.go` に設計書 02 編 §2.7 の `DMA` 構造体を定義する
- [x] get/put の位相を APU のクロックから取る。バスが別に数えない
- [x] 位相の判定を「これから実行するサイクル」について行う。APU の位相は直前のサイクルのものなので反転する
- [x] `InitState.DMAGetPutPhase` で初期位相を設定する
- [x] `haltedAddr uint16` を追加する（停止時に読んでいたアドレス）

### 3.2 停止の仕組み

- [x] `serviceDMA(addr uint16, isRead bool)` を実装する
- [x] DMA が要求されていないとき何もしない
- [x] DMA が要求されているとき、halt サイクルを 1 サイクル消費する
- [x] halt が成立するのは CPU のリードサイクルのみとする。ライトサイクルでは失敗し次のサイクルで再試行する
- [x] 停止中の各 no-operation サイクルで `repeatHaltedRead` を呼ぶ
- [x] `repeatHaltedRead` が `region.DMCDMARegisterConflict` と設定を見て再読み出しを行う
- [x] DMA 完了後に CPU が停止時に試みていたリードを実行する
- [x] ライトサイクルで停止が最大 3 サイクル遅れることを確認する（RMW 命令の連続 2 ライト、割り込みの連続 3 ライト）

### 3.3 OAM DMA

- [x] `$4014` への書き込みで `oamPending` を立て、`oamPage` を設定する
- [x] 書き込みの次のサイクルで停止するようスケジュールする
- [x] halt サイクルを 1 サイクル消費する
- [x] 次が get サイクルでなければ alignment サイクルを 1 サイクル消費する
- [x] 256 組の get / put を実行する。get で `$xx00` から読み、put で `$2004` へ書く
- [x] ページ先頭から前方向にコピーする
- [x] 合計 513 または 514 サイクルになることを確認する
- [x] `INC $4014` のような RMW 命令で「2 回目に書かれたページ」からコピーされることを実装する
- [x] レンダリング中の `$2004` 書き込みが OAM を変更しない扱いと矛盾しないことを確認する（OAM DMA は通常 VBlank 中に行われる）
- [x] サイクル数を検証するテストを書く（get 開始と put 開始の両方）

### 3.4 DMC DMA

- [x] `dmcKind`（`load` と `reload`）を定義する
- [x] `apu` の `RequestDMCFetch` から DMA を要求し、get サイクルで読んだ値を `CompleteDMCFetch` で返す経路を実装する
- [x] load DMA を実装する。`$4015` の D4 セット後、サンプルバッファが空のときのみ発生する
- [x] reload DMA を実装する。サンプルバッファが空になったことへの応答として発生する
- [x] 種別を状態として持つ（load と reload で停止位置が違う。位置の違いは扱わない）
- [x] halt の後に dummy サイクルを 1 サイクル消費する
- [x] 次が get サイクルでなければ alignment サイクルを 1 サイクル消費する
- [x] get で PRG からサンプルバイトを読む
- [x] 通常 3 または 4 サイクルになることを確認する
- [x] DMC DMA が OAM DMA より優先されることを実装する
- [x] OAM DMA 中に DMC DMA の get が発生したとき OAM DMA を一時停止する
- [x] この一時停止により OAM DMA に alignment サイクルが追加されうることを実装する

### 3.5 レジスタ競合

- [x] `haltedAddr` に停止時のリードアドレスを記録する
- [x] 停止中の no-operation サイクルごとに `b.read(haltedAddr)` を呼ぶ
- [x] `$2007` の読み出しで余分なインクリメントが発生することを確認する
- [x] `$2002` の読み出しで VBlank フラグが余分にクリアされることを確認する
- [x] `$4015` の読み出しでフレーム IRQ フラグが余分にクリアされることを確認する
- [x] `$4016` / `$4017` の読み出しでシフトレジスタが余分にクロックされることを確認する
- [x] この結果 Right が押されたように見えることを確認する
- [x] `region.DMCDMARegisterConflict` が false のとき再読み出しを行わないことを確認する
- [x] 設定 `emulation.dmcDmaRegisterConflicts` で切り替えられることを確認する
- [x] 競合が発生したとき `warn.compat` へ記録する

### 3.6 スプライトの表示確認

- [x] OAM DMA を使う ROM でスプライトが表示されることを確認する
- [x] OAM DMA を使う ROM のフレームハッシュが維持されることを確認する（`scanline`・`sprite_hit_tests`・`sprite_overflow_tests`）
- [x] `dpcmletterbox` で DMC IRQ によるレターボックスが出ることを目視確認する

### 3.7 テスト ROM による検証

- [x] `internal/testrom/dma_test.go` を作成する
- [x] `sprdma_and_dmc_dma/` は未達として記録する（§5）
- [x] `dmc_dma_during_read4/` の各 ROM を実行する（`dma_4016_read` は未達として記録する）
- [x] `read_joy3/` の各 ROM を実行する（DMC DMA 競合のサブテストを含む）
- [x] `dma_sync_test_v2` は取得できないため扱わない（設計書 12 編 §12.6.1）
- [x] `dpcmletterbox` を実行する
- [x] `dmc_tests/` の 4 本は画面に何も出ないため扱わない（設計書 12 編 §12.6.1）
- [x] `oam_stress` が引き続き合格することを確認する
- [x] 各 ROM の期待結果を `testdata/golden/testroms.json` に登録する

- [x] フェーズ 2 から引き継いだ ROM を実行する（設計書 12 編 §12.6.1）
- [x] `cpu_interrupts_v2/rom_singles/4-irq_and_dma.nes` を実行する（DMC DMA が必要）

- [x] フェーズ 3 から引き継いだ ROM を実行する（設計書 12 編 §12.6.1）
- [x] `ppu_read_buffer/test_ppu_read_buffer.nes` を実行する（スプライト 0 ヒットと OAM DMA を組み合わせて検証する）

### 3.8 往復テスト

- [x] DMA の全フィールドを `SaveState` / `LoadState` に追加する
- [x] get/put の位相が往復することを確認する
- [x] `oamPending`・`oamPage` が往復することを確認する
- [x] `dmcPending`・`dmcKind`・`dmcAddr` が往復することを確認する
- [x] `haltedAddr` が往復することを確認する
- [x] 転送が 1 回のバスアクセスの中で完結するため、転送途中の状態を保存しないことを確認する（設計書 02 編 §2.7）
- [x] DPCM を再生する ROM で全フレーム往復テストを実行する

### 3.9 フェーズの締め

- [ ] `go test ./...` が 3 OS で通ることを CI で確認する
- [x] 設計書 08 編 §8.4 の「保存する状態」の表が実装と一致することを確認する
- [x] `docs/plans/README.md` のフェーズ 7 の状態を「完了」にする

## 4. 完了判定

| テスト ROM | 期待結果 |
|---|---|
| `read_joy3/test_buttons`・`thorough_test` | 画面に `Passed` |
| `read_joy3/count_errors` | 競合が 1 回以上起こること |
| `dmc_dma_during_read4/dma_2007_write`・`read_write_2007` | 画面に `Passed` |
| `cpu_interrupts_v2/rom_singles/4-irq_and_dma` | 結果コード 0 |
| `dpcmletterbox` | フレームハッシュが golden と一致 |
| OAM DMA のサイクル数 | 513 または 514 |
| フェーズ 6 までのテスト | すべて維持されること |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| DMC DMA の中止バグ（サンプル停止のタイミングに起因するもの） | 第 1 弾の範囲外。該当する ROM が非常に限られる |
| `sprdma_and_dmc_dma`（OAM DMA 中の DMC DMA の割り込み位置） | 未達。1 サイクルの精度で測るテストで、合格する条件を特定できていない |
| `dmc_dma_during_read4/dma_4016_read` | 未達。DMC DMA を実装する前から画面に何も出ない |
| load と reload で停止位置が違うこと | 種別は持つが位置の違いは未実装 |
| マッパー 0 以外 | フェーズ 8 |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| OAM DMA が 513 か 514 か | 開始が get サイクルか put サイクルかで決まる |
| DMC DMA が 3 か 4 か | get/put の判定が「これから実行するサイクル」になっているか確認する。APU の位相は直前のサイクルのものである |
| Right が誤入力されない | `repeatHaltedRead` が `b.read` を呼んでいるか確認する。`b.Read` ではない（`tick` を二重に呼ばないため） |
| Right が常に誤入力される | 再読み出しの回数が多すぎる。no-operation サイクルの数を確認する |
| `read_joy3` の失敗 | どのサブテストが失敗するか個別に確認する |
| OAM DMA 中に DMC DMA が入ると壊れる | 転送の途中で DMC の要求を見て、済ませてから get へそろえ直しているか確認する |
