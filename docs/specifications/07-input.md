# 07 入力設計

- 文書バージョン: 1.0
- 作成日: 2026-09-21
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 7.1 デバイスインタフェース

コントローラポート 1 と 2 にそれぞれ 1 個のデバイスを接続する。

```go
package input

type Device interface {
    // Strobe は $4016 への書き込みの下位 3 bit を受け取る。
    Strobe(v uint8)

    // Read は下位 5 bit を返す。上位 3 bit はバスがオープンバスから合成する。
    Read() uint8

    // Peek は副作用を発生させずに現在の出力を返す。
    Peek() uint8

    // Kind は保存したデバイス種別との照合に使う名前を返す。
    Kind() string

    state.Snapshotter
}
```

上位 3 bit の合成をデバイスではなくバスで行う（「02 エミュレーションコア設計」§2.4.1）。これにより、デバイスがオープンバスの状態を知る必要がなくなる。

## 7.2 標準コントローラ

```go
type StandardController struct {
    src      Source  // 押下状態の供給元
    port     int
    buttons  uint8   // 直近に取り込んだ押下状態。bit 0 から A, B, Select, Start, Up, Down, Left, Right
    shiftReg uint8
    strobe   bool
}
```

ボタンの並びを次のとおり定める。

| bit | ボタン |
|---|---|
| 0 | A |
| 1 | B |
| 2 | Select |
| 3 | Start |
| 4 | Up |
| 5 | Down |
| 6 | Left |
| 7 | Right |

```go
func (c *StandardController) Strobe(v uint8) {
    high := v&1 != 0
    if high {
        c.reload()   // strobe が high の間、継続的にリロードする
    }
    c.strobe = high
}

func (c *StandardController) Read() uint8 {
    if c.strobe {
        c.reload()
    }
    v := c.shiftReg & 1
    if !c.strobe {
        c.shiftReg = (c.shiftReg >> 1) | 0x80   // 空きビットに 1 をシフトインする
    }
    return v
}

// reload は供給元から押下状態を取り込む。
func (c *StandardController) reload() {
    c.buttons = c.src.Buttons(c.port)
    c.shiftReg = c.buttons
}
```

`strobe` が high の間は最初のボタン（A）の状態を返し続ける。シフトは行わない。

8 回読み終えた後は 1 を返す。`Read` で上位ビットに 1 をシフトインすることでこれを実現する。

`Strobe` で 1 を書いてから 0 を書くまでの間にボタン状態が変化した場合、`Read` は最新の状態を返す。実機のシフトレジスタが strobe 中に継続してリロードされる挙動に対応する。

デバイスが接続されていないポートには `NoDevice` を接続する。`Read` は常に 0 を返す。

## 7.3 入力状態の受け渡し

UI スレッドがキーイベントを受け取り、共有のビットマスクを更新する。エミュレーションゴルーチンはフレームの開始時にこのビットマスクを取り込み、そのフレームの間は取り込んだ値を使う。

デバイスは供給元を interface として受け取る。

```go
package input

// Source は押下状態の供給元。エミュレーションゴルーチンから呼ばれる。
type Source interface {
    Buttons(port int) uint8
}
```

共有のビットマスクそのものは `internal/emu` に置く。

```go
package emu

// InputState は UI スレッドとエミュレーションゴルーチンが共有する。
type InputState struct {
    ports [2]atomic.Uint32   // 下位 8 bit がボタン状態
}

func (s *InputState) Set(port int, buttons uint8)
func (s *InputState) Buttons(port int) uint8
```

`internal/nes` に置かないのは、共有のビットマスクがエミュレーション状態ではなく UI スレッドとの境界であることによる。`internal/nes` は単一のゴルーチンから触るものだけを持ち、並行処理の仕組みを含まない（「01 全体アーキテクチャ設計」§1.7）。境界を `internal/emu` に置くことで、コアのテストが同期の仕組みを必要としなくなる。

`atomic.Uint32` を使うのはロックを避けるためである。ボタン状態は 8 bit で、1 回のアトミック操作で読み書きできる。

取り込んだ値は `internal/emu` の `frameLatch` が保持し、デバイスの `Source` としてこれを接続する。`$4016` への strobe 書き込みとシフトレジスタの挙動（§7.2）は変わらない。1 フレーム内に複数回ポーリングするプログラムは同じ値を何度も読む。

フレームの開始時に取り込むのは、記録した入力と再生した結果を一致させるためである（「08 セーブステートと入力ムービー設計」§8.7.1）。取り込まずに strobe のたびに共有のビットマスクを読むと、UI スレッドがフレームの途中で書き換えた値が混ざり、記録した値と実際に使われた値が食い違う。

入力ムービーを再生しているときは、ムービーの当該フレームの値を返す `Source` を代わりに接続する（「08 セーブステートと入力ムービー設計」§8.7）。

## 7.4 キーバインドの解決

### 7.4.1 論理アクション

```go
type Action uint8

const (
    // プレイヤー入力
    ActionP1Up Action = iota
    ActionP1Down
    ActionP1Left
    ActionP1Right
    ActionP1A
    ActionP1B
    ActionP1Start
    ActionP1Select
    ActionP2Up
    // ... P2 も同様

    // ホットキー
    ActionPause
    ActionFrameAdvance
    ActionFastForward
    ActionSlowMotion
    ActionReset
    ActionHardReset
    ActionSaveState
    ActionLoadState
    ActionNextSlot
    ActionPrevSlot
    ActionRewind
    ActionScreenshot
    ActionToggleFullscreen
    ActionMute
)
```

### 7.4.2 物理キーの表現

物理キーを W3C の `KeyboardEvent.code` と同じ名前で表す。`ArrowUp`、`KeyZ`、`ShiftRight` のような文字列を設定ファイルに保存する。

この表現を採るのは、キーボードレイアウト（JIS、US、AZERTY）に依存せず物理位置を表せることによる。

Fyne のキーイベントから `code` への変換表を `internal/ui/keymap.go` に置く。変換表はこの 1 ファイルのみに存在する。

```go
package ui

// fyneKeyToCode は Fyne のキー名を KeyboardEvent.code 形式へ変換する。
var fyneKeyToCode = map[fyne.KeyName]string{ /* ... */ }
```

### 7.4.3 バインドの構造

1 つのアクションに複数の物理キーを割り当てられる。

```go
package config

type Binding struct {
    Type string `json:"type"`   // "key"
    Code string `json:"code"`   // "KeyZ" など
}

type Keybindings struct {
    Version int                      `json:"version"`
    Players []PlayerBindings         `json:"players"`
    Hotkeys map[string][]Binding     `json:"hotkeys"`
}

type PlayerBindings struct {
    Player   int                  `json:"player"`
    Device   string               `json:"device"`
    Bindings map[string][]Binding `json:"bindings"`
    Turbo    map[string]int       `json:"turbo"`   // アクション名 → 連射レート（Hz）。0 で無効
}
```

解決は起動時と設定変更時に 1 回行い、`map[string][]Action` を作る。キーイベントごとに JSON を参照しない。

同じ物理キーが複数のアクションに割り当てられている場合、すべてのアクションを実行する。設定 GUI では重複を表示する。

### 7.4.4 既定のバインド

| アクション | 1P | 2P |
|---|---|---|
| Up / Down / Left / Right | `ArrowUp` / `ArrowDown` / `ArrowLeft` / `ArrowRight` | `KeyW` / `KeyS` / `KeyA` / `KeyD` |
| A | `KeyX` | `KeyG` |
| B | `KeyZ` | `KeyF` |
| Start | `Enter` | `KeyR` |
| Select | `ShiftRight` | `KeyT` |

A を `KeyX`、B を `KeyZ` とするのは、NES のコントローラで B が左・A が右に並ぶ物理配置と一致させるためである。

| ホットキー | 既定のキー |
|---|---|
| 一時停止 | `Space` |
| コマ送り | `Period` |
| 早送り（押している間） | `Tab` |
| スロー（押している間） | `Backquote` |
| リセット | `F1` |
| セーブステート | `F5` |
| ロードステート | `F7` |
| 巻き戻し（押している間） | `Backspace` |
| スクリーンショット | `F12` |
| フルスクリーン切り替え | `F11` |

## 7.5 連射

連射は本アプリケーションの機能であり、実機のコントローラの機能ではない。

```go
type turbo struct {
    rateHz   int
    lastFlip uint64   // フレーム番号
    state    bool
}
```

トグルはフレーム番号を基準に行う。strobe の回数を基準にしない。strobe ごとにトグルすると、「同じ結果が 2 回連続で得られるまで読み直す」ループを持つプログラムが進まなくなる。

入力ムービーを記録するとき、連射を適用した後の状態を記録する。再生時に連射を再適用しない。

## 7.6 DMC DMA との競合

`$4016` または `$4017` の読み出しが DMC DMA の停止サイクルと重なると、コントローラのシフトレジスタが余分にクロックされ、レポートから 1 bit が失われる。

この挙動はバスの `repeatHaltedRead`（「02 エミュレーションコア設計」§2.7.1）が `Bus.read` を再度呼ぶことで自然に発生する。`StandardController.Read` は呼ばれた回数だけシフトする。デバイス側に特別な処理を置かない。

症状として Right が押されたように見える。回避策を持つプログラムはこの挙動の下で正しく動作する。

## 7.7 保存する状態

| 項目 |
|---|
| ポート 1 と 2 のデバイス種別（検証用） |
| 各デバイスの `buttons`, `shiftReg`, `strobe` |
| `$4016` へ書き込まれた下位 3 bit |

`Source` は保存しない。押下状態の供給元は UI スレッド側の状態であり、エミュレーション状態ではない。復元した直後の読み出しで供給元から取り込み直す。

連射の状態（`turbo`）は保存しない。本アプリケーションの機能であり、エミュレーション状態ではないためである。
