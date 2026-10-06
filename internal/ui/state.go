package ui

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"image/png"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// stateMenu はステートメニューを作る。
func (u *UI) stateMenu() *fyne.Menu {
	save := fyne.NewMenuItem(i18n.T(i18n.MenuQuickSave), u.quickSave)
	load := fyne.NewMenuItem(i18n.T(i18n.MenuQuickLoad), u.quickLoad)

	slots := make([]*fyne.MenuItem, 0, emu.SlotCount)
	for i := range emu.SlotCount {
		slots = append(slots, fyne.NewMenuItem(i18n.T(i18n.MenuSlotN, i), func() {
			u.emu.SetSlot(i)
			u.status.notify(i18n.T(i18n.StatusSlotSelected, i))
		}))
	}
	slot := fyne.NewMenuItem(i18n.T(i18n.MenuSelectSlot), nil)
	slot.ChildMenu = fyne.NewMenu("", slots...)

	list := fyne.NewMenuItem(i18n.T(i18n.MenuSlotList), u.showSlotList)
	saveAs := fyne.NewMenuItem(i18n.T(i18n.MenuSaveStateAs), u.saveStateAs)
	loadFrom := fyne.NewMenuItem(i18n.T(i18n.MenuLoadStateFrom), u.loadStateFrom)

	return fyne.NewMenu(i18n.T(i18n.SetTabState), save, load, fyne.NewMenuItemSeparator(),
		slot, list, fyne.NewMenuItemSeparator(), saveAs, loadFrom)
}

// movieMenu はムービーメニューを作る。
func (u *UI) movieMenu() *fyne.Menu {
	record := fyne.NewMenuItem(i18n.T(i18n.MenuStartRecording), u.startRecordingMovie)
	stopRecord := fyne.NewMenuItem(i18n.T(i18n.MenuStopRecording), func() {
		u.showError(u.emu.StopRecordingMovie())
	})
	play := fyne.NewMenuItem(i18n.T(i18n.MenuPlayMovie), u.playMovie)
	stop := fyne.NewMenuItem(i18n.T(i18n.MenuStop), func() { u.showError(u.emu.StopMovie()) })

	return fyne.NewMenu(i18n.T(i18n.MenuMovie), record, stopRecord, fyne.NewMenuItemSeparator(), play, stop)
}

// quickSave は選択中のスロットへ保存する。
func (u *UI) quickSave() {
	n := u.emu.Slot()
	if err := u.emu.SaveSlot(n); err != nil {
		u.showError(err)
		return
	}
	u.status.notify(i18n.T(i18n.StatusSavedSlot, n))
}

// quickLoad は選択中のスロットから復元する。
func (u *UI) quickLoad() {
	n := u.emu.Slot()
	if err := u.emu.LoadSlot(n); err != nil {
		u.showError(err)
		return
	}
	u.status.notify(i18n.T(i18n.StatusLoadedSlot, n))
}

// saveStateAs は名前を付けてステートを保存する。
func (u *UI) saveStateAs() {
	path, err := zenity.SelectFileSave(
		zenity.Title(i18n.T(i18n.DialogSaveState)),
		zenity.ConfirmOverwrite(),
		zenity.Filename(u.defaultStateName()),
		zenity.FileFilter{Name: i18n.T(i18n.SetPathState), Patterns: []string{"*.state"}},
	)
	if !u.dialogPath(path, err) {
		return
	}
	u.showError(u.emu.SaveToFile(path))
}

// loadStateFrom はファイルからステートを読み込む。
func (u *UI) loadStateFrom() {
	path, err := zenity.SelectFile(
		zenity.Title(i18n.T(i18n.DialogLoadState)),
		zenity.FileFilter{Name: i18n.T(i18n.SetPathState), Patterns: []string{"*.state"}},
	)
	if !u.dialogPath(path, err) {
		return
	}
	u.showError(u.emu.LoadFromFile(path))
}

// startRecordingMovie は書き出し先を尋ねてムービーの記録を始める。
func (u *UI) startRecordingMovie() {
	path, err := zenity.SelectFileSave(
		zenity.Title(i18n.T(i18n.DialogRecordMovie)),
		zenity.ConfirmOverwrite(),
		zenity.Filename(u.defaultMovieName()),
		zenity.FileFilter{Name: i18n.T(i18n.SetPathMovie), Patterns: []string{"*.movie"}},
	)
	if !u.dialogPath(path, err) {
		return
	}
	if err := u.emu.StartRecordingMovie(path); err != nil {
		u.showError(err)
		return
	}
	u.status.notify(i18n.T(i18n.StatusRecordingStarted))
}

// playMovie はファイルを選んでムービーを再生する。
func (u *UI) playMovie() {
	path, err := zenity.SelectFile(
		zenity.Title(i18n.T(i18n.DialogPlayMovie)),
		// Repro の repro.shgm も選べるようにする（設計書 14 編 §14.16.3）。
		zenity.FileFilter{Name: i18n.T(i18n.SetPathMovie), Patterns: []string{"*.movie", "*.shgm"}},
	)
	if !u.dialogPath(path, err) {
		return
	}
	if err := u.emu.PlayMovieFile(path); err != nil {
		u.showError(err)
		return
	}
	u.paused = false
	u.status.notify(i18n.T(i18n.StatusPlaybackStarted))
}

// dialogPath はファイル選択の結果を判定する。選ばれたとき true を返す。
func (u *UI) dialogPath(path string, err error) bool {
	if err != nil {
		// 取り消しはエラーにしない。
		if !isCanceled(err) {
			u.showError(err)
		}
		return false
	}
	return path != ""
}

// defaultStateName は名前を付けて保存するときの既定のファイル名を返す。
func (u *UI) defaultStateName() string {
	return u.defaultSaveName(".state")
}

// defaultMovieName はムービーの既定のファイル名を返す。
func (u *UI) defaultMovieName() string {
	return u.defaultSaveName(".movie")
}

// defaultSaveName は ROM 名と日時からファイル名を作る。
func (u *UI) defaultSaveName(ext string) string {
	name := u.emu.Status().ROMName
	if name == "" {
		name = "shogun"
	}
	return fmt.Sprintf("%s-%s%s", name, time.Now().Format("20060102-150405"), ext)
}

// showSlotList はスロットの一覧をダイアログで表示する。
func (u *UI) showSlotList() {
	infos, err := u.emu.SlotInfos()
	if err != nil {
		u.showError(err)
		return
	}
	rows := make([]fyne.CanvasObject, 0, len(infos))
	for _, info := range infos {
		rows = append(rows, u.slotRow(info))
	}
	content := container.NewVBox(rows...)
	d := dialog.NewCustom(i18n.T(i18n.DialogSlotList), i18n.T(i18n.CommonClose), container.NewVScroll(content), u.win)
	d.Resize(fyne.NewSize(slotDialogWidth, slotDialogHeight))
	d.Show()
}

// スロット一覧のダイアログとサムネイルの大きさ。
const (
	slotDialogWidth  = 420
	slotDialogHeight = 480
	slotThumbWidth   = 96
	slotThumbHeight  = 90
)

// slotRow はスロット 1 行分の表示を作る。
func (u *UI) slotRow(info emu.SlotInfo) fyne.CanvasObject {
	text := i18n.T(i18n.SlotEmpty, info.Index)
	switch {
	case info.Err != nil:
		text = i18n.T(i18n.SlotUnreadable, info.Index, info.Err)
	case info.Exists:
		text = i18n.T(i18n.SlotInfo,
			info.Index, info.SavedAt.Format("2006-01-02 15:04:05"), info.Frames)
	}
	label := widget.NewLabel(text)

	thumb := u.slotThumbnail(info)
	if thumb == nil {
		return container.NewHBox(label)
	}
	return container.NewHBox(thumb, label)
}

// slotThumbnail はスロットのスクリーンショットを表示する部品を作る。
func (u *UI) slotThumbnail(info emu.SlotInfo) fyne.CanvasObject {
	if len(info.Screenshot) == 0 {
		return nil
	}
	img, err := png.Decode(newByteReader(info.Screenshot))
	if err != nil {
		return nil
	}
	c := canvas.NewImageFromImage(img)
	c.FillMode = canvas.ImageFillContain
	c.SetMinSize(fyne.NewSize(slotThumbWidth, slotThumbHeight))
	return c
}

// isCanceled はファイル選択が取り消されたかを返す。
func isCanceled(err error) bool { return errors.Is(err, zenity.ErrCanceled) }

// newByteReader はバイト列を読む Reader を返す。
func newByteReader(b []uint8) *bytes.Reader { return bytes.NewReader(b) }
