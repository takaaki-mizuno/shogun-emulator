package debug

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"sync"
)

// Game State Definition（設計書 14 編 §14.12）。
//
// Game State の各項目（名前、場所、型、値の意味）を定める。ROM ごとに 1 つで、
// Symbols に置いて同じ ROM の Instance で共有する。

// gameStateVersion はファイルの形式のバージョン。
const gameStateVersion = 1

// GameStateItem は Game State の 1 項目。ファイルの items の 1 要素と同じ形。
type GameStateItem struct {
	Name   string            `json:"name"`
	Loc    string            `json:"loc"`
	Type   string            `json:"type"`
	Size   int               `json:"size,omitempty"`
	Order  string            `json:"order,omitempty"`
	Enum   map[string]string `json:"enum,omitempty"`
	Bits   map[string]string `json:"bits,omitempty"`
	Count  int               `json:"count,omitempty"`
	Stride int               `json:"stride,omitempty"`
	Desc   string            `json:"desc,omitempty"`
	Hidden bool              `json:"hidden,omitempty"`
}

// gameStateFile はファイルの形。
type gameStateFile struct {
	Version int             `json:"version"`
	Items   []GameStateItem `json:"items"`
}

// GameStateTypes は使える型。
var GameStateTypes = []string{"u8", "s8", "u16", "s16", "u24", "u32", "bcd", "digits", "bool", "bits"}

var itemName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ByteSize は 1 要素のバイト数を返す。
func (it GameStateItem) ByteSize() int {
	switch it.Type {
	case "u8", "s8", "bool":
		return 1
	case "u16", "s16":
		return 2
	case "u24":
		return 3
	case "u32":
		return 4
	case "bits":
		if it.Size > 0 {
			return it.Size
		}
		return 1
	}
	return it.Size
}

// stride は要素の間隔を返す。
func (it GameStateItem) stride() int {
	if it.Stride > 0 {
		return it.Stride
	}
	return it.ByteSize()
}

// bigEndian は多バイトの値を上位の桁から並べるかを返す。整数は既定で
// リトルエンディアン（6502 の並び）、BCD と digits は既定で上位の桁が先
// （画面に出す順に置くことが多いため）とする。
func (it GameStateItem) bigEndian() bool {
	switch it.Order {
	case "big":
		return true
	case "little":
		return false
	}
	return it.Type == "bcd" || it.Type == "digits"
}

// Validate は項目を確かめる。
func (it GameStateItem) Validate() error {
	if !itemName.MatchString(it.Name) {
		return fmt.Errorf("name %q は英字・数字・アンダースコアで、英字かアンダースコアで始める", it.Name)
	}
	if it.Loc == "" {
		return errors.New("loc が無い")
	}
	if !slices.Contains(GameStateTypes, it.Type) {
		return fmt.Errorf("type %q を知らない（%v）", it.Type, GameStateTypes)
	}
	switch it.Type {
	case "bcd", "digits":
		if it.Size < 1 || it.Size > 8 {
			return fmt.Errorf("type %s の size は 1–8 とする（%d）", it.Type, it.Size)
		}
	case "bits":
		if it.Size < 0 || it.Size > 4 {
			return fmt.Errorf("type bits の size は 1–4 とする（%d）", it.Size)
		}
	default:
		if it.Size != 0 && it.Size != it.ByteSize() {
			return fmt.Errorf("type %s の size は %d である（%d）", it.Type, it.ByteSize(), it.Size)
		}
	}
	if it.Order != "" && it.Order != "big" && it.Order != "little" {
		return fmt.Errorf("order は big か little とする（%q）", it.Order)
	}
	if it.Count < 0 || it.Count > 256 || it.Stride < 0 {
		return fmt.Errorf("count は 0–256、stride は 0 以上とする")
	}
	for k := range it.Enum {
		if _, err := strconv.ParseInt(k, 0, 64); err != nil {
			return fmt.Errorf("enum の値 %q を数として読めない", k)
		}
	}
	for k := range it.Bits {
		b, err := strconv.Atoi(k)
		if err != nil || b < 0 || b >= it.ByteSize()*8 {
			return fmt.Errorf("bits のビット番号 %q は 0–%d とする", k, it.ByteSize()*8-1)
		}
	}
	return nil
}

// Decode は 1 要素のバイト列を値にする。JSON に出す値と、式で使う値を返す。
func (it GameStateItem) Decode(b []uint8) (any, Value) {
	var raw uint64
	n := len(b)
	for i := range n {
		v := b[i]
		if it.bigEndian() {
			v = b[n-1-i]
		}
		raw |= uint64(v) << (8 * i)
	}
	switch it.Type {
	case "s8":
		v := int64(int8(raw))
		return it.enum(v)
	case "s16":
		v := int64(int16(raw))
		return it.enum(v)
	case "bool":
		return raw != 0, boolValue(raw != 0)
	case "bits":
		var names []string
		for bit := 0; bit < n*8; bit++ {
			if raw&(1<<bit) != 0 {
				if name, ok := it.Bits[strconv.Itoa(bit)]; ok {
					names = append(names, name)
				}
			}
		}
		if names == nil {
			names = []string{}
		}
		return names, num(int64(raw))
	case "bcd":
		var v int64
		for i := range n {
			x := b[i]
			if !it.bigEndian() {
				x = b[n-1-i]
			}
			v = v*100 + int64(x>>4)*10 + int64(x&0x0F)
		}
		return it.enum(v)
	case "digits":
		var v int64
		for i := range n {
			x := b[i]
			if !it.bigEndian() {
				x = b[n-1-i]
			}
			v = v*10 + int64(x%10)
		}
		return it.enum(v)
	}
	return it.enum(int64(raw))
}

// enum は値を列挙の名前に直す。対応が無ければ数のまま返す。
func (it GameStateItem) enum(v int64) (any, Value) {
	if name, ok := it.Enum[strconv.FormatInt(v, 10)]; ok {
		return name, Value{Num: v, Str: name, HasStr: true}
	}
	for k, name := range it.Enum {
		if x, err := strconv.ParseInt(k, 0, 64); err == nil && x == v {
			return name, Value{Num: v, Str: name, HasStr: true}
		}
	}
	return v, num(v)
}

// GameStateDef は Game State Definition。
type GameStateDef struct {
	mu    sync.Mutex
	items []GameStateItem
	// invalid は誤りのある項目の名前と理由。誤りのある項目も items に残し、
	// 保存のときに失わない。
	invalid map[string]string
	// path は読み込んだファイル。無ければ空。
	path string
}

// NewGameStateDef は空の定義を作る。
func NewGameStateDef() *GameStateDef { return &GameStateDef{invalid: map[string]string{}} }

// LoadGameStateFile はファイルから読む。項目ごとに確かめ、誤りのある項目
// だけを無効にする（1 項目の誤りで全体を失わない）。
func LoadGameStateFile(path string) (*GameStateDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f gameStateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("debug: %s: %w", path, err)
	}
	if f.Version > gameStateVersion {
		return nil, fmt.Errorf("debug: %s は新しい形式である（%d）", path, f.Version)
	}
	d := NewGameStateDef()
	d.path = path
	for _, it := range f.Items {
		d.items = append(d.items, it)
		if err := it.Validate(); err != nil {
			d.invalid[it.Name] = err.Error()
		}
	}
	return d, nil
}

// Path は読み込んだか保存したファイルのパスを返す。
func (d *GameStateDef) Path() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.path
}

// Items は項目を定義の順に返す。
func (d *GameStateDef) Items() []GameStateItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]GameStateItem(nil), d.items...)
}

// Invalid は誤りのある項目の理由を返す。
func (d *GameStateDef) Invalid(name string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.invalid[name]
	return r, ok
}

// Item は有効な項目を名前で引く。
func (d *GameStateDef) Item(name string) (GameStateItem, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, bad := d.invalid[name]; bad {
		return GameStateItem{}, false
	}
	for _, it := range d.items {
		if it.Name == name {
			return it, true
		}
	}
	return GameStateItem{}, false
}

// Define は項目を追加するか、同じ名前の項目を置き換える。ファイルには書かない。
func (d *GameStateDef) Define(it GameStateItem) error {
	if err := it.Validate(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.invalid, it.Name)
	for i, x := range d.items {
		if x.Name == it.Name {
			d.items[i] = it
			return nil
		}
	}
	d.items = append(d.items, it)
	return nil
}

// Remove は項目を消す。消したとき true。
func (d *GameStateDef) Remove(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, x := range d.items {
		if x.Name == name {
			d.items = append(d.items[:i], d.items[i+1:]...)
			delete(d.invalid, name)
			return true
		}
	}
	return false
}

// Save はファイルへ書く。一時ファイルへ書いてから名前を変える。
func (d *GameStateDef) Save(path string) error {
	d.mu.Lock()
	f := gameStateFile{Version: gameStateVersion, Items: append([]GameStateItem{}, d.items...)}
	d.mu.Unlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, append(data, '\n')); err != nil {
		return err
	}
	d.mu.Lock()
	d.path = path
	d.mu.Unlock()
	return nil
}
