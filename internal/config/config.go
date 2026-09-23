package config

// Version は設定ファイルの形式のバージョン。
const Version = 1

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
	LogCategories     []string `json:"logCategories"`
	LogToFile         bool     `json:"logToFile"`
	TraceRingSize     int      `json:"traceRingSize"`
	ChangeDecayFrames int      `json:"changeDecayFrames"`
	MemoryEditWrite   bool     `json:"memoryEditWrite"`
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
}

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
)

// 拡大率の範囲。
const (
	MinScale = 1
	MaxScale = 8
)

// MaxOverscan は上下左右それぞれで隠せる最大のピクセル数。
const MaxOverscan = 16

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
			FilterProfile:           "nes",
		},
		Input: InputConfig{
			Port1Device: DeviceStandard,
			Port2Device: DeviceStandard,
			TurboRateHz: 15,
		},
		Debug: DebugConfig{
			LogCategories:     []string{"warn.compat", "error"},
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
			Language:     "ja",
			Theme:        "auto",
		},
	}
}
