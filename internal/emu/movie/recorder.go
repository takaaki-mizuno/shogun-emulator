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
	r.frames++
	r.movie.Header.TotalFrames = r.frames
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
}

// Movie は記録したムービーを返す。
func (r *Recorder) Movie() *Movie { return &r.movie }

// Encode は記録したムービーをバイト列にする。
func (r *Recorder) Encode() []uint8 { return r.movie.Encode() }
