package debug

import "testing"

// frameMem は追跡対象の内容を組み立てる。
func frameMem(ram, prgram []uint8) [regionCount][]uint8 {
	var m [regionCount][]uint8
	m[RegionRAM] = ram
	m[RegionCIRAM] = make([]uint8, 4096)
	m[RegionOAM] = make([]uint8, 256)
	m[RegionPalette] = make([]uint8, 32)
	m[RegionPRGRAM] = prgram
	return m
}

// TestChangeTrackerHeatDecays は書き換わったバイトの値が時間とともに
// 下がることを確かめる。
func TestChangeTrackerHeatDecays(t *testing.T) {
	tr := NewChangeTracker(10)
	ram := make([]uint8, 2048)
	tr.Update(frameMem(ram, nil)) // 最初は取り込むだけ

	if h := tr.Heat(RegionRAM, 5); h != 0 {
		t.Fatalf("書き換えていないのに %v", h)
	}

	ram[5] = 1
	tr.Update(frameMem(ram, nil))
	if h := tr.Heat(RegionRAM, 5); h != 1 {
		t.Errorf("書き換えた直後 = %v, 期待 1", h)
	}
	if h := tr.Heat(RegionRAM, 6); h != 0 {
		t.Errorf("書き換えていないバイト = %v, 期待 0", h)
	}

	prev := float32(1)
	for i := range 10 {
		tr.Update(frameMem(ram, nil))
		h := tr.Heat(RegionRAM, 5)
		if h >= prev && h != 0 {
			t.Errorf("%d フレーム後に下がっていない（%v → %v）", i+1, prev, h)
		}
		prev = h
	}
	if prev != 0 {
		t.Errorf("decay を過ぎても %v が残っている", prev)
	}
}

// TestChangeTrackerIgnoresSameValue は同じ値の書き込みで色が付かない
// ことを確かめる。
func TestChangeTrackerIgnoresSameValue(t *testing.T) {
	tr := NewChangeTracker(10)
	ram := make([]uint8, 2048)
	ram[3] = 7
	tr.Update(frameMem(ram, nil))
	ram[3] = 7
	tr.Update(frameMem(ram, nil))
	if h := tr.Heat(RegionRAM, 3); h != 0 {
		t.Errorf("同じ値で %v になった", h)
	}
}

// TestChangeTrackerMemory は 8 KiB の PRG-RAM を持つ構成で記録の量が
// 設計書の見積もり程度に収まることを確かめる。
func TestChangeTrackerMemory(t *testing.T) {
	tr := NewChangeTracker(0)
	tr.Update(frameMem(make([]uint8, 2048), make([]uint8, 8192)))
	// 記録 4 バイト + 比較用 1 バイト。(2 + 4 + 0.25 + 0.03 + 8) KiB × 5。
	const limit = 80 * 1024
	if n := tr.Bytes(); n > limit {
		t.Errorf("記録の量 = %d バイト, 上限 %d バイト", n, limit)
	}
	t.Logf("記録の量: %d バイト", tr.Bytes())
}

// TestChangeTrackerHeatRow は 1 行分の値をまとめて取り出せることを
// 確かめる。
func TestChangeTrackerHeatRow(t *testing.T) {
	tr := NewChangeTracker(10)
	ram := make([]uint8, 2048)
	tr.Update(frameMem(ram, nil))
	ram[0x12] = 9
	tr.Update(frameMem(ram, nil))

	row := make([]float32, 16)
	tr.HeatRow(RegionRAM, 0x10, row)
	for i, h := range row {
		want := float32(0)
		if i == 2 {
			want = 1
		}
		if h != want {
			t.Errorf("行の %d 番目 = %v, 期待 %v", i, h, want)
		}
	}
}
