// Package cart は ROM の読み込みとマッパーの実装を提供する。
package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// Mirroring はネームテーブルの配置を表す。
type Mirroring uint8

const (
	// MirrorHorizontal は水平ミラーリング（垂直配置）。CIRAM A10 = PPU A11。
	MirrorHorizontal Mirroring = iota
	// MirrorVertical は垂直ミラーリング（水平配置）。CIRAM A10 = PPU A10。
	MirrorVertical
	// MirrorSingleA は 1 画面（CIRAM の前半のみ）。
	MirrorSingleA
	// MirrorSingleB は 1 画面（CIRAM の後半のみ）。
	MirrorSingleB
	// MirrorFourScreen は 4 画面。カートリッジ側 VRAM を使う。
	MirrorFourScreen
)

// String は配置の名前を返す。
func (m Mirroring) String() string {
	switch m {
	case MirrorHorizontal:
		return "horizontal"
	case MirrorVertical:
		return "vertical"
	case MirrorSingleA:
		return "single-A"
	case MirrorSingleB:
		return "single-B"
	case MirrorFourScreen:
		return "four-screen"
	}
	return "unknown"
}

// NametableKind はネームテーブルのアクセス先の種別。
type NametableKind uint8

const (
	// NametableCIRAM は本体の CIRAM。
	NametableCIRAM NametableKind = iota
	// NametableCart はカートリッジ側 VRAM。
	NametableCart
	// NametableCHR は CHR-ROM をネームテーブルに割り当てたもの。
	NametableCHR
)

// NametableTarget はネームテーブルのアクセス先。
type NametableTarget struct {
	Kind   NametableKind
	Offset uint32
}

// BankView はデバッガに現在のバンク構成を見せるための 1 ウィンドウ分の情報。
type BankView struct {
	// CPUOrPPUAddr はウィンドウの先頭アドレス。
	CPUOrPPUAddr uint16
	// Size はウィンドウの大きさ。
	Size int
	// SourceKind は "PRG-ROM"・"PRG-RAM"・"CHR-ROM"・"CHR-RAM" のいずれか。
	SourceKind string
	// BankIndex は割り当てられたバンク番号。
	BankIndex int
	// Offset は元データの先頭からのオフセット。
	Offset uint32
}

// Info はカートリッジの構成。
type Info struct {
	MapperName   string
	MapperNumber uint16
	Submapper    uint8
	PRGBanks     []BankView
	CHRBanks     []BankView
	Mirroring    Mirroring
}

// Cartridge はカートリッジの振る舞い。
type Cartridge interface {
	// ReadPRG は CPU バスの $4020-$FFFF を読む。
	//
	// handled が false のとき、バスはオープンバスの値を返す。$6000-$7FFF に
	// 何も無いカートリッジのオープンバス挙動を表すためである。
	ReadPRG(addr uint16) (value uint8, handled bool)
	// WritePRG は CPU バスの $4020-$FFFF へ書く。
	WritePRG(addr uint16, value uint8)

	// ReadCHR は PPU バスの $0000-$1FFF を読む。
	ReadCHR(addr uint16) uint8
	// WriteCHR は PPU バスの $0000-$1FFF へ書く。
	WriteCHR(addr uint16, value uint8)
	// MapNametable は $2000-$2FFF のアクセス先を返す。
	MapNametable(addr uint16) NametableTarget

	// NotifyPPUAddress は PPU がアドレスバスに値を出したときに呼ばれる。
	//
	// dot は電源投入からの PPU のドット数である。A12 のフィルタが
	// low の継続時間をドット単位で測るために必要になる。CPU サイクル
	// 単位では、スプライトのフェッチの間に生じる 4 ドットの low が
	// 1 サイクルにも 2 サイクルにも見え、立ち上がりの判定が揺れる。
	NotifyPPUAddress(addr uint16, dot uint64)

	// Tick は CPU サイクルの経過を伝える。
	Tick(cycles int)

	// IRQAsserted はマッパーが IRQ をアサートしているかを返す。
	IRQAsserted() bool

	// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
	// PRG-ROM に当たらないとき false。
	//
	// デバッガが実行した命令をファイルオフセットで記録するために使う。
	// バンク切り替えにより同じ CPU アドレスが複数のバンクを指すためである。
	PRGOffset(addr uint16) (int, bool)

	// PRGRAM は $6000-$7FFF に現れる RAM の全体を返す。持たないとき nil。
	//
	// 状態のハッシュ（設計書 08 編 §8.7.3）とデバッガの表示が使う。
	// 不揮発かどうかによらず全体を返す。
	PRGRAM() []uint8

	// BatteryRAM は不揮発メモリの内容を返す。持たないとき nil。
	BatteryRAM() []uint8
	// SetBatteryRAM は不揮発メモリの内容を設定する。
	SetBatteryRAM(data []uint8) error

	state.Snapshotter

	// Info は構成を返す。
	Info() Info
}
