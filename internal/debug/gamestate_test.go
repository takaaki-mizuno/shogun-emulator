package debug

import (
	"reflect"
	"testing"
)

// TestGameStateDecode は各型と修飾の解釈を確かめる（設計書 14 編 §14.12.2）。
func TestGameStateDecode(t *testing.T) {
	cases := []struct {
		item GameStateItem
		b    []uint8
		want any
	}{
		{GameStateItem{Type: "u8"}, []uint8{200}, int64(200)},
		{GameStateItem{Type: "s8"}, []uint8{0xFE}, int64(-2)},
		{GameStateItem{Type: "u16"}, []uint8{0x34, 0x12}, int64(0x1234)},
		{GameStateItem{Type: "u16", Order: "big"}, []uint8{0x12, 0x34}, int64(0x1234)},
		{GameStateItem{Type: "s16"}, []uint8{0xFF, 0xFF}, int64(-1)},
		{GameStateItem{Type: "u24"}, []uint8{1, 2, 3}, int64(0x030201)},
		{GameStateItem{Type: "u32", Order: "big"}, []uint8{0, 0, 1, 0}, int64(256)},
		{GameStateItem{Type: "bcd", Size: 3}, []uint8{0x01, 0x23, 0x45}, int64(12345)},
		{GameStateItem{Type: "bcd", Size: 2, Order: "little"}, []uint8{0x45, 0x23}, int64(2345)},
		{GameStateItem{Type: "digits", Size: 3}, []uint8{1, 2, 3}, int64(123)},
		{GameStateItem{Type: "bool"}, []uint8{0}, false},
		{GameStateItem{Type: "bool"}, []uint8{5}, true},
		{GameStateItem{Type: "bits", Bits: map[string]string{"0": "jumping", "7": "left", "3": "unused"}}, []uint8{0x81}, []string{"jumping", "left"}},
		{GameStateItem{Type: "u8", Enum: map[string]string{"0": "title", "1": "play"}}, []uint8{1}, "play"},
		{GameStateItem{Type: "u8", Enum: map[string]string{"0": "title"}}, []uint8{9}, int64(9)},
	}
	for _, c := range cases {
		got, _ := c.item.Decode(c.b)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v %v = %#v, 期待 %#v", c.item, c.b, got, c.want)
		}
	}
}

// TestGameStateValidate は項目の誤りを検出することを確かめる。
func TestGameStateValidate(t *testing.T) {
	ok := GameStateItem{Name: "x", Loc: "$00", Type: "u8"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []GameStateItem{
		{Name: "", Loc: "$00", Type: "u8"},
		{Name: "1x", Loc: "$00", Type: "u8"},
		{Name: "x", Loc: "", Type: "u8"},
		{Name: "x", Loc: "$00", Type: "u9"},
		{Name: "x", Loc: "$00", Type: "bcd"},
		{Name: "x", Loc: "$00", Type: "u16", Size: 3},
		{Name: "x", Loc: "$00", Type: "u8", Order: "middle"},
		{Name: "x", Loc: "$00", Type: "u8", Enum: map[string]string{"one": "a"}},
		{Name: "x", Loc: "$00", Type: "bits", Bits: map[string]string{"8": "a"}},
		{Name: "x", Loc: "$00", Type: "u8", Count: -1},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v を受け付けた", bad)
		}
	}
}

// TestGameStateDefine は定義の追加・置き換え・削除と保存を確かめる。
func TestGameStateDefine(t *testing.T) {
	d := NewGameStateDef()
	if err := d.Define(GameStateItem{Name: "a", Loc: "$00", Type: "u8"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Define(GameStateItem{Name: "a", Loc: "$01", Type: "u8"}); err != nil {
		t.Fatal(err)
	}
	if it, _ := d.Item("a"); it.Loc != "$01" || len(d.Items()) != 1 {
		t.Errorf("置き換え = %+v", d.Items())
	}
	path := t.TempDir() + "/x.gamestate.json"
	if err := d.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := LoadGameStateFile(path)
	if err != nil || len(again.Items()) != 1 || again.Path() != path {
		t.Errorf("読み直し = %+v, %v", again, err)
	}
	if !d.Remove("a") || d.Remove("a") {
		t.Error("削除の結果が違う")
	}
}
