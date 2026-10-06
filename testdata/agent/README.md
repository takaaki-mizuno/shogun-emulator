# Agent Interface のテスト用 ROM

設計書 14 編 §14.24、計画フェーズ 16 §3.1 のテスト用 ca65 プロジェクト。ソースは自作であり、既存のエミュレータやゲームのコードを使っていない。

| ファイル | 内容 |
|---|---|
| `src/game.s` | 本体。NMI ごとに Game State の値を決まった規則で更新する（規則はファイルの先頭に書いてある） |
| `src/macros.inc` | マクロ。ソース行の対応で、マクロの展開より呼び出した行を優先することの確認に使う |
| `src/game.cfg` | ld65 のリンカ設定。MMC1、PRG 128 KiB（16 KiB × 8）、CHR-RAM |
| `game.nes`・`game.dbg` | ビルドした ROM とデバッグ情報。CI に cc65 を入れずにテストを動かすため、リポジトリに含める |
| `build.sh` | ビルドの手順 |

## 確かめるための仕掛け

| 仕掛け | 確かめること |
|---|---|
| `BANK0` と `BANK1` の `$8000` に別の名前のラベル（`bank0_entry`・`bank1_entry`） | 同じ CPU アドレスの名前を、現在のバンクで区別する |
| `.proc` の中のラベル（`reset::loop` など） | `scope` から修飾名を作る |
| `inc16`・`mmc1_write` のマクロ | ソース行がマクロの定義ではなく呼び出した行を指す |
| `ZEROPAGE`・`BSS` の 1・2・3・8 バイトの変数 | `SpaceCPU` の位置とサイズ |
| `PLAYER_Y_START = $80` | `type=equ` の定数 |
| `RODATA` の `message` | フェーズ 20 の `execute_data` |

## ビルド

```sh
testdata/agent/build.sh
```

`src` ディレクトリでビルドし、`.dbg` のファイル名を相対パス（`game.s`）にする。ビルドし直すと `.dbg` の `file` 行の `mtime` と `size` が変わる。テストはこれらを使わない。

確認した cc65 の版:

| 項目 | 版 |
|---|---|
| ca65 | V2.18（macOS、Homebrew の `cc65`） |
| ld65 | V2.18 |

cc65 の入れ方: macOS は `brew install cc65`、Linux はディストリビューションのパッケージ（`apt install cc65` など）、Windows は cc65 の公式配布物。

## Diagnostic とプロファイルのテスト用 ROM（`diag/`）

計画フェーズ 20 §3.8 のテスト用 ROM。NROM-128（PRG 16 KiB、CHR-RAM）で、共通部分は `diag/src/common.inc`、リンカ設定は `diag/src/nrom.cfg` にある。各 ROM は準備の後に対象の誤りを 1 回だけ起こし（命令に `trigger` のラベルを付ける）、`forever` の無限ループ（`JMP *`）で止まる。

| ROM | 起こすもの |
|---|---|
| `control` | 何も起こさない対照。NMI ごとに `update`（`helper` を呼ぶ）を呼び、`wait_nmi` で待つ |
| `vram_during_render` | 描画中の `$2007` の読み出し |
| `warmup` | PPU の起動前の `$2000` への書き込み |
| `uninit_read` | 書き込みの無い RAM の読み出し |
| `stack_overflow`・`stack_underflow` | S が `$00` での PHA、`$FF` での PLA |
| `nmi_reentry` | NMI の処理の途中の次の NMI（1 回だけ） |
| `execute_data` | RODATA に置いた JMP の実行（ラベル `data_code`） |
| `execute_ram` | `$0200` に置いた RTS の実行 |
| `unstable_opcode` | XAA（`$8B`）の実行 |
| `oamaddr_dma` | OAMADDR が `$10` のままの OAM DMA |
| `palette_0d` | 色 `$0D` のパレットへの書き込み |
| `lag` | プロファイル用。`wait_vblank` で待ち、4 フレームに 1 回、1 フレームを超える `heavy` を呼ぶ |

ビルドは `testdata/agent/diag/build.sh`（`diag/src` の `*.s` をすべてビルドし、`.nes` と `.dbg` を `diag/` に置く）。

