package debug

import (
	"bufio"
	"io"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// DefaultTraceRingSize はトレースのリングに保持する命令数の既定値。
//
// 1 命令 32 バイトで 32 MiB を占める（設計書 09 編 §9.7）。
const DefaultTraceRingSize = 1_000_000

// Tracer は実行した命令を記録する。
//
// リングバッファに最新 N 命令を保持し、要求されたときやブレーク
// ポイントで止まったときに書き出す。整形は書き出すときに行う。
// 命令ごとに文字列を作ると、トレースを有効にするだけで実行が遅れる。
//
// Record はエミュレーションゴルーチンだけが呼ぶ。
type Tracer struct {
	ring []cpu.TraceRecord
	// next は次に書き込む位置。
	next int
	// count は記録した命令の総数。
	count uint64
	// out は常時の出力先。nil のとき出さない。
	out *bufio.Writer
	// outErr は常時の出力で起きた最初のエラー。
	outErr error

	// frames はフレームの開始のサイクル数の表（traceq.go）。リングの範囲に
	// 合わせて古いものを捨てる。
	frames []FrameStart
	// ints と intCount は割り込みのリング。
	ints     []IntEvent
	intCount uint64
	// bus と busCount はバスアクセスのリング（bus: true のときだけ）。
	bus      []BusAccess
	busCount uint64
}

// NewTracer はトレースを作る。size が 0 以下のとき既定値を使う。
func NewTracer(size int) *Tracer {
	if size <= 0 {
		size = DefaultTraceRingSize
	}
	return &Tracer{ring: make([]cpu.TraceRecord, size)}
}

// Record は 1 命令分を記録する。
func (t *Tracer) Record(r cpu.TraceRecord) {
	t.ring[t.next] = r
	t.next++
	if t.next == len(t.ring) {
		t.next = 0
	}
	t.count++
	if t.out != nil && t.outErr == nil {
		if _, err := t.out.WriteString(r.Line() + "\n"); err != nil {
			t.outErr = err
		}
	}
}

// Len はリングに入っている命令の数を返す。
func (t *Tracer) Len() int {
	if t.count < uint64(len(t.ring)) {
		return int(t.count)
	}
	return len(t.ring)
}

// Count は記録した命令の総数を返す。
func (t *Tracer) Count() uint64 { return t.count }

// Clear はリングを空にする。
func (t *Tracer) Clear() {
	t.next = 0
	t.count = 0
	t.frames = nil
	t.intCount = 0
	t.busCount = 0
}

// WriteTo はリングの内容を古い順に書き出す。
//
// 形式は State.TraceLine と同じである（設計書 03 編 §3.8）。
func (t *Tracer) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	var written int64
	n := t.Len()
	start := t.next - n
	if start < 0 {
		start += len(t.ring)
	}
	for i := range n {
		line := t.ring[(start+i)%len(t.ring)].Line()
		m, err := bw.WriteString(line + "\n")
		written += int64(m)
		if err != nil {
			return written, err
		}
	}
	return written, bw.Flush()
}

// Last は最新の n 命令を古い順に返す。表示に使う。
func (t *Tracer) Last(n int) []cpu.TraceRecord {
	n = min(n, t.Len())
	out := make([]cpu.TraceRecord, n)
	for i := range n {
		idx := t.next - n + i
		if idx < 0 {
			idx += len(t.ring)
		}
		out[i] = t.ring[idx]
	}
	return out
}

// SetOutput は常時の出力先を設定する。nil で止める。
//
// 毎秒 30 万行程度になる。バッファを通して書く。
func (t *Tracer) SetOutput(w io.Writer) error {
	err := t.Flush()
	t.outErr = nil
	if w == nil {
		t.out = nil
		return err
	}
	t.out = bufio.NewWriterSize(w, 1<<16)
	return err
}

// Flush は常時の出力のバッファを書き出す。
func (t *Tracer) Flush() error {
	if t.out == nil {
		return t.outErr
	}
	if err := t.out.Flush(); err != nil && t.outErr == nil {
		t.outErr = err
	}
	return t.outErr
}

// Bytes はリングが占めるメモリの量を返す。
func (t *Tracer) Bytes() int { return len(t.ring) * traceRecordBytes }

// traceRecordBytes は 1 命令分の大きさ。
const traceRecordBytes = 32
