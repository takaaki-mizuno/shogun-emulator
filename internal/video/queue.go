package video

// Queue はエミュレーションから表示へフレームを渡す。
//
// 2 枚のフレームを交互に使う。表示側が取りに来る前に次のフレームが
// 完成した場合、古いフレームを捨てる。表示が遅れたときにエミュレーションを
// 待たせないためである。エミュレーションの進行はオーディオの消費で決まり、
// 映像で律速させない。
//
// 同期はエミュレーション側と表示側の受け渡しを担う呼び出し側が行う。
// この型はエミュレーションゴルーチンからのみ触る。
type Queue struct {
	// buffers は描画中と完成済みの 2 枚。
	buffers [2]*Frame
	// writing は描画中のフレームの添字。
	writing int
	// ready は完成済みのフレームがあるかを表す。
	ready bool
	// dropped は表示側が取る前に捨てたフレーム数。
	dropped uint64
}

// NewQueue は 2 枚のフレームを持つキューを返す。
func NewQueue() *Queue {
	return &Queue{buffers: [2]*Frame{NewFrame(), NewFrame()}}
}

// Writing は描画中のフレームを返す。PPU がここへ書く。
func (q *Queue) Writing() *Frame { return q.buffers[q.writing] }

// Commit は描画中のフレームを完成として扱い、もう 1 枚へ切り替える。
func (q *Queue) Commit() {
	if q.ready {
		// 表示側が取りに来る前に次が完成した
		q.dropped++
	}
	q.ready = true
	q.writing ^= 1
}

// Take は完成済みのフレームを返す。無いとき nil を返す。
//
// 返したフレームは次の Commit までのあいだ有効である。表示側は内容を
// 複製するか、次のフレームが完成する前に使い終える。
func (q *Queue) Take() *Frame {
	if !q.ready {
		return nil
	}
	q.ready = false
	return q.buffers[q.writing^1]
}

// Dropped は捨てたフレーム数を返す。
func (q *Queue) Dropped() uint64 { return q.dropped }
