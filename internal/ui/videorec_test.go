package ui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
)

// TestExportOutcome は書き出しの結果の扱いを確かめる。取り消したときは
// エラーを出さず、終わった後の取り消し（ダイアログを閉じたこと）は成功のままにする。
func TestExportOutcome(t *testing.T) {
	fail := errors.New("書けない")
	cases := []struct {
		name     string
		err      error
		canceled bool
		wantMsg  string
		wantErr  error
	}{
		{"成功", nil, false, i18n.T(i18n.VideoExportDone, "out.mp4"), nil},
		{"終わった後に閉じた", nil, true, i18n.T(i18n.VideoExportDone, "out.mp4"), nil},
		{"取り消した", context.Canceled, true, "", nil},
		{"失敗", fail, false, "", fail},
	}
	for _, c := range cases {
		msg, err := exportOutcome(c.err, c.canceled, "/tmp/out.mp4")
		if msg != c.wantMsg || !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
			t.Errorf("%s: (%q, %v), 期待 (%q, %v)", c.name, msg, err, c.wantMsg, c.wantErr)
		}
	}
}

// TestNoteVideoClosedClearsStalePath は ROM を閉じるなどで録画が閉じたとき、
// 保存を知らせて録画中のパスを消すことを確かめる。
func TestNoteVideoClosedClearsStalePath(t *testing.T) {
	u := newTestUI(t)
	u.videoPath = "/movies/game.mp4"

	// 録画中のときは何もしない。
	u.noteVideoClosed(emu.Status{Video: emu.VideoStatus{Recording: true}})
	if u.videoPath == "" {
		t.Fatal("録画中なのにパスを消した")
	}

	u.noteVideoClosed(emu.Status{})
	if u.videoPath != "" {
		t.Errorf("録画を閉じた後もパスが残っている: %q", u.videoPath)
	}
	if !strings.Contains(u.status.message, "game.mp4") {
		t.Errorf("保存の知らせが無い: %q", u.status.message)
	}

	// 2 度目は知らせない。
	u.status.message = ""
	u.noteVideoClosed(emu.Status{})
	if u.status.message != "" {
		t.Errorf("録画していないのに知らせた: %q", u.status.message)
	}
}

// TestVideoStoppedByFailure は書き込みの失敗で録画が止まったことに refresh が
// 気付いたとき、エラーを出すこと、失敗でなければ黙ってパスを忘れることを
// 確かめる（設計書 08 編 §8.8.3）。
func TestVideoStoppedByFailure(t *testing.T) {
	u := newTestUI(t)

	// 録画していないとき・録画中のときは何もしない。
	if err := u.videoStopped(emu.Status{Video: emu.VideoStatus{Error: "書けない"}}); err != nil {
		t.Errorf("録画を始めていないのにエラーを返した: %v", err)
	}
	u.videoPath = "/movies/game.mp4"
	if err := u.videoStopped(emu.Status{Video: emu.VideoStatus{Recording: true}}); err != nil || u.videoPath == "" {
		t.Fatalf("録画中なのに (%v, %q)", err, u.videoPath)
	}

	err := u.videoStopped(emu.Status{Video: emu.VideoStatus{Error: "書けない"}})
	if err == nil || !strings.Contains(err.Error(), "書けない") {
		t.Errorf("途中の失敗のエラー = %v", err)
	}
	if u.videoPath != "" {
		t.Errorf("パスが残っている: %q", u.videoPath)
	}

	// 失敗でなく止まったときは黙ってパスを忘れる。
	u.videoPath = "/movies/game.mp4"
	if err := u.videoStopped(emu.Status{}); err != nil {
		t.Errorf("失敗していないのにエラーを返した: %v", err)
	}
	if u.videoPath != "" {
		t.Errorf("パスが残っている: %q", u.videoPath)
	}
}

// TestExportTrackerCancelsAndWaits はアプリの終了で、実行中の書き出しを
// 取り消し、終わるまで待つことを確かめる。待たないと書きかけのファイルが残る。
func TestExportTrackerCancelsAndWaits(t *testing.T) {
	var tr exportTracker
	var finished atomic.Bool
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		end := tr.begin(cancel)
		go func() {
			<-ctx.Done()
			// 書きかけのファイルを消す処理に見立てて少し待つ。
			time.Sleep(20 * time.Millisecond)
			finished.Store(true)
			end()
		}()
	}
	tr.cancelAndWait()
	if !finished.Load() {
		t.Error("書き出しが終わる前に戻った")
	}
	// 書き出しが無いときはすぐ戻る。
	tr.cancelAndWait()
}

// TestExportTrackerForgetsFinished は終わった書き出しを取り消さないことを確かめる。
func TestExportTrackerForgetsFinished(t *testing.T) {
	var tr exportTracker
	called := false
	end := tr.begin(func() { called = true })
	end()
	tr.cancelAndWait()
	if called {
		t.Error("終わった書き出しを取り消した")
	}
}
