package cart

import "fmt"

// mapperEntry は 1 つのマッパーの登録。
type mapperEntry struct {
	name string
	// fourScreen はマッパーが 4 画面構成を扱えるかを表す。
	fourScreen bool
	new        func(rom *ROM, o Options) (Cartridge, error)
}

// mappers は対応するマッパーの表。
//
// 番号から引ける形にしておくことで、対応マッパーを増やすときに
// この表への 1 行の追加で済む。
var mappers = map[uint16]mapperEntry{
	0:  {name: "NROM", fourScreen: false, new: newNROM},
	1:  {name: "MMC1", fourScreen: false, new: newMMC1},
	2:  {name: "UxROM", fourScreen: false, new: newUxROM},
	3:  {name: "CNROM", fourScreen: false, new: newCNROM},
	4:  {name: "MMC3", fourScreen: true, new: newMMC3},
	7:  {name: "AxROM", fourScreen: false, new: newAxROM},
	66: {name: "GxROM", fourScreen: false, new: newGxROM},
}

// New は既定の設定で ROM に対応するカートリッジを作る。
func New(rom *ROM) (Cartridge, error) {
	return NewWithOptions(rom, DefaultOptions())
}

// NewWithOptions は ROM に対応するカートリッジを作る。
func NewWithOptions(rom *ROM, o Options) (Cartridge, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	if err := checkConsoleType(rom); err != nil {
		return nil, err
	}

	e, ok := mappers[rom.Mapper]
	if !ok {
		return nil, fmt.Errorf("cart: 未対応のマッパー %d（%s）", rom.Mapper, mapperName(rom.Mapper))
	}

	// 4 画面ビットの扱いはマッパーごとに決まる。扱えないマッパーで
	// ビットが立っている ROM があるため、ここで判断する。
	if rom.FourScreen {
		if e.fourScreen {
			rom.UseFourScreen()
		} else {
			rom.IgnoreFourScreen()
		}
	}

	c, err := e.new(rom, o)
	if err != nil {
		return nil, err
	}

	// バス競合の層はマッパーの外側に置く。マッパー本体は競合を意識しない。
	mode, err := busConflictModeFor(rom, o.BusConflicts)
	if err != nil {
		return nil, err
	}
	return wrapBusConflict(c, mode)
}

// コンソール種別。ヘッダのバイト 7 の bit 1-0。
const (
	// ConsoleNES は NES / Famicom。
	ConsoleNES uint8 = 0
	// ConsoleVS は VS System。
	ConsoleVS uint8 = 1
	// ConsolePlayChoice は PlayChoice-10。
	ConsolePlayChoice uint8 = 2
	// ConsoleExtended はバイト 13 で種別を指定する構成。
	ConsoleExtended uint8 = 3
)

// consoleNames はコンソール種別の名前。
var consoleNames = map[uint8]string{
	ConsoleVS:         "VS System",
	ConsolePlayChoice: "PlayChoice-10",
	ConsoleExtended:   "拡張コンソール",
}

// checkConsoleType は NES / Famicom 以外の ROM を断る。
//
// VS System と PlayChoice-10 は PPU と入出力が異なる。動かしても正しい
// 画面にならず、原因の分からない故障に見える。
func checkConsoleType(rom *ROM) error {
	if rom.ConsoleType == ConsoleNES {
		return nil
	}
	name, ok := consoleNames[rom.ConsoleType]
	if !ok {
		name = "不明"
	}
	return fmt.Errorf("cart: 未対応のコンソール種別 %d（%s）", rom.ConsoleType, name)
}

// Supported は対応しているマッパー番号かを返す。
func Supported(mapper uint16) bool {
	_, ok := mappers[mapper]
	return ok
}

// knownNames は対応していないマッパーの名前。
//
// エラーメッセージに名前を含めると、利用者が何を求めているかが分かる。
// 対応を追加する順序を決める材料にもなる。
var knownNames = map[uint16]string{
	1:   "MMC1",
	2:   "UxROM",
	4:   "MMC3",
	5:   "MMC5",
	7:   "AxROM",
	9:   "MMC2",
	10:  "MMC4",
	11:  "Color Dreams",
	19:  "Namco 163",
	23:  "VRC2/VRC4",
	24:  "VRC6",
	25:  "VRC2/VRC4",
	33:  "Taito TC0190",
	34:  "BNROM/NINA-001",
	66:  "GxROM",
	69:  "Sunsoft FME-7",
	71:  "Codemasters",
	85:  "VRC7",
	206: "Namco 108",
}

// mapperName はマッパー番号に対応する名前を返す。
func mapperName(mapper uint16) string {
	if e, ok := mappers[mapper]; ok {
		return e.name
	}
	if n, ok := knownNames[mapper]; ok {
		return n
	}
	return "名称不明"
}
