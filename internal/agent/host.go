package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// APIVersion は Agent Interface の版（設計書 14 編 §14.5.1）。
const APIVersion = 1

// Kind は Host の種類。
type Kind string

// Host の種類。発見ファイルの kind に書く値と同じ。
const (
	KindGUI   Kind = "gui"
	KindServe Kind = "serve"
)

// InstanceID は Instance の識別子（"i1"・"i2"…）。
type InstanceID string

// Options は Host の設定。
type Options struct {
	Kind Kind
	// Server はバージョン文字列（session.hello の server）。
	Server string
	// MaxInstances は headless の Instance の上限。0 のとき 16。
	MaxInstances int
	// ImageScale は Observation の画像の既定の拡大率。0 のとき 2。
	ImageScale int
	// OnROMChanged は ROM の監視で ROM ファイルの変化を見つけたときに呼ばれる
	// （GUI 版の agent.romWatchAction）。監視のゴルーチンから呼ばれる。
	OnROMChanged func(inst *Instance)
	// OnAgentBreakpoint は GUI 版で、Control を持たない接続がブレークポイントを
	// 加えたときに呼ばれる（設計書 14 編 §14.7.1）。ブレークポイントで止まると
	// 人間の操作も止まるため、GUI で知らせる。どのゴルーチンからも呼ばれる。
	OnAgentBreakpoint func()
	// EmuConfig は headless の Instance を作るときの元になる設定。
	// 音声・進行の待ち方・一時停止の開始は Host が上書きする。
	EmuConfig emu.Config
}

// Host は Instance の登録簿（設計書 14 編 §14.3.1）。
type Host struct {
	opts     Options
	registry *Registry

	mu        sync.Mutex
	instances []*Instance // 作成順
	nextID    int
	shared    []*romShared
	conns     []*Conn
	nextConn  ConnID
	closed    bool
	// saveDir は headless の Instance のバッテリーバックアップの保存先。
	// Host ごとの一時ディレクトリで、Close で消す。
	saveDir string
}

// Instance は独立した一台分のエミュレータ。
type Instance struct {
	ID  InstanceID
	Emu *emu.Emulator

	// romName・romPath・romData は読み込んだ ROM。Fork で同じ内容を
	// 読み込むために持つ。GUI 版では持たない。
	romName string
	romPath string
	romData []byte
	shared  *romShared
	// opts は作ったときの指定。Fork で同じ指定のエミュレータを作る。
	opts InstanceOptions

	// advance は進行と書き換えを 1 つずつ処理するための排他。
	advance sync.Mutex
	// states は名前付きの保管場所（state.save の name）。Instance を閉じたら捨てる。
	states []namedState

	mu      sync.Mutex
	control controlState
	closed  bool
	// observed は接続ごとの前回の観測値。
	observed map[ConnID]*observedValues
	// events はイベントのキュー。
	events eventQueue
	// advCancel は進行中の進行の要求を取り消す。進行していないとき nil。
	// 人間が Control を取り返したときに使う。
	advCancel context.CancelFunc
	// controlLost は進行中に人間が Control を取り返したことを表す。
	controlLost bool
	// onControl は Control の持ち主が変わったときに呼ばれる（GUI のバナー）。
	onControl func(ControlStatus)
	// cmdLog はこの Instance で実行した Agent Command の記録（Repro の
	// commands.jsonl と scenario.export）。
	cmdLog []CommandLogEntry
	// cmdSeq は cmdLog の最後の通し番号。
	cmdSeq uint64
	// exportStart は scenario.export の from: start の開始。mark は
	// scenario.mark の印。印が無いとき nil。
	exportStart exportAnchor
	mark        *exportAnchor
	// watchStop は ROM の監視を止める。監視していないとき nil。
	watchStop chan struct{}
}

// romShared は同じ ROM を読み込んだ Instance で共有するもの。
type romShared struct {
	path    string // シンボルファイルのパス
	symbols *debug.Symbols
	refs    int
}

// NewHost は Host を作る。Agent Command は builtin のものを登録する。
func NewHost(opts Options) *Host {
	if opts.MaxInstances <= 0 {
		opts.MaxInstances = 16
	}
	if opts.ImageScale <= 0 {
		opts.ImageScale = 2
	}
	return &Host{opts: opts, registry: NewDefaultRegistry()}
}

// NewDefaultRegistry は全 Agent Command を登録した登録簿を作る。
//
// CLI が名前の解決とヘルプに使う。処理は Host を通してしか呼ばれないため、
// 登録簿だけを作っても副作用は無い。
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	registerBuiltin(r)
	registerExec(r)
	registerObs(r)
	registerMem(r)
	registerDebug(r)
	registerState(r)
	registerSymbol(r)
	registerGameState(r)
	registerEvents(r)
	registerRunPause(r)
	registerDevLoop(r)
	registerScenario(r)
	registerAnalysis(r)
	return r
}

// Registry は Agent Command の登録簿を返す。
func (h *Host) Registry() *Registry { return h.registry }

// Kind は Host の種類を返す。
func (h *Host) Kind() Kind { return h.opts.Kind }

// Connect は新しい接続を登録する。
func (h *Host) Connect() *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextConn++
	c := &Conn{ID: h.nextConn}
	h.conns = append(h.conns, c)
	return c
}

// Disconnect は接続を外し、その接続が持っていた Control を返す
// （設計書 14 編 §14.4.1）。
func (h *Host) Disconnect(c *Conn) {
	h.mu.Lock()
	h.conns = slices.DeleteFunc(h.conns, func(x *Conn) bool { return x == c })
	insts := slices.Clone(h.instances)
	h.mu.Unlock()
	for _, inst := range insts {
		inst.release(h, c)
		inst.forgetObserved(c)
	}
}

// AttachGUI は GUI 版の Host に表示中のエミュレータを Instance i1 として
// 登録する。人間が Control を持ち、Real-Time で始める。
func (h *Host) AttachGUI(e *emu.Emulator) *Instance {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	inst := &Instance{ID: InstanceID(fmt.Sprintf("i%d", h.nextID)), Emu: e}
	inst.control = controlState{owner: OwnerHuman, mode: ModeRealTime}
	h.instances = append(h.instances, inst)
	h.watchEmulator(inst)
	return inst
}

// ConnInfo は接続の概要（GUI の「接続中のクライアント」）。
type ConnInfo struct {
	ID     ConnID
	Client string
	// Control は接続がいずれかの Instance の Control を持つことを表す。
	Control bool
}

// Connections は session.hello を済ませた接続を返す。
func (h *Host) Connections() []ConnInfo {
	h.mu.Lock()
	conns := slices.Clone(h.conns)
	insts := slices.Clone(h.instances)
	h.mu.Unlock()
	var out []ConnInfo
	for _, c := range conns {
		if !c.Authenticated() {
			continue
		}
		info := ConnInfo{ID: c.ID, Client: c.Client()}
		for _, inst := range insts {
			inst.mu.Lock()
			if inst.control.heldBy(c) {
				info.Control = true
			}
			inst.mu.Unlock()
		}
		out = append(out, info)
	}
	return out
}

// SetROMPath は GUI 版の Instance に、人間が開いた ROM のパスを伝える。
// gamestate.save の保存先を決めるために使う。
func (inst *Instance) SetROMPath(path string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.romPath = path
}

// Instances は Instance を作成順に返す。
func (h *Host) Instances() []*Instance {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.instances)
}

// Instance は ID で引く。
func (h *Host) Instance(id InstanceID) (*Instance, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, inst := range h.instances {
		if inst.ID == id {
			return inst, true
		}
	}
	return nil, false
}

// resolve は引数 instance から対象を決める（設計書 14 編 §14.3.1）。
func (h *Host) resolve(id string) (*Instance, error) {
	if id != "" {
		inst, ok := h.Instance(InstanceID(id))
		if !ok {
			return nil, Errorf(KindInstanceNotFound, "Instance %q が無い", id)
		}
		return inst, nil
	}
	insts := h.Instances()
	switch len(insts) {
	case 0:
		return nil, Errorf(KindInstanceNotFound, "Instance が 1 つも無い")
	case 1:
		return insts[0], nil
	}
	return nil, Errorf(KindInstanceRequired, "Instance が %d 個あるため instance を指定する", len(insts))
}

// Dispatch は Agent Command を 1 つ実行する。
//
// Transport はすべてここを通す。Control・GUI 版での可否・対象の Instance の
// 決定を共通に行う。
func (h *Host) Dispatch(ctx context.Context, conn *Conn, method string, params json.RawMessage) (any, *Error) {
	spec, ok := h.registry.Lookup(method)
	if !ok {
		return nil, Errorf(KindMethodNotFound, "Agent Command %q は無い", method)
	}
	if h.opts.Kind == KindGUI && !spec.GUI {
		return nil, Errorf(KindUnsupportedInGUI, "%s は GUI 版では使えない", method)
	}
	if h.opts.Kind != KindGUI && !spec.Headless {
		return nil, Errorf(KindUnsupportedHeadless, "%s は headless では使えない", method)
	}
	c := &Context{Ctx: ctx, Conn: conn, Host: h, Spec: spec}
	if spec.Target {
		var p InstanceParam
		// 他の引数は処理の側で厳密に読む。ここでは instance だけを見る。
		if len(params) > 0 && string(params) != "null" {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, Errorf(KindInvalidParams, "引数を解釈できない: %v", err)
			}
		}
		inst, err := h.resolve(p.Instance)
		if err != nil {
			return nil, AsError(err)
		}
		c.Instance = inst
	}
	if spec.Class.NeedsControl() {
		if c.Instance == nil {
			return nil, Errorf(KindInternalError, "%s は対象の Instance を持たない", method)
		}
		c.Instance.mu.Lock()
		held := c.Instance.control.heldBy(conn)
		if !held && h.opts.Kind != KindGUI && c.Instance.control.owner == OwnerNone {
			// headless の Instance で誰も Control を持っていなければ、要求した
			// 接続が得る。shogun ctl のように要求ごとに接続し直すクライアントが、
			// control.acquire を送らずに進められるようにするためである。
			c.Instance.control.owner, c.Instance.control.conn = OwnerConn, conn
			held = true
			defer c.Instance.controlChanged(h)
		}
		c.Instance.mu.Unlock()
		if !held {
			return nil, Errorf(KindControlRequired, "%s には Control が要る（control.acquire）", method)
		}
		if spec.Class == ClassMutate && c.Instance.Emu.Status().Movie.Playing {
			// 入力がムービーで決まっている間は書き換えない（設計書 14 編 §14.7.3）。
			return nil, Errorf(KindMovieConflict, "ムービーの再生中は %s を使えない", method)
		}
		// 同じ Instance への進行と書き換えを、接続をまたいで 1 つずつ処理する。
		c.Instance.advance.Lock()
		defer c.Instance.advance.Unlock()
		// 人間が Control を取り返したとき、進行中の要求を取り消せるようにする。
		actx, cancel := context.WithCancel(ctx)
		defer cancel()
		c.Ctx = actx
		c.Instance.mu.Lock()
		c.Instance.advCancel, c.Instance.controlLost = cancel, false
		c.Instance.mu.Unlock()
		defer func() {
			c.Instance.mu.Lock()
			c.Instance.advCancel = nil
			c.Instance.mu.Unlock()
		}()
	}
	var startFrame uint64
	if c.Instance != nil {
		startFrame = c.Instance.Emu.Status().Frames
	}
	res, err := spec.Handler(c, params)
	if c.Instance != nil {
		c.Instance.logCommand(method, params, startFrame, res, err)
		if err == nil {
			c.Instance.noteExportStart(method, params)
		}
	}
	if err != nil {
		return nil, AsError(err)
	}
	return res, nil
}

// InstanceOptions は headless の Instance を作るときの指定。
type InstanceOptions struct {
	Deterministic bool
	RAMInit       string
	RAMSeed       uint64
}

// ramInitPatterns は ram_init に書ける値（設計書 11 編 §11.3.1）。
var ramInitPatterns = []string{"zero", "ff", "pattern", "random"}

// newEmulator は headless の Instance のエミュレータを作って動かし始める。
//
// 音声を出さず、待たずに進め、一時停止した状態で ROM を読み込む
// （設計書 11 編 §11.5.2 と同じ理由）。
func (h *Host) newEmulator(o InstanceOptions) (*emu.Emulator, error) {
	cfg := h.opts.EmuConfig
	cfg.Audio.Enabled = false
	cfg.NewPacer = func(*region.Region) emu.Pacer { return emu.NewNoPacer() }
	cfg.StartPaused = true
	cfg.LoadSymbols = h.loadSymbols
	// 利用者のセーブデータを読み書きしない。読むと結果がセーブデータに
	// 依存して再現できなくなり、書くと利用者のセーブデータを上書きする。
	dir, err := h.batteryDir()
	if err != nil {
		return nil, Errorf(KindIOError, "保存先を作れない: %v", err)
	}
	cfg.Paths.SaveDir = dir
	if o.Deterministic {
		config.ApplyDeterministic(&cfg.Emulation)
	}
	if o.RAMInit != "" {
		if !slices.Contains(ramInitPatterns, o.RAMInit) {
			return nil, Errorf(KindInvalidParams, "ram_init %q は使えない（%s）", o.RAMInit, strings.Join(ramInitPatterns, "・"))
		}
		cfg.Emulation.RAMInitPattern = o.RAMInit
	}
	if o.RAMSeed != 0 {
		cfg.Emulation.RAMSeed = o.RAMSeed
	}
	e := emu.New(cfg)
	e.Start()
	// headless の Instance は常に Agent-Paced で、入力はエージェントが与える。
	e.SetAgentInput(true)
	// headless の Instance では Diagnostic を既定の種類で有効にする（設計書 14 編
	// §14.20.1）。GUI 版では人間の遊びを遅くしないため、diag.configure まで無効とする。
	e.WithDebugger(func(d *debug.Debugger) { d.SetDiagConfig(debug.DefaultDiagConfig()) })
	return e, nil
}

// batteryDir は headless の Instance のバッテリーバックアップの保存先を返す。
func (h *Host) batteryDir() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.saveDir == "" {
		dir, err := os.MkdirTemp("", "shogun-agent-saves-")
		if err != nil {
			return "", err
		}
		h.saveDir = dir
	}
	return h.saveDir, nil
}

// loadSymbols は ROM ごとの Symbols を共有する。
//
// 同じパスの Symbols を一度だけ読み、以降は同じものを返す。参照の数は
// Instance の登録と取り外しで数える。
func (h *Host) loadSymbols(path string) (*debug.Symbols, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.shared {
		if s.path == path {
			return s.symbols, nil
		}
	}
	syms, err := debug.LoadSymbols(path)
	if err != nil {
		syms = debug.NewSymbols()
	}
	h.shared = append(h.shared, &romShared{path: path, symbols: syms})
	return syms, err
}

// attachShared は Instance を読み込んだ ROM の共有物に結び付ける。
func (h *Host) attachShared(inst *Instance) {
	var syms *debug.Symbols
	inst.Emu.WithDebugger(func(d *debug.Debugger) { syms = d.Symbols() })
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.shared {
		if s.symbols == syms {
			s.refs++
			inst.shared = s
			return
		}
	}
}

// detachShared は共有物の参照を外し、最後の参照なら捨てる。
func (h *Host) detachShared(inst *Instance) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := inst.shared
	if s == nil {
		return
	}
	inst.shared = nil
	s.refs--
	if s.refs <= 0 {
		h.shared = slices.DeleteFunc(h.shared, func(x *romShared) bool { return x == s })
	}
}

// register は Instance を登録する。上限を超えるときは誤りを返す。
func (h *Host) register(e *emu.Emulator, owner *Conn) (*Instance, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, Errorf(KindInternalError, "Host は閉じている")
	}
	if len(h.instances) >= h.opts.MaxInstances {
		return nil, Errorf(KindLimitExceeded, "Instance は %d 個までである", h.opts.MaxInstances)
	}
	h.nextID++
	inst := &Instance{ID: InstanceID(fmt.Sprintf("i%d", h.nextID)), Emu: e}
	inst.control = controlState{owner: OwnerConn, conn: owner, mode: ModeAgentPaced}
	h.instances = append(h.instances, inst)
	return inst, nil
}

// reserve は上限に空きがあるかを確かめる。エミュレータを作る前に断るために使う。
func (h *Host) reserve() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.instances) >= h.opts.MaxInstances {
		return Errorf(KindLimitExceeded, "Instance は %d 個までである", h.opts.MaxInstances)
	}
	return nil
}

// CreateInstance は headless の Instance を作り、ROM を読み込む。
// 作った接続が Control を持つ（設計書 14 編 §14.4.1）。
func (h *Host) CreateInstance(owner *Conn, rom, statePath string, o InstanceOptions) (*Instance, error) {
	if err := h.reserve(); err != nil {
		return nil, err
	}
	var data []byte
	if rom != "" {
		var err error
		data, err = os.ReadFile(rom)
		if err != nil {
			return nil, Errorf(KindIOError, "ROM を読めない: %v", err)
		}
	}
	e, err := h.newEmulator(o)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(rom), filepath.Ext(rom))
	if data != nil {
		if err := e.LoadROMDataFrom(data, name, rom); err != nil {
			e.Stop()
			return nil, Errorf(KindInvalidParams, "ROM を読み込めない: %v", err)
		}
		if statePath != "" {
			if err := e.LoadFromFile(statePath); err != nil {
				e.Stop()
				return nil, Errorf(KindIOError, "セーブステートを読み込めない: %v", err)
			}
		}
	} else if statePath != "" {
		e.Stop()
		return nil, Errorf(KindInvalidParams, "state を使うには rom が要る")
	}
	inst, err := h.register(e, owner)
	if err != nil {
		e.Stop()
		return nil, err
	}
	inst.romName, inst.romPath, inst.romData, inst.opts = name, rom, data, o
	if data != nil {
		h.attachShared(inst)
	}
	// scenario.export の開始。セーブステートから始めたときはその状態を持つ。
	inst.setExportStart(inst.newAnchor(rom, statePath != ""))
	h.watchEmulator(inst)
	return inst, nil
}

// CloseInstance は Instance を閉じる。
func (h *Host) CloseInstance(inst *Instance) {
	inst.stopWatch()
	inst.Emu.SetObserver(nil)
	h.PushEvent(inst, EventInstanceClosed, inst.Emu.Status().Frames, nil)
	h.mu.Lock()
	h.instances = slices.DeleteFunc(h.instances, func(x *Instance) bool { return x == inst })
	h.mu.Unlock()
	inst.mu.Lock()
	inst.closed = true
	inst.control = controlState{}
	inst.mu.Unlock()
	inst.Emu.Stop()
	h.detachShared(inst)
}

// Close は全 Instance を止める。GUI 版の Instance のエミュレータは
// GUI が止めるため止めない。
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	insts := slices.Clone(h.instances)
	h.mu.Unlock()
	if h.opts.Kind == KindGUI {
		return
	}
	for _, inst := range insts {
		h.CloseInstance(inst)
	}
	h.mu.Lock()
	dir := h.saveDir
	h.saveDir = ""
	h.mu.Unlock()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// acquire は conn に Control を持たせる（設計書 14 編 §14.4.1）。
func (inst *Instance) acquire(h *Host, conn *Conn) error {
	inst.mu.Lock()
	c := &inst.control
	switch c.owner {
	case OwnerConn:
		if c.conn == conn {
			inst.mu.Unlock()
			return nil
		}
		other := c.conn
		inst.mu.Unlock()
		return Errorf(KindControlHeld, "接続 #%d（%s）が Control を持っている", other.ID, other.Client())
	case OwnerHuman:
		// 人間から奪う。Agent-Paced にして止め、入力をエージェントに切り替える。
		inst.Emu.Pause()
		inst.Emu.SetAgentInput(true)
	}
	c.owner, c.conn, c.mode = OwnerConn, conn, ModeAgentPaced
	inst.mu.Unlock()
	inst.controlChanged(h)
	return nil
}

// release は conn が持つ Control を返す。GUI 版では人間に戻して Real-Time に
// する（取り返す直前の一時停止の状態は保たない。設計書 14 編 §14.4.2）。
// conn が持っていないときは何もせず false を返す。
func (inst *Instance) release(h *Host, conn *Conn) bool {
	inst.mu.Lock()
	c := &inst.control
	if !c.heldBy(conn) {
		inst.mu.Unlock()
		return false
	}
	if h.opts.Kind == KindGUI {
		c.owner, c.conn, c.mode = OwnerHuman, nil, ModeRealTime
		inst.mu.Unlock()
		inst.Emu.SetAgentInput(false)
		inst.Emu.Resume()
	} else {
		c.owner, c.conn = OwnerNone, nil
		inst.mu.Unlock()
	}
	inst.controlChanged(h)
	return true
}

// TakeBack は GUI の人間が Control を取り返す（設計書 14 編 §14.4.1）。
// 取り返したとき true を返す。進行中のエージェントの要求は
// stop_reason: control_lost で終える。
func (h *Host) TakeBack(inst *Instance) bool {
	inst.mu.Lock()
	c := &inst.control
	if c.owner != OwnerConn {
		inst.mu.Unlock()
		return false
	}
	c.owner, c.conn, c.mode = OwnerHuman, nil, ModeRealTime
	cancel := inst.advCancel
	if cancel != nil {
		inst.controlLost = true
	}
	inst.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// 進行中の要求が止まってから走らせる。先に走らせると、取り消しの一時停止で
	// 止まってしまう。UI スレッドを待たせないよう、別のゴルーチンで行う。
	go func() {
		inst.advance.Lock()
		defer inst.advance.Unlock()
		inst.Emu.SetAgentInput(false)
		inst.Emu.Resume()
	}()
	inst.controlChanged(h)
	return true
}

// controlChanged は Control の変化をイベントに積み、GUI へ知らせる。
func (inst *Instance) controlChanged(h *Host) {
	st := inst.ControlStatus()
	h.PushEvent(inst, EventControlChanged, inst.Emu.Status().Frames, st)
	inst.mu.Lock()
	fn := inst.onControl
	inst.mu.Unlock()
	if fn != nil {
		fn(st)
	}
}

// SetOnControl は Control の持ち主が変わったときの知らせ先を設定する（GUI の
// バナー）。UI スレッド以外から呼ばれる。
func (inst *Instance) SetOnControl(fn func(ControlStatus)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onControl = fn
}

// takeControlLost は進行中に Control を取り返されたかを返し、印を消す。
func (inst *Instance) takeControlLost() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	lost := inst.controlLost
	inst.controlLost = false
	return lost
}

// setMode は進行モードを変える。
func (inst *Instance) setMode(m Mode) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.control.mode = m
}

// ControlStatus は Control の状態を返す。
func (inst *Instance) ControlStatus() ControlStatus {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.control.status()
}
