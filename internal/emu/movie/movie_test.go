package movie

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// sampleHeader は往復の対象にするヘッダを返す。
func sampleHeader() Header {
	h := Header{
		Version:   "1.2.3",
		Commit:    "abcdef0",
		ROMName:   "game.nes",
		Region:    "NTSC",
		Mapper:    4,
		Submapper: 1,
		Init: state.Init{
			RAMPattern:      state.PatternRandom,
			RAMSeed:         42,
			CPUPPUAlignment: 1,
			DMAGetPutPhase:  1,
			PPUVBlankFlag:   true,
		},
		Start:            StartPowerOn,
		Ports:            [2]string{"standard", "none"},
		Rerecords:        3,
		Author:           "水野貴明",
		Comment:          "テスト",
		ChecksumInterval: 60,
	}
	for i := range h.ROMHash {
		h.ROMHash[i] = uint8(i * 3)
	}
	return h
}

// TestRecordRoundtrip はヘッダとレコードが往復することを確かめる。
func TestRecordRoundtrip(t *testing.T) {
	r := NewRecorder(sampleHeader())
	r.MarkReset(false)
	r.BeginFrame([2]uint8{0x01, 0x02}, [8]uint8{1, 2, 3, 4, 5, 6, 7, 8})
	r.BeginFrame([2]uint8{0x80, 0x00}, [8]uint8{})
	r.MarkReset(true)
	r.BeginFrame([2]uint8{0xFF, 0xAA}, [8]uint8{})

	got, err := Decode(r.Encode())
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if got.Header.Author != "水野貴明" || got.Header.Comment != "テスト" {
		t.Errorf("作者とコメントが往復しない（%q / %q）", got.Header.Author, got.Header.Comment)
	}
	if got.Header.Init != sampleHeader().Init {
		t.Errorf("初期状態 = %+v", got.Header.Init)
	}
	if got.Header.TotalFrames != 3 {
		t.Errorf("総フレーム数 = %d, 期待 3", got.Header.TotalFrames)
	}
	if got.Header.ChecksumInterval != 60 {
		t.Errorf("チェックサム間隔 = %d, 期待 60", got.Header.ChecksumInterval)
	}
	if len(got.Records) != len(r.Movie().Records) {
		t.Fatalf("レコード数 = %d, 期待 %d", len(got.Records), len(r.Movie().Records))
	}
	for i := range got.Records {
		if got.Records[i] != r.Movie().Records[i] {
			t.Errorf("レコード %d = %+v, 期待 %+v", i, got.Records[i], r.Movie().Records[i])
		}
	}
}

// TestPlayerReplaysRecording は記録した内容がそのまま再生されることを
// 確かめる。
func TestPlayerReplaysRecording(t *testing.T) {
	h := sampleHeader()
	h.ChecksumInterval = 2
	r := NewRecorder(h)
	hashes := [][8]uint8{{9}, {8}, {7}, {6}}
	inputs := [][2]uint8{{1, 0}, {2, 0}, {3, 0}, {4, 0}}
	r.MarkReset(false)
	for i := range inputs {
		r.BeginFrame(inputs[i], hashes[i])
	}

	m, err := Decode(r.Encode())
	if err != nil {
		t.Fatal(err)
	}
	p := NewPlayer(m)
	if p.TotalFrames() != 4 {
		t.Errorf("総フレーム数 = %d, 期待 4", p.TotalFrames())
	}
	for i := range inputs {
		f, ok := p.BeginFrame()
		if !ok {
			t.Fatalf("フレーム %d が再生できない", i)
		}
		if f.Buttons != inputs[i] {
			t.Errorf("フレーム %d の入力 = %v, 期待 %v", i, f.Buttons, inputs[i])
		}
		if i == 0 && !f.Reset {
			t.Error("フレーム 0 のリセットが再生されない")
		}
		// 間隔 2 なのでフレーム 0 と 2 にチェックサムが付く。
		wantChecksum := i%2 == 0
		if (f.Checksum != nil) != wantChecksum {
			t.Errorf("フレーム %d のチェックサムの有無 = %v, 期待 %v", i, f.Checksum != nil, wantChecksum)
		}
		if wantChecksum && *f.Checksum != hashes[i] {
			t.Errorf("フレーム %d のチェックサム = %v, 期待 %v", i, *f.Checksum, hashes[i])
		}
	}
	if _, ok := p.BeginFrame(); ok {
		t.Error("終端を越えて再生できてしまう")
	}
	if !p.Done() {
		t.Error("Done が false のままである")
	}
}

// TestTruncateToDiscardsLaterFrames は巻き戻しの後に以降のレコードを
// 捨て、再記録回数が増えることを確かめる。
func TestTruncateToDiscardsLaterFrames(t *testing.T) {
	r := NewRecorder(sampleHeader())
	for i := range 5 {
		r.BeginFrame([2]uint8{uint8(i), 0}, [8]uint8{})
	}
	before := r.Rerecords()

	r.TruncateTo(2)
	if r.Frames() != 2 {
		t.Errorf("フレーム数 = %d, 期待 2", r.Frames())
	}
	if r.Rerecords() != before+1 {
		t.Errorf("再記録回数 = %d, 期待 %d", r.Rerecords(), before+1)
	}

	r.BeginFrame([2]uint8{0x77, 0}, [8]uint8{})
	p := NewPlayer(r.Movie())
	var last [2]uint8
	for {
		f, ok := p.BeginFrame()
		if !ok {
			break
		}
		last = f.Buttons
	}
	if last != [2]uint8{0x77, 0} {
		t.Errorf("最後の入力 = %v, 期待 {119 0}", last)
	}
	if r.Frames() != 3 {
		t.Errorf("録り直した後のフレーム数 = %d, 期待 3", r.Frames())
	}
}

// TestVerifyHeader は照合の条件を確かめる。
func TestVerifyHeader(t *testing.T) {
	want := sampleHeader()

	t.Run("一致", func(t *testing.T) {
		p := NewPlayer(&Movie{Header: sampleHeader()})
		w, err := p.VerifyHeader(&want)
		if err != nil || w != "" {
			t.Errorf("警告 %q、エラー %v", w, err)
		}
	})

	for _, tt := range []struct {
		name   string
		modify func(*Header)
	}{
		{"ROM ハッシュ", func(h *Header) { h.ROMHash[0] ^= 0xFF }},
		{"マッパー", func(h *Header) { h.Mapper = 0 }},
		{"リージョン", func(h *Header) { h.Region = "PAL" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := sampleHeader()
			tt.modify(&h)
			p := NewPlayer(&Movie{Header: h})
			if _, err := p.VerifyHeader(&want); err == nil {
				t.Errorf("%s の不一致を見逃した", tt.name)
			}
		})
	}

	t.Run("バージョン違いは警告", func(t *testing.T) {
		h := sampleHeader()
		h.Version = "9.9.9"
		p := NewPlayer(&Movie{Header: h})
		w, err := p.VerifyHeader(&want)
		if err != nil {
			t.Errorf("エラーになった: %v", err)
		}
		if w == "" {
			t.Error("警告が返らない")
		}
	})
}

// TestDecodeRejectsBrokenData は壊れたデータを断ることを確かめる。
func TestDecodeRejectsBrokenData(t *testing.T) {
	if _, err := Decode([]uint8{1, 2, 3}); err == nil {
		t.Error("ムービーでないデータを受け入れた")
	}

	r := NewRecorder(sampleHeader())
	r.BeginFrame([2]uint8{1, 2}, [8]uint8{})
	b := r.Encode()
	// 末尾の入力レコードを 1 バイト削る。
	if _, err := Decode(b[:len(b)-1]); err == nil {
		t.Error("途中で切れたデータを受け入れた")
	}
}
