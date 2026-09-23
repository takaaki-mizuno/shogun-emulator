package ppu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/cart"

// Bus は PPU から見たカートリッジ。
type Bus interface {
	// ReadCHR は $0000-$1FFF を読む。
	ReadCHR(addr uint16) uint8
	// WriteCHR は $0000-$1FFF へ書く。
	WriteCHR(addr uint16, v uint8)
	// MapNametable は $2000-$2FFF のアクセス先を返す。
	MapNametable(addr uint16) cart.NametableTarget
	// NotifyPPUAddress は PPU がアドレスバスに値を出したことを伝える。
	NotifyPPUAddress(addr uint16, dot uint64)
}

// ciramSize は PPU が持つネームテーブル用の記憶域の大きさ。
//
// 本体の CIRAM は 2 KiB だが、4 画面構成のカートリッジは追加の 2 KiB を
// 持つ。両方をこの 1 つの配列で表す。アクセスするのは PPU だけであり、
// 記憶域の持ち主を 1 つにするとセーブステートで二重に保存する誤りを
// 避けられる。
const ciramSize = 4096

// paletteSize はパレット RAM の大きさ。
const paletteSize = 32

// paletteIndex はパレット RAM の添字を返す。
//
// 下位 2 bit が 0 のスプライト側エントリ（$3F10・$3F14・$3F18・$3F1C）は
// 背景側（$3F00・$3F04・$3F08・$3F0C）と同一の記憶域を指す。
func paletteIndex(addr uint16) int {
	i := int(addr & 0x1F)
	if i&0x13 == 0x10 {
		i &= 0x0F
	}
	return i
}

// PeekVRAM は副作用を起こさずに PPU アドレス空間を読む。
//
// デバッガのビューアと、画面に結果を表示するテスト ROM の判定に使う。
func (p *PPU) PeekVRAM(addr uint16) uint8 { return p.readVRAM(addr) }

// readVRAM は PPU アドレス空間を読む。
func (p *PPU) readVRAM(addr uint16) uint8 {
	addr &= 0x3FFF
	switch {
	case addr < 0x2000:
		return p.bus.ReadCHR(addr)
	case addr < 0x3F00:
		// $3000-$3EFF は $2000-$2EFF のミラー
		return p.readNametable(addr & 0x2FFF)
	}
	return p.palette[paletteIndex(addr)]
}

// writeVRAM は PPU アドレス空間へ書く。
func (p *PPU) writeVRAM(addr uint16, v uint8) {
	addr &= 0x3FFF
	switch {
	case addr < 0x2000:
		p.bus.WriteCHR(addr, v)
	case addr < 0x3F00:
		p.writeNametable(addr&0x2FFF, v)
	default:
		if p.Warn != nil && v&0x3F == forbiddenColor {
			p.warn("色 $0D をパレット $%04X へ書いた", addr)
		}
		p.palette[paletteIndex(addr)] = v & 0x3F
	}
}

// forbiddenColor は使用を避けるべき色。
//
// 黒よりも暗い電圧を出力し、テレビによっては同期信号と誤認する。
const forbiddenColor = 0x0D

// readNametable は $2000-$2FFF を読む。アクセス先はカートリッジが決める。
func (p *PPU) readNametable(addr uint16) uint8 {
	t := p.bus.MapNametable(addr)
	switch t.Kind {
	case cart.NametableCHR:
		return p.bus.ReadCHR(uint16(t.Offset))
	default:
		return p.ciram[int(t.Offset)%ciramSize]
	}
}

// writeNametable は $2000-$2FFF へ書く。
func (p *PPU) writeNametable(addr uint16, v uint8) {
	t := p.bus.MapNametable(addr)
	switch t.Kind {
	case cart.NametableCHR:
		// CHR-ROM をネームテーブルに割り当てた構成では書き込めない
	default:
		p.ciram[int(t.Offset)%ciramSize] = v
	}
}

// fetchAddress はアドレスをバスへ出す。
//
// フェッチの 1 ドット目に呼ぶ。マッパーが監視する A12 の遷移を実機と同じ
// 回数・同じタイミングで起こすため、値を読まないアクセスでも呼ぶ。
func (p *PPU) fetchAddress(addr uint16) {
	p.busAddr = addr & 0x3FFF
	p.bus.NotifyPPUAddress(p.busAddr, p.dots)
}

// putAddressOnBus は $2006 と $2007 のアクセスでアドレスをバスへ出す。
//
// busAddr は変えない。レンダリングのフェッチが使っているアドレスと
// 別の経路であり、フェッチの 2 ドット目が読む先を変えてはならない。
func (p *PPU) putAddressOnBus(addr uint16) {
	p.bus.NotifyPPUAddress(addr&0x3FFF, p.dots)
}

// fetchValue は 1 ドット目に出したアドレスから値を読む。
func (p *PPU) fetchValue() uint8 {
	return p.readVRAM(p.busAddr)
}
