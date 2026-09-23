package state

import "encoding/binary"

// Writer はセクション付きバイナリを組み立てる。
//
// セクションの構造は次のとおり。
//
//	[名前の長さ: 1 バイト][名前: n バイト][本体の長さ: 4 バイト LE][本体]
//
// 本体の長さは Section の戻り値を呼んだ時点で確定して書き戻す。
type Writer struct {
	buf   []byte
	depth int // 未クローズのセクションの数。Bytes の呼び出し時に検証する
}

// NewWriter は空の Writer を返す。
func NewWriter() *Writer {
	return &Writer{buf: make([]byte, 0, 64*1024)}
}

// Section は名前付きセクションを開始する。戻り値の関数を呼ぶとセクションを閉じる。
//
//	end := w.Section("cpu")
//	w.U8(c.A)
//	end()
//
// 呼び出しは入れ子にできる。
func (w *Writer) Section(name string) func() {
	if len(name) == 0 || len(name) > maxSectionNameLen {
		panic("state: セクション名の長さが不正: " + name)
	}
	w.buf = append(w.buf, byte(len(name)))
	w.buf = append(w.buf, name...)

	lenPos := len(w.buf)
	w.buf = append(w.buf, 0, 0, 0, 0) // 本体の長さ。閉じるときに書き戻す
	bodyStart := len(w.buf)
	w.depth++

	closed := false
	return func() {
		if closed {
			panic("state: セクション " + name + " を二重に閉じた")
		}
		closed = true
		w.depth--
		binary.LittleEndian.PutUint32(w.buf[lenPos:], uint32(len(w.buf)-bodyStart))
	}
}

// U8 は 1 バイトを書く。
func (w *Writer) U8(v uint8) { w.buf = append(w.buf, v) }

// Bool は真偽値を 1 バイトで書く。
func (w *Writer) Bool(v bool) {
	if v {
		w.buf = append(w.buf, 1)
	} else {
		w.buf = append(w.buf, 0)
	}
}

// U16 は 2 バイトをリトルエンディアンで書く。
func (w *Writer) U16(v uint16) {
	w.buf = binary.LittleEndian.AppendUint16(w.buf, v)
}

// U32 は 4 バイトをリトルエンディアンで書く。
func (w *Writer) U32(v uint32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
}

// U64 は 8 バイトをリトルエンディアンで書く。
func (w *Writer) U64(v uint64) {
	w.buf = binary.LittleEndian.AppendUint64(w.buf, v)
}

// I32 は符号付き 32 bit を書く。
func (w *Writer) I32(v int32) { w.U32(uint32(v)) }

// Int は int を符号付き 32 bit として書く。
//
// int をそのまま書かないのは、32 bit 環境と 64 bit 環境で幅が変わると
// ステートの互換が崩れるためである。
func (w *Writer) Int(v int) { w.I32(int32(v)) }

// Bytes は長さ付きのバイト列を書く。
func (w *Writer) Bytes(v []uint8) {
	w.U32(uint32(len(v)))
	w.buf = append(w.buf, v...)
}

// RawBytes は長さを書かずにバイト列を書く。
// 読み出し側が長さを知っている固定長の配列に使う。
func (w *Writer) RawBytes(v []uint8) {
	w.buf = append(w.buf, v...)
}

// String は長さ付きの文字列を書く。
func (w *Writer) String(s string) {
	w.U32(uint32(len(s)))
	w.buf = append(w.buf, s...)
}

// Data は組み立てたバイト列を返す。未クローズのセクションがあるとパニックする。
func (w *Writer) Data() []byte {
	if w.depth != 0 {
		panic("state: 閉じていないセクションがある")
	}
	return w.buf
}

// Len は現在のバイト数を返す。
func (w *Writer) Len() int { return len(w.buf) }
