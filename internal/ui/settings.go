package ui

import (
	"errors"
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// 設定画面のタブの番号。
const (
	settingsTabEmulation = iota
	settingsTabVideo
	settingsTabAudio
	settingsTabInput
	settingsTabPaths
	settingsTabDebug
	settingsTabState
	settingsTabAppearance
)

// 反映の時期（設計書 11 編 §11.3.2）。
var (
	timingNow    = i18n.T(i18n.SetTimingNow)
	timingReload = i18n.T(i18n.SetTimingReload)
	timingMixed  = i18n.T(i18n.SetTimingMixed)
)

// settingsForm は設定画面（設計書 10 編 §10.8）。
//
// 保存する設定の写し edit を編集し、「保存」で保存する設定へ入れる。
// 「取り消し」では写しを捨てる。
type settingsForm struct {
	u    *UI
	edit *config.Config
	keys *config.Keybindings
	win  fyne.Window
	tabs *container.AppTabs
	// refreshers は写しの値を部品へ読み直す。既定値に戻したときに使う。
	refreshers []func()
	keyEditor  *keyEditor
}

// openSettings は設定画面を開く。開いているときは前面に出す。
func (u *UI) openSettings(tab int) {
	if u.settingsWin != nil {
		u.settingsWin.RequestFocus()
		return
	}
	f := u.newSettingsForm()
	f.win = u.app.NewWindow(i18n.T(i18n.MenuSettings))
	u.shareMainMenu(f.win)
	f.win.SetContent(f.content())
	f.tabs.SelectIndex(tab)
	f.win.Resize(fyne.NewSize(720, 640))
	f.installKeyCapture()
	f.win.SetOnClosed(func() {
		u.settingsWin = nil
		u.keyCapture = false
	})
	u.settingsWin = f.win
	f.win.Show()
}

// newSettingsForm は保存する設定とキーバインドの写しを作る。
func (u *UI) newSettingsForm() *settingsForm {
	f := &settingsForm{u: u}
	if u.store != nil {
		f.edit = u.store.Base()
		f.keys = u.store.Keys().Clone()
	} else {
		f.edit = u.cfg.Clone()
		f.keys = config.DefaultKeybindings()
	}
	return f
}

// content はタブとボタンを組み立てる。
func (f *settingsForm) content() fyne.CanvasObject {
	f.tabs = container.NewAppTabs(
		container.NewTabItem(i18n.T(i18n.SetTabEmulation), f.page(timingReload, f.emulationItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabVideo), f.page(timingNow, f.videoItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabAudio), f.page(timingMixed, f.audioItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabInput), f.page(timingMixed, f.inputItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabPaths), f.page(timingReload, f.pathItems()...)),
		container.NewTabItem(i18n.T(i18n.MenuDebug), f.page(timingMixed, f.debugItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabState), f.page(timingReload, f.stateItems()...)),
		container.NewTabItem(i18n.T(i18n.SetTabAppearance), f.page(timingNow, f.appearanceItems()...)),
	)
	defaults := widget.NewButton(i18n.T(i18n.SetResetDefaults), f.resetDefaults)
	openDir := widget.NewButton(i18n.T(i18n.SetOpenConfigDir), f.openConfigDir)
	cancel := widget.NewButton(i18n.T(i18n.CommonCancel), func() { f.close() })
	save := widget.NewButtonWithIcon(i18n.T(i18n.CommonSave), nil, f.save)
	save.Importance = widget.HighImportance
	buttons := container.NewHBox(defaults, openDir, layout.NewSpacer(), cancel, save)
	return container.NewBorder(nil, buttons, nil, nil, f.tabs)
}

// page は 1 つのタブの中身を作る。先頭に反映の時期を置く。
func (f *settingsForm) page(timing string, items ...fyne.CanvasObject) fyne.CanvasObject {
	head := widget.NewLabel(timing)
	head.Importance = widget.WarningImportance
	box := container.NewVBox(head, widget.NewSeparator())
	for _, it := range items {
		box.Add(it)
	}
	return container.NewVScroll(box)
}

// item は 1 項目を、名前・部品・説明・上書きの表示の順に並べる。
func (f *settingsForm) item(label, desc string, control fyne.CanvasObject, pick func(c *config.Config) any) fyne.CanvasObject {
	name := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	box := container.NewVBox(name, control)
	if desc != "" {
		d := widget.NewLabel(desc)
		d.Wrapping = fyne.TextWrapWord
		d.Importance = widget.LowImportance
		box.Add(d)
	}
	if note := f.overrideNote(pick); note != "" {
		n := widget.NewLabel(note)
		n.Importance = widget.DangerImportance
		n.Wrapping = fyne.TextWrapWord
		box.Add(n)
	}
	return box
}

// overrideNote は環境変数と引数で上書きされている項目に添える説明を返す。
func (f *settingsForm) overrideNote(pick func(c *config.Config) any) string {
	if f.u.store == nil || pick == nil {
		return ""
	}
	by := f.u.store.OverriddenBy(pick)
	if len(by) == 0 {
		return ""
	}
	return i18n.T(i18n.SetOverriddenNote,
		strings.Join(by, "・"), pick(f.u.cfg))
}

// choiceOpt は選択肢の値と表示。
type choiceOpt struct{ value, label string }

// choice は選択肢から選ぶ項目を作る。
func (f *settingsForm) choice(label, desc string, opts []choiceOpt, get func() string, set func(string), pick func(*config.Config) any) fyne.CanvasObject {
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.label
	}
	sel := widget.NewSelect(labels, func(l string) {
		for _, o := range opts {
			if o.label == l {
				set(o.value)
			}
		}
	})
	read := func() {
		for i, o := range opts {
			if o.value == get() {
				sel.SetSelectedIndex(i)
			}
		}
	}
	read()
	f.refreshers = append(f.refreshers, read)
	return f.item(label, desc, sel, pick)
}

// check はチェックボックスの項目を作る。
func (f *settingsForm) check(label, desc string, get func() bool, set func(bool), pick func(*config.Config) any) fyne.CanvasObject {
	c := widget.NewCheck(label, set)
	read := func() { c.SetChecked(get()) }
	read()
	f.refreshers = append(f.refreshers, read)
	return f.item(label, desc, c, pick)
}

// intField は整数を入力する項目を作る。範囲外の値は受け付けず、直前の値を保つ。
func (f *settingsForm) intField(label, desc string, lo, hi int, get func() int, set func(int), pick func(*config.Config) any) fyne.CanvasObject {
	e := widget.NewEntry()
	e.Validator = func(s string) error {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < lo || n > hi {
			return errors.New(i18n.T(i18n.SetIntRange, lo, hi))
		}
		return nil
	}
	e.OnChanged = func(s string) {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= lo && n <= hi {
			set(n)
		}
	}
	read := func() { e.SetText(strconv.Itoa(get())) }
	read()
	f.refreshers = append(f.refreshers, read)
	return f.item(label, i18n.T(i18n.SetWithRange, desc, lo, hi), e, pick)
}

// volumeField は 0 から 1 の音量を選ぶ項目を作る。
func (f *settingsForm) volumeField(label, desc string, get func() float64, set func(float64), pick func(*config.Config) any) fyne.CanvasObject {
	value := widget.NewLabel("")
	s := widget.NewSlider(0, 100)
	s.Step = 5
	s.OnChanged = func(v float64) {
		set(v / 100)
		value.SetText(fmt.Sprintf("%.0f%%", v))
	}
	read := func() {
		s.SetValue(get() * 100)
		value.SetText(fmt.Sprintf("%.0f%%", get()*100))
	}
	read()
	f.refreshers = append(f.refreshers, read)
	return f.item(label, desc, container.NewBorder(nil, nil, nil, value, s), pick)
}

// pathField はディレクトリまたはファイルを選ぶ項目を作る。空欄は既定の場所を表す。
func (f *settingsForm) pathField(label, desc string, dir bool, get func() string, set func(string), pick func(*config.Config) any) fyne.CanvasObject {
	e := widget.NewEntry()
	e.SetPlaceHolder(i18n.T(i18n.SetDefaultLocation))
	e.OnChanged = func(s string) { set(strings.TrimSpace(s)) }
	browse := widget.NewButton(i18n.T(i18n.SetBrowse), func() {
		opts := []zenity.Option{zenity.Title(label)}
		if dir {
			opts = append(opts, zenity.Directory())
		} else {
			opts = append(opts, zenity.FileFilters{{Name: i18n.T(i18n.FilterPalette), Patterns: []string{"*.pal"}, CaseFold: true}})
		}
		path, err := zenity.SelectFile(opts...)
		if isCanceled(err) {
			return
		}
		if err != nil {
			f.u.showError(err)
			return
		}
		e.SetText(path)
	})
	clear := widget.NewButton(i18n.T(i18n.SetUseDefault), func() { e.SetText("") })
	read := func() { e.SetText(get()) }
	read()
	f.refreshers = append(f.refreshers, read)
	return f.item(label, desc, container.NewBorder(nil, nil, nil, container.NewHBox(browse, clear), e), pick)
}

func (f *settingsForm) emulationItems() []fyne.CanvasObject {
	e := &f.edit.Emulation
	return []fyne.CanvasObject{
		f.choice(i18n.T(i18n.SetRegion), i18n.T(i18n.SetRegionDesc),
			[]choiceOpt{{"auto", i18n.T(i18n.SetAuto)}, {"ntsc", "NTSC"}, {"pal", "PAL"}, {"dendy", "Dendy"}},
			func() string { return e.Region }, func(v string) { e.Region = v },
			func(c *config.Config) any { return c.Emulation.Region }),
		f.choice(i18n.T(i18n.SetRAMInit), i18n.T(i18n.SetRAMInitDesc),
			[]choiceOpt{{"random", i18n.T(i18n.SetRAMRandom)}, {"zero", i18n.T(i18n.SetRAMZero)}, {"ff", i18n.T(i18n.SetRAMFF)}, {"pattern", i18n.T(i18n.SetRAMPattern)}},
			func() string { return e.RAMInitPattern }, func(v string) { e.RAMInitPattern = v },
			func(c *config.Config) any { return c.Emulation.RAMInitPattern }),
		f.item(i18n.T(i18n.SetRAMSeed), i18n.T(i18n.SetRAMSeedDesc), f.uintEntry(&e.RAMSeed),
			func(c *config.Config) any { return c.Emulation.RAMSeed }),
		f.intField(i18n.T(i18n.SetAlignment), i18n.T(i18n.SetAlignmentDesc), 0, 2,
			func() int { return e.CPUPPUAlignment }, func(v int) { e.CPUPPUAlignment = v },
			func(c *config.Config) any { return c.Emulation.CPUPPUAlignment }),
		f.intField(i18n.T(i18n.SetDMAPhase), i18n.T(i18n.SetDMAPhaseDesc), 0, 1,
			func() int { return e.DMAGetPutPhase }, func(v int) { e.DMAGetPutPhase = v },
			func(c *config.Config) any { return c.Emulation.DMAGetPutPhase }),
		f.check(i18n.T(i18n.SetVBlankFlag), i18n.T(i18n.SetVBlankFlagDesc),
			func() bool { return e.PPUVBlankFlag }, func(v bool) { e.PPUVBlankFlag = v },
			func(c *config.Config) any { return c.Emulation.PPUVBlankFlag }),
		f.choice(i18n.T(i18n.SetMMC3IRQ), i18n.T(i18n.SetMMC3IRQDesc),
			[]choiceOpt{{"sharp", i18n.T(i18n.SetMMC3Sharp)}, {"nec", i18n.T(i18n.SetMMC3NEC)}},
			func() string { return e.MMC3IRQVariant }, func(v string) { e.MMC3IRQVariant = v },
			func(c *config.Config) any { return c.Emulation.MMC3IRQVariant }),
		f.choice(i18n.T(i18n.SetBusConflicts), i18n.T(i18n.SetBusConflictsDesc),
			[]choiceOpt{{"auto", i18n.T(i18n.SetAuto)}, {"always", i18n.T(i18n.SetBusAlways)}, {"never", i18n.T(i18n.SetBusNever)}},
			func() string { return e.BusConflicts }, func(v string) { e.BusConflicts = v },
			func(c *config.Config) any { return c.Emulation.BusConflicts }),
		f.check(i18n.T(i18n.SetDMCConflicts), i18n.T(i18n.SetDMCConflictsDesc),
			func() bool { return e.DMCDMARegisterConflicts }, func(v bool) { e.DMCDMARegisterConflicts = v },
			func(c *config.Config) any { return c.Emulation.DMCDMARegisterConflicts }),
	}
}

// uintEntry は符号なし整数の入力欄を作る。
func (f *settingsForm) uintEntry(dst *uint64) fyne.CanvasObject {
	e := widget.NewEntry()
	e.OnChanged = func(s string) {
		if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err == nil {
			*dst = n
		}
	}
	read := func() { e.SetText(strconv.FormatUint(*dst, 10)) }
	read()
	f.refreshers = append(f.refreshers, read)
	return e
}

func (f *settingsForm) videoItems() []fyne.CanvasObject {
	v := &f.edit.Video
	overscan := func(name string, p *int, pick func(c *config.Config) any) fyne.CanvasObject {
		return f.intField(i18n.T(i18n.SetOverscan, name), i18n.T(i18n.SetOverscanDesc), 0, config.MaxOverscan,
			func() int { return *p }, func(n int) { *p = n }, pick)
	}
	return []fyne.CanvasObject{
		f.intField(i18n.T(i18n.MenuScale), i18n.T(i18n.SetScaleDesc), config.MinScale, config.MaxScale,
			func() int { return v.Scale }, func(n int) { v.Scale = n },
			func(c *config.Config) any { return c.Video.Scale }),
		f.check(i18n.T(i18n.SetIntegerScale), i18n.T(i18n.SetIntegerScaleDesc),
			func() bool { return v.IntegerScale }, func(b bool) { v.IntegerScale = b },
			func(c *config.Config) any { return c.Video.IntegerScale }),
		f.check(i18n.T(i18n.SetAspect), i18n.T(i18n.SetAspectDesc),
			func() bool { return v.AspectRatioCorrection }, func(b bool) { v.AspectRatioCorrection = b },
			func(c *config.Config) any { return c.Video.AspectRatioCorrection }),
		f.choice(i18n.T(i18n.SetFilter), i18n.T(i18n.SetFilterDesc),
			[]choiceOpt{{config.FilterNearest, i18n.T(i18n.SetFilterNearest)}, {config.FilterLinear, i18n.T(i18n.SetFilterLinear)}},
			func() string { return v.Filter }, func(s string) { v.Filter = s },
			func(c *config.Config) any { return c.Video.Filter }),
		overscan(i18n.T(i18n.SetTop), &v.OverscanTop, func(c *config.Config) any { return c.Video.OverscanTop }),
		overscan(i18n.T(i18n.SetBottom), &v.OverscanBottom, func(c *config.Config) any { return c.Video.OverscanBottom }),
		overscan(i18n.T(i18n.SetLeft), &v.OverscanLeft, func(c *config.Config) any { return c.Video.OverscanLeft }),
		overscan(i18n.T(i18n.SetRight), &v.OverscanRight, func(c *config.Config) any { return c.Video.OverscanRight }),
		f.pathField(i18n.T(i18n.SetPaletteFile), i18n.T(i18n.SetPaletteFileDesc), false,
			func() string { return v.PaletteFile }, func(s string) { v.PaletteFile = s },
			func(c *config.Config) any { return c.Video.PaletteFile }),
		f.check(i18n.T(i18n.SetFullscreen), "",
			func() bool { return v.Fullscreen }, func(b bool) { v.Fullscreen = b },
			func(c *config.Config) any { return c.Video.Fullscreen }),
	}
}

func (f *settingsForm) audioItems() []fyne.CanvasObject {
	a := &f.edit.Audio
	items := []fyne.CanvasObject{
		f.check(i18n.T(i18n.SetAudioEnabled), i18n.T(i18n.SetAfterRestart),
			func() bool { return a.Enabled }, func(b bool) { a.Enabled = b },
			func(c *config.Config) any { return c.Audio.Enabled }),
		f.intField(i18n.T(i18n.SetBuffer), i18n.T(i18n.SetBufferDesc), 5, 200,
			func() int { return a.BufferMilliseconds }, func(n int) { a.BufferMilliseconds = n },
			func(c *config.Config) any { return c.Audio.BufferMilliseconds }),
		f.volumeField(i18n.T(i18n.SetMasterVolume), i18n.T(i18n.SetImmediate),
			func() float64 { return a.MasterVolume }, func(v float64) { a.MasterVolume = v },
			func(c *config.Config) any { return c.Audio.MasterVolume }),
	}
	names := map[string]string{"pulse1": "Pulse 1", "pulse2": "Pulse 2", "triangle": "Triangle", "noise": "Noise", "dmc": "DMC"}
	for _, ch := range config.ChannelNames {
		items = append(items, f.volumeField(i18n.T(i18n.SetChannelVolume, names[ch]), i18n.T(i18n.SetImmediate),
			func() float64 { return a.ChannelVolumes[ch] }, func(v float64) { a.ChannelVolumes[ch] = v },
			func(c *config.Config) any { return c.Audio.ChannelVolumes[ch] }))
	}
	return append(items,
		f.choice(i18n.T(i18n.SetFilterProfile), i18n.T(i18n.SetFilterProfileDesc),
			[]choiceOpt{{"nes", "NES"}, {"famicom", i18n.T(i18n.SetFamicom)}, {"none", i18n.T(i18n.SetNoneOption)}},
			func() string { return a.FilterProfile }, func(s string) { a.FilterProfile = s },
			func(c *config.Config) any { return c.Audio.FilterProfile }),
		f.check(i18n.T(i18n.SetMuteFastForward), i18n.T(i18n.SetImmediate),
			func() bool { return a.MuteOnFastForward }, func(b bool) { a.MuteOnFastForward = b },
			func(c *config.Config) any { return c.Audio.MuteOnFastForward }),
		f.check(i18n.T(i18n.SetSilenceTriangle), i18n.T(i18n.SetSilenceTriangleDesc),
			func() bool { return a.SilenceUltrasonicTriangle }, func(b bool) { a.SilenceUltrasonicTriangle = b },
			func(c *config.Config) any { return c.Audio.SilenceUltrasonicTriangle }),
	)
}

func (f *settingsForm) inputItems() []fyne.CanvasObject {
	in := &f.edit.Input
	devices := []choiceOpt{{config.DeviceStandard, i18n.T(i18n.SetStandardController)}, {config.DeviceNone, i18n.T(i18n.SetNoneOption)}}
	f.keyEditor = newKeyEditor(f)
	return []fyne.CanvasObject{
		f.choice(i18n.T(i18n.SetPort1), i18n.T(i18n.SetAfterReload), devices,
			func() string { return in.Port1Device }, func(s string) { in.Port1Device = s },
			func(c *config.Config) any { return c.Input.Port1Device }),
		f.choice(i18n.T(i18n.SetPort2), i18n.T(i18n.SetAfterReload), devices,
			func() string { return in.Port2Device }, func(s string) { in.Port2Device = s },
			func(c *config.Config) any { return c.Input.Port2Device }),
		f.intField(i18n.T(i18n.SetTurboDefault), i18n.T(i18n.SetTurboDefaultDesc), 1, 30,
			func() int { return in.TurboRateHz }, func(n int) { in.TurboRateHz = n },
			func(c *config.Config) any { return c.Input.TurboRateHz }),
		f.item(i18n.T(i18n.SetKeybindings), i18n.T(i18n.SetImmediate), f.keyEditor.content(), nil),
	}
}

func (f *settingsForm) pathItems() []fyne.CanvasObject {
	p := &f.edit.Paths
	field := func(label string, dst *string, pick func(c *config.Config) any) fyne.CanvasObject {
		return f.pathField(label, i18n.T(i18n.SetPathDesc), true, func() string { return *dst }, func(s string) { *dst = s }, pick)
	}
	return []fyne.CanvasObject{
		field("ROM", &p.ROMDir, func(c *config.Config) any { return c.Paths.ROMDir }),
		field(i18n.T(i18n.SetPathSave), &p.SaveDir, func(c *config.Config) any { return c.Paths.SaveDir }),
		field(i18n.T(i18n.SetPathState), &p.StateDir, func(c *config.Config) any { return c.Paths.StateDir }),
		field(i18n.T(i18n.SetPathScreenshot), &p.ScreenshotDir, func(c *config.Config) any { return c.Paths.ScreenshotDir }),
		field(i18n.T(i18n.ViewerLog), &p.LogDir, func(c *config.Config) any { return c.Paths.LogDir }),
		field(i18n.T(i18n.SetPathMovie), &p.MovieDir, func(c *config.Config) any { return c.Paths.MovieDir }),
	}
}

func (f *settingsForm) debugItems() []fyne.CanvasObject {
	d := &f.edit.Debug
	cats := widget.NewCheckGroup(config.LogCategoryNames(), func(sel []string) {
		d.LogCategories = append([]string{}, sel...)
	})
	readCats := func() { cats.SetSelected(slices.Clone(d.LogCategories)) }
	readCats()
	f.refreshers = append(f.refreshers, readCats)
	return []fyne.CanvasObject{
		f.item(i18n.T(i18n.SetLogCategories), i18n.T(i18n.SetLogCategoriesDesc), cats,
			func(c *config.Config) any { return c.Debug.LogCategories }),
		f.choice(i18n.T(i18n.SetLogOutput), i18n.T(i18n.SetAfterRestart),
			[]choiceOpt{{config.LogNone, i18n.T(i18n.SetLogNone)}, {config.LogStderr, i18n.T(i18n.SetLogStderr)}, {config.LogStdout, i18n.T(i18n.SetLogStdout)}, {config.LogFile, i18n.T(i18n.MenuFile)}},
			func() string { return d.LogOutput }, func(s string) { d.LogOutput = s },
			func(c *config.Config) any { return c.Debug.LogOutput }),
		f.intField(i18n.T(i18n.SetLogSize), i18n.T(i18n.SetLogSizeDesc), 1, 1024,
			func() int { return int(d.LogMaxBytes >> 20) }, func(n int) { d.LogMaxBytes = int64(n) << 20 },
			func(c *config.Config) any { return c.Debug.LogMaxBytes }),
		f.intField(i18n.T(i18n.SetLogGenerations), i18n.T(i18n.SetAfterRestart), 1, 20,
			func() int { return d.LogGenerations }, func(n int) { d.LogGenerations = n },
			func(c *config.Config) any { return c.Debug.LogGenerations }),
		f.intField(i18n.T(i18n.SetTraceRing), i18n.T(i18n.SetTraceRingDesc), 1000, 10000000,
			func() int { return d.TraceRingSize }, func(n int) { d.TraceRingSize = n },
			func(c *config.Config) any { return c.Debug.TraceRingSize }),
		f.intField(i18n.T(i18n.SetDecay), i18n.T(i18n.SetDecayDesc), 1, 600,
			func() int { return d.ChangeDecayFrames }, func(n int) { d.ChangeDecayFrames = n },
			func(c *config.Config) any { return c.Debug.ChangeDecayFrames }),
		f.check(i18n.T(i18n.SetMemoryWrite), i18n.T(i18n.SetMemoryWriteDesc),
			func() bool { return d.MemoryEditWrite }, func(b bool) { d.MemoryEditWrite = b },
			func(c *config.Config) any { return c.Debug.MemoryEditWrite }),
		f.check(i18n.T(i18n.SetBreakUninit), i18n.T(i18n.SetBreakUninitDesc),
			func() bool { return d.BreakOnUninitializedRAMRead }, func(b bool) { d.BreakOnUninitializedRAMRead = b },
			func(c *config.Config) any { return c.Debug.BreakOnUninitializedRAMRead }),
	}
}

func (f *settingsForm) stateItems() []fyne.CanvasObject {
	st := &f.edit.State
	m := &f.edit.Movie
	return []fyne.CanvasObject{
		f.intField(i18n.T(i18n.SetSlots), i18n.T(i18n.SetSlotsDesc), 1, 10,
			func() int { return st.Slots }, func(n int) { st.Slots = n },
			func(c *config.Config) any { return c.State.Slots }),
		f.check(i18n.T(i18n.SetStateScreenshot), i18n.T(i18n.SetStateScreenshotDesc),
			func() bool { return st.SaveScreenshot }, func(b bool) { st.SaveScreenshot = b },
			func(c *config.Config) any { return c.State.SaveScreenshot }),
		f.check(i18n.T(i18n.SetRewind), "",
			func() bool { return st.RewindEnabled }, func(b bool) { st.RewindEnabled = b },
			func(c *config.Config) any { return c.State.RewindEnabled }),
		f.intField(i18n.T(i18n.SetRewindSeconds), "", 1, 600,
			func() int { return st.RewindSeconds }, func(n int) { st.RewindSeconds = n },
			func(c *config.Config) any { return c.State.RewindSeconds }),
		f.intField(i18n.T(i18n.SetRewindInterval), i18n.T(i18n.SetRewindIntervalDesc), 1, 60,
			func() int { return st.RewindIntervalFrames }, func(n int) { st.RewindIntervalFrames = n },
			func(c *config.Config) any { return c.State.RewindIntervalFrames }),
		f.intField(i18n.T(i18n.SetChecksumInterval), i18n.T(i18n.SetChecksumIntervalDesc), 1, 3600,
			func() int { return m.ChecksumIntervalFrames }, func(n int) { m.ChecksumIntervalFrames = n },
			func(c *config.Config) any { return c.Movie.ChecksumIntervalFrames }),
		f.check(i18n.T(i18n.SetStopOnDesync), "",
			func() bool { return m.StopOnDesync }, func(b bool) { m.StopOnDesync = b },
			func(c *config.Config) any { return c.Movie.StopOnDesync }),
		f.check(i18n.T(i18n.SetVerifyChecksums), "",
			func() bool { return m.VerifyChecksums }, func(b bool) { m.VerifyChecksums = b },
			func(c *config.Config) any { return c.Movie.VerifyChecksums }),
	}
}

func (f *settingsForm) appearanceItems() []fyne.CanvasObject {
	ui := &f.edit.UI
	return []fyne.CanvasObject{
		f.choice(i18n.T(i18n.SetTheme), i18n.T(i18n.SetThemeDesc),
			[]choiceOpt{{config.ThemeAuto, i18n.T(i18n.SetThemeAuto)}, {config.ThemeLight, i18n.T(i18n.SetThemeLight)}, {config.ThemeDark, i18n.T(i18n.SetThemeDark)}},
			func() string { return ui.Theme }, func(s string) { ui.Theme = s },
			func(c *config.Config) any { return c.UI.Theme }),
		f.choice(i18n.T(i18n.MenuViewerLayout), "",
			[]choiceOpt{{config.LayoutWindows, i18n.T(i18n.MenuLayoutWindows)}, {config.LayoutDocked, i18n.T(i18n.MenuLayoutTabs)}},
			func() string { return ui.ViewerLayout }, func(s string) { ui.ViewerLayout = s },
			func(c *config.Config) any { return c.UI.ViewerLayout }),
		f.choice(i18n.T(i18n.SetLanguage), "",
			[]choiceOpt{{config.LanguageJapanese, i18n.T(i18n.SetJapanese)}},
			func() string { return ui.Language }, func(s string) { ui.Language = s },
			func(c *config.Config) any { return c.UI.Language }),
	}
}

// resetDefaults は写しを既定値にする。ウィンドウ状態と最近使った ROM は残す。
func (f *settingsForm) resetDefaults() {
	d := config.Default()
	d.UI.Windows = f.edit.UI.Windows
	d.UI.RecentROMs = f.edit.UI.RecentROMs
	*f.edit = *d
	*f.keys = *config.DefaultKeybindings()
	for _, r := range f.refreshers {
		r()
	}
	if f.keyEditor != nil {
		f.keyEditor.rebuild()
	}
}

// openConfigDir は設定ファイルのディレクトリを OS のファイラで開く。
func (f *settingsForm) openConfigDir() {
	if f.u.store == nil || f.u.store.File == "" {
		return
	}
	dir := filepath.Dir(f.u.store.File)
	if err := f.u.app.OpenURL(&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}); err != nil {
		f.u.showError(err)
	}
}

// save は写しを保存する設定へ入れ、反映して閉じる。
func (f *settingsForm) save() {
	old := f.u.cfg.Clone()
	if f.u.store != nil {
		if err := f.u.store.Replace(f.edit.Clone()); err != nil {
			f.u.showError(err)
			return
		}
		if err := f.u.store.SetKeys(f.keys.Clone()); err != nil {
			f.u.showError(err)
			return
		}
	} else {
		*f.u.cfg = *f.edit.Clone()
	}
	f.u.applySettings(old, f.keys)
	f.close()
}

// close はウィンドウを閉じる。
func (f *settingsForm) close() {
	if f.win != nil {
		f.win.Close()
	}
}

// applySettings は保存した設定を画面とエミュレータへ反映する（設計書 11 編 §11.3.2）。
func (u *UI) applySettings(old *config.Config, keys *config.Keybindings) {
	c := u.cfg
	u.emu.ApplySettings(c)
	if c.Video.PaletteFile != old.Video.PaletteFile {
		*u.pal = *loadPalette(c.Video.PaletteFile)
	}
	if u.screen != nil {
		u.screen.applyVideo(c.Video)
	}
	if u.win != nil && c.Video.Fullscreen != old.Video.Fullscreen {
		u.win.SetFullScreen(c.Video.Fullscreen)
	}
	u.applyTheme()
	if c.UI.ViewerLayout != old.UI.ViewerLayout {
		u.switchLayout()
	}
	u.applyKeys(keys)

	var notes []string
	if u.emu.Status().Loaded && needsReload(old, c) {
		notes = append(notes, i18n.T(i18n.SetNoteReload))
	}
	if needsRestart(old, c) {
		notes = append(notes, i18n.T(i18n.SetNoteRestart))
	}
	if len(notes) > 0 && u.status != nil {
		u.status.notify(strings.Join(notes, "。"))
	}
}

// needsReload は ROM の再読み込み後に効く項目が変わったかを返す。
func needsReload(a, b *config.Config) bool {
	return a.Emulation != b.Emulation || a.Paths != b.Paths || a.State != b.State || a.Movie != b.Movie ||
		a.Input.Port1Device != b.Input.Port1Device || a.Input.Port2Device != b.Input.Port2Device ||
		a.Audio.FilterProfile != b.Audio.FilterProfile ||
		a.Debug.BreakOnUninitializedRAMRead != b.Debug.BreakOnUninitializedRAMRead
}

// needsRestart は再起動後に効く項目が変わったかを返す。
func needsRestart(a, b *config.Config) bool {
	return a.Audio.Enabled != b.Audio.Enabled || a.Debug.LogOutput != b.Debug.LogOutput ||
		a.Debug.LogMaxBytes != b.Debug.LogMaxBytes || a.Debug.LogGenerations != b.Debug.LogGenerations ||
		a.Debug.TraceRingSize != b.Debug.TraceRingSize
}
