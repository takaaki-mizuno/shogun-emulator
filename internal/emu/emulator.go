// Package emu はエミュレーションコアの駆動と、映像・音声・入力の
// 受け渡しを担う。
package emu

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// commandQueueSize はコマンドキューの大きさ。
//
// UI スレッドがコマンドを送ってブロックしないだけの余裕を持たせる。
// 1 フレームの間に送られるコマンドは、入力の変化とホットキーを合わせても
// 数個である。
const commandQueueSize = 64

// Config は Emulator の設定。
type Config struct {
	// Emulation は ROM の読み込みと電源投入に関わる設定。
	Emulation config.EmulationConfig
	// Input はポートに接続するデバイスの設定。
	Input config.InputConfig
	// Audio は音声出力の設定。Enabled が false のとき音を出さない。
	Audio config.AudioConfig
	// Paths は保存先の設定。空の項目は既定の場所を使う。
	Paths config.PathsConfig
	// State はセーブステートと巻き戻しの設定。
	State config.StateConfig
	// Movie は入力ムービーの設定。
	Movie config.MovieConfig
	// AppName は音量調整の UI に出す名前。
	AppName string
	// Version と Commit はセーブステートのヘッダに書く識別情報。
	Version string
	Commit  string
	// Debug はデバッガの設定。
	Debug config.DebugConfig
	// Dirs は保存先のディレクトリ（設計書 11 編 §11.2）。Data が空のとき
	// 既定の場所を使う。
	Dirs config.Paths
	// SymbolsDir と TraceDir は名前とトレースの保存先。空のとき Dirs から決める。
	SymbolsDir string
	TraceDir   string
	// PatchesDir はオーバーレイの保存先。空のとき Dirs から決める。
	PatchesDir string
	// LogWriter はログの出力先。nil のときログビューア向けの保持だけを行う。
	LogWriter io.Writer
	// StartPaused は ROM を読み込んだときに一時停止した状態で始めることを
	// 表す。headless 実行が、進めるフレーム数を正確に数えるために使う。
	StartPaused bool
	// OnBreak はブレークポイントで止まったときに呼ばれる。nil のとき
	// 呼ばない。エミュレーションゴルーチンから呼ばれる。
	OnBreak func(info debug.BreakInfo)
	// Notify は利用者へ短い知らせを伝える。nil のとき伝えない。
	//
	// エミュレーションゴルーチンから呼ばれる。UI を直接触らないこと。
	Notify func(msg string)
	// Warn は続行できる不具合を知らせる。nil のとき知らせない。
	//
	// ROM を読み込むゴルーチンとエミュレーションゴルーチンの両方から
	// 呼ばれる。複数のゴルーチンから安全に呼べる実装を渡すこと。
	// UI を直接触らないこと。
	Warn func(format string, args ...any)
	// NewPacer は進行の待ち方を作る。
	//
	// nil のときは音声の有無で決める。音声が有効なら、リングバッファの
	// 高水位が待ちを担うため待たない Pacer を使う。無効なら壁時計で待つ。
	NewPacer func(r *region.Region) Pacer
	// LoadSymbols は ROM ごとの名前とブレークポイントを読む。nil のとき
	// ファイルから読む（debug.LoadSymbols）。
	//
	// 同じ ROM を読み込んだ複数の Instance が 1 つの Symbols を共有する
	// ために使う（設計書 14 編 §14.3.1）。別々に読むと、終了時に互いの
	// 内容で上書きし合う。
	LoadSymbols func(path string) (*debug.Symbols, error)
}

// Status は UI へ見せる実行状態。
type Status struct {
	// Loaded は ROM が読み込まれているかを表す。
	Loaded bool
	// Paused は一時停止中かを表す。
	Paused bool
	// ROMName は読み込んでいる ROM のファイル名。
	ROMName string
	// ROMKey は ROM ハッシュから作った識別子。保存先の名前に使う。
	ROMKey string
	// Slot は選択中のセーブステートのスロット番号。
	Slot int
	// Movie はムービーの記録・再生の状態。
	Movie MovieStatus
	// Video は録画の状態（設計書 08 編 §8.8.4）。
	Video VideoStatus
	// Rewinding は巻き戻し中かを表す。
	Rewinding bool
	// Break は直近にブレークポイントで止まった理由。止まっていないとき空。
	Break string
	// MidInstruction は命令の途中で止まっていることを表す。
	MidInstruction bool
	// MapperName はマッパーの名前。
	MapperName string
	// MapperNumber はマッパー番号。
	MapperNumber uint16
	// RegionName はリージョンの名前。
	RegionName string
	// AudioEnabled は音声が出ているかを表す。
	AudioEnabled bool
	// Muted は消音中かを表す。
	Muted bool
	// AudioFill と AudioHigh はリングバッファの充填量と高水位。
	//
	// 充填量が高水位のまま保たれていれば、進行がオーディオに追従して
	// いる。減り続けていれば処理が追いついていない。
	AudioFill, AudioHigh int
	// AudioUnderruns はリングが空のまま読まれた回数。音切れの回数である。
	AudioUnderruns uint64
	// PictureHeight は表示する画の高さ。リージョンによって異なる。
	PictureHeight int
	// Speed は速度倍率。
	Speed float64
	// Frames と Cycles は電源投入からの累積。
	Frames uint64
	Cycles uint64
}

// MovieStatus は入力ムービーの状態。
type MovieStatus struct {
	// Recording は記録中かを表す。
	Recording bool
	// Playing は再生中かを表す。
	Playing bool
	// Frame は記録または再生が進んだフレーム数。
	Frame uint64
	// Total は再生するムービーの総フレーム数。
	Total uint64
	// Rerecords は再記録回数。
	Rerecords uint64
}

// Active はムービーを扱っているかを返す。
func (m MovieStatus) Active() bool { return m.Recording || m.Playing }

// Emulator はエミュレーションの実行を管理する。
//
// エミュレーション状態（*nes.NES）を触るのはエミュレーションゴルーチン
// だけである。外部からの操作はすべてコマンドキューを経由し、命令境界で
// 処理される。
type Emulator struct {
	cfg Config

	// Input は UI スレッドと共有する押下状態。
	Input InputState
	// Frames は完成したフレームの受け渡し。
	Frames *FrameBuffer

	// audio は音声の経路。音声を出さないとき nil。
	audio *audioPipeline

	// cmds はコマンドキュー。
	cmds chan command
	// stop はゴルーチンの停止を伝える。
	stop chan struct{}
	// done はゴルーチンが終わったことを伝える。
	done chan struct{}
	// stopOnce は多重の停止を防ぐ。
	stopOnce sync.Once

	// statusMu は status と started と projectNotes を守る。
	statusMu     sync.Mutex
	status       Status
	started      bool
	projectNotes []string

	// desyncMu は desyncErr を守る。
	desyncMu  sync.Mutex
	desyncErr error

	// audioErr は音声の初期化に失敗した理由。
	audioErr error

	// 以下はエミュレーションゴルーチンだけが触る。
	machine *nes.NES
	// battery は不揮発メモリの保存先。持たない ROM では nil。
	battery *battery
	// latch はフレームの開始時に取り込んだ押下状態。
	latch frameLatch
	// recorder はムービーの記録。記録していないとき nil。
	recorder *movie.Recorder
	// video は録画（設計書 08 編 §8.8）。nil のとき録画していない。
	video *videoRecorder
	// videoErr は書き込みの失敗で録画が止まったときのエラー。次の
	// StopRecordingVideo で返して消す。次に録画を始めたときも消す。
	// エミュレーションゴルーチンだけが触る。
	videoErr error
	// recordPath は記録の書き出し先。
	recordPath string
	// player はムービーの再生。再生していないとき nil。
	player *movie.Player
	// rewind は巻き戻しの記録。無効のとき nil。
	rewind *rewindBuffer
	// silent は前方再生中であることを表す。映像と音声を出さない。
	silent bool
	// rewinding は押している間の巻き戻しが続いていることを表す。
	rewinding bool
	// dbg はデバッガ。ROM を読み込むたびに本体を渡し直す。
	dbg *debug.Debugger
	// debug はデバッガに関わる状態。
	debug debugState
	// gate は命令の途中で止まっている間の受け渡し。
	gate cycleGate
	// until はステップ実行で止める条件。nil のとき止めない。
	until func() bool
	// traceFile はトレースの常時出力先。出力していないとき nil。
	traceFile *os.File
	// apuMute は APU のミュートのビット。ROM を読み込み直しても保つ。
	apuMute uint8
	// turbo は連射の状態。
	turbo turboState
	// startupBreaks は ROM を読み込むたびに置く実行ブレークポイント。
	startupBreaks []uint16
	// lastFrame は直前に完成したフレーム。セーブステートに添える
	// スクリーンショットを作るために保持する。
	lastFrame *video.Frame
	// hasFrame は lastFrame に内容が入っていることを表す。
	hasFrame bool
	// powerOnCycles は電源を入れた直後の累積サイクル数。
	//
	// ムービーの記録を電源投入から始められるかの判定に使う。
	powerOnCycles uint64
	pacer         Pacer
	speed         float64
	paused        bool
	// pending は進行の結果を待っている者への知らせ先。待っていないとき nil。
	pending chan StepResult
	// agentInput はエージェントが与える入力。nil のときキーボードの入力を
	// 使う（設計書 14 編 §14.4.2）。
	agentInput *[2]uint8
	// framePending はフレームの開始時の処理を遅らせていることを表す。
	framePending bool
	// stepCond と stepCondInstr は進行を止める条件（cmdStep.cond）。
	stepCond      func() bool
	stepCondInstr bool
	// ignoreBreaks は進行の間ブレークポイントで止まらないことを表す。
	ignoreBreaks bool

	// journal は常時記録。エミュレーションゴルーチンだけが触る。
	journal journalState
	// incoming は再生中のこのフレームの介入。フレームの開始の記録の後で受け取る。
	incoming []movie.Record

	// observer は Agent Interface のイベントの知らせ先（SetObserver）。
	observer atomic.Pointer[Observer]

	// cancelStep は進行の取り消しを伝える。StepAndWait の呼び出し側が
	// 立て、コマンドの処理の中で進めている間にも気付けるようにする。
	cancelStep atomic.Bool
}

// New は Emulator を作る。ゴルーチンはまだ起動しない。
//
// 音声の初期化に失敗したときは音声を無効にして続ける。音が出ないことで
// 起動しない状態を作らない（設計書 01 編 §1.9）。失敗した理由は
// AudioError で取り出せる。
func New(cfg Config) *Emulator {
	if cfg.Dirs.Data == "" {
		// 保存先を与えられなかったときは既定の場所を使う。ここでは
		// ディレクトリを作らず、書き込むときに作る。
		if p, err := config.DefaultPaths(); err == nil {
			cfg.Dirs = p
		}
	}
	e := &Emulator{
		cfg:       cfg,
		Frames:    NewFrameBuffer(),
		lastFrame: video.NewFrame(),
		gate:      newCycleGate(),
		cmds:      make(chan command, commandQueueSize),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		speed:     1.0,
		pacer:     NewNoPacer(),
	}
	e.status.Speed = 1.0
	e.dbg = newDebugger(cfg)
	e.dbg.DiagHook = func(x debug.Diagnostic) {
		if o := e.observer.Load(); o != nil && o.OnDiagnostic != nil {
			o.OnDiagnostic(x)
		}
	}

	if cfg.Audio.Enabled {
		p, err := newAudioPipeline(cfg.Audio, cfg.AppName)
		if err != nil {
			e.audioErr = err
		} else {
			e.audio = p
		}
	}
	if e.cfg.NewPacer == nil {
		e.cfg.NewPacer = e.defaultPacer
	}
	e.status.AudioEnabled = e.audio != nil
	return e
}

// defaultPacer は音声の有無から進行の待ち方を決める。
//
// 音声が有効なときは待たない Pacer を返す。オーディオリングバッファの
// 高水位が書き込み側を止め、それが進行の駆動になるためである。両方で
// 待つと、遅いほうに合わせて進行が乱れる。
func (e *Emulator) defaultPacer(r *region.Region) Pacer {
	if e.audio != nil {
		return NewNoPacer()
	}
	return NewWallClockPacer(r)
}

// Dirs は保存先のディレクトリを返す。
func (e *Emulator) Dirs() config.Paths { return e.cfg.Dirs }

// SetOnBreak はブレークポイントで止まったときの知らせ先を差し替える。
//
// エミュレーションゴルーチンを起動する前に呼ぶ。
func (e *Emulator) SetOnBreak(fn func(info debug.BreakInfo)) { e.cfg.OnBreak = fn }

// Observer は Agent Interface がイベント（設計書 14 編 §14.14）を積むための
// 知らせ先。すべてエミュレーションゴルーチンから呼ばれる。待たずに戻ること。
type Observer struct {
	// OnBreak はブレークポイントで止まったときに呼ばれる。
	OnBreak func(info debug.BreakInfo, frame uint64)
	// OnLoad は ROM を読み込んだときに呼ばれる。
	OnLoad func(name string)
	// OnDesync はムービーの再生で desync を検出したときに呼ばれる。
	OnDesync func(err error, frame uint64)
	// OnDiagnostic は Diagnostic の種類と位置の組の最初の 1 件で呼ばれる
	// （設計書 14 編 §14.20.3）。エミュレーションゴルーチンから呼ばれる。
	OnDiagnostic func(x debug.Diagnostic)
}

// SetObserver は知らせ先を差し替える。nil で外す。いつ呼んでもよい。
//
// GUI の知らせ（Config.OnBreak）とは別に持つ。GUI 版で Agent Interface を
// 有効にしたり無効にしたりしても、GUI の知らせを変えないためである。
func (e *Emulator) SetObserver(o *Observer) { e.observer.Store(o) }

// SetNotify は知らせの送り先を差し替える。
//
// エミュレーションゴルーチンを起動する前に呼ぶ。画面を組み立てた後に
// 差し込めるようにするために用意する。
func (e *Emulator) SetNotify(fn func(msg string)) { e.cfg.Notify = fn }

// AudioError は音声の初期化に失敗した理由を返す。成功したとき nil。
func (e *Emulator) AudioError() error { return e.audioErr }

// Start はエミュレーションゴルーチンを起動する。
//
// 2 回目以降の呼び出しは何もしない。停止した後は起動しない。
func (e *Emulator) Start() {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	if e.started || e.stopped() {
		return
	}
	e.started = true
	go e.run()
}

// stopped は停止を指示されたかを返す。
func (e *Emulator) stopped() bool {
	select {
	case <-e.stop:
		return true
	default:
		return false
	}
}

// Stop はエミュレーションゴルーチンを止め、終わるまで待つ。
func (e *Emulator) Stop() {
	e.stopOnce.Do(func() {
		close(e.stop)
		// リングの高水位で待っているかもしれない。先に解く。
		if e.audio != nil {
			e.audio.close()
		}
	})

	e.statusMu.Lock()
	started := e.started
	e.statusMu.Unlock()
	if started {
		<-e.done
	}
}

// Status は現在の実行状態を返す。UI スレッドが呼ぶ。
func (e *Emulator) Status() Status {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	return e.status
}

// send はコマンドを送る。停止済みのときは false を返す。
func (e *Emulator) send(c command) bool {
	select {
	case <-e.stop:
		return false
	default:
	}
	select {
	case e.cmds <- c:
		// 命令の途中で止まっているときは待ちを解く。命令を最後まで実行
		// させ、命令境界でこのコマンドを処理する（設計書 09 編 §9.5）。
		e.gate.release()
		return true
	case <-e.stop:
		return false
	}
}

// LoadROM は ROM を読み込む。
//
// 本体の組み立てを呼び出し側のゴルーチンで行い、出来上がったものを
// コマンドで渡す。ファイルの読み込みで実行中のエミュレーションを
// 止めないためである。
func (e *Emulator) LoadROM(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if err := e.LoadROMDataFrom(data, name, path); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// LoadROMData は読み込み済みの ROM の内容を読み込む。name は表示に使う名前。
//
// Agent Interface の Fork が、元の Instance と同じ内容を読み込むために使う
// （設計書 14 編 §14.3.2）。ファイルを読み直すと、ビルドで書き換わった
// 別の内容を読むおそれがある。
func (e *Emulator) LoadROMData(data []uint8, name string) error {
	return e.LoadROMDataFrom(data, name, "")
}

// LoadROMDataFrom は読み込み済みの ROM の内容を、元のファイルのパスと共に
// 読み込む。パスから同じディレクトリの .dbg と Game State Definition を探す
// （設計書 14 編 §14.11.2、§14.12.1）。
func (e *Emulator) LoadROMDataFrom(data []uint8, name, path string) error {
	m, bat, err := e.buildMachine(data)
	if err != nil {
		return err
	}
	return e.apply(cmdLoadMachine{machine: m, battery: bat, name: name, path: path, done: make(chan error, 1)})
}

// ProjectNotes は直前の ROM の読み込みで、.dbg と Game State Definition に
// ついて知らせることを返す。
func (e *Emulator) ProjectNotes() []string {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	return append([]string(nil), e.projectNotes...)
}

// buildMachine は ROM のデータから本体を組み立て、電源を入れる。
//
// 不揮発メモリを持つ ROM では、保存先を開いて内容を読み込む。
// ファイルの読み書きを呼び出し側のゴルーチンで行い、実行中の
// エミュレーションを止めない。
func (e *Emulator) buildMachine(data []uint8) (*nes.NES, *battery, error) {
	rom, err := cart.LoadROM(data)
	if err != nil {
		return nil, nil, err
	}
	r, err := e.resolveRegion(rom)
	if err != nil {
		return nil, nil, err
	}
	m, err := nes.NewWithOptions(rom, r, nes.Options{
		Cart: cart.Options{
			BusConflicts:   e.cfg.Emulation.BusConflicts,
			MMC3IRQVariant: e.cfg.Emulation.MMC3IRQVariant,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if e.cfg.Version != "" {
		m.Version = e.cfg.Version
	}
	if e.cfg.Commit != "" {
		m.Commit = e.cfg.Commit
	}
	if e.cfg.Input.Port1Device != config.DeviceNone {
		// コントローラはフレームの開始時にラッチした値を読む。
		m.ConnectStandardControllers(&e.latch)
	}
	m.Bus.SetDMCRegisterConflicts(e.cfg.Emulation.DMCDMARegisterConflicts)
	m.APU.SetVolumes(volumes(e.cfg.Audio.ChannelVolumes))
	m.APU.SetSilenceUltrasonicTriangle(e.cfg.Audio.SilenceUltrasonicTriangle)
	// ヘッダの解釈で補正した点を知らせる。4 画面ビットを扱えない
	// マッパーで立っている場合などがここに現れる。
	for _, w := range rom.Warnings {
		e.warn("ROM のヘッダ: %s", w)
	}

	// 電源を入れる前にオーバーレイを当てる。リセットベクタを書き換えた
	// パッチも電源投入から効くようにする。
	if err := loadOverlay(e.patchesPath(rom), rom); err != nil {
		e.warn("%v", err)
	}

	init, err := e.initState()
	if err != nil {
		return nil, nil, err
	}
	m.PowerOn(init)

	bat, err := e.openBattery(m)
	if err != nil {
		return nil, nil, err
	}
	return m, bat, nil
}

// openBattery は不揮発メモリの保存先を開く。
//
// 持たない ROM では nil を返す。
func (e *Emulator) openBattery(m *nes.NES) (*battery, error) {
	return newBattery(e.cfg.Dirs.SaveDir(e.cfg.Paths.SaveDir), m.ROM.Hash[:], m.Cart)
}

// resolveRegion は設定と ROM のヘッダからリージョンを決める。
func (e *Emulator) resolveRegion(rom *cart.ROM) (*region.Region, error) {
	switch e.cfg.Emulation.Region {
	case "", config.RegionAuto:
		return nes.RegionForROM(rom), nil
	case config.RegionNTSC:
		return region.NTSC, nil
	case config.RegionPAL:
		return region.PAL, nil
	case config.RegionDendy:
		return region.Dendy, nil
	}
	return nil, fmt.Errorf("emu: 知らないリージョン %q", e.cfg.Emulation.Region)
}

// initState は設定から電源投入時の状態を決める。
func (e *Emulator) initState() (nes.InitState, error) {
	pattern := state.PatternRandom
	if s := e.cfg.Emulation.RAMInitPattern; s != "" {
		p, ok := state.ParsePattern(s)
		if !ok {
			return nes.InitState{}, fmt.Errorf("emu: 知らない RAM 初期化パターン %q", s)
		}
		pattern = p
	}
	seed := e.cfg.Emulation.RAMSeed
	if seed == 0 && pattern == state.PatternRandom {
		seed = freshSeed()
	}
	return nes.InitState{
		RAMPattern:      pattern,
		RAMSeed:         seed,
		CPUPPUAlignment: e.cfg.Emulation.CPUPPUAlignment,
		DMAGetPutPhase:  e.cfg.Emulation.DMAGetPutPhase,
		PPUVBlankFlag:   e.cfg.Emulation.PPUVBlankFlag,
	}, nil
}

// freshSeed は起動ごとに違う種を返す。
//
// 種はセーブステートと入力ムービーに記録されるため、再現性は失われない。
// crypto/rand を使うのは、種の生成が 1 回きりであり、決定論の規約が
// 対象とするエミュレーションの経路に含まれないためである。
func freshSeed() uint64 {
	var b [8]uint8
	if _, err := rand.Read(b[:]); err != nil {
		// 乱数を取れないときも起動を止めない。
		return 1
	}
	return binary.LittleEndian.Uint64(b[:])
}

// Unload は ROM を取り外す。
func (e *Emulator) Unload() error {
	return e.apply(cmdUnload{done: make(chan error, 1)})
}

// Reset はリセットする。hard が true のとき電源を入れ直す。
func (e *Emulator) Reset(hard bool) error {
	return e.apply(cmdReset{hard: hard, done: make(chan error, 1)})
}

// apply は完了を待つコマンドを送り、結果を返す。
//
// 完了を伝えるチャネルは commandDone が返す。コマンドを増やしたときに
// ここへの追加を忘れると、結果を待ったまま戻らなくなる。
func (e *Emulator) apply(c command) error {
	done := commandDone(c)
	if done == nil {
		return fmt.Errorf("emu: 完了を待てないコマンドである（%T）", c)
	}
	if !e.send(c) {
		return errors.New("emu: エミュレーションが停止している")
	}
	select {
	case err := <-done:
		return err
	case <-e.done:
		return errors.New("emu: エミュレーションが停止した")
	}
}

// Pause は一時停止する。
func (e *Emulator) Pause() { e.send(cmdPause{paused: true}) }

// Resume は再開する。
func (e *Emulator) Resume() { e.send(cmdPause{paused: false}) }

// SetPaused は一時停止と再開を切り替える。
func (e *Emulator) SetPaused(p bool) { e.send(cmdPause{paused: p}) }

// FrameAdvance は一時停止したまま 1 フレーム進める。
func (e *Emulator) FrameAdvance() { e.send(cmdStep{kind: StepFrame, count: 1}) }

// StepFrames は一時停止したまま count フレーム進める。
func (e *Emulator) StepFrames(count int) { e.send(cmdStep{kind: StepFrame, count: count}) }

// StepInstruction は一時停止したまま 1 命令進める。
func (e *Emulator) StepInstruction() { e.send(cmdStep{kind: StepInstruction, count: 1}) }

// SetSpeed は速度倍率を変える。
func (e *Emulator) SetSpeed(factor float64) { e.send(cmdSetSpeed{factor: factor}) }

// SetMuted は消音を切り替える。
func (e *Emulator) SetMuted(muted bool) { e.send(cmdSetMuted{muted: muted}) }

// SetInput はポートの押下状態を命令境界で設定する。
func (e *Emulator) SetInput(port int, buttons uint8) {
	e.send(cmdSetInput{port: port, buttons: buttons})
}

// SaveState は命令境界で状態を直列化して返す。
func (e *Emulator) SaveState() ([]byte, error) {
	data, _, err := e.saveState(false)
	return data, err
}

// SaveStateWithScreenshot は保存時の画面を添えて状態を直列化して返す。
func (e *Emulator) SaveStateWithScreenshot() ([]byte, error) {
	data, _, err := e.saveState(true)
	return data, err
}

// saveState は命令境界で状態を直列化し、保存した位置の PC とともに返す。
//
// PC を返すのは、サイクル単位ステップで命令の途中に止まっているときに
// 「命令境界まで進めてから保存した」ことを利用者へ伝えるためである
// （設計書 08 編 §8.3.2）。
func (e *Emulator) saveState(screenshot bool) ([]byte, uint16, error) {
	out := make(chan saveResult, 1)
	if !e.send(cmdSaveState{out: out, screenshot: screenshot}) {
		return nil, 0, errors.New("emu: エミュレーションが停止している")
	}
	select {
	case r := <-out:
		return r.data, r.pc, r.err
	case <-e.done:
		return nil, 0, errors.New("emu: エミュレーションが停止した")
	}
}

// LoadState は命令境界で状態を復元する。
func (e *Emulator) LoadState(data []byte) error {
	return e.apply(cmdLoadState{data: data, done: make(chan error, 1)})
}

// WithMachine は命令境界で本体を参照する処理を実行し、完了を待つ。
//
// スクリーンショットとデバッガの表示が、命令の途中ではない一貫した
// 状態を読むために使う。渡した関数の中で本体を保持しない。
//
// Start の前に呼ばれたときは、呼び出し元でその場で実行する。エミュレーション
// ゴルーチンがまだ無く、処理を待つ相手がいないためである。GUI の組み立て
// （連射の設定など）は Start の前に行われる。Start と同じゴルーチンから呼ぶ。
func (e *Emulator) WithMachine(fn func(*nes.NES)) bool {
	e.statusMu.Lock()
	started := e.started
	e.statusMu.Unlock()
	if !started && !e.stopped() {
		fn(e.machine)
		return true
	}
	done := make(chan struct{})
	if e.gate.active.Load() {
		// 命令の途中で止まっている。待ちの中で実行する。
		select {
		case e.gate.req <- gateRequest{fn: func() { fn(e.machine) }, done: done}:
			select {
			case <-done:
				return true
			case <-e.done:
				return false
			}
		case <-e.done:
			return false
		}
	}
	if !e.send(cmdFunc{fn: fn, done: done}) {
		return false
	}
	select {
	case <-done:
		return true
	case <-e.done:
		return false
	}
}
