// Command fetch-test-roms はテスト ROM を取得して testdata/roms/ へ配置する。
//
// # 使い方
//
//	go run ./tools/fetch-test-roms              取得する（既にあるものは飛ばす）
//	go run ./tools/fetch-test-roms -force       既にあるものも取り直す
//	go run ./tools/fetch-test-roms -verify      取得せずにハッシュだけ照合する
//	go run ./tools/fetch-test-roms -update-hashes
//	                                            取得した内容でハッシュ表を作り直す
//	go run ./tools/fetch-test-roms -dir DIR     配置先を変える（既定 testdata/roms）
//
// # ROM をリポジトリに含めない理由
//
// 配布条件が明示されていない ROM を取り込まない。あわせてリポジトリを小さく
// 保つ。testdata/roms/ は .gitignore の対象で、ROM が無い環境ではテストを
// 飛ばす。
//
// # 取得元
//
// 取得元はコミットまたはタグで固定する。上流が変わっても取得する内容が
// 変わらないようにするためである。
//
//	christopherpow/nes-test-roms   コミットを固定して zip を取得する
//	pinobatch/allpads-nes          リリースの .nes を直接取得する
//	pinobatch/holy-mapperel        リリースの .7z を取得する
//
// holy-mapperel は 7z 形式でのみ配布されている。展開には 7z・7zz・7za の
// いずれかのコマンドを使う。見つからないときは書庫をそのまま置き、展開の
// 指示を表示して続行する。7z の展開のために外部モジュールを足さない。
//
// # 取得できない ROM
//
// 次の ROM は固定できる配布元が無い。手で testdata/roms/ へ置く。
// 置かれていないあいだ、対応するテストは飛ばされる。
//
//	test_apu_env、test_apu_sweep、test_apu_timers、test_tri_lin_ctr
//	apu_phase_reset
//	dma_sync_test_v2
//	serom
//
// # ハッシュの照合
//
// 取得した各ファイルの SHA-256 を testdata/golden/romhashes.json と照合する。
// 不一致のとき失敗する。取得元が同じ内容を返し続けていることを確かめ、
// テストの期待値が別の ROM に対して評価されることを防ぐ。
package main
