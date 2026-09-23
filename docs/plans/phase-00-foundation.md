# フェーズ 0: 基盤整備

- 作成日: 2026-09-21
- 前提フェーズ: なし
- 完了条件: CI が 3 OS で回り、テスト ROM を取得でき、静的検査が通る

## 1. 背景

エミュレータの実装に入る前に、開発を支える仕組みを用意する。とくに次の 3 つは、後から入れると既に書いたコードの手直しが発生する。

| 仕組み | 後から入れたときに起きること |
|---|---|
| 静的検査（依存方向とスレッド境界） | `internal/nes` から GUI を参照するコードが増えてから検査を入れると、広範囲の修正になる |
| 状態直列化の基盤（`state` パッケージ） | 各コンポーネントを書いた後に直列化を足すと、保存すべきフィールドの洗い出しをやり直すことになる |
| テスト ROM の取得と判定 | CPU を書き始めた時点で nestest との比較ができないと、誤りが積み上がる |

フェーズ 0 の時点では動くエミュレータはできない。それでも先にここを固めるのは、フェーズ 1 の最初のコミットから nestest と突き合わせられる状態にするためである。

## 2. 方針とその理由

### 2.1 テスト ROM をリポジトリに含めない

取得スクリプトで `testdata/roms/` へ配置し、`.gitignore` に入れる。ROM が無い環境ではテストを `t.Skip` する。

理由: 配布条件が明示されていない ROM をリポジトリに取り込まない。加えて、リポジトリのサイズを小さく保つ。

### 2.2 静的検査を Go のテストとして書く

`internal/arch` パッケージに通常の Go テストとして置く。外部のリンタ設定に依存させない。インポートグラフは `go list -json ./...` の出力から得て、構文の検査は `go/parser` と `go/ast` で行う。

理由: `go test ./...` だけで検査が走る。開発者が別のツールを入れる必要がなく、CI の設定も単純になる。標準ライブラリと Go の付属コマンドだけを使うことで、静的検査のために外部モジュールを取得する必要がなくなる。

### 2.3 `state` パッケージを最初に作る

セクション付きバイナリの読み書きを、どのコンポーネントよりも先に実装する。

理由: 設計書 08 編 §8.2 の形式を先に確定させると、各コンポーネントの `SaveState` が同じ書き方になる。往復テストのヘルパもここで用意できる。

### 2.4 `region` パッケージを最初に作る

`341`・`262`・`3` のような定数をコードに直接書かせない。

理由: 後から `Region` を導入すると、PPU と APU の全体に散った定数を探すことになる。フェーズ 3 以降でこれらの値を使い始める前に用意する。

## 3. タスク

### 3.1 リポジトリの初期化

- [x] `go mod init github.com/takaakimizuno/shogun-emulator` を実行する
- [x] Go のバージョンを `go 1.25` に設定する
- [x] `.gitignore` を作成し、`testdata/roms/`、`*.pdf`、ビルド成果物（`shogun`、`shogun.exe`、`dist/`）、`node_modules/` を登録する
- [x] `.gitattributes` を作成し、`*.nes binary`、`*.log text eol=lf` を登録する
- [x] LICENSE を配置する（ライセンスの選定は人に確認する）
- [x] 設計書 13 編 §13.10 のディレクトリ構成に従って空ディレクトリと `doc.go` を作る

### 3.2 ディレクトリと最小のパッケージ

- [x] `cmd/shogun/main.go` を作成し、`--version` を表示して終了する処理だけを書く
- [x] `internal/nes/doc.go`、`internal/emu/doc.go`、`internal/debug/doc.go`、`internal/ui/doc.go`、`internal/config/doc.go`、`internal/audio/doc.go`、`internal/video/doc.go` にパッケージコメントを書く
- [x] `go build ./...` が通ることを確認する

### 3.3 バージョン情報の埋め込み

- [x] `cmd/shogun/version.go` に `version`・`commit`・`buildDate` の変数を定義する
- [x] `--version` でこれらと Go のバージョンを表示する
- [x] `Makefile` または `tools/build.sh` に設計書 13 編 §13.2 の `-ldflags` を書く
- [x] `debug.ReadBuildInfo` から VCS 情報を取得して併記する

### 3.4 `region` パッケージ

- [x] `internal/nes/region/region.go` に設計書 02 編 §2.2 の `Region` 構造体を定義する
- [x] `NTSC` の各フィールドの値を `docs/research/07_timing_and_synchronization.md` の 2 節の表から転記する
- [x] `PAL` の各フィールドの値を同じ表から転記する
- [x] `Dendy` の各フィールドの値を同じ表から転記する
- [x] `NoisePeriods` を `docs/research/04_apu.md` の 4.6 節の表から転記する（NTSC と PAL）
- [x] `DMCRates` を同 4.7 節の表から転記する（NTSC と PAL）
- [x] `FrameCounterSteps` を同 3.1・3.2 節の表から転記する
- [x] `ByName(string) (*Region, error)` を実装する（`ntsc`、`pal`、`dendy`）
- [x] 各リージョンの `PPUDotsNum` / `PPUDotsDen` が既約分数であることを検証するテストを書く
- [x] 1 フレームあたりの CPU サイクル数を `Region` から計算し、調査結果の値（NTSC 29780.5、PAL 33247.5、Dendy 35464）と一致することを検証するテストを書く

### 3.5 `state` パッケージ（直列化の基盤）

- [x] `internal/nes/state/writer.go` に `Writer` を実装する
- [x] `Writer.Section(name string) func()` を実装する。セクション名と長さを記録し、戻り値の関数で閉じる
- [x] `Writer` の `U8`・`U16`・`U32`・`U64`・`Bool`・`Bytes`・`String` を実装する。バイト順はリトルエンディアンとする
- [x] `internal/nes/state/reader.go` に `Reader` を実装する
- [x] `Reader.Section(name string) (end func(), ok bool)` を実装する。名前が一致しないセクションは長さ分読み飛ばす
- [x] `Reader` の各読み出しメソッドを実装する。エラーは内部に蓄積し `Err()` で返す
- [x] `Snapshotter` インタフェースを定義する
- [x] セクションの入れ子が正しく閉じることを検証するテストを書く
- [x] 未知のセクションを読み飛ばせることを検証するテストを書く
- [x] 途中で切れたデータを読んだとき `Err()` がエラーを返すことを検証するテストを書く
- [x] `internal/nes/state/diff.go` に `FirstDiff(a, b []byte) string` を実装する。往復テストの失敗時に、どのセクションが異なるかを階層付きで示す
- [x] `SectionNames(b []byte) []string` を実装する。ステートに含まれるセクション名を階層付きで列挙する

### 3.6 決定論のための乱数

- [x] `internal/nes/state/rng.go` にシード付きの生成器を実装する（SplitMix64 を自前で持つ。標準ライブラリの生成器の内部実装が変わっても生成列が変わらないようにするため）
- [x] `FillPattern(dst []uint8, pattern Pattern, seed uint64, salt uint64)` を実装する。`Zero`・`FF`・`Pattern`・`Random` に対応する
- [x] 埋める対象を区別する salt を定義する（RAM・OAM・パレット・CIRAM・CHR-RAM・PRG-RAM）
- [x] salt が違えば同じシードでも内容が変わることを検証するテストを書く
- [x] `ParsePattern` と `Pattern.String` が設定ファイルの値と対応することを検証するテストを書く
- [x] `Pattern` は `$00` と `$FF` を 8 バイトごとに繰り返す並びとする
- [x] 同じシードから常に同じ内容が得られることを検証するテストを書く
- [x] `math/rand` のグローバル関数を使っていないことを確認する

### 3.7 静的検査

- [x] `internal/arch/arch_test.go` を作成する
- [x] `loadModule(t)` を実装する。`go list -json ./...` を 1 回実行してモジュール内の全パッケージのインポートを得る
- [x] `assertNoImport(t, pattern string, forbidden []string)` を実装する。モジュール内のパッケージを推移的にたどる
- [x] `assertNoSymbol(t, pattern string, forbidden []string)` を実装する。`go/parser` で構文木を作り、セレクタ式を検査する
- [x] `TestCoreHasNoUIDependency` を実装する。`internal/nes/...`・`internal/emu/...`・`internal/debug/...` が `fyne.io/` と `github.com/ebitengine/oto` を参照しないことを検証する
- [x] `TestCoreImportsOnlyStdlibAndModule` を実装する。`internal/nes/...` が標準ライブラリとモジュール内のパッケージ以外を参照しないことを検証する
- [x] `TestNoTimeInEmulationCore` を実装する。`internal/nes/...` が `time` を参照しないことを検証する
- [x] `TestNoGlobalRandInEmulationCore` を実装する。`internal/nes/...` が `math/rand` のグローバル関数を呼ばないことを検証する
- [x] `TestNoGoroutinesInEmulationCore` を実装する。`internal/nes/...` が `sync` を参照せず、`go` 文と `select` 文を持たないことを検証する
- [x] `TestDispatchIsTheOnlyFyneDoCaller` を実装する。`internal/ui` 内で `fyne.Do` と `fyne.DoAndWait` を呼ぶファイルが `dispatch.go` のみであることを検証する
- [x] `TestDependencyDirection` を実装する。設計書 01 編 §1.4 の依存グラフに反する参照がないことを検証する
- [x] 検査が違反を検出できることを、合成したパッケージグラフと合成したソースを入力にして検証する（`internal/arch/detect_test.go`）
- [x] 存在しないパッケージを対象にしたとき、検査が失敗せずに飛ばされることを確認する

### 3.8 テスト ROM の取得

- [x] `tools/fetch-test-roms/main.go` を作成する
- [x] `christopherpow/nes-test-roms` の必要なディレクトリを取得して `testdata/roms/` へ配置する処理を実装する（コミットを固定した zip から必要なパスだけ取り出す）
- [x] `nestest.nes` と `nestest.log` を取得する処理を実装する
- [x] Holy Mapperel を取得する処理を実装する（7z 形式のため、`7z`・`7zz`・`7za` のいずれかで展開する。見つからないときは書庫を置いて手順を表示する）
- [x] `allpads` を取得する処理を実装する（リリースの資産を直接取得する）
- [x] 取得済みのファイルを再取得しない（存在すれば飛ばす）
- [x] 取得した各ファイルの SHA-256 を `testdata/golden/romhashes.json` と照合する。不一致なら失敗する
- [x] `-update-hashes` でハッシュ表を作り直せるようにする
- [x] `-verify` で取得せずに照合だけ行えるようにする
- [x] 固定できる配布元が無い ROM（`test_apu_env` ほか）を設計書 12 編 §12.5.2 の表に記録する
- [x] `go run ./tools/fetch-test-roms` の使い方を `tools/fetch-test-roms/doc.go` に書く

### 3.9 テスト ROM ランナーの骨組み

- [x] `internal/testrom/runner.go` に設計書 12 編 §12.5 の `RunBlargg` を実装する
- [x] `hasSignature` を実装する（`$6001`–`$6003` が `$DE $B0 $61`）
- [x] `readMessage` を実装する（`$6004` からゼロ終端まで）
- [x] `$6000` が `$81` のときリセットする処理を実装する
- [x] タイムアウトを実装する
- [x] `internal/testrom/expect.go` に `testdata/golden/testroms.json` を読み込む処理を実装する
- [x] ROM が存在しないとき `t.Skip` するヘルパ `requireROM(t, path)` を実装する
- [x] `internal/testrom/roundtrip.go` に設計書 12 編 §12.4 の往復テストのヘルパを実装する（この時点では `NES` が無いのでインタフェースだけ定義する）

### 3.10 CI

- [x] `.github/workflows/ci.yml` を作成する
- [x] `ubuntu-latest`・`macos-latest`・`windows-latest` のマトリクスを設定する
- [x] Go 1.25 をセットアップする
- [x] Linux で Fyne のビルドに必要なパッケージを導入する手順を入れる
- [x] テスト ROM を取得するステップを入れ、結果をキャッシュする
- [x] `go vet ./...` を実行する
- [x] `gofmt -l .` の出力が空であることを確認する
- [x] `go test ./...` を実行する
- [x] `go build ./...` を実行する
- [x] キャッシュ（モジュールとビルド）を設定する

### 3.11 スパイクコードの整理

- [x] `tools/spikes` が本体のモジュールに含まれないことを確認する（独立した `go.mod` を持つ）
- [x] CI で `tools/spikes` をビルドしない（本体のテストを遅くしないため）
- [x] `tools/spikes/README.md` の実行手順が現在のコードと一致することを確認する

### 3.12 フェーズの締め

- [x] `gofmt -l .`・`go vet ./...`・`go build ./...`・`go test ./...` が手元で通ることを確認する
- [x] `go run ./tools/fetch-test-roms` が動作し、ROM が配置されることを確認する
- [x] 2 回目の実行で取得を飛ばし、`-verify` で照合だけ行えることを確認する
- [x] `tools/build.sh` が release・debug・Windows 向けクロスビルドで動作することを確認する
- [ ] `go test ./...` が 3 OS で通ることを CI で確認する（リポジトリを GitHub へ置いた後）
- [x] `docs/plans/README.md` のフェーズ 0 の状態を更新する

## 4. 完了判定

| 判定項目 | 確認方法 |
|---|---|
| CI が 3 OS で成功する | GitHub Actions の結果 |
| 静的検査が動作する | 意図的に違反を入れたとき失敗すること |
| テスト ROM が取得できる | `testdata/roms/` にファイルが配置されること |
| `state` パッケージが往復できる | セクションの入れ子と読み飛ばしのテストが通ること |
| `region` の値が調査結果と一致する | フレームあたり CPU サイクル数の検証テストが通ること |

## 5. このフェーズで扱わないもの

| 対象 | 扱うフェーズ |
|---|---|
| CPU・PPU・APU の実装 | フェーズ 1 以降 |
| GUI | フェーズ 5 |
| アイコンとパッケージング | フェーズ 13 |
| 設定ファイルの読み書き | フェーズ 12。それまでは既定値を構造体リテラルで持つ |

## 6. 人に確認する点

- [x] ライセンスの選定（MIT）
- [x] Go モジュールパス（`github.com/takaakimizuno/shogun-emulator`）
- [x] リポジトリの公開・非公開（公開の方向）
