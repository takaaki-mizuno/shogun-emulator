package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// httpClient はすべての取得で使うクライアント。
//
// 既定のクライアントを使わないのは、応答が返らないときに無期限に待たない
// ようにするためである。
var httpClient = &http.Client{Timeout: 10 * time.Minute}

// download は url の内容を取得して w へ書く。
func download(url string, w io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// GitHub はユーザエージェントの無い要求を拒むことがある。
	req.Header.Set("User-Agent", "shogun-emulator-fetch-test-roms")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return err
	}
	return nil
}

// downloadToTemp は url の内容を一時ファイルへ落とし、そのパスを返す。
//
// zip の読み出しには io.ReaderAt が必要で、応答の本体をそのまま渡せない。
// 書庫全体をメモリに載せないためにファイルを経由する。
func downloadToTemp(url, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := download(url, f); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// writeFile は dir/rel へ内容を書く。途中のディレクトリを作る。
func writeFile(dir, rel string, r io.Reader) error {
	dst := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// stripTopDir は書庫内のパスから最上位のディレクトリを除く。
// GitHub の zip は「リポジトリ名-コミット」というディレクトリで包まれている。
func stripTopDir(p string) string {
	_, rest, ok := strings.Cut(p, "/")
	if !ok {
		return ""
	}
	return rest
}

// wants は rel を取り出すかを返す。
func (s zipSource) wants(rel string) bool {
	matched := false
	for _, inc := range s.include {
		if strings.HasSuffix(inc, "/") {
			if strings.HasPrefix(rel, inc) {
				matched = true
				break
			}
			continue
		}
		if rel == inc {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	if slices.Contains(s.extra, rel) {
		return true
	}
	if len(s.exts) == 0 {
		return true
	}
	return slices.Contains(s.exts, strings.ToLower(path.Ext(rel)))
}

// fetchZip は zip 書庫を取得し、必要なファイルを dir へ展開する。
// 展開したファイルの相対パスを返す。
func fetchZip(s zipSource, dir string) ([]string, error) {
	tmp, err := downloadToTemp(s.url, "shogun-roms-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)

	zr, err := zip.OpenReader(tmp)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var written []string
	for _, e := range zr.File {
		if e.FileInfo().IsDir() {
			continue
		}
		rel := stripTopDir(e.Name)
		if rel == "" || !s.wants(rel) {
			continue
		}
		// 書庫内のパスが配置先の外を指していないことを確かめる。
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return nil, fmt.Errorf("書庫内の不正なパス: %s", e.Name)
		}
		rc, err := e.Open()
		if err != nil {
			return nil, err
		}
		err = writeFile(dir, rel, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		written = append(written, rel)
	}
	if len(written) == 0 {
		return nil, errors.New("取り出すファイルが 1 つも無かった")
	}
	return written, nil
}

// fetchFile は単一のファイルを取得する。
func fetchFile(s fileSource, dir string) (string, error) {
	tmp, err := downloadToTemp(s.url, "shogun-rom-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)

	f, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := writeFile(dir, s.dest, f); err != nil {
		return "", err
	}
	return s.dest, nil
}

// sevenZipCommands は 7z 形式を展開できるコマンドの候補。
var sevenZipCommands = []string{"7zz", "7z", "7za"}

// find7z は使える 7z のコマンドを返す。
func find7z() (string, bool) {
	for _, name := range sevenZipCommands {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	return "", false
}

// fetchArchive は書庫を取得し、7z のコマンドがあれば展開する。
//
// コマンドが無いときは書庫をそのまま置き、展開の指示を返す。展開できない
// ことを失敗にしないのは、他の ROM の取得を止めないためである。
func fetchArchive(s archiveSource, dir string) (written []string, note string, err error) {
	destDir := filepath.Join(dir, filepath.FromSlash(s.dest))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, "", err
	}
	archiveRel := path.Join(s.dest, s.file)

	tmp, err := downloadToTemp(s.url, "shogun-archive-*.7z")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(tmp)

	f, err := os.Open(tmp)
	if err != nil {
		return nil, "", err
	}
	err = writeFile(dir, archiveRel, f)
	f.Close()
	if err != nil {
		return nil, "", err
	}
	written = append(written, archiveRel)

	cmdPath, ok := find7z()
	if !ok {
		return written, fmt.Sprintf(
			"%s を展開できなかった（%s のいずれも見つからない）。"+
				"7z を導入して次を実行する: 7z x -o%s %s",
			s.file, strings.Join(sevenZipCommands, "・"), destDir, filepath.Join(destDir, s.file)), nil
	}

	cmd := exec.Command(cmdPath, "x", "-y", "-o"+destDir, filepath.Join(destDir, s.file))
	if out, err := cmd.CombinedOutput(); err != nil {
		return written, fmt.Sprintf("%s の展開に失敗した: %v\n%s", s.file, err, out), nil
	}

	// 展開された .nes を数える。ハッシュの照合の対象にする。
	extracted, err := listROMs(destDir, dir)
	if err != nil {
		return written, "", err
	}
	written = append(written, extracted...)
	return written, "", nil
}

// listROMs は root 以下の .nes を base からの相対パスで返す。
func listROMs(root, base string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(p)) != ".nes" {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}
