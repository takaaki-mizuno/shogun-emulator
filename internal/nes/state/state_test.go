package state

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestRoundTripPrimitives は各型の書き込みと読み出しが往復することを確かめる。
func TestRoundTripPrimitives(t *testing.T) {
	w := NewWriter()
	end := w.Section("test")
	w.U8(0x42)
	w.Bool(true)
	w.Bool(false)
	w.U16(0xBEEF)
	w.U32(0xDEADBEEF)
	w.U64(0x0123456789ABCDEF)
	w.I32(-12345)
	w.Int(-1)
	w.Bytes([]uint8{1, 2, 3})
	w.String("将軍")
	raw := []uint8{9, 8, 7, 6}
	w.RawBytes(raw)
	end()

	r := NewReader(w.Data())
	rend := r.RequireSection("test")
	if got := r.U8(); got != 0x42 {
		t.Errorf("U8 = 0x%02X, 期待 0x42", got)
	}
	if got := r.Bool(); got != true {
		t.Errorf("Bool = %v, 期待 true", got)
	}
	if got := r.Bool(); got != false {
		t.Errorf("Bool = %v, 期待 false", got)
	}
	if got := r.U16(); got != 0xBEEF {
		t.Errorf("U16 = 0x%04X, 期待 0xBEEF", got)
	}
	if got := r.U32(); got != 0xDEADBEEF {
		t.Errorf("U32 = 0x%08X, 期待 0xDEADBEEF", got)
	}
	if got := r.U64(); got != 0x0123456789ABCDEF {
		t.Errorf("U64 = 0x%016X", got)
	}
	if got := r.I32(); got != -12345 {
		t.Errorf("I32 = %d, 期待 -12345", got)
	}
	if got := r.Int(); got != -1 {
		t.Errorf("Int = %d, 期待 -1", got)
	}
	if got := r.Bytes(); !bytes.Equal(got, []uint8{1, 2, 3}) {
		t.Errorf("Bytes = %v, 期待 [1 2 3]", got)
	}
	if got := r.String(); got != "将軍" {
		t.Errorf("String = %q, 期待 \"将軍\"", got)
	}
	gotRaw := make([]uint8, 4)
	r.RawBytes(gotRaw)
	if !bytes.Equal(gotRaw, raw) {
		t.Errorf("RawBytes = %v, 期待 %v", gotRaw, raw)
	}
	rend()
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v, 期待 nil", err)
	}
}

// TestNestedSections はセクションの入れ子が正しく閉じることを確かめる。
func TestNestedSections(t *testing.T) {
	w := NewWriter()
	endNES := w.Section("nes")
	endCPU := w.Section("cpu")
	w.U8(1)
	endCPU()
	endPPU := w.Section("ppu")
	w.U8(2)
	endSprites := w.Section("sprites")
	w.U8(3)
	endSprites()
	endPPU()
	endNES()

	r := NewReader(w.Data())
	e1 := r.RequireSection("nes")
	e2 := r.RequireSection("cpu")
	if got := r.U8(); got != 1 {
		t.Errorf("cpu の値 = %d, 期待 1", got)
	}
	e2()
	e3 := r.RequireSection("ppu")
	if got := r.U8(); got != 2 {
		t.Errorf("ppu の値 = %d, 期待 2", got)
	}
	e4 := r.RequireSection("sprites")
	if got := r.U8(); got != 3 {
		t.Errorf("sprites の値 = %d, 期待 3", got)
	}
	e4()
	e3()
	e1()
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
}

// TestSkipUnknownSection は知らないセクションを読み飛ばせることを確かめる。
// 将来バージョンが追加したセクションを古いバージョンが無視できる必要がある。
func TestSkipUnknownSection(t *testing.T) {
	w := NewWriter()
	e1 := w.Section("cpu")
	w.U8(0x11)
	e1()
	e2 := w.Section("future") // 読み出し側が知らないセクション
	w.U64(0xFFFFFFFFFFFFFFFF)
	w.Bytes(make([]uint8, 100))
	e2()
	e3 := w.Section("ppu")
	w.U8(0x22)
	e3()

	r := NewReader(w.Data())
	ec := r.RequireSection("cpu")
	if got := r.U8(); got != 0x11 {
		t.Errorf("cpu = 0x%02X, 期待 0x11", got)
	}
	ec()
	// "future" を飛ばして "ppu" へ到達できること
	ep := r.RequireSection("ppu")
	if got := r.U8(); got != 0x22 {
		t.Errorf("ppu = 0x%02X, 期待 0x22", got)
	}
	ep()
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
}

// TestSectionNotFound は存在しないセクションを要求したときエラーになることを
// 確かめる。
func TestSectionNotFound(t *testing.T) {
	w := NewWriter()
	e := w.Section("cpu")
	w.U8(1)
	e()

	r := NewReader(w.Data())
	if _, ok := r.Section("apu"); ok {
		t.Fatal("存在しないセクションが見つかった")
	}
	r2 := NewReader(w.Data())
	r2.RequireSection("apu")
	if !errors.Is(r2.Err(), ErrSectionMismatch) {
		t.Errorf("Err = %v, 期待 ErrSectionMismatch", r2.Err())
	}
}

// TestUnreadBodyIsSkipped はセクション内を読み残しても、閉じたときに
// 次のセクションへ正しく進むことを確かめる。
func TestUnreadBodyIsSkipped(t *testing.T) {
	w := NewWriter()
	e1 := w.Section("a")
	w.U8(1)
	w.U8(2)
	w.U8(3)
	e1()
	e2 := w.Section("b")
	w.U8(9)
	e2()

	r := NewReader(w.Data())
	ea := r.RequireSection("a")
	if got := r.U8(); got != 1 {
		t.Errorf("a の 1 バイト目 = %d", got)
	}
	ea() // 2 バイト読み残したまま閉じる
	eb := r.RequireSection("b")
	if got := r.U8(); got != 9 {
		t.Errorf("b の値 = %d, 期待 9", got)
	}
	eb()
	if err := r.Err(); err != nil {
		t.Fatalf("Err = %v", err)
	}
}

// TestTruncated は途中で切れたデータを読んだときエラーになることを確かめる。
func TestTruncated(t *testing.T) {
	w := NewWriter()
	e := w.Section("cpu")
	w.U64(1)
	e()
	full := w.Data()

	for n := 1; n < len(full); n++ {
		r := NewReader(full[:n])
		if end, ok := r.Section("cpu"); ok {
			r.U64()
			end()
		}
		if r.Err() == nil {
			// セクションが見つからない場合もエラーにならないことがある。
			// その場合は値が読めていないことを確認する。
			r2 := NewReader(full[:n])
			e2 := r2.RequireSection("cpu")
			r2.U64()
			e2()
			if r2.Err() == nil {
				t.Errorf("%d バイトに切り詰めてもエラーにならない", n)
			}
		}
	}
}

// TestSectionBoundaryIsEnforced はセクションの終端を越えて読めないことを
// 確かめる。セクションの長さが誤っていても隣のセクションを壊さない。
func TestSectionBoundaryIsEnforced(t *testing.T) {
	w := NewWriter()
	e1 := w.Section("a")
	w.U8(1)
	e1()
	e2 := w.Section("b")
	w.U8(2)
	e2()

	r := NewReader(w.Data())
	ea := r.RequireSection("a")
	r.U8()
	// セクション a は 1 バイトしかない。もう 1 バイト読もうとするとエラー。
	r.U8()
	if !errors.Is(r.Err(), ErrTruncated) {
		t.Errorf("Err = %v, 期待 ErrTruncated", r.Err())
	}
	ea()
}

// TestRemaining は未読バイト数が正しいことを確かめる。
func TestRemaining(t *testing.T) {
	w := NewWriter()
	e := w.Section("a")
	w.U8(1)
	w.U8(2)
	w.U8(3)
	e()

	r := NewReader(w.Data())
	ea := r.RequireSection("a")
	if got := r.Remaining(); got != 3 {
		t.Errorf("Remaining = %d, 期待 3", got)
	}
	r.U8()
	if got := r.Remaining(); got != 2 {
		t.Errorf("Remaining = %d, 期待 2", got)
	}
	ea()
}

// TestUnclosedSectionPanics は閉じていないセクションがあるとき Data が
// パニックすることを確かめる。閉じ忘れを開発中に検出する。
func TestUnclosedSectionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("閉じていないセクションでパニックしなかった")
		}
	}()
	w := NewWriter()
	w.Section("a")
	w.U8(1)
	_ = w.Data()
}

// TestDoubleCloseSectionPanics はセクションを二重に閉じたときパニックする
// ことを確かめる。
func TestDoubleCloseSectionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("二重クローズでパニックしなかった")
		}
	}()
	w := NewWriter()
	e := w.Section("a")
	w.U8(1)
	e()
	e()
}

// TestFirstDiffIdentical は同じステートで差分が無いことを確かめる。
func TestFirstDiffIdentical(t *testing.T) {
	build := func(v uint8) []byte {
		w := NewWriter()
		e1 := w.Section("nes")
		e2 := w.Section("cpu")
		w.U8(v)
		e2()
		e1()
		return w.Data()
	}
	if d := FirstDiff(build(1), build(1)); d != "" {
		t.Errorf("同一のステートで差分が出た: %s", d)
	}
}

// TestFirstDiffLocatesSection は差分のあるセクション名を返すことを確かめる。
// 往復テストの失敗時に、どのコンポーネントの状態が漏れているかを示す。
func TestFirstDiffLocatesSection(t *testing.T) {
	build := func(cpu, ppu uint8) []byte {
		w := NewWriter()
		e1 := w.Section("nes")
		e2 := w.Section("cpu")
		w.U8(cpu)
		e2()
		e3 := w.Section("ppu")
		e4 := w.Section("sprites")
		w.U8(ppu)
		e4()
		e3()
		e1()
		return w.Data()
	}

	d := FirstDiff(build(1, 5), build(2, 5))
	if !strings.Contains(d, "nes.cpu") {
		t.Errorf("差分の位置 = %q, \"nes.cpu\" を含むことを期待", d)
	}

	d = FirstDiff(build(1, 5), build(1, 6))
	if !strings.Contains(d, "nes.ppu.sprites") {
		t.Errorf("差分の位置 = %q, \"nes.ppu.sprites\" を含むことを期待", d)
	}
}

// TestSectionNames はセクション名を階層付きで列挙できることを確かめる。
func TestSectionNames(t *testing.T) {
	w := NewWriter()
	e1 := w.Section("nes")
	e2 := w.Section("cpu")
	w.U8(1)
	e2()
	e3 := w.Section("ppu")
	e4 := w.Section("sprites")
	w.U8(2)
	e4()
	e3()
	e1()

	got := SectionNames(w.Data())
	want := []string{"nes", "nes.cpu", "nes.ppu", "nes.ppu.sprites"}
	if len(got) != len(want) {
		t.Fatalf("SectionNames = %v, 期待 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SectionNames[%d] = %q, 期待 %q", i, got[i], want[i])
		}
	}
}

// TestFillPatternDeterministic は同じシードから常に同じ内容が得られることを
// 確かめる。ムービーの再現性の前提になる。
func TestFillPatternDeterministic(t *testing.T) {
	a := make([]uint8, 2048)
	b := make([]uint8, 2048)
	FillPattern(a, PatternRandom, 12345, SaltRAM)
	FillPattern(b, PatternRandom, 12345, SaltRAM)
	if !bytes.Equal(a, b) {
		t.Error("同じシードから異なる内容が得られた")
	}
}

// TestFillPatternSeedMatters は異なるシードで内容が変わることを確かめる。
func TestFillPatternSeedMatters(t *testing.T) {
	a := make([]uint8, 2048)
	b := make([]uint8, 2048)
	FillPattern(a, PatternRandom, 1, SaltRAM)
	FillPattern(b, PatternRandom, 2, SaltRAM)
	if bytes.Equal(a, b) {
		t.Error("異なるシードで同じ内容が得られた")
	}
}

// TestFillPatternSaltMatters は同じシードでも salt が違えば内容が変わることを
// 確かめる。RAM と OAM を同じシードで埋めたときに同一の並びにならないため。
func TestFillPatternSaltMatters(t *testing.T) {
	a := make([]uint8, 256)
	b := make([]uint8, 256)
	FillPattern(a, PatternRandom, 42, SaltRAM)
	FillPattern(b, PatternRandom, 42, SaltOAM)
	if bytes.Equal(a, b) {
		t.Error("salt が違うのに同じ内容が得られた")
	}
}

// TestFillPatternFixed は固定パターンの内容を確かめる。
func TestFillPatternFixed(t *testing.T) {
	buf := make([]uint8, 16)

	FillPattern(buf, PatternZero, 0, 0)
	for i, v := range buf {
		if v != 0x00 {
			t.Errorf("PatternZero: [%d] = 0x%02X, 期待 0x00", i, v)
		}
	}

	FillPattern(buf, PatternFF, 0, 0)
	for i, v := range buf {
		if v != 0xFF {
			t.Errorf("PatternFF: [%d] = 0x%02X, 期待 0xFF", i, v)
		}
	}

	FillPattern(buf, PatternAlternating, 0, 0)
	for i, v := range buf {
		want := uint8(0x00)
		if i&0x08 != 0 {
			want = 0xFF
		}
		if v != want {
			t.Errorf("PatternAlternating: [%d] = 0x%02X, 期待 0x%02X", i, v, want)
		}
	}
}

// TestFillPatternOddLength は 8 の倍数でない長さでも埋まることを確かめる。
func TestFillPatternOddLength(t *testing.T) {
	for _, n := range []int{0, 1, 7, 8, 9, 32, 33} {
		buf := make([]uint8, n)
		FillPattern(buf, PatternRandom, 1, SaltRAM)
		// 長さ 0 以外で、すべてが 0 のままではないこと（確率的だが十分)
		if n >= 8 {
			allZero := true
			for _, v := range buf {
				if v != 0 {
					allZero = false
					break
				}
			}
			if allZero {
				t.Errorf("長さ %d が埋まっていない", n)
			}
		}
	}
}

// TestParsePattern は設定ファイルの値との対応を確かめる。
func TestParsePattern(t *testing.T) {
	tests := []struct {
		s    string
		want Pattern
		ok   bool
	}{
		{"zero", PatternZero, true},
		{"ff", PatternFF, true},
		{"pattern", PatternAlternating, true},
		{"random", PatternRandom, true},
		{"nope", PatternZero, false},
	}
	for _, tt := range tests {
		got, ok := ParsePattern(tt.s)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParsePattern(%q) = %v, %v; 期待 %v, %v", tt.s, got, ok, tt.want, tt.ok)
		}
		if tt.ok && tt.want.String() != tt.s {
			t.Errorf("%v.String() = %q, 期待 %q", tt.want, tt.want.String(), tt.s)
		}
	}
}
