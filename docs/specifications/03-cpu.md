# 03 CPU 設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 3.1 実装方式

CPU は「アドレッシングモードごとのサイクルシーケンスを手続きとして書き、バスアクセスのたびに `tick` が呼ばれる」方式で実装する。マイクロコード表に基づく明示的なステートマシンにはしない。

この方式を採る理由は 2 つある。`docs/research/02_cpu_6502.md` の 8 節のサイクル単位の表がコードと 1 対 1 で対応するため、`nestest.log` との差分から誤りの箇所を特定できる。もう 1 つは、コード量が明示的ステートマシンの 3 分の 1 程度に収まることである。

この方式の帰結として、命令の途中では CPU の状態がホストのコールスタックに存在する。したがってスナップショットを取れるのは命令境界のみである。扱いは「08 セーブステートと入力ムービー設計」§8.3。

## 3.2 型定義

```go
package cpu

type CPU struct {
    A, X, Y uint8
    S       uint8
    PC      uint16

    // ステータスフラグ。6 個の bool として持つ。
    // B と bit 5 は CPU 内部に存在しないため、push 時に引数で与える。
    C, Z, I, D, V, N bool

    bus Bus

    // 割り込み
    nmiLinePrev bool  // 前サイクルの NMI 線の状態。立ち下がり検出に使う
    nmiPending  bool  // 立ち下がりを検出してから処理されるまで true
    pollNMI     bool  // 現サイクルの終わりのポーリング結果
    pollIRQ     bool
    pollNMIPrev bool  // 前サイクルの終わりのポーリング結果
    pollIRQPrev bool
    overridePoll bool // 分岐命令がポーリング結果を決めたことを表す
    overrideNMI  bool
    overrideIRQ  bool
    iPending    bool  // CLI / SEI / PLP による I の変更を 1 命令遅延させる
    iPendingVal bool

    // インデックス付きアドレッシングの途中の情報。
    // 不安定な非公式命令が書き込む値とアドレスを決めるのに使う。
    indexBase    uint16
    indexCrossed bool

    opPC uint16 // 実行中の命令の opcode が置かれたアドレス

    halted bool // STP を実行した
}
```

`Bus` はインタフェースとして受け取る。CPU は `internal/nes/bus` の具体型に依存しない。

CPU からのバスアクセスはすべて内部の 1 組のメソッドを経由させる。そこで `cycles` を進め、`sampleInterruptLines`（§3.6.1）を呼ぶ。サイクルごとに NMI の立ち下がりを検出する必要があり、アクセスの箇所ごとに書くと漏れるためである。リセットシーケンス中のライト抑止も同じ場所で行う。

```go
type Bus interface {
    Read(addr uint16) uint8
    Write(addr uint16, v uint8)
    Peek(addr uint16) uint8      // 副作用なし。トレース出力に使う
    NMILine() bool
    IRQAsserted() bool
}
```

`Peek` を含めるのは、トレース出力が命令バイト列と実効アドレスの値を読むためである。`Read` を使うと PPU レジスタの副作用が発生し、トレースを取ることがエミュレーションの結果を変えてしまう。

### 3.2.1 ステータスレジスタの合成と分解

```go
// packP は push する 1 バイトを組み立てる。
// bFlag は BRK と PHP で true、NMI と IRQ で false。
func (c *CPU) packP(bFlag bool) uint8

// unpackP はスタックから取り出した値を反映する。bit 5 と bit 4 は無視する。
func (c *CPU) unpackP(v uint8)
```

## 3.3 命令表

opcode 256 個を配列で持つ。

```go
type addrMode uint8
type accessKind uint8

const (
    accessRead accessKind = iota
    accessWrite
    accessRMW
    accessNone
)

type opcode struct {
    mnemonic string      // "LDA", "SLO" など
    alias    string      // 別名。無ければ空文字列
    mode     addrMode
    access   accessKind
    official bool
    exec     func(c *CPU, addr uint16)
}

var opcodes [256]opcode
```

`opcodes` の内容は `docs/research/02_cpu_6502.md` の 6.1 節と 7.1 節の表を転記する。

`mnemonic` には `nestest.log` と同じ名前を入れる。トレースを `nestest.log` と行単位で比較することが CPU の検証手段であり、命令欄の桁数に別名を併記する余裕がないためである。`ISC` に対して `nestest.log` は `ISB` を使うため、`mnemonic` を `ISB`、`alias` を `ISC` とする。

`alias` には別の文書で使われる名前（`AXS` に対する `SBX` など）を入れる。逆アセンブラが併記する。

命令の実行は「アドレッシングモードのサイクルシーケンス」と「演算」の組み合わせで表す。非公式命令は既存のアドレッシングモードと既存の演算の組み合わせであるため、この構造によりサイクル数が自動的に正しくなる。

```go
func (c *CPU) StepInstruction() {
    if c.halted {
        c.read(c.PC) // STP 後もバスは止まらない
        return
    }
    c.applyPendingI()
    op := &opcodes[c.fetch()]
    addr := c.resolve(op)   // §3.4。ここで所定のサイクル数が消費される
    op.exec(c, addr)
    c.pollInterrupts()      // §3.6.1
    c.takePolledInterrupt()
}
```

実効アドレスに対する最後のアクセス（リード・ライト・RMW）は `exec` が行う。`resolve` はその手前までのサイクルを消費する。この分担により、同じアドレッシングモードでリードとライトのサイクル数が変わる規則（`a,X` のリードは 4 または 5、ライトは常に 5）を `resolve` の中の 1 箇所で表せる。

## 3.4 アドレッシングモードのサイクルシーケンス

`resolve` はアドレッシングモードごとに `docs/research/02_cpu_6502.md` の 8 節のシーケンスを実行し、実効アドレスを返す。ダミーリードとダミーライトを省略しない。

| モード | サイクル構成 | 備考 |
|---|---|---|
| Implied / Accumulator | 次の命令バイトを読んで捨てる | |
| Immediate | 値をフェッチして `PC++` | |
| Zero page | アドレスをフェッチ | |
| Zero page indexed | アドレスをフェッチ → そのアドレスを読んで捨て、インデックスを加算 | 上位バイトは常に `$00` |
| Absolute | 下位・上位をフェッチ | |
| Absolute indexed（read） | 上位フェッチ時に下位へ加算 → 不正アドレスから読み、上位を修正 → ページ越えなら再読み | |
| Absolute indexed（write / RMW） | 同上。ページ越えに関係なく必ずダミーリードを行う | |
| Relative | オペランドをフェッチ。分岐成立で +1、ページ越えで +1 | §3.5.1 |
| Indexed indirect `(d,X)` | ポインタを読んで捨て、X を加算 → 下位・上位をゼロページからフェッチ | ゼロページ内でラップ |
| Indirect indexed `(d),Y` | 下位・上位をフェッチ、上位フェッチ時に Y を下位へ加算 → 不正アドレスから読み、上位を修正 → ページ越えなら再読み | |
| Absolute indirect `JMP (a)` | ポインタの上位バイトを下位バイトのみで計算する | `$xxFF` でページをまたがない |
| JSR | 下位バイトをフェッチ → スタックアクセス → PCH を push → PCL を push → 上位バイトをフェッチ | 上位バイトのフェッチが push より後 |

JSR を absolute と同じ経路にしない。上位バイトのフェッチが push より後に来るため、アドレスを先に組み立てる他の absolute とは順序が異なる。逆アセンブラの表記は absolute と同じにする。

### 3.4.1 read-modify-write

RMW 命令は「読む → 元の値をそのまま書く → 変更後の値を書く」の順でバスにアクセスする。

```go
func (c *CPU) rmw(addr uint16, f func(uint8) uint8) {
    v := c.bus.Read(addr)
    c.bus.Write(addr, v)      // 元の値を書き戻す
    c.bus.Write(addr, f(v))   // 変更後の値を書く
}
```

この二重ライトにより、`INC $2007` が PPUDATA に 2 回書き込む挙動が再現される。

## 3.5 演算の実装

各演算は `func(c *CPU, addr uint16)` として実装する。アドレッシングモードが解決した実効アドレスを受け取る。

| 分類 | 対象 |
|---|---|
| ロード / ストア | LDA, LDX, LDY, STA, STX, STY |
| 転送 | TAX, TAY, TXA, TYA, TSX, TXS |
| 演算 | ADC, SBC, INC, DEC, INX, INY, DEX, DEY |
| シフト | ASL, LSR, ROL, ROR |
| 論理 | AND, ORA, EOR, BIT |
| 比較 | CMP, CPX, CPY |
| 分岐 | BCC, BCS, BEQ, BNE, BMI, BPL, BVC, BVS |
| ジャンプ | JMP, JSR, RTS, BRK, RTI |
| スタック | PHA, PHP, PLA, PLP |
| フラグ | CLC, SEC, CLI, SEI, CLD, SED, CLV |
| その他 | NOP |
| 非公式（安定） | SLO, RLA, SRE, RRA, SAX, LAX, DCP, ISC, ALR, ANC, ARR, AXS, IGN, SKB |
| 非公式（不安定） | XAA, LAX#, TAS, AHX, SHX, SHY, LAS |
| 非公式（停止） | STP |

各演算のフラグ更新規則は `docs/research/02_cpu_6502.md` の 6.1 節の表に従う。

ADC のオーバーフロー判定は `(result ^ A) & (result ^ memory) & 0x80`、SBC は `(result ^ A) & (result ^ ^memory) & 0x80` とする。

### 3.5.1 分岐命令の割り込みポーリング

分岐命令は他の命令と異なるポーリング点を持つ。

| ポーリング点 | 条件 |
|---|---|
| 2 サイクル目（オペランドフェッチ）の前 | 常に行う |
| 分岐成立時の 3 サイクル目の前 | 行わない |
| ページ越え時の PCH 修正サイクルの前 | 行う |

いずれかの点で検出されれば割り込みが発生する。

### 3.5.2 不安定な非公式命令

| 命令 | 実装 |
|---|---|
| XAA `#i` | `A = (A \| 0xEE) & X & i` |
| LAX `#i` | `A = X = i`。内部定数が `$FF` として観測されるため、A との論理積が効かない |
| TAS `a,Y` | `S = A & X`、`memory = A & X & (上位バイト + 1)` |
| AHX `(d),Y` / `a,Y` | `memory = A & X & (上位バイト + 1)` |
| SHX `a,Y` | `memory = X & (上位バイト + 1)` |
| SHY `a,X` | `memory = Y & (上位バイト + 1)` |
| LAS `a,Y` | `A = X = S = memory & S` |

`SHX`・`SHY`・`AHX`・`TAS` はページ境界をまたぐとき、書き込み先アドレスの上位バイトも書き込む値に置き換わる。アドレスの上位バイトを保持するラッチが正しく駆動されないためである。

これらを実行したとき `warn.compat` カテゴリのログに opcode と PC を記録する。

STP を実行したとき `halted` を true にし、`warn.compat` に記録する。以降 `StepInstruction` は PC のリードのみを行う。無限ループにしない。

## 3.6 割り込み

### 3.6.1 検出

NMI はエッジ検出、IRQ はレベル検出である。

```go
// 各サイクルの終わりに呼ぶ
func (c *CPU) endCycle() {
    c.sampleInterruptLines()
    // ポーリング結果を 1 サイクル分ずらして保持する
    c.pollNMIPrev, c.pollIRQPrev = c.pollNMI, c.pollIRQ
    c.pollNMI = c.nmiPending
    c.pollIRQ = !c.I && c.bus.IRQAsserted()
}

func (c *CPU) sampleInterruptLines() {
    nmi := c.bus.NMILine()
    if c.nmiLinePrev && !nmi {   // 立ち下がり（負論理でアサート）
        c.nmiPending = true
    }
    c.nmiLinePrev = nmi
}
```

割り込みが実際に効くのは「命令の最後から 2 番目のサイクルの終わり」の割り込み線の状態である。
各サイクルの終わりに結果を求めて 1 サイクル分ずらして保持することで、命令が終わった時点で
`pollNMIPrev` と `pollIRQPrev` がその位置の結果になる。アドレッシングモードごとにポーリング点を
書き込むと、命令の種類だけ書き漏らす箇所が増えるためこの形を採る。

`nmiPending` は NMI が処理されるまで保持する。`pollIRQ` はそのサイクルの状態のみを反映する。

分岐命令は §3.5.1 のとおりポーリング点が異なるため、`setPollResult` で結果を上書きする。

NMI と IRQ が同時に pending のとき NMI を処理し、IRQ の pending 状態は破棄する。IRQ はレベル検出であるため、次のポーリングで再検出される。

割り込みシーケンス自体はポーリングを行わない。割り込みハンドラの最初の 1 命令は必ず実行される。

### 3.6.2 I フラグの遅延

CLI・SEI・PLP は I を即座に変更する。効果が 1 命令遅れて見えるのは、割り込みのポーリングが命令の最後から 2 番目のサイクルの終わりに行われ、その時点ではまだ変更前の値が使われるためである。

フラグの更新そのものを遅らせない。遅らせると、変更の直後に割り込みが入ったときスタックへ積まれる P の I ビットが実機と違う値になる。`cpu_interrupts_v2/rom_singles/1-cli_latency` がこれを検証する。

RTI は P をポーリングより前のサイクルで復元するため、同じ仕組みで自然に即座の反映になる。

### 3.6.3 割り込みシーケンス

```go
func (c *CPU) serviceInterrupt(kind interruptKind) {
    c.bus.Read(c.PC)                    // 1: opcode をフェッチして破棄
    c.bus.Read(c.PC)                    // 2: 同じアドレスを読んで破棄
    c.push(uint8(c.PC >> 8))            // 3
    c.push(uint8(c.PC))                 // 4
    // ここでベクタが決まる（ハイジャック）
    vec := c.resolveVector(kind)
    c.push(c.packP(kind == intBRK))     // 5
    c.I = true
    lo := c.bus.Read(vec)               // 6
    hi := c.bus.Read(vec + 1)           // 7
    c.PC = uint16(hi)<<8 | uint16(lo)
}
```

ベクタを決定するのはサイクル 4 とサイクル 5 の間である。この 1 箇所でハイジャックを実装する。BRK の実行中にサイクル 4 までに NMI がアサートされた場合、B フラグをセットしたまま NMI ベクタへ分岐する。

リセットは同じシーケンスをたどるが、ライトを抑止する。S は 3 回デクリメントされ、メモリは変更されない。I は必ずセットされる。

## 3.7 電源投入とリセット

電源投入は、A・X・Y を 0、S を `$00` にした上でリセットシーケンス（§3.6.3）を実行して作る。シーケンス中に S が 3 回デクリメントされるため `$FD` になり、7 サイクルを消費して PC が `($FFFC)` になる。電源投入後の状態を別に書き下さないのは、`nestest.log` の 1 行目が `CYC:7` であることと同じ経路で一致させるためである。

| レジスタ | 電源投入時 | リセット後 |
|---|---|---|
| A, X, Y | 0 | 変更しない |
| PC | `($FFFC)` | `($FFFC)` |
| S | `$FD` | `S -= 3` |
| C, Z, D, V, N | false | 変更しない |
| I | true | true |

## 3.8 トレース出力

`State` を `nestest.log` と同一形式で文字列化する。

```go
type State struct {
    PC        uint16
    A, X, Y   uint8
    P         uint8   // packP(false) の結果
    S         uint8
    Bytes     [3]uint8
    ByteLen   int
    Disasm    string
    Official  bool    // 非公式命令のとき、ニモニックの前に * を付ける
    PPUScanline int
    PPUDot      int
    Cycles      uint64
}

// State は PPU の走査位置を引数で受け取る。CPU が PPU を参照しないためである。
func (c *CPU) State(scanline, dot int) State

func (s State) TraceLine() string
```

出力形式は次のとおりである。

```
C000  4C F5 C5  JMP $C5F5                       A:00 X:00 Y:00 P:24 SP:FD PPU:  0, 21 CYC:7
C6BD  04 A9    *NOP $A9 = 00                    A:AA X:97 Y:4E P:EF SP:F9 PPU:128, 89 CYC:14579
```

桁の構成を次に示す。

| 桁 | 内容 |
|---|---|
| 0–3 | PC |
| 6–13 | 機械語。3 バイト分の幅で左詰め |
| 15 | 非公式命令のとき `*`、公式命令のとき空白 |
| 16–47 | 逆アセンブル結果。左詰め |
| 48 以降 | レジスタ・PPU の走査位置・累積サイクル数 |

逆アセンブル結果には実効アドレスと、その位置の現在の値を注記する。注記の形をアドレッシングモードごとに次に示す。

| モード | 表記 |
|---|---|
| implied | なし |
| accumulator | `A` |
| immediate | `#$44` |
| zero page | `$44 = FF` |
| zero page indexed | `$44,X @ 55 = FF` |
| absolute | `$4400 = FF` |
| absolute indexed | `$4400,X @ 4455 = FF` |
| indexed indirect | `($44,X) @ 55 = 4400 = FF` |
| indirect indexed | `($44),Y = 4400 @ 4455 = FF` |
| relative | `$C5F5`（分岐先） |
| absolute indirect | `($0200) = DB7E`（ジャンプ先） |

`= FF` の注記は実効アドレスをデータとして読み書きする命令にのみ付ける。`JMP`・`JSR`・分岐には付けない。

この形式を採るのは、`nestest.log` と行単位で比較するためである。比較手順は「12 テスト設計」§12.3。

## 3.9 保存する状態

| 項目 |
|---|
| A, X, Y, S, PC |
| C, Z, I, D, V, N |
| `nmiLinePrev`, `nmiPending` |
| `pollNMI`, `pollIRQ`, `pollNMIPrev`, `pollIRQPrev` |
| `iPending`, `iPendingVal` |
| `halted` |

`nmiLinePrev` を保存するのはエッジ検出のためである。これを省くと、復元直後に NMI を取りこぼすか二重に発生させる。

累積サイクル数は CPU が持たない。バスが数え、`Bus.Cycles()` で参照する（「02 エミュレーションコア設計」§2.7）。
