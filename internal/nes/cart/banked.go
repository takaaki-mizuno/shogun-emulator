package cart

// banked はバンク切り替えされたメモリを表す。
//
// 各マッパーがアドレス計算を個別に書くと誤りが混入する。ウィンドウごとの
// バンク番号だけをマッパーが決め、アドレスの計算はここに集める。
type banked struct {
	data     []uint8
	bankSize int
	// windows はウィンドウごとのバンク番号。
	windows []int
}

// newBanked はウィンドウ数 n の banked を作る。
func newBanked(data []uint8, bankSize, n int) *banked {
	return &banked{data: data, bankSize: bankSize, windows: make([]int, n)}
}

// bankCount はバンクの総数を返す。
func (b *banked) bankCount() int {
	if b.bankSize <= 0 {
		return 0
	}
	return len(b.data) / b.bankSize
}

// resolve はウィンドウ内のオフセットから data のインデックスを返す。
//
// バンク番号をバンク総数で剰余を取る。ROM の末尾を超えるバンクは前のバンクの
// ミラーになる。負のバンク番号（末尾からの指定）も扱う。
func (b *banked) resolve(windowIndex int, offsetInWindow uint16) (int, bool) {
	if len(b.data) == 0 || b.bankSize <= 0 {
		return 0, false
	}
	n := b.bankCount()
	if n == 0 {
		// メモリがウィンドウより小さい。4 KiB の CHR-RAM を 8 KiB の
		// ウィンドウへ置く場合がこれにあたる。実機では容量を超える
		// アドレス線が繋がっておらず、同じ内容が繰り返し現れる。
		return int(offsetInWindow) % len(b.data), true
	}
	bank := b.windows[windowIndex] % n
	if bank < 0 {
		bank += n
	}
	i := bank*b.bankSize + int(offsetInWindow)
	if i < 0 || i >= len(b.data) {
		return 0, false
	}
	return i, true
}

// read はウィンドウ内のオフセットを読む。
func (b *banked) read(windowIndex int, offsetInWindow uint16) (uint8, bool) {
	i, ok := b.resolve(windowIndex, offsetInWindow)
	if !ok {
		return 0, false
	}
	return b.data[i], true
}

// write はウィンドウ内のオフセットへ書く。
func (b *banked) write(windowIndex int, offsetInWindow uint16, v uint8) bool {
	i, ok := b.resolve(windowIndex, offsetInWindow)
	if !ok {
		return false
	}
	b.data[i] = v
	return true
}

// setBank はウィンドウにバンクを割り当てる。
func (b *banked) setBank(windowIndex, bank int) { b.windows[windowIndex] = bank }

// bankOffset はウィンドウの先頭が data 内のどこを指すかを返す。
// デバッガの表示に使う。
func (b *banked) bankOffset(windowIndex int) uint32 {
	i, ok := b.resolve(windowIndex, 0)
	if !ok {
		return 0
	}
	return uint32(i)
}

// normalizedBank はウィンドウに割り当てられたバンク番号を剰余を取った形で返す。
func (b *banked) normalizedBank(windowIndex int) int {
	n := b.bankCount()
	if n == 0 {
		return 0
	}
	bank := b.windows[windowIndex] % n
	if bank < 0 {
		bank += n
	}
	return bank
}

// cpuOffset は $8000 以降の CPU アドレスに対応する data の位置を返す。
//
// ウィンドウが $8000 から bankSize ずつ並ぶ PRG に使う。デバッガが
// 実行した命令を PRG-ROM のファイルオフセットで記録するために用いる。
func (b *banked) cpuOffset(addr uint16) (int, bool) {
	if addr < 0x8000 || b.bankSize <= 0 {
		return 0, false
	}
	rel := int(addr - 0x8000)
	w := rel / b.bankSize
	if w >= len(b.windows) {
		return 0, false
	}
	return b.resolve(w, uint16(rel%b.bankSize))
}
