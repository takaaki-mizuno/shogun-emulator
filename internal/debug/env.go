package debug

// Debugger は式の評価の取り出し口（ExprEnv）と、解析のときの名前の確認
// （Resolver）を兼ねる。どちらもエミュレーションゴルーチンで使う。

// PeekSpace は空間 s の addr を副作用なしに読む。範囲外は 0。
func (d *Debugger) PeekSpace(s Space, addr int) uint8 {
	if d.n == nil || addr < 0 || addr >= Size(d.n, s) {
		return 0
	}
	return readByte(d.n, s, addr)
}

// ResolveSymbol は Symbol の位置を返す。PRG-ROM の Symbol は PRG-ROM の
// オフセット、バンクを問わない Symbol は CPU アドレスで返す。
func (d *Debugger) ResolveSymbol(name string) (Space, int, int, bool) {
	sym, ok := d.symbols.Lookup(name)
	if !ok {
		return 0, 0, -1, false
	}
	if sym.Kind == SymConstant {
		// 定数は位置ではなく値である。位置の式でも値として使う。
		return SpaceCPU, int(sym.Value), -1, true
	}
	if sym.Loc.Space == SymSpacePRG {
		cpu := -1
		if sym.CPU != 0 {
			cpu = int(sym.CPU)
		}
		return SpacePRGROM, int(sym.Loc.Offset), cpu, true
	}
	return SpaceCPU, int(sym.Loc.Offset), -1, true
}

// Constant は定数の値を返す。
func (d *Debugger) Constant(name string) (int64, bool) {
	sym, ok := d.symbols.Lookup(name)
	if !ok || sym.Kind != SymConstant {
		return 0, false
	}
	return sym.Value, true
}

// HasSymbol は Symbol があるかを返す。
func (d *Debugger) HasSymbol(name string) bool { return d.symbols.HasSymbol(name) }

// HasGameValue は式で使える Game State の項目かを返す。配列は式で使えない。
func (d *Debugger) HasGameValue(name string) bool {
	it, ok := d.symbols.GameState().Item(name)
	return ok && it.Count <= 1
}

// GameValue は Game State の項目の値を返す。
func (d *Debugger) GameValue(name string) (Value, bool) {
	it, ok := d.symbols.GameState().Item(name)
	if !ok || it.Count > 1 {
		return Value{}, false
	}
	_, v, err := d.readItem(it, 0)
	if err != nil {
		return Value{}, false
	}
	return v, true
}

// Position は実行の位置を返す。
func (d *Debugger) Position(p Position) int64 {
	if d.n == nil {
		return 0
	}
	switch p {
	case PosFrame:
		return int64(d.n.Frames())
	case PosScanline:
		return int64(d.n.PPU.Scanline())
	case PosDot:
		return int64(d.n.PPU.Dot())
	case PosCycles:
		return int64(d.n.Cycles())
	}
	return 0
}

// Label は CPU アドレスの位置の名前を、現在のバンク構成で引く。
func (d *Debugger) Label(addr uint16) string {
	if d.n == nil {
		return d.symbols.LabelAt(addr, nil)
	}
	return d.symbols.LabelAt(addr, d.n.Cart.PRGOffset)
}

// NearestLabel は addr 以前で最も近い名前を "name+3" の形で返す。無ければ空。
func (d *Debugger) NearestLabel(addr uint16) string {
	var off Offsetter
	if d.n != nil {
		off = d.n.Cart.PRGOffset
	}
	name, delta, ok := d.symbols.NearestAt(addr, off)
	if !ok {
		return ""
	}
	if delta == 0 {
		return name
	}
	return name + "+" + itoa(delta)
}

// Source は CPU アドレスに当たるソースの位置を返す。
func (d *Debugger) Source(addr uint16) (SourceRef, bool) {
	if d.n == nil {
		return SourceRef{}, false
	}
	off, ok := d.n.Cart.PRGOffset(addr)
	if !ok {
		return SourceRef{}, false
	}
	return d.symbols.SourceAt(off)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// readItem は Game State の項目の要素 index を読む。
func (d *Debugger) readItem(it GameStateItem, index int) (any, Value, error) {
	e, err := d.addrExpr(it.Loc)
	if err != nil {
		return nil, Value{}, err
	}
	space, addr, _ := e.Eval(d)
	addr += index * it.stride()
	b := make([]uint8, it.ByteSize())
	for i := range b {
		b[i] = d.PeekSpace(space, addr+i)
	}
	j, v := it.Decode(b)
	return j, v, nil
}

// addrExpr は位置の式を解析する。命令ごとの判定で毎回解析し直さないよう、
// 解析した結果を Symbol の版ごとに覚える。
func (d *Debugger) addrExpr(loc string) (*AddrExpr, error) {
	gen := d.symbols.Generation()
	if d.exprGen != gen || d.exprCache == nil {
		d.exprCache = map[string]*AddrExpr{}
		d.exprGen = gen
	}
	if e, ok := d.exprCache[loc]; ok {
		return e, nil
	}
	e, err := ParseAddrExpr(loc, d)
	if err != nil {
		return nil, err
	}
	d.exprCache[loc] = e
	return e, nil
}

// GameStateValue は Game State の項目の値を JSON に出す形で返す。配列は要素の
// 並びにする。
func (d *Debugger) GameStateValue(it GameStateItem) (any, error) {
	if it.Count <= 1 {
		j, _, err := d.readItem(it, 0)
		return j, err
	}
	out := make([]any, it.Count)
	for i := range it.Count {
		j, _, err := d.readItem(it, i)
		if err != nil {
			return nil, err
		}
		out[i] = j
	}
	return out, nil
}

// SymbolItem は .dbg の変数の Symbol を Game State の項目として読む形にする
// （sym:<名前>、設計書 14 編 §14.12.3）。size が 1 なら u8、2 なら u16、
// それ以外は u8 の配列とする。
func (d *Debugger) SymbolItem(name string) (GameStateItem, bool) {
	sym, ok := d.symbols.Lookup(name)
	if !ok || sym.Kind == SymConstant {
		return GameStateItem{}, false
	}
	it := GameStateItem{Name: "sym:" + name, Loc: name, Type: "u8"}
	switch {
	case sym.Size == 2:
		it.Type = "u16"
	case sym.Size > 2:
		it.Count = int(sym.Size)
	}
	return it, true
}

// GameStateValues は Game State の有効な項目の現在値を返す。hidden の項目も
// 含める。読めない項目は飛ばす。
func (d *Debugger) GameStateValues() map[string]any {
	out := map[string]any{}
	for _, it := range d.symbols.GameState().Items() {
		if _, bad := d.symbols.GameState().Invalid(it.Name); bad {
			continue
		}
		if v, err := d.GameStateValue(it); err == nil {
			out[it.Name] = v
		}
	}
	return out
}

// HiddenGameState は差分に含めない項目の名前を返す。
func (d *Debugger) HiddenGameState() []string {
	var out []string
	for _, it := range d.symbols.GameState().Items() {
		if it.Hidden {
			out = append(out, it.Name)
		}
	}
	return out
}
