package emu

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// playPaused は再生を始め、そのまま一時停止する。
//
// PlayMovie は再生を始めると一時停止を解く。その後に SetPaused(true) を
// 送ると、届くまでの間にエミュレーションが進むフレーム数が実行のたびに
// 変わる。playMoviePaused がエミュレーションゴルーチンの中で続けて
// 止めることで、再生の開始の位置から runFrames で進めた分だけを進める。
func playPaused(t *testing.T, e *Emulator, m *movie.Movie) {
	t.Helper()
	if err := e.playMoviePaused(m); err != nil {
		t.Fatalf("再生を始められない: %v", err)
	}
}

// playFilePaused はファイルのムービーを PlayMovieFilePaused で再生する。
func playFilePaused(t *testing.T, e *Emulator, path string) {
	t.Helper()
	if err := e.PlayMovieFilePaused(path); err != nil {
		t.Fatalf("再生を始められない: %v", err)
	}
}

// recordSession は入力を変えながらムービーを記録し、最後の状態の
// ハッシュとムービーのパスを返す。
func recordSession(t *testing.T, e *Emulator, path string) [8]uint8 {
	t.Helper()
	if err := e.StartRecordingMovie(path); err != nil {
		t.Fatalf("記録を始められない: %v", err)
	}
	// フレームごとに押すボタンを変える。入力が結果へ反映されることを
	// 確かめられるようにするためである。
	for i, buttons := range []uint8{0, input.ButtonA, input.ButtonRight, 0, input.ButtonStart} {
		e.Input.Set(0, buttons)
		runFrames(t, e, 6)
		if i == 2 {
			e.Input.Set(1, input.ButtonB)
		}
	}
	e.Input.Set(0, 0)
	e.Input.Set(1, 0)
	if err := e.StopRecordingMovie(); err != nil {
		t.Fatalf("記録を止められない: %v", err)
	}
	return hashOf(t, e)
}

// TestMovieRecordAndReplay は記録したムービーを再生すると同じ状態に
// なることを確かめる。
func TestMovieRecordAndReplay(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "test.movie")

	want := recordSession(t, e, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ムービーが書き出されていない: %v", err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatalf("ムービーを読めない: %v", err)
	}
	if m.Header.TotalFrames == 0 {
		t.Fatal("フレームが記録されていない")
	}

	// 再生する。再生の終わりで記録時と同じ状態になる。
	playFilePaused(t, e, path)
	runFrames(t, e, int(m.Header.TotalFrames)-1)

	if got := hashOf(t, e); got != want {
		t.Errorf("再生後のハッシュ = %x, 記録時 %x", got, want)
	}
	if err := e.DesyncError(); err != nil {
		t.Errorf("desync を検出した: %v", err)
	}

	// 終端まで進めると再生が止まり、通常の入力へ戻る。
	runFrames(t, e, 5)
	if e.Status().Movie.Playing {
		t.Error("ムービーの終わりで再生が止まらない")
	}
	e.Input.Set(0, input.ButtonA)
	runFrames(t, e, 2)
	var got uint8
	e.WithMachine(func(*nes.NES) { got = e.latch.get()[0] })
	if got != input.ButtonA {
		t.Errorf("再生の後の入力 = %#08b, 期待 %#08b", got, input.ButtonA)
	}
}

// TestMovieReplayIsRepeatable は同じムービーを 2 回再生して同じ結果に
// なることを確かめる。
//
// ホスト側の非決定性（map のたどり方・ゴルーチン・壁時計・グローバル
// 乱数）が混ざっていれば、2 回目の結果が変わる。
func TestMovieReplayIsRepeatable(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "test.movie")
	recordSession(t, e, path)

	var hashes [2][8]uint8
	for i := range hashes {
		playFilePaused(t, e, path)
		runFrames(t, e, 40)
		hashes[i] = hashOf(t, e)
	}
	if hashes[0] != hashes[1] {
		t.Errorf("2 回の再生で結果が違う（%x と %x）", hashes[0], hashes[1])
	}
}

// TestMovieDetectsDesync はチェックサムが合わないときに検出して停止する
// ことを確かめる。
func TestMovieDetectsDesync(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "test.movie")
	recordSession(t, e, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	// 途中のチェックサムを 1 つ壊す。
	broken := false
	for i := range m.Records {
		if m.Records[i].Kind == movie.KindChecksum && i > 2 {
			m.Records[i].Hash[0] ^= 0xFF
			broken = true
			break
		}
	}
	if !broken {
		t.Fatal("チェックサムが記録されていない")
	}

	playPaused(t, e, m)
	runFrames(t, e, int(m.Header.TotalFrames))

	err = e.DesyncError()
	if err == nil {
		t.Fatal("desync を検出しなかった")
	}
	var d *movie.DesyncError
	if !errors.As(err, &d) {
		t.Fatalf("種類が違う: %v", err)
	}
	if d.Frame == 0 {
		t.Error("フレーム番号が報告されない")
	}
	if e.Status().Movie.Playing {
		t.Error("stopOnDesync が true なのに再生が続いている")
	}
}

// TestMovieStopsOnStateLoad はセーブステートのロードで記録が止まる
// ことを確かめる（設計書 08 編 §8.7.5）。
func TestMovieStopsOnStateLoad(t *testing.T) {
	e := newPausedEmulator(t)
	runFrames(t, e, 10)
	if err := e.SaveSlot(0); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "test.movie")
	if err := e.StartRecordingMovie(path); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 5)
	if !e.Status().Movie.Recording {
		t.Fatal("記録中にならない")
	}

	if err := e.LoadSlot(0); err != nil {
		t.Fatal(err)
	}
	if e.Status().Movie.Recording {
		t.Error("ステートのロードで記録が止まらない")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("記録が書き出されていない: %v", err)
	}
}

// TestMovieRejectsOtherROM は別の ROM のムービーを断ることを確かめる。
func TestMovieRejectsOtherROM(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "test.movie")
	recordSession(t, e, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	m.Header.ROMHash[0] ^= 0xFF

	if err := e.PlayMovie(m); err == nil {
		t.Error("別の ROM のムービーを受け入れた")
	}
}

// TestRewindTruncatesMovie は巻き戻したときに以降のレコードが捨てられ、
// 再記録回数が増えることを確かめる。
func TestRewindTruncatesMovie(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "test.movie")
	if err := e.StartRecordingMovie(path); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 40)
	before := e.Status().Movie

	if err := e.Rewind(20); err != nil {
		t.Fatalf("巻き戻せない: %v", err)
	}
	after := e.Status().Movie
	if after.Frame >= before.Frame {
		t.Errorf("記録したフレーム数が減っていない（%d → %d）", before.Frame, after.Frame)
	}
	if after.Rerecords != before.Rerecords+1 {
		t.Errorf("再記録回数 = %d, 期待 %d", after.Rerecords, before.Rerecords+1)
	}
}
