package main

// nesTestROMsCommit は christopherpow/nes-test-roms の取得するコミット。
//
// ブランチ名ではなくコミットで固定する。上流に変更が入っても取得する内容が
// 変わらず、ハッシュの照合が意味を持つようにするためである。
const nesTestROMsCommit = "95d8f621ae55cee0d09b91519a8989ae0e64753b"

// zipSource は zip 書庫から一部のファイルを取り出す取得元。
type zipSource struct {
	name string
	url  string
	// include は取り出すパスの接頭辞。書庫の最上位ディレクトリを除いた
	// 相対パスに対して判定する。
	include []string
	// exts は取り出す拡張子。空のときは拡張子で絞らない。
	exts []string
	// extra は exts に含まれなくても取り出すパス。
	extra []string
}

// fileSource は単一のファイルを取得する取得元。
type fileSource struct {
	name string
	url  string
	dest string
}

// archiveSource は外部コマンドで展開する書庫の取得元。
type archiveSource struct {
	name string
	url  string
	// dest は書庫を置く先。展開もこのディレクトリへ行う。
	dest string
	file string
}

// nesTestROMs は blargg らのテスト ROM 群。
//
// 取り出すディレクトリは設計書 12 編 §12.6 の完了条件に対応させる。
// 書庫全体を展開しないのは、testdata/roms/ に使わないファイルを置かない
// ためである。
var nesTestROMs = zipSource{
	name: "nes-test-roms",
	url:  "https://codeload.github.com/christopherpow/nes-test-roms/zip/" + nesTestROMsCommit,
	include: []string{
		// CPU
		"instr_test-v5/",
		"instr_misc/",
		"instr_timing/",
		"cpu_reset/",
		"cpu_timing_test6/",
		"branch_timing_tests/",
		"cpu_dummy_reads/",
		"cpu_dummy_writes/",
		"cpu_exec_space/",
		"cpu_interrupts_v2/",
		// PPU
		"ppu_vbl_nmi/",
		"vbl_nmi_timing/",
		"nmi_sync/",
		"blargg_ppu_tests_2005.09.15b/",
		"ppu_open_bus/",
		"ppu_read_buffer/",
		"oam_read/",
		"oam_stress/",
		"full_palette/",
		"scanline/",
		"sprite_hit_tests_2005.10.05/",
		"sprite_overflow_tests/",
		"sprdma_and_dmc_dma/",
		// APU
		"apu_test/",
		"apu_reset/",
		"apu_mixer/",
		"blargg_apu_2005.07.30/",
		"dmc_tests/",
		"dmc_dma_during_read4/",
		"dpcmletterbox/",
		"volume_tests/",
		"pal_apu_tests/",
		// マッパー
		"MMC1_A12/",
		"mmc3_test_2/",
		"mmc3_irq_tests/",
		// 入力
		"read_joy3/",
		// 総合
		"240pee/",
		"stress/",
		// nestest とその正解ログ
		"other/nestest.nes",
		"other/nestest.log",
	},
	exts: []string{".nes"},
	extra: []string{
		"other/nestest.log",
	},
}

// allpads は複数のコントローラを読むテスト ROM。
var allpads = []fileSource{
	{
		name: "allpads",
		url:  "https://github.com/pinobatch/allpads-nes/releases/download/v0.09/allpads.nes",
		dest: "allpads/allpads.nes",
	},
	{
		name: "allpads218",
		url:  "https://github.com/pinobatch/allpads-nes/releases/download/v0.09/allpads218.nes",
		dest: "allpads/allpads218.nes",
	},
}

// holyMapperel はマッパーの実装を網羅的に確かめる ROM 群。
var holyMapperel = archiveSource{
	name: "holy-mapperel",
	url:  "https://github.com/pinobatch/holy-mapperel/releases/download/v0.02/holy-mapperel-bin-0.02.7z",
	dest: "holy-mapperel",
	file: "holy-mapperel-bin-0.02.7z",
}
