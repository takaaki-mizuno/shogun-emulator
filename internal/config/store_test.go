package config

import (
	"path/filepath"
	"testing"
)

// TestStoreKeepsOverridesOutOfFile は上書きを使う設定にだけ重ね、保存する設定と
// ファイルへ混ぜないことを確かめる（設計書 11 編 §11.1）。
func TestStoreKeepsOverridesOutOfFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s := NewStore(Paths{}, path, "", Default(), []Override{
		{Source: "--scale", Apply: func(c *Config) { c.Video.Scale = 7 }},
		{Source: "SHOGUN_LOG", Apply: func(c *Config) { c.Debug.LogOutput = LogFile }},
	}, DefaultKeybindings())
	eff := s.Config()
	if eff.Video.Scale != 7 || eff.Debug.LogOutput != LogFile {
		t.Fatalf("使う設定 = %d, %s", eff.Video.Scale, eff.Debug.LogOutput)
	}
	if err := s.Update(func(c *Config) { c.Video.Filter = FilterLinear; c.Video.Scale = 2 }); err != nil {
		t.Fatal(err)
	}
	if s.Config() != eff || eff.Video.Filter != FilterLinear || eff.Video.Scale != 7 {
		t.Errorf("作り直した使う設定 = %+v（ポインタを保つ）", eff.Video)
	}
	saved, _, _ := Load(path)
	if saved.Video.Scale != 2 || saved.Debug.LogOutput != LogStderr || saved.Video.Filter != FilterLinear {
		t.Errorf("ファイル = %+v %+v", saved.Video, saved.Debug.LogOutput)
	}
	if by := s.OverriddenBy(func(c *Config) any { return c.Video.Scale }); len(by) != 1 || by[0] != "--scale" {
		t.Errorf("上書きの出どころ = %v", by)
	}
	if by := s.OverriddenBy(func(c *Config) any { return c.Video.Filter }); len(by) != 0 {
		t.Errorf("上書きしていない項目の出どころ = %v", by)
	}
}
