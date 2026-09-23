package cart

import "fmt"

// busConflictMode はバス競合の扱い。
type busConflictMode uint8

const (
	// busConflictNone は競合を再現しない。
	busConflictNone busConflictMode = iota
	// busConflictAND は書いた値と ROM の内容の論理積を実効値とする。
	busConflictAND
)

// 設定 emulation.busConflicts が受け付ける値。
const (
	BusConflictsAuto   = "auto"
	BusConflictsAlways = "always"
	BusConflictsNever  = "never"
)

// prgPeeker は副作用なしに PRG を読める。
//
// バス競合の層が ROM の内容を必要とする。通常の ReadPRG を使わないのは、
// 読み出しに副作用を持つマッパーを後から追加したときに、競合の計算が
// その副作用を起こしてしまうことを避けるためである。
type prgPeeker interface {
	peekPRG(addr uint16) uint8
}

// conflictLayer はマッパーを包み、$8000 以上への書き込みで値と ROM の
// 内容の論理積を取る。
//
// ディスクリートロジックのマッパーでは、CPU が $8000 以上へ書き込むとき
// PRG-ROM も同じデータバスへ値を出す。両者がぶつかった結果、実効値は
// 論理積になる。
//
// 層として外側に置くのは、対象のマッパーが 4 つあり、それぞれに同じ処理を
// 書くと漏れるためである。マッパー本体はバス競合を意識しない。
type conflictLayer struct {
	Cartridge
	peek prgPeeker
}

// WritePRG は値と ROM の内容の論理積を取ってから内側へ渡す。
func (c *conflictLayer) WritePRG(addr uint16, v uint8) {
	if addr >= 0x8000 {
		v &= c.peek.peekPRG(addr)
	}
	c.Cartridge.WritePRG(addr, v)
}

// wrapBusConflict はモードに応じてマッパーを包む。
//
// 競合を再現しないときは包まない。呼び出しを 1 段増やさないためである。
func wrapBusConflict(c Cartridge, mode busConflictMode) (Cartridge, error) {
	if mode == busConflictNone {
		return c, nil
	}
	p, ok := c.(prgPeeker)
	if !ok {
		return nil, fmt.Errorf("cart: このマッパーはバス競合を扱えない（peekPRG が無い）")
	}
	return &conflictLayer{Cartridge: c, peek: p}, nil
}

// discreteBusConflict は本体がディスクリートロジックで、バス競合が
// 起こりうるマッパー。
//
// MMC1・MMC3 のような専用チップはレジスタがバスを駆動しないため、
// 競合が起こらない。設定を always にしても、この表に無いマッパーでは
// 競合を再現しない。
var discreteBusConflict = map[uint16]bool{
	2:  true,
	3:  true,
	7:  true,
	66: true,
}

// busConflictSubmappers は NES 2.0 のサブマッパーが示すモード。
//
// サブマッパー 1 が競合なし、2 が競合ありを表す。サブマッパー 0 は
// 「どちらか不明」であり、競合を再現しない。競合を再現すると、
// 回避していないプログラムが動かなくなるためである。
var busConflictSubmappers = map[uint16]map[uint8]busConflictMode{
	2: {1: busConflictNone, 2: busConflictAND},
	3: {1: busConflictNone, 2: busConflictAND},
	7: {1: busConflictNone, 2: busConflictAND},
}

// busConflictModeFor は ROM と設定からモードを決める。
func busConflictModeFor(rom *ROM, setting string) (busConflictMode, error) {
	switch setting {
	case "", BusConflictsAuto:
		if rom.Format != FormatNES20 {
			return busConflictNone, nil
		}
		m, ok := busConflictSubmappers[rom.Mapper]
		if !ok {
			return busConflictNone, nil
		}
		mode, ok := m[rom.Submapper]
		if !ok {
			return busConflictNone, nil
		}
		return mode, nil
	case BusConflictsAlways:
		if discreteBusConflict[rom.Mapper] {
			return busConflictAND, nil
		}
		return busConflictNone, nil
	case BusConflictsNever:
		return busConflictNone, nil
	}
	return busConflictNone, fmt.Errorf("cart: 知らない busConflicts の値 %q", setting)
}
