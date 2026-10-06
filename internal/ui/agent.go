package ui

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
)

// GUI 版の Agent Interface（設計書 10 編 §10.11、設計書 14 編 §14.4、§14.21）。
//
// 表示中のエミュレータを Instance i1 として AI と共有する。既定では無効で、
// 「AI」メニュー・設定・引数 --agent で有効にする。バナーとステータスの表示は
// 画面の更新（refresh）で Agent の状態を読んで行い、他のゴルーチンから Fyne の
// 部品を触らない（設計書 10 編 §10.3）。

// agentLink は有効にした Agent Interface。
type agentLink struct {
	host *agent.Host
	run  *rpc.Running
	inst *agent.Instance
}

// agentUI は Agent Interface の表示の部品と状態。
type agentUI struct {
	link *agentLink
	// bpAdded は Control を持たない AI がブレークポイントを加えたことを表す。
	// Agent のゴルーチンが立て、画面の更新で知らせる。
	bpAdded atomic.Bool
	// romChanged は ROM ファイルの変化を見つけたことを表す。監視のゴルーチンが
	// 立て、画面の更新で agent.romWatchAction に従って扱う。
	romChanged atomic.Bool
	// romPath は表示中の ROM のパス。
	romPath string
	// cacheDir は発見ファイルの置き場所を差し替える（テスト用）。空なら既定。
	cacheDir string

	banner      *fyne.Container
	bannerLabel *widget.Label
	allowItem   *fyne.MenuItem
	lastBanner  string
}

// bannerObject はメインウィンドウの上端に置くバナーを作る（設計書 14 編
// §14.4.3）。AI が Control を持つ間だけ表示する。
func (u *UI) bannerObject() fyne.CanvasObject {
	a := &u.agentUI
	if a.banner == nil {
		a.bannerLabel = widget.NewLabel("")
		a.bannerLabel.Importance = widget.WarningImportance
		take := widget.NewButton(i18n.T(i18n.AITakeBack), u.takeBackFromAgent)
		a.banner = container.NewHBox(a.bannerLabel, layout.NewSpacer(), take)
		a.banner.Hide()
	}
	return a.banner
}

// agentCacheDir は発見ファイルとソケットを置くキャッシュディレクトリを返す。
func (u *UI) agentCacheDir() string {
	if u.agentUI.cacheDir != "" {
		return u.agentUI.cacheDir
	}
	if u.store != nil {
		return u.store.Paths.Cache
	}
	if p, err := config.DefaultPaths(); err == nil {
		return p.Cache
	}
	return ""
}

// SetAgentEnabled は Agent Interface を有効または無効にする。save が true の
// とき設定 agent.enabled にも書く。UI スレッドから呼ぶ。
func (u *UI) SetAgentEnabled(on, save bool) {
	a := &u.agentUI
	if save {
		u.update(func(c *config.Config) { c.Agent.Enabled = on })
	}
	if on && a.link == nil {
		host := agent.NewHost(agent.Options{
			Kind: agent.KindGUI, Server: "shogun " + u.version, ImageScale: u.cfg.Agent.ObserveImageScale,
			EmuConfig:         emu.Config{Dirs: u.emu.Dirs()},
			OnAgentBreakpoint: func() { a.bpAdded.Store(true) },
			OnROMChanged:      func(*agent.Instance) { a.romChanged.Store(true) },
		})
		inst := host.AttachGUI(u.emu)
		inst.SetROMPath(a.romPath)
		run, err := rpc.Start(host, u.cfg.Agent.Listen, u.agentCacheDir(), u.emu.Status().ROMName)
		if err != nil {
			u.emu.SetObserver(nil)
			host.Close()
			u.showError(err)
			if save {
				u.update(func(c *config.Config) { c.Agent.Enabled = false })
			}
			u.syncAgentMenu()
			return
		}
		a.link = &agentLink{host: host, run: run, inst: inst}
		u.watchROM()
		u.status.notify(i18n.T(i18n.StatusAIListening, run.Endpoint))
	}
	if !on && a.link != nil {
		l := a.link
		a.link = nil
		// AI が操作していれば人間に戻してから止める。
		l.host.TakeBack(l.inst)
		l.run.Close()
		l.host.Close()
		u.emu.SetObserver(nil)
		u.status.notify(i18n.T(i18n.StatusAIStopped))
	}
	u.syncAgentMenu()
}

// AgentEnabled は Agent Interface が有効かを返す。
func (u *UI) AgentEnabled() bool { return u.agentUI.link != nil }

// AgentDiscovery は発見ファイルのパスを返す。無効のとき空。
func (u *UI) AgentDiscovery() string {
	if u.agentUI.link == nil {
		return ""
	}
	return u.agentUI.link.run.Discovery
}

// takeBackFromAgent は人間が Control を取り返す（設計書 14 編 §14.4.1）。
func (u *UI) takeBackFromAgent() {
	l := u.agentUI.link
	if l == nil {
		return
	}
	if l.host.TakeBack(l.inst) {
		u.paused = false
		u.status.notify(i18n.T(i18n.StatusAITakenBack))
	}
}

// agentHasControl は AI が Control を持っているかを返す。
func (u *UI) agentHasControl() bool {
	l := u.agentUI.link
	return l != nil && l.inst.ControlStatus().Owner == agent.OwnerConn.String()
}

// agentKeyDown はゲームの入力キーを押したとき、AI が Control を持っていれば
// 人間に戻す。ホットキーでは戻さない（設計書 10 編 §10.6）。
func (u *UI) agentKeyDown(code string) {
	if !u.agentHasControl() {
		return
	}
	for _, act := range u.bindings[code] {
		if _, _, ok := emu.PlayerButton(act); ok {
			u.takeBackFromAgent()
			return
		}
	}
}

// agentSafeHotkey は AI が Control を持つ間も受け付けるホットキーかを返す。
// 画面の表示と音だけに効くものに限る。
func agentSafeHotkey(a config.Action) bool {
	switch a {
	case config.ActionScreenshot, config.ActionToggleFullscreen, config.ActionMute:
		return true
	}
	return false
}

// refreshAgent はバナーとステータスバーの AI の表示を更新する。画面の更新で
// 呼ぶ。
func (u *UI) refreshAgent() {
	a := &u.agentUI
	if a.bpAdded.CompareAndSwap(true, false) {
		u.status.notify(i18n.T(i18n.StatusAIBreakpoint))
	}
	if a.romChanged.CompareAndSwap(true, false) {
		u.handleROMChanged()
	}
	text, note := "", ""
	if l := a.link; l != nil {
		st := l.inst.ControlStatus()
		if st.Owner == agent.OwnerConn.String() {
			client := st.Client
			if client == "" {
				client = i18n.T(i18n.AIUnnamed)
			}
			text = i18n.T(i18n.AIBanner, client, int(st.Conn))
		} else if n := len(l.host.Connections()); n > 0 {
			note = i18n.T(i18n.StatusAIConnected, n)
		} else {
			note = i18n.T(i18n.StatusAIListeningNote)
		}
	}
	u.status.agentNote = note
	if a.banner == nil || text == a.lastBanner {
		return
	}
	a.lastBanner = text
	if text == "" {
		a.banner.Hide()
		return
	}
	a.bannerLabel.SetText(text)
	a.banner.Show()
}

// watchROM は表示中の ROM ファイルの監視を始める（設計書 14 編 §14.15.2）。
// 変化を見つけたときの動作は agent.romWatchAction に従う（refreshAgent）。
func (u *UI) watchROM() {
	a := &u.agentUI
	if a.link == nil || a.romPath == "" {
		return
	}
	if err := a.link.host.Watch(a.link.inst); err != nil {
		u.showError(err)
	}
}

// handleROMChanged は ROM ファイルの変化を agent.romWatchAction に従って扱う。
// 既定は知らせるだけにする。人間が遊んでいる最中に画面が切り替わることを
// 避けるためである（設計書 14 編 §14.15.2）。
func (u *UI) handleROMChanged() {
	a := &u.agentUI
	if a.link == nil {
		return
	}
	if u.cfg.Agent.RomWatchAction != config.RomWatchReload {
		u.status.notify(i18n.T(i18n.StatusAIROMChanged))
		return
	}
	l := a.link
	go func() {
		if err := l.host.Reload(context.Background(), l.inst, agent.ReloadOptions{ReReach: "frame"}); err != nil {
			u.postNotice(err.Error())
			return
		}
		u.postNotice(i18n.T(i18n.StatusAIROMReloaded))
	}()
}

// agentROMLoaded は ROM を開いたことを Agent Interface に伝える。
func (u *UI) agentROMLoaded(path string) {
	a := &u.agentUI
	a.romPath = path
	if a.link == nil {
		return
	}
	a.link.inst.SetROMPath(path)
	a.link.host.Unwatch(a.link.inst)
	u.watchROM()
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if err := a.link.run.UpdateROM(name); err != nil {
		u.showError(err)
	}
}

// aiMenu は「AI」メニューを作る（設計書 10 編 §10.5）。
func (u *UI) aiMenu() *fyne.Menu {
	a := &u.agentUI
	a.allowItem = fyne.NewMenuItem(i18n.T(i18n.MenuAIAllow), func() {
		u.SetAgentEnabled(a.link == nil, true)
	})
	a.allowItem.Checked = a.link != nil
	copyItem := fyne.NewMenuItem(i18n.T(i18n.MenuAICopy), func() {
		path := u.AgentDiscovery()
		if path == "" {
			u.status.notify(i18n.T(i18n.AIClientsDisabled))
			return
		}
		if u.win != nil {
			u.app.Clipboard().SetContent(path)
		}
		u.status.notify(i18n.T(i18n.StatusAICopied, path))
	})
	clients := fyne.NewMenuItem(i18n.T(i18n.MenuAIClients), u.showAgentClients)
	disconnect := fyne.NewMenuItem(i18n.T(i18n.MenuAIDisconnect), func() {
		if a.link != nil {
			a.link.run.Server.DisconnectAll()
		}
	})
	return fyne.NewMenu(i18n.T(i18n.MenuAI), a.allowItem, fyne.NewMenuItemSeparator(), copyItem, clients, disconnect)
}

// agentClientLines は接続中のクライアントの一覧の行を返す。
func (u *UI) agentClientLines() []string {
	l := u.agentUI.link
	if l == nil {
		return []string{i18n.T(i18n.AIClientsDisabled)}
	}
	conns := l.host.Connections()
	if len(conns) == 0 {
		return []string{i18n.T(i18n.AIClientsNone)}
	}
	var lines []string
	for _, c := range conns {
		name := c.Client
		if name == "" {
			name = i18n.T(i18n.AIUnnamed)
		}
		if c.Control {
			name = i18n.T(i18n.AIClientControl, name)
		}
		lines = append(lines, i18n.T(i18n.AIClientLine, int(c.ID), name))
	}
	return lines
}

// showAgentClients は接続中のクライアントの一覧を出す。
func (u *UI) showAgentClients() {
	if u.win == nil {
		return
	}
	dialog.ShowCustom(i18n.T(i18n.AIClientsTitle), i18n.T(i18n.CommonClose),
		widget.NewLabel(strings.Join(u.agentClientLines(), "\n")), u.win)
}

// syncAgentMenu は「AI からの接続を許可」のチェックを状態に合わせる。
func (u *UI) syncAgentMenu() {
	if u.agentUI.allowItem == nil {
		return
	}
	u.agentUI.allowItem.Checked = u.agentUI.link != nil
	u.refreshMainMenu()
}

// closeAgent は終了時に Agent Interface を止める。発見ファイルとソケットを消す。
func (u *UI) closeAgent() {
	if u.agentUI.link != nil {
		u.SetAgentEnabled(false, false)
	}
}
