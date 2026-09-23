package video

import (
	"image"
	"testing"
)

// TestOverscanRect は隠す量から表示範囲が決まることを確かめる。
func TestOverscanRect(t *testing.T) {
	tests := []struct {
		name   string
		o      Overscan
		height int
		want   image.Rectangle
	}{
		{"隠さない", Overscan{}, Height, image.Rect(0, 0, 256, 240)},
		{"既定", DefaultOverscan(), Height, image.Rect(0, 8, 256, 232)},
		{"PAL の画の高さ", DefaultOverscan(), 239, image.Rect(0, 8, 256, 231)},
		{"左右も隠す", Overscan{Left: 8, Right: 8}, Height, image.Rect(8, 0, 248, 240)},
		{"負の値は 0 として扱う", Overscan{Top: -4}, Height, image.Rect(0, 0, 256, 240)},
	}
	for _, tt := range tests {
		if got := tt.o.Rect(tt.height); got != tt.want {
			t.Errorf("%s: Rect(%d) = %v, 期待 %v", tt.name, tt.height, got, tt.want)
		}
	}
}

// TestOverscanRectNeverEmpty は範囲が空にならないことを確かめる。
// 空の範囲では描画先の画像を作れない。
func TestOverscanRectNeverEmpty(t *testing.T) {
	for _, o := range []Overscan{
		{Top: 240, Bottom: 240},
		{Left: 256, Right: 256},
		{Top: 1000, Bottom: 1000, Left: 1000, Right: 1000},
	} {
		r := o.Rect(Height)
		if r.Dx() <= 0 || r.Dy() <= 0 {
			t.Errorf("%+v の範囲が空である: %v", o, r)
		}
	}
}
