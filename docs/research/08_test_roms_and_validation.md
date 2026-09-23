# テスト ROM と検証手法の調査

> 調査日: 2026-09-20 / 一次情報源: NESdev Wiki "Emulator tests"、christopherpow/nes-test-roms リポジトリ

## 1. 背景・目的

NES エミュレータの正確さは**テスト ROM でしか客観的に検証できない**。目で見て「動いている」ように見えても、タイミングが 1 PPU クロックずれているだけで特定のゲームが壊れる。

project.md は「テストについては https://github.com/christopherpow/nes-test-roms の ROM ファイルが利用できるかもしれない」と書いている。本書はこのリポジトリと NESdev Wiki の一覧を整理し、**開発の各段階でどのテストを通すべきか**を決める。

## 2. テスト ROM の入手

| 入手先 | 内容 | 備考 |
|---|---|---|
| https://github.com/christopherpow/nes-test-roms | 主要なテスト ROM のアーカイブ | **NESdev Wiki が挙げるダウンロードリンクの多くが死んでおり、ここにアーカイブされている**。これを第一の入手元にする |
| https://www.nesdev.org/wiki/Emulator_tests | 一覧と説明 | 各テストの目的と関連フォーラムスレッドがある |
| https://github.com/pinobatch/holy-mapperel | Holy Mapperel（マッパー検証） | 十数種のマッパーを検出して全バンク到達性を検証 |
| https://github.com/pinobatch/allpads-nes | allpads（入力デバイス検証） | |
| https://github.com/bbbradsmith/nes-audio-tests | 拡張音源・NSF 関連 | |

リポジトリ内のディレクトリ（2026-09-20 時点で確認したもの）:

```
240pee                      apu_mixer               apu_mixer_recordings
apu_reset                   apu_test                blargg_apu_2005.07.30
blargg_litewall             blargg_nes_cpu_test5    blargg_ppu_tests_2005.09.15b
branch_timing_tests         cpu_dummy_reads         cpu_dummy_writes
cpu_exec_space              cpu_interrupts_v2       cpu_reset
cpu_timing_test6            dmc_dma_during_read4    dmc_tests
dpcmletterbox               exram                   fdsirqtests
full_palette                instr_misc              instr_test-v3
instr_test-v5               instr_timing            m22chrbankingtest
MMC1_A12                    mmc3_irq_tests          mmc3_test
mmc3_test_2                 mmc5test                mmc5test_v2
nes15-1.0.0                 nes_instr_test          nmi_sync
nrom368                     ny2011                  oam_read
oam_stress                  other                   PaddleTest3
pal_apu_tests               ppu_open_bus            ppu_read_buffer
ppu_vbl_nmi                 read_joy3               scanline
scanline-a1                 scrolltest              soundtest
sprdma_and_dmc_dma          sprite_hit_tests_2005.10.05
sprite_overflow_tests       spritecans-2011         stars_se
stomper                     stress                  tutor
tvpassfail                  vaus-test               vbl_nmi_timing
volume_tests                window5
（+ status.txt, test_roms.xml）
```

> **法的な注意**: テスト ROM の多くはフリーに配布されているが、本プロジェクトのリポジトリには**含めない**。開発者が自分でダウンロードするための取得スクリプトと、ローカルパスを指す設定を用意する。

## 3. blargg 形式テストの自動判定プロトコル（重要）

blargg のテスト ROM は**エミュレータが自動で合否を判定できる**仕組みを持っている。これを実装すれば CI で回せる。

| アドレス | 内容 |
|---|---|
| **$6000** | テストステータス。`$80` = 実行中、`$81` = リセットボタンを（今から 100 ms 以上遅らせて）押す必要がある、`$00`–`$7F` = 完了しこの結果コードを返した |
| **$6001–$6003** | 有効性シグネチャ `$DE $B0 $61`。これが書かれていれば「$6000 以降のデータは有効」と判断できる |
| **$6004 以降** | テキスト出力。ゼロバイト終端。テキストが増えると終端が前に移動するので、エミュレータはいつでも現在のテキストを表示できる |

結果コードの意味: `0` = 合格、`1` = 不合格、`2` 以上 = 具体的な理由（テストのソースの `set_test N` の行を見る）。

音声出力もある（バイトをトーン列で報告。低音 = 0、高音 = 1、先頭のゼロは省略。最初のトーンは常に 0）。

> Wiki / readme には `$DE $B0 $G1` と書かれているが `$G1` は明らかな誤記で、実際のバイトは `$DE $B0 $61`。実装時は 3 バイトの一致を確認すること。

**自動テストランナーの設計**:

1. ROM をロードし、$6001–$6003 が `$DE $B0 $61` になるまで実行する（タイムアウトを設ける）
2. $6000 が `$80` の間は実行を続ける
3. $6000 が `$81` になったら 100 ms 以上待ってリセットする
4. $6000 が `$00`–`$7F` になったら終了。`$00` なら合格、それ以外は $6004 以降のテキストを失敗理由として記録する

## 4. nestest（CPU の最初の検証）

**CPU エミュレータを最初に動かすときに使う最良のテスト。** kevtris 作。

- ROM: http://nickmass.com/images/nestest.nes
- ドキュメント: https://www.qmtpro.com/~nes/misc/nestest.txt
- 既知の正しいログ: https://www.qmtpro.com/~nes/misc/nestest.log

使い方: **実行を $C000 から開始し**、既知の正しいログと 1 行ずつ比較する。ログは Nintendulator（CPU が正しく動作することが検証されたエミュレータ。電源投入時の状態の細部を除く）で作られたもの。

ログの形式（1 命令 1 行）:

```
C000  4C F5 C5  JMP $C5F5                       A:00 X:00 Y:00 P:24 SP:FD PPU:  0, 21 CYC:7
```

- PC、機械語バイト列、逆アセンブル、レジスタ、PPU の（スキャンライン, ドット）、累積 CPU サイクル数

**このログとの完全一致を第 1 マイルストーンにする。** 逆アセンブラの検証にもなるので二重に価値がある（project.md は逆アセンブル機能を要求している）。

> 注意: nestest の自動モード（$C000 開始）は PPU を使わない。通常起動（リセットベクタから）では画面に結果が出るが、こちらは PPU が動いてから使う。

## 5. 段階ごとのテスト計画

### フェーズ 1: CPU（公式命令）

| テスト | 作者 | 目的 |
|---|---|---|
| **nestest**（$C000 開始、ログ比較） | kevtris | 公式命令と基本タイミングの網羅的検証。**最初にこれを通す** |
| `instr_test-v5/official_only.nes` | blargg | 公式命令の網羅的検証（チェックサム方式） |
| `instr_misc` | blargg | 16 bit アドレスのラップアラウンド、ダミーリードなど雑多な挙動 |
| `cpu_reset` | blargg | 電源投入直後の CPU レジスタ、リセット時の変化、リセットで RAM が変わらないこと |

### フェーズ 2: CPU（非公式命令・タイミング）

| テスト | 作者 | 目的 |
|---|---|---|
| `instr_test-v5/all_instrs.nes` | blargg | 非公式命令を含む全命令 |
| `instr_timing` | blargg | 全命令のタイミング（非公式命令、ページ越えを含む） |
| `cpu_timing_test6` | blargg | 全公式・非公式命令の命令タイミング（分岐 8 個と HLT 12 個を除く） |
| `branch_timing_tests` | blargg | 分岐命令のタイミング（エッジケースを含む） |
| `cpu_dummy_reads` | blargg | **ダミーリード**（→ `02_cpu_6502.md` §5.1） |
| `cpu_dummy_writes` | bisqwit | **ダミーライト**（RMW 命令の二重ライト） |
| `cpu_exec_space` | bisqwit | I/O 空間にマップされた場所からでもコードを実行できること |

### フェーズ 3: 割り込み

| テスト | 作者 | 目的 |
|---|---|---|
| `cpu_interrupts_v2` | blargg | **IRQ と NMI の挙動とタイミング。割り込みハイジャック、CLI/SEI/PLP の遅延**（→ `02_cpu_6502.md` §9） |

### フェーズ 4: PPU（基本）

| テスト | 作者 | 目的 |
|---|---|---|
| `blargg_ppu_tests_2005.09.15b` | blargg | パレット RAM、スプライト RAM など雑多な PPU テスト |
| `ppu_vbl_nmi` | blargg | **NTSC PPU の VBL フラグ、NMI 有効化、NMI 割り込みの挙動とタイミング。1 PPU クロック精度**。最重要 |
| `vbl_nmi_timing` | blargg | ppu_vbl_nmi の旧版 |
| `ppu_open_bus` | blargg | PPU のオープンバスビット / レジスタの読み出し |
| `ppu_read_buffer` | bisqwit | **$2007 のリードバッファ**を中心に NES の多くの側面を検証する大規模テストパック |
| `oam_read` | blargg | $2004 の読み出しが $2003 の現在アドレスの OAM バイトを返すこと |
| `oam_stress` | blargg | OAMADDR ($2003) と OAMDATA ($2004) の徹底テスト |
| `full_palette` | blargg | エンファシス全状態を含む全パレットの表示。PPU の直接色制御のデモ |
| `color_test` | rainwarrior | 任意の色を全画面表示 |

> `ppu_vbl_nmi` の readme に注意書きがある: 「**NES はクロック分周器が異なる値で起動することがよくあり、PPU タイミングがわずかに違って一部のテストが失敗する。** これらのテストは最もよく文書化されエミュレートされているタイミングを検証する」。つまり CPU/PPU アライメントの選択によって結果が変わるテストがある。→ **エミュレータのアライメントを固定値（最も特殊ケースが少ないもの）にし、設定で変えられるようにする**（→ `03_ppu.md` §11）。

`ppu_vbl_nmi` のサブテスト（readme より、期待値の一部）:

- `01-vbl_basics` — 基本的な VBL 動作と VBL 期間。「$2002 は $200A にミラーされるべき」「$2002 は $2FFA まで 8 バイトごとにミラーされるべき」「BG off でも VBL 期間が短くなりすぎない/長くなりすぎない」
- `02-vbl_set_time` — VBL フラグがセットされる時刻。$2002 を 2 回読み、1 PPU クロックずつ遅らせて実行。**04 の行で「フラグのセットが抑止される」**
- `03-vbl_clear_time` — VBL フラグがクリアされる時刻（06 の行から `-` になる）
- `04-nmi_control` — VBL フラグが既に立っているときに NMI を有効化したときの即時 NMI。「**即時発生は次の命令の後**」
- `05-nmi_timing` — NMI のタイミング

### フェーズ 5: PPU（スプライト・スクロール）

| テスト | 作者 | 目的 |
|---|---|---|
| `ppu_sprite_hit` / `sprite_hit_tests_2005.10.05` | blargg | **スプライト 0 ヒットの挙動とタイミング** |
| `sprite_overflow_tests` | blargg | **スプライトオーバーフローの挙動とタイミング**（→ `03_ppu.md` §7.2 のバグ） |
| `oamtest3` | lidnariq | $2003/$2004 経由の OAM アップロード。**OAMADDR バグの検証に使える** |
| `scanline` | Quietust | エミュレーションが完全でないとグリッチが出るテスト画面 |
| `scrolltest` | – | スクロール |
| `nmi_sync` | blargg | 画面に特定のパターンを作って NMI タイミングを検証（NTSC / PAL 版） |
| `sprdma_and_dmc_dma` | blargg | **スプライト DMA 実行中の DMC DMA のサイクル奪取** |

### フェーズ 6: APU

| テスト | 作者 | 目的 |
|---|---|---|
| `apu_test` | blargg | CPU から見える APU の多くの側面 |
| `blargg_apu_2005.07.30` | blargg | レングスカウンタ、フレームカウンタ、IRQ など |
| `apu_reset` | blargg | 電源投入時の APU 状態とリセットの効果 |
| `apu_mixer` | blargg | **ミキサーの動作（チャンネル間の相対音量と非線形ミキシング）**。NES 実機での録音も同梱されているが、必要ないように作られている |
| `test_apu_env` | blargg | エンベロープ |
| `test_apu_sweep` | blargg | スイープの加算・減算・オーバーフローカットオフ・最小周期 |
| `test_apu_timers` | blargg | 5 チャンネルすべての周波数タイマー |
| `test_tri_lin_ctr` | blargg | Triangle のリニアカウンタとフレームカウンタによるクロック |
| `square_timer_div2` | blargg | Pulse タイマーの周期 |
| `apu_phase_reset` | Rahsennor | **$4003/$4007 への書き込みでデューティシーケンサはリセットされるがクロック分周器はリセットされないこと** |
| `test_apu_2 (1-11)` | x0000 | フレームカウンタを含む多数の挙動 |
| `dmc_dma_during_read4` | blargg | **DMA 実行中のレジスタ読み書きの挙動**（→ `07_...` §3.6） |
| `dmc_tests` | ? | DMC |
| `dpcmletterbox` | tepples | DMC チャンネルの IRQ 精度 |
| `volume_tests` | tepples | $4011 の各設定における全チャンネルの相対音量。NES の音声出力の録音を同梱 |
| `pal_apu_tests` | blargg | `blargg_apu_2005.07.30` の PAL 版 |

### フェーズ 7: マッパー

| テスト | 作者 | 目的 |
|---|---|---|
| **Holy Mapperel** | tepples | **十数種のマッパーを検出し、全 PRG/CHR ROM バンクが到達可能、PRG/CHR RAM が書いて読み戻せる、ネームテーブルミラーリング・IRQ・WRAM 保護が動くことを検証。マッパー検証の本命** |
| `mmc3_test` / `mmc3_test_2` | blargg | **MMC3 のスキャンラインカウンタと IRQ 生成** |
| `mmc3_irq_tests` | blargg | MMC3 IRQ |
| `mmc3irqtest` | N-K | MMC3 スキャンライン IRQ と $C000 グリッチの調査 |
| `MMC1_A12` | – | MMC1 の A12 |
| `serom` | lidnariq | MMC1 の SEROM / SHROM / SH1ROM バリアントの制約 |
| `mmc1atest` | tepples | MMC1A vs MMC1B の挙動 |
| `BNTest` / `bxrom_512k_test` | tepples / rainwarrior | BxROM と AxROM で到達可能な PRG バンク数 |
| NES 2.0 サブマッパーテスト（`2_test`, `3_test`, `7_test`, `34_test`） | rainwarrior | 各マッパーのサブマッパー 0/1/2（バス競合の有無） |
| `mmc5test` / `mmc5test_v2` / `exram` | Drag / AWJ / Quietust | MMC5（将来） |
| `vrc24test` / `vrc6test` | AWJ / natt | VRC 系（将来） |

### フェーズ 8: 入力

| テスト | 作者 | 目的 |
|---|---|---|
| `allpads` | tepples | NES / SNES コントローラ、Famicom マイク、Four Score、Zapper、Arkanoid、SNES マウスに対応した複合テスト。raw 32 bit レポートモードあり |
| `read_joy3` | blargg | **各種 NES コントローラのテスト。DMC DMA によるリード破損を含む** |
| `dma_sync_test_v2` | Rahsennor | **DMC DMA のリード破損** |
| `ctrltest` | rainwarrior | 5 本の入力データラインの 16 bit レポートのログ |
| `Telling LYs?` | tepples | 入力が任意のスキャンラインで変化できるか |
| `Zap Ruder` | tepples | Zapper（将来） |
| `PaddleTest3` / `vaus` / `vaus_test` | 3gengames / lidnariq / tepples | Arkanoid コントローラ（将来） |

### フェーズ 9: 総合・画質

| テスト | 作者 | 目的 |
|---|---|---|
| `240pee`（240p Test Suite） | tepples / Artemio Urbina | 総合的な映像テストスイート。MDFourier トーンジェネレータも含む |
| `ntsc_torture` | rainwarrior | NTSC 信号のアーティファクトを示す視覚パターン |
| `tvpassfail` | tepples | NTSC 色と NTSC/PAL のピクセルアスペクト比 |
| `palette` | rainwarrior | スキャンライン単位のパレット変更だけで全パレットを表示 |
| `NEStress` | Flubba | 古いテスト。**一部のテストは実機でも失敗するはず**（そのまま合否に使わない） |
| `oc-r1a` | tepples | NES の正確なクロックレートを検出・表示 |

## 6. 自動テストの仕組み（Wiki の推奨）

> 「ボタン 1 つでテストスイートを自動実行できるのが最善。変更するたびに手間なく再実行できる。自動化は難しいことがある。エミュレータが人の手を借りずに成否を判定できなければならないからだ」

### 6.1 ムービー（入力記録）機能

自動テストの第 1 の部品は**ムービー（どのボタンがいつ押されたかのリスト）**。

- エミュレータはユーザーのプレイ中の押下を記録してムービーを作る
- 再生時は記録された押下を入力システムに流し込む
- 自動テストだけでなく、スピードランナーにとっても魅力的になる

### 6.2 テストケースの作り方

1. ROM 内の全テストを起動するムービーを記録する
2. 各結果画面のスクリーンショットを取り、**時刻とスクリーンショットのハッシュを記録する**
3. 実行時はムービーを早送り（フレーム間の遅延なし）で再生し、同じ時刻にスクリーンショットを取る
4. ハッシュが違えばログに記録する
5. 前のリリースの同じテストケースの出力とフレーム単位で比較できる

最も単純なテスト ROM はボタン押下を必要としない。複数のことをテストする ROM は必要とする可能性が高い。実際のゲームはプレイスルーが必要。

## 7. 実装方針と、その理由

### 7.1 テストランナーを最初に作る

**Go のテストとして実装する。** `go test ./...` で全テスト ROM を回せるようにする。

```
internal/testrom/
    runner.go        // blargg プロトコルの自動判定（§3）
    nestest.go       // nestest.log との突き合わせ（§4）
    screenshot.go    // フレームハッシュ比較（§6.2）
testdata/
    roms/            // .gitignore。取得スクリプトでダウンロードする
    golden/          // 期待されるログ・ハッシュ（これはコミットする）
```

理由:

1. **CPU の実装は「nestest.log と 1 行ずつ一致する」という機械的な基準があるので、TDD が自然に成立する。** 手動確認では絶対に到達できない精度が出る
2. blargg プロトコル（§3）を実装すれば、大半のテストが「$6000 が 0 になるか」という単純な判定になる
3. PPU の検証は最終的に画面出力のハッシュ比較に落ちる。ここを最初に用意しておかないと、後から「どこから壊れたか」を追えなくなる

### 7.2 テスト ROM は取得スクリプトで

`tools/fetch-test-roms.sh`（または Go プログラム）で `christopherpow/nes-test-roms` を `testdata/roms/` にクローンまたはダウンロードする。**リポジトリには ROM を含めない。**

ROM が無い環境ではテストを `t.Skip()` する（CI で ROM を取得する手順を用意する）。

### 7.3 マイルストーンの定義に使う

各開発フェーズの完了条件を「このテストが通ること」で定義する（→ `docs/plans/`）。曖昧な「動くようになった」を排除する。

### 7.4 実機での目視確認も必要なもの

テスト ROM で自動判定できないもの:

- パレットの色合い（実機と TV の組み合わせで変わる。§`03_ppu.md` §10.3）
- オーディオの音質（`apu_mixer` と `volume_tests` に実機録音が同梱されているので比較できるが、最終判断は耳）
- NTSC のアーティファクト表現

これらは「録音・スクリーンショットとの比較 + 目視/試聴」で確認し、テストの合否には含めない。

## 8. 未解決・後続調査

- [ ] `test_roms.xml` / `status.txt` の内容（リポジトリのメタデータ。テスト一覧の自動生成に使えるか）
- [ ] nestest.log のフォーマットの完全な仕様（PPU カウンタの表記）
- [ ] `ppu_read_buffer`（bisqwit の大規模テスト）の全サブテスト内容

## 9. 参考資料

| 資料 | URL | 参照日 |
|---|---|---|
| NESdev Wiki: Emulator tests | https://www.nesdev.org/wiki/Emulator_tests | 2026-09-20 |
| nes-test-roms（アーカイブ） | https://github.com/christopherpow/nes-test-roms | 2026-09-20 |
| blargg テストの出力プロトコル（cpu_interrupts_v2 readme） | https://github.com/christopherpow/nes-test-roms/blob/master/cpu_interrupts_v2/readme.txt | 2026-09-20 |
| ppu_vbl_nmi readme | https://github.com/christopherpow/nes-test-roms/blob/master/ppu_vbl_nmi/readme.txt | 2026-09-20 |
| instr_test-v5 readme | https://github.com/christopherpow/nes-test-roms/blob/master/instr_test-v5/readme.txt | 2026-09-20 |
| nestest | http://nickmass.com/images/nestest.nes | 2026-09-20 |
| nestest ドキュメント | https://www.qmtpro.com/~nes/misc/nestest.txt | 2026-09-20 |
| nestest.log（正解ログ） | https://www.qmtpro.com/~nes/misc/nestest.log | 2026-09-20 |
| Holy Mapperel | https://github.com/pinobatch/holy-mapperel | 2026-09-20 |
| allpads | https://github.com/pinobatch/allpads-nes | 2026-09-20 |
| nes-audio-tests | https://github.com/bbbradsmith/nes-audio-tests | 2026-09-20 |
