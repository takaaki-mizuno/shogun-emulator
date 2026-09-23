package video

import "image"

// Overscan は画の周囲から隠すピクセル数。
//
// 実機の TV は画の周囲が隠れる。その前提で作られたゲームは画面端に
// 使わないタイルを出すことがあり、隠さないとそれが見える。
type Overscan struct {
	Top, Bottom, Left, Right int
}

// DefaultOverscan は既定の隠し方を返す。
//
// 上下 8 ピクセルを隠す。NTSC の名目上の可視高さが 224 行であることに
// 合わせる。左右を隠さないのは、横方向に隠すと横スクロールの継ぎ目を
// 確認できなくなるためである。
func DefaultOverscan() Overscan { return Overscan{Top: 8, Bottom: 8} }

// Rect は Frame のうち表示する範囲を返す。
//
// height には表示する画の高さを渡す。リージョンによって異なるためである。
// 隠す量が画の大きさ以上のときは 1 ピクセルを残す。範囲が空になると
// 描画先の画像を作れない。
func (o Overscan) Rect(height int) image.Rectangle {
	if height <= 0 || height > Height {
		height = Height
	}
	left := clampOverscan(o.Left, Width)
	right := clampOverscan(o.Right, Width)
	top := clampOverscan(o.Top, height)
	bottom := clampOverscan(o.Bottom, height)

	x0, x1 := left, Width-right
	if x0 >= x1 {
		x0, x1 = 0, 1
	}
	y0, y1 := top, height-bottom
	if y0 >= y1 {
		y0, y1 = 0, 1
	}
	return image.Rect(x0, y0, x1, y1)
}

// clampOverscan は隠す量を 0 から limit-1 の範囲に収める。
func clampOverscan(v, limit int) int {
	if v < 0 {
		return 0
	}
	if v > limit-1 {
		return limit - 1
	}
	return v
}
