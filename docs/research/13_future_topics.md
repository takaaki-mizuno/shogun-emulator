# 将来対応トピックの整理（第 1 弾の範囲外）

> 調査日: 2026-09-21
> project.md は「様々な拡張 ROM にも今後対応するが、まずは基本的な機能をすべて動作するようにする」と書いている。本書は「今はやらないが、将来やるときに設計が破綻しないようにしておくこと」を整理する。

## 1. 第 1 弾の範囲（スコープの明示）

含む:

- NTSC（RP2A03 / RP2C02）を正確にエミュレートする
- iNES + NES 2.0 形式の .nes を読む
- マッパー 0 / 1 / 2 / 3 / 4 / 7 / 66
- 標準コントローラ 2 個
- GUI（メニュー、ROM を開く、設定）
- デバッガ（`11_debugger_features.md` の D1–D9）
- **セーブステート・入力ムービー・巻き戻し**（2026-09-21 の人間の判断により第 1 弾に含めることが決定 → `14_savestate_and_movie.md`）
- macOS / Windows / Linux で動く単一バイナリ

含まない（本書で扱う）:

- PAL / Dendy（**設計には入れるが検証はしない**）
- FDS（ディスクシステム）
- 拡張音源
- マッパー 0/1/2/3/4/7/66 以外
- Zapper 等の周辺機器
- Vs. System / PlayChoice-10
- ネットプレイ、実績、チート

> **2026-10-04 追記: 第 2 弾として Agent Interface を加える。** AI エージェントやプログラムからエミュレータを操作・観測・デバッグする窓口（MCP・JSON-RPC・CLI）である。設計は `docs/specifications/14-agent-interface.md`、構成の判断は `docs/adr/0001-jsonrpc-core-with-mcp-bridge.md` にまとめた。

## 2. PAL / Dendy

**第 1 弾では「設計に入れるが検証しない」。**

理由: リージョン差は「定数の違い」に見えて実際には挙動の違いがある。あとから足すと全体に手を入れることになる。

| 差分 | 内容 | 影響箇所 |
|---|---|---|
| クロック分周比 | NTSC 12 / PAL 16 / Dendy 15 | `07_timing_and_synchronization.md` §2 |
| **PPU ドット / CPU サイクル** | NTSC 3 / **PAL 3.2**（整数でない） | PPU のステップ駆動を分数カウンタにする必要がある |
| スキャンライン構成 | 可視 240/239、VBlank 20/70、ポストレンダー 1/1/51 | PPU のスキャンラインカウンタ |
| **エンファシスのビット順** | PAL / Dendy は赤と緑が入れ替わる | パレット変換 |
| **ボーダー** | PAL は常に黒で、画面の左右 2px と上 1px を侵食する | 描画 |
| **OAM 強制リフレッシュ** | PAL PPU は NMI 後 24 スキャンラインで OAM を強制リフレッシュし、その間 OAM に書けない | OAM |
| **DMC DMA のレジスタ競合** | **2A07 では修正されている** | DMA |
| Noise / DMC の周期テーブル | 値が異なる | APU |
| APU フレームカウンタ | 60 / 50 / 59 Hz | APU |
| Dendy の VBlank フラグ | ちょうど VBlank 開始時に読むと 1 を返さずクリアされる。フレームが 8 の倍数サイクルなので特定のループが永久に進まない | PPU |

**設計への織り込み方**: `Region` という型を作り、上記の定数と「挙動の分岐フラグ」をまとめて持つ。ハードコードされた `341`、`262`、`3` を全部この構造体経由にする。

```go
type Region struct {
    Name                  string
    CPUClockDivider       int
    PPUDotsPerCPUCycleNum int  // NTSC: 3/1, PAL: 16/5
    PPUDotsPerCPUCycleDen int
    VisibleScanlines      int
    PostRenderScanlines   int
    VBlankScanlines       int
    NoisePeriods          [16]uint16
    DMCRates              [16]uint16
    EmphasisBitOrder      [3]int
    ForcedOAMRefresh      bool
    DMCDMARegisterConflict bool
    // ...
}
```

検証は PAL 用テスト ROM（`pal_apu_tests`、`nmi_sync` の PAL 版）で後から行う。

## 3. Famicom Disk System（FDS）

- ファイル形式が `.fds`（.nes とは別）。ヘッダ形式も別
- ディスクの読み書き、ディスク交換の UI が必要
- **FDS 専用の音源（波形メモリ 1ch）**
- BIOS ROM（8 KiB）が必要。**著作物なのでユーザーが自分で用意する**
- IRQ の挙動が独特（`FdsIrqTests` などのテスト ROM がある）

設計への織り込み: **ROM ローダを「.nes 専用」と決め打ちしない。** `Cartridge` を interface にして、iNES / NES 2.0 / FDS が実装できる形にしておく。

## 4. 拡張音源

Famicom はカートリッジポート経由で追加音源を APU 出力に混ぜられた（NES にはこの配線がない）。

| 音源 | マッパー | 内容 |
|---|---|---|
| VRC6 | 24 / 26 | Pulse ×2 + Sawtooth。悪魔城伝説 |
| VRC7 | 85 | FM 音源 6ch。ラグランジュポイント |
| FDS | – | 波形メモリ 1ch |
| MMC5 | 5 | Pulse ×2 + PCM |
| Namco 163 | 19 | 波形メモリ最大 8ch |
| Sunsoft 5B | 69 | AY-3-8910 相当 3ch。ギミック! |

設計への織り込み: **ミキサーに「追加チャンネル」を差せるようにする。**

```go
type ExpansionAudio interface {
    // CPU サイクルごとに呼ばれる
    Tick()
    // 現在の出力（0.0–1.0 の範囲に正規化済み）
    Output() float64
}
```

`04_apu.md` §5 のミキサーは 2 グループ（pulse と tnd）の非線形和だが、拡張音源は**その後で線形に加算される**（カートリッジ側で混ぜられるため）。相対音量は音源ごとに実測値がある（`nes-audio-tests` が参考になる）。

## 5. 追加マッパー

優先順位の案（タイトル数と必要性から）:

| 順 | マッパー | 理由 |
|---|---|---|
| 1 | 9 / 10（MMC2 / MMC4） | **PPU のタイルフェッチ（$FD/$FE）を見て CHR バンクを自動切り替えする**という特殊機構。Punch-Out!!、ファイアーエムブレム。PPU とマッパーの連携の検証になる |
| 2 | 69（Sunsoft FME-7） | **CPU サイクルベースの IRQ カウンタ**。マッパー IRQ のもう 1 つの形 |
| 3 | 21/22/23/25（VRC2/VRC4） | CPU サイクル IRQ、細かい CHR バンキング |
| 4 | 11（Color Dreams）、34（BNROM/NINA-001）、206（DxROM） | 単純で数が多い |
| 5 | 5（MMC5） | **最も複雑。** 拡張属性モード、垂直分割、追加音源、乗算器、追加 RAM |
| 6 | 24/26（VRC6）、85（VRC7）、19（Namco 163） | 拡張音源つき |
| 7 | 16（Bandai FCG） | EEPROM セーブ |

`05_cartridge_and_mappers.md` §7.3 の `Mapper` interface で全部扱えるはずだが、MMC5 は「ネームテーブルフェッチの検出」「属性の差し替え」が必要なので、**PPU 側のフックが足りるかを MMC5 の実装前に確認する。**

## 6. 周辺機器

| デバイス | 難易度 | 備考 |
|---|---|---|
| Four Score（4 人用） | 低 | D0 のみ。シグネチャバイトのプロトコルがある |
| Zapper | 中 | **PPU の描画内容（輝度）を参照する必要がある。** マウス位置 → 画面座標 → 輝度判定。D3/D4 ライン |
| Arkanoid controller | 中 | アナログ（9 bit のパドル値） |
| Power Pad / Family Trainer | 低 | マット型。キーボードに割り当てる |
| Famicom マイク | 低 | $4016 D2。キーを 1 個割り当てる |
| SNES コントローラ / マウス | 低 | プロトコルが後方互換 |
| Family BASIC キーボード | 中 | |

`06_input_devices.md` §7.1 の `InputDevice` interface で扱える。**Zapper だけは PPU への参照が必要**なので、interface に「フレームバッファへのアクセス」を渡す形を考えておく。

## 7. Vs. System / PlayChoice-10

- Vs. System: 専用 PPU（2C03/2C04/2C05）でパレットが全く違う。コイン投入、ディップスイッチ、2 台構成（Dual System）
- PlayChoice-10: 2C03 PPU + ヒント画面用の追加 ROM

NES 2.0 のヘッダでコンソール種別とハードウェア種別が指定できる（`05_cartridge_and_mappers.md` §3.1）。**ローダはこれを読んで「非対応」と明示的に警告する**ようにしておく（黙って誤動作させない）。

## 8. エミュレータとしての付加機能

| 機能 | 優先度 | 備考 |
|---|---|---|
| ~~セーブステート~~ | — | **第 1 弾に移動** → `14_savestate_and_movie.md` |
| ~~巻き戻し（rewind）~~ | — | **第 1 弾に移動** → `14_savestate_and_movie.md` §8 |
| ~~入力ムービー~~ | — | **第 1 弾に移動** → `14_savestate_and_movie.md` §7 |
| run-ahead（入力遅延削減） | 低 | セーブステート + 再実行。**第 1 弾の基盤の上にそのまま載る**（`14` §8.2） |
| チート（Game Genie / Pro Action Replay） | 低 | メモリパッチ。**2026-10-04: 値の固定（Freeze）はデバッグ機能として Agent Interface（設計書 14 編 §14.13.2）で扱う。Game Genie 等のコードの入力と適用は引き続き範囲外** |
| NSF / NSFe 再生 | 低 | 音楽ファイル形式。APU が完成していれば比較的容易 |
| CRT / NTSC フィルタ | 低 | シェーダが必要 |
| ネットプレイ | 低 | 決定論的な実行が必要（lock-step なら成立する） |
| 実績・進捗管理 | 低 | |
| UNIF 形式 | 低 | NES 2.0 に置き換えられた非推奨形式 |

> **2026-09-21: セーブステート・入力ムービー・巻き戻しは第 1 弾に含めることが決定した。** 設計は `14_savestate_and_movie.md` にまとめた。とくに**セーブステートが CPU の実装方式を縛る**（命令境界でのみスナップショット可能な方式を採る）ことが判明したため、設計書を書く前に確定させる必要があった。

## 9. 国際化

project.md は日本語で書かれており、開発者も日本語話者だが、UI の文言は将来英語対応する可能性がある。

- 文言をコードに直書きせず、1 箇所にまとめる（`internal/ui/i18n`）
- 設定に `"language"` フィールドを用意する（`12_config_and_packaging.md` §2.4 に含めた）
- **第 1 弾は日本語のみ。** ただし文言を集約する形だけは守る

## 10. 参考資料

| 資料 | URL | 参照日 |
|---|---|---|
| NESdev Wiki: Cycle reference chart（リージョン差） | https://www.nesdev.org/wiki/Cycle_reference_chart | 2026-09-20 |
| NESdev Wiki: Family Computer Disk System | https://www.nesdev.org/wiki/Family_Computer_Disk_System | 2026-09-20 |
| NESdev Wiki: MMC5 | https://www.nesdev.org/wiki/MMC5 | 2026-09-20 |
| NESdev Wiki: MMC2 | https://www.nesdev.org/wiki/MMC2 | 2026-09-20 |
| NESdev Wiki: VRC6 | https://www.nesdev.org/wiki/VRC6 | 2026-09-20 |
| NESdev Wiki: Sunsoft FME-7 | https://www.nesdev.org/wiki/Sunsoft_FME-7 | 2026-09-20 |
| NESdev Wiki: Input devices | https://www.nesdev.org/wiki/Input_devices | 2026-09-20 |
| nes-audio-tests（拡張音源の相対音量） | https://github.com/bbbradsmith/nes-audio-tests | 2026-09-20 |
