# 02 エミュレーションコア設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 2.1 コンポーネント構成

`internal/nes` は 1 台の NES を表す `NES` 型を提供する。`NES` は各コンポーネントを保持し、バスを介して相互に接続する。

```go
package nes

type NES struct {
    Region *region.Region
    Bus    *bus.Bus
    CPU    *cpu.CPU
    PPU    *ppu.PPU
    APU    *apu.APU
    Cart   cart.Cartridge
    Ports  [2]input.Device

    Hooks Hooks   // デバッグ用。ゼロ値では何もしない
}
```

各コンポーネントはバスを介してのみ相互に通信する。`ppu` が `cpu` を直接参照することはない。割り込みはバスが集約する（§2.8）。

## 2.2 リージョン定数

NTSC・PAL・Dendy の差分を `region.Region` に集約する。コード中に `341`、`262`、`3` のような値を直接書かない。

```go
package region

type Region struct {
    Name string

    // クロック
    CPUClockDivider int    // NTSC 12, PAL 16, Dendy 15
    PPUDotsNum      int    // PPU ドット / CPU サイクル の分子。NTSC 3, PAL 16
    PPUDotsDen      int    // 同 分母。NTSC 1, PAL 5

    // スキャンライン構成
    VisibleScanlines    int // 3 機種とも 240。PPU のカウンタ上の行数
    PostRenderScanlines int // NTSC 1, PAL 1, Dendy 51
    VBlankScanlines     int // NTSC 20, PAL 70, Dendy 20
    PreRenderScanlines  int // 3 機種とも 1
    DotsPerScanline     int // 341
    PictureHeight       int // 表示される画の高さ。NTSC 240, PAL 239, Dendy 239

    // 挙動の差分
    EmphasisBitShift      [3]uint8 // 赤・緑・青に対応する PPUMASK のビット位置
    ForcedOAMRefresh      bool     // PAL のみ true
    DMCDMARegisterConflict bool    // NTSC true, PAL false
    BlackBorder           bool     // PAL のみ true

    // テーブル
    NoisePeriods [16]uint16
    DMCRates     [16]uint16
    FrameCounterSteps [2][]uint32 // 4 ステップ / 5 ステップの APU サイクル境界
}

var NTSC = &Region{ /* ... */ }
var PAL = &Region{ /* ... */ }
var Dendy = &Region{ /* ... */ }
```

各フィールドの値は `docs/research/07_timing_and_synchronization.md` の 2 節、`docs/research/04_apu.md` の 3・4.6・4.7 節の表を転記する。

PAL の PPU ドット / CPU サイクルが 3.2 であるため、比を分数（`PPUDotsNum` / `PPUDotsDen`）で保持する。浮動小数点で累積すると誤差が入り、決定論が保てない。

`VisibleScanlines` は 3 機種とも 240 である。PAL の画の高さが 239 であるのは、常に黒いボーダーが画の上 1 ピクセルを覆うためであり、スキャンラインの数が少ないのではない。`PictureHeight` はボーダーの描画範囲を決めるために別に持つ。

## 2.3 クロックと lock-step

進行の単位は 1 CPU サイクルである。1 CPU サイクルは 1 回のバスアクセス（リードまたはライト）に対応する。

```go
package bus

// Read は 1 サイクルを消費して読む。PPU をアクセスの前後に分けて進める。
func (b *Bus) Read(addr uint16) uint8 {
    before, after := b.splitDots()   // §2.3.1
    b.stepPPU(before)
    v := b.read(addr)
    b.stepPPU(after)
    b.finishCycle()                  // APU・カートリッジ・サイクル数
    return v
}
```

### 2.3.1 CPU と PPU のサイクル内の位相

1 CPU サイクルの間に PPU は 3 ドット（PAL では 3 または 4）進む。CPU がバスへアクセスするのはそのうちどの時点かが決まっており、`accessDots` で表す。アクセスの前に `accessDots` ドット、後に残りを進める。

| 設定 | 意味 |
|---|---|
| `accessDots = 1` | アクセスはサイクルの 1 ドット目の後 |
| `accessDots = 2` | 2 ドット目の後。実機の位相 |
| `accessDots = 3` | 3 ドット目の後 |

`InitState.CPUPPUAlignment` から `accessDots = CPUPPUAlignment + 1` として設定する。既定値は 2 とする。

位相を持つ理由は、VBlank フラグの読み出し競合と NMI のタイミングが 1 ドットの精度でこれに依存することである。`ppu_vbl_nmi` の `05-nmi_timing`・`06-suppression`・`07-nmi_on_timing`・`08-nmi_off_timing` は他の位相では合格しない。

PPU の観測結果とバスアクセスの位置をそろえる。CPU が割り込み線を採取するのはサイクルの末尾であり、アクセスより後になる。

`Read` と `Write` は `tick` を伴う。CPU は「1 サイクル = 1 バスアクセス」の原則を守るだけでよい。

APU は CPU のバスアクセスより前に 1 サイクル進める。PPU と同じく、コンポーネントの内部処理が CPU のアクセスより先に起こる。この順序により、レジスタへの書き込みはそのサイクルの APU の処理が終わった後に反映される。`blargg_apu_2005.07.30` の `08.irq_timing` がこの順序を検証する。

```go
func (b *Bus) Read(addr uint16) uint8 {
    b.dma.serviceBeforeRead(b)   // §2.7
    b.tick()
    v := b.read(addr)
    if b.hooks.OnCPURead != nil {
        b.hooks.OnCPURead(addr, v)
    }
    return v
}

func (b *Bus) Write(addr uint16, v uint8) {
    b.tick()
    old := b.peek(addr)
    b.write(addr, v)
    if b.hooks.OnCPUWrite != nil {
        b.hooks.OnCPUWrite(addr, v, old)
    }
}
```

CPU が値を使わないダミーリードも `Read` を呼ぶ。PPU レジスタやマッパーの副作用を発生させるためである。

`openBus` の更新は `read` の中で、アドレスごとに行う。`Read` の側で一律に更新しない。`$4015` の読み出しはオープンバスを更新しないという規則（§2.4.1）を、アドレスデコードと同じ場所で表すためである。

## 2.4 CPU アドレス空間

`read` と `write` のアドレスデコードを次のとおり定める。

| アドレス範囲 | 処理 |
|---|---|
| `$0000`–`$1FFF` | 内蔵 RAM。`addr & 0x07FF` でミラーを畳む |
| `$2000`–`$3FFF` | PPU レジスタ。`addr & 0x0007` でレジスタ番号を得る |
| `$4000`–`$4013` | APU レジスタ |
| `$4014` | OAM DMA 起動（書き込みのみ） |
| `$4015` | APU ステータス |
| `$4016` | コントローラポート 1 |
| `$4017` | 書き込みは APU フレームカウンタ、読み出しはコントローラポート 2 |
| `$4018`–`$401F` | 何もしない |
| `$4020`–`$FFFF` | カートリッジ |

読み出しがどのデバイスにも当たらない場合はオープンバスの値を返す。

### 2.4.1 オープンバス

`Bus.openBus` に「直前にバスから読まれた値」を保持する。マップされていないアドレスの読み出しはこの値を返す。

`$4016` と `$4017` の読み出しは下位 5 bit をデバイスから取り、上位 3 bit を `openBus` から取る。

```go
func (b *Bus) readController(port int) uint8 {
    v := b.ports[port].Read() & 0x1F
    return v | (b.openBus & 0xE0)
}
```

`$4015` の読み出しは CPU 内部で完結するため `openBus` を更新しない。

### 2.4.2 副作用のない読み出し

デバッガがメモリを表示するために `Read` を使うと、PPU レジスタやコントローラの状態が変化する。これを避けるため、副作用を持たない `Peek` と `Poke` を用意する。

```go
// Peek は副作用を発生させずに値を読む。tick もフックも呼ばない。
func (b *Bus) Peek(addr uint16) uint8

// Poke は副作用を発生させずに値を書く。tick もフックも呼ばない。
func (b *Bus) Poke(addr uint16, v uint8)
```

`Peek` の対象が副作用を持つレジスタである場合、各コンポーネントの `PeekRegister` を呼ぶ。`$2002` の読み出しで VBlank フラグをクリアせず、`$2007` でアドレスを進めない。

デバッガは `Read` と `Write` を呼ばない。

## 2.5 リセットと電源投入

```go
func New(rom *cart.ROM, r *region.Region) (*NES, error)
func (n *NES) PowerOn(init InitState)
func (n *NES) Reset()
```

`InitState` を `New` ではなく `PowerOn` に渡す。電源を入れ直すときに別の `InitState` を与えられるようにするためである。

`InitState` は電源投入時に実機で値が定まらない状態を決定する。

```go
type InitState struct {
    RAMPattern      RAMPattern // Zero, FF, Pattern, Random
    RAMSeed         uint64
    CPUPPUAlignment int        // 0..2
    DMAGetPutPhase  int        // 0..1
    PPUVBlankFlag   bool
}
```

`RAMPattern` が `Random` のとき、`RAMSeed` を種とする専用の生成器で内蔵 RAM・OAM・パレット RAM・CIRAM・CHR-RAM を埋める。`math/rand` を使わず、SplitMix64 を自前で持つ。生成列が処理系とバージョンに依存すると、セーブステートとムービーの再現性が失われる。

埋める対象ごとに異なる値（salt）を種に混ぜる。同じ種で複数の領域を埋めたときに、同一の並びが繰り返されることを避ける。同じ `InitState` から常に同じ内容を得る。

`Reset` は電源投入時と異なり、RAM の内容を変更しない。CPU は `S -= 3` と `I = 1` を行い、書き込みを抑止したまま 3 回のスタックアクセスを実行する。PPU は内部リセット信号をセットし、VBlank 終了時にクリアする。

## 2.6 進行の駆動

進行を待たせる役目を `emu.Pacer` に分ける。

```go
package emu

// Pacer は 1 フレームを進め終えたところで呼ばれ、次のフレームを始めてよく
// なるまで待つ。
type Pacer interface {
    // WaitFrame は 1 フレーム分の進行が終わったことを伝える。
    // speed は速度倍率で、1.0 が実時間と同じ速さである。
    WaitFrame(speed float64)

    // Reset は待ちの基準を現在に合わせる。一時停止から復帰したときと
    // セーブステートを読み込んだときに呼ぶ。
    Reset()
}
```

オーディオが有効なとき、進行はオーディオデバイスの消費量で決まる。壁時計を
参照しない。

オーディオが無効なとき（設定で切ったとき、およびデバイスの初期化に失敗した
とき。「01 全体アーキテクチャ設計」§1.9）は、消費の通知が来ないため壁時計で
待つ。リージョンのフレーム周期を基準に、前のフレームを始めた時刻からの
経過時間が不足していれば待つ。

| 実装 | 待ちの基準 | 使う場面 |
|---|---|---|
| `audioPacer` | オーディオリングバッファの高水位 | オーディオが有効なとき |
| `wallClockPacer` | リージョンのフレーム周期 | オーディオが無効なとき |

壁時計で待つ実装は、ホストの時計とオーディオデバイスのクロックのずれを
吸収できない。音を出しているときにこれを使うと、ずれが累積して音が途切れる。
オーディオが有効なときは必ず `audioPacer` を使う。

`Pacer` を差し替えの点とするのは、進行の待ち方が「エミュレーション結果を
決める経路」の外にあることを構造として表すためである。どちらの実装を使っても
同じ入力から同じ結果を得る。


`apu.Step` が生成したサンプルは `internal/audio` のリングバッファに書かれる。リングバッファが高水位に達すると、書き込み側がブロックする。

```go
package audio

type Ring struct {
    mu   sync.Mutex
    cond *sync.Cond
    buf  []int16
    r, w int
    high int   // 高水位。サンプル数
}

// Write はエミュレーションゴルーチンから呼ばれる。
// 高水位に達していればブロックする。
func (r *Ring) Write(l, right int16) bool

// Read は oto の再生スレッドから呼ばれる。
// 読み終えたら cond.Broadcast でエミュレーションゴルーチンを起こす。
func (r *Ring) Read(p []byte) (int, error)
```

| 項目 | 値 |
|---|---|
| サンプリングレート | 48000 Hz |
| oto のバッファ | 25 ms |
| リングの高水位 | oto のバッファの 2 倍（50 ms 相当） |
| リングの容量 | 高水位の 2 倍 |

高水位を oto のバッファの 2 倍とするのは、処理が一時的に遅れたときの余裕を確保するためである。等倍では余裕がなく、遅れがそのまま音切れになる。

再生開始の前にリングを高水位まで埋める。埋めずに再生を始めると、起動時にバッファ 1 杯分の無音が出力される。

一時停止・ブレークポイントによる停止中は、エミュレーションゴルーチンがサンプルを生成しない。リングが空になったとき `Read` は無音を返す。

速度倍率が 1.0 以外のとき、生成するサンプル数に倍率を掛ける。倍率が 4.0 以上のときはリングへの書き込みを行わず、ブロックもしない。このとき進行速度はホストの処理能力で決まる。

## 2.7 DMA

`bus.DMA` が OAM DMA と DMC DMA を保持する。DMA は CPU の外側にあり、CPU を停止させる。

```go
type DMA struct {
    // getPutInvert は get/put の位相を反転するかどうか
    getPutInvert bool
    // haltedAddr は CPU を止めたときに読もうとしていたアドレス
    haltedAddr uint16

    oamPending bool
    oamPage    uint8

    dmcPending bool
    dmcKind    dmcKind // load または reload
    dmcAddr    uint16
}
```

`Bus.Read` と `Bus.Write` の先頭で `serviceDMA(addr, isRead)` を呼ぶ。DMA が要求されていれば、そこで CPU を停止させて転送を行う。`addr` と `isRead` は、これから CPU が行おうとしているアクセスである。

転送は `serviceDMA` の中で最後まで行う。途中で抜けて次のアクセスへ戻る形にしない。スナップショットを取れるのは命令境界のみであり（「08 セーブステートと入力ムービー設計」§8.3）、転送は 1 回のバスアクセスの中で完結するため、転送の途中の状態は外から観測できない。転送位置を状態として持つ必要がない。

get/put は APU のクロックの前半・後半である。バスが別に数えず、APU の位相をそのまま使う。同じクロックのはずの 2 つが食い違うことを避けるためである。`InitState.DMAGetPutPhase` はどちらを get と呼ぶかを選ぶ。

位相の判定は「これから実行するサイクル」について行う。APU の位相は直前に進めたサイクルのものであるため、反転して使う。1 サイクルずれると DMC DMA の長さが 3 と 4 で入れ替わり、`read_joy3/thorough_test` と `dmc_dma_during_read4/dma_2007_write` が通らなくなる。

サイクル数はバスが数える。1 CPU サイクル = 1 バスアクセスであり、DMA が CPU を止めている間のサイクルもバスのアクセスとして現れる。CPU は自前の計数を持たず `Bus.Cycles()` を参照する。計数の持ち主を 1 つにすることで、DMA のサイクルが数え漏れることを防ぐ。

| 種別 | 構成 | サイクル数 |
|---|---|---|
| OAM DMA | halt + （必要なら alignment）+ get/put を 256 回 | 513 または 514 |
| DMC DMA（load） | halt + dummy + （必要なら alignment）+ get | 3 または 4 |
| DMC DMA（reload） | halt + dummy + （必要なら alignment）+ get | 3 または 4 |

DMA が停止できるのは CPU のリードサイクルのみである。ライトサイクルでは停止に失敗し、次のサイクルで再試行する。

DMC DMA は OAM DMA より優先される。OAM DMA 中に DMC DMA の get が発生した場合、OAM DMA を一時停止する。

DMC のサンプルは、要求した時点ではなく DMA の get サイクルで読む。APU は `RequestDMCFetch` で要求だけを出し、バスが読んだ値を `CompleteDMCFetch` で受け取る。残りバイト数が減り、ループと割り込みの判定が行われるのもそのときである。要求した時点で読むと、サンプルの消費が実機より数サイクル早くなる。

### 2.7.1 レジスタ競合

CPU が停止している間、no-operation な DMA サイクルごとに「停止時に読んでいたアドレス」を再度読む。読み出しに副作用を持つレジスタを読んでいた場合、副作用が複数回発生する。

```go
// 停止中の各 no-operation サイクルで呼ぶ
func (d *DMA) repeatHaltedRead(b *Bus) {
    if b.region.DMCDMARegisterConflict {
        _ = b.read(d.haltedAddr)   // 副作用を発生させる
    }
}
```

これにより `$4016` の読み出しからビットが 1 つ失われ、Right が押されたように見える挙動が再現される。`region.DMCDMARegisterConflict` が false のリージョンではこの再読み出しを行わない。

設定で無効にできる。無効時は再読み出しを行わない。

読み直す対象を、読み出しに副作用を持つアドレス（`$2002`・`$2007`・`$4015`・`$4016`・`$4017`）に限る。副作用の無いアドレスを読み直しても同じ値が読まれるだけであり、観測できる違いは生じない。対象を絞ることで、競合の記録が意味のある場面だけに出る。

## 2.8 割り込み線の集約

IRQ はレベル検出であり、複数の発生源の論理和である。バスが集約して CPU に渡す。

```go
type IRQSource uint8

const (
    IRQAPUFrame IRQSource = 1 << 0
    IRQAPUDMC   IRQSource = 1 << 1
    IRQMapper   IRQSource = 1 << 2
)

func (b *Bus) SetIRQ(src IRQSource, asserted bool)
func (b *Bus) IRQAsserted() bool   // いずれかの発生源がアサートしていれば true
```

APU とカートリッジは自身の割り込みフラグを状態として持つ。バスは `IRQAsserted` のたびにそれらへ問い合わせる。フラグを持つ側が変化のたびにバスへ知らせる形にすると、フラグを変える経路すべてに通知を書くことになり、書き漏らしが割り込みの取りこぼしとして現れる。`SetIRQ` は状態を持たない発生源のために残す。

NMI はエッジ検出であり、PPU のみが発生源である。バスは PPU の NMI 線の状態をそのまま CPU に渡す。CPU 側で前回値と比較して立ち下がりを検出する（「03 CPU 設計」§3.6）。

## 2.9 デバッグフック

フックは関数フィールドとして保持する。`nil` のときは呼ばない。

外から設定する口は `nes.Hooks` に集約する。`NES.SetHooks` が各コンポーネントへ必要な分だけ配る。

```go
package nes

type Hooks struct {
    OnCPURead  func(addr uint16, value uint8)
    OnCPUWrite func(addr uint16, value uint8, old uint8)
    OnInstructionStart func(s cpu.State)      // 命令のフェッチ直前
    OnBeforeExec       func(pc uint16) bool   // true を返すと命令を実行しない
    OnCycle            func()                 // CPU サイクルの終わり
    OnInterrupt        func(k cpu.Interrupt)  // NMI・IRQ・リセットの受け付け
    OnSprite0Hit       func()                 // スプライト 0 ヒットが立った
    OnFrameComplete    func(f *video.Frame)
}

func (n *NES) SetHooks(h Hooks)
```

| フック | 用途 |
|---|---|
| `OnCPURead`・`OnCPUWrite` | 読み出し・書き込みブレークポイント、未初期化 RAM の検出 |
| `OnInstructionStart` | トレース。`cpu.State` の組み立てに逆アセンブルを伴うため、トレース以外では使わない |
| `OnBeforeExec` | 実行ブレークポイント。命令のフェッチ前に PC だけを渡す。`true` を返すと `StepInstruction` は命令を実行せずに戻る |
| `OnCycle` | サイクル単位ステップ、PPU 位置ブレークポイント、マッパー IRQ の検出、任意スキャンラインのスナップショット |
| `OnInterrupt` | NMI・IRQ・リセットのイベントブレークポイント |
| `OnSprite0Hit` | スプライト 0 ヒットのイベントブレークポイント |
| `OnFrameComplete` | スナップショットと変更追跡 |

`OnBeforeExec` を `OnInstructionStart` と分けるのは、実行ブレークポイントの判定に逆アセンブルが要らないためである。`OnInstructionStart` だけで判定すると、ブレークポイントを 1 つ置くだけで毎命令の逆アセンブルが走る。

```go
package bus

// bus が保持するのは CPU バスに関わる分だけである。
type Hooks struct {
    OnCPURead  func(addr uint16, value uint8)
    OnCPUWrite func(addr uint16, value uint8, old uint8)
}
```

`bus` が PPU と映像のフックを持たないのは、`bus` が `video` を参照しないためである（§1.4）。

`OnCPURead` と `OnCPUWrite` は毎秒 180 万回程度呼ばれる。フック内で UI を操作せず、共有構造体への書き込みのみを行う。デバッグウィンドウが開いていないときは `nil` を設定する。

MMC3 の A12 カウンタはフックを経由せず、`cart.Cartridge.NotifyPPUAddress` で直接受け取る（「06 カートリッジとマッパー設計」§6.4）。フックはデバッガ専用である。

## 2.10 実行の単位

`NES` は命令単位の実行を提供する。サイクル単位の制御は「09 デバッガ設計」§9.5 で扱う。

```go
// StepInstruction は 1 命令を実行する。内部で tick が複数回呼ばれる。
func (n *NES) StepInstruction()

// Cycles は電源投入からの累積 CPU サイクル数を返す。
func (n *NES) Cycles() uint64
```

外部からのコマンド（セーブステート、ロード、速度変更、ステップ）は命令境界でのみ処理する。この制約の理由と扱いは「08 セーブステートと入力ムービー設計」§8.3 に記述する。

## 2.11 状態の直列化

各コンポーネントは `state.Snapshotter` を実装する。

```go
package state

type Writer struct { /* セクション付きバイナリを書く */ }
type Reader struct { /* セクション付きバイナリを読む */ }

type Snapshotter interface {
    SaveState(w *Writer)
    LoadState(r *Reader) error
}
```

`NES.SaveState` は各コンポーネントの `SaveState` を順に呼ぶ。保存する項目の一覧と形式は「08 セーブステートと入力ムービー設計」§8.4 に記述する。
