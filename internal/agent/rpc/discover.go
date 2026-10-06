package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Discovery は発見ファイルの内容（設計書 14 編 §14.5.1）。
type Discovery struct {
	PID      int    `json:"pid"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Kind     string `json:"kind"`
	ROM      string `json:"rom"`
	Started  string `json:"started"`
}

// WriteDiscovery は発見ファイルを dir/<pid>.json に書き、そのパスを返す。
//
// 一時ファイルに 0600 で書いてから名前を変える。読む側が書きかけの
// ファイルを読まないためである。Windows では 0600 が所有者だけの ACL に
// ならない。Windows の ACL は未確認の項目として計画に残す。
func WriteDiscovery(dir string, d Discovery) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, strconv.Itoa(d.PID)+".json")
	tmp, err := os.CreateTemp(dir, ".discovery-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// ErrNoServer は接続先が見つからないことを表す。
var ErrNoServer = errors.New("rpc: 起動中の Shogun Emulator が見つからない（shogun serve、または GUI で AI からの接続を許可する）")

// FindDiscovery は発見ファイルから接続先を選ぶ（設計書 14 編 §14.5.1）。
//
// pid が 0 でなければそのプロセスのもの。0 なら kind が gui で最も新しい
// もの、無ければ最も新しいものを選ぶ。プロセスが無い発見ファイルは
// 古いものとして消す。
func FindDiscovery(cacheDir string, pid int) (Discovery, error) {
	dir := filepath.Join(cacheDir, DirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Discovery{}, ErrNoServer
		}
		return Discovery{}, err
	}
	var found []Discovery
	for _, ent := range entries {
		name := ent.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var d Discovery
		if json.Unmarshal(data, &d) != nil || d.PID == 0 {
			continue
		}
		if !processAlive(d.PID) {
			_ = os.Remove(path)
			if p, ok := strings.CutPrefix(d.Endpoint, "unix:"); ok {
				_ = os.Remove(p)
			}
			continue
		}
		found = append(found, d)
	}
	if pid != 0 {
		for _, d := range found {
			if d.PID == pid {
				return d, nil
			}
		}
		return Discovery{}, fmt.Errorf("rpc: プロセス %d の接続先が無い", pid)
	}
	if len(found) == 0 {
		return Discovery{}, ErrNoServer
	}
	// 新しい順に並べ、gui を優先する。
	slices.SortStableFunc(found, func(a, b Discovery) int {
		ta, _ := time.Parse(time.RFC3339, a.Started)
		tb, _ := time.Parse(time.RFC3339, b.Started)
		return tb.Compare(ta)
	})
	for _, d := range found {
		if d.Kind == "gui" {
			return d, nil
		}
	}
	return found[0], nil
}
