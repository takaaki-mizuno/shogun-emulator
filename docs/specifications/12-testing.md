# 12 テスト設計

- 文書バージョン: 1.1
- 作成日: 2026-09-21
- 更新日: 2026-10-04（Agent Interface の追加に伴い §12.1・§12.6・§12.7・§12.8・§12.9 を更新し、§12.11 を追加）
- 対象システム: Shogun Emulator（将軍エミュレータ）

---

## 12.1 テストの層

| 層 | 対象 | 実行方法 |
|---|---|---|
| 単体テスト | 各パッケージの関数と型 | `go test ./...` |
| テスト ROM | CPU・PPU・APU・マッパーの挙動 | `go test ./internal/testrom` |
| 往復テスト | 状態直列化の網羅性 | `go test ./internal/testrom -run Roundtrip` |
| 決定論テスト | ホスト由来の非決定性 | `go test ./internal/testrom -run Determinism` |
| 静的検査 | 依存方向とスレッド境界 | `go test ./internal/arch` |
| Agent Interface | Agent Command・Transport・常時記録・Scenario・Diagnostic（§12.11） | `go test ./internal/agent/... ./internal/debug/...` |
| レース検出 | ゴルーチン間の受け渡し | `go test -race $(go list ./... \| grep -v internal/testrom)` |

テスト ROM を必要とするテストは、ROM が存在しないとき `t.Skip` する。

レース検出の対象から `internal/testrom` を外す。並行処理を持つのは `internal/emu` と `internal/ui` であり、テスト ROM が動かすのは単一ゴルーチンのコアである（`internal/nes` がゴルーチンを持たないことは §12.7 の静的検査が保証する）。レース検出はメモリアクセスごとに計測を挟むため、1 秒あたり数百万回のバスアクセスを行うテスト ROM の実行時間が 20 倍以上になる。得られるものがないまま実行時間だけが延びる。

## 12.2 テスト ROM の取得

テスト ROM をリポジトリに含めない。取得スクリプトで `testdata/roms/` へ配置する。

```
tools/fetch-test-roms.go     取得スクリプト
testdata/roms/               取得先。バージョン管理の対象外
testdata/golden/             期待値。バージョン管理の対象
```

期待値として次を管理する。

| 内容 | ファイル |
|---|---|
| nestest の正解ログ | `testdata/golden/nestest.log` |
| テスト ROM ごとの期待結果コード | `testdata/golden/testroms.json` |
| フレームハッシュ | `testdata/golden/frames/<rom>.json` |
| 入力ムービー | `testdata/golden/movies/<name>.movie` |

## 12.3 nestest による CPU の検証

`nestest.nes` を `$C000` から実行し、既知の正解ログと 1 行ずつ比較する。

```go
func TestNestest(t *testing.T) {
    n := loadROM(t, "nestest.nes")
    n.PowerOn(deterministicInit())
    n.CPU.PC = 0xC000

    golden := openGolden(t, "nestest.log")
    for i := 0; ; i++ {
        line := n.CPU.State().TraceLine()
        want, ok := golden.Next()
        if !ok {
            break
        }
        if line != want {
            t.Fatalf("行 %d で不一致\n実際: %s\n期待: %s", i+1, line, want)
        }
        n.StepInstruction()
    }
}
```

比較する項目に PC・機械語バイト列・逆アセンブル結果・A・X・Y・P・S・PPU の位置・累積サイクル数を含む。逆アセンブラの検証も同時に行える。

このテストを CPU 実装の最初の完了条件とする。

## 12.4 往復テスト

状態の保存漏れを検出する。

```go
func TestRoundtrip(t *testing.T, romPath string, warmupFrames, compareFrames int) {
    a := newNES(t, romPath)
    a.RunFrames(warmupFrames)

    blob := a.SaveState()

    // 経路 1: そのまま進める
    a.RunFrames(compareFrames)
    want := a.SaveState()

    // 経路 2: 復元してから進める
    b := newNES(t, romPath)
    if err := b.LoadState(blob); err != nil {
        t.Fatal(err)
    }
    b.RunFrames(compareFrames)
    got := b.SaveState()

    if !bytes.Equal(want, got) {
        t.Fatalf("状態が一致しない。最初の差分: %s", state.FirstDiff(want, got))
    }
}
```

比較を直列化した結果のバイト列で行う。構造体を直接比較しないのは、直列化に含めていないフィールドを検出する必要があるためである。直列化した結果が一致すれば、直列化の対象となる状態はすべて復元されている。

直列化に含めていないフィールドが挙動に影響する場合、経路 1 と経路 2 で `compareFrames` 進めた後の状態が異なる。これにより保存漏れが検出される。

`state.FirstDiff` はセクションヘッダを解析し、最初に内容が異なるセクションの位置を、階層をドットで連結した形（`nes.ppu.sprites` など）で返す。CPU・PPU・APU・カートリッジのどこに漏れがあるかを示す。

### 12.4.1 全フレーム往復テスト

```go
func TestRoundtripEveryFrame(t *testing.T) {
    for _, rom := range roundtripROMs {
        for frame := 0; frame < 600; frame++ {
            TestRoundtrip(t, rom, frame, 1)
        }
    }
}
```

600 フレーム分、毎フレームで往復を検証する。1 フレームだけ進めて比較するため、差分が現れた原因のフレームが特定できる。

実行時間が長いため、`-short` を指定したときは 60 フレームに減らす。

### 12.4.2 対象 ROM

| ROM | 検証対象 |
|---|---|
| NROM のテスト ROM | CPU・PPU・APU の基本 |
| MMC1 を使う ROM | シリアルポートの状態、`lastWriteCycle` |
| MMC3 を使う ROM | A12 フィルタの状態、IRQ カウンタ |
| DPCM を再生する ROM | DMC の DMA 途中の状態 |
| スプライトオーバーフローを使う ROM | スプライト評価の途中状態 |

## 12.5 テスト ROM ランナー

blargg 形式のテスト ROM は結果をメモリへ書き出す。これを判定に用いる。

```go
package testrom

type Result struct {
    Code    uint8
    Message string
}

// RunBlargg は $6001-$6003 のシグネチャを待ち、$6000 の状態を監視する。
func RunBlargg(n *nes.NES, timeout int) (Result, error) {
    for frame := 0; frame < timeout; frame++ {
        n.RunFrame()
        if !hasSignature(n) {
            continue
        }
        switch s := n.Bus.Peek(0x6000); {
        case s == 0x80:
            continue                      // 実行中
        case s == 0x81:
            n.RunFrames(10)               // 100 ms 以上待つ
            n.Reset()
        default:
            return Result{Code: s, Message: readMessage(n)}, nil
        }
    }
    return Result{}, errTimeout
}

func hasSignature(n *nes.NES) bool {
    return n.Bus.Peek(0x6001) == 0xDE &&
        n.Bus.Peek(0x6002) == 0xB0 &&
        n.Bus.Peek(0x6003) == 0x61
}
```

`$6000` の値が `$00` のとき合格、それ以外のとき不合格とする。`$6004` 以降のゼロ終端文字列を失敗の内容として記録する。

メモリの読み出しに `Bus.Peek` を用いる。テストの観測がエミュレーション状態を変えないためである。

### 12.5.1 期待値の管理

```json
{
  "instr_test-v5/official_only.nes": { "expect": 0, "timeout": 3600 },
  "ppu_vbl_nmi/ppu_vbl_nmi.nes":     { "expect": 0, "timeout": 7200 },
  "sprite_overflow_tests/1.Basics.nes": { "expect": 0, "timeout": 1800 }
}
```

期待値が 0 でない ROM も記録する。実機でも特定の結果を返すテストがあるためである。

音の検証は結果コードでは行えない。次の 2 つで確かめる。

| 検証 | 方法 |
|---|---|
| 波形が出ていること | APU からリサンプラまでを通し、振幅とゼロ交差の数を見る |
| 音の高さが正しいこと | タイマー値を指定して鳴らし、平均をまたぐ回数から周波数を求め、`f_CPU / (16 × (t + 1))`（Triangle は 32）と比べる |

周波数の検証は、APU のタイマーからミキサー、リサンプラまでの経路のどこかで
2 倍または半分になっていることを検出する。出力デバイスを必要としない。

### 12.5.2 ROM の取得

ROM はリポジトリに含めず、`tools/fetch-test-roms` で `testdata/roms/` へ配置する。
配置先は `.gitignore` の対象とする。ROM が無いとき、対応するテストを飛ばす。

| 取得元 | 固定する値 | 取得するもの |
|---|---|---|
| christopherpow/nes-test-roms | コミット | blargg らのテスト ROM 群、`nestest.nes`、`nestest.log` |

| pinobatch/allpads-nes | タグ | `allpads.nes`、`allpads218.nes` |
| pinobatch/holy-mapperel | タグ | マッパーのテスト ROM 群 |

取得元はコミットまたはタグで固定する。上流が変わっても取得する内容が変わらず、
SHA-256 の照合が意味を持つようにするためである。

取得した各ファイルの SHA-256 を `testdata/golden/romhashes.json` と照合し、
不一致のとき失敗する。期待値が別の内容の ROM に対して評価される状態を防ぐ。

holy-mapperel は 7z 形式で配布されている。展開には `7z`・`7zz`・`7za` のいずれかの
コマンドを使う。見つからないとき、書庫をそのまま配置して展開の手順を表示し、
他の ROM の取得を続ける。7z の展開のために外部モジュールを追加しない。

次の ROM は固定できる配布元が無く、手で `testdata/roms/` へ配置する。

| ROM | 検証対象 |
|---|---|
| `test_apu_env`、`test_apu_sweep`、`test_apu_timers`、`test_tri_lin_ctr` | APU の各部品の周期 |
| `apu_phase_reset` | APU の位相のリセット |
| `serom`、`mmc1atest` | MMC1 の SEROM 構成、MMC1A と MMC1B の違い |
| `BNTest`、`bxrom_512k_test` | オーバーサイズの PRG バンク |
| `2_test`、`3_test`、`7_test` | NES 2.0 のサブマッパーとバス競合 |

## 12.6 フェーズごとの完了条件

実装の各段階の完了条件を、通過すべきテスト ROM で定める。

| 段階 | 完了条件 |
|---|---|
| CPU（公式命令） | nestest のログ一致、`instr_test-v5/official_only`、`instr_misc`、`cpu_reset` |
| CPU（非公式命令とタイミング） | `instr_test-v5/all_instrs`、`instr_timing`、`cpu_timing_test6`、`branch_timing_tests`、`cpu_dummy_reads`、`cpu_dummy_writes`、`cpu_exec_space` |
| 割り込み | `cpu_interrupts_v2` |
| PPU（基本） | `ppu_vbl_nmi`、`blargg_ppu_tests`、`ppu_open_bus`、`oam_read`、`oam_stress` |
| PPU（スプライトとスクロール） | `sprite_hit_tests`、`sprite_overflow_tests`、`oam_read`、`oam_stress` |
| APU | `apu_test`、`blargg_apu`、`apu_reset`、`apu_mixer`、`dmc_dma_during_read4` |
| マッパー | Holy Mapperel、`mmc3_test_2`、`mmc3_irq_tests`、`MMC1_A12`、`serom`、NES 2.0 サブマッパーテスト |
| 入力 | `allpads`、`read_joy3` |
| DMA | `dmc_dma_during_read4/dma_2007_write`、`read_write_2007`、`cpu_interrupts_v2/rom_singles/4-irq_and_dma` |
| 総合 | 240p Test Suite の各項目の目視確認 |
| Agent Interface | §12.11 のテストがすべて合格し、§12.8.5 の決定論テストが合格する。段階ごとの条件は `docs/plans/` のフェーズ 14–20 に定める |

### 12.6.1 テスト ROM の前提

テスト ROM は CPU 以外のコンポーネントを時間の基準や観測手段に使うものが多い。
それぞれの前提が揃った段階で検証する。

| テスト ROM | 前提 |
|---|---|
| `instr_test-v5/rom_singles/` の全 ROM | CPU のみ |
| `instr_misc/rom_singles/01-abs_x_wrap`、`02-branch_wrap` | CPU のみ |
| `cpu_exec_space/test_cpu_exec_space_apu` | CPU のみ |
| `cpu_reset/registers`、`ram_after_reset` | CPU のみ |
| `instr_misc/rom_singles/03-dummy_reads` | `$2002` の読み出し |
| `cpu_exec_space/test_cpu_exec_space_ppuio` | PPU レジスタ |
| `ppu_vbl_nmi/rom_singles/` の全 ROM | PPU のフレームタイミング |
| `ppu_open_bus` | PPU の I/O ラッチ |
| `blargg_ppu_tests_2005.09.15b/palette_ram`、`vram_access`、`vbl_clear_time` | PPU のメモリ |
| `branch_timing_tests/` の全 ROM、`cpu_timing_test6` | PPU の描画 |
| `cpu_dummy_writes/cpu_dummy_writes_ppumem` | PPU のメモリ |
| `cpu_interrupts_v2/rom_singles/2-nmi_and_brk`、`3-nmi_and_irq` | PPU の NMI |
| `cpu_dummy_writes/cpu_dummy_writes_oam`、`blargg_ppu_tests_2005.09.15b/sprite_ram` | OAM とスプライト評価 |
| `sprite_hit_tests_2005.10.05/` の全 ROM、`sprite_overflow_tests/` の全 ROM、`oam_read`、`oam_stress` | スプライトと OAM DMA |
| `instr_timing/rom_singles/` の全 ROM | APU の長さカウンタ |
| `instr_misc/rom_singles/04-dummy_reads_apu` | APU |
| `cpu_interrupts_v2/rom_singles/1-cli_latency`、`5-branch_delays_irq` | APU のフレーム IRQ |
| `cpu_interrupts_v2/rom_singles/4-irq_and_dma` | DMC DMA |
| `ppu_read_buffer/test_ppu_read_buffer` | スプライト 0 ヒット・OAM DMA・DMA の精密なサイクル |
| `read_joy3/test_buttons`、`thorough_test` | コントローラのシフトレジスタ |
| `allpads` のリセット後の測定画面 | コントローラのデータ線とオープンバス |
| `allpads` の一覧画面 | APU のフレームカウンタ |
| 240p Test Suite | マッパー 2（UxROM） |
| `instr_test-v5/all_instrs`、`official_only`、`instr_misc/instr_misc`、`instr_timing/instr_timing`、`cpu_interrupts_v2/cpu_interrupts` | MMC1 |
| Holy Mapperel の各 ROM | 該当するマッパー |
| `mmc3_test_2/`、`mmc3_irq_tests/` の全 ROM | MMC3 の A12 監視とスキャンライン IRQ |
| `MMC1_A12/mmc1_a12` | MMC1 の PRG-RAM 無効化ビット |

`instr_test-v5` などは分割された ROM とまとめた ROM の両方が配布されている。
分割された ROM はマッパー 0 で動く。検証する内容は同じであるため、マッパーを
実装するまでは分割された ROM を使う。

`blargg_ppu_tests_2005.09.15b/power_up_palette` は検証しない。電源投入時の
パレット RAM の内容を特定の値と比べるテストであり、実機では値が定まらない。

`cpu_interrupts_v2/rom_singles/2-nmi_and_brk` は未達である。NMI が BRK を
横取りしたときにスタックへ積まれる P の値が実機と違う。割り込みの経路の
問題であり、APU の実装とは関係がない。まとめた `cpu_interrupts_v2/cpu_interrupts`
は 5 つの副試験を順に実行し、この 2 番目で止まる。分割された ROM のうち
未達の 1 本を除く 4 本で検証する。

`cpu_dummy_reads` は結果を `$6000` へ書かず画面に表示する（§12.6.2）。

`blargg_apu_2005.07.30/10.len_halt_timing` と `11.len_reload_timing` は
検証しない。レジスタへの書き込みとレングスカウンタのクロックが同一の CPU
サイクルに重なったときの順序を見るテストで、合格する条件を特定できていない。
この 2 つに相当するテストは、同じ作者による新しい版の `apu_test` には無い。

`test_apu_env`・`test_apu_sweep`・`test_apu_timers`・`test_tri_lin_ctr`・
`square_timer_div2`・`apu_phase_reset`・`test_apu_2` は取得できない。§12.2 の
取得元に含まれておらず、安定した配布元が見つからない。これらが検証する
エンベロープ・スイープ・タイマー・リニアカウンタの挙動は、部品ごとの単体
テスト（`internal/nes/apu`）と、波形の周波数を測るテスト（§12.5.1）で
確かめる。

`mmc3_test_2/rom_singles/6-MMC3_alt` と `mmc3_irq_tests/5.MMC3_rev_A` は
`emulation.mmc3IrqVariant` が `nec` のときに合格する。`5-MMC3` と
`6.MMC3_rev_B` は `sharp` のときに合格する。実機に 2 種類が存在し、
一方の版の挙動を検証する ROM は他方の版では不合格になる。

`MMC1_A12/mmc1_a12` は合否を表示しない。遅延を手で変えながら画面を見る
道具である。表示まで進むことをフレームハッシュで確かめる。

`serom` は §12.5.2 のとおり手で配置する ROM である。`mmc1atest`・`BNTest`・
`bxrom_512k_test`・`mmc3irqtest`（N-K）・NES 2.0 サブマッパーテスト
（`2_test`・`3_test`・`7_test`）は固定できる配布元が見つからない。これらが
検証するサブマッパー 5（`$E000` が無機能な構成）とバス競合の有無は、
単体テスト（`internal/nes/cart`）で確かめる。

### 12.6.2 画面に結果を表示するテスト ROM

結果を `$6000` へ書かず画面に表示する ROM がある。2005 年版の blargg の
テストは結果コードを `$01` の形で表示し、命令のタイミングを測るテストは
`PASSED` と表示する。

blargg のテスト ROM はタイル番号を ASCII コードと同じに並べた CHR を使う。
ネームテーブルのバイト列をそのまま文字として読むことで、画面の内容を
機械的に判定できる。`testrom.ScreenText` がこの変換を行う。

Holy Mapperel と `mmc3_irq_tests` は字形を `$01` から並べた CHR を使う。
数字と記号は ASCII と同じ位置にあるため、`$01`–`$1A` だけを大文字へ
読み替える。`testrom.ScreenTextCompactFont` がこの変換を行う。

Holy Mapperel は基板の構成を自分で判別し、PRG と CHR の全バンクへの到達、
RAM の読み書き、ミラーリング、IRQ、WRAM 保護を順に試す。結果を画面の
「DETAILED TEST RESULT」に 16 進 4 桁で出す。`0000` が全項目の合格である。

## 12.7 静的検査

依存方向とスレッド境界を機械的に検査する。

```go
package arch

func TestCoreHasNoUIDependency(t *testing.T) {
    // GUI はどこからも参照させない（internal/ui を除く）
    for _, pkg := range []string{
        "internal/nes/...", "internal/emu/...", "internal/debug/...",
    } {
        assertNoImport(t, pkg, []string{"fyne.io/"})
    }
    // オーディオデバイスを直接参照してよいのは internal/audio だけである。
    // internal/emu は internal/audio を介して進行の駆動に使う（§1.4）
    for _, pkg := range []string{"internal/nes/...", "internal/debug/..."} {
        assertNoImport(t, pkg, []string{"github.com/ebitengine/oto"})
    }
}

func TestDispatchIsTheOnlyFyneDoCaller(t *testing.T) {
    files := grepFiles(t, "internal/ui", "fyne.Do")
    if len(files) != 1 || filepath.Base(files[0]) != "dispatch.go" {
        t.Fatalf("fyne.Do を呼ぶファイル: %v", files)
    }
}

func TestNoTimeInEmulationCore(t *testing.T) {
    assertNoImport(t, "internal/nes/...", []string{"time"})
}

func TestNoGlobalRandInEmulationCore(t *testing.T) {
    assertNoImport(t, "internal/nes/...", []string{"math/rand"})
    assertNoSymbol(t, "internal/nes/...", []string{"rand.Intn", "rand.Uint64", "rand.Float64"})
}

func TestNoGoroutinesInEmulationCore(t *testing.T) {
    assertNoImport(t, "internal/nes/...", []string{"sync"})
    assertNoStatement(t, "internal/nes/...", []string{"go", "select"})
}

func TestNoMapIterationInEmulationCore(t *testing.T) {
    // internal/nes が map をたどらないことを検証する
}

func TestCoreImportsOnlyStdlibAndModule(t *testing.T) {
    // internal/nes が依存するのは標準ライブラリとモジュール内のパッケージだけである
}

func TestAgentDependencyRules(t *testing.T) {
    // 「14 Agent Interface 設計」§14.2.2 の規則
    // internal/agent 以下は GUI を参照しない
    assertNoImport(t, "internal/agent/...", []string{"internal/ui", "fyne.io/"})
    // MCP ブリッジは internal/agent を直接参照せず、rpc のクライアントだけを使う
    assertNoDirectImport(t, "internal/agent/mcpbridge", []string{"internal/agent"})
    // MCP SDK を参照してよいのは internal/agent/mcpbridge だけである
    assertOnlyImporter(t, "github.com/modelcontextprotocol/go-sdk", "internal/agent/mcpbridge")
    // コアは Agent Interface を参照しない
    assertNoImport(t, "internal/nes/...", []string{"internal/agent"})
    assertNoImport(t, "internal/emu/...", []string{"internal/agent"})
    assertNoImport(t, "internal/debug/...", []string{"internal/agent"})
}

func TestUITextLivesInCatalog(t *testing.T) {
    // internal/ui の _test.go 以外のファイル（internal/ui/i18n を除く）に
    // 日本語の文字列リテラルが無いことを検証する（「10 GUI 設計」§10.9）
}
```

対象のパッケージがまだ存在しないとき、検査は失敗せずに飛ばす。段階ごとにパッケージが増えるため、
存在しないことを失敗にすると早い段階の CI が通らなくなる。

`assertNoImport` は `go list -json ./...` の出力を解析してモジュール内のインポートグラフを組み立て、そこを推移的にたどる。標準ライブラリの内部はたどらない。`fmt` が `os` を経由して `time` に至るような経路を違反としないためである。標準ライブラリだけで実装するのは、静的検査が外部モジュールの取得なしに動くようにするためである。

`assertNoDirectImport` は直接のインポートだけを見る。`internal/agent/mcpbridge` は `internal/agent/rpc` を介して推移的に `internal/agent` に依存するため、推移的にたどると規則を表せない。`assertOnlyImporter` はモジュール内で指定したモジュールを直接インポートするパッケージが、指定したパッケージだけであることを確かめる。

`assertNoSymbol` と `grepFiles` は `go/parser` と `go/ast` で対象パッケージのファイルを走査する。文字列の一致ではなく構文木を見るのは、コメントと文字列リテラル内の記述を誤検出しないためである。

`map` のイテレーションの検査も構文木で行う。ファイルの中で `map` 型として宣言された名前を集め、`range` の対象がその名前であれば違反とする。型情報を使わないため見落としよりも過検出の側に倒れるが、`internal/nes` が `map` をたどらないという規約を機械的に保てる。テストのファイルは対象にしない。

## 12.8 決定論テスト

### 12.8.1 同一環境での再現

```go
func TestMovieReproducible(t *testing.T) {
    for _, m := range goldenMovies {
        h1 := playMovieAndHash(t, m)
        h2 := playMovieAndHash(t, m)
        if h1 != h2 {
            t.Fatalf("%s: 2 回の再生でハッシュが異なる", m)
        }
    }
}
```

`replayMovie` はムービーを再生する。記録されたチェックサムをその都度照合し、最後の状態のハッシュを返す。間隔ごとのハッシュを連結せずその場で照合するのは、食い違ったフレームを直接報告できるようにするためである。

golden ムービーは `SHOGUN_UPDATE_MOVIES=1` を設定して実行すると記録し直せる。入力はフレーム番号から決まる並びで与える。乱数を使わないのは、生成したムービーが実行ごとに変わらないようにするためである。

### 12.8.2 ムービーの desync 検出

golden ムービーを再生し、記録されたチェックサムと一致することを確認する。一致しないとき、最初に異なったフレーム番号を報告する。

エミュレーションの挙動を意図して変更したとき、このテストが失敗する。golden ムービーを記録し直し、変更内容を説明する。

### 12.8.3 デバッガを有効にした再生

`TestMovieUnchangedByDebugger` は、デバッガをつないだ状態で golden ムービーを再生し、つないでいない状態と最後のハッシュが一致することを確認する。つなぐデバッガでは、実行・読み出し・書き込み・PPU 位置・全イベントのブレークポイントを成り立たない条件式付きで置き、トレース・CPU の表示・変更追跡・スナップショット（フレーム末とスキャンライン 2 か所の購読）・全カテゴリのログ・APU の全チャンネルのミュートを有効にする。

`TestMovieChangesWithOverlay` は、PRG にオーバーレイの変更を置いて同じ入力を与えると最後のハッシュが変わり、オーバーレイを無効にすると元のハッシュに戻ることを確認する。オーバーレイは ROM の内容を変えるため、結果が変わるのが正しい挙動である。フックがエミュレーションの状態を読むだけで書き換えないことを、ハッシュの一致で保証する。

Agent Interface の機能（PPU 書き込みの記録・全項目の Diagnostic・プロファイル・トレースのバスアクセス記録）も、このテストでつなぐデバッガに含める。いずれも観測だけを行い、結果を変えないことを確かめる。Freeze は値を書き換えるため含めない。

### 12.8.4 クロスプラットフォーム

CI の macOS・Windows・Linux の各ランナーで、同じ golden ムービーのハッシュが一致することを確認する。ハッシュを成果物として保存し、ジョブ間で比較する。`TestDeterminismHashes` が `determinism <ムービー名> <ハッシュ>` の形で標準出力へ書き出す。

### 12.8.5 Agent Command の列の再実行

`TestAgentCommandsReproducible`（`internal/agent`）は、プロセス内の Host に同じ Agent Command の列（ROM の読み込み、`exec.step`・`exec.input_sequence`・`exec.run_until`、`mem.write`、`cpu.set`、`mem.freeze`、`exec.step_unit` でフレームの途中に止まってからの書き込みを含む）を 2 回与え、各 Observation（画像を含む）と最後の状態のハッシュが一致することを確かめる。

同じテストで、途中で Fork した 2 つの Instance に以降の同じ列を与え、両者の Observation が一致することも確かめる。

`TestAgentRecordingReplays` は、上の列の後に `repro.export` で書き出した SHGM（バージョン 2）を通常のムービー再生で再生し、最後の状態のハッシュが一致することを確かめる。介入のレコードが正しい位置に適用されることを保証する（「08 セーブステートと入力ムービー設計」§8.7.2）。

## 12.9 チェックリスト

コードレビューで確認する項目を次に示す。

| 項目 |
|---|
| `internal/nes` で `map` をイテレートしていない |
| `internal/nes` で `time` を参照していない |
| `internal/nes` でゴルーチンを生成していない |
| 新しい状態フィールドを `SaveState` と `LoadState` に追加した |
| 新しい状態フィールドを対応する編の「保存する状態」の表に追加した |
| ダミーリードとダミーライトを省略していない |
| `Bus.Read` と `Bus.Write` をデバッガから呼んでいない |
| 浮動小数点の値でエミュレーション状態の分岐をしていない |
| PAL のドット比を浮動小数点で累積していない |
| Agent Command がエミュレーション状態に直接触れず、コマンドキューか `WithMachine` を通している |
| 新しい Agent Command を登録簿に登録し、分類（観測・設定・進行・書き込み）を正しく付けた |
| Machine State を変える新しい操作を介入として常時記録に残している |
| Agent Interface の機能が、使われていないときにフックを `nil` に保っている |

## 12.10 ベンチマーク

```go
func BenchmarkFrame(b *testing.B) {
    n := newNES(b, "nestest.nes")
    n.PowerOn(deterministicInit())
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        n.RunFrame()
    }
}
```

1 フレームの実行時間を計測する。60 fps を維持するには 16.6 ms 以内に収める。フックを設定した場合と設定しない場合の両方を計測する。

`BenchmarkFrameWithDebugger` はデバッガの使い方ごとに 1 フレームの実行時間を計測する。

| 場合 | 設定 |
|---|---|
| なし | デバッガをつながない |
| ビューア 3 つ | CPU の表示・変更追跡・スナップショットを有効にする |
| ブレークポイント 10 個 | 実行と書き込みのブレークポイントを 5 個ずつ置く |
| トレース | トレースの記録を有効にする |
| PPU ビューア 5 つ | フレーム末とスキャンライン 1 か所のスナップショットを購読する |

`BenchmarkPPUViewersDraw`（`internal/ui`）は、PPU 系の 5 つのビューアを 1 回ずつ描き直す UI スレッドの費用を計測する。

## 12.11 Agent Interface のテスト

「14 Agent Interface 設計」§14.24 の各テストを次のとおり置く。

| テスト | パッケージ | 内容 |
|---|---|---|
| Agent Command の単体テスト | `internal/agent` | 各 Agent Command をプロセス内の Host に対して呼び、結果の形とエラーの `kind` を確かめる |
| Transport の往復 | `internal/agent/rpc`・`internal/agent/mcpbridge`・`cmd/shogun` | 同じ要求を JSON-RPC・MCP ブリッジ・`shogun ctl` で送り、同じ結果を得る。MCP は SDK のクライアントを `net.Pipe` でつないで確かめる |
| 名前の変換 | `internal/agent` | 登録簿のすべての名前が JSON-RPC・MCP・CLI の変換で往復できる |
| 決定論 | `internal/agent` | §12.8.5 |
| 常時記録の再生 | `internal/agent` | §12.8.5 の `TestAgentRecordingReplays` |
| Re-Reach | `internal/agent` | 同じ ROM で `rom.reload` と Re-Reach（`frame`）を行い、読み直す前と同じ状態ハッシュになる |
| `.dbg` の読み込み | `internal/debug` | `testdata/agent/` の `.dbg` から、バンクを区別した Symbol とソース行を得る。バージョン 1 のシンボルファイルの移行 |
| 式の拡張 | `internal/debug` | 「09 デバッガ設計」§9.6.1 の既存の式の結果が拡張後も変わらない。追加した要素の評価 |
| Game State | `internal/debug` | 各型と修飾の解釈。列挙の名前での比較 |
| Diagnostic | `internal/debug` | 各項目を起こすテスト ROM で検知する。検知を有効にしても §12.8.3 のハッシュが変わらない |
| Scenario | `internal/agent/scenario`・`cmd/shogun` | 合格する Scenario と失敗する Scenario を実行し、終了コード（0・5・6）と JUnit XML を確かめる。お手本の画像の比較と `--update-golden` |
| セキュリティ | `internal/agent/rpc` | トークンなし・誤ったトークンの接続を断る。発見ファイルとソケットのパーミッションが 0600。`127.0.0.1` と `::1` 以外の TCP アドレスを断る |

### 12.11.1 テスト用 ROM

Agent Interface のテストに使う ROM は自作し、`testdata/agent/` に ca65 のソースと共に置く。既存のエミュレータやゲームのコードを使わない（project.md のクリーンルームの規則）。

| ROM | 用途 |
|---|---|
| バンク切り替えを持つ ROM（マッパー 1 または 4） | 同じ CPU アドレスに異なる Symbol が置かれる場合の `.dbg` の読み込みとバンクを区別したブレークポイント |
| Game State を持つ小さなゲーム | 入力で座標と状態が変わる。進行・`exec.run_until`・Scenario・Re-Reach |
| Diagnostic を起こす ROM | §14.20.1 の各項目を、入力やフレーム番号で順に起こす |

ビルド済みの `.nes` と `.dbg` をリポジトリに含める。CI に cc65 を入れずに済ませるためである。テスト ROM のソースを直したときに作り直す手順は `tools/build-agent-testroms`（ca65/ld65 を呼ぶスクリプト）に置く。開発環境に cc65 が無い場合は、作り直しが必要になった時点で導入する。テストは ROM のソースではなく、リポジトリに含めたビルド済みのファイルを使う。

テスト ROM が存在しないとき、そのテストは `t.Skip` せずに失敗させる。テスト ROM はリポジトリに含めるため、存在しないのはリポジトリの不備だからである。

### 12.11.2 Scenario による利用者のテスト

利用者（ゲームの開発者）は Scenario（「14 Agent Interface 設計」§14.17）で自分のゲームのテストを書き、`shogun run` で CI に流す。エミュレータ自身のテストではないが、`shogun run` の挙動はこの節の約束に従う。

| 項目 | 約束 |
|---|---|
| 決定論 | Scenario の Instance は既定で `deterministic`。同じ Scenario は何度実行しても同じ結果になる。`timeout_ms` を使ったステップは警告する |
| 画面の比較 | パレット適用前の値で比べる（既定のパレットで描いた PNG と比べる）。パレットの設定に依らない |
| 終了コード | 0 合格、1 ROM などの読み込みの失敗、5 アサーションの失敗、6 Scenario ファイルの不正 |
| 出力 | 標準出力に成否・所要時間・失敗の内容。`--junit` で JUnit XML。失敗ごとに Repro |
| お手本 | `--update-golden` で作り、リポジトリに含める |

CI の例（GitHub Actions）と、`scenario.export` で書き出した Scenario にアサーションを足す手順は利用者向けの文書（`packaging/usage.md` の「自動テスト（Scenario）」）に置く。
