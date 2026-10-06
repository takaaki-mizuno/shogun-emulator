package emu

import (
	"errors"
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// 常時記録（ジャーナル、設計書 14 編 §14.16）。
//
// すべてのエミュレータで、入力と介入（入力以外で Machine State を変えた操作）を
// 常に記録する。形式は入力ムービー（SHGM）と同じで、利用者のムービー記録とは
// 別に動く。記録の対象（フレームの開始と介入）がすべてエミュレーション
// ゴルーチンで起きるため、ここに置く。

// ReproAuthor は Repro のムービーのヘッダの作者に入れる印。再生し終えたとき
// 最後のフレームで一時停止する（設計書 14 編 §14.16.3）。
const ReproAuthor = "shogun-repro"

// journalState は常時記録の状態。エミュレーションゴルーチンだけが触る。
type journalState struct {
	rec *movie.Recorder
	// blob は記録を始めた時点のセーブステート。Repro に埋め込む。
	blob []uint8
	// base は記録を始めた時点の本体のフレーム数。
	base uint64
	// frameStart は今のフレームの開始時の累積 CPU サイクル数。
	frameStart uint64
	// pending は再生中の、まだ適用していない今のフレームの介入。
	pending []movie.Record
	// replaying は Re-Reach の再生中であることを表す。チェックサムを比べず、
	// ヘッダを照合しない。
	replaying bool
	// pauseAtEnd は再生し終えたら一時停止することを表す（Repro）。
	pauseAtEnd bool
}

// startJournal は記録を新しく始める。start が StartSaveState のときは今の状態を
// 埋め込む。
func (e *Emulator) startJournal(start movie.StartKind) {
	if e.machine == nil {
		e.journal = journalState{}
		return
	}
	h := e.movieHeader()
	blob := e.machine.SaveState()
	if start == movie.StartSaveState {
		h.Start, h.StateBlob = movie.StartSaveState, blob
	}
	e.journal.rec = movie.NewRecorder(h)
	e.journal.blob = blob
	e.journal.base = e.machine.Frames()
	e.journal.frameStart = e.machine.Cycles()
}

// journalFrame はフレームの開始を記録する。beginFrame から呼ぶ。
func (e *Emulator) journalFrame() {
	e.journal.frameStart = e.machine.Cycles()
	r := e.journal.rec
	if r == nil {
		return
	}
	var hash [8]uint8
	if r.NeedsChecksum() {
		hash = e.machine.StateHash()
	}
	r.BeginFrame(e.latch.get(), hash)
}

// intervene は介入を記録する（設計書 14 編 §14.16.2）。フレームの開始を
// 待っている間（エージェントの入力の経路で、次のフレームの最初の命令の前）の
// 介入は、次のフレームの開始時の介入として記録する。
func (e *Emulator) intervene(rec movie.Record) {
	r := e.journal.rec
	if r == nil || e.machine == nil {
		return
	}
	if e.framePending {
		r.Intervene(rec, true)
		return
	}
	rec.Cycle = uint32(e.machine.Cycles() - e.journal.frameStart)
	r.Intervene(rec, false)
}

// applyIntervention は再生で介入を適用し、常時記録にも残す。再生中の編集を断る
// 規則（設計書 09 編 §9.4.8）は、再生そのものが行う適用には及ばない。
func (e *Emulator) applyIntervention(rec movie.Record) {
	m := e.machine
	switch rec.Kind {
	case movie.KindPoke:
		if err := debug.WriteMemory(m, debug.Space(rec.Space), int(rec.Addr), uint8(rec.Value), rec.Flag); err != nil {
			e.notifyError(err)
		}
	case movie.KindSetRegister:
		e.dbg.SetRegister(debug.Register(rec.Reg), uint16(rec.Value))
	case movie.KindFreeze:
		e.setFreeze(rec)
	case movie.KindUnfreeze:
		e.removeFreeze(uint16(rec.Addr))
	case movie.KindOverlay:
		m.ROM.Overlay().SetEnabled(rec.Flag)
	}
	e.intervene(rec)
}

// setFreeze は Freeze のレコードを Freeze の一覧に反映する。
func (e *Emulator) setFreeze(rec movie.Record) {
	val := make([]uint8, rec.Size)
	for i := range val {
		val[i] = uint8(rec.Value >> (8 * i))
	}
	list := e.dbg.Freezes()
	replaced := false
	for i, f := range list {
		if f.Addr == uint16(rec.Addr) {
			list[i].Value, replaced = val, true
		}
	}
	if !replaced {
		list = append(list, debug.Freeze{Addr: uint16(rec.Addr), Value: val})
	}
	e.dbg.SetFreezes(list)
}

// removeFreeze は Freeze を外す。
func (e *Emulator) removeFreeze(addr uint16) {
	var keep []debug.Freeze
	for _, f := range e.dbg.Freezes() {
		if f.Addr != addr {
			keep = append(keep, f)
		}
	}
	e.dbg.SetFreezes(keep)
}

// takeInterventions はフレームの介入を受け取る。サイクル数 0 のものは今
// 適用し、残りは命令境界ごとに applyDueInterventions で適用する。
func (e *Emulator) takeInterventions(list []movie.Record) {
	// 前のフレームで適用しきれなかったものを先に適用する。
	for _, rec := range e.journal.pending {
		e.applyIntervention(rec)
	}
	e.journal.pending = e.journal.pending[:0]
	for _, rec := range list {
		if rec.Cycle == 0 {
			e.applyIntervention(rec)
			continue
		}
		e.journal.pending = append(e.journal.pending, rec)
	}
}

// applyDueInterventions はフレームの開始からのサイクル数が記録の値以上に
// なった介入を適用する。命令を実行する前に呼ぶ。
func (e *Emulator) applyDueInterventions() {
	if len(e.journal.pending) == 0 {
		return
	}
	elapsed := e.machine.Cycles() - e.journal.frameStart
	n := 0
	for _, rec := range e.journal.pending {
		if rec.Cycle == movie.EndOfFrame || uint64(rec.Cycle) > elapsed {
			break
		}
		e.applyIntervention(rec)
		n++
	}
	e.journal.pending = append(e.journal.pending[:0], e.journal.pending[n:]...)
}

// flushInterventions は残っている介入をすべて適用する。再生の終わりで呼ぶ。
// フレームの終わり（EndOfFrame）の介入はここで適用される。
func (e *Emulator) flushInterventions() {
	for _, rec := range e.journal.pending {
		e.applyIntervention(rec)
	}
	e.journal.pending = nil
}

// JournalStatus は常時記録の状態（record.status）。
type JournalStatus struct {
	// Start は開始方法（power-on・savestate）。
	Start string
	// Frames は記録したフレーム数。
	Frames uint64
	// Inputs と Interventions は入力と介入のレコードの数。
	Inputs, Interventions int
	Rerecords             uint64
	// ReReachable は Re-Reach できる（電源投入から始まっている）ことを表す。
	ReReachable bool
}

// Journal は常時記録の写しと、記録を始めた時点のセーブステートを返す。
func (e *Emulator) Journal() (*movie.Movie, []uint8, error) {
	var m *movie.Movie
	var blob []uint8
	ok := e.WithMachine(func(*nes.NES) {
		if e.journal.rec != nil {
			m = e.journal.rec.Snapshot()
			blob = append([]uint8(nil), e.journal.blob...)
		}
	})
	if !ok {
		return nil, nil, errStopped
	}
	if m == nil {
		return nil, nil, errNoROM
	}
	return m, blob, nil
}

// JournalStatus は常時記録の状態を返す。
func (e *Emulator) JournalStatus() (JournalStatus, error) {
	m, _, err := e.Journal()
	if err != nil {
		return JournalStatus{}, err
	}
	st := JournalStatus{Start: m.Header.Start.String(), Frames: m.Header.TotalFrames, Rerecords: m.Header.Rerecords,
		ReReachable: m.Header.Start == movie.StartPowerOn}
	for _, r := range m.Records {
		switch {
		case r.Kind == movie.KindInput:
			st.Inputs++
		case r.Kind.IsIntervention():
			st.Interventions++
		}
	}
	return st, nil
}

// SetJournal は常時記録を m（と開始時のセーブステート blob）に差し替え、続きを
// 記録する。Fork で元の Instance の記録を複製するために使う（設計書 14 編
// §14.3.2）。
func (e *Emulator) SetJournal(m *movie.Movie, blob []uint8) {
	e.WithMachine(func(n *nes.NES) {
		if n == nil {
			return
		}
		e.journal.rec = movie.ResumeRecorder(m)
		e.journal.blob = append([]uint8(nil), blob...)
		e.journal.base = n.Frames() - m.Header.TotalFrames
	})
}

// cmdReplay は Re-Reach の再生を始める。
type cmdReplay struct {
	m    *movie.Movie
	done chan error
}

func (cmdReplay) isCommand() {}

// Replay は記録 m を Re-Reach として再生し始める（設計書 14 編 §14.15.3）。
//
// 記録の初期状態で電源を入れ直し、記録の入力と介入を使う。ROM が変わっている
// ことを前提にし、ヘッダを照合せず、チェックサムも比べない。進めるのは
// StepWith で行い、止めるには StopReplay を呼ぶ。電源投入から始まる記録だけを
// 受け付ける。
func (e *Emulator) Replay(m *movie.Movie) error {
	if m.Header.Start != movie.StartPowerOn {
		return errors.New("emu: セーブステートから始まる記録は Re-Reach できない")
	}
	return e.apply(cmdReplay{m: m, done: make(chan error, 1)})
}

// StopReplay は Re-Reach の再生を止める。
func (e *Emulator) StopReplay() error { return e.StopMovie() }

// startReplay はエミュレーションゴルーチンで Re-Reach の再生を始める。
func (e *Emulator) startReplay(m *movie.Movie) error {
	if e.machine == nil {
		return errNoROM
	}
	if err := e.stopRecording(); err != nil {
		return err
	}
	e.stopPlayback("")
	e.cancelPending()
	e.machine.PowerOn(nes.InitState(m.Header.Init))
	e.machine.APU.SetOutput(e.apuOutput())
	e.powerOnCycles = e.machine.Cycles()
	e.afterLoadState()
	e.resetRewind()
	e.dbg.SetFreezes(nil)
	e.startJournal(movie.StartPowerOn)
	e.player = movie.NewPlayer(m)
	e.journal.replaying = true
	e.journal.pending = nil
	e.paused = true
	e.setPaused(true)
	e.hasFrame = false
	e.framePending = false
	e.frameBoundary()
	e.setMovieStatus()
	e.updateStatus()
	return nil
}

// errReproDir は .repro のディレクトリに repro.shgm が無いことを表す。
var errReproDir = fmt.Errorf("emu: ディレクトリに repro.shgm が無い")

// SetRegister は CPU のレジスタを書き換え、常時記録に残す。命令の途中で
// 止まっているときは断る。CPU デバッガと Agent Interface の cpu.set が使う。
func (e *Emulator) SetRegister(r debug.Register, v uint16) error {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil {
			err = errNoROM
			return
		}
		if e.recorder != nil || e.player != nil {
			err = errMovieEdit
			return
		}
		e.dbg.SetRegister(r, v)
		e.intervene(movie.Record{Kind: movie.KindSetRegister, Reg: uint8(r), Value: uint32(v)})
	})
	if !ok {
		return errStopped
	}
	return err
}

// SetFreezes は Freeze の一覧を差し替え、変わった分を常時記録に残す
// （設計書 14 編 §14.13.2、§14.16.2）。
func (e *Emulator) SetFreezes(list []debug.Freeze) error {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil {
			err = errNoROM
			return
		}
		old := e.dbg.Freezes()
		for _, o := range old {
			found := false
			for _, n := range list {
				if n.Addr == o.Addr {
					found = true
				}
			}
			if !found {
				e.intervene(movie.Record{Kind: movie.KindUnfreeze, Addr: uint32(o.Addr)})
			}
		}
		for _, n := range list {
			same := false
			for _, o := range old {
				if o.Addr == n.Addr && string(o.Value) == string(n.Value) {
					same = true
				}
			}
			if !same {
				var v uint32
				for i, b := range n.Value {
					v |= uint32(b) << (8 * i)
				}
				e.intervene(movie.Record{Kind: movie.KindFreeze, Addr: uint32(n.Addr), Size: uint8(len(n.Value)), Value: v})
			}
		}
		e.dbg.SetFreezes(list)
	})
	if !ok {
		return errStopped
	}
	return err
}

// RewindState は巻き戻しの記録から、frame 以前で最も新しい状態を返す。その
// 状態のフレーム番号と、取り出せる最も古いフレームも返す。巻き戻しが無効か、
// 範囲の外のときは誤りを返す。
func (e *Emulator) RewindState(frame uint64) (at uint64, data []uint8, earliest uint64, err error) {
	ok := e.WithMachine(func(*nes.NES) {
		if e.rewind == nil {
			err = errors.New("emu: 巻き戻しが無効である（設定 state.rewindEnabled）")
			return
		}
		first, has := e.rewind.earliest()
		earliest = first
		if !has || frame < first {
			err = fmt.Errorf("emu: フレーム %d は巻き戻しの記録の範囲（%d 以降）の外である", frame, first)
			return
		}
		st, found := e.rewind.stateAtOrBefore(frame)
		if !found {
			err = fmt.Errorf("emu: フレーム %d 以前の状態が記録に無い", frame)
			return
		}
		at = st.frame
		data, err = decompressState(st.data)
	})
	if !ok {
		return 0, nil, 0, errStopped
	}
	return at, data, earliest, err
}

// JournalBase は常時記録を始めた時点の本体のフレーム数を返す。
func (e *Emulator) JournalBase() uint64 {
	var b uint64
	e.WithMachine(func(*nes.NES) { b = e.journal.base })
	return b
}
