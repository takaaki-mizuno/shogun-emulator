package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// hashFile はファイルの SHA-256 を 16 進で返す。
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashTable はファイルの相対パスから SHA-256 への対応。
type hashTable map[string]string

// loadHashes はハッシュ表を読む。ファイルが無いときは空の表を返す。
func loadHashes(path string) (hashTable, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return hashTable{}, nil
	}
	if err != nil {
		return nil, err
	}
	var t hashTable
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// saveHashes はハッシュ表を書く。
//
// キーを並べ替えて書くのは、内容が同じなら差分が出ないようにするためである。
func saveHashes(path string, t hashTable) error {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("{\n")
	for i, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return err
		}
		sb.WriteString("  ")
		sb.Write(kb)
		sb.WriteString(": \"")
		sb.WriteString(t[k])
		sb.WriteString("\"")
		if i != len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// verifyResult は照合の結果。
type verifyResult struct {
	checked  int
	added    int
	missing  []string
	mismatch []string
}

// verify は files の SHA-256 を表と照合する。
//
// update が真のとき、表に無い項目を追加し、異なる項目を書き換える。
func verify(dir string, files []string, t hashTable, update bool) (verifyResult, error) {
	var res verifyResult
	for _, rel := range files {
		sum, err := hashFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return res, err
		}
		res.checked++

		want, ok := t[rel]
		switch {
		case !ok && update:
			t[rel] = sum
			res.added++
		case !ok:
			res.missing = append(res.missing, rel)
		case want != sum && update:
			t[rel] = sum
			res.added++
		case want != sum:
			res.mismatch = append(res.mismatch, fmt.Sprintf("%s\n    期待 %s\n    実際 %s", rel, want, sum))
		}
	}
	sort.Strings(res.missing)
	sort.Strings(res.mismatch)
	return res, nil
}
