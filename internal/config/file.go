package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// FixedSampleRate は出力のサンプリングレート。設定の値によらずこれを使う
// （設計書 11 編 §11.3.1）。
const FixedSampleRate = 48000

// Load は設定ファイルを読む（設計書 11 編 §11.3）。
//
// warnings は利用者へ知らせる注意である。ファイルが壊れているときも
// エラーにせず既定値を返し、理由を warnings に入れる。err を返すのは、
// ファイルを読めない（権限が無いなど）ときと、移行した設定を書けない
// ときである。
func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil, nil
	}
	if err != nil {
		return Default(), nil, err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		broken := path + ".broken"
		msg := fmt.Sprintf("設定ファイル %s を読めないため既定値で起動する（%v）", path, err)
		if rerr := os.Rename(path, broken); rerr == nil {
			msg += fmt.Sprintf("。元のファイルを %s として残した", broken)
		}
		return Default(), []string{msg}, nil
	}

	version := versionOf(raw)
	if version > Version {
		c := Default()
		c.noSave = true
		return c, []string{fmt.Sprintf(
			"設定ファイル %s の version %d はこのバージョン（%d）より新しいため既定値で起動する。ファイルは書き換えない",
			path, version, Version)}, nil
	}

	var warnings []string
	migrated := false
	if version < Version {
		if err := migrate(raw, version); err != nil {
			return Default(), nil, err
		}
		backup := fmt.Sprintf("%s.v%d.bak", path, version)
		if err := writeFileAtomic(backup, data); err != nil {
			return Default(), nil, err
		}
		if data, err = json.Marshal(raw); err != nil {
			return Default(), nil, err
		}
		migrated = true
		warnings = append(warnings, fmt.Sprintf(
			"設定ファイルを version %d から %d へ移行した。元の内容を %s として残した", version, Version, backup))
	}

	for _, key := range unknownKeys(raw, reflect.TypeOf(Config{}), "") {
		warnings = append(warnings, fmt.Sprintf("設定ファイルの知らない項目 %s を無視する", key))
	}

	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		// 型の合わない値（数値の場所に文字列など）。その項目の読み込みは
		// 途中で止まるため、既定値で起動する。
		return Default(), append(warnings, fmt.Sprintf("設定ファイル %s の値を読めないため既定値で起動する（%v）", path, err)), nil
	}
	c.Version = Version
	warnings = append(warnings, c.Validate()...)
	if migrated {
		if err := c.Save(path); err != nil {
			return c, warnings, err
		}
	}
	return c, warnings, nil
}

// versionOf は JSON の version を返す。無いときは 1 とする。
func versionOf(raw map[string]any) int {
	if v, ok := raw["version"].(float64); ok {
		return int(v)
	}
	return 1
}

// errNoSave は保存を止めた設定を保存しようとしたことを表す。
var errNoSave = errors.New("config: 新しいバージョンの設定ファイルを上書きしないため保存しない")

// Save は設定ファイルへ書く。一時ファイルへ書いてから rename する。
func (c *Config) Save(path string) error {
	if c.noSave {
		return errNoSave
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// SaveDisabled は保存を止めているか（新しいバージョンの設定ファイルを
// 読んだか）を返す。
func (c *Config) SaveDisabled() bool { return c.noSave }

// Clone は設定の深い写しを返す。
func (c *Config) Clone() *Config {
	out := *c
	out.Audio.ChannelVolumes = cloneMap(c.Audio.ChannelVolumes)
	out.Debug.LogCategories = slices.Clone(c.Debug.LogCategories)
	out.UI.Windows = cloneMap(c.UI.Windows)
	out.UI.RecentROMs = slices.Clone(c.UI.RecentROMs)
	return &out
}

func cloneMap[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// writeFileAtomic は一時ファイルへ書いてから rename する。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// unknownKeys は raw のうち型 t の JSON タグに無いキーを、ドットで区切った
// パスで返す。map 型のフィールドは任意のキーを受け付ける。
func unknownKeys(raw map[string]any, t reflect.Type, prefix string) []string {
	fields := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		fields[tag] = f.Type
	}
	var out []string
	for key, val := range raw {
		ft, ok := fields[key]
		if !ok {
			out = append(out, prefix+key)
			continue
		}
		if sub, isMap := val.(map[string]any); isMap && ft.Kind() == reflect.Struct {
			out = append(out, unknownKeys(sub, ft, prefix+key+".")...)
		}
	}
	sort.Strings(out)
	return out
}
