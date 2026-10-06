# 開発計画（docs/plans）

project.md の作業順序「3. 開発計画とタスクを書く」に対応する。開発の段階ごとに 1 ファイルとし、各ファイルに「背景」「方針とその理由」「具体的なタスク」を置く。すべてのタスクにチェックボックスを付け、進捗が分かる形にしている。

作成日: 2026-09-21

第 2 弾: Agent Interface（2026-10-04 追加）。AI エージェントやプログラムからエミュレータを操作・観測・デバッグする窓口をフェーズ 14〜20 で作る。設計は `docs/specifications/14-agent-interface.md`、用語は `GLOSSARY.md`、構成の判断は `docs/adr/` にある。

## 全体ロードマップ

```
Phase 0  基盤整備 ──┐
                    ├─ Phase 1  バス・ROM・マッパー0・CPU（公式命令）
                    │      └─ Phase 2  CPU（非公式命令・タイミング・割り込み）
                    │              └─ Phase 3  PPU（フレームタイミングと背景）
                    │                      └─ Phase 4  PPU（スプライトとスクロール）
                    │                              └─ Phase 5  映像出力・GUI最小・入力
                    │                                      ├─ Phase 6  APU とオーディオ
                    │                                      │      └─ Phase 7  DMA の精密化
                    │                                      └─ Phase 8  マッパー拡充
                    │                                              └─ Phase 9  ステート・巻き戻し・ムービー
                    │                                                      ├─ Phase 10 デバッガ（CPU 系）
                    │                                                      │      └─ Phase 11 デバッガ（PPU 系）
                    │                                                      └─ Phase 12 設定・CLI・GUI 完成
                    └───────────────────────────────────────────────────────────── Phase 13 配布
```

第 2 弾（Agent Interface）は Phase 12 の完了後に始められる。Phase 13 の残項目とは独立に進める。

```
Phase 12 ── Phase 14  Agent Interface の基盤（JSON-RPC・Instance・Fork・shogun ctl）
                └─ Phase 15  進行と観測
                        ├─ Phase 16  Symbol・Game State・式の拡張
                        │      ├─────────────────┐
                        │      │                 └─ Phase 20  解析（トレース・プロファイル・Diagnostic）
                        └─ Phase 17  MCP ブリッジと GUI の共有
                               │      │
                               └──────┴─ Phase 18  開発ループと再現（16 と 17 の両方が前提）
                                                └─ Phase 19  Scenario とテスト実行
```

## フェーズ一覧

| フェーズ | 内容 | 完了条件（要約） | 状態 |
|---|---|---|---|
| [00](phase-00-foundation.md) | 基盤整備 | CI が回り、テスト ROM を取得でき、静的検査が通る | 完了（CI の 3 OS 実行のみ未確認） |
| [01](phase-01-cpu-official.md) | バス・ROM ローダ・マッパー 0・CPU（公式命令） | **nestest.log と完全一致** | 完了（公式命令の 5003 行が一致。CI の 3 OS 実行のみ未確認） |
| [02](phase-02-cpu-full.md) | CPU（非公式命令・タイミング・割り込み） | nestest.log 全行一致、`instr_test-v5/rom_singles/` が合格 | 完了（8991 行一致 + 16 ROM 合格。CI の 3 OS 実行のみ未確認） |
| [03](phase-03-ppu-background.md) | PPU（フレームタイミングと背景描画） | `ppu_vbl_nmi` が合格し、背景が正しく描画される | 完了（`ppu_vbl_nmi` 10/10 + 表示型 7 本。CI の 3 OS 実行のみ未確認） |
| [04](phase-04-ppu-sprites.md) | PPU（スプライトとスクロール） | `sprite_hit_tests`、`sprite_overflow_tests`、`oam_stress` が合格 | 完了（スプライト関連 20 本すべて合格。CI の 3 OS 実行のみ未確認） |
| [05](phase-05-video-gui-input.md) | 映像出力・GUI 最小構成・入力 | NROM のゲームがキーボードで遊べる（音なし） | 完了（`read_joy3` と `allpads` が合格。10 分間 60.07 fps。ウィンドウ操作の目視確認と CI の 3 OS 実行のみ未確認） |
| [06](phase-06-apu-audio.md) | APU とオーディオ出力 | `apu_test` ほかが合格し、音切れなく音が出る | 完了（APU 関連 27 本が合格。10 分間 60.10 fps・アンダーラン 0。聴感の確認と CI の 3 OS 実行のみ未確認） |
| [07](phase-07-dma.md) | DMA の精密化 | `read_joy3`、`dmc_dma_during_read4` の一部、`dpcmletterbox` が合格 | 完了（`read_joy3` 2 本・`4-irq_and_dma`・`dpcmletterbox` が合格。`sprdma_and_dmc_dma` と `dma_4016_read` は未達。CI の 3 OS 実行のみ未確認） |
| [08](phase-08-mappers.md) | マッパー 2・3・7・66・1・4 | Holy Mapperel と `mmc3_test_2` が合格 | 完了（Holy Mapperel 25 本・`mmc3_test_2` 6 本・`mmc3_irq_tests` 6 本が合格。実ゲームでの確認と CI の 3 OS 実行のみ未確認） |
| [09](phase-09-state-movie.md) | セーブステート・巻き戻し・入力ムービー | 全フレーム往復テストと決定論テストが合格 | 完了（往復テスト 5 種・決定論テスト 4 ムービーが合格。実ゲームでの巻き戻し確認と CI の 3 OS 実行のみ未確認） |
| [10](phase-10-debugger-cpu.md) | デバッガ（逆アセンブル・ブレークポイント・メモリ） | サイクル単位ステップと条件付きブレークが動作する | 完了（ステップ・全種ブレークポイント・メモリビューアの結合テストが合格。デバッガ全機能を有効にしても決定論テスト 4 ムービーのハッシュが一致。GUI の目視確認と CI の 3 OS 実行のみ未確認） |
| [11](phase-11-debugger-ppu.md) | デバッガ（PPU 系ビューアと編集） | 4 ビューアが実時間で同期し、編集が反映される | 完了（4 ビューア・APU ビューア・タイルエディタ・オーバーレイを実装。5 種類の編集がフレーム出力に反映されることと、ビューア 5 つを開いても決定論テスト 4 ムービーのハッシュが一致することを確認。PPU の属性が 8 ピクセルずれる不具合を修正。GUI の目視確認と CI の 3 OS 実行のみ未確認） |
| [12](phase-12-config-cli-gui.md) | 設定・CLI・GUI 完成 | 設定 GUI で全項目を変更でき、CLI の全オプションが動作する | 完了（設定ファイル・キーバインドファイルの読み書き・移行・環境変数・全 CLI オプション・設定画面・連射・ウィンドウ状態・文言の集約を実装しテストが合格。Windows のコンソール接続と GUI の目視確認、CI の 3 OS 実行のみ未確認） |
| [13](phase-13-distribution.md) | 配布 | 3 OS の成果物が CI で生成される | 一部完了（アイコンの各形式・Windows のリソース・`.app` と `.dmg`・`.zip`・`.tar.gz`・AppImage の手順・同梱文書・ライセンス一覧・リリースと決定論の CI を実装。macOS では `.dmg` を作り、Finder と同じ経路で `.nes` を開けることを確認。Windows と Linux の実機確認、CI とリリースの実行、人に確認する 5 点が残る） |
| [14](phase-14-agent-foundation.md) | Agent Interface の基盤（Agent Command の登録簿・JSON-RPC・トークン認証・Instance と Fork・`shogun serve`・`shogun ctl`） | `shogun ctl` から `session.*`・`instance.*`・`control.*` が動作し、不正なトークンを断るテストと依存規則の検査が合格する | 完了（`shogun serve`・`shogun ctl`・JSON-RPC・Host と Fork・Control・進行の完了通知を実装し、全テストが合格。Windows の発見ファイルの ACL と CI の 3 OS 実行のみ未確認） |
| [15](phase-15-agent-exec-observe.md) | 進行と観測（`exec.step`・`exec.run_until`・`exec.input_sequence`・Observation と差分・構造化された観測・書き込みと Freeze） | `exec.*` が Stop Reason と Observation を返し、同じ列の再実行と Fork 同士で Observation（画像を含む）が一致する | 完了（`exec.*`・`obs.*`・`mem.*`・`cpu.*`・`debug.bp/watch.*`・`state.*`・`rom.load`・Freeze・PPU 書き込みの記録を実装。同じ列の 2 回の実行と Fork 同士で Observation（画像を含む）が一致。RMW 命令の中の Freeze の観測と CI の 3 OS 実行のみ未確認） |
| [16](phase-16-agent-symbols-gamestate.md) | Symbol・Game State・式の拡張（`.dbg` の読み込み・バンクを区別する Symbol・Game State Definition・位置の指定） | `.dbg` からバンクを区別した Symbol とソース行を得て、シンボルファイルを v2 へ移行し、Game State の差分が Observation に出る | 完了（バンクを区別する Symbol・シンボルファイル v2 と移行・`.dbg` の読み込みとソース行・式の拡張・位置の指定・Game State Definition と `symbol.*`・`gamestate.*` を実装。テスト用 ca65 プロジェクト `testdata/agent/` を追加。CI の 3 OS 実行のみ未確認） |
| [17](phase-17-agent-mcp-gui.md) | MCP ブリッジと GUI の共有（`shogun mcp`・Control とバナー・イベント） | 同じ要求を JSON-RPC・MCP・`shogun ctl` で送って同じ結果を得て、GUI で Control の取得・バナー・取り返しと `control_changed` が動作する | 完了（`shogun mcp`（MCP SDK v1.8.0）・`--attach`・イベント・`exec.run`/`pause`・`control_lost`・GUI の「AI」メニューとバナーと取り返し・`ctl events watch`・利用者向けの「AI から使う」を実装。3 つの Transport の往復テストと実際のバイナリでの MCP の確認が合格。GUI の目視の確認と CI の 3 OS 実行のみ未確認） |
| [18](phase-18-agent-devloop.md) | 開発ループと再現（常時記録・SHGM v2 の介入レコード・`rom.reload`・ROM の監視・Re-Reach・Repro） | Repro の再生で最後のフレームの状態ハッシュが一致し、同じ ROM での Re-Reach が読み直す前と同じ状態ハッシュになる | 完了（常時記録（SHGM v2 の介入）・`record.status`・`rom.reload` と Re-Reach（frame・condition）・`rom.watch` と GUI の `agent.romWatchAction`・`repro.export` を実装。Repro の再生と Re-Reach で状態ハッシュが一致。GUI の目視の確認と CI の 3 OS 実行のみ未確認） |
| [19](phase-19-agent-scenario.md) | Scenario とテスト実行（`shogun run`・アサーション・JUnit XML・`scenario.export`） | 合格で終了コード 0、失敗で終了コード 5 と JUnit XML と Repro を出し、`scenario.export` の Scenario を再実行して同じ結果になる | 完了 |
| [20](phase-20-agent-analysis.md) | 解析（トレースの絞り込みと要約・プロファイル・Diagnostic） | Diagnostic 11 項目がテスト ROM で検知され、全機能を有効にしても決定論テストのハッシュが変わらず 60 fps を維持する | 完了 |

## 進捗の更新方法

タスクを完了したらチェックボックスを `[x]` にする。フェーズの全タスクが完了し、かつ完了条件を満たしたら、上の表の状態を「完了」にする。

完了条件を満たさないままフェーズを進めない。完了条件はテスト ROM の合否など機械的に判定できる形にしてある。

## 各フェーズ共通の規則

| 規則 | 理由 |
|---|---|
| コンポーネントを実装したら、同じフェーズ内で `SaveState` / `LoadState` と往復テストを書く | 後からまとめて書くと必ず漏れる（設計書 12 編 §12.4） |
| 新しい状態フィールドを追加したら、設計書の該当編の「保存する状態」の表にも追加する | 表とコードの対応を保つ |
| フェーズの完了時に `go test ./...` と静的検査を通す | 依存方向とスレッド境界の破れを早期に検出する |
| 設計書と異なる実装にする場合、先に設計書を直す | 設計書がコードと食い違った時点で読まれなくなる |
| 判断に迷う点は推測で進めず、人に確認する | |

## 参照

| 文書 | 内容 |
|---|---|
| `docs/research/` | ハードウェアの調査結果と技術選定の根拠 |
| `docs/specifications/` | 詳細設計。実装はこれに従う |
| `docs/specifications/12-testing.md` | 各フェーズの完了条件の元になるテスト設計 |
| `docs/specifications/14-agent-interface.md` | Agent Interface の設計（フェーズ 14〜20） |
| `GLOSSARY.md` | Agent Interface の用語集 |
| `docs/adr/` | 構成の判断の記録（ADR） |
