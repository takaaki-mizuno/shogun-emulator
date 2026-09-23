package emu

import (
	"os"
	"path/filepath"
)

// writeFileAtomic は一時ファイルへ書いてから rename する。
//
// 書き込みの途中で電源が切れても、前回の内容が残る。セーブデータと
// セーブステートとムービーはいずれもこの方法で書く。
func writeFileAtomic(path string, data []uint8) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
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
