package debug

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/ppu"
)

// Diagnostic（設計書 14 編 §14.20）。
//
// ファミコン特有の誤りの疑いを検知する。判定は値の読み取り（Peek）だけで
// 行い、Bus.Read を呼ばない。エミュレーションの結果を変えないためである。
// 検知に要るフックは、その項目が有効なときだけ updateHooks が設定する。

// DiagKind は Diagnostic の種類。
type DiagKind uint8

// Diagnostic の種類（§14.20.1 の表の順）。
const (
	DiagVRAMDuringRender DiagKind = iota
	DiagPPUWriteBeforeWarmup
	DiagUninitRAMRead
	DiagStackOverflow
	DiagStackUnderflow
	DiagNMIReentry
	DiagExecuteData
	DiagExecuteRAM
	DiagUnstableOpcode
	DiagOAMAddrNonzeroAtDMA
	DiagPaletteColor0D
	// DiagKindCount は種類の数。
	DiagKindCount
)

var diagNames = [DiagKindCount]string{
	"vram_access_during_render", "ppu_write_before_warmup", "uninitialized_ram_read",
	"stack_overflow", "stack_underflow", "nmi_reentry", "execute_data", "execute_ram",
	"unstable_opcode", "oamaddr_nonzero_at_dma", "palette_color_0d",
}

// diagDefaults は既定で有効な種類（§14.20.1）。execute_ram と palette_color_0d は
// 意図して使うプログラムがあるため既定で無効にする。
var diagDefaults = [DiagKindCount]bool{
	true, true, true, true, true, true, true, false, true, true, false,
}

func (k DiagKind) String() string {
	if k < DiagKindCount {
		return diagNames[k]
	}
	return "unknown"
}

// ParseDiagKind は名前から種類を引く。
func ParseDiagKind(s string) (DiagKind, bool) {
	for i, n := range diagNames {
		if n == s {
			return DiagKind(i), true
		}
	}
	return 0, false
}

// DiagKindNames は種類の名前を §14.20.1 の順に返す。
func DiagKindNames() []string { return append([]string(nil), diagNames[:]...) }

// Diagnostic は検知した 1 件。
type Diagnostic struct {
	Kind     DiagKind
	Frame    uint64
	Scanline int16
	Dot      int16
	// PC は検知した命令の位置。
	PC uint16
	// PRGOffset は PC に当たる PRG-ROM のオフセット。RAM なら -1。
	PRGOffset int32
	// Detail は説明。種類と PC の組の最初の 1 件だけが持つ。
	Detail string
	// Seq は検知の通し番号（1 から）。
	Seq uint64
}

// DiagConfig は種類ごとの有効・無効と停止の指定。
type DiagConfig struct {
	Enabled [DiagKindCount]bool
	// Stop は検知した命令を終えた命令境界で止める種類。
	Stop [DiagKindCount]bool
}

// DefaultDiagConfig は既定の指定を返す。
func DefaultDiagConfig() DiagConfig { return DiagConfig{Enabled: diagDefaults} }

// any は有効な種類があるかを返す。
func (c DiagConfig) any() bool {
	for _, on := range c.Enabled {
		if on {
			return true
		}
	}
	return false
}

// DiagStat は種類と位置の組ごとの集計。
type DiagStat struct {
	Kind      DiagKind
	PC        uint16
	PRGOffset int32
	Count     uint64
	// First は最初の 1 件。LastFrame は最後に検知したフレーム。
	First     Diagnostic
	LastFrame uint64
}

// diagKey は集計の単位。PRG-ROM の位置はバンクを区別する。
type diagKey struct {
	kind DiagKind
	pc   uint16
	off  int32
}

const (
	// maxDiagStats は集計する組の上限（diag.list の上限と同じ）。
	maxDiagStats = 1000
	// diagRecentSize は最近の検知を保つ数。Observation の差分に使う。
	diagRecentSize = 4096
	// maxIntDepth は割り込みの入れ子を数える上限。
	maxIntDepth = 16
)

// diagState は Diagnostic の状態。エミュレーションゴルーチンだけが触る。
type diagState struct {
	cfg DiagConfig
	// stats は組ごとの集計。order は組の初出の順。
	stats map[diagKey]*DiagStat
	order []diagKey
	// overflow は上限を超えたため集計できなかった組の件数。
	overflow uint64
	// totals は種類ごとの累積件数。
	totals [DiagKindCount]uint64
	// recent は最近の検知のリング。seq は最後の通し番号。
	recent []Diagnostic
	seq    uint64

	// 命令の境界の追跡（スタックと NMI）。
	havePrev bool
	prevPC   uint16
	prevOp   uint8
	prevS    uint8
	// intStack は処理中の割り込みの種類（RTI で取り除く）。
	intStack []cpu.Interrupt

	// execData は PRG-ROM のオフセットごとのデータの指定。execGen は作った
	// ときの Symbol の版。
	execData []bool
	execGen  uint64
}

// Hook は新しい組の最初の 1 件を知らせる（イベント diagnostic）。
// エミュレーションゴルーチンから呼ばれる。

// SetDiagConfig は Diagnostic の指定を差し替え、フックを設定し直す。
func (d *Debugger) SetDiagConfig(c DiagConfig) {
	d.diag.cfg = c
	if c.Enabled[DiagExecuteData] {
		d.refreshExecData(true)
	}
	d.updateHooks()
}

// DiagConfig は Diagnostic の指定を返す。
func (d *Debugger) DiagConfig() DiagConfig { return d.diag.cfg }

// resetDiag は検知の記録を消す。ROM を読み込んだときに呼ぶ。指定は残す。
func (d *Debugger) resetDiag() {
	cfg := d.diag.cfg
	d.diag = diagState{cfg: cfg}
}

// StateLoaded はセーブステートを読み込んだときに呼ぶ。書き込みの記録は
// セーブステートに含まれないため、内蔵 RAM をすべて書き込み済みとみなす
// （uninitialized_ram_read の誤検知を防ぐ）。命令の境界の追跡も捨てる。
func (d *Debugger) StateLoaded() {
	for i := range d.ramWritten {
		d.ramWritten[i] = true
	}
	d.diag.havePrev = false
	d.diag.intStack = d.diag.intStack[:0]
}

// DiagTotals は種類ごとの累積件数を返す。
func (d *Debugger) DiagTotals() [DiagKindCount]uint64 { return d.diag.totals }

// DiagSeq は最後の通し番号を返す。
func (d *Debugger) DiagSeq() uint64 { return d.diag.seq }

// DiagSince は通し番号 seq より後の検知のうち、種類ごとの最初の 1 件を返す。
// リングから消えたものは含まない。
func (d *Debugger) DiagSince(seq uint64) map[DiagKind]Diagnostic {
	out := map[DiagKind]Diagnostic{}
	s := &d.diag
	n := len(s.recent)
	if n == 0 || s.seq <= seq {
		return out
	}
	// リングは seq の昇順に並ぶ（古い順にたどる）。
	start := int(s.seq % uint64(diagRecentSize))
	if n < diagRecentSize {
		start = 0
	}
	for i := 0; i < n; i++ {
		x := s.recent[(start+i)%n]
		if x.Seq <= seq {
			continue
		}
		if _, ok := out[x.Kind]; !ok {
			x.Detail = d.diagDetail(x)
			out[x.Kind] = x
		}
	}
	return out
}

// diagDetail は組の最初の 1 件の説明を返す。
func (d *Debugger) diagDetail(x Diagnostic) string {
	if st, ok := d.diag.stats[diagKey{x.Kind, x.PC, x.PRGOffset}]; ok {
		return st.First.Detail
	}
	return ""
}

// DiagStats は組ごとの集計を初出の順に返す（最大 1000 組）。上限を超えた
// 件数も返す。
func (d *Debugger) DiagStats() ([]DiagStat, uint64) {
	out := make([]DiagStat, 0, len(d.diag.order))
	for _, k := range d.diag.order {
		out = append(out, *d.diag.stats[k])
	}
	return out, d.diag.overflow
}

// diagOn は種類が有効かを返す。
func (d *Debugger) diagOn(k DiagKind) bool { return d.diag.cfg.Enabled[k] }

// report は Diagnostic を記録する。detail は組の最初の 1 件のときだけ呼ぶ。
// 停止を指定した種類なら止める理由を立て、true を返す。
func (d *Debugger) report(k DiagKind, pc uint16, detail func() string) bool {
	n := d.n
	s := &d.diag
	off := int32(-1)
	if o, ok := n.Cart.PRGOffset(pc); ok && pc >= 0x8000 {
		off = int32(o)
	}
	s.seq++
	x := Diagnostic{Kind: k, Frame: n.Frames(), Scanline: int16(n.PPU.Scanline()), Dot: int16(n.PPU.Dot()),
		PC: pc, PRGOffset: off, Seq: s.seq}
	s.totals[k]++
	if s.recent == nil {
		s.recent = make([]Diagnostic, 0, 64)
	}
	if len(s.recent) < diagRecentSize {
		s.recent = append(s.recent, x)
	} else {
		s.recent[(s.seq-1)%diagRecentSize] = x
	}
	if s.stats == nil {
		s.stats = map[diagKey]*DiagStat{}
	}
	key := diagKey{k, pc, off}
	if st, ok := s.stats[key]; ok {
		st.Count++
		st.LastFrame = x.Frame
	} else if len(s.order) < maxDiagStats {
		x.Detail = detail()
		s.stats[key] = &DiagStat{Kind: k, PC: pc, PRGOffset: off, Count: 1, First: x, LastFrame: x.Frame}
		s.order = append(s.order, key)
		if d.DiagHook != nil {
			d.DiagHook(x)
		}
	} else {
		s.overflow++
	}
	if s.cfg.Stop[k] {
		d.raiseDiag(x)
		return true
	}
	return false
}

// raiseDiag は Diagnostic で止まる理由を立てる。
func (d *Debugger) raiseDiag(x Diagnostic) {
	if d.hit != nil {
		return
	}
	if x.Detail == "" {
		x.Detail = d.diagDetail(x)
	}
	n := d.n
	d.hit = &BreakInfo{
		Reason:     fmt.Sprintf("Diagnostic %s（$%04X）", x.Kind, x.PC),
		PC:         n.CPU.PC,
		Cycles:     n.Cycles(),
		Scanline:   n.PPU.Scanline(),
		Dot:        n.PPU.Dot(),
		Diagnostic: &x,
	}
}

// diagNeeds は有効な種類が要るフック。
type diagNeeds struct {
	beforeExec, read, write, interrupt, frame, cpuCompat, ppuCompat bool
}

// needs は有効な種類から要るフックを決める（§14.20.2）。
func (c DiagConfig) needs() diagNeeds {
	var n diagNeeds
	e := c.Enabled
	n.beforeExec = e[DiagStackOverflow] || e[DiagStackUnderflow] || e[DiagNMIReentry] || e[DiagExecuteData] || e[DiagExecuteRAM]
	n.read = e[DiagUninitRAMRead]
	n.write = e[DiagUninitRAMRead] || e[DiagOAMAddrNonzeroAtDMA]
	n.interrupt = e[DiagStackOverflow] || e[DiagStackUnderflow] || e[DiagNMIReentry]
	n.frame = e[DiagExecuteData]
	n.cpuCompat = e[DiagUnstableOpcode]
	n.ppuCompat = e[DiagVRAMDuringRender] || e[DiagPPUWriteBeforeWarmup] || e[DiagPaletteColor0D]
	return n
}

// uninitRAMRead は書き込みの無い内蔵 RAM の読み出しかを返す。ブレークポイントの
// EventUninitializedRAMRead と Diagnostic の uninitialized_ram_read が共有する
// 判定である。CPU のダミーリード（LDA $10,X の $0010、PLA の前のスタックの
// 読み出しなど）は値を使わないため数えない。
func (d *Debugger) uninitRAMRead(addr uint16) bool {
	return addr < 0x2000 && !d.ramWritten[addr&0x07FF] && !d.n.CPU.DummyRead()
}

// stackEffect は命令がスタックへ積む（正）か取り出す（負）バイト数。
// BRK と割り込みは onDiagInterrupt で扱う。
func stackEffect(op uint8) int {
	switch op {
	case 0x48, 0x08: // PHA, PHP
		return 1
	case 0x20: // JSR
		return 2
	case 0x68, 0x28: // PLA, PLP
		return -1
	case 0x60: // RTS
		return -2
	case 0x40: // RTI
		return -3
	}
	return 0
}

// diagBeforeExec は命令の実行の前に呼ばれる。直前の命令のスタックの変化を
// 確かめ、この命令の位置を確かめる。止めるとき true を返す（直前の命令を
// 終えた境界で止める）。
func (d *Debugger) diagBeforeExec(pc uint16) bool {
	n := d.n
	s := &d.diag
	stop := false
	if s.havePrev {
		stop = d.checkStack(n.CPU.S)
	}
	s.havePrev, s.prevPC, s.prevOp, s.prevS = true, pc, n.Bus.Peek(pc), n.CPU.S
	if pc < 0x2000 && s.cfg.Enabled[DiagExecuteRAM] {
		d.report(DiagExecuteRAM, pc, func() string { return fmt.Sprintf("RAM の $%04X を実行した", pc) })
	}
	if s.cfg.Enabled[DiagExecuteData] && pc >= 0x8000 {
		if off, ok := n.Cart.PRGOffset(pc); ok && off < len(s.execData) && s.execData[off] {
			d.report(DiagExecuteData, pc, func() string {
				return fmt.Sprintf("データと指定された位置 $%04X（PRG $%05X）を実行した", pc, off)
			})
		}
	}
	return stop
}

// checkStack は直前の命令（prevOp）のスタックの変化を確かめる。sAfter は
// その命令を終えた後の S。
func (d *Debugger) checkStack(sAfter uint8) bool {
	s := &d.diag
	s.havePrev = false
	eff := stackEffect(s.prevOp)
	stop := false
	pc := s.prevPC
	switch {
	case eff > 0 && sAfter > s.prevS && s.cfg.Enabled[DiagStackOverflow]:
		before := s.prevS
		stop = d.report(DiagStackOverflow, pc, func() string {
			return fmt.Sprintf("積み込みで S が $%02X から $%02X へ回った", before, sAfter)
		})
	case eff < 0 && sAfter < s.prevS && s.cfg.Enabled[DiagStackUnderflow]:
		before := s.prevS
		stop = d.report(DiagStackUnderflow, pc, func() string {
			return fmt.Sprintf("取り出しで S が $%02X から $%02X へ回った", before, sAfter)
		})
	}
	if s.prevOp == 0x40 && len(s.intStack) > 0 {
		s.intStack = s.intStack[:len(s.intStack)-1]
	}
	return stop
}

// diagInterrupt は割り込みシーケンスを終えたときに呼ばれる。
func (d *Debugger) diagInterrupt(k cpu.Interrupt) {
	n := d.n
	s := &d.diag
	switch k {
	case cpu.InterruptReset:
		// リセットは S を 3 減らすが積み込みではない（§14.20 つまずきやすい点）。
		s.havePrev = false
		s.intStack = s.intStack[:0]
		return
	case cpu.InterruptBRK:
		// 直前の命令は BRK 自身である。積み込みの 3 バイトで回ったかを確かめる。
		if s.havePrev {
			before := s.prevS
			s.havePrev = false
			if n.CPU.S > before && s.cfg.Enabled[DiagStackOverflow] {
				pc := s.prevPC
				d.report(DiagStackOverflow, pc, func() string {
					return fmt.Sprintf("BRK の積み込みで S が $%02X から $%02X へ回った", before, n.CPU.S)
				})
			}
		}
	case cpu.InterruptNMI, cpu.InterruptIRQ:
		sBefore := n.CPU.S + 3
		if s.havePrev {
			d.checkStack(sBefore)
		}
		if n.CPU.S > sBefore && s.cfg.Enabled[DiagStackOverflow] {
			pc := s.prevPC
			d.report(DiagStackOverflow, pc, func() string {
				return fmt.Sprintf("%s の積み込みで S が $%02X から $%02X へ回った", interruptName(k), sBefore, n.CPU.S)
			})
		}
		if k == cpu.InterruptNMI && s.cfg.Enabled[DiagNMIReentry] {
			for _, x := range s.intStack {
				if x == cpu.InterruptNMI {
					pc := s.prevPC
					d.report(DiagNMIReentry, pc, func() string {
						return fmt.Sprintf("NMI の処理の途中（入れ子 %d）で次の NMI が起きた", len(s.intStack))
					})
					break
				}
			}
		}
	default:
		return
	}
	if len(s.intStack) == maxIntDepth {
		s.intStack = append(s.intStack[:0], s.intStack[1:]...)
	}
	s.intStack = append(s.intStack, k)
}

func interruptName(k cpu.Interrupt) string {
	switch k {
	case cpu.InterruptNMI:
		return "NMI"
	case cpu.InterruptIRQ:
		return "IRQ"
	case cpu.InterruptBRK:
		return "BRK"
	case cpu.InterruptReset:
		return "リセット"
	}
	return "割り込み"
}

// diagRead はバスの読み出しで呼ばれる。
func (d *Debugger) diagRead(addr uint16) {
	if d.diag.cfg.Enabled[DiagUninitRAMRead] && d.uninitRAMRead(addr) {
		d.report(DiagUninitRAMRead, d.n.CPU.OpPC(), func() string {
			return fmt.Sprintf("書き込まれていない RAM $%04X を読んだ", addr)
		})
	}
}

// diagWrite はバスの書き込みで呼ばれる（RAM の書き込みの記録の前）。
func (d *Debugger) diagWrite(addr uint16, v uint8) {
	if addr == 0x4014 && d.diag.cfg.Enabled[DiagOAMAddrNonzeroAtDMA] {
		if oa := d.n.PPU.OAMAddr(); oa != 0 {
			d.report(DiagOAMAddrNonzeroAtDMA, d.n.CPU.OpPC(), func() string {
				return fmt.Sprintf("OAMADDR が $%02X のまま $4014 へ $%02X を書いた", oa, v)
			})
		}
	}
}

// onCPUCompat は CPU の互換性の事象を受け取る（warn.compat と判定を共有する）。
func (d *Debugger) onCPUCompat(kind cpu.Compat, pc uint16) {
	if !d.diag.cfg.Enabled[DiagUnstableOpcode] {
		return
	}
	op := d.n.Bus.Peek(pc)
	d.report(DiagUnstableOpcode, pc, func() string {
		if kind == cpu.CompatSTP {
			return fmt.Sprintf("STP（$%02X）を実行した", op)
		}
		return fmt.Sprintf("不安定な非公式命令 $%02X を実行した", op)
	})
}

// onPPUCompat は PPU の互換性の事象を受け取る。
func (d *Debugger) onPPUCompat(kind ppu.Compat, reg uint16) {
	n := d.n
	pc := n.CPU.OpPC()
	switch kind {
	case ppu.CompatRenderAccess:
		if d.diagOn(DiagVRAMDuringRender) {
			line, dot := n.PPU.Scanline(), n.PPU.Dot()
			d.report(DiagVRAMDuringRender, pc, func() string {
				return fmt.Sprintf("描画中（スキャンライン %d、ドット %d）に $%04X へアクセスした", line, dot, reg)
			})
		}
	case ppu.CompatWarmupWrite:
		if d.diagOn(DiagPPUWriteBeforeWarmup) {
			d.report(DiagPPUWriteBeforeWarmup, pc, func() string {
				return fmt.Sprintf("PPU が書き込みを受け付ける前に $%04X へ書いた（無視された）", reg)
			})
		}
	case ppu.CompatColor0D:
		if d.diagOn(DiagPaletteColor0D) {
			d.report(DiagPaletteColor0D, pc, func() string {
				return fmt.Sprintf("色 $0D をパレット $%04X へ書いた", reg)
			})
		}
	}
}

// refreshExecData は execute_data の表を Symbol の版が変わったときに作り直す。
func (d *Debugger) refreshExecData(force bool) {
	if d.n == nil {
		return
	}
	gen := d.symbols.Generation()
	if !force && gen == d.diag.execGen && d.diag.execData != nil {
		return
	}
	d.diag.execData = d.symbols.ExecDataMap(len(d.n.ROM.PRG))
	d.diag.execGen = gen
}
