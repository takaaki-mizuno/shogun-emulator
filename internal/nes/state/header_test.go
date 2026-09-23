package state

import (
	"strings"
	"testing"
)

// sampleHeader は照合の基準にするヘッダを返す。
func sampleHeader() Header {
	h := Header{
		FormatVersion: FormatVersion,
		Version:       "1.2.3",
		Commit:        "abcdef0",
		Mapper:        4,
		Submapper:     1,
		Region:        "NTSC",
		Init: Init{
			RAMPattern:      PatternRandom,
			RAMSeed:         0x0123456789ABCDEF,
			CPUPPUAlignment: 2,
			DMAGetPutPhase:  1,
			PPUVBlankFlag:   true,
		},
		Cycles: 123456,
		Frames: 789,
	}
	for i := range h.ROMHash {
		h.ROMHash[i] = uint8(i)
	}
	return h
}

// encodeHeader はヘッダをバイト列にする。
func encodeHeader(h Header) []uint8 {
	w := NewWriter()
	h.Write(w)
	return w.Data()
}

// TestHeaderRoundtrip はヘッダの全項目が往復することを確かめる。
func TestHeaderRoundtrip(t *testing.T) {
	want := sampleHeader()
	want.Screenshot = []uint8{0x89, 'P', 'N', 'G'}

	got, err := ReadHeader(encodeHeader(want))
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if got.Version != want.Version || got.Commit != want.Commit {
		t.Errorf("バージョン = %q/%q, 期待 %q/%q", got.Version, got.Commit, want.Version, want.Commit)
	}
	if got.ROMHash != want.ROMHash {
		t.Errorf("ROM ハッシュが往復しない")
	}
	if got.Mapper != want.Mapper || got.Submapper != want.Submapper {
		t.Errorf("マッパー = %d.%d, 期待 %d.%d", got.Mapper, got.Submapper, want.Mapper, want.Submapper)
	}
	if got.Region != want.Region {
		t.Errorf("リージョン = %q, 期待 %q", got.Region, want.Region)
	}
	if got.Init != want.Init {
		t.Errorf("初期状態 = %+v, 期待 %+v", got.Init, want.Init)
	}
	if got.Cycles != want.Cycles || got.Frames != want.Frames {
		t.Errorf("累積 = %d サイクル %d フレーム, 期待 %d/%d",
			got.Cycles, got.Frames, want.Cycles, want.Frames)
	}
	if string(got.Screenshot) != string(want.Screenshot) {
		t.Errorf("スクリーンショットが往復しない")
	}
}

// TestHeaderRejectsBadMagic はマジックが違うデータを断ることを確かめる。
func TestHeaderRejectsBadMagic(t *testing.T) {
	b := encodeHeader(sampleHeader())
	// セクション名の直後にマジックが並ぶ。先頭のバイトを壊す。
	i := strings.Index(string(b), Magic)
	if i < 0 {
		t.Fatal("マジックが見つからない")
	}
	b[i] = 'X'

	_, err := ReadHeader(b)
	if err == nil {
		t.Fatal("マジックが違うデータを受け入れた")
	}
	if !strings.Contains(err.Error(), "マジック") {
		t.Errorf("理由がマジックでない: %v", err)
	}
}

// TestHeaderRejectsBadFormatVersion は形式のバージョンが違うデータを
// 断ることを確かめる。
func TestHeaderRejectsBadFormatVersion(t *testing.T) {
	h := sampleHeader()
	h.FormatVersion = FormatVersion + 1
	_, err := ReadHeader(encodeHeader(h))
	if err == nil {
		t.Fatal("形式のバージョンが違うデータを受け入れた")
	}
	if !strings.Contains(err.Error(), "形式のバージョン") {
		t.Errorf("理由が形式のバージョンでない: %v", err)
	}
}

// TestHeaderVerifyMismatch は照合する各項目の不一致を検出することを
// 確かめる。
//
// エラーには「何が」「期待値」「実際の値」が入る。利用者が別の ROM の
// ステートを読み込んだことに気づけるようにするためである。
func TestHeaderVerifyMismatch(t *testing.T) {
	for _, tt := range []struct {
		name   string
		modify func(*Header)
		want   string
	}{
		{"バージョン", func(h *Header) { h.Version = "9.9.9" }, "バージョン"},
		{"コミット", func(h *Header) { h.Commit = "0000000" }, "コミット"},
		{"ROM ハッシュ", func(h *Header) { h.ROMHash[0] ^= 0xFF }, "ROM ハッシュ"},
		{"マッパー", func(h *Header) { h.Mapper = 1 }, "マッパー"},
		{"サブマッパー", func(h *Header) { h.Submapper = 9 }, "マッパー"},
		{"リージョン", func(h *Header) { h.Region = "PAL" }, "リージョン"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := sampleHeader()
			got := sampleHeader()
			tt.modify(&got)

			err := got.Verify(&want)
			if err == nil {
				t.Fatalf("%s の不一致を見逃した", tt.name)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラーに %q が含まれない: %v", tt.want, err)
			}
		})
	}
}

// TestHeaderVerifyAccepts は一致するヘッダを受け入れることを確かめる。
//
// 初期状態と累積サイクル数は照合しない。ステートごとに異なる値である。
func TestHeaderVerifyAccepts(t *testing.T) {
	want := sampleHeader()
	got := sampleHeader()
	got.Init.RAMSeed = 42
	got.Cycles = 1
	got.Frames = 2
	if err := got.Verify(&want); err != nil {
		t.Errorf("一致するヘッダを断った: %v", err)
	}
}
