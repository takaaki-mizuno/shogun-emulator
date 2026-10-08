package config

import (
	"fmt"
	"strconv"
	"strings"
)

// EnvPrefix は環境変数の接頭辞（設計書 11 編 §11.1）。
const EnvPrefix = "SHOGUN_"

// 保存先を決めるための環境変数。ApplyEnv では扱わず、ResolvePaths の前に読む。
const (
	EnvConfig   = EnvPrefix + "CONFIG"
	EnvPortable = EnvPrefix + "PORTABLE"
)

// envSetting は 1 つの環境変数と設定項目の対応。
type envSetting struct {
	name  string
	apply func(c *Config, v string) error
}

// envSettings は設定項目を上書きする環境変数（設計書 11 編 §11.1 の表）。
var envSettings = []envSetting{
	{"REGION", func(c *Config, v string) error { c.Emulation.Region = v; return nil }},
	{"RAM_INIT", func(c *Config, v string) error { c.Emulation.RAMInitPattern = v; return nil }},
	{"SCALE", func(c *Config, v string) error { return setInt(&c.Video.Scale, v) }},
	{"AUDIO", func(c *Config, v string) error { return setBool(&c.Audio.Enabled, v) }},
	{"AUDIO_BUFFER", func(c *Config, v string) error { return setInt(&c.Audio.BufferMilliseconds, v) }},
	{"ROM_DIR", func(c *Config, v string) error { c.Paths.ROMDir = v; return nil }},
	{"SAVE_DIR", func(c *Config, v string) error { c.Paths.SaveDir = v; return nil }},
	{"STATE_DIR", func(c *Config, v string) error { c.Paths.StateDir = v; return nil }},
	{"SCREENSHOT_DIR", func(c *Config, v string) error { c.Paths.ScreenshotDir = v; return nil }},
	{"LOG_DIR", func(c *Config, v string) error { c.Paths.LogDir = v; return nil }},
	{"MOVIE_DIR", func(c *Config, v string) error { c.Paths.MovieDir = v; return nil }},
	{"VIDEO_DIR", func(c *Config, v string) error { c.Paths.VideoDir = v; return nil }},
	{"LOG", func(c *Config, v string) error { c.Debug.LogOutput = v; return nil }},
	{"AGENT", func(c *Config, v string) error { return setBool(&c.Agent.Enabled, v) }},
	{"AGENT_LISTEN", func(c *Config, v string) error { c.Agent.Listen = v; return nil }},
	{"LOG_CATEGORIES", func(c *Config, v string) error {
		c.Debug.LogCategories = splitList(v)
		return nil
	}},
}

// EnvNames は設定項目を上書きする環境変数の名前を返す。
func EnvNames() []string {
	out := []string{EnvConfig, EnvPortable}
	for _, s := range envSettings {
		out = append(out, EnvPrefix+s.name)
	}
	return out
}

// EnvOverrides は環境変数による上書きを返す。解釈できない値は無視して
// 警告に入れる。範囲の検証は重ねた後に Validate で行う。
func EnvOverrides(getenv func(string) string) (overrides []Override, warnings []string) {
	for _, s := range envSettings {
		name := EnvPrefix + s.name
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			continue
		}
		if err := s.apply(Default(), v); err != nil {
			warnings = append(warnings, fmt.Sprintf("環境変数 %s の値 %q を解釈できないため無視する", name, v))
			continue
		}
		apply := s.apply
		overrides = append(overrides, Override{Source: name, Apply: func(c *Config) { _ = apply(c, v) }})
	}
	return overrides, warnings
}

// ApplyEnv は環境変数で設定を上書きし、上書きした環境変数の名前と警告を返す。
func ApplyEnv(c *Config, getenv func(string) string) (applied, warnings []string) {
	overrides, warnings := EnvOverrides(getenv)
	for _, o := range overrides {
		o.Apply(c)
		applied = append(applied, o.Source)
	}
	return applied, warnings
}

// EnvBool は環境変数を真偽値として読む。空か解釈できないとき false。
func EnvBool(getenv func(string) string, name string) bool {
	var b bool
	_ = setBool(&b, strings.TrimSpace(getenv(name)))
	return b
}

func setInt(dst *int, v string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return err
	}
	*dst = n
	return nil
}

func setBool(dst *bool, v string) error {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		*dst = true
	case "0", "false", "no", "off":
		*dst = false
	default:
		return fmt.Errorf("真偽値ではない")
	}
	return nil
}

// splitList はカンマ区切りの並びを分ける。空の要素は除く。
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SplitList はカンマ区切りの並びを分ける。空の要素は除く。
func SplitList(v string) []string { return splitList(v) }
