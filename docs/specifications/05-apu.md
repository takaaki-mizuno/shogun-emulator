# 05 APU 設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 5.1 実装方式

APU は 1 CPU サイクルを単位として進める。`Step` の 1 回の呼び出しが 1 CPU サイクルに対応する。

```go
package apu

// Step は 1 CPU サイクル進める。バスから 1 CPU サイクルごとに 1 回呼ばれる。
func (a *APU) Step()

// Output はミキサーの出力を受け取る先。1 CPU サイクルごとに 1 回呼ばれる。
type Output interface {
    WriteSample(v float32)
}

// SetOutput は出力先を設定する。nil のとき呼ばない。
func (a *APU) SetOutput(o Output)
```

出力先を nil にできるようにするのは、音を出さない実行（テスト ROM ランナー）でリサンプラを用意させないためである。

CPU サイクル単位とするのは、Triangle のタイマーが CPU クロックでティックすること、DMC の DMA 要求が CPU サイクル単位で発生すること、`$4017` 書き込みによるフレームカウンタのリセットが 3 または 4 CPU サイクル後であることによる。

APU サイクル（2 CPU サイクル）を単位とする処理は `evenCycle` で判定する。電源投入時の `evenCycle` を true とし、2 番目の CPU サイクルが APU サイクルになるようにする。この位相でのみ `blargg_apu_2005.07.30` の `01.len_ctr` と `08.irq_timing` が同時に合格する。

バスは 1 CPU サイクルの処理のうち、CPU のバスアクセスより前に APU を進める（「02 エミュレーションコア設計」§2.3）。レジスタへの書き込みは、そのサイクルの APU の処理が終わった後に反映される。

## 5.2 型定義

分周器・シーケンサ・エンベロープ・レングスカウンタ・スイープを個別の型として定義し、各チャンネルがこれらを組み合わせて持つ。ハードウェアの部品構成と対応させることで、各部品の挙動を個別に検証できる。

```go
type APU struct {
    region *region.Region
    bus    Bus

    pulse1, pulse2 pulseChannel
    triangle       triangleChannel
    noise          noiseChannel
    dmc            dmcChannel
    frame          frameCounter

    cycles    uint64
    evenCycle bool

    out Output   // リサンプラへの出力先
}

// Bus は DMC がサンプルを読むために使う。
type Bus interface {
    // RequestDMCFetch は DMC のサンプル 1 バイトの読み出しを要求する。
    // バスは CPU を停止させ、その get サイクルで読み、CompleteDMCFetch で返す。
    RequestDMCFetch(addr uint16, reload bool)
}
```

共通部品を次のとおり定義する。

```go
// divider は周期 P+1 の分周器。
type divider struct {
    period  uint16
    counter uint16
}

// clock は 1 回クロックし、出力クロックを生成したとき true を返す。
func (d *divider) clock() bool {
    if d.counter == 0 {
        d.counter = d.period
        return true
    }
    d.counter--
    return false
}

// reload はカウンタを period にする。出力クロックは生成しない。
func (d *divider) reload() { d.counter = d.period }

type lengthCounter struct {
    value   uint8
    halt    bool
    enabled bool
}

type envelope struct {
    start      bool
    div        divider
    decayLevel uint8
    loop       bool
    constant   bool
    param      uint8
}

type sweep struct {
    enabled bool
    negate  bool
    shift   uint8
    div     divider
    reload  bool
    ones    bool   // Pulse 1 は 1 の補数、Pulse 2 は 2 の補数
}
```

## 5.3 レジスタ

| アドレス | チャンネル | 内容 |
|---|---|---|
| `$4000`–`$4003` | Pulse 1 | デューティ・エンベロープ、スイープ、タイマー下位、レングス + タイマー上位 |
| `$4004`–`$4007` | Pulse 2 | 同上 |
| `$4008`–`$400B` | Triangle | リニアカウンタ、未使用、タイマー下位、レングス + タイマー上位 |
| `$400C`–`$400F` | Noise | エンベロープ、未使用、モード + 周期、レングス |
| `$4010`–`$4013` | DMC | フラグ + レート、直接ロード、サンプルアドレス、サンプル長 |
| `$4015` | 全体 | 書き込みは有効化、読み出しはステータス |
| `$4017` | 全体 | 書き込みはフレームカウンタ制御 |

各レジスタのビット配置は `docs/research/04_apu.md` の 4 節の表に従う。

### 5.3.1 `$4015` の読み出し

```go
func (a *APU) readStatus() uint8 {
    var v uint8
    if a.frame.irqFlag { v |= 0x40 }
    if a.dmc.irqFlag   { v |= 0x80 }
    if a.dmc.bytesRemaining > 0 { v |= 0x10 }
    if a.noise.length.value > 0    { v |= 0x08 }
    if a.triangle.length.value > 0 { v |= 0x04 }
    if a.pulse2.length.value > 0   { v |= 0x02 }
    if a.pulse1.length.value > 0   { v |= 0x01 }
    a.frame.irqFlag = false          // DMC 側のフラグはクリアしない
    return v
}
```

割り込み線の状態は `IRQAsserted` として外へ出す。バスがサイクルごとに問い合わせる（「02 エミュレーションコア設計」§2.8）。

```go
// IRQAsserted はフレーム IRQ と DMC IRQ のいずれかが立っているかを返す。
func (a *APU) IRQAsserted() bool { return a.frame.irqFlag || a.dmc.irqFlag }
```

読み出しと同一サイクルにフラグがセットされた場合、1 を返してクリアしない。

このレジスタは CPU 内部で完結するため、バスは `openBus` を更新しない。bit 5 は `$4015` を読まなかった最後のサイクルのオープンバス値になる。

### 5.3.2 `$4015` の書き込み

チャンネル有効ビットを 0 にすると、そのチャンネルのレングスカウンタを 0 にし、再度有効にするまで変更できない状態にする。

DMC ビットを 0 にすると `bytesRemaining` を 0 にする。1 にすると、`bytesRemaining` が 0 のときのみサンプルを再スタートする。書き込みは DMC の割り込みフラグをクリアする。

## 5.4 フレームカウンタ

```go
type frameCounter struct {
    mode      uint8   // 0: 4 ステップ, 1: 5 ステップ
    irqInhibit bool
    apuCycles uint32
    irqFlag   bool

    pendingWrite      bool
    pendingValue      uint8
    pendingDelay      int   // 3 または 4 CPU サイクル
}
```

シーケンサは 2 CPU サイクルごとに進む。ステップの境界は `region.FrameCounterSteps` に定義する。値は `docs/research/04_apu.md` の 3.1・3.2 節の表を転記する。

| モード | quarter frame | half frame | フレーム IRQ |
|---|---|---|---|
| 4 ステップ | ステップ 1, 2, 3, 4 | ステップ 2, 4 | ステップ 4 の 3 箇所でセット |
| 5 ステップ | ステップ 1, 2, 3, 5 | ステップ 2, 5 | セットしない |

quarter frame はエンベロープと Triangle のリニアカウンタをクロックする。half frame はレングスカウンタとスイープをクロックする。

`$4017` への書き込みは即座に反映せず、3 または 4 CPU サイクル後にタイマーをリセットする。書き込みが APU サイクル中なら 3、APU サイクルの間なら 4 とする。`mode` が 1 のとき、リセットと同時に quarter frame と half frame の両方を生成する。

実装では数える値を 1 つ大きくする。書き込みが起きた CPU サイクルの APU の処理がすでに終わっているためである。`4-jitter`・`09.reset_timing`・`apu_reset/4017_timing` がこの値を検証する。

### 5.4.1 APU サイクルの前半と後半

1 つの APU サイクルは 2 つの CPU サイクルからなる。調査文書の表では前半を GET、後半を PUT と呼ぶ。累積 APU サイクル数が所定値に達するのは前半であり、quarter frame と half frame の信号はその 1 CPU サイクル後、後半で出る。

4 ステップモードのフレーム割り込みフラグは、最終ステップの APU サイクルの前半と後半、および次の APU サイクルの前半の 3 箇所で立てる。

`irqInhibit` をセットすると `irqFlag` をクリアする。`irqFlag` がセットされている間、IRQ 線をアサートし続ける。

## 5.5 Pulse チャンネル

```go
type pulseChannel struct {
    duty     uint8
    seqPos   uint8
    timer    divider     // APU サイクルごとにクロック
    env      envelope
    swp      sweep
    length   lengthCounter
    enabled  bool
}
```

デューティのシーケンスを次のとおり定義する。内部カウンタが下向きに数えるため、テーブルを 0, 7, 6, 5, 4, 3, 2, 1 の順に読む。

| デューティ | 出力波形 |
|---|---|
| 0 | `0 1 0 0 0 0 0 0` |
| 1 | `0 1 1 0 0 0 0 0` |
| 2 | `0 1 1 1 1 0 0 0` |
| 3 | `1 0 0 1 1 1 1 1` |

タイマーは APU サイクル（2 CPU サイクル）ごとにクロックする。波形の周期は `16 × (t + 1)` CPU サイクルである。

`$4003` / `$4007` への書き込みでシーケンサ位置を 0 に戻し、エンベロープを再スタートする。タイマーの分周器はリセットしない。

### 5.5.1 スイープ

目標周期を継続的に計算する。

```go
func (p *pulseChannel) targetPeriod() int {
    change := int(p.timer.period >> p.swp.shift)
    if p.swp.negate {
        if p.swp.ones {
            change = -change - 1     // Pulse 1
        } else {
            change = -change         // Pulse 2
        }
    }
    t := int(p.timer.period) + change
    if t < 0 { t = 0 }
    return t
}

func (p *pulseChannel) muted() bool {
    return p.timer.period < 8 || p.targetPeriod() > 0x7FF
}
```

ミュートの判定はスイープが無効であっても行う。`negate` が false でシフト量が 0、周期が `$400` 以上のとき目標周期が `$7FF` を超えてミュートされる。

half frame クロック時に、分周器のカウンタが 0 かつスイープ有効かつシフト量が非ゼロで、ミュートしていない場合に周期を目標周期へ更新する。ミュートしている場合は周期を変えず、分周器のカウントとリロードのみ行う。

### 5.5.2 ミキサーへの出力

次のいずれかが真のとき 0 を出力する。そうでなければエンベロープの音量を出力する。

| 条件 |
|---|
| シーケンサ出力が 0 |
| スイープがミュートしている |
| レングスカウンタが 0 |

## 5.6 Triangle チャンネル

```go
type triangleChannel struct {
    linearCounter uint8
    linearReload  uint8
    reloadFlag    bool
    control       bool
    timer         divider   // CPU サイクルごとにクロック
    seqPos        uint8     // 0..31
    length        lengthCounter
    enabled       bool
}
```

タイマーは CPU サイクルごとにクロックする。Pulse と異なり APU サイクルではない。

シーケンサは 32 ステップである。

```
15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0,
 0,  1,  2,  3,  4,  5, 6, 7, 8, 9,10,11,12,13,14,15
```

リニアカウンタのクロック時に次の順で処理する。

1. `reloadFlag` が立っていれば `linearCounter = linearReload`。立っていなければ非ゼロなら 1 減らす
2. `control` が false なら `reloadFlag` をクリアする

`control` が true の間 `reloadFlag` はクリアされない。両方が立っている状態で `$4008` に書いた値は、毎回のリニアカウンタクロックでリロードされる。

シーケンサはリニアカウンタとレングスカウンタの両方が非ゼロの間だけクロックする。停止時は最後の値を出力し続ける。0 にはしない。

タイマー値が 2 未満のとき超音波域となる。設定 `audio.silenceUltrasonicTriangle` が true のとき、この範囲でシーケンサを停止する。false のときは実機どおりに動作させる。

## 5.7 Noise チャンネル

```go
type noiseChannel struct {
    lfsr    uint16    // 15 bit
    mode    bool
    timer   divider
    env     envelope
    length  lengthCounter
    enabled bool
}
```

周期は `region.NoisePeriods` の値を CPU サイクル数として使う。

LFSR のクロック時に次の順で処理する。

```go
func (n *noiseChannel) clockLFSR() {
    var other uint16
    if n.mode {
        other = (n.lfsr >> 6) & 1
    } else {
        other = (n.lfsr >> 1) & 1
    }
    fb := (n.lfsr & 1) ^ other
    n.lfsr >>= 1
    n.lfsr |= fb << 14
}
```

LFSR の初期値は 1 とする。

ミキサーへの出力は、LFSR の bit 0 が 1 のとき、またはレングスカウンタが 0 のとき 0 とする。そうでなければエンベロープの音量とする。

## 5.8 DMC チャンネル

```go
type dmcChannel struct {
    rateIndex uint8
    timer     divider
    loop      bool
    irqEnable bool
    irqFlag   bool

    // メモリリーダー
    sampleAddr     uint16
    sampleLength   uint16
    currentAddr    uint16
    bytesRemaining uint16
    sampleBuffer   uint8
    bufferFilled   bool

    // 出力ユニット
    shiftReg      uint8
    bitsRemaining uint8
    outputLevel   uint8   // 0..127
    silence       bool

    enabled bool
}
```

レートは `region.DMCRates` の値を CPU サイクル数として使う。

出力レベルはチャンネルの有効・無効に関係なくミキサーへ送る。`$4011` への書き込みと DPCM 再生で更新する。

### 5.8.1 メモリリーダー

`bufferFilled` が false かつ `bytesRemaining` が非ゼロのとき、DMA を要求する。

```go
// 要求だけを出す。読むのはバスの get サイクルである。
func (d *dmcChannel) fillBuffer() {
    if d.bufferFilled || d.fetchPending || d.bytesRemaining == 0 {
        return
    }
    d.fetchPending = true
    d.bus.RequestDMCFetch(d.currentAddr, !d.firstFetch)
    d.firstFetch = false
}

// バスが読み終えた値を受け取る。
func (d *dmcChannel) completeFetch(v uint8) {
    d.fetchPending = false
    d.sampleBuffer = v
    d.bufferFilled = true
    if d.currentAddr == 0xFFFF {
        d.currentAddr = 0x8000
    } else {
        d.currentAddr++
    }
    d.bytesRemaining--
    if d.bytesRemaining == 0 {
        if d.loop {
            d.restart()
        } else if d.irqEnable {
            d.irqFlag = true
        }
    }
}
```

`reload` は、再生中にサンプルバッファが空になったことへの応答かを表す。チャンネルを有効にした直後の最初の読み出しでは false になる。

`irqFlag` がセットされている間、IRQ 線を継続的にアサートする。`$4015` への書き込みまたは `$4010` の bit 7 のクリアで解除する。

### 5.8.2 出力ユニット

タイマーがクロックを出したとき次の順で処理する。

1. `silence` が false なら、シフトレジスタの bit 0 が 1 なら出力レベルに 2 を加え、0 なら 2 を引く。結果が 0–127 の範囲を出るときは変更しない
2. シフトレジスタを右に 1 bit シフトする
3. `bitsRemaining` を 1 減らす。0 になったら新しい出力サイクルを開始する

新しい出力サイクルの開始時に `bitsRemaining` を 8 にする。`bufferFilled` が false なら `silence` をセットし、true なら `silence` をクリアしてサンプルバッファをシフトレジスタへ移す。

出力サイクルは中断しない。

## 5.9 ミキサー

2 つのルックアップテーブルで非線形ミキシングを実装する。

```go
var pulseTable [31]float32   // pulseTable[n] = 95.52 / (8128/n + 100)
var tndTable   [203]float32  // tndTable[n]   = 163.67 / (24329/n + 100)

func (a *APU) mix() float32 {
    p := pulseTable[a.pulse1.output()+a.pulse2.output()]
    t := tndTable[3*a.triangle.output()+2*a.noise.output()+a.dmc.outputLevel]
    return p + t
}
```

テーブルは初期化時に 1 回だけ計算する。`n == 0` の要素は 0 とする。

チャンネル別の音量（設定 `audio.channelVolumes`）は、合成する前に各チャンネルの値へ掛ける。合成した後に掛けると、音量を下げたチャンネルが他のチャンネルへ与える非線形の影響まで一緒に変わる。表の添字は整数であるため、掛けた結果を丸める。

非線形にするのは、DMC のレベルが Triangle と Noise の音量に影響する挙動を再現するためである。この挙動を Triangle の音量調整に使うゲームがある。

デバッガの APU 状態ビューアは、チャンネルごとのミュートを `SetMute` で指定する。ミュートしたチャンネルは合成の前に 0 として扱う。ミュートは合成にしか影響せず、チャンネルの状態は変わらない。

```go
// SetMute はミュートするチャンネルをビットで指定する。
// bit 0 から順に Pulse 1・Pulse 2・Triangle・Noise・DMC。
func (a *APU) SetMute(mask uint8)

// Inspect はデバッガ向けに各チャンネルの現在値を返す。状態を変えない。
func (a *APU) Inspect() Inspection
```

`Inspection` は次を持つ。

| チャンネル | 値 |
|---|---|
| Pulse 1・2 | 周期、デューティ、音量（エンベロープの出力）、エンベロープの設定、レングスカウンタ、スイープの設定、出力 |
| Triangle | 周期、リニアカウンタ、レングスカウンタ、シーケンス位置、出力 |
| Noise | 周期、モード、LFSR、音量、レングスカウンタ、出力 |
| DMC | 出力レベル、サンプルアドレス、現在のアドレス、残りバイト数、ループ、IRQ |
| フレームカウンタ | モード（4 ステップ・5 ステップ）、APU サイクル（位相）、IRQ 禁止、IRQ フラグ |

ミキサーの出力値はエミュレーション状態へ戻さない。浮動小数点の演算結果が分岐に影響しないため、決定論に影響しない。

## 5.10 フィルタとリサンプリング

ミキサーの出力は CPU クロック（約 1.79 MHz）で変化する。これを 48 kHz へ変換する。

```go
package audio

type Resampler struct {
    hp90  onePoleHighPass   // 90 Hz
    hp440 onePoleHighPass   // 440 Hz
    lp14k onePoleLowPass    // 14 kHz

    accum float64
    step  float64   // 出力 1 サンプルあたりの入力サンプル数
    prev  float32
}
```

フィルタの構成は設定 `audio.filterProfile` で選ぶ。

| プロファイル | 構成 |
|---|---|
| `nes` | 90 Hz ハイパス + 440 Hz ハイパス + 14 kHz ローパス |
| `famicom` | 37 Hz ハイパスのみ |
| `none` | フィルタなし |

ローパスがアンチエイリアシングを兼ねる。ダウンサンプルは線形補間で行う。

速度倍率が 1.0 以外のとき `step` に倍率を掛ける。これによりピッチが変化する。設定 `audio.muteOnFastForward` が true かつ倍率が 4.0 以上のとき、出力を 0 にする。

## 5.11 電源投入とリセット

| 項目 | 電源投入時 | リセット後 |
|---|---|---|
| `$4000`–`$400F` | 0 | 変更しない |
| Triangle の `seqPos` | 0 | 0 |
| Noise の `lfsr` | 1 | 変更しない |
| DMC の `$4010`・`$4012`・`$4013` | 0 | 変更しない |
| DMC の `outputLevel` | 0 | `outputLevel &= 1` |
| `$4015` | 0 | 0 |
| フレームカウンタの `mode`・`irqInhibit` | 0 | 変更しない |

電源投入とリセットの直後は、最初のコードが実行される 10 CPU サイクル前に `$4017` へ 0 が書き込まれた状態と等価にする。

## 5.12 保存する状態

§5.2 の `APU` 構造体のうち、`region`・`bus`・`out` と、チャンネル別の音量・ミュート・超音波域の Triangle の扱いを除く全フィールドを保存する。除いたものは出力段の設定であり、エミュレーション状態ではない。

リサンプラのフィルタ状態とリングバッファは保存しない。エミュレーション状態ではないためである。ロード後にフィルタをリセットし、リングを高水位まで再度埋める。

保存漏れが生じやすい項目を次に挙げる。

| 項目 | 省いたときの症状 |
|---|---|
| 各 `divider` の `counter` | 復元直後の位相がずれ、音が一瞬変わる |
| `frame.apuCycles` | フレームカウンタの位相がずれ、エンベロープとレングスの更新タイミングが変わる |
| `frame.pendingWrite`・`pendingDelay` | `$4017` 書き込みの直後に保存したステートで遅延処理が失われる |
| `noise.lfsr` | ノイズの波形が変わる |
| `dmc.shiftReg`・`bitsRemaining`・`bufferFilled` | DPCM 再生が途切れる |
| `evenCycle` | APU サイクルの位相がずれ、Pulse と Noise のタイマーが半サイクルずれる |
