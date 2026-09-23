package config

import "slices"

// Override は環境変数または引数による 1 つの上書き（設計書 11 編 §11.1）。
type Override struct {
	// Source は上書きの出どころ（環境変数の名前、または --scale などの引数）。
	Source string
	// Apply は設定を上書きする。
	Apply func(c *Config)
}

// Store は保存する設定と使う設定を持つ（設計書 11 編 §11.1）。
//
// 保存する設定は設定ファイルの内容であり、設定画面とメニューの操作で
// 変える。使う設定は保存する設定に上書きを重ねたもので、エミュレータと
// GUI が参照する。上書きを保存する設定へ混ぜないため、2 つを分けて持つ。
//
// UI スレッドだけが使う。
type Store struct {
	// Paths は保存先のディレクトリ。
	Paths Paths
	// File と KeysFile は設定ファイルとキーバインドファイルのパス。
	File, KeysFile string

	base      *Config
	effective *Config
	overrides []Override
	keys      *Keybindings
}

// NewStore は保存する設定 base と上書きから Store を作る。
func NewStore(paths Paths, file, keysFile string, base *Config, overrides []Override, keys *Keybindings) *Store {
	s := &Store{Paths: paths, File: file, KeysFile: keysFile, base: base,
		effective: &Config{}, overrides: overrides, keys: keys}
	s.derive()
	return s
}

// derive は保存する設定に上書きを重ねて使う設定を作り直す。
//
// 使う設定のポインタは変えない。GUI とエミュレータが同じ値を参照し続けられる。
func (s *Store) derive() {
	c := s.base.Clone()
	for _, o := range s.overrides {
		o.Apply(c)
	}
	c.Validate()
	*s.effective = *c
}

// Config は使う設定を返す。値を直接書き換えず、Update を使う。
func (s *Store) Config() *Config { return s.effective }

// Base は保存する設定の写しを返す。設定画面の編集に使う。
func (s *Store) Base() *Config { return s.base.Clone() }

// Overridden は上書きされている項目の出どころを返す。
func (s *Store) Overridden() []string {
	out := make([]string, 0, len(s.overrides))
	for _, o := range s.overrides {
		out = append(out, o.Source)
	}
	return out
}

// OverriddenBy は fn が参照する項目を上書きしている出どころを返す。
//
// 保存する設定に上書きを 1 つずつ当て、pick の値が変わるものを集める。
// 設定画面が「引数で上書きされている」と表示するために使う。
func (s *Store) OverriddenBy(pick func(c *Config) any) []string {
	var out []string
	for _, o := range s.overrides {
		c := s.base.Clone()
		before := pick(c)
		o.Apply(c)
		if !equalValue(before, pick(c)) {
			out = append(out, o.Source)
		}
	}
	return out
}

// equalValue は設定の値を比べる。スライスは要素で比べる。
func equalValue(a, b any) bool {
	if as, ok := a.([]string); ok {
		bs, _ := b.([]string)
		return slices.Equal(as, bs)
	}
	return a == b
}

// Update は保存する設定を fn で変え、使う設定を作り直して保存する。
func (s *Store) Update(fn func(base *Config)) error {
	fn(s.base)
	s.base.Validate()
	s.derive()
	return s.Save()
}

// Replace は保存する設定を c に置き換えて保存する。設定画面の「保存」に使う。
func (s *Store) Replace(c *Config) error {
	c.noSave = s.base.noSave
	c.Validate()
	s.base = c
	s.derive()
	return s.Save()
}

// Save は保存する設定をファイルへ書く。新しいバージョンの設定ファイルを
// 読んだときは書かない。
func (s *Store) Save() error {
	if s.base.noSave || s.File == "" {
		return nil
	}
	return s.base.Save(s.File)
}

// Keys はキーバインドを返す。
func (s *Store) Keys() *Keybindings { return s.keys }

// SetKeys はキーバインドを置き換えて保存する。
func (s *Store) SetKeys(k *Keybindings) error {
	s.keys = k
	if s.KeysFile == "" {
		return nil
	}
	return k.Save(s.KeysFile)
}

// MemoryStore はファイルへ保存しない Store を作る。テストと headless で使う。
func MemoryStore(c *Config) *Store {
	return NewStore(Paths{}, "", "", c, nil, DefaultKeybindings())
}
