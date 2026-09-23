package config

import "fmt"

// migration は設定ファイルの version を 1 つ進める移行（設計書 11 編 §11.8）。
//
// JSON を map として読んだ段階で行う。構造体へ読む前に行うのは、古い
// 形式にしか無い項目の値を新しい項目へ移すためである。
type migration struct {
	from, to int
	apply    func(raw map[string]any) error
}

// migrations は version の順に並べた移行。
var migrations = []migration{
	{from: 1, to: 2, apply: migrateLogToFile},
}

// migrate は raw を from から現在の Version まで順に移行する。
func migrate(raw map[string]any, from int) error {
	return migrateWith(migrations, raw, from, Version)
}

// migrateWith は list の移行を from から to まで順に当てる。
func migrateWith(list []migration, raw map[string]any, from, to int) error {
	for v := from; v < to; {
		applied := false
		for _, m := range list {
			if m.from != v {
				continue
			}
			if err := m.apply(raw); err != nil {
				return fmt.Errorf("config: version %d から %d への移行に失敗した: %w", m.from, m.to, err)
			}
			v = m.to
			applied = true
			break
		}
		if !applied {
			return fmt.Errorf("config: version %d からの移行が無い", v)
		}
	}
	raw["version"] = float64(to)
	return nil
}

// migrateLogToFile は debug.logToFile を debug.logOutput に置き換える（1 → 2）。
func migrateLogToFile(raw map[string]any) error {
	dbg, ok := raw["debug"].(map[string]any)
	if !ok {
		return nil
	}
	if v, ok := dbg["logToFile"].(bool); ok {
		if v {
			dbg["logOutput"] = LogFile
		} else {
			dbg["logOutput"] = LogStderr
		}
	}
	delete(dbg, "logToFile")
	return nil
}
