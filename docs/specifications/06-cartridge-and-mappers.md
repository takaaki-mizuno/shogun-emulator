# 06 カートリッジとマッパー設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 6.1 ROM ローダ

`.nes` ファイルを読み、ヘッダを解析して `ROM` を組み立てる。

```go
package cart

type Format uint8

const (
    FormatArchaicINES Format = iota
    FormatINES
    FormatNES20
)

type ROM struct {
    Format      Format
    Mapper      uint16   // 0..4095
    Submapper   uint8
    PRG         []uint8
    CHR         []uint8
    Trainer     []uint8
    MiscROM     []uint8

    PRGRAMSize   int
    PRGNVRAMSize int
    CHRRAMSize   int
    CHRNVRAMSize int

    Mirroring    Mirroring  // ハードワイヤードな配置
    FourScreen   bool
    HasBattery   bool
    ConsoleType  uint8
    TimingMode   uint8      // 0: NTSC, 1: PAL, 2: 複数, 3: Dendy

    Hash [20]uint8          // PRG + CHR の SHA-1
}

func LoadROM(data []uint8) (*ROM, error)
```

### 6.1.1 形式の判別

```go
func detectFormat(h []uint8, fileSize int) Format {
    switch {
    case h[7]&0x0C == 0x08 && nes20SizeFits(h, fileSize):
        return FormatNES20
    case h[7]&0x0C == 0x04:
        return FormatArchaicINES
    case h[7]&0x0C == 0x00 && allZero(h[12:16]):
        return FormatINES
    default:
        return FormatArchaicINES
    }
}
```

先頭 4 バイトが `$4E $45 $53 $1A` でないときエラーを返す。

`FormatArchaicINES` と判定した場合、マッパー番号の上位 4 bit を 0 にする。バイト 7–15 にリッパーが書いた文字列が入っていることがあり、そのままでは誤ったマッパー番号になる。この処理を行ったとき `warn.compat` にログを出す。

### 6.1.2 サイズの解釈

iNES ではバイト 4 と 5 をそれぞれ 16 KiB 単位と 8 KiB 単位のサイズとする。

NES 2.0 ではバイト 9 の上位・下位ニブルを MSB として 12 bit で表す。MSB ニブルが `$F` のときは指数・乗数表記とする。

```go
func nes20Size(lsb uint8, msb uint8, unit int) int {
    if msb == 0x0F {
        exp := (lsb >> 2) & 0x3F
        mult := int(lsb&0x03)*2 + 1
        return (1 << exp) * mult
    }
    return (int(msb)<<8 | int(lsb)) * unit
}
```

### 6.1.3 ミラーリングの決定

```go
type Mirroring uint8

const (
    MirrorHorizontal Mirroring = iota  // 垂直配置。CIRAM A10 = PPU A11
    MirrorVertical                     // 水平配置。CIRAM A10 = PPU A10
    MirrorSingleA
    MirrorSingleB
    MirrorFourScreen
)
```

ヘッダのバイト 6 の bit 0 が 0 なら `MirrorHorizontal`、1 なら `MirrorVertical` とする。bit 3 が立っているとき、マッパーが 4 画面構成を扱える場合のみ `MirrorFourScreen` とする。扱えない場合は bit 3 を無視し、`warn.compat` にログを出す。

ミラーリングをマッパーが制御する場合、ヘッダの bit 0 は初期値としても使わない。マッパーの電源投入時の値を使う。

### 6.1.4 PRG-RAM のサイズ

NES 2.0 ではバイト 10 のシフトカウントから求める。バイト数は `64 << シフトカウント` であり、シフトカウントが 0 のとき PRG-RAM を持たない。CHR-RAM のサイズもバイト 11 から同じ式で求める。

iNES ではサイズを表す手段がないため、バッテリーフラグが立っているか、マッパーが PRG-RAM を持つ構成のときに 8 KiB を割り当てる。MMC1 では 32 KiB を割り当てる。

`PRGRAMSize` が 0 のとき、`$6000`–`$7FFF` の読み出しはオープンバスを返す。

## 6.2 カートリッジインタフェース

```go
type Cartridge interface {
    // CPU バス
    ReadPRG(addr uint16) (value uint8, handled bool)
    WritePRG(addr uint16, value uint8)

    // PPU バス
    ReadCHR(addr uint16) uint8
    WriteCHR(addr uint16, value uint8)
    MapNametable(addr uint16) NametableTarget

    // PPU がアドレスバスに値を出したときに呼ばれる
    // dot は電源投入からの PPU のドット数
    NotifyPPUAddress(addr uint16, dot uint64)

    // CPU サイクルの経過
    Tick(cycles int)

    // IRQ
    IRQAsserted() bool

    // CPU アドレスに対応する PRG-ROM のオフセット（デバッガの実行記録に使う）
    PRGOffset(addr uint16) (int, bool)

    // PRG-RAM の全体（状態のハッシュとデバッガの表示に使う）
    PRGRAM() []uint8

    // 不揮発メモリ
    BatteryRAM() []uint8
    SetBatteryRAM(data []uint8) error

    // 状態
    state.Snapshotter

    // 情報
    Info() Info
}
```

`ReadPRG` が `handled == false` を返したとき、バスはオープンバスの値を返す。これにより `$6000`–`$7FFF` に何もないカートリッジのオープンバス挙動を表現する。

```go
type Info struct {
    MapperName   string
    MapperNumber uint16
    Submapper    uint8
    PRGBanks     []BankView   // デバッガ表示用
    CHRBanks     []BankView
    Mirroring    Mirroring
}

type BankView struct {
    CPUOrPPUAddr uint16
    Size         int
    SourceKind   string   // "PRG-ROM", "PRG-RAM", "CHR-ROM", "CHR-RAM"
    BankIndex    int
    Offset       uint32
}
```

`Info` はデバッガが現在のバンク構成を表示するために使う（「09 デバッガ設計」§9.4）。

## 6.3 バンクマッピングの共通実装

各マッパーがアドレス計算を個別に書くと誤りが混入する。共通の `banked` 型を用意する。

```go
type banked struct {
    data      []uint8
    bankSize  int
    windows   []int   // ウィンドウごとのバンク番号
}

func (b *banked) read(windowIndex int, offsetInWindow uint16) uint8 {
    bank := b.windows[windowIndex]
    if n := len(b.data) / b.bankSize; n > 0 {
        bank %= n
    }
    return b.data[bank*b.bankSize+int(offsetInWindow)]
}

func (b *banked) setBank(windowIndex, bank int) { b.windows[windowIndex] = bank }
```

バンク番号をバンク総数で剰余を取る。ROM の末尾を超えるバンクは前のバンクのミラーになる。

メモリがウィンドウより小さいとき（4 KiB の CHR-RAM を 8 KiB のウィンドウへ置く場合など）は、ウィンドウ内のオフセットをメモリの大きさで畳む。実機ではメモリの容量を超えるアドレス線が繋がっておらず、同じ内容が繰り返し現れる。畳まずにアクセスを捨てると、CHR-RAM が常に 0 を返し、画面が背景色だけになる。

```go
func (b *banked) resolve(windowIndex int, offsetInWindow uint16) (int, bool) {
    if len(b.data) == 0 {
        return 0, false
    }
    n := len(b.data) / b.bankSize
    if n == 0 {
        // ウィンドウより小さいメモリ。上位のアドレス線が無いものとして畳む。
        return int(offsetInWindow) % len(b.data), true
    }
    // ...
}
```

## 6.4 PPU アドレスバスの監視

`NotifyPPUAddress` は PPU が 2 ドットのアクセスの 1 ドット目にアドレスをバスへ出したときに呼ばれる。スプライトフェッチのダミーフェッチ、プリレンダー行のフェッチ、dot 337–340 の未使用フェッチも含めて呼ばれる。

レンダリングのフェッチに加えて、次の 3 つの時点でも呼ぶ。

| 時点 | バスに出るアドレス |
|---|---|
| `$2006` の 2 回目の書き込み | 転送後の `v` |
| `$2007` のアクセス | アクセスする `v` |
| `$2007` のアクセス後の `v` の更新 | 更新後の `v` |

`$2007` の前後で 2 回呼ぶのは、`$0FFF` を読んで `$1000` へ進む読み出しが A12 を立ち上げるためである。

MMC3 はこのフックで A12 の立ち上がりを数える。呼ばれる回数とタイミングが実機と一致することが、スキャンライン IRQ の精度の前提となる。

```go
// MMC3 の A12 フィルタ
type a12Filter struct {
    prevHigh bool
    lowDots  int   // A12 が low のまま経過した PPU のドット数
}

func (f *a12Filter) notify(addr uint16, dots int) (rising bool) {
    high := addr&0x1000 != 0
    if !high {
        f.lowDots += dots
        f.prevHigh = false
        return false
    }
    rising = !f.prevHigh && f.lowDots >= a12LowDotsRequired
    // high になった時点で low の継続は途切れる。
    f.lowDots = 0
    f.prevHigh = true
    return rising
}
```

`a12LowDotsRequired` を 10 ドットとする。CPU サイクルではなくドットで測るのは、スプライトのパターンフェッチの間に現れる 4 ドットの low が、位相によって 1 CPU サイクルにも 2 CPU サイクルにも見えるためである。しきい値をまたいだ走査線だけ余分な立ち上がりを数え、IRQ が早まる。

しきい値を 10 とするのは、`$2000` の bit 4 を立てた構成（背景が `$1000`、スプライトが `$0000`）で dot 337 から次の走査線の dot 4 までに現れる 9 ドットの low を数えないためである。この low を数えると走査線あたりの立ち上がりが 2 回になり、`mmc3_test_2/4-scanline_timing` の第 12 項が通らない。

立ち上がりと認めなかったときも `lowDots` を 0 に戻す。戻さないと、スプライトのフェッチの間に現れる短い low が積み上がり、走査線あたり 3 回の立ち上がりを数える。

## 6.5 マッパーの実装

コンソール種別が NES / Famicom 以外の ROM はカートリッジを作らずエラーにする。VS System と PlayChoice-10 は PPU と入出力が異なり、動かしても正しい画面にならない。

対応するマッパーを次に示す。

| 番号 | 名称 | PRG バンク | CHR バンク | ミラーリング | IRQ |
|---|---|---|---|---|---|
| 0 | NROM | 固定 | 固定 | ハードワイヤード | なし |
| 2 | UxROM | 16 KiB 可変 + 16 KiB 固定 | CHR-RAM 8 KiB | ハードワイヤード | なし |
| 3 | CNROM | 32 KiB 固定 | 8 KiB 可変 | ハードワイヤード | なし |
| 7 | AxROM | 32 KiB 可変 | CHR-RAM 8 KiB | 1 画面（マッパー制御） | なし |
| 66 | GxROM | 32 KiB 可変 | 8 KiB 可変 | ハードワイヤード | なし |
| 1 | MMC1 | 16 KiB × 2 または 32 KiB | 4 KiB × 2 または 8 KiB | マッパー制御 | なし |
| 4 | MMC3 | 8 KiB × 4 | 2 KiB × 2 + 1 KiB × 4 | マッパー制御 | スキャンライン |

実装順序は上の表の順とする。レジスタを持たない構成から始め、最後にスキャンライン IRQ を持つ MMC3 を実装する。MMC3 の実装により §6.4 のフックが正しく動くことが確認できる。

### 6.5.1 マッパー 0（NROM）

| 領域 | 割り当て |
|---|---|
| `$6000`–`$7FFF` | PRG-RAM（存在する場合。ミラーして 8 KiB を埋める） |
| `$8000`–`$BFFF` | PRG-ROM の先頭 16 KiB |
| `$C000`–`$FFFF` | PRG-ROM の末尾 16 KiB。16 KiB の ROM では `$8000`–`$BFFF` のミラー |
| PPU `$0000`–`$1FFF` | CHR 8 KiB |

レジスタを持たない。`WritePRG` は何もしない。

### 6.5.2 マッパー 2（UxROM）

`$8000`–`$FFFF` への書き込みの下位 8 bit を `$8000`–`$BFFF` のバンク番号とする。`$C000`–`$FFFF` は末尾のバンクに固定する。

CHR は 8 KiB の CHR-RAM とする。

### 6.5.3 マッパー 3（CNROM）

`$8000`–`$FFFF` への書き込みの下位 2 bit（オーバーサイズ構成では下位 4 bit）を CHR の 8 KiB バンク番号とする。PRG は 32 KiB 固定である。

### 6.5.4 マッパー 7（AxROM）

```
 7  bit  0
 ---- ----
 xxxM xPPP
    |  |||
    |  +++- $8000-$FFFF の 32 KiB PRG バンク
    +------ 4 つのネームテーブルすべてに使う 1 KiB VRAM ページ
```

bit 4 が 0 のとき `MirrorSingleA`、1 のとき `MirrorSingleB` とする。CHR は 8 KiB の CHR-RAM とする。電源投入時は `MirrorSingleA` とする。

オーバーサイズの構成（PRG が 256 KiB を超える最大 512 KiB）では bit 3 も PRG バンクに使う。バンク番号はバンク総数で剰余を取るため、256 KiB 以下の構成で bit 3 が立っていても見えるバンクは変わらない。

サブマッパー 0 のときバス競合をエミュレートしない。

### 6.5.5 マッパー 66（GxROM）

```
 7  bit  0
 ---- ----
 xxPP xxCC
   ||   ||
   ||   ++- 8 KiB CHR バンク
   ++------ 32 KiB PRG バンク
```

### 6.5.6 マッパー 1（MMC1）

シリアルポートで設定する。`$8000`–`$FFFF` への書き込みを 5 回集めて 1 つの内部レジスタへ反映する。

```go
type mmc1 struct {
    shiftReg   uint8   // 初期値 0x10。1 が bit 0 に来たら満杯
    control    uint8   // 5 bit
    chrBank0   uint8   // 5 bit
    chrBank1   uint8   // 5 bit
    prgBank    uint8   // 5 bit
    lastWriteCycle uint64   // 連続サイクル書き込みの判定に使う
}

func (m *mmc1) WritePRG(addr uint16, v uint8) {
    if addr < 0x8000 {
        m.writePRGRAM(addr, v)
        return
    }
    if m.cycles == m.lastWriteCycle+1 {
        m.lastWriteCycle = m.cycles
        return                      // 連続サイクルの 2 回目以降は無視する
    }
    m.lastWriteCycle = m.cycles

    if v&0x80 != 0 {
        m.shiftReg = 0x10
        m.control |= 0x0C           // $C000-$FFFF を末尾バンクに固定する
        return
    }
    full := m.shiftReg&1 != 0
    m.shiftReg = (m.shiftReg >> 1) | ((v & 1) << 4)
    if !full {
        return
    }
    value := m.shiftReg & 0x1F
    switch (addr >> 13) & 3 {
    case 0: m.control = value
    case 1: m.chrBank0 = value
    case 2: m.chrBank1 = value
    case 3: m.prgBank = value
    }
    m.shiftReg = 0x10
}
```

アドレスが意味を持つのは 5 回目の書き込みのみであり、そのときも bit 14–13 のみを使う。

`control` の構成を次に示す。

```
 4bit0
 -----
 CPPMM
 |||||
 |||++- ネームテーブル配置（0: 1 画面 A, 1: 1 画面 B, 2: 水平配置, 3: 垂直配置）
 |++--- PRG バンクモード（0,1: 32 KiB 切り替え / 2: $8000 固定 / 3: $C000 固定）
 +----- CHR バンクモード（0: 8 KiB / 1: 4 KiB × 2）
```

`prgBank` の bit 4 は MMC1B では PRG-RAM の有効・無効を表す。1 のとき `$6000`–`$7FFF` の読み出しをオープンバスにする。

PRG が 512 KiB の構成（SUROM）では、`chrBank0` の bit 4 が 256 KiB のブロックを選ぶ。`prgBank` が 4 bit しかないためである。4 KiB の CHR モードでは 2 つの CHR レジスタが別のブロックを示しうるが、この構成を使うカートリッジは CHR-RAM 8 KiB であり、`chrBank0` だけが意味を持つ。

PRG-RAM が 8 KiB を超える構成（SOROM・SXROM）では、`chrBank0` の bit 3-2 が 8 KiB の RAM バンクを選ぶ。

この 2 つの転用は CHR-RAM を載せた基板でのみ行う。CHR-ROM の基板では同じビットが CHR のバンク番号として意味を持ち、転用すると CHR を切り替えるたびに PRG-RAM の見える場所と PRG のブロックが変わる。

サブマッパー 5（SEROM / SHROM / SH1ROM）では `$E000` のレジスタが無機能となり、32 KiB 全体がバンクなしで見える。電源投入時に PRG バンク 0 を選ぶ。

電源投入時は PRG バンクモード 3 とする。

### 6.5.7 マッパー 4（MMC3）

レジスタは 4 対あり、偶数アドレスが下位、奇数アドレスが上位である。

| アドレス | 内容 |
|---|---|
| `$8000` 偶数 | バンク選択。bit 2–0 が更新対象、bit 6 が PRG モード、bit 7 が CHR A12 反転 |
| `$8001` 奇数 | バンクデータ |
| `$A000` 偶数 | ネームテーブル配置。bit 0 が 0 で `MirrorVertical`、1 で `MirrorHorizontal` |
| `$A001` 奇数 | PRG-RAM 保護。bit 6 が書き込み保護、bit 7 がチップイネーブル |
| `$C000` 偶数 | IRQ ラッチ |
| `$C001` 奇数 | IRQ リロード要求 |
| `$E000` 偶数 | IRQ 無効化と acknowledge |
| `$E001` 奇数 | IRQ 有効化 |

CHR のバンク割り当てを次に示す。

| PPU アドレス | `$8000` bit 7 = 0 | `$8000` bit 7 = 1 |
|---|---|---|
| `$0000`–`$03FF` | R0 | R2 |
| `$0400`–`$07FF` | R0 | R3 |
| `$0800`–`$0BFF` | R1 | R4 |
| `$0C00`–`$0FFF` | R1 | R5 |
| `$1000`–`$13FF` | R2 | R0 |
| `$1400`–`$17FF` | R3 | R0 |
| `$1800`–`$1BFF` | R4 | R1 |
| `$1C00`–`$1FFF` | R5 | R1 |

R0 と R1 は 2 KiB バンクであり、バンク番号の最下位ビットを無視する。

PRG のバンク割り当てを次に示す。

| CPU アドレス | `$8000` bit 6 = 0 | `$8000` bit 6 = 1 |
|---|---|---|
| `$8000`–`$9FFF` | R6 | 末尾から 2 番目 |
| `$A000`–`$BFFF` | R7 | R7 |
| `$C000`–`$DFFF` | 末尾から 2 番目 | R6 |
| `$E000`–`$FFFF` | 末尾 | 末尾 |

R6 と R7 は上位 2 bit を無視する。

IRQ の動作を次のとおり実装する。

```go
func (m *mmc3) NotifyPPUAddress(addr uint16) {
    if !m.a12.notify(addr, m.cyclesSinceLast) {
        return
    }
    if m.irqCounter == 0 || m.irqReload {
        m.irqCounter = m.irqLatch
        m.irqReload = false
    } else {
        m.irqCounter--
    }
    if m.irqCounter == 0 && m.irqEnabled {
        m.irqAsserted = true
    }
}
```

`$C000` への書き込みはカウンタの現在値を変えない。リロード時にのみ使う。`$C001` への書き込みはカウンタを 0 にし、リロードフラグを立てる。`$E000` への書き込みは IRQ 生成を止めるが、カウンタは動き続ける。

`$C000` が 0 のときの挙動に 2 種類ある。設定 `emulation.mmc3IrqVariant` で選ぶ。

| 設定値 | 挙動 |
|---|---|
| `sharp` | カウンタが 0 に等しいときに IRQ を出す。毎スキャンライン IRQ が発生する |
| `nec` | カウンタが 0 にデクリメントされたときに IRQ を出す。1 回だけ発生する |

既定値は `sharp` とする。

PRG-RAM 保護レジスタ（`$A001`）は実装する。bit 7 が 0 のとき `$6000`–`$7FFF` の読み出しをオープンバスにする。bit 6 が 1 のとき書き込みを無視する。電源投入時は bit 7 を 1、bit 6 を 0 とする。`$A001` へ書かずに PRG-RAM を使うプログラムがあるためである。

`nec` では、カウンタが自然に 0 に達したあとのリロードで IRQ を出さない。`$C001` への書き込みによるリロードでは、カウンタがすでに 0 であっても出す。

## 6.6 バス競合

ディスクリートロジックのマッパーでは、`$8000`–`$FFFF` への書き込み時に PRG-ROM も同じバスへ値を出す。実効値は「書いた値 AND その番地の ROM の内容」となる。

```go
type busConflictMode uint8

const (
    busConflictNone busConflictMode = iota
    busConflictAND
)

func (c *conflictLayer) WritePRG(addr uint16, v uint8) {
    if c.mode == busConflictAND && addr >= 0x8000 {
        v &= c.inner.peekPRG(addr)
    }
    c.inner.WritePRG(addr, v)
}
```

この層をマッパーの外側に置く。マッパー本体はバス競合を意識しない。

モードの決定を次のとおり行う。

| 条件 | モード |
|---|---|
| NES 2.0 のサブマッパーが AND 型を示す | `busConflictAND` |
| NES 2.0 のサブマッパーが競合なしを示す | `busConflictNone` |
| マッパー 7 のサブマッパー 0 | `busConflictNone` |
| それ以外 | `busConflictNone` |

マッパー 2・3・7 のサブマッパー 1 が競合なし、2 が AND 型を示す。サブマッパー 0 は「どちらか不明」であり、競合を再現しない。再現すると、回避していないプログラムが動かなくなる。

設定 `emulation.busConflicts` で `auto`・`always`・`never` を選べる。既定値は `auto` とする。`always` を選んでも、ディスクリートロジックのマッパー（2・3・7・66）以外では競合を再現しない。MMC1 と MMC3 はレジスタがデータバスを駆動しないため、競合が起こらない。

## 6.7 不揮発メモリ

`HasBattery` が true のとき、PRG-RAM の内容をファイルに保存する。保存先は「11 設定と CLI 設計」§11.2 に定める。

保存の契機を次に示す。

| 契機 | 処理 |
|---|---|
| PRG-RAM の内容が変わってから 3 秒間変化がない | ファイルへ書き出す |
| ROM を閉じるとき | ファイルへ書き出す |
| アプリケーションを終了するとき | ファイルへ書き出す |

変化の検出は内容の比較で行う。フレームが完成するたびに、前回比較した内容と現在の内容を比べる。カートリッジに「書かれた」ことを知らせる仕組みを持たせないのは、`internal/nes` に時刻を持ち込まず、バスの書き込み経路に保存のための分岐を入れないためである。1 フレームに 1 回 8 KiB から 32 KiB を比較する費用は十分に小さい。

ROM を閉じるときは、最後の比較から後の変化も拾ってから書き出す。1 フレームも完成しないうちに閉じる場合があり、比較を待つと直前の書き込みが失われる。

書き出しは一時ファイルへ書いて `rename` する。処理は `internal/emu` が行う（`internal/emu/battery.go`）。

初回のファイルが存在しないとき、PRG-RAM を `$00` で埋める。

## 6.8 CHR と PRG のオーバーレイ

デバッガのタイル編集とプログラムのパッチを、ROM ファイルを書き換えずに実現する。

```go
type Overlay struct {
    enabled bool
    prg     []Patch          // PRG-ROM の変更。オフセットの昇順
    chr     []Patch          // CHR-ROM の変更。オフセットの昇順
    prgData, chrData []uint8 // ROM.PRG と ROM.CHR。マッパーが読む記憶域そのもの
    prgOrig, chrOrig []uint8 // 最初の変更の時点で取った元の内容
}

func (r *ROM) Overlay() *Overlay

func (o *Overlay) SetPRG(offset int, v uint8) error
func (o *Overlay) SetCHR(offset int, v uint8) error
func (o *Overlay) SetEnabled(on bool)
func (o *Overlay) Clear()
func (o *Overlay) Patches() (prg, chr []Patch) // オフセットの昇順
func (o *Overlay) Load(prg, chr []Patch, enabled bool) error
func (o *Overlay) Hash() [8]uint8
```

オーバーレイは値を `ROM.PRG` と `ROM.CHR` へ直接書き込んで反映する。マッパーの `banked` はこの 2 つのスライスを共有しているため、`ReadPRG` と `ReadCHR` は何もしなくても変更後の値を返す。読み出しのたびに変更の一覧を引かないのは、CHR の読み出しが毎秒 100 万回を超え、通常のプレイを遅くするためである。

最初の変更の時点で `ROM.PRG` と `ROM.CHR` の写しを取り、元の内容とする。無効にしたときは変更した位置へ元の内容を書き戻し、有効に戻したときはマップの値を書き直す。変更の一覧は有効・無効によらず保持する。一覧を `map` ではなくオフセットの昇順に並べたスライスで持つのは、`internal/nes` が `map` をたどらない規約（「12 テスト設計」§12.7）を保つためである。変更は利用者の操作でしか起こらず、挿入の位置を二分探索で求める費用は問題にならない。CHR-RAM のカートリッジは CHR のオーバーレイを持たない。CHR-RAM は実機でも書き換えられるため、編集は CHR-RAM へ直接書く。

`Hash` はオーバーレイがエミュレーションへ与える影響を表す FNV-1a のハッシュである。無効のときと変更が無いときはすべて 0 を返す。有効のときは PRG と CHR の変更をオフセットの昇順に並べてハッシュを取る。

オーバーレイは別ファイル（「11 設定と CLI 設計」§11.2 の `patches/<rom-hash>.json`）へ保存する。ROM ファイル自体を書き換えない。書き出すときはオフセットの昇順に並べる。変更が無く有効のときはファイルを置かない。ファイルの読み書きは `internal/emu` が行い、読み込みは電源を入れる前に行う。リセットベクタを書き換えたパッチも電源投入から効かせるためである。

## 6.9 保存する状態

| 項目 |
|---|
| PRG-RAM の全内容 |
| CHR-RAM の全内容 |
| マッパー番号とサブマッパー（検証用） |
| 各マッパーのレジスタ |
| MMC1: `shiftReg`, `control`, `chrBank0`, `chrBank1`, `prgBank`, `lastWriteCycle` |
| MMC3: バンク選択, R0–R7, PRG-RAM 保護, `irqLatch`, `irqCounter`, `irqReload`, `irqEnabled`, `irqAsserted`, `a12Filter` の `prevHigh` と `lowDots`, `lastNotifyDot` |

PRG モード・CHR A12 反転・ミラーリングはバンク選択とミラーリングのレジスタから復元する。ウィンドウへの割り当ても同じくレジスタから計算し直す。

各マッパーの状態は `regs` と `common` の 2 つのセクションに分けて書く。レジスタを素のバイト列として並べると、セクションの並びとして読めるかどうかが値に依存し、ステートの構造を機械的にたどれなくなる。

`a12Filter` の状態を保存しないと、復元直後のスキャンライン IRQ が 1 行ずれる。

オーバーレイは保存しない。ROM の一部に近い性質を持ち、セーブステートより寿命が長いためである。ステートのヘッダにオーバーレイのハッシュを入れ、ロード時に現在のハッシュと比べる。異なれば `warn.compat` にログを出し、ロードは続ける（「08 セーブステートと入力ムービー設計」§8.2.2）。
