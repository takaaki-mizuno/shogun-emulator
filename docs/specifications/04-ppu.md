# 04 PPU 設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 4.1 実装方式

PPU は 1 ドットを単位とするステートマシンとして実装する。`Step` の 1 回の呼び出しが 1 PPU ドットに対応する。

```go
package ppu

// Step は 1 ドット進める。バスから 1 CPU サイクルごとに
// リージョンの比に応じた回数呼ばれる。
func (p *PPU) Step()
```

ドット単位とするのは、画面途中でのスクロール変更、スプライト 0 ヒットの発生位置、VBlank フラグの設定と読み出しの競合、マッパーの A12 監視が、いずれも 1 ドットの精度を要するためである。

## 4.2 型定義

```go
type PPU struct {
    region *region.Region
    bus    Bus       // CHR とネームテーブルへのアクセス。カートリッジ経由

    // 外部レジスタの内容
    ctrl  Control    // $2000
    mask  Mask       // $2001
    status Status    // $2002
    oamAddr uint8    // $2003

    // 内部レジスタ
    v uint16  // 15 bit
    t uint16  // 15 bit
    x uint8   // 3 bit（fine X）
    w bool    // 書き込みトグル

    readBuffer uint8   // $2007 のリードバッファ
    ioLatch    uint8   // I/O バスの動的ラッチ

    // 走査位置
    scanline int    // 0..261
    dot      int    // 0..340
    oddFrame bool

    // 背景のフェッチ結果とシフタ
    ntLatch, atLatch, bgLoLatch, bgHiLatch uint8
    bgShiftLo, bgShiftHi uint16
    atShiftLo, atShiftHi uint8
    atLatchLo, atLatchHi bool

    // スプライト
    oam       [256]uint8
    secondary [32]uint8
    sprites   [8]spriteUnit
    spriteCount int
    sprite0OnNext bool
    sprite0OnCurrent bool
    eval evalState

    // メモリ
    palette [32]uint8
    ciram   [4096]uint8   // 4 画面構成でも足りる容量を確保する

    // リセット
    warmupDots uint32   // 0 になるまで一部レジスタへの書き込みを無視する
    nmiLine bool

    // I/O ラッチの減衰
    latchExpiry    [8]uint64   // ビットごとの保持の期限
    minLatchExpiry uint64      // 毎ドットの走査を避けるための最小値
    dots           uint64      // 電源投入からの累積ドット数

    busAddr        uint16  // フェッチの 1 ドット目に出したアドレス
    suppressVBlank bool    // VBlank フラグのセットを 1 回飛ばす
    skipDot        bool    // プリレンダー行の末尾の 1 ドットを飛ばす

    frame *video.Frame
    queue *video.Queue
}

type spriteUnit struct {
    patternLo, patternHi uint8
    attr                 uint8
    xCounter             uint8
    active               bool
}

type evalState struct {
    n, m        int
    writeDisable bool
    phase       uint8
    latch       uint8
}
```

`ctrl`・`mask`・`status` はビットフィールドを名前付きメソッドで読む型とする。呼び出し側でシフトとマスクを書かない。

```go
type Control uint8

func (c Control) NametableSelect() uint16 { return uint16(c&0x03) << 10 }
func (c Control) VRAMIncrement() uint16   { if c&0x04 != 0 { return 32 }; return 1 }
func (c Control) SpritePatternBase() uint16
func (c Control) BGPatternBase() uint16
func (c Control) SpriteHeight() int        // 8 または 16
func (c Control) NMIEnabled() bool

type Mask uint8

func (m Mask) Greyscale() bool
func (m Mask) ShowBGLeft() bool
func (m Mask) ShowSpritesLeft() bool
func (m Mask) BGEnabled() bool
func (m Mask) SpritesEnabled() bool
func (m Mask) RenderingEnabled() bool     // BGEnabled または SpritesEnabled
func (m Mask) Emphasis(r *region.Region) uint8
```

## 4.3 アドレス空間

| アドレス範囲 | 割り当て |
|---|---|
| `$0000`–`$1FFF` | カートリッジの CHR |
| `$2000`–`$2FFF` | ネームテーブル。カートリッジがミラーリングを決める |
| `$3000`–`$3EFF` | `$2000`–`$2EFF` のミラー |
| `$3F00`–`$3FFF` | パレット RAM。`addr & 0x1F` で畳む |

パレット RAM のアクセスでは、下位 2 bit が 0 のエントリ（`$3F00`, `$3F04`, `$3F08`, `$3F0C`）と対応するスプライト側（`$3F10`, `$3F14`, `$3F18`, `$3F1C`）が同一の記憶域を指す。

```go
func paletteIndex(addr uint16) int {
    i := int(addr & 0x1F)
    if i&0x13 == 0x10 {   // $3F10, $3F14, $3F18, $3F1C
        i &= 0x0F
    }
    return i
}
```

ネームテーブル領域のアクセスはカートリッジに委譲する。カートリッジが CIRAM・カートリッジ側 VRAM・CHR-ROM のいずれに向けるかを決める。

```go
type Bus interface {
    ReadCHR(addr uint16) uint8
    WriteCHR(addr uint16, v uint8)
    MapNametable(addr uint16) cart.NametableTarget
    NotifyPPUAddress(addr uint16, dot uint64)
}
```

`NotifyPPUAddress` の `dot` は電源投入からの PPU のドット数である。マッパーが A12 の low の継続時間をドット単位で測る（「06 カートリッジとマッパー」§6.4）。

## 4.4 レジスタアクセス

CPU 側から見えるレジスタは 8 個であり、`$2008`–`$3FFF` に 8 バイトごとにミラーされる。バスが `addr & 0x0007` を渡す。

### 4.4.1 I/O ラッチ

どのポートへの書き込みでも `ioLatch` を更新する。読み出し可能なポートの読み出しでも更新する。書き込み専用ポートの読み出しは `ioLatch` の現在値を返し、ラッチを駆動しない。

I/O バスは動的ラッチであり、駆動されないと電荷が抜けて 0 に戻る。減衰はビットごとに独立している。ビットごとに保持の期限を持ち、期限を過ぎたビットを 0 にする。保持時間は 36 フレーム分とする。

| アクセス | 駆動されるビット |
|---|---|
| どのポートへの書き込み | 全 8 bit |
| `$2002` の読み出し | 上位 3 bit のみ |
| `$2004` の読み出し | 全 8 bit |
| `$2007` の読み出し（パレット以外） | 全 8 bit |
| `$2007` の読み出し（パレット） | 下位 6 bit のみ |
| 書き込み専用ポートの読み出し | なし |

`$2002` が下位 5 bit を駆動しないことを `ppu_open_bus` が検証する。読み出しで全ビットを駆動する実装では、読み続けるだけで保持が続き減衰が観測できない。

| ポート | 読み出し | 書き込み |
|---|---|---|
| `$2000` PPUCTRL | `ioLatch` | `ctrl` を更新し、`t` の bit 10-11 を書き換える |
| `$2001` PPUMASK | `ioLatch` | `mask` を更新する |
| `$2002` PPUSTATUS | 上位 3 bit を `status` から、下位 5 bit を `ioLatch` から合成。VBlank をクリアし `w` を false にする | `ioLatch` のみ更新 |
| `$2003` OAMADDR | `ioLatch` | `oamAddr` を更新する |
| `$2004` OAMDATA | `oam[oamAddr]` | §4.4.3 |
| `$2005` PPUSCROLL | `ioLatch` | §4.4.2 |
| `$2006` PPUADDR | `ioLatch` | §4.4.2 |
| `$2007` PPUDATA | §4.4.4 | VRAM へ書き、`v` を増やす |

`warmupDots` が 0 でない間、`$2000`・`$2001`・`$2005`・`$2006` への書き込みを無視する。`w` のトグルも行わない。`$2002`・`$2003`・`$2004`・`$2007` は即座に動作する。

### 4.4.2 スクロールレジスタの転送

`$2000`・`$2005`・`$2006` の書き込みと `$2002` の読み出しによる `v` / `t` / `x` / `w` の変化を次のとおり実装する。`d` は書き込まれた値である。

| 契機 | 転送 |
|---|---|
| `$2000` 書き込み | `t = (t & 0xF3FF) \| (uint16(d&0x03) << 10)` |
| `$2002` 読み出し | `w = false` |
| `$2005` 1 回目（`w == false`） | `t = (t & 0xFFE0) \| uint16(d>>3)` / `x = d & 0x07` / `w = true` |
| `$2005` 2 回目（`w == true`） | `t = (t & 0x8C1F) \| (uint16(d&0x07) << 12) \| (uint16(d&0xF8) << 2)` / `w = false` |
| `$2006` 1 回目（`w == false`） | `t = (t & 0x00FF) \| (uint16(d&0x3F) << 8)` / `w = true` |
| `$2006` 2 回目（`w == true`） | `t = (t & 0xFF00) \| uint16(d)` / `v = t` / `w = false` |

`$2006` の 1 回目の書き込みで `t` の bit 14 は 0 になる。上の式で `d & 0x3F` としているのがこれに対応する。

レンダリング中の `v` の自動更新を次のとおり行う。`mask.RenderingEnabled()` が true のときのみ実行する。

| タイミング | 処理 |
|---|---|
| 各スキャンラインの dot 256 | `incrementY` |
| 各スキャンラインの dot 257 | `v = (v & 0x7BE0) \| (t & 0x041F)`（水平成分をコピー） |
| プリレンダー行の dot 280–304 | `v = (v & 0x041F) \| (t & 0x7BE0)`（垂直成分をコピー） |
| dot 328, 336、および次行の 8, 16, …, 248, 256 | `incrementX` |

```go
func (p *PPU) incrementX() {
    if v := p.v & 0x001F; v == 31 {
        p.v &= ^uint16(0x001F)
        p.v ^= 0x0400
    } else {
        p.v++
    }
}

func (p *PPU) incrementY() {
    if p.v&0x7000 != 0x7000 {
        p.v += 0x1000
        return
    }
    p.v &= ^uint16(0x7000)
    y := (p.v & 0x03E0) >> 5
    switch y {
    case 29:
        y = 0
        p.v ^= 0x0800
    case 31:
        y = 0
    default:
        y++
    }
    p.v = (p.v & ^uint16(0x03E0)) | (y << 5)
}
```

### 4.4.3 OAMDATA

レンダリング中（プリレンダー行と可視行、かつ `mask.RenderingEnabled()`）の `$2004` への書き込みは OAM を変更しない。この期間の書き込みを無視する。

レンダリング中の `$2004` の読み出しは、スプライト評価が参照している値を返す。dot 1–64 の期間は `0xFF` を返す。

`oamAddr` はレンダリング中の dot 257–320 で 0 にリセットされる。

属性バイト（`oamAddr & 0x03 == 2`）の bit 2–4 に対応する記憶素子が存在しない。読み出すと常に 0 になる。

### 4.4.4 PPUDATA の読み出し

`$2007` の読み出しは内部リードバッファの内容を返し、そのあとでバッファを更新する。読み出しは 1 回分遅延する。

```go
func (p *PPU) readData() uint8 {
    addr := p.v & 0x3FFF
    var out uint8
    if addr >= 0x3F00 {
        out = p.palette[paletteIndex(addr)] & 0x3F
        if p.mask.Greyscale() {
            out &= 0x30
        }
        out |= p.ioLatch & 0xC0
        p.readBuffer = p.readVRAM(addr)   // 下敷きのメモリを読む
    } else {
        out = p.readBuffer
        p.readBuffer = p.readVRAM(addr)
    }
    p.incrementVRAMAddress()
    return out
}
```

パレット領域の読み出しは即座に値を返し、上位 2 bit に `ioLatch` を混ぜる。同時に下敷きのメモリを読んでバッファへ入れる。

レンダリング中に `$2007` へアクセスした場合、`incrementVRAMAddress` の代わりに `incrementX` と `incrementY` を同時に実行する。

## 4.5 フレームタイミング

1 フレームは `region` が定めるスキャンライン構成に従う。NTSC では 262 スキャンライン、PAL と Dendy では 312 スキャンラインで、いずれも各 341 ドットである。可視スキャンラインは 3 機種とも 240 行であり、差は VBlank とポストレンダーの行数にある。

| スキャンライン | 処理 |
|---|---|
| 0–239（可視） | 背景フェッチ、ピクセル出力、スプライト評価、スプライトフェッチ |
| 240（ポストレンダー） | 何もしない |
| 241 dot 1 | VBlank フラグをセットし、`ctrl.NMIEnabled()` なら NMI 線をアサートする |
| 241–260（VBlank） | メモリアクセスを行わない |
| 261（プリレンダー） | dot 1 で 3 つのフラグをクリア。背景フェッチを行う。dot 280–304 で垂直成分をコピー |

`mask.RenderingEnabled()` が true の奇数フレームでは、プリレンダー行の dot 339 の次を dot 0 のスキャンライン 0 とし、1 ドットを飛ばす。`oddFrame` はレンダリングの有無に関係なく毎フレーム反転する。

飛ばすかどうかの判定はプリレンダー行の dot 338 で確定させ、`skipDot` に保持する。飛ばすドットの直前で判定すると、`$2001` への書き込みに対して 1 ドット遅れる。`ppu_vbl_nmi` の `10-even_odd_timing` がこの位置を検証する。

### 4.5.1 可視スキャンラインのドット割り当て

| ドット | 処理 |
|---|---|
| 0 | 何もしない |
| 1–256 | 背景フェッチ（8 ドットで 4 アクセス）、ピクセル出力 |
| 65–256 | スプライト評価（次行分） |
| 257–320 | スプライトフェッチ（8 スプライト × 8 ドット）、`oamAddr = 0` |
| 321–336 | 次行の最初の 2 タイルをフェッチ |
| 337–340 | ネームテーブルバイトを 2 回フェッチ |

背景フェッチの 8 ドット周期は次のとおりである。1 回のアクセスは 2 ドットを要する。

| 相対ドット | アクセス | アドレス |
|---|---|---|
| 1–2 | ネームテーブル | `0x2000 \| (v & 0x0FFF)` |
| 3–4 | 属性テーブル | `0x23C0 \| (v & 0x0C00) \| ((v >> 4) & 0x38) \| ((v >> 2) & 0x07)` |
| 5–6 | パターン下位 | `ctrl.BGPatternBase() \| (nt << 4) \| ((v >> 12) & 7)` |
| 7–8 | パターン上位 | 同上 + 8 |

属性テーブルを読んだ 2 ドット目に、読んだ 1 バイトから 4×4 タイルのうちのどの 2×2 に当たる 2 bit かを選び、`atLatch` にはその 2 bit を入れる。選択には属性のアドレスを作ったのと同じ `v` の coarse X の bit 1 と coarse Y の bit 1 を使う（シフト量 `((v >> 4) & 4) | (v & 2)`）。シフタへ転送する dot 9, 17, … の時点では、dot 8, 16, … の coarse X の加算で `v` が次のタイルへ進んでいる。転送の時点の `v` で選ぶと、coarse X の bit 1 が変わるタイルで隣の 2×2 の属性を使い、パレットが 8 ピクセルずれる。

アクセスの 1 ドット目にアドレスをバスへ出し、`bus.NotifyPPUAddress` を呼ぶ。2 ドット目に値を読む。この 2 段構成により、マッパーが監視する A12 の遷移が実機と同じ回数・同じタイミングで発生する。

レンダリングのフェッチ以外に、`$2006` の 2 回目の書き込みと `$2007` のアクセスでもアドレスをバスへ出す。`$2007` ではアクセスするアドレスと、更新後の `v` の 2 回呼ぶ。レンダリングを止めている間に `$2006` と `$2007` でスキャンラインカウンタを進めるプログラムがある。この経路では `busAddr` を変えない。レンダリングのフェッチが 2 ドット目に読む先を変えないためである。

dot 9, 17, 25, …, 257 でラッチの内容をシフタへ転送する。転送はその dot のシフトより前に行う。転送はシフタの下位 8 bit へ入れ、ピクセルは上位 8 bit から選ぶ。順序を逆にすると画面が 1 ピクセルずれる。スプライト 0 ヒットが背景との重なりを 1 ピクセル単位で見るため、`sprite_hit_tests` の `02.alignment`・`03.corners`・`04.flip` がこの順序を検証する。

## 4.6 ピクセルの生成

各可視ドットで背景ピクセルとスプライトピクセルを求め、優先度で選択する。

```go
func (p *PPU) renderPixel() {
    bg := p.backgroundPixel()     // 4 bit。下位 2 bit がパターン値
    sp, spAttr, spIndex := p.spritePixel()

    idx := p.multiplex(bg, sp, spAttr)     // 5 bit
    p.detectSprite0Hit(bg, sp, spIndex)
    p.frame.Set(p.dot-1, p.scanline, p.paletteValue(idx))
}
```

優先度の決定表を次に示す。

| 背景 | スプライト | スプライトの優先度ビット | 出力 |
|---|---|---|---|
| 0 | 0 | – | `$3F00` |
| 0 | 1–3 | – | スプライト |
| 1–3 | 0 | – | 背景 |
| 1–3 | 1–3 | 0 | スプライト |
| 1–3 | 1–3 | 1 | 背景 |

スプライト同士の優先度は OAM 上のインデックスで決まる。若いインデックスが優先される。優先度の選択は背景との比較より前に行う。この順序により、背面指定の若いスプライトが前面指定の後続スプライトより先に選ばれる。

`mask.ShowBGLeft()` が false のとき x = 0–7 の背景ピクセルを透明として扱う。`mask.ShowSpritesLeft()` が false のとき同区間のスプライトピクセルを透明として扱う。

`mask.RenderingEnabled()` が false のとき、`v & 0x3FFF` がパレット領域を指していればそのアドレスの色を出力し、そうでなければ `$3F00` を出力する。

### 4.6.1 パレット値の生成

フレームバッファにはパレットインデックスとエンファシスを保持する。RGB への変換は表示直前に行う。

```go
func (p *PPU) paletteValue(idx uint8) uint16 {
    v := uint16(p.palette[idx] & 0x3F)
    if p.mask.Greyscale() {
        v &= 0x30
    }
    return v | (uint16(p.mask.Emphasis(p.region)) << 6)
}
```

### 4.6.2 スプライト 0 ヒット

`sprite0OnCurrent` が true で、スプライト出力ユニット 0 が不透明値を出力し、背景も不透明値を出力したときにフラグをセットする。

セットしない条件を次に示す。

| 条件 |
|---|
| `mask.BGEnabled()` または `mask.SpritesEnabled()` が false |
| 左端クリッピングが有効な x = 0–7 |
| x = 255 |
| 当該フレームで既にセット済み |

`sprite0OnNext` はスプライト評価がスプライト 0 を範囲内と判定したときに立てる。スキャンラインの開始時に `sprite0OnCurrent` へ移す。2 つのフラグに分けるのは、評価が次行の判定を行っている最中に現在行のヒット判定が走るためである。

## 4.7 スプライト評価

dot 1–64 で secondary OAM を `0xFF` で初期化する。dot 65–256 で評価を行う。奇数ドットで primary OAM から読み、偶数ドットで secondary OAM へ書く。

```go
// 評価のアルゴリズム。docs/research/03_ppu.md の 7.2 節 の手順に従う。
//  1. OAM[n][0] を Y として読み、空きスロットへコピーする
//     1a. 範囲内なら残り 3 バイトもコピーする
//  2. n をインクリメントする
//     2a. n が 0 に戻ったら 4 へ
//     2b. 8 個未満なら 1 へ
//     2c. ちょうど 8 個なら writeDisable を立てて 3 へ
//  3. OAM[n][m] を Y として評価する
//     3a. 範囲内ならオーバーフローフラグを立て、次の 3 エントリを読む
//     3b. 範囲外なら n と m をキャリーなしでインクリメントする
//  4. コピーを試みて失敗し、n をインクリメントする
```

手順 3b で `m` もインクリメントする。この結果、OAM を斜めに走査してタイル番号・属性・X 座標を Y 座標として評価する。この挙動によりオーバーフローフラグに偽陽性と偽陰性が生じる。

手順 4 に入った後は手順 1 へ戻らない。スプライト番号を進めるだけを HBLANK まで繰り返す。手順 1 へ戻す実装では、64 個を見終えた後に評価が再開してオーバーフローフラグが余分に立つ。`sprite_overflow_tests` の `2.Details`・`3.Timing`・`4.Obscure`・`5.Emulator` がこれを検証する。

手順 3b でスプライト番号が 63 を越えたときに手順 4 へ移る。8 bit のアドレスで計算すると桁上がりが失われ、番号が 0 に戻ったことを判定できない。

`writeDisable` が立っている間、secondary OAM への書き込みは読み出しに変わる。読まれる値は secondary OAM の先頭スプライトの Y 座標である。

プリレンダー行ではスプライト評価を行わない。この結果、スキャンライン 0 にスプライトが描かれない。

### 4.7.1 スプライトフェッチ

dot 257–320 で 8 個分のフェッチを行う。1 スプライトあたり 8 ドットで 4 アクセスを行う。

| 相対ドット | アクセス |
|---|---|
| 1–2 | ネームテーブル（値を使わない） |
| 3–4 | ネームテーブル（値を使わない）。1 ドット目に属性、2 ドット目に X 座標を secondary OAM からロードする |
| 5–6 | パターン下位 |
| 7–8 | パターン上位 |

範囲内のスプライトが 8 個未満のとき、残りのスロットはタイル `$FF` へのフェッチを行い、結果を捨てて透明な値をロードする。このフェッチもバスに現れるため `NotifyPPUAddress` を呼ぶ。

8×16 スプライトのパターンアドレスは、タイル番号の bit 0 でパターンテーブルを選び、bit 7–1 で上半分のタイルを選ぶ。垂直反転時は 2 つの副タイルをそれぞれ反転し、位置を入れ替える。

## 4.8 VBlank フラグと NMI

| タイミング | 処理 |
|---|---|
| scanline 241, dot 1 | VBlank フラグをセットする。`ctrl.NMIEnabled()` なら NMI 線をアサートする |
| scanline 261, dot 1 | VBlank・スプライト 0 ヒット・オーバーフローの 3 フラグをクリアし、NMI 線を解除する |
| `$2002` 読み出し | VBlank フラグを読んでクリアする |
| `$2000` 書き込みで NMI を 0→1 | VBlank フラグが立っていれば NMI 線をアサートする |

`$2002` の読み出しが VBlank フラグのセットと同一ドットまたはその前後 1 ドットに起きたときの挙動を次のとおり実装する。

| 読み出しのタイミング | 返り値 | NMI |
|---|---|---|
| セットの 1 ドット前 | 0 | そのフレームは発生しない |
| セットと同一ドット、または 1 ドット後 | 1 | そのフレームは抑止される |
| 2 ドット以上離れている | 通常 | 通常 |

読み出しが起きたドットの求め方を次に定める。`dot` は「次に処理するドット」を指す。CPU の読み出しはサイクルの末尾に起こるため、読み出しが起きたドットは `dot - 1` である。したがって「セットの 1 ドット前」は `dot == 1` のときになる。この場合だけ、そのフレームのセットを飛ばすフラグを立てる。

同一ドットおよび 1 ドット後の NMI の抑止は、専用の処理を持たない。読み出しが VBlank フラグをクリアすると NMI 線が戻り、CPU がサイクルの末尾で線を採取する前に解除されるためである。

## 4.9 PPU から見えるカートリッジ

ネームテーブルのアクセス先はカートリッジが決める。

```go
package cart

type NametableKind uint8

const (
    NametableCIRAM NametableKind = iota  // 本体の CIRAM
    NametableCart                        // カートリッジ側 VRAM
    NametableCHR                         // CHR-ROM をネームテーブルに割り当てる
)

type NametableTarget struct {
    Kind   NametableKind
    Offset uint32
}
```

ミラーリングの解決表を次に示す。`addr` は `$2000`–`$2FFF` の範囲である。

| 配置 | CIRAM のオフセット |
|---|---|
| 水平ミラーリング（垂直配置） | `(addr & 0x03FF) \| ((addr & 0x0800) >> 1)` |
| 垂直ミラーリング（水平配置） | `addr & 0x07FF` |
| 1 画面 A | `addr & 0x03FF` |
| 1 画面 B | `(addr & 0x03FF) \| 0x0400` |
| 4 画面 | `addr & 0x0FFF`（カートリッジ側 VRAM） |

## 4.10 電源投入とリセット

| 項目 | 電源投入時 | リセット後 |
|---|---|---|
| `ctrl`, `mask` | 0 | 0 |
| `status` の VBlank | `InitState.PPUVBlankFlag` | 変更しない |
| `oamAddr` | 0 | 変更しない |
| `v` | 0 | 変更しない |
| `t`, `x`, `w` | 0 | 0 |
| `readBuffer` | 0 | 0 |
| `oam`, `palette`, `ciram` | `InitState` に従う | 変更しない |
| `warmupDots` | 約 29658 CPU サイクル相当のドット数 | 同じ値 |
| `scanline`, `dot` | 0, 0 | 0, 0 |

`warmupDots` は CPU サイクルではなく PPU ドットで数える。リージョンごとの値は `docs/research/03_ppu.md` の 11 節の値を CPU サイクルから換算する。

### 4.10.1 デバッガ向けのスクロール位置

```go
// FrameScroll はそのフレームの描画を始めたときのスクロール位置を返す。
func (p *PPU) FrameScroll() (v uint16, fineX uint8)
```

プリレンダー行のドット 304 で `v` と `x` を記録する。ドット 280–304 の垂直方向のコピーを終えた時点であり、レンダリング中はこの時点の `v` が画面の左上に描くタイルを指す。レンダリングが無効のときは `t` を記録する。フレームの終わりの `v` を使わないのは、240 行分の垂直方向の増加で縦のネームテーブル選択が反転しているためである。ネームテーブルビューアのスクロール枠に使う（「09 デバッガ設計」§9.4.2）。

## 4.11 保存する状態

§4.2 の `PPU` 構造体のうち、`region`・`bus`・`frame`・`queue` と、§4.10.1 のスクロール位置の記録を除く全フィールドを保存する。スクロール位置の記録はデバッガの表示にしか使わず、次のフレームで記録し直すためである。

保存漏れが生じやすい項目を次に挙げる。

| 項目 | 省いたときの症状 |
|---|---|
| `eval`（n, m, writeDisable, phase, latch） | オーバーフローフラグをタイミング源に使うゲームで復元後の挙動が変わる |
| `sprite0OnNext`, `sprite0OnCurrent` | 復元直後のスキャンラインでスプライト 0 ヒットを取りこぼす |
| `bgShiftLo`, `bgShiftHi`, `atShiftLo`, `atShiftHi` | 復元直後の 1 タイル分の描画が崩れる |
| `oddFrame` | 1 ドットスキップの位相がずれ、ドットクロールの見え方が変わる |
| `ioLatch` | 書き込み専用レジスタの読み出し結果が変わる |
| `warmupDots` | 起動直後のステートを復元したときにレジスタ書き込みの扱いが変わる |
| `latchExpiry`, `dots` | 書き込み専用レジスタの読み出し結果が変わる |
| `skipDot` | 復元直後のフレームの長さが 1 ドットずれる |
