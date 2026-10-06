package agent

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func noop(*Context, json.RawMessage) (any, error) { return nil, nil }

// expectPanic は fn が panic することを確かめる。
func expectPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s で panic しない", what)
		}
	}()
	fn()
}

// TestRegistryRejectsBadNames は名前の重複・使えない文字・MCP の名前の
// 衝突を登録時に検出することを確かめる。
func TestRegistryRejectsBadNames(t *testing.T) {
	r := NewRegistry()
	r.Register(CommandSpec{Name: "a.b_c", Handler: noop})
	expectPanic(t, "重複", func() { r.Register(CommandSpec{Name: "a.b_c", Handler: noop}) })
	expectPanic(t, "MCP の名前の衝突", func() { r.Register(CommandSpec{Name: "a_b.c", Handler: noop}) })
	for _, bad := range []string{"", "A.b", "a-b", "a..b", ".a", "a.", "a b"} {
		expectPanic(t, "名前 "+bad, func() { r.Register(CommandSpec{Name: bad, Handler: noop}) })
	}
	expectPanic(t, "処理が無い", func() { r.Register(CommandSpec{Name: "x.y"}) })
	expectPanic(t, "知らない位置引数", func() {
		r.Register(CommandSpec{Name: "x.z", Params: InstanceParam{}, Positional: []string{"nope"}, Handler: noop})
	})
}

// TestNamesRoundTrip は登録簿のすべての名前が JSON-RPC・MCP・CLI の間で
// 往復できることを確かめる（設計書 14 編 §14.5.4）。
func TestNamesRoundTrip(t *testing.T) {
	r := NewDefaultRegistry()
	if len(r.All()) == 0 {
		t.Fatal("登録簿が空である")
	}
	for _, s := range r.All() {
		if got, ok := r.LookupMCP(MCPName(s.Name)); !ok || got != s {
			t.Errorf("%s: MCP の名前 %q から戻せない", s.Name, MCPName(s.Name))
		}
		words := CLIWords(s.Name)
		if FromCLIWords(words) != s.Name {
			t.Errorf("%s: CLI %v から戻せない", s.Name, words)
		}
		if got, ok := r.LookupCLI(words); !ok || got != s {
			t.Errorf("%s: CLI %v で引けない", s.Name, words)
		}
		for _, w := range words {
			if strings.Contains(w, "_") {
				t.Errorf("%s: CLI の語 %q にアンダースコアが残る", s.Name, w)
			}
		}
	}
}

type schemaSample struct {
	InstanceParam
	Frames int      `json:"frames,omitempty" desc:"進めるフレーム数" default:"1"`
	Input  string   `json:"input" desc:"入力"`
	Flag   bool     `json:"flag,omitempty"`
	List   []string `json:"list,omitempty"`
	Nested struct {
		X float64 `json:"x"`
	} `json:"nested,omitempty"`
	hidden int
}

// TestSchemaOf は引数の構造体から JSON Schema を作れることを確かめる。
func TestSchemaOf(t *testing.T) {
	s := SchemaOf(schemaSample{})
	if got := s.PropertyNames(); !slices.Equal(got, []string{"instance", "frames", "input", "flag", "list", "nested"}) {
		t.Errorf("プロパティの順 = %v", got)
	}
	if !slices.Equal(s.Required, []string{"input"}) {
		t.Errorf("必須 = %v, 期待 [input]", s.Required)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{"instance":{"type":"string","description":"対象の Instance の ID。1 つだけなら省略できる"},` +
		`"frames":{"type":"integer","description":"進めるフレーム数","default":1},"input":{"type":"string","description":"入力"},` +
		`"flag":{"type":"boolean"},"list":{"type":"array","items":{"type":"string"}},` +
		`"nested":{"type":"object","properties":{"x":{"type":"number"}},"required":["x"],"additionalProperties":false}},` +
		`"required":["input"],"additionalProperties":false}`
	if string(data) != want {
		t.Errorf("スキーマ =\n%s\n期待\n%s", data, want)
	}
	// 定義の順を保つ。2 回作っても同じ。
	data2, _ := json.Marshal(SchemaOf(schemaSample{}))
	if string(data) != string(data2) {
		t.Error("スキーマが実行ごとに変わる")
	}
}

// TestDecodeParams は知らない引数を invalid_params とすることを確かめる。
func TestDecodeParams(t *testing.T) {
	var p createParams
	if err := DecodeParams(json.RawMessage(`{"rom":"a.nes","bogus":1}`), &p); err == nil || AsError(err).Kind != KindInvalidParams {
		t.Errorf("知らない引数を受け付けた: %v", err)
	}
	if err := DecodeParams(nil, &p); err != nil {
		t.Errorf("引数の省略を誤りとした: %v", err)
	}
	if err := DecodeParams(json.RawMessage(`{"rom":5}`), &p); err == nil {
		t.Error("型の誤りを受け付けた")
	}
}
