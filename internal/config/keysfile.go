package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
)

// KeybindingsVersion はキーバインドファイルの形式のバージョン。
const KeybindingsVersion = 1

// MaxTurboRateHz は連射レートの上限。60 fps の半分で、毎フレーム押下と
// 離しを入れ替える速さである。
const MaxTurboRateHz = 30

// LoadKeybindings はキーバインドファイルを読む（設計書 11 編 §11.4）。
//
// knownCode はキーの変換表にあるコードかを返す。変換表は GUI の側にあるため
// 呼び出し側が渡す。nil のときはコードを検査しない。
func LoadKeybindings(path string, knownCode func(code string) bool) (*Keybindings, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultKeybindings(), nil, nil
	}
	if err != nil {
		return DefaultKeybindings(), nil, err
	}
	var k Keybindings
	if err := json.Unmarshal(data, &k); err != nil {
		broken := path + ".broken"
		msg := fmt.Sprintf("キーバインドファイル %s を読めないため既定値を使う（%v）", path, err)
		if rerr := os.Rename(path, broken); rerr == nil {
			msg += fmt.Sprintf("。元のファイルを %s として残した", broken)
		}
		return DefaultKeybindings(), []string{msg}, nil
	}
	warnings := k.sanitize(knownCode)
	return &k, warnings, nil
}

// Save はキーバインドファイルへ書く。一時ファイルへ書いてから rename する。
func (k *Keybindings) Save(path string) error {
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// Clone は深い写しを返す。
func (k *Keybindings) Clone() *Keybindings {
	out := &Keybindings{Version: k.Version, Hotkeys: map[string][]Binding{}}
	for name, list := range k.Hotkeys {
		out.Hotkeys[name] = slices.Clone(list)
	}
	for _, p := range k.Players {
		q := PlayerBindings{Player: p.Player, Device: p.Device,
			Bindings: map[string][]Binding{}, Turbo: map[string]int{}}
		for name, list := range p.Bindings {
			q.Bindings[name] = slices.Clone(list)
		}
		for name, hz := range p.Turbo {
			q.Turbo[name] = hz
		}
		out.Players = append(out.Players, q)
	}
	return out
}

// Player は player（1 または 2）の割り当てを返す。無ければ作る。
func (k *Keybindings) Player(player int) *PlayerBindings {
	for i := range k.Players {
		if k.Players[i].Player == player {
			return &k.Players[i]
		}
	}
	k.Players = append(k.Players, PlayerBindings{Player: player, Device: DeviceStandard,
		Bindings: map[string][]Binding{}, Turbo: map[string]int{}})
	return &k.Players[len(k.Players)-1]
}

// sanitize は知らないアクション名・キーコード・範囲外の連射レートを除き、
// 除いたものを警告として返す。
func (k *Keybindings) sanitize(knownCode func(string) bool) []string {
	var w []string
	cleanList := func(where string, list []Binding) []Binding {
		out := list[:0:0]
		for _, b := range list {
			switch {
			case b.Type != BindingTypeKey:
				w = append(w, fmt.Sprintf("%s の割り当ての種類 %q を扱えないため無視する", where, b.Type))
			case knownCode != nil && !knownCode(b.Code):
				w = append(w, fmt.Sprintf("%s の知らないキーコード %q を無視する", where, b.Code))
			default:
				out = append(out, b)
			}
		}
		return out
	}

	hotkeys := map[string][]Binding{}
	for _, name := range sortedKeys(k.Hotkeys) {
		if _, ok := HotkeyAction(name); !ok {
			w = append(w, fmt.Sprintf("知らないホットキー %q を無視する", name))
			continue
		}
		hotkeys[name] = cleanList(name, k.Hotkeys[name])
	}
	k.Hotkeys = hotkeys

	var players []PlayerBindings
	for _, p := range k.Players {
		if p.Player < 1 || p.Player > 2 {
			w = append(w, fmt.Sprintf("プレイヤー %d の割り当てを無視する", p.Player))
			continue
		}
		bindings := map[string][]Binding{}
		for _, name := range sortedKeys(p.Bindings) {
			if !slices.Contains(playerButtonOrder, name) {
				w = append(w, fmt.Sprintf("プレイヤー %d の知らないボタン %q を無視する", p.Player, name))
				continue
			}
			bindings[name] = cleanList(fmt.Sprintf("p%d.%s", p.Player, name), p.Bindings[name])
		}
		turbo := map[string]int{}
		for _, name := range sortedKeys(p.Turbo) {
			hz := p.Turbo[name]
			switch {
			case !slices.Contains(playerButtonOrder, name):
				w = append(w, fmt.Sprintf("プレイヤー %d の知らないボタン %q の連射を無視する", p.Player, name))
			case hz < 0 || hz > MaxTurboRateHz:
				w = append(w, fmt.Sprintf("p%d.%s の連射レート %d は範囲 0-%d の外にあるため無効にする", p.Player, name, hz, MaxTurboRateHz))
			case hz > 0:
				turbo[name] = hz
			}
		}
		p.Bindings, p.Turbo = bindings, turbo
		if p.Device == "" {
			p.Device = DeviceStandard
		}
		players = append(players, p)
	}
	k.Players = players
	k.Player(1)
	k.Player(2)
	sort.Slice(k.Players, func(i, j int) bool { return k.Players[i].Player < k.Players[j].Player })
	k.Version = KeybindingsVersion
	return w
}

// sortedKeys は map のキーを整列して返す。警告の順序を実行ごとに変えない。
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Duplicates は複数のアクションに割り当てられたキーコードを返す（設定 GUI の強調表示に使う）。
func (k *Keybindings) Duplicates() map[string]bool {
	count := map[string]int{}
	for code, actions := range k.Resolve() {
		count[code] = len(actions)
	}
	out := map[string]bool{}
	for code, n := range count {
		if n > 1 {
			out[code] = true
		}
	}
	return out
}
