package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
)

// videoDir は録画の保存先を返す。無ければ作る（設計書 11 編 §11.2）。
func (u *UI) videoDir() string {
	dir := u.emu.Dirs().VideoDir(u.cfg.Paths.VideoDir)
	// 作れないときもダイアログは開ける。保存先は利用者が選び直せる。
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// askVideoPath は録画の保存先を尋ねる。取り消したとき空を返す。
func (u *UI) askVideoPath() string {
	path, err := zenity.SelectFileSave(
		zenity.Title(i18n.T(i18n.DialogVideoSave)),
		zenity.ConfirmOverwrite(),
		zenity.Filename(filepath.Join(u.videoDir(), u.defaultSaveName(".mp4"))),
		zenity.FileFilter{Name: i18n.T(i18n.FilterMP4), Patterns: []string{"*.mp4"}},
	)
	if !u.dialogPath(path, err) {
		return ""
	}
	return path
}

// startRecordingVideo は保存先を尋ねて録画を始める（設計書 08 編 §8.8.4）。
func (u *UI) startRecordingVideo() {
	path := u.askVideoPath()
	if path == "" {
		return
	}
	// オーバースキャンは画面の表示と同じにする。
	if err := u.emu.StartRecordingVideo(path, u.pal, u.cfg.Video.RecordScale, u.screen.Overscan()); err != nil {
		u.showError(err)
		return
	}
	u.videoPath = path
	u.status.notify(i18n.T(i18n.StatusVideoStarted))
}

// stopRecordingVideo は録画を止める。
func (u *UI) stopRecordingVideo() {
	if err := u.emu.StopRecordingVideo(); err != nil {
		u.videoPath = ""
		u.showError(err)
		return
	}
	u.noteVideoClosed(u.emu.Status())
}

// noteVideoClosed は録画が閉じていれば保存したことを知らせる。録画の停止と、
// ROM を閉じた・開き直したことで録画が閉じたとき（設計書 08 編 §8.8.4）に呼ぶ。
// 録画を始めていないとき・録画中のときは何もしない。
func (u *UI) noteVideoClosed(s emu.Status) {
	if u.videoPath == "" || s.Video.Recording {
		return
	}
	u.status.notify(i18n.T(i18n.StatusVideoSaved, filepath.Base(u.videoPath)))
	u.videoPath = ""
}

// videoStopped は refresh が録画の終わりに気付いたときに呼ぶ。録画を始めて
// いて、録画中でなくなっていればパスを忘れる。書き込みの失敗で止まっていた
// ときは、出すエラーを返す（設計書 08 編 §8.8.3）。それ以外は nil を返す。
func (u *UI) videoStopped(s emu.Status) error {
	if u.videoPath == "" || s.Video.Recording {
		return nil
	}
	u.videoPath = ""
	if s.Video.Error == "" {
		return nil
	}
	// エミュレータが覚えているエラーを受け取って消す。消さないと、次に
	// 録画の停止を選んだときに同じエラーをもう一度出してしまう。
	if err := u.emu.StopRecordingVideo(); err != nil {
		return err
	}
	return errors.New(s.Video.Error)
}

// exportVideo は操作の記録を選び、動画を書き出す。進み具合をダイアログに出し、
// 取り消せる（設計書 08 編 §8.8.4）。
func (u *UI) exportVideo() {
	rom := u.agentUI.romPath
	if !u.emu.Status().Loaded || rom == "" {
		u.showError(errors.New(i18n.T(i18n.VideoNeedROM)))
		return
	}
	src, err := zenity.SelectFile(
		zenity.Title(i18n.T(i18n.DialogVideoExportSource)),
		zenity.FileFilter{Name: i18n.T(i18n.SetPathMovie), Patterns: []string{"*.movie", "*.shgm"}},
	)
	if !u.dialogPath(src, err) {
		return
	}
	out := u.askVideoPath()
	if out == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	end := u.exports.begin(cancel)
	bar := widget.NewProgressBar()
	label := widget.NewLabel("")
	d := dialog.NewCustom(i18n.T(i18n.VideoExportTitle), i18n.T(i18n.CommonCancel),
		container.NewVBox(label, bar), u.win)
	// 取り消しのボタンでも、終わって閉じたときも呼ばれる。終わった後の
	// 取り消しは何もしない。
	d.SetOnClosed(cancel)
	d.Show()

	x := emu.VideoExport{
		ROMPath: rom, MoviePath: src, OutPath: out, Palette: u.pal,
		Scale: u.cfg.Video.RecordScale, Overscan: u.screen.Overscan(),
		Progress: func(done, total uint64) {
			Post(func() {
				// 終了処理が始まった後は呼び出し元のゴルーチンで走るため、
				// 部品に触らない（dispatch.go）。
				if u.closing.Load() {
					return
				}
				bar.SetValue(float64(done) / float64(max(total, 1)))
				label.SetText(i18n.T(i18n.VideoExportProgress, done, total))
			})
		},
	}
	go func() {
		err := u.emu.ExportVideo(ctx, x)
		// 終了を待つ側（アプリの終了）へ先に知らせる。UI スレッドへ渡す処理は
		// 終了を待つ間は走らないため、これより後に置く。
		end()
		Post(func() {
			if u.closing.Load() {
				return
			}
			// ダイアログを閉じると ctx が取り消されるため、閉じる前に確かめる。
			canceled := ctx.Err() != nil
			d.Hide()
			msg, fail := exportOutcome(err, canceled, out)
			if fail != nil {
				u.showError(fail)
				return
			}
			if msg != "" {
				u.status.notify(msg)
			}
		})
	}()
}

// exportTracker は実行中の書き出しを覚える。アプリの終了で取り消し、
// 書き出しが書きかけのファイルと一時ディレクトリを消し終えるまで待つ。
// 書き出しは同時に複数走りうる（macOS ではダイアログの間もメニューを選べる）。
type exportTracker struct {
	mu     sync.Mutex
	next   int
	cancel map[int]context.CancelFunc
	wg     sync.WaitGroup
}

// begin は書き出しを覚える。返す関数は書き出しが終わったときに 1 度だけ呼ぶ。
func (t *exportTracker) begin(cancel context.CancelFunc) (end func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel == nil {
		t.cancel = map[int]context.CancelFunc{}
	}
	id := t.next
	t.next++
	t.cancel[id] = cancel
	t.wg.Add(1)
	return func() {
		t.mu.Lock()
		delete(t.cancel, id)
		t.mu.Unlock()
		t.wg.Done()
	}
}

// cancelAndWait は実行中の書き出しをすべて取り消し、終わるまで待つ。
func (t *exportTracker) cancelAndWait() {
	t.mu.Lock()
	for _, c := range t.cancel {
		c()
	}
	t.mu.Unlock()
	t.wg.Wait()
}

// exportOutcome は書き出しの結果から、知らせる文言と出すエラーを決める。
// 利用者が取り消したときはどちらも出さない。書き終えていれば、閉じる操作と
// 行き違いになっても成功として知らせる。
func exportOutcome(err error, canceled bool, out string) (string, error) {
	switch {
	case err == nil:
		return i18n.T(i18n.VideoExportDone, filepath.Base(out)), nil
	case canceled || errors.Is(err, context.Canceled):
		return "", nil
	}
	return "", err
}
