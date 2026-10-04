package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// entry は書庫に入れる 1 つのファイル。
type entry struct {
	// name は書庫の中のパス（区切りは /）。
	name string
	data []byte
	// mode は許可の設定。実行ファイルは 0o755。
	mode fs.FileMode
}

// archiveTime は書庫に書く時刻。SOURCE_DATE_EPOCH があればそれを使い、
// 同じ入力から同じ書庫を作れるようにする。
func archiveTime() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Now().UTC().Truncate(time.Second)
}

// sortEntries は名前の順に並べる。書庫の並びを実行ごとに変えない。
func sortEntries(es []entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].name < es[j].name })
}

// writeZip は .zip を書く。
func writeZip(path string, es []entry) error {
	sortEntries(es)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	mtime := archiveTime()
	for _, e := range es {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(e.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := w.Write(e.data); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeTarGz は .tar.gz を書く。
func writeTarGz(path string, es []entry) error {
	sortEntries(es)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	mtime := archiveTime()
	for _, e := range es {
		h := &tar.Header{Name: e.name, Mode: int64(e.mode.Perm()), Size: int64(len(e.data)),
			ModTime: mtime, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(h); err != nil {
			f.Close()
			return err
		}
		if _, err := tw.Write(e.data); err != nil {
			f.Close()
			return err
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			return err
		}
	}
	return nil
}

// writeTree は es をディレクトリ dir の下へ書く。AppDir と .app を作るのに使う。
func writeTree(dir string, es []entry) error {
	for _, e := range es {
		p := filepath.Join(dir, filepath.FromSlash(e.name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, e.data, e.mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

// docEntries は同梱する文書を prefix の下の書庫の中身にする。
func docEntries(prefix string, docs []docFile) []entry {
	out := make([]entry, 0, len(docs))
	for _, d := range docs {
		out = append(out, entry{name: prefix + d.name, data: d.data, mode: 0o644})
	}
	return out
}
