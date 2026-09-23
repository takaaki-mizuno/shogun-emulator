package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSaveLoadRoundTrip は保存と読み込みの往復を確かめる。
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Video.Scale = 5
	c.Audio.ChannelVolumes["noise"] = 0.25
	c.Debug.LogCategories = []string{"mapper"}
	c.UI.Windows["main"] = WindowState{Width: 800, Height: 700, Visible: true}
	c.UI.RecentROMs = []RecentROM{{Path: "/a/b.nes", Name: "b"}}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("読み込み: %v %v", err, warnings)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("往復で変わった\n実際 %+v\n期待 %+v", got, c)
	}
}

// TestAllFieldsWritten は全フィールドが JSON に出ることを確かめる（omitempty を付けない）。
func TestAllFieldsWritten(t *testing.T) {
	data, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var walk func(v reflect.Type, m map[string]any, prefix string)
	walk = func(v reflect.Type, m map[string]any, prefix string) {
		for i := range v.NumField() {
			f := v.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			val, ok := m[tag]
			if !ok {
				t.Errorf("%s%s が出力されていない", prefix, tag)
				continue
			}
			if val == nil {
				t.Errorf("%s%s が null になっている", prefix, tag)
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, val.(map[string]any), prefix+tag+".")
			}
		}
	}
	walk(reflect.TypeOf(Config{}), raw, "")
}

// TestLoadMissingFileReturnsDefaults はファイルが無いとき既定値を返すことを確かめる。
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	c, w, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(w) != 0 || !reflect.DeepEqual(c, Default()) {
		t.Errorf("既定値にならない: %v %v", err, w)
	}
}

// TestLoadBrokenJSON は壊れた JSON で既定値を返し、元のファイルを残すことを確かめる。
func TestLoadBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte("{ broken"), 0o644)
	c, w, err := Load(path)
	if err != nil || len(w) != 1 || !reflect.DeepEqual(c, Default()) {
		t.Fatalf("既定値にならない: %v %v", err, w)
	}
	if _, err := os.Stat(path + ".broken"); err != nil {
		t.Error("元のファイルが .broken として残っていない")
	}
}

// TestLoadNewerVersion は新しい version のファイルを既定値で読み、保存しないことを確かめる。
func TestLoadNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	orig := []byte(`{"version": 99, "video": {"scale": 7}}`)
	os.WriteFile(path, orig, 0o644)
	c, w, err := Load(path)
	if err != nil || len(w) != 1 || c.Video.Scale != Default().Video.Scale {
		t.Fatalf("既定値にならない: %v %v", err, w)
	}
	if err := c.Save(path); err == nil {
		t.Error("新しい version のファイルを上書きできてしまう")
	}
	if data, _ := os.ReadFile(path); string(data) != string(orig) {
		t.Error("ファイルが書き換わった")
	}
}

// TestLoadUnknownFieldsAndRanges は知らない項目と範囲外の値を警告して扱うことを確かめる。
func TestLoadUnknownFieldsAndRanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"version": 2, "unknownTop": 1,
		"video": {"scale": 20, "oops": true, "overscanTop": 4},
		"audio": {"sampleRate": 44100, "ringHighWaterMultiplier": 1},
		"debug": {"logCategories": ["mapper", "nope"]}}`), 0o644)
	c, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(w, "\n")
	for _, want := range []string{"unknownTop", "video.oops", "video.scale", "sampleRate", "nope"} {
		if !strings.Contains(text, want) {
			t.Errorf("警告に %q が無い: %s", want, text)
		}
	}
	if c.Video.Scale != 3 || c.Video.OverscanTop != 4 || c.Audio.SampleRate != 48000 ||
		c.Audio.RingHighWaterMultiplier != 2 || len(c.Debug.LogCategories) != 1 {
		t.Errorf("値の扱いが違う: %+v %+v %+v", c.Video, c.Audio, c.Debug)
	}
}

// TestMigrateV1 は version 1 のファイルを移行し、元の内容を残すことを確かめる（設計書 11 編 §11.8）。
func TestMigrateV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	orig := []byte(`{"version": 1, "debug": {"logToFile": true}}`)
	os.WriteFile(path, orig, 0o644)
	c, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Debug.LogOutput != LogFile || len(w) != 1 {
		t.Errorf("移行した値 = %q, 警告 %v", c.Debug.LogOutput, w)
	}
	if data, _ := os.ReadFile(path + ".v1.bak"); string(data) != string(orig) {
		t.Error("移行前の内容が残っていない")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"version": 2`) || strings.Contains(string(data), "logToFile") {
		t.Errorf("移行後の設定が保存されていない: %s", data)
	}
}

// TestMigrationChain は移行が順に当たる仕組みを、ダミーの移行で確かめる。
func TestMigrationChain(t *testing.T) {
	var order []int
	list := []migration{
		{from: 2, to: 3, apply: func(raw map[string]any) error { order = append(order, 2); raw["b"] = 1.0; return nil }},
		{from: 1, to: 2, apply: func(raw map[string]any) error { order = append(order, 1); raw["a"] = 1.0; return nil }},
	}
	raw := map[string]any{}
	if err := migrateWith(list, raw, 1, 3); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int{1, 2}) || raw["version"] != 3.0 || raw["a"] != 1.0 || raw["b"] != 1.0 {
		t.Errorf("順序 %v, raw %v", order, raw)
	}
	if err := migrateWith(list, map[string]any{}, 0, 3); err == nil {
		t.Error("移行の無い version でエラーにならない")
	}
}

// TestEnvOverridesFile は環境変数が設定ファイルより優先されることを確かめる。
func TestEnvOverridesFile(t *testing.T) {
	c := Default()
	c.Video.Scale = 2
	env := map[string]string{"SHOGUN_SCALE": "6", "SHOGUN_LOG_DIR": "/l", "SHOGUN_AUDIO": "0",
		"SHOGUN_LOG_CATEGORIES": "mapper, dma", "SHOGUN_AUDIO_BUFFER": "abc"}
	applied, w := ApplyEnv(c, func(k string) string { return env[k] })
	if c.Video.Scale != 6 || c.Paths.LogDir != "/l" || c.Audio.Enabled ||
		!reflect.DeepEqual(c.Debug.LogCategories, []string{"mapper", "dma"}) {
		t.Errorf("上書きされていない: %+v", c)
	}
	if len(applied) != 4 || len(w) != 1 || !strings.Contains(w[0], "SHOGUN_AUDIO_BUFFER") {
		t.Errorf("上書き %v, 警告 %v", applied, w)
	}
}

// TestCloneIsDeep は写しを変えても元が変わらないことを確かめる。
func TestCloneIsDeep(t *testing.T) {
	c := Default()
	d := c.Clone()
	d.Audio.ChannelVolumes["dmc"] = 0
	d.UI.Windows["x"] = WindowState{}
	d.Debug.LogCategories[0] = "dma"
	if c.Audio.ChannelVolumes["dmc"] != 1 || len(c.UI.Windows) != 0 || c.Debug.LogCategories[0] != "warn.compat" {
		t.Error("写しの変更が元に及んだ")
	}
}
