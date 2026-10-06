package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// セーブステートと ROM の Agent Command（設計書 14 編 §14.7.2）。

// namedState は Instance 内の名前付きの保管場所の 1 件。ファイルに書かない。
type namedState struct {
	name    string
	frame   uint64
	created time.Time
	data    []byte
}

func registerState(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("state.save", ClassObserve, stateParams{}, []string{"name"}, "セーブステートを作る。path でファイル、name で Instance 内に保管",
		"Save a state to a file (path) or to a named in-memory slot of this instance (name).", handleStateSave)
	reg("state.load", ClassMutate, stateParams{}, []string{"name"}, "セーブステートを読み込む",
		"Load a state from a file (path) or a named in-memory slot (name).", handleStateLoad)
	reg("state.list", ClassObserve, InstanceParam{}, nil, "Instance 内に保管したセーブステートの一覧",
		"List in-memory saved states of this instance.", handleStateList)
	reg("rom.load", ClassMutate, romLoadParams{}, []string{"path"}, "ROM を読み込み、一時停止した状態にする",
		"Load a ROM file into this instance and pause.", handleROMLoad)
}

type stateParams struct {
	InstanceParam
	Name string `json:"name,omitempty" desc:"Instance 内の保管場所の名前"`
	Path string `json:"path,omitempty" desc:"ファイルのパス"`
}

func handleStateSave(c *Context, raw json.RawMessage) (any, error) {
	var p stateParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	st := c.Instance.Emu.Status()
	var notes []string
	if st.MidInstruction {
		notes = append(notes, "命令の途中で止まっていたため、命令を完了させてから保存した")
	}
	if p.Path != "" {
		if err := c.Instance.Emu.SaveToFile(p.Path); err != nil {
			return nil, Errorf(KindIOError, "保存できない: %v", err)
		}
		return struct {
			Path  string   `json:"path"`
			Frame uint64   `json:"frame"`
			Notes []string `json:"notes,omitempty"`
		}{p.Path, c.Instance.Emu.Status().Frames, notes}, nil
	}
	data, err := c.Instance.Emu.SaveState()
	if err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	frame := c.Instance.Emu.Status().Frames
	name := p.Name
	if name == "" {
		name = fmt.Sprintf("auto-%d", frame)
	}
	inst := c.Instance
	inst.mu.Lock()
	ns := namedState{name: name, frame: frame, created: time.Now(), data: data}
	replaced := false
	for i, s := range inst.states {
		if s.name == name {
			inst.states[i], replaced = ns, true
		}
	}
	if !replaced {
		inst.states = append(inst.states, ns)
	}
	inst.mu.Unlock()
	return struct {
		Name  string   `json:"name"`
		Frame uint64   `json:"frame"`
		Notes []string `json:"notes,omitempty"`
	}{name, frame, notes}, nil
}

func handleStateLoad(c *Context, raw json.RawMessage) (any, error) {
	var p stateParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if (p.Name == "") == (p.Path == "") {
		return nil, Errorf(KindInvalidParams, "name か path のどちらか一方を指定する")
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	if p.Path != "" {
		if err := c.Instance.Emu.LoadFromFile(p.Path); err != nil {
			return nil, Errorf(KindIOError, "読み込めない: %v", err)
		}
	} else {
		var data []byte
		c.Instance.mu.Lock()
		for _, s := range c.Instance.states {
			if s.name == p.Name {
				data = s.data
			}
		}
		c.Instance.mu.Unlock()
		if data == nil {
			return nil, Errorf(KindInvalidParams, "保管場所 %q は無い", p.Name)
		}
		if err := c.Instance.Emu.LoadState(data); err != nil {
			return nil, Errorf(KindInternalError, "%v", err)
		}
	}
	// Freeze は状態に含めない。読み込んだ状態にも固定した値を書き直す。常時記録は
	// 読み込んだ状態から始め直すため、Freeze を記録し直す。
	if f := c.Instance.freezes(); len(f) > 0 {
		_ = c.Instance.Emu.SetFreezes(nil)
		_ = c.Instance.Emu.SetFreezes(f)
	}
	return c.Host.observe(c.Instance, c.Conn, observeSpec{})
}

// StateInfo は state.list の 1 件。
type StateInfo struct {
	Name    string `json:"name"`
	Frame   uint64 `json:"frame"`
	Created string `json:"created"`
}

func handleStateList(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	out := []StateInfo{}
	c.Instance.mu.Lock()
	for _, s := range c.Instance.states {
		out = append(out, StateInfo{Name: s.name, Frame: s.frame, Created: s.created.Format(time.RFC3339)})
	}
	c.Instance.mu.Unlock()
	return out, nil
}

type romLoadParams struct {
	InstanceParam
	Path string `json:"path" desc:"ROM のパス"`
}

func handleROMLoad(c *Context, raw json.RawMessage) (any, error) {
	var p romLoadParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, Errorf(KindIOError, "ROM を読めない: %v", err)
	}
	inst := c.Instance
	name := strings.TrimSuffix(filepath.Base(p.Path), filepath.Ext(p.Path))
	if err := inst.Emu.LoadROMDataFrom(data, name, p.Path); err != nil {
		return nil, Errorf(KindIOError, "ROM を読み込めない: %v", err)
	}
	inst.Emu.Pause()
	c.Host.detachShared(inst)
	inst.romName, inst.romPath, inst.romData = name, p.Path, data
	c.Host.attachShared(inst)
	inst.mu.Lock()
	inst.states = nil
	inst.mu.Unlock()
	ob, err := c.Host.observe(inst, c.Conn, observeSpec{})
	if err != nil {
		return nil, err
	}
	ob.Notes = append(ob.Notes, inst.Emu.ProjectNotes()...)
	return ob, nil
}
