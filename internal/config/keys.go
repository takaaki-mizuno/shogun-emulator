package config

import "sort"

// Action は論理アクション。
//
// 物理キーを直接扱わず、いったんアクションへ変換する。キーの割り当てを
// 差し替えても、割り当てを受け取る側のコードが変わらないようにするためである。
type Action uint8

// アクションの一覧。プレイヤー入力を先に並べる。
const (
	ActionNone Action = iota

	ActionP1Up
	ActionP1Down
	ActionP1Left
	ActionP1Right
	ActionP1A
	ActionP1B
	ActionP1Start
	ActionP1Select

	ActionP2Up
	ActionP2Down
	ActionP2Left
	ActionP2Right
	ActionP2A
	ActionP2B
	ActionP2Start
	ActionP2Select

	ActionPause
	ActionFrameAdvance
	ActionFastForward
	ActionSlowMotion
	ActionReset
	ActionHardReset
	ActionSaveState
	ActionLoadState
	ActionNextSlot
	ActionPrevSlot
	ActionRewind
	ActionScreenshot
	ActionToggleFullscreen
	ActionMute
)

// firstHotkey はホットキーのアクションの先頭。
const firstHotkey = ActionPause

// ButtonName はプレイヤー入力のアクション名。設定ファイルのキーになる。
type ButtonName = string

// プレイヤー入力のアクション名。
const (
	ButtonUp     ButtonName = "up"
	ButtonDown   ButtonName = "down"
	ButtonLeft   ButtonName = "left"
	ButtonRight  ButtonName = "right"
	ButtonA      ButtonName = "a"
	ButtonB      ButtonName = "b"
	ButtonStart  ButtonName = "start"
	ButtonSelect ButtonName = "select"
)

// playerButtonOrder はプレイヤー入力のアクション名を並べたもの。
//
// スライスで持つのは、設定 GUI の表示順と解決の順序を決めるためである。
// map をたどると順序が実行ごとに変わる。
var playerButtonOrder = []ButtonName{
	ButtonUp, ButtonDown, ButtonLeft, ButtonRight,
	ButtonA, ButtonB, ButtonStart, ButtonSelect,
}

// PlayerButtonOrder はプレイヤー入力のアクション名を並び順で返す。
func PlayerButtonOrder() []ButtonName {
	return append([]ButtonName(nil), playerButtonOrder...)
}

// PlayerAction は player（1 または 2）とボタン名からアクションを返す。
func PlayerAction(player int, name ButtonName) (Action, bool) {
	idx := -1
	for i, n := range playerButtonOrder {
		if n == name {
			idx = i
			break
		}
	}
	if idx < 0 || player < 1 || player > 2 {
		return ActionNone, false
	}
	base := ActionP1Up
	if player == 2 {
		base = ActionP2Up
	}
	return base + Action(idx), true
}

// hotkeyNames はホットキーのアクション名。
//
// 配列の添字を ActionPause からの差とする。名前とアクションの対応を
// 1 か所に保つためである。
var hotkeyNames = []string{
	"pause",
	"frameAdvance",
	"fastForward",
	"slowMotion",
	"reset",
	"hardReset",
	"saveState",
	"loadState",
	"nextSlot",
	"prevSlot",
	"rewind",
	"screenshot",
	"toggleFullscreen",
	"mute",
}

// HotkeyNames はホットキーのアクション名を並び順で返す。
func HotkeyNames() []string {
	return append([]string(nil), hotkeyNames...)
}

// HotkeyAction は名前からホットキーのアクションを返す。
func HotkeyAction(name string) (Action, bool) {
	for i, n := range hotkeyNames {
		if n == name {
			return firstHotkey + Action(i), true
		}
	}
	return ActionNone, false
}

// IsPlayerInput はアクションがプレイヤー入力かを返す。
func (a Action) IsPlayerInput() bool {
	return a >= ActionP1Up && a <= ActionP2Select
}

// Player はプレイヤー入力のアクションのポート番号（1 または 2）を返す。
// プレイヤー入力でないときは 0 を返す。
func (a Action) Player() int {
	switch {
	case a >= ActionP1Up && a <= ActionP1Select:
		return 1
	case a >= ActionP2Up && a <= ActionP2Select:
		return 2
	}
	return 0
}

// ButtonName はプレイヤー入力のアクションのボタン名を返す。
func (a Action) ButtonName() (ButtonName, bool) {
	switch {
	case a >= ActionP1Up && a <= ActionP1Select:
		return playerButtonOrder[a-ActionP1Up], true
	case a >= ActionP2Up && a <= ActionP2Select:
		return playerButtonOrder[a-ActionP2Up], true
	}
	return "", false
}

// String はアクションの名前を返す。
func (a Action) String() string {
	if name, ok := a.ButtonName(); ok {
		if a.Player() == 1 {
			return "p1." + name
		}
		return "p2." + name
	}
	if i := int(a) - int(firstHotkey); i >= 0 && i < len(hotkeyNames) {
		return hotkeyNames[i]
	}
	return "none"
}

// Binding は 1 つの物理キーの割り当て。
type Binding struct {
	Type string `json:"type"`
	Code string `json:"code"`
}

// BindingTypeKey はキーボードの割り当てを表す Binding.Type の値。
const BindingTypeKey = "key"

// Key はキーボードの割り当てを作る。
func Key(code string) Binding { return Binding{Type: BindingTypeKey, Code: code} }

// PlayerBindings は 1 人分の割り当て。
type PlayerBindings struct {
	Player   int                  `json:"player"`
	Device   string               `json:"device"`
	Bindings map[string][]Binding `json:"bindings"`
	Turbo    map[string]int       `json:"turbo"`
}

// Keybindings はキーの割り当て全体。
type Keybindings struct {
	Version int                  `json:"version"`
	Players []PlayerBindings     `json:"players"`
	Hotkeys map[string][]Binding `json:"hotkeys"`
}

// Resolve は物理キーからアクションへの表を作る。
//
// 起動時と設定変更時に 1 回だけ行う。キーイベントのたびに割り当ての
// 構造をたどると、1 秒間に何十回も map を検索することになる。
//
// 同じ物理キーが複数のアクションに割り当てられているとき、すべてを返す。
// 並びはアクションの定義順に揃える。map をたどる順序が実行ごとに変わり、
// 適用の順序が変わることを避けるためである。
func (k *Keybindings) Resolve() map[string][]Action {
	out := map[string][]Action{}
	add := func(b Binding, a Action) {
		if b.Type != BindingTypeKey || b.Code == "" {
			return
		}
		out[b.Code] = append(out[b.Code], a)
	}

	for _, p := range k.Players {
		for _, name := range playerButtonOrder {
			a, ok := PlayerAction(p.Player, name)
			if !ok {
				continue
			}
			for _, b := range p.Bindings[name] {
				add(b, a)
			}
		}
	}
	for _, name := range hotkeyNames {
		a, ok := HotkeyAction(name)
		if !ok {
			continue
		}
		for _, b := range k.Hotkeys[name] {
			add(b, a)
		}
	}

	for code, actions := range out {
		sort.Slice(actions, func(i, j int) bool { return actions[i] < actions[j] })
		out[code] = dedupeActions(actions)
	}
	return out
}

// dedupeActions は並べ替え済みの一覧から重複を除く。
//
// 同じアクションに同じキーを二重に割り当てたとき、押下を 2 回処理しない。
func dedupeActions(a []Action) []Action {
	out := a[:0]
	for i, v := range a {
		if i == 0 || v != a[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// DefaultKeybindings は既定の割り当てを返す。
//
// A を KeyX、B を KeyZ とするのは、NES のコントローラで B が左・A が右に
// 並ぶ物理配置と一致させるためである。
func DefaultKeybindings() *Keybindings {
	return &Keybindings{
		Version: Version,
		Players: []PlayerBindings{
			{
				Player: 1,
				Device: DeviceStandard,
				Bindings: map[string][]Binding{
					ButtonUp:     {Key("ArrowUp")},
					ButtonDown:   {Key("ArrowDown")},
					ButtonLeft:   {Key("ArrowLeft")},
					ButtonRight:  {Key("ArrowRight")},
					ButtonA:      {Key("KeyX")},
					ButtonB:      {Key("KeyZ")},
					ButtonStart:  {Key("Enter")},
					ButtonSelect: {Key("ShiftRight")},
				},
				Turbo: map[string]int{},
			},
			{
				Player: 2,
				Device: DeviceStandard,
				Bindings: map[string][]Binding{
					ButtonUp:     {Key("KeyW")},
					ButtonDown:   {Key("KeyS")},
					ButtonLeft:   {Key("KeyA")},
					ButtonRight:  {Key("KeyD")},
					ButtonA:      {Key("KeyG")},
					ButtonB:      {Key("KeyF")},
					ButtonStart:  {Key("KeyR")},
					ButtonSelect: {Key("KeyT")},
				},
				Turbo: map[string]int{},
			},
		},
		Hotkeys: map[string][]Binding{
			"pause":            {Key("Space")},
			"frameAdvance":     {Key("Period")},
			"fastForward":      {Key("Tab")},
			"slowMotion":       {Key("Backquote")},
			"reset":            {Key("F1")},
			"saveState":        {Key("F5")},
			"loadState":        {Key("F7")},
			"nextSlot":         {Key("F6")},
			"prevSlot":         {Key("F4")},
			"rewind":           {Key("Backspace")},
			"screenshot":       {Key("F12")},
			"toggleFullscreen": {Key("F11")},
		},
	}
}
