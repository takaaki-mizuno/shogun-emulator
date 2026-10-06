package movie

// Recorder はムービーを記録する。
//
// フレーム 1 つにつき入力レコードを 1 つ書く。リセットはそのフレームの
// 入力レコードの前に、チェックサムは後に挿入する。再生側は同じ順序で
// 読み、フレームの開始時にイベントを適用してから入力を使う。
type Recorder struct {
	movie Movie
	// frames は記録済みのフレーム数。
	frames uint64
	// pendingReset はこのフレームで実行するリセット。
	pendingReset   bool
	pendingHard    bool
	hasPendingHard bool
	// pendingInterventions は次のフレームの開始時（サイクル数 0）に行う介入。
	// フレームの境界で止まっている間の介入を、次のフレームの入力の後に置く。
	pendingInterventions []Record
}

// NewRecorder は記録を始める。
func NewRecorder(h Header) *Recorder {
	h.FormatVersion = FormatVersion
	return &Recorder{movie: Movie{Header: h}}
}

// Frames は記録済みのフレーム数を返す。
func (r *Recorder) Frames() uint64 { return r.frames }

// Rerecords は再記録回数を返す。
func (r *Recorder) Rerecords() uint64 { return r.movie.Header.Rerecords }

// MarkReset は次のフレームでリセットが行われることを記録する。
//
// hard が true のとき電源の入れ直しである。
func (r *Recorder) MarkReset(hard bool) {
	if hard {
		r.hasPendingHard = true
		r.pendingHard = true
		return
	}
	r.pendingReset = true
}

// BeginFrame は 1 フレーム分を記録する。
//
// buttons は連射を適用した後の押下状態である。適用前を記録すると、
// 再生時に連射が別の位相で入り desync する（設計書 08 編 §8.7.5）。
// hash はチェックサムを記録するときの状態のハッシュ。
func (r *Recorder) BeginFrame(buttons [2]uint8, hash [8]uint8) {
	if r.hasPendingHard && r.pendingHard {
		r.movie.Records = append(r.movie.Records, Record{Kind: KindHardReset})
		r.hasPendingHard = false
		r.pendingHard = false
	}
	if r.pendingReset {
		r.movie.Records = append(r.movie.Records, Record{Kind: KindReset})
		r.pendingReset = false
	}

	r.movie.Records = append(r.movie.Records, Record{Kind: KindInput, Buttons: buttons})

	if n := r.movie.Header.ChecksumInterval; n > 0 && r.frames%uint64(n) == 0 {
		r.movie.Records = append(r.movie.Records, Record{Kind: KindChecksum, Hash: hash})
	}
	r.movie.Records = append(r.movie.Records, r.pendingInterventions...)
	r.pendingInterventions = r.pendingInterventions[:0]
	r.frames++
	r.movie.Header.TotalFrames = r.frames
}

// ResumeRecorder は記録済みのムービーの続きを記録する記録器を作る。最後の
// フレームの終わり（EndOfFrame）の介入は、次のフレームを待つ介入に戻す。
func ResumeRecorder(m *Movie) *Recorder {
	r := &Recorder{movie: Movie{Header: m.Header}}
	r.movie.Header.StateBlob = append([]uint8(nil), m.Header.StateBlob...)
	recs := m.Records
	for len(recs) > 0 && recs[len(recs)-1].Kind.IsIntervention() && recs[len(recs)-1].Cycle == EndOfFrame {
		rec := recs[len(recs)-1]
		rec.Cycle = 0
		r.pendingInterventions = append([]Record{rec}, r.pendingInterventions...)
		recs = recs[:len(recs)-1]
	}
	r.movie.Records = append([]Record(nil), recs...)
	r.frames = m.Header.TotalFrames
	return r
}

// NeedsChecksum は次の BeginFrame でチェックサムを記録するかを返す。状態の
// ハッシュを毎フレーム計算しないために使う。
func (r *Recorder) NeedsChecksum() bool {
	n := r.movie.Header.ChecksumInterval
	return n > 0 && r.frames%uint64(n) == 0
}

// Intervene は介入を記録する（設計書 14 編 §14.16.2）。
//
// nextFrame が true のときは、まだ記録していない次のフレームの開始時
// （サイクル数 0）の介入として持ち、そのフレームの入力の後に置く。false の
// ときは記録中のフレームの介入として、rec.Cycle のまま今の位置に置く。
func (r *Recorder) Intervene(rec Record, nextFrame bool) {
	if nextFrame || r.frames == 0 {
		rec.Cycle = 0
		r.pendingInterventions = append(r.pendingInterventions, rec)
		return
	}
	r.movie.Records = append(r.movie.Records, rec)
}

// TruncateTo はフレーム frame 以降のレコードを捨て、再記録回数を 1 増やす。
//
// 巻き戻した後に記録を続けるときに呼ぶ。巻き戻し自体はムービーに
// 残らない（設計書 08 編 §8.7.5）。
func (r *Recorder) TruncateTo(frame uint64) {
	if frame >= r.frames {
		return
	}
	var count uint64
	cut := len(r.movie.Records)
	for i, rec := range r.movie.Records {
		if rec.Kind != KindInput {
			continue
		}
		if count == frame {
			cut = i
			break
		}
		count++
	}
	// 入力レコードの直前に置いたイベントもまとめて捨てる。
	for cut > 0 && r.movie.Records[cut-1].Kind != KindInput &&
		r.movie.Records[cut-1].Kind != KindChecksum {
		cut--
	}
	r.movie.Records = r.movie.Records[:cut]
	r.frames = frame
	r.movie.Header.TotalFrames = frame
	r.movie.Header.Rerecords++
	r.pendingReset = false
	r.pendingHard = false
	r.hasPendingHard = false
	r.pendingInterventions = nil
}

// Movie は記録したムービーを返す。
func (r *Recorder) Movie() *Movie { return &r.movie }

// Snapshot は記録したムービーの写しを返す。次のフレームを待っている介入は、
// 最後のフレームの終わり（EndOfFrame）の介入として含める。再生では記録の
// 終わりで適用する。最後のフレームの終わりと次のフレームの開始の間では命令を
// 実行しないため、同じ状態になる。
func (r *Recorder) Snapshot() *Movie {
	m := &Movie{Header: r.movie.Header}
	m.Header.StateBlob = append([]uint8(nil), r.movie.Header.StateBlob...)
	m.Records = append([]Record(nil), r.movie.Records...)
	for _, rec := range r.pendingInterventions {
		rec.Cycle = EndOfFrame
		m.Records = append(m.Records, rec)
	}
	return m
}

// Encode は記録したムービーをバイト列にする。
func (r *Recorder) Encode() []uint8 { return r.movie.Encode() }

// From はフレーム frame（0 から数える）から始まる記録を作る。開始の状態として
// blob を埋め込む。frame より前のレコードを捨て、frame の前に置いたリセットは
// 残す。Repro の from_frame に使う（設計書 14 編 §14.16.3）。
func (m *Movie) From(frame uint64, blob []uint8) *Movie {
	out := &Movie{Header: m.Header}
	out.Header.Start = StartSaveState
	out.Header.StateBlob = append([]uint8(nil), blob...)
	if frame > out.Header.TotalFrames {
		frame = out.Header.TotalFrames
	}
	out.Header.TotalFrames -= frame
	cut := len(m.Records)
	var n uint64
	for i, rec := range m.Records {
		if rec.Kind != KindInput {
			continue
		}
		if n == frame {
			cut = i
			break
		}
		n++
	}
	for cut > 0 && (m.Records[cut-1].Kind == KindReset || m.Records[cut-1].Kind == KindHardReset) {
		cut--
	}
	out.Records = append([]Record(nil), m.Records[cut:]...)
	return out
}
