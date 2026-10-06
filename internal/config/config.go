package config

// Version は設定ファイルの形式のバージョン。移行は migrate.go に置く。
const Version = 2

// Config はアプリケーション全体の設定。
//
// JSON のタグを全フィールドに付け、omitempty を付けない。設定ファイルを
// 開いた利用者が設定できる項目を一覧できるようにするためである。
type Config struct {
	Version   int             `json:"version"`
	Emulation EmulationConfig `json:"emulation"`
	Video     VideoConfig     `json:"video"`
	Audio     AudioConfig     `json:"audio"`
	Input     InputConfig     `json:"input"`
	Paths     PathsConfig     `json:"paths"`
	Debug     DebugConfig     `json:"debug"`
	State     StateConfig     `json:"state"`
	Movie     MovieConfig     `json:"movie"`
	UI        UIConfig        `json:"ui"`
	Agent     AgentConfig     `json:"agent"`

	// noSave は新しいバージョンの設定ファイルを読んだため保存しないことを表す。
	noSave bool
}

// EmulationConfig はエミュレーションの挙動の設定。
type EmulationConfig struct {
	Region                  string `json:"region"`
	RAMInitPattern          string `json:"ramInitPattern"`
	RAMSeed                 uint64 `json:"ramSeed"`
	CPUPPUAlignment         int    `json:"cpuPpuAlignment"`
	DMAGetPutPhase          int    `json:"dmaGetPutPhase"`
	PPUVBlankFlag           bool   `json:"ppuVBlankFlag"`
	MMC3IRQVariant          string `json:"mmc3IrqVariant"`
	BusConflicts            string `json:"busConflicts"`
	DMCDMARegisterConflicts bool   `json:"dmcDmaRegisterConflicts"`
}

// VideoConfig は映像出力の設定。
type VideoConfig struct {
	Scale                 int    `json:"scale"`
	IntegerScale          bool   `json:"integerScale"`
	AspectRatioCorrection bool   `json:"aspectRatioCorrection"`
	Filter                string `json:"filter"`
	OverscanTop           int    `json:"overscanTop"`
	OverscanBottom        int    `json:"overscanBottom"`
	OverscanLeft          int    `json:"overscanLeft"`
	OverscanRight         int    `json:"overscanRight"`
	PaletteFile           string `json:"paletteFile"`
	Fullscreen            bool   `json:"fullscreen"`
}

// AudioConfig は音声出力の設定。
type AudioConfig struct {
	Enabled                   bool               `json:"enabled"`
	SampleRate                int                `json:"sampleRate"`
	BufferMilliseconds        int                `json:"bufferMilliseconds"`
	RingHighWaterMultiplier   int                `json:"ringHighWaterMultiplier"`
	MasterVolume              float64            `json:"masterVolume"`
	ChannelVolumes            map[string]float64 `json:"channelVolumes"`
	FilterProfile             string             `json:"filterProfile"`
	MuteOnFastForward         bool               `json:"muteOnFastForward"`
	SilenceUltrasonicTriangle bool               `json:"silenceUltrasonicTriangle"`
}

// InputConfig は入力デバイスの設定。キーバインドは別のファイルに置く。
type InputConfig struct {
	Port1Device string `json:"port1Device"`
	Port2Device string `json:"port2Device"`
	TurboRateHz int    `json:"turboRateHz"`
}

// PathsConfig は保存先の設定。
//
// 空の値はそのパスの既定の場所を意味する。
type PathsConfig struct {
	ROMDir        string `json:"romDir"`
	SaveDir       string `json:"saveDir"`
	StateDir      string `json:"stateDir"`
	ScreenshotDir string `json:"screenshotDir"`
	LogDir        string `json:"logDir"`
	MovieDir      string `json:"movieDir"`
}

// DebugConfig はデバッグ出力の設定。
type DebugConfig struct {
	LogCategories []string `json:"logCategories"`
	// LogOutput はログの出力先。none・stderr・stdout・file。
	LogOutput string `json:"logOutput"`
	// LogMaxBytes と LogGenerations はファイル出力のローテーションの設定。
	LogMaxBytes       int64 `json:"logMaxBytes"`
	LogGenerations    int   `json:"logGenerations"`
	TraceRingSize     int   `json:"traceRingSize"`
	ChangeDecayFrames int   `json:"changeDecayFrames"`
	MemoryEditWrite   bool  `json:"memoryEditWrite"`
	// BreakOnUninitializedRAMRead は ROM を読み込むたびに未初期化 RAM の
	// 読み出しのイベントブレークポイントを置く。
	BreakOnUninitializedRAMRead bool `json:"breakOnUninitializedRamRead"`
}

// StateConfig はセーブステートと巻き戻しの設定。
type StateConfig struct {
	Slots                int  `json:"slots"`
	RewindEnabled        bool `json:"rewindEnabled"`
	RewindSeconds        int  `json:"rewindSeconds"`
	RewindIntervalFrames int  `json:"rewindIntervalFrames"`
	SaveScreenshot       bool `json:"saveScreenshot"`
}

// MovieConfig は入力ムービーの設定。
type MovieConfig struct {
	ChecksumIntervalFrames int  `json:"checksumIntervalFrames"`
	StopOnDesync           bool `json:"stopOnDesync"`
	VerifyChecksums        bool `json:"verifyChecksums"`
}

// UIConfig は画面構成の設定。
type UIConfig struct {
	ViewerLayout string `json:"viewerLayout"`
	Language     string `json:"language"`
	Theme        string `json:"theme"`
	// Windows はウィンドウの名前ごとのサイズと表示状態（設計書 10 編 §10.7）。
	Windows map[string]WindowState `json:"windows"`
	// RecentROMs は最近使った ROM。新しい順。
	RecentROMs []RecentROM `json:"recentRoms"`
}

// AgentConfig は Agent Interface の設定（設計書 14 編 §14.23）。
type AgentConfig struct {
	// Enabled は GUI 版で Agent Interface を有効にすることを表す。
	// headless のサブコマンドはこの値によらず有効である。
	Enabled bool `json:"enabled"`
	// Listen は接続方式。unix、または tcp:127.0.0.1:PORT・tcp:[::1]:PORT。
	Listen string `json:"listen"`
	// MaxInstances は headless の Instance の上限。
	MaxInstances int `json:"maxInstances"`
	// RomWatchAction は GUI 版で ROM ファイルの変化を見つけたときの動作。
	RomWatchAction string `json:"romWatchAction"`
	// ObserveImageScale は Observation の画像の既定の拡大率。
	ObserveImageScale int `json:"observeImageScale"`
}

// WindowState はウィンドウのサイズと表示状態。
//
// 位置は持たない。Fyne はウィンドウの位置を取得・設定する手段を持たない。
type WindowState struct {
	Width   int  `json:"width"`
	Height  int  `json:"height"`
	Visible bool `json:"visible"`
}

// RecentROM は最近使った ROM の 1 件。
type RecentROM struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// ChannelNames は audio.channelVolumes のキー。APU のチャンネルの順。
var ChannelNames = []string{"pulse1", "pulse2", "triangle", "noise", "dmc"}

// defaultChannelVolumes は全チャンネルを等倍にした音量を返す。
func defaultChannelVolumes() map[string]float64 {
	m := map[string]float64{}
	for _, n := range ChannelNames {
		m[n] = 1.0
	}
	return m
}

// MaxRecentROMs は最近使った ROM を残す件数。
const MaxRecentROMs = 10

// 設定値として受け付ける文字列。
const (
	RegionAuto  = "auto"
	RegionNTSC  = "ntsc"
	RegionPAL   = "pal"
	RegionDendy = "dendy"

	FilterNearest = "nearest"
	FilterLinear  = "linear"

	DeviceStandard = "standard"
	DeviceNone     = "none"

	LayoutWindows = "windows"
	LayoutDocked  = "docked"

	LogNone   = "none"
	LogStderr = "stderr"
	LogStdout = "stdout"
	LogFile   = "file"

	ThemeAuto  = "auto"
	ThemeLight = "light"
	ThemeDark  = "dark"

	LanguageJapanese = "ja"

	AgentListenUnix = "unix"
	RomWatchNotify  = "notify"
	RomWatchReload  = "reload"
)

// 拡大率の範囲。
const (
	MinScale = 1
	MaxScale = 8
)

// MaxOverscan は上下左右それぞれで隠せる最大のピクセル数。
const MaxOverscan = 16

// ApplyDeterministic は値が定まらない状態をすべて固定値にする（引数
// --deterministic、設計書 08 編 §8.5.2）。
func ApplyDeterministic(e *EmulationConfig) {
	e.RAMInitPattern = "zero"
	e.RAMSeed = 0
	e.CPUPPUAlignment = 0
	e.DMAGetPutPhase = 0
	e.PPUVBlankFlag = false
}

// Default は既定の設定を返す。
//
// 設定ファイルが無い状態でも起動できるように、既定値をここに持つ。
// ファイルの読み書きはこの値を出発点として上書きする形で行う。
func Default() *Config {
	return &Config{
		Version: Version,
		Emulation: EmulationConfig{
			Region:                  RegionAuto,
			RAMInitPattern:          "random",
			RAMSeed:                 0,
			CPUPPUAlignment:         2,
			DMAGetPutPhase:          0,
			MMC3IRQVariant:          "sharp",
			BusConflicts:            "auto",
			DMCDMARegisterConflicts: true,
		},
		Video: VideoConfig{
			Scale:          3,
			IntegerScale:   true,
			Filter:         FilterNearest,
			OverscanTop:    8,
			OverscanBottom: 8,
		},
		Audio: AudioConfig{
			Enabled:                 true,
			SampleRate:              48000,
			BufferMilliseconds:      25,
			RingHighWaterMultiplier: 2,
			MasterVolume:            1.0,
			ChannelVolumes:          defaultChannelVolumes(),
			FilterProfile:           "nes",
		},
		Input: InputConfig{
			Port1Device: DeviceStandard,
			Port2Device: DeviceStandard,
			TurboRateHz: 15,
		},
		Debug: DebugConfig{
			LogCategories:     []string{"warn.compat", "error"},
			LogOutput:         LogStderr,
			LogMaxBytes:       10 << 20,
			LogGenerations:    3,
			TraceRingSize:     1000000,
			ChangeDecayFrames: 30,
		},
		State: StateConfig{
			Slots:                10,
			RewindEnabled:        true,
			RewindSeconds:        60,
			RewindIntervalFrames: 10,
			SaveScreenshot:       true,
		},
		Movie: MovieConfig{
			ChecksumIntervalFrames: 60,
			StopOnDesync:           true,
			VerifyChecksums:        true,
		},
		UI: UIConfig{
			ViewerLayout: LayoutWindows,
			Language:     LanguageJapanese,
			Theme:        ThemeAuto,
			Windows:      map[string]WindowState{},
			RecentROMs:   []RecentROM{},
		},
		Agent: AgentConfig{
			Listen:            AgentListenUnix,
			MaxInstances:      16,
			RomWatchAction:    RomWatchNotify,
			ObserveImageScale: 2,
		},
	}
}
