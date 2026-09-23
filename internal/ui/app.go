// Package ui は Fyne による画面を提供する。
package ui

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// appID はアプリケーションの識別子。Fyne が設定の保存先に使う。
const appID = "dev.takaakimizuno.shogun-emulator"

// refreshCycle は画面の更新を駆動するアニメーションの 1 周期。
//
// Fyne のアニメーションは進捗（0 から 1）を渡して毎フレーム呼ばれる。
// ここでは進捗を使わず、呼ばれること自体を周期実行として用いるため、
// 値は表示に影響しない。終わらないアニメーションとして登録する。
const refreshCycle = time.Second

// maxRecentROMs は最近使った ROM を覚える数。
const maxRecentROMs = 10

// 早送りとスローの倍率。
//
// 早送りで待ちを行わない境界を選ぶ。これより下の倍率にすると、
// 押している間の待ちが残り、早送りの効きが頭打ちになる。
const (
	fastForwardSpeed = emu.UncappedSpeed
	slowMotionSpeed  = 0.25
)

// UI はアプリケーションの画面。
//
// すべてのメソッドを UI スレッドから呼ぶ。画面の更新を行うゴルーチンは
// Post を経由する。
type UI struct {
	app fyne.App
	win fyne.Window

	emu *emu.Emulator
	cfg *config.Config
	pal *video.Palette

	// bindings は物理キーからアクションへの表。
	bindings map[string][]config.Action
	// pressed は押されているキー。キーリピートを無視するために持つ。
	pressed map[string]bool

	screen *screen
	status *statusBar

	// tabs はビューアをタブに置く配置で使う。
	tabs *container.AppTabs
	host ViewerHost

	// baseSpeed は早送りとスローを適用する前の速度倍率。
	baseSpeed float64
	// fastForward と slowMotion は押している間の状態。
	fastForward bool
	slowMotion  bool
	// paused は一時停止中かどうかの、UI 側が持つ見かけ。
	paused bool
	// muted は消音中かどうかの、UI 側が持つ見かけ。
	muted bool

	// recent は最近使った ROM のパス。新しいものを先頭に置く。
	recent []string
	// recentItem は最近使った ROM のメニュー項目。中身を作り直すために持つ。
	recentItem *fyne.MenuItem

	// version はバージョン情報の表示に使う文字列。
	version string

	// refreshAnim は画面の更新を駆動するアニメーション。
	refreshAnim *fyne.Animation

	// notifyMu はエミュレーションゴルーチンから届く知らせを守る。
	notifyMu sync.Mutex
	// notices は表示していない知らせ。
	notices []string
	// desyncShown は desync のダイアログを出したことを表す。
	desyncShown bool

	// debugUsers はデバッガの機能を使っているビューアの数。
	debugUsers debugUsers
	// cpuViewer は CPU デバッガ。1 つだけ作る。
	cpuViewer *cpuViewer
	// logViewer はログビューア。1 つだけ作る。
	logViewer *logViewer
	// bpViewer はブレークポイント一覧。1 つだけ作る。
	bpViewer *breakpointViewer
	// memoryCount は開いたメモリビューアの数。題名の番号に使う。
	memoryCount int
	// PPU 系のビューア。1 つずつ作る。
	patternViewer   *patternViewer
	nametableViewer *nametableViewer
	spriteViewer    *spriteViewer
	paletteViewer   *paletteViewer
	apuViewer       *apuViewer
	// tileEditors はタイルのピクセルエディタ。PPU アドレスごとに 1 つ。
	tileEditors map[uint16]*tileEditor
	// traceItem と traceFileItem はデバッグメニューのチェック付きの項目。
	traceItem, traceFileItem *fyne.MenuItem
	// overlayItem はオーバーレイの有効・無効の項目。
	overlayItem *fyne.MenuItem
	// breakPending はブレークポイントで止まったことを表す。画面の更新で
	// CPU デバッガを前面に出す。
	breakPending atomic.Bool
	// viewerTick はビューアの更新を間引くための数。
	viewerTick int
}

// New は画面を作る。
func New(e *emu.Emulator, cfg *config.Config, keys *config.Keybindings, version string) *UI {
	a := app.NewWithID(appID)
	pal := loadPalette(cfg.Video.PaletteFile)

	u := &UI{
		app:       a,
		emu:       e,
		cfg:       cfg,
		pal:       pal,
		bindings:  keys.Resolve(),
		pressed:   map[string]bool{},
		baseSpeed: 1.0,
		version:   version,
	}
	u.screen = newScreen(e.Frames, pal, cfg.Video)
	u.status = newStatusBar(e.Frames)
	// エミュレーションゴルーチンからの知らせを溜め、画面の更新で出す。
	// UI の部品はこのゴルーチンから触らない。
	e.SetNotify(u.postNotice)
	// ブレークポイントで止まったら、画面の更新で CPU デバッガを前面に出す。
	e.SetOnBreak(func(debug.BreakInfo) { u.breakPending.Store(true) })
	return u
}

// loadPalette は設定で指定されたパレットファイルを読む。
//
// 読めないときは組み込みのパレットを使う。パレットファイルを理由に
// 起動できない状態を作らない。
func loadPalette(path string) *video.Palette {
	if path == "" {
		return video.DefaultPalette()
	}
	p, err := video.LoadPaletteFile(path)
	if err != nil {
		fyne.LogError("パレットファイルを読めないため組み込みのパレットを使う", err)
		return video.DefaultPalette()
	}
	return p
}

// Run はメインウィンドウを表示してイベントループへ入る。
//
// 戻るのはウィンドウを閉じたときである。
func (u *UI) Run() {
	u.app.SetIcon(appIcon())

	// 画面を開く前に ROM を読み込んでいることがある。表示する画の
	// 高さをリージョンに合わせてからウィンドウの大きさを決める。
	u.screen.SetPictureHeight(u.emu.Status().PictureHeight)

	u.win = u.app.NewWindow(u.windowTitle())
	u.win.SetIcon(appIcon())
	u.win.SetMaster()
	u.win.SetContent(u.buildContent())
	u.win.SetMainMenu(u.buildMainMenu())
	u.win.Resize(u.preferredSize())
	u.win.SetFullScreen(u.cfg.Video.Fullscreen)
	u.installKeyHandlers()

	u.win.SetOnClosed(func() {
		u.stopRefreshing()
		u.emu.Stop()
	})

	u.emu.Start()
	u.startRefreshing()
	u.win.ShowAndRun()
}

// buildContent はメインウィンドウの中身を組み立てる。
//
// ビューアをタブに置く配置のときだけタブを作る。ビューアが無いあいだも
// タブを置くと、空のタブ列が画面を占める。
func (u *UI) buildContent() fyne.CanvasObject {
	center := container.NewCenter(u.screen.CanvasObject())
	if u.cfg.UI.ViewerLayout == config.LayoutDocked {
		u.tabs = container.NewAppTabs()
		u.host = newDockedHost(u.tabs)
		return container.NewBorder(nil, u.status.CanvasObject(), nil, nil,
			container.NewHSplit(center, u.tabs))
	}
	u.host = newWindowHost(u.app)
	return container.NewBorder(nil, u.status.CanvasObject(), nil, nil, center)
}

// SetViewerLayout は配置を切り替える。
//
// 表示中のビューアは新しい置き場所へ移す。メインウィンドウがまだ無い
// ときは設定だけを変える。
func (u *UI) SetViewerLayout(layout string) {
	if layout != config.LayoutWindows && layout != config.LayoutDocked {
		return
	}
	if layout == u.cfg.UI.ViewerLayout {
		return
	}
	u.cfg.UI.ViewerLayout = layout
	if u.win == nil {
		return
	}

	old := u.host
	u.win.SetContent(u.buildContent())
	if old != nil {
		switchHost(old, u.host)
	}
}

// preferredSize はウィンドウの初期の大きさを返す。
//
// 画面の大きさとステータスバーの高さから決める。
func (u *UI) preferredSize() fyne.Size {
	w, h := u.screen.PixelSize()
	statusHeight := u.status.CanvasObject().MinSize().Height
	return fyne.NewSize(float32(w), float32(h)+statusHeight)
}

// windowTitle はウィンドウタイトルを返す。
func (u *UI) windowTitle() string {
	s := u.emu.Status()
	if !s.Loaded {
		return appTitle
	}
	return fmt.Sprintf("%s — %s", s.ROMName, appTitle)
}

// appTitle はウィンドウタイトルに使うアプリケーション名。
const appTitle = "将軍エミュレータ"

// startRefreshing は画面の更新を Fyne のフレーム駆動に載せる。
//
// 自前のゴルーチンからタイマーで更新しない。Fyne は終了処理が始まった
// 後、fyne.Do に渡した関数を呼び出し元のゴルーチンで実行する。終了と
// 更新が重なると、UI オブジェクトを UI スレッド以外から触ることになる。
//
// アニメーションの tick は Fyne の描画ループが、画面を描く直前に
// UI スレッドから呼ぶ。完成したフレームを描画の直前に取り込めるため、
// 表示までの遅れも短くなる。
func (u *UI) startRefreshing() {
	u.refreshAnim = fyne.NewAnimation(refreshCycle, func(float32) { u.refresh() })
	u.refreshAnim.RepeatCount = fyne.AnimationRepeatForever
	u.app.Driver().StartAnimation(u.refreshAnim)
}

// stopRefreshing は画面の更新を止める。
func (u *UI) stopRefreshing() {
	if u.refreshAnim == nil {
		return
	}
	u.app.Driver().StopAnimation(u.refreshAnim)
	u.refreshAnim = nil
}

// refresh は画面とステータスバーを更新する。UI スレッドで実行される。
func (u *UI) refresh() {
	u.screen.refresh()
	u.drainNotices()
	u.checkDesync()
	u.status.update(u.emu.Status())
	u.refreshViewers()
}

// viewerRefreshInterval はビューアを更新する間隔（画面の更新の回数）。
//
// ビューアの内容はエミュレーションゴルーチンから命令境界で受け取る。
// 画面の更新ごとに受け取ると、実行中の待ちが増える。人が読む表示には
// 1 秒に 15 回で足りる。
const viewerRefreshInterval = 4

// viewerRefreshRunningInterval は実行中にビューアを更新する間隔。
//
// 実行中はエミュレーションゴルーチンがフレームの間の待ちに入っている
// ことがあり、受け取りに数ミリ秒かかる。その間 UI スレッドが止まるため、
// 実行中は 1 秒に 4 回に抑える。
const viewerRefreshRunningInterval = 15

// refreshViewers は表示しているビューアを更新する。
func (u *UI) refreshViewers() {
	if u.breakPending.CompareAndSwap(true, false) && u.host != nil {
		// ブレークポイントで止まった。CPU デバッガを前面に出す。
		u.host.Show(u.cpuDebugger())
	}
	if u.host == nil {
		return
	}
	u.viewerTick++
	interval := viewerRefreshInterval
	if !u.emu.Status().Paused {
		interval = viewerRefreshRunningInterval
	}
	if u.viewerTick%interval != 0 {
		return
	}
	for _, v := range u.host.Visible() {
		v.Refresh()
	}
}

// postNotice は知らせを溜める。エミュレーションゴルーチンが呼ぶ。
func (u *UI) postNotice(msg string) {
	u.notifyMu.Lock()
	defer u.notifyMu.Unlock()
	u.notices = append(u.notices, msg)
}

// drainNotices は溜まった知らせをステータスバーへ出す。
func (u *UI) drainNotices() {
	u.notifyMu.Lock()
	msgs := u.notices
	u.notices = nil
	u.notifyMu.Unlock()

	if len(msgs) == 0 {
		return
	}
	// 最後の 1 つだけを出す。1 フレームの間に複数届くことは稀であり、
	// 溜めて順に出すと表示が遅れる。
	u.status.notify(msgs[len(msgs)-1])
}

// checkDesync はムービーの再生で状態の食い違いを検出したときに知らせる。
func (u *UI) checkDesync() {
	err := u.emu.DesyncError()
	if err == nil {
		u.desyncShown = false
		return
	}
	if u.desyncShown {
		return
	}
	u.desyncShown = true
	u.showError(err)
}

// installKeyHandlers はキーイベントの受け取りを設定する。
//
// 押下と離上の両方を受け取るために desktop.Canvas を使う。
// fyne.Canvas の SetOnTypedKey は押下しか通知しない。
func (u *UI) installKeyHandlers() {
	dc, ok := u.win.Canvas().(desktop.Canvas)
	if !ok {
		// モバイル環境などキーボードの無い環境。この段階では対象外。
		return
	}
	dc.SetOnKeyDown(u.onKeyDown)
	dc.SetOnKeyUp(u.onKeyUp)
}

// showError はエラーをダイアログで表示する。
func (u *UI) showError(err error) {
	if err == nil {
		return
	}
	dialog.ShowError(err, u.win)
}
