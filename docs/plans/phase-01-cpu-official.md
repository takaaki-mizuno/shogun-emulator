# フェーズ 1: バス・ROM ローダ・マッパー 0・CPU（公式命令）

- 作成日: 2026-09-21
- 前提フェーズ: 0
- 完了条件: **nestest.nes を `$C000` から実行したトレースが nestest.log と完全一致する**

## 1. 背景

CPU はエミュレータの土台であり、ここに誤りがあると PPU と APU の実装で原因の切り分けができなくなる。

一方で、CPU の正しさは `nestest.log` との行単位の比較で機械的に判定できる。これは NES エミュレータの開発で得られる最も強い検証手段である。したがってこのフェーズの目標を「nestest.log と完全一致」の 1 点に絞る。

nestest を動かすには CPU 単体では足りない。ROM を読み、バスを通し、マッパー 0 でアドレスを解決する必要がある。トレース行に PPU の走査位置が含まれるため、PPU のカウンタも必要になる。このフェーズではそれらの最小限を用意する。

## 2. 方針とその理由

### 2.1 PPU はカウンタだけを先に作る

このフェーズでは PPU の描画を実装しない。`scanline` と `dot` を進めるカウンタと、`Step()` の呼び出しだけを用意する。

理由: nestest.log のトレース行に `PPU:  0, 21` の形で走査位置が含まれる。この値が一致しないと比較できない。描画は不要だが位置の進み方は必要である。

### 2.2 サイクルシーケンスを表から機械的に書き写す

アドレッシングモードごとの処理を、設計書 03 編 §3.4 と `docs/research/02_cpu_6502.md` の 8 節の表から 1 行ずつ対応させて書く。読みやすさのために手順をまとめたりしない。

理由: 表と実装が 1 対 1 で対応していれば、nestest の差分が出たときに「表の何行目に対応する処理が違うか」を特定できる。まとめてしまうと、この対応が失われる。

### 2.3 ダミーリードを省略しない

値を使わないリードも `bus.Read` を呼ぶ。

理由: nestest はサイクル数を検証する。ダミーリードを省くとサイクル数が合わない。加えて、フェーズ 3 以降で PPU レジスタの副作用が絡んだときに原因が分からなくなる。

### 2.4 トレース出力を最初から作る

`cpu.State` と `TraceLine()` を CPU の実装と同時に書く。デバッガの一部としてではなく、CPU の検証手段として位置づける。

理由: トレースがないと nestest との比較ができず、このフェーズが完了しない。

### 2.5 公式命令に絞る

非公式命令はフェーズ 2 で実装する。nestest は前半（`$C000` から始まる自動テスト）で公式命令を検証し、後半で非公式命令を検証する。フェーズ 1 では公式命令の範囲まで一致すれば合格とする。

理由: 一度に両方を実装すると、差分が出たときの原因の候補が増える。

## 3. タスク

### 3.1 バスの骨組み

- [x] `internal/nes/bus/bus.go` に `Bus` 構造体を定義する
- [x] 内蔵 RAM を `[2048]uint8` として持つ
- [x] `openBus uint8` を持つ
- [x] `cycles uint64` と `ppuDotAccum int` を持つ
- [x] `tick()` を実装する。設計書 02 編 §2.3 のとおり、PPU を分数比の回数進め、APU を 1 回進め、カートリッジへ通知し、`cycles` を増やす
- [x] `Read(addr uint16) uint8` を実装する。`tick()` を呼んでから `read` を呼ぶ
- [x] `Write(addr uint16, v uint8)` を実装する。`tick()` を呼ぶ
- [x] `read`（内部）にアドレスデコードを実装する。設計書 02 編 §2.4 の表に従う
- [x] `write`（内部）に同じデコードを実装する
- [x] 内蔵 RAM のミラー（`addr & 0x07FF`）を実装する
- [x] PPU レジスタのミラー（`addr & 0x0007`）を実装する
- [x] マップされていないアドレスの読み出しで `openBus` を返す
- [x] `openBus` の更新を `read` の中でアドレスごとに行う（`$4015` は更新しない。設計書 02 編 §2.4.1）
- [x] `Peek(addr uint16) uint8` を実装する。`tick` もフックも呼ばない
- [x] `Poke(addr uint16, v uint8)` を実装する
- [x] `Cycles() uint64` を実装する

### 3.2 割り込み線の集約

- [x] `internal/nes/bus/irq.go` に `IRQSource` を定義する（`IRQAPUFrame`、`IRQAPUDMC`、`IRQMapper`）
- [x] `SetIRQ(src IRQSource, asserted bool)` を実装する
- [x] `IRQAsserted() bool` を実装する
- [x] `NMILine() bool` を実装する。PPU の NMI 線の状態をそのまま返す

### 3.3 ROM ローダ

- [x] `internal/nes/cart/rom.go` に設計書 06 編 §6.1 の `ROM` 構造体を定義する
- [x] `LoadROM(data []uint8) (*ROM, error)` を実装する
- [x] マジック `$4E $45 $53 $1A` の検証を実装する。不一致ならエラーを返す
- [x] `detectFormat` を実装する。設計書 06 編 §6.1.1 の判別手順に従う
- [x] archaic iNES と判定したときマッパー番号の上位 4 bit を 0 にし、ログを出す
- [x] iNES のサイズ解釈（バイト 4 を 16 KiB 単位、バイト 5 を 8 KiB 単位）を実装する
- [x] NES 2.0 のサイズ解釈を実装する。`nes20Size` の指数・乗数表記に対応する
- [x] トレーナー（512 バイト）の読み飛ばしを実装する
- [x] ミラーリングの決定を実装する。設計書 06 編 §6.1.3 に従う
- [x] 4 画面ビットをマッパーが扱えないとき無視し、ログを出す
- [x] PRG-RAM サイズの決定を実装する。設計書 06 編 §6.1.4 に従う
- [x] CHR サイズが 0 のとき CHR-RAM を割り当てる
- [x] ROM ハッシュ（PRG + CHR の SHA-1）を計算する
- [x] `TimingMode` からリージョンを決める処理を実装する
- [x] 不正なヘッダ（サイズがファイルを超える、など）でエラーを返すことを検証するテストを書く
- [x] iNES と NES 2.0 の両方のヘッダを解析できることを検証するテストを書く（テストデータをコード内で組み立てる）

### 3.4 カートリッジインタフェースとマッパー 0

- [x] `internal/nes/cart/cart.go` に設計書 06 編 §6.2 の `Cartridge` インタフェースを定義する
- [x] `NametableTarget` と `NametableKind` を定義する
- [x] `Info` と `BankView` を定義する
- [x] `internal/nes/cart/banked.go` に設計書 06 編 §6.3 の `banked` 型を実装する
- [x] バンク番号をバンク総数で剰余を取る処理を実装する
- [x] `internal/nes/cart/mirroring.go` にミラーリングの解決を実装する。設計書 04 編 §4.9 の表に従う
- [x] `internal/nes/cart/mapper000.go` にマッパー 0 を実装する
- [x] `$8000`–`$BFFF` と `$C000`–`$FFFF` のマッピングを実装する。16 KiB の ROM では後者を前者のミラーにする
- [x] `$6000`–`$7FFF` の PRG-RAM を実装する。サイズが 8 KiB 未満のときミラーして埋める
- [x] PRG-RAM が無いとき `handled == false` を返す
- [x] `ReadCHR` と `WriteCHR` を実装する。CHR-ROM への書き込みは無視する
- [x] `NotifyPPUAddress` と `Tick` を空実装にする
- [x] `IRQAsserted` は常に false を返す
- [x] `SaveState` / `LoadState` を実装する。PRG-RAM・CHR-RAM・ミラーリング・CHR のバンク割り当て・4 画面構成の VRAM・マッパー番号（照合用）を含める
- [x] 共通部分（`common`）の直列化を 1 箇所にまとめ、各マッパーから呼ぶ
- [x] マッパー番号が一致しないステートを拒むことを検証するテストを書く
- [x] `internal/nes/cart/factory.go` に `New(rom *ROM) (Cartridge, error)` を実装する。未対応のマッパー番号ではエラーを返し、番号と名前を含める

### 3.5 PPU のカウンタ

- [x] `internal/nes/ppu/ppu.go` に `PPU` 構造体を定義する。このフェーズでは `scanline`・`dot`・`oddFrame`・`region` のみ
- [x] `Step()` を実装する。`dot` を進め、`DotsPerScanline` で `scanline` を進め、プリレンダ行の次で 0 に戻す。走査線の数は `region` から取る
- [x] `Scanline()` と `Dot()` を実装する
- [x] レジスタの読み書きを受け取る口だけ用意する。読み出しは 0、書き込みは無視する
- [x] `NMILine()` は常に true を返す（負論理のため、アサートなしが true）
- [x] `Frame()` を実装する。フレーム境界の判定に使う
- [x] `SaveState` / `LoadState` を実装する

### 3.6 APU の空実装

- [x] `internal/nes/apu/apu.go` に `APU` 構造体を定義する。このフェーズでは `cycles` と `evenCycle` のみ
- [x] `Step()` を実装する。`cycles` を増やし `evenCycle` を反転する
- [x] レジスタの読み書きを受け取る口を用意する。読み出しは `$4015` を 0、それ以外はオープンバス、書き込みは無視する
- [x] `SaveState` / `LoadState` を実装する

### 3.7 CPU の構造

- [x] `internal/nes/cpu/cpu.go` に設計書 03 編 §3.2 の `CPU` 構造体を定義する
- [x] `Bus` インタフェースを定義する
- [x] `packP(bFlag bool) uint8` を実装する。bit 5 は常に 1、bit 4 は引数で決める
- [x] `unpackP(v uint8)` を実装する。bit 5 と bit 4 を無視する
- [x] `setZN(v uint8)` を実装する
- [x] `push(v uint8)` と `pull() uint8` を実装する。`$0100 + S` へアクセスし、S を増減する
- [x] バスアクセスを内部の `read` / `write` に集約し、そこでサイクル数を進めて割り込み線を採取する
- [x] `peekStack()` を実装する。`PLA`・`PLP`・`RTS`・`RTI` の取り出し前の 1 サイクルに使う
- [x] `PowerOn()` を実装する。A・X・Y を 0、S を `$00` にしてリセットシーケンスを実行する。結果として S が `$FD`、I が true、PC が `($FFFC)`、サイクル数が 7 になる
- [x] `Reset()` を実装する。`S -= 3`、I を true、PC を `($FFFC)` にする。ライトを抑止したまま 3 回のスタックアクセスを行う

### 3.8 アドレッシングモード

各モードについて、設計書 03 編 §3.4 の表の 1 行ごとにバスアクセスを対応させる。

- [x] `addrImplied` を実装する。次の命令バイトを読んで捨てる
- [x] `addrImmediate` を実装する
- [x] `addrZeroPage` を実装する
- [x] `addrZeroPageX` を実装する。`address` を読んで捨て、X を加算する。上位バイトは常に `$00`
- [x] `addrZeroPageY` を実装する
- [x] `addrAbsolute` を実装する
- [x] `addrAbsoluteIndexed(index, access)` を実装する。リードはページ越えのときだけダミーリードを行い、ライトと RMW は常に行う（リード用とライト用で関数を分けず、`accessKind` で分けるのは、ページ越えの判定を 1 箇所に置くため）
- [x] `addrIndirectX` を実装する。ポインタ読み出しをゼロページ内でラップする
- [x] `addrIndirectY(access)` を実装する。ページ越えの扱いは `addrAbsoluteIndexed` と同じ
- [x] `addrRelative` を実装する
- [x] `addrIndirect`（`JMP (a)` 用）を実装する。上位バイトを下位バイトのみで計算する
- [x] `addrJSR` を実装する。下位バイトだけを先に読み、上位バイトは push の後に読む（設計書 03 編 §3.4）
- [x] `rmw(addr uint16, f func(uint8) uint8)` を実装する。読む・元の値を書く・変更後の値を書く の 3 アクセス
- [x] 各モードのサイクル数が設計書の表と一致することを検証するテストを書く（バスアクセスの回数を数える擬似バスを使う）
- [x] `JMP ($03FF)` が `$03FF` と `$0300` を読むことを検証するテストを書く

### 3.9 公式命令の実装

- [x] `internal/nes/cpu/ops.go` に演算を実装する
- [x] ロード: `LDA`・`LDX`・`LDY`
- [x] ストア: `STA`・`STX`・`STY`
- [x] 転送: `TAX`・`TAY`・`TXA`・`TYA`・`TSX`・`TXS`（`TXS` はフラグを変えない）
- [x] 加減算: `ADC`・`SBC`。オーバーフロー判定を設計書 03 編 §3.5 の式で実装する
- [x] 増減: `INC`・`DEC`・`INX`・`INY`・`DEX`・`DEY`。C と V を変更しない
- [x] シフト: `ASL`・`LSR`・`ROL`・`ROR`。アキュムレータ版とメモリ版の両方
- [x] 論理: `AND`・`ORA`・`EOR`
- [x] `BIT`。Z は `A & M`、N は M の bit 7、V は M の bit 6
- [x] 比較: `CMP`・`CPX`・`CPY`。V を変更しない
- [x] 分岐: `BCC`・`BCS`・`BEQ`・`BNE`・`BMI`・`BPL`・`BVC`・`BVS`
- [x] ジャンプ: `JMP`（絶対と間接）・`JSR`・`RTS`
- [x] `JSR` が `PC+2` を push することを確認する（次の命令の 1 バイト前）
- [x] `RTS` が pull した後に PC をインクリメントすることを確認する
- [x] `BRK`・`RTI`。`RTI` は pull した PC をインクリメントしない
- [x] スタック: `PHA`・`PHP`・`PLA`・`PLP`
- [x] フラグ: `CLC`・`SEC`・`CLI`・`SEI`・`CLD`・`SED`・`CLV`
- [x] `NOP`（`$EA`）

### 3.10 命令表

- [x] `internal/nes/cpu/opcodes.go` に `opcode` 構造体と `opcodes [256]opcode` を定義する
- [x] 公式命令 151 個のエントリを `docs/research/02_cpu_6502.md` の 6.1 節の表から転記する
- [x] 実装していない opcode には共通のエントリを入れ、実行時に記録を残して 2 サイクル消費する（フェーズ 2 で非公式命令に置き換える）
- [x] `StepInstruction()` を実装する。設計書 03 編 §3.3 の流れに従う
- [x] 転記した表の opcode・バイト数・サイクル数が調査結果と一致することを検証するテストを書く（表駆動）

### 3.11 割り込みの最小実装

- [x] `sampleInterruptLines()` を実装する。NMI の立ち下がりを検出する
- [x] `pollInterrupts()` を実装する
- [x] `serviceInterrupt(kind)` を実装する。設計書 03 編 §3.6.3 のサイクル列に従う
- [x] ベクタの決定をサイクル 4 と 5 の間の 1 箇所で行う
- [x] `applyPendingI()` を実装する。`CLI`・`SEI`・`PLP` の 1 命令遅延を処理する
- [x] `RTI` が I を即座に反映することを確認する

### 3.12 トレース出力

- [x] `internal/nes/cpu/state.go` に設計書 03 編 §3.8 の `State` を定義する
- [x] `CPU.State()` を実装する。現在の PC から命令バイト列を `Peek` で読む（副作用を起こさない）
- [x] `internal/nes/cpu/disasm.go` に逆アセンブル関数を実装する
- [x] 各アドレッシングモードのオペランド表記を実装する（`$44`、`$4400`、`$44,X`、`($44),Y`、`#$44`、`$C5F5` など）
- [x] `State.TraceLine()` を実装する。nestest.log の桁位置と空白を正確に合わせる
- [x] `TraceLine` の出力が nestest.log の 1 行目と一致することを検証するテストを書く
- [x] 非公式命令の行（`*NOP $A9 = 00`）の形も一致することを検証するテストを書く

### 3.13 NES の組み立て

- [x] `internal/nes/nes.go` に `NES` 構造体を定義する
- [x] `New(rom *cart.ROM, r *region.Region) (*NES, error)` を実装する（`InitState` は `PowerOn` に渡す。電源を入れ直すときに別の値を与えられるようにするため）
- [x] `InitState` を定義する。設計書 02 編 §2.5 のフィールド
- [x] `PowerOn(init InitState)` を実装する。RAM を `FillPattern` で埋め、各コンポーネントを初期化する
- [x] `Reset()` を実装する
- [x] `StepInstruction()` を実装する
- [x] `RunFrame()` を実装する。PPU のフレーム境界まで命令を実行する
- [x] `RunFrames(n int)` を実装する
- [x] `SaveState() []byte` と `LoadState(b []byte) error` を実装する。ヘッダの検証を含む
- [x] `Hooks` を定義する。設計書 02 編 §2.9 のうち、このフェーズで意味を持つ `OnCPURead`・`OnCPUWrite`・`OnInstructionStart` を入れる（PPU と映像のフックはそれぞれのフェーズで加える）
- [x] `SetHooks` を実装する。`bus.Hooks` へ CPU バスの分だけを配る

### 3.14 nestest による検証

- [x] `internal/testrom/nestest_test.go` を作成する
- [x] nestest.nes を読み込み、`PowerOn` の後に `PC = 0xC000` を設定する
- [x] nestest.log を 1 行ずつ読む処理を実装する
- [x] 1 命令ごとに `TraceLine()` を生成して比較する
- [x] 不一致のとき、行番号・実際の値・期待値・直前 10 行を表示して失敗する
- [x] 公式命令の範囲（`$C6BD` の直前まで）で一致することを確認する
- [x] 一致した行数をテストのログに出力する

### 3.15 往復テスト

- [x] `internal/testrom/roundtrip_test.go` を作成する
- [x] nestest.nes で 60 フレーム進めた後に往復テストを実行する
- [x] 不一致のとき `state.FirstDiff` で差分のセクション名を表示する
- [x] CPU・バス・PPU カウンタ・APU カウンタ・カートリッジの各セクションが往復することを確認する

### 3.16 フェーズの締め

- [x] `gofmt -l .`・`go vet ./...`・`go test ./...` が手元で通ることを確認する
- [x] 静的検査が通ることを確認する
- [x] 設計書 03 編 §3.9 の「保存する状態」の表が実装と一致することを確認する
- [ ] `go test ./...` が 3 OS で通ることを CI で確認する（リポジトリを GitHub へ置いた後）
- [x] `docs/plans/README.md` のフェーズ 1 の状態を更新する

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| nestest.log と一致する | 公式命令の範囲で全行一致すること |
| サイクル数が正しい | nestest.log の `CYC:` が一致すること |
| PPU の走査位置が正しい | nestest.log の `PPU:` が一致すること |
| 逆アセンブルが正しい | nestest.log のニーモニック欄が一致すること |
| 往復テストが通る | CPU・バスの状態が復元されること |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| 非公式命令 | フェーズ 2 |
| 分岐命令の割り込みポーリングの細部 | フェーズ 2 |
| 割り込みハイジャック | フェーズ 2 |
| PPU の描画 | フェーズ 3 |
| APU の音源 | フェーズ 6 |
| DMA | フェーズ 7。このフェーズでは `$4014` への書き込みを無視する |
| マッパー 0 以外 | フェーズ 8 |

## 6. つまずきやすい点

| 点 | 対処 |
|---|---|
| nestest.log の空白の桁数 | 期待値と実際の値を 16 進ダンプで比較する |
| `PPU:` の初期値 | `PowerOn` の時点の `scanline` と `dot` を nestest.log の 1 行目と合わせる |
| `CYC:` の初期値 | nestest.log の 1 行目は `CYC:7`。リセットシーケンスの 7 サイクルを数える |
| ストア命令のサイクル数 | `a,X`・`a,Y`・`(d),Y` はページ越えに関係なく 1 サイクル多い |
| `JSR` と `RTI` のリターンアドレスの違い | 設計書 03 編 §3.5 の記述を確認する |
