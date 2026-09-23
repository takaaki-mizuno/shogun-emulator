package debug

import "fmt"

// BreakKind はブレークポイントの種別。
type BreakKind uint8

// ブレークポイントの種別。
const (
	// BreakExec は指定アドレスの命令を実行する前に止まる。
	BreakExec BreakKind = iota
	// BreakRead は指定アドレスを読んだ命令の後に止まる。
	BreakRead
	// BreakWrite は指定アドレスへ書いた命令の後に止まる。
	BreakWrite
	// BreakPPUPosition は指定したスキャンラインとドットで止まる。
	BreakPPUPosition
	// BreakEvent は指定したイベントで止まる。
	BreakEvent

	breakKindCount
)

// String は種別の名前を返す。
func (k BreakKind) String() string {
	switch k {
	case BreakExec:
		return "実行"
	case BreakRead:
		return "読み出し"
	case BreakWrite:
		return "書き込み"
	case BreakPPUPosition:
		return "PPU 位置"
	case BreakEvent:
		return "イベント"
	}
	return "?"
}

// EventKind はイベントブレークポイントの対象。
type EventKind uint8

// イベントの種類。
const (
	// EventNMI は NMI の受け付け。
	EventNMI EventKind = iota
	// EventIRQ は IRQ の受け付け。
	EventIRQ
	// EventReset はリセット。
	EventReset
	// EventSprite0Hit はスプライト 0 ヒットのフラグが立ったこと。
	EventSprite0Hit
	// EventMapperIRQ はマッパーが IRQ 線をアサートしたこと。
	EventMapperIRQ
	// EventUninitializedRAMRead は書き込まれていない内蔵 RAM の読み出し。
	EventUninitializedRAMRead
	// EventMMC3IRQReloadWithoutClocks は A12 の立ち上がりを 2 回挟まない
	// $C001 への書き込み。
	EventMMC3IRQReloadWithoutClocks

	eventKindCount
)

// String はイベントの名前を返す。
func (e EventKind) String() string {
	switch e {
	case EventNMI:
		return "NMI"
	case EventIRQ:
		return "IRQ"
	case EventReset:
		return "リセット"
	case EventSprite0Hit:
		return "スプライト 0 ヒット"
	case EventMapperIRQ:
		return "マッパー IRQ"
	case EventUninitializedRAMRead:
		return "未初期化 RAM の読み出し"
	case EventMMC3IRQReloadWithoutClocks:
		return "MMC3 のクロックを挟まない $C001"
	}
	return "?"
}

// Breakpoint はブレークポイント 1 つ。
type Breakpoint struct {
	// ID は一覧の中で一意な番号。
	ID   int
	Kind BreakKind
	// AddrStart と AddrEnd はアドレスの範囲。単一アドレスなら同じ値。
	AddrStart uint16
	AddrEnd   uint16
	// Scanline と Dot は BreakPPUPosition の位置。
	Scanline int
	Dot      int
	// Event は BreakEvent の対象。
	Event EventKind
	// Condition は止まる条件。nil のとき常に止まる。
	Condition *Condition
	Enabled   bool
	// HitCount は条件を満たして止まった回数。
	HitCount uint64
	// Temporary は保存しないブレークポイントであることを表す。引数と設定で
	// 置いたものが使う（設計書 09 編 §9.9）。
	Temporary bool
}

// Describe は一覧に表示する説明を返す。
func (b *Breakpoint) Describe() string {
	var s string
	switch b.Kind {
	case BreakExec, BreakRead, BreakWrite:
		if b.AddrStart == b.AddrEnd {
			s = fmt.Sprintf("%s $%04X", b.Kind, b.AddrStart)
		} else {
			s = fmt.Sprintf("%s $%04X-$%04X", b.Kind, b.AddrStart, b.AddrEnd)
		}
	case BreakPPUPosition:
		s = fmt.Sprintf("%s %d, %d", b.Kind, b.Scanline, b.Dot)
	case BreakEvent:
		s = fmt.Sprintf("%s %s", b.Kind, b.Event)
	}
	if b.Condition != nil {
		s += " if " + b.Condition.Expr
	}
	return s
}

// covers は addr が範囲に入るかを返す。
func (b *Breakpoint) covers(addr uint16) bool {
	return addr >= b.AddrStart && addr <= b.AddrEnd
}

// BreakpointSet はブレークポイントの一覧。
//
// 種別ごとの有効件数を保持する。件数が 0 の種別に対応するフックを
// 設定しないためである。OnCPURead と OnCPUWrite は毎秒 180 万回
// 呼ばれ、条件の評価を毎回行うと通常のプレイに影響する（設計書 09 編 §9.6）。
//
// エミュレーションゴルーチンだけが触る。
type BreakpointSet struct {
	list   []*Breakpoint
	nextID int
	counts [breakKindCount]int
	events [eventKindCount]int
}

// NewBreakpointSet は空の一覧を作る。
func NewBreakpointSet() *BreakpointSet { return &BreakpointSet{nextID: 1} }

// Add はブレークポイントを加え、割り当てた ID を返す。
func (s *BreakpointSet) Add(b Breakpoint) int {
	if b.AddrEnd < b.AddrStart {
		b.AddrEnd = b.AddrStart
	}
	b.ID = s.nextID
	s.nextID++
	bp := b
	s.list = append(s.list, &bp)
	s.recount()
	return b.ID
}

// Remove はブレークポイントを取り除く。
func (s *BreakpointSet) Remove(id int) {
	for i, b := range s.list {
		if b.ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			break
		}
	}
	s.recount()
}

// SetEnabled は有効・無効を切り替える。
func (s *BreakpointSet) SetEnabled(id int, on bool) {
	if b := s.find(id); b != nil {
		b.Enabled = on
	}
	s.recount()
}

// SetCondition は条件式を差し替える。nil で条件を外す。
func (s *BreakpointSet) SetCondition(id int, c *Condition) {
	if b := s.find(id); b != nil {
		b.Condition = c
	}
}

// find は ID のブレークポイントを返す。
func (s *BreakpointSet) find(id int) *Breakpoint {
	for _, b := range s.list {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// recount は有効件数を数え直す。
func (s *BreakpointSet) recount() {
	s.counts = [breakKindCount]int{}
	s.events = [eventKindCount]int{}
	for _, b := range s.list {
		if !b.Enabled {
			continue
		}
		s.counts[b.Kind]++
		if b.Kind == BreakEvent {
			s.events[b.Event]++
		}
	}
}

// Count は種別の有効件数を返す。
func (s *BreakpointSet) Count(k BreakKind) int { return s.counts[k] }

// EventCount はイベントの有効件数を返す。
func (s *BreakpointSet) EventCount(e EventKind) int { return s.events[e] }

// Empty は有効なブレークポイントが無いかを返す。
func (s *BreakpointSet) Empty() bool {
	for _, n := range s.counts {
		if n > 0 {
			return false
		}
	}
	return true
}

// List は一覧の写しを返す。
func (s *BreakpointSet) List() []Breakpoint {
	out := make([]Breakpoint, 0, len(s.list))
	for _, b := range s.list {
		out = append(out, *b)
	}
	return out
}

// MatchAddr はアドレスに掛かる種別 k のブレークポイントを探す。
//
// 条件を満たしたものの HitCount を増やして返す。無ければ nil。
func (s *BreakpointSet) MatchAddr(k BreakKind, addr uint16, env Env) *Breakpoint {
	if s.counts[k] == 0 {
		return nil
	}
	for _, b := range s.list {
		if !b.Enabled || b.Kind != k || !b.covers(addr) {
			continue
		}
		if b.Condition.Eval(env) {
			b.HitCount++
			return b
		}
	}
	return nil
}

// MatchPPU は PPU の位置 (scanline, dot) に掛かるブレークポイントを探す。
func (s *BreakpointSet) MatchPPU(scanline, dot int, env Env) *Breakpoint {
	if s.counts[BreakPPUPosition] == 0 {
		return nil
	}
	for _, b := range s.list {
		if !b.Enabled || b.Kind != BreakPPUPosition {
			continue
		}
		if b.Scanline == scanline && b.Dot == dot && b.Condition.Eval(env) {
			b.HitCount++
			return b
		}
	}
	return nil
}

// MatchEvent はイベント e に掛かるブレークポイントを探す。
func (s *BreakpointSet) MatchEvent(e EventKind, env Env) *Breakpoint {
	if s.events[e] == 0 {
		return nil
	}
	for _, b := range s.list {
		if !b.Enabled || b.Kind != BreakEvent || b.Event != e {
			continue
		}
		if b.Condition.Eval(env) {
			b.HitCount++
			return b
		}
	}
	return nil
}
