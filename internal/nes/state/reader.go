package state

import "encoding/binary"

// Reader はセクション付きバイナリを読み出す。
//
// エラーは内部に蓄積する。各読み出しメソッドはエラーを返さず、最後に Err で
// 確認する。エラーが起きた後の読み出しはゼロ値を返し、位置を進めない。
type Reader struct {
	buf []byte
	pos int
	err error

	// limits は入れ子になったセクションの終端位置。
	limits []int
}

// NewReader はバイト列を読む Reader を返す。
func NewReader(b []byte) *Reader {
	return &Reader{buf: b}
}

// Err は最初に発生したエラーを返す。
func (r *Reader) Err() error { return r.err }

// fail はエラーを記録する。既にエラーがあれば上書きしない。
func (r *Reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// Fail は読み出し側が見つけた不整合を記録する。
//
// 値そのものは読めても内容が矛盾している場合に使う。以降の読み出しは
// ゼロ値を返し、Err がこのエラーを返す。
func (r *Reader) Fail(err error) { r.fail(err) }

// limit は現在有効な終端位置を返す。
func (r *Reader) limit() int {
	if n := len(r.limits); n > 0 {
		return r.limits[n-1]
	}
	return len(r.buf)
}

// take は n バイトを切り出す。足りなければエラーを記録して nil を返す。
func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.pos+n > r.limit() {
		r.fail(ErrTruncated)
		return nil
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

// Section は名前が一致するセクションへ入る。
//
// 一致しないセクションが現れた場合、それを長さ分読み飛ばして次を探す。
// 現在のセクションの終端まで探して見つからなければ ok に false を返す。
//
//	if end, ok := r.Section("cpu"); ok {
//	    c.A = r.U8()
//	    end()
//	}
func (r *Reader) Section(name string) (end func(), ok bool) {
	if r.err != nil {
		return func() {}, false
	}
	for r.pos < r.limit() {
		start := r.pos

		nameLen := r.take(1)
		if nameLen == nil {
			return func() {}, false
		}
		gotName := r.take(int(nameLen[0]))
		if gotName == nil {
			return func() {}, false
		}
		lenBytes := r.take(4)
		if lenBytes == nil {
			return func() {}, false
		}
		bodyLen := int(binary.LittleEndian.Uint32(lenBytes))
		if r.pos+bodyLen > r.limit() {
			r.fail(ErrTruncated)
			return func() {}, false
		}

		if string(gotName) == name {
			bodyEnd := r.pos + bodyLen
			r.limits = append(r.limits, bodyEnd)
			closed := false
			return func() {
				if closed {
					panic("state: セクション " + name + " を二重に閉じた")
				}
				closed = true
				r.limits = r.limits[:len(r.limits)-1]
				r.pos = bodyEnd // 未読の部分を読み飛ばす
			}, true
		}

		// 名前が違うセクションは読み飛ばす
		r.pos += bodyLen
		if r.pos <= start {
			r.fail(ErrBadFormat) // 進んでいない。壊れたデータ
			return func() {}, false
		}
	}
	return func() {}, false
}

// RequireSection は Section と同じだが、見つからないときエラーを記録する。
func (r *Reader) RequireSection(name string) func() {
	end, ok := r.Section(name)
	if !ok {
		r.fail(ErrSectionMismatch)
	}
	return end
}

// U8 は 1 バイトを読む。
func (r *Reader) U8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

// Bool は真偽値を読む。
func (r *Reader) Bool() bool { return r.U8() != 0 }

// U16 は 2 バイトをリトルエンディアンで読む。
func (r *Reader) U16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

// U32 は 4 バイトをリトルエンディアンで読む。
func (r *Reader) U32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

// U64 は 8 バイトをリトルエンディアンで読む。
func (r *Reader) U64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

// I32 は符号付き 32 bit を読む。
func (r *Reader) I32() int32 { return int32(r.U32()) }

// Int は符号付き 32 bit を int として読む。
func (r *Reader) Int() int { return int(r.I32()) }

// Bytes は長さ付きのバイト列を読む。返り値は元のバッファへの参照ではなく複製。
func (r *Reader) Bytes() []uint8 {
	n := r.U32()
	b := r.take(int(n))
	if b == nil {
		return nil
	}
	out := make([]uint8, len(b))
	copy(out, b)
	return out
}

// RawBytes は長さを読まずに dst の長さ分だけ読み、dst へ複製する。
func (r *Reader) RawBytes(dst []uint8) {
	b := r.take(len(dst))
	if b == nil {
		return
	}
	copy(dst, b)
}

// String は長さ付きの文字列を読む。
func (r *Reader) String() string {
	n := r.U32()
	b := r.take(int(n))
	if b == nil {
		return ""
	}
	return string(b)
}

// Remaining は現在のセクション内の未読バイト数を返す。
func (r *Reader) Remaining() int {
	if r.err != nil {
		return 0
	}
	return r.limit() - r.pos
}
