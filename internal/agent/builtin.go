package agent

import "encoding/json"

// registerBuiltin は session・instance・control の Agent Command を登録する
// （設計書 14 編 §14.7.2）。
func registerBuiltin(r *Registry) {
	r.Register(CommandSpec{
		Name: "session.hello", Class: ClassSession, Params: helloParams{},
		DescJA: "認証と版の確認。接続して最初に送る",
		DescEN: "Authenticate with the token and negotiate the API version. Must be the first request on a connection.",
		GUI:    true, Headless: true, Handler: handleHello,
	})
	r.Register(CommandSpec{
		Name: "session.commands", Class: ClassSession,
		DescJA: "Agent Command の一覧と引数のスキーマを返す",
		DescEN: "List all Agent Commands with their parameter schemas.",
		GUI:    true, Headless: true, Handler: handleCommands,
	})
	r.Register(CommandSpec{
		Name: "instance.list", Class: ClassSession,
		DescJA: "Instance の一覧（ID、ROM、フレーム番号、Control の持ち主、進行モード）",
		DescEN: "List emulator instances with ROM, frame number, control owner and pacing mode.",
		GUI:    true, Headless: true, Handler: handleInstanceList,
	})
	r.Register(CommandSpec{
		Name: "instance.create", Class: ClassSession, Params: createParams{}, Positional: []string{"rom"},
		DescJA:   "新しい Instance を作り、ROM を読み込む。作った接続が Control を持つ",
		DescEN:   "Create a new headless instance and load a ROM. The calling connection gets control of it.",
		Headless: true, Handler: handleInstanceCreate,
	})
	r.Register(CommandSpec{
		Name: "instance.fork", Class: ClassSession, Params: InstanceParam{}, Positional: []string{"instance"},
		DescJA:   "Instance の現在の状態を複製した新しい Instance を作る",
		DescEN:   "Fork an instance: create a new instance with a copy of its current machine state.",
		Headless: true, Target: true, Handler: handleInstanceFork,
	})
	r.Register(CommandSpec{
		Name: "instance.close", Class: ClassSession, Params: InstanceParam{}, Positional: []string{"instance"},
		DescJA:   "Instance を閉じる",
		DescEN:   "Close an instance.",
		Headless: true, Target: true, Handler: handleInstanceClose,
	})
	r.Register(CommandSpec{
		Name: "control.acquire", Class: ClassSession, Params: InstanceParam{}, Positional: []string{"instance"},
		DescJA: "Control を得る。得ると Agent-Paced になり一時停止する",
		DescEN: "Acquire control of an instance. The instance pauses and becomes agent-paced.",
		GUI:    true, Headless: true, Target: true, Handler: handleControlAcquire,
	})
	r.Register(CommandSpec{
		Name: "control.release", Class: ClassSession, Params: InstanceParam{}, Positional: []string{"instance"},
		DescJA: "Control を返す。GUI 版では人間に戻り Real-Time になる",
		DescEN: "Release control. In the GUI the human gets control back and the game runs in real time.",
		GUI:    true, Headless: true, Target: true, Handler: handleControlRelease,
	})
	r.Register(CommandSpec{
		Name: "control.status", Class: ClassSession, Params: InstanceParam{}, Positional: []string{"instance"},
		DescJA: "Control の持ち主と進行モードを返す",
		DescEN: "Show who holds control of an instance and its pacing mode.",
		GUI:    true, Headless: true, Target: true, Handler: handleControlStatus,
	})
}

type helloParams struct {
	Token      string `json:"token" desc:"発見ファイルに書かれたトークン"`
	Client     string `json:"client,omitempty" desc:"クライアントの名前。GUI のバナーに出す"`
	APIVersion int    `json:"api_version,omitempty" desc:"クライアントが話す Agent Interface の版" default:"1"`
}

// HelloResult は session.hello の結果。
type HelloResult struct {
	APIVersion int          `json:"api_version"`
	Server     string       `json:"server"`
	Kind       Kind         `json:"kind"`
	Instances  []InstanceID `json:"instances"`
}

// handleHello は版を確かめる。トークンの照合は Transport が行う。
// トークンを持つのは待ち受けた側であり、Host は知らないためである。
func handleHello(c *Context, raw json.RawMessage) (any, error) {
	var p helloParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.APIVersion != 0 && p.APIVersion != APIVersion {
		return nil, Errorf(KindInvalidParams, "api_version %d には対応していない（%d だけ）", p.APIVersion, APIVersion)
	}
	c.Conn.SetAuthenticated(p.Client)
	ids := []InstanceID{}
	for _, inst := range c.Host.Instances() {
		ids = append(ids, inst.ID)
	}
	return HelloResult{APIVersion: APIVersion, Server: c.Host.opts.Server, Kind: c.Host.opts.Kind, Instances: ids}, nil
}

// CommandInfo は session.commands の 1 件。
type CommandInfo struct {
	Name       string       `json:"name"`
	MCPName    string       `json:"mcp_name"`
	CLI        []string     `json:"cli"`
	Class      CommandClass `json:"class"`
	DescJA     string       `json:"desc_ja"`
	DescEN     string       `json:"desc_en"`
	GUI        bool         `json:"gui"`
	Headless   bool         `json:"headless"`
	Target     bool         `json:"target"`
	Positional []string     `json:"positional,omitempty"`
	// Params は引数の JSON Schema。受け取った側がそのまま MCP のツール
	// 定義に渡せるよう、JSON のまま持つ。
	Params json.RawMessage `json:"params"`
}

// CommandInfos は登録簿の全 Agent Command の説明を返す。
func CommandInfos(r *Registry) []CommandInfo {
	var out []CommandInfo
	for _, s := range r.All() {
		schema, err := json.Marshal(s.Schema())
		if err != nil {
			// 登録時に作ったスキーマは必ず JSON にできる。
			panic(err)
		}
		out = append(out, CommandInfo{
			Name: s.Name, MCPName: MCPName(s.Name), CLI: CLIWords(s.Name), Class: s.Class,
			DescJA: s.DescJA, DescEN: s.DescEN, GUI: s.GUI, Headless: s.Headless, Target: s.Target,
			Positional: s.Positional, Params: schema,
		})
	}
	return out
}

func handleCommands(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &struct{}{}); err != nil {
		return nil, err
	}
	return CommandInfos(c.Host.registry), nil
}

// InstanceInfo は instance.list の 1 件。
type InstanceInfo struct {
	ID      InstanceID    `json:"id"`
	ROM     string        `json:"rom"`
	Loaded  bool          `json:"loaded"`
	Frame   uint64        `json:"frame"`
	Paused  bool          `json:"paused"`
	Control ControlStatus `json:"control"`
	// Notes は ROM の読み込みで .dbg と Game State Definition について知らせること。
	Notes []string `json:"notes,omitempty"`
}

// Info は Instance の概要を返す。
func (inst *Instance) Info() InstanceInfo {
	st := inst.Emu.Status()
	return InstanceInfo{
		ID: inst.ID, ROM: st.ROMName, Loaded: st.Loaded, Frame: st.Frames, Paused: st.Paused,
		Control: inst.ControlStatus(),
	}
}

func handleInstanceList(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &struct{}{}); err != nil {
		return nil, err
	}
	out := []InstanceInfo{}
	for _, inst := range c.Host.Instances() {
		out = append(out, inst.Info())
	}
	return out, nil
}

type createParams struct {
	ROM           string `json:"rom,omitempty" desc:"読み込む ROM のパス"`
	State         string `json:"state,omitempty" desc:"ROM を読み込んだ後に読み込むセーブステートのパス"`
	Deterministic bool   `json:"deterministic,omitempty" desc:"値が定まらない状態をすべて固定値にする"`
	RAMInit       string `json:"ram_init,omitempty" desc:"RAM の初期化パターン（zero・ff・pattern・random）"`
	RAMSeed       uint64 `json:"ram_seed,omitempty" desc:"RAM 初期化の乱数シード"`
}

func handleInstanceCreate(c *Context, raw json.RawMessage) (any, error) {
	var p createParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	inst, err := c.Host.CreateInstance(c.Conn, p.ROM, p.State,
		InstanceOptions{Deterministic: p.Deterministic, RAMInit: p.RAMInit, RAMSeed: p.RAMSeed})
	if err != nil {
		return nil, err
	}
	info := inst.Info()
	info.Notes = inst.Emu.ProjectNotes()
	return info, nil
}

// ForkResult は instance.fork の結果。
type ForkResult struct {
	InstanceInfo
	Notes []string `json:"notes,omitempty"`
}

func handleInstanceFork(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	inst, notes, err := c.Host.Fork(c.Conn, c.Instance)
	if err != nil {
		return nil, err
	}
	return ForkResult{InstanceInfo: inst.Info(), Notes: notes}, nil
}

func handleInstanceClose(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	c.Host.CloseInstance(c.Instance)
	return struct {
		Closed InstanceID `json:"closed"`
	}{c.Instance.ID}, nil
}

func handleControlAcquire(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := c.Instance.acquire(c.Host, c.Conn); err != nil {
		return nil, err
	}
	return c.Instance.ControlStatus(), nil
}

func handleControlRelease(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if !c.Instance.release(c.Host, c.Conn) {
		return nil, Errorf(KindControlRequired, "この接続は Control を持っていない")
	}
	return c.Instance.ControlStatus(), nil
}

func handleControlStatus(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	return c.Instance.ControlStatus(), nil
}
