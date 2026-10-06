package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// CommandClass は Agent Command の分類（設計書 14 編 §14.7.1）。
type CommandClass uint8

// Agent Command の分類。
const (
	// ClassObserve は観測。Control 不要。Machine State を変えない。
	ClassObserve CommandClass = iota
	// ClassConfig はデバッグ設定。Control 不要。
	ClassConfig
	// ClassAdvance は進行。Control が必要。
	ClassAdvance
	// ClassMutate は Machine State の書き換え、ROM・ステートの読み込み。
	// Control が必要。
	ClassMutate
	// ClassSession は接続と Instance の管理。
	ClassSession
)

var classNames = []string{"observe", "config", "advance", "mutate", "session"}

// String は分類の名前を返す。
func (c CommandClass) String() string {
	if int(c) < len(classNames) {
		return classNames[c]
	}
	return "unknown"
}

// MarshalJSON は分類を名前で書く。
func (c CommandClass) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON は名前から分類を読む。
func (c *CommandClass) UnmarshalJSON(b []byte) error {
	i, err := nameIndex(b, classNames)
	*c = CommandClass(i)
	return err
}

// NeedsControl は Control が必要な分類かを返す。
func (c CommandClass) NeedsControl() bool { return c == ClassAdvance || c == ClassMutate }

// Handler は Agent Command の処理。結果は JSON に直せる値とする。
type Handler func(ctx *Context, params json.RawMessage) (any, error)

// CommandSpec は Agent Command 1 つの定義。
type CommandSpec struct {
	// Name は JSON-RPC の名前（"instance.fork"）。
	Name  string
	Class CommandClass
	// Params は引数の構造体の零値。JSON Schema の生成と引数の解釈に使う。
	// 引数を持たないとき nil。
	Params any
	// Positional は CLI の位置引数を入れる引数の名前の順。
	Positional []string
	DescJA     string
	DescEN     string
	// GUI は GUI 版の Host で受け付けるかを表す。
	GUI bool
	// Headless は headless の Host で受け付けるかを表す。
	Headless bool
	// Target は対象の Instance を引数 instance で選ぶことを表す。
	Target  bool
	Handler Handler

	schema *Schema
}

// Schema は引数の JSON Schema を返す。
func (s *CommandSpec) Schema() *Schema { return s.schema }

// Image は Agent Command の結果に含める画像。JSON では data を Base64 の
// 文字列にする（encoding/json の []byte の扱い）。MCP ブリッジは mime と
// data を持つ値を ImageContent に、shogun ctl は --out のファイルに直す。
type Image struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// InstanceParam は対象の Instance を選ぶ引数。Target の Agent Command の
// 引数の構造体に埋め込む。
type InstanceParam struct {
	Instance string `json:"instance,omitempty" desc:"対象の Instance の ID。1 つだけなら省略できる"`
}

// Registry は Agent Command の登録簿（設計書 14 編 §14.7.1）。
//
// JSON-RPC の振り分け、session.commands、CLI のヘルプと引数の変換、
// MCP のツール定義はすべてここから作る。
type Registry struct {
	specs []*CommandSpec
}

// NewRegistry は空の登録簿を作る。
func NewRegistry() *Registry { return &Registry{} }

// Register は Agent Command を登録する。
//
// 名前の重複、使えない文字、MCP の名前の衝突は起動時の誤りであり、
// panic する。エミュレーションの途中で起きることはない。
func (r *Registry) Register(spec CommandSpec) {
	if !ValidName(spec.Name) {
		panic(fmt.Sprintf("agent: Agent Command の名前 %q に使えない文字がある", spec.Name))
	}
	for _, s := range r.specs {
		if s.Name == spec.Name {
			panic(fmt.Sprintf("agent: Agent Command %q を 2 回登録した", spec.Name))
		}
		if MCPName(s.Name) == MCPName(spec.Name) {
			panic(fmt.Sprintf("agent: Agent Command %q と %q の MCP の名前が衝突する", s.Name, spec.Name))
		}
	}
	if spec.Handler == nil {
		panic(fmt.Sprintf("agent: Agent Command %q に処理が無い", spec.Name))
	}
	spec.schema = SchemaOf(spec.Params)
	for _, name := range spec.Positional {
		if _, ok := spec.schema.Property(name); !ok {
			panic(fmt.Sprintf("agent: Agent Command %q の位置引数 %q が引数に無い", spec.Name, name))
		}
	}
	r.specs = append(r.specs, &spec)
}

// Lookup は JSON-RPC の名前で引く。
func (r *Registry) Lookup(name string) (*CommandSpec, bool) {
	for _, s := range r.specs {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// LookupMCP は MCP のツール名で引く。
func (r *Registry) LookupMCP(name string) (*CommandSpec, bool) {
	for _, s := range r.specs {
		if MCPName(s.Name) == name {
			return s, true
		}
	}
	return nil, false
}

// LookupCLI は CLI の語の並びで引く。
func (r *Registry) LookupCLI(words []string) (*CommandSpec, bool) {
	return r.Lookup(FromCLIWords(words))
}

// All は登録した順に全 Agent Command を返す。
func (r *Registry) All() []*CommandSpec { return append([]*CommandSpec(nil), r.specs...) }

// Context は Agent Command 1 回分の文脈。
type Context struct {
	// Ctx は要求の取り消しを伝える。
	Ctx context.Context
	// Conn は要求を送った接続。
	Conn *Conn
	Host *Host
	// Instance は対象の Instance。Target でない Agent Command では nil。
	Instance *Instance
	Spec     *CommandSpec
}

// DecodeParams は引数を v へ読む。知らない引数を誤りとする。
//
// 誤りは invalid_params として返す。引数を持たない Agent Command に
// 空の引数（null・{}・省略）を渡すことは誤りとしない。
func DecodeParams(raw json.RawMessage, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errorf(KindInvalidParams, "引数を解釈できない: %v", err)
	}
	return nil
}
