package testrom

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeMachine は $6000 以降の領域だけを持つ模擬のエミュレータ。
//
// 実装が進む前からランナーの論理を確かめるために使う。
type fakeMachine struct {
	mem map[uint16]uint8
	// script はフレームごとに実行する変更。
	script  []func(f *fakeMachine)
	frame   int
	resets  int
	peeks   int
	onReset func(f *fakeMachine)
}

func newFakeMachine() *fakeMachine {
	return &fakeMachine{mem: map[uint16]uint8{}}
}

func (f *fakeMachine) RunFrame() {
	if f.frame < len(f.script) {
		if fn := f.script[f.frame]; fn != nil {
			fn(f)
		}
	}
	f.frame++
}

func (f *fakeMachine) Reset() {
	f.resets++
	if f.onReset != nil {
		f.onReset(f)
	}
}

func (f *fakeMachine) Peek(addr uint16) uint8 {
	f.peeks++
	return f.mem[addr]
}

// writeSignature はシグネチャを書き込む。
func (f *fakeMachine) writeSignature() {
	for i, v := range signature {
		f.mem[signatureAddr+uint16(i)] = v
	}
}

// writeMessage は結果の文字列を書き込む。
func (f *fakeMachine) writeMessage(s string) {
	for i := range len(s) {
		f.mem[messageAddr+uint16(i)] = s[i]
	}
	f.mem[messageAddr+uint16(len(s))] = 0
}

// at はフレーム n で fn を実行するように仕込む。
func (f *fakeMachine) at(n int, fn func(*fakeMachine)) {
	for len(f.script) <= n {
		f.script = append(f.script, nil)
	}
	f.script[n] = fn
}

// TestRunBlarggPass は合格を判定できることを確かめる。
func TestRunBlarggPass(t *testing.T) {
	m := newFakeMachine()
	m.at(0, func(f *fakeMachine) {
		f.writeSignature()
		f.mem[statusAddr] = statusRunning
	})
	m.at(5, func(f *fakeMachine) {
		f.mem[statusAddr] = 0
		f.writeMessage("Passed")
	})

	res, err := RunBlargg(m, 100)
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if !res.OK() {
		t.Errorf("OK() = false, 期待 true（%s）", res)
	}
	if res.Code != 0 {
		t.Errorf("Code = %d, 期待 0", res.Code)
	}
	if res.Message != "Passed" {
		t.Errorf("Message = %q, 期待 \"Passed\"", res.Message)
	}
	if res.Frames != 6 {
		t.Errorf("Frames = %d, 期待 6", res.Frames)
	}
}

// TestRunBlarggFail は不合格の結果とメッセージを返すことを確かめる。
func TestRunBlarggFail(t *testing.T) {
	m := newFakeMachine()
	m.at(0, func(f *fakeMachine) {
		f.writeSignature()
		f.mem[statusAddr] = statusRunning
	})
	m.at(2, func(f *fakeMachine) {
		f.mem[statusAddr] = 3
		f.writeMessage("3\n\nLDA abs,X failed")
	})

	res, err := RunBlargg(m, 100)
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if res.OK() {
		t.Error("OK() = true, 期待 false")
	}
	if res.Code != 3 {
		t.Errorf("Code = %d, 期待 3", res.Code)
	}
	if res.Message != "3\n\nLDA abs,X failed" {
		t.Errorf("Message = %q", res.Message)
	}
}

// TestRunBlarggIgnoresGarbageBeforeSignature はシグネチャが書かれる前の値を
// 結果として読まないことを確かめる。電源投入直後の未初期化の値を拾わない。
func TestRunBlarggIgnoresGarbageBeforeSignature(t *testing.T) {
	m := newFakeMachine()
	// シグネチャが無いまま $6000 に 0 以外が入っている
	m.mem[statusAddr] = 0x42
	m.at(3, func(f *fakeMachine) {
		f.writeSignature()
		f.mem[statusAddr] = statusRunning
	})
	m.at(6, func(f *fakeMachine) { f.mem[statusAddr] = 0 })

	res, err := RunBlargg(m, 100)
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if res.Code != 0 {
		t.Errorf("Code = %d, 期待 0（シグネチャ前の値を読んでいる）", res.Code)
	}
}

// TestRunBlarggReset は $81 でリセットを掛けることを確かめる。
func TestRunBlarggReset(t *testing.T) {
	m := newFakeMachine()
	m.at(0, func(f *fakeMachine) {
		f.writeSignature()
		f.mem[statusAddr] = statusNeedsReset
	})
	m.onReset = func(f *fakeMachine) { f.mem[statusAddr] = statusRunning }
	// リセット後しばらくして合格する
	m.at(20, func(f *fakeMachine) { f.mem[statusAddr] = 0 })

	res, err := RunBlargg(m, 100)
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if res.Resets != 1 {
		t.Errorf("Resets = %d, 期待 1", res.Resets)
	}
	if m.resets != 1 {
		t.Errorf("Reset の呼び出し回数 = %d, 期待 1", m.resets)
	}
	if res.Code != 0 {
		t.Errorf("Code = %d, 期待 0", res.Code)
	}
}

// TestRunBlarggTimeout は結果が出ないとき ErrTimeout を返すことを確かめる。
func TestRunBlarggTimeout(t *testing.T) {
	m := newFakeMachine()
	m.at(0, func(f *fakeMachine) {
		f.writeSignature()
		f.mem[statusAddr] = statusRunning
	})

	res, err := RunBlargg(m, 30)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, 期待 ErrTimeout", err)
	}
	if res.Frames != 30 {
		t.Errorf("Frames = %d, 期待 30", res.Frames)
	}
}

// TestRunBlarggNoSignatureTimeout はシグネチャが現れないときもタイムアウト
// することを確かめる。
func TestRunBlarggNoSignatureTimeout(t *testing.T) {
	m := newFakeMachine()
	if _, err := RunBlargg(m, 10); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, 期待 ErrTimeout", err)
	}
}

// TestReadMessageStopsAtLimit はゼロ終端が無いとき上限で止まることを確かめる。
func TestReadMessageStopsAtLimit(t *testing.T) {
	m := newFakeMachine()
	for i := range maxMessageLen + 100 {
		m.mem[messageAddr+uint16(i)] = 'A'
	}
	if got := len(readMessage(m)); got != maxMessageLen {
		t.Errorf("長さ = %d, 期待 %d", got, maxMessageLen)
	}
}

// TestResultString は結果の表示を確かめる。
func TestResultString(t *testing.T) {
	if got := (Result{Code: 0, Frames: 10}).String(); got != "合格（10 フレーム）" {
		t.Errorf("String() = %q", got)
	}
	if got := (Result{Code: 2, Frames: 5}).String(); got != "コード 2（5 フレーム）" {
		t.Errorf("String() = %q", got)
	}
	if got := (Result{Code: 2, Message: "x", Frames: 5}).String(); got != "コード 2: x（5 フレーム）" {
		t.Errorf("String() = %q", got)
	}
}

// TestLoadExpectations は期待値の表を読めることを確かめる。
func TestLoadExpectations(t *testing.T) {
	path := filepath.Join(RepoRoot(t), expectationsFile)
	m, err := LoadExpectations(path)
	if err != nil {
		t.Fatalf("%s を読めない: %v", expectationsFile, err)
	}
	if len(m) == 0 {
		t.Fatal("期待値が 1 件も無い")
	}
	for name, e := range m {
		if e.Timeout <= 0 {
			t.Errorf("%s: timeout = %d", name, e.Timeout)
		}
	}
}

// TestLoadExpectationsRejectsZeroTimeout は timeout が 0 の項目を拒むことを
// 確かめる。0 だと 1 フレームも進めずに必ずタイムアウトする。
func TestLoadExpectationsRejectsZeroTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"a.nes":{"expect":0,"timeout":0}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExpectations(path); err == nil {
		t.Error("timeout 0 を受け入れてしまった")
	}
}

// TestRequireROMSkipsWhenAbsent は ROM が無いときテストを飛ばすことを確かめる。
func TestRequireROMSkipsWhenAbsent(t *testing.T) {
	var skipped bool
	fake := &recordingTB{TB: t, onSkip: func() { skipped = true }}
	RequireROM(fake, "存在しない/rom.nes")
	if !skipped {
		t.Error("Skipf が呼ばれなかった")
	}
}

// recordingTB は Skipf の呼び出しを記録する testing.TB。
//
// Skipf を横取りするのは、飛ばす動作そのものをテストするためである。
type recordingTB struct {
	testing.TB
	onSkip func()
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Skipf(format string, args ...any) {
	r.onSkip()
}

// TestRepoRootHasGoMod はリポジトリのルートを見つけられることを確かめる。
func TestRepoRootHasGoMod(t *testing.T) {
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("%s に go.mod が無い: %v", root, err)
	}
}
