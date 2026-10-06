package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// Game State の Agent Command（設計書 14 編 §14.7.2、§14.12）。

func registerGameState(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("gamestate.get", ClassObserve, gsGetParams{}, nil, "Game State の値を返す。names を省くとすべて。sym:<名前> で .dbg の変数を読む",
		"Read Game State values (all, or the given names). 'sym:<name>' reads a .dbg variable without a definition.", handleGameStateGet)
	reg("gamestate.define", ClassConfig, gsDefineParams{}, nil, "Game State Definition の項目を追加または置き換える（ファイルには書かない）",
		"Add or replace a Game State item (name, loc, type u8/s8/u16/s16/u24/u32/bcd/digits/bool/bits, enum, bits, count, stride, desc, hidden). Not saved until gamestate.save.", handleGameStateDefine)
	reg("gamestate.remove", ClassConfig, gsNameParams{}, []string{"name"}, "Game State Definition の項目を消す",
		"Remove a Game State item.", handleGameStateRemove)
	reg("gamestate.list", ClassObserve, InstanceParam{}, nil, "Game State Definition の項目と説明、誤りのある項目の理由",
		"List Game State items with descriptions, and the reasons for invalid items.", handleGameStateList)
	reg("gamestate.save", ClassConfig, InstanceParam{}, nil, "Game State Definition をファイルに書く",
		"Save the Game State definition to <rom>.gamestate.json next to the ROM (or the data directory).", handleGameStateSave)
	reg("gamestate.load", ClassConfig, gsLoadParams{}, []string{"path"}, "Game State Definition をファイルから読み込み、今の定義と置き換える",
		"Load a Game State definition file (replacing the current definition). Later gamestate_save writes back to this file.", handleGameStateLoad)
}

type gsGetParams struct {
	InstanceParam
	Names []string `json:"names,omitempty" desc:"項目の名前。sym:<名前> で .dbg の変数"`
}

func handleGameStateGet(c *Context, raw json.RawMessage) (any, error) {
	var p gsGetParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	out := map[string]any{}
	var gerr error
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		if len(p.Names) == 0 {
			out = d.GameStateValues()
			return
		}
		def := d.Symbols().GameState()
		for _, name := range p.Names {
			var it debug.GameStateItem
			var ok bool
			if sym, isSym := strings.CutPrefix(name, "sym:"); isSym {
				it, ok = d.SymbolItem(sym)
			} else {
				it, ok = def.Item(name)
				if !ok {
					if reason, bad := def.Invalid(name); bad {
						gerr = Errorf(KindInvalidParams, "項目 %q は誤りがあり無効である: %s", name, reason)
						return
					}
				}
			}
			if !ok {
				gerr = Errorf(KindInvalidParams, "項目 %q は無い（gamestate.list で一覧を見る）", name)
				return
			}
			v, err := d.GameStateValue(it)
			if err != nil {
				gerr = locationError(it.Loc, err, d.Symbols())
				return
			}
			out[name] = v
		}
	})
	if gerr != nil {
		return nil, gerr
	}
	return out, nil
}

type gsDefineParams struct {
	InstanceParam
	debug.GameStateItem
}

func handleGameStateDefine(c *Context, raw json.RawMessage) (any, error) {
	var p gsDefineParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	it := p.GameStateItem
	if err := it.Validate(); err != nil {
		return nil, Errorf(KindInvalidParams, "%v", err)
	}
	var derr error
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		// 位置の書き方を確かめる。Symbol の名前で書いた項目は、ROM を作り直して
		// 位置が変わっても追従する。
		if _, err := debug.ParseAddrExpr(it.Loc, d); err != nil {
			derr = locationError(it.Loc, err, d.Symbols())
			return
		}
		derr = d.Symbols().GameState().Define(it)
	})
	if derr != nil {
		return nil, AsError(derr)
	}
	return handleGameStateList(c, nil)
}

type gsNameParams struct {
	InstanceParam
	Name string `json:"name" desc:"項目の名前"`
}

func handleGameStateRemove(c *Context, raw json.RawMessage) (any, error) {
	var p gsNameParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	syms := c.Instance.symbols()
	if syms == nil || !syms.GameState().Remove(p.Name) {
		return nil, Errorf(KindInvalidParams, "項目 %q は無い", p.Name)
	}
	return handleGameStateList(c, nil)
}

// GameStateItemInfo は gamestate.list の 1 件。
type GameStateItemInfo struct {
	debug.GameStateItem
	Error string `json:"error,omitempty"`
}

func handleGameStateList(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	def := syms.GameState()
	items := []GameStateItemInfo{}
	for _, it := range def.Items() {
		info := GameStateItemInfo{GameStateItem: it}
		if reason, bad := def.Invalid(it.Name); bad {
			info.Error = reason
		}
		items = append(items, info)
	}
	return struct {
		Items []GameStateItemInfo `json:"items"`
		Path  string              `json:"path,omitempty"`
	}{items, def.Path()}, nil
}

// gameStateSavePath は保存先を決める（設計書 14 編 §14.12.1）。読み込んだ
// 場所があればそこ、無ければ ROM のディレクトリに書けるならそこ、書けなければ
// データディレクトリ。
func (h *Host) gameStateSavePath(inst *Instance, def *debug.GameStateDef) (string, error) {
	if p := def.Path(); p != "" {
		return p, nil
	}
	paths := debug.ProjectPathsFor(inst.romPath, h.opts.EmuConfig.Dirs.Data, inst.Emu.Status().ROMKey)
	if paths.GameStateLocal != "" && dirWritable(filepath.Dir(paths.GameStateLocal)) {
		return paths.GameStateLocal, nil
	}
	if paths.GameStateData != "" {
		return paths.GameStateData, nil
	}
	return "", Errorf(KindIOError, "保存先を決められない（ROM のパスもデータディレクトリも分からない）")
}

// dirWritable はディレクトリにファイルを作れるかを返す。
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".shogun-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func handleGameStateSave(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	def := syms.GameState()
	path, err := c.Host.gameStateSavePath(c.Instance, def)
	if err != nil {
		return nil, err
	}
	if err := def.Save(path); err != nil {
		return nil, Errorf(KindIOError, "書けない: %v", err)
	}
	return struct {
		Path  string `json:"path"`
		Items int    `json:"items"`
	}{path, len(def.Items())}, nil
}

type gsLoadParams struct {
	InstanceParam
	Path string `json:"path" desc:"Game State Definition のファイル"`
}

// handleGameStateLoad は Game State Definition を読み込む（設計書 14 編
// §14.12.3）。同じ ROM の Instance が共有する定義を置き換える。
func handleGameStateLoad(c *Context, raw json.RawMessage) (any, error) {
	var p gsLoadParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == "" {
		return nil, Errorf(KindInvalidParams, "path を指定する")
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	def, err := debug.LoadGameStateFile(p.Path)
	if err != nil {
		return nil, Errorf(KindIOError, "読めない: %v", err)
	}
	syms.SetGameState(def)
	invalid := []string{}
	for _, it := range def.Items() {
		if reason, bad := def.Invalid(it.Name); bad {
			invalid = append(invalid, it.Name+": "+reason)
		}
	}
	return struct {
		Path    string   `json:"path"`
		Items   int      `json:"items"`
		Invalid []string `json:"invalid,omitempty"`
	}{p.Path, len(def.Items()), invalid}, nil
}
