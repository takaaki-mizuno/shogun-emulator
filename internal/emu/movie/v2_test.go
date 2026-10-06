package movie

import (
	"reflect"
	"testing"
)

// TestInterventionsRoundTrip は介入のレコードを含むムービーを書いて読み、
// 同じ内容に戻ることと、バージョン 2 で書くことを確かめる（設計書 08 編 §8.7.2）。
func TestInterventionsRoundTrip(t *testing.T) {
	r := NewRecorder(Header{ROMName: "x", ChecksumInterval: 2})
	r.Intervene(Record{Kind: KindPoke, Space: 0, Addr: 0x300, Value: 0x42}, true)
	r.BeginFrame([2]uint8{1, 0}, [8]uint8{9})
	r.Intervene(Record{Kind: KindSetRegister, Cycle: 120, Reg: 4, Value: 0xC123}, false)
	r.Intervene(Record{Kind: KindFreeze, Cycle: 300, Addr: 0x10, Size: 2, Value: 0x1234}, false)
	r.BeginFrame([2]uint8{2, 0}, [8]uint8{})
	r.Intervene(Record{Kind: KindUnfreeze, Cycle: 5, Addr: 0x10}, false)
	r.Intervene(Record{Kind: KindOverlay, Cycle: 6, Flag: true}, false)
	r.Intervene(Record{Kind: KindPoke, Space: 3, Addr: 0x20, Value: 7, Flag: true}, true)
	m := r.Snapshot()
	data := m.Encode()
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.FormatVersion != 2 {
		t.Errorf("バージョン = %d, 期待 2", got.Header.FormatVersion)
	}
	if !reflect.DeepEqual(got.Records, m.Records) {
		t.Errorf("読み直したレコードが違う:\n%+v\n%+v", got.Records, m.Records)
	}
	// BeginFrame の後の介入は、記録中のフレーム（BeginFrame したもの）に属する。
	p := NewPlayer(got)
	f, _ := p.BeginFrame()
	if f.Buttons[0] != 1 || len(f.Interventions) != 3 || f.Interventions[0].Kind != KindPoke ||
		f.Interventions[1].Kind != KindSetRegister || f.Interventions[2].Value != 0x1234 {
		t.Errorf("フレーム 0 = %+v", f)
	}
	f, _ = p.BeginFrame()
	if len(f.Interventions) != 3 || f.Interventions[0].Kind != KindUnfreeze || f.Interventions[2].Cycle != EndOfFrame {
		t.Errorf("フレーム 1 = %+v", f)
	}
	// 最後のフレームの後に待っていた介入は、フレームの終わりの介入になる。
	last := m.Records[len(m.Records)-1]
	if last.Kind != KindPoke || last.Cycle != EndOfFrame {
		t.Errorf("最後のレコード = %+v", last)
	}
}

// TestVersion1WithoutInterventions は介入を含まないムービーをバージョン 1 で
// 書き、そのまま読めることを確かめる。
func TestVersion1WithoutInterventions(t *testing.T) {
	r := NewRecorder(Header{ROMName: "x"})
	r.BeginFrame([2]uint8{1, 2}, [8]uint8{})
	got, err := Decode(r.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.FormatVersion != 1 {
		t.Errorf("バージョン = %d, 期待 1", got.Header.FormatVersion)
	}
}

// TestJournalSizePerHour は 1 時間分（216000 フレーム）の記録が約 650 KiB に
// 収まることを確かめる（設計書 14 編 §14.16.1）。
func TestJournalSizePerHour(t *testing.T) {
	r := NewRecorder(Header{ROMName: "x", ChecksumInterval: 60})
	for i := range 216000 {
		r.BeginFrame([2]uint8{uint8(i), 0}, [8]uint8{})
	}
	size := len(r.Encode())
	if size > 700<<10 {
		t.Errorf("1 時間分の大きさ = %d バイト（約 650 KiB を見込む）", size)
	}
	t.Logf("1 時間分 = %d KiB", size>>10)
}
