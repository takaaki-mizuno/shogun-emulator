package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	defaultROMDir = "testdata/roms"
	hashPath      = "testdata/golden/romhashes.json"
)

func main() {
	var (
		dir          = flag.String("dir", defaultROMDir, "ROM を配置するディレクトリ")
		hashes       = flag.String("hashes", hashPath, "SHA-256 の表のパス")
		force        = flag.Bool("force", false, "既にあるファイルも取り直す")
		verifyOnly   = flag.Bool("verify", false, "取得せずに配置済みのファイルを照合する")
		updateHashes = flag.Bool("update-hashes", false, "取得した内容でハッシュ表を作り直す")
	)
	flag.Parse()

	if err := run(*dir, *hashes, *force, *verifyOnly, *updateHashes); err != nil {
		fmt.Fprintf(os.Stderr, "失敗: %v\n", err)
		os.Exit(1)
	}
}

func run(dir, hashesPath string, force, verifyOnly, updateHashes bool) error {
	table, err := loadHashes(hashesPath)
	if err != nil {
		return err
	}

	if verifyOnly {
		return runVerifyOnly(dir, table)
	}

	var files []string
	var notes []string

	// nes-test-roms
	if done, ok := nesTestROMsDone(dir, table, force); ok {
		fmt.Printf("nes-test-roms: 配置済み（%d ファイル）\n", len(done))
		files = append(files, done...)
	} else {
		fmt.Printf("nes-test-roms: 取得中（コミット %s）\n", nesTestROMsCommit[:12])
		got, err := fetchZip(nesTestROMs, dir)
		if err != nil {
			return fmt.Errorf("nes-test-roms: %w", err)
		}
		fmt.Printf("nes-test-roms: %d ファイルを配置した\n", len(got))
		files = append(files, got...)
	}

	// allpads
	for _, s := range allpads {
		if !force && exists(dir, s.dest) {
			fmt.Printf("%s: 配置済み\n", s.name)
			files = append(files, s.dest)
			continue
		}
		fmt.Printf("%s: 取得中\n", s.name)
		rel, err := fetchFile(s, dir)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		files = append(files, rel)
	}

	// holy-mapperel
	if !force && exists(dir, filepath.ToSlash(filepath.Join(holyMapperel.dest, holyMapperel.file))) {
		fmt.Printf("%s: 配置済み\n", holyMapperel.name)
		got, err := listROMs(filepath.Join(dir, holyMapperel.dest), dir)
		if err != nil {
			return err
		}
		files = append(files, got...)
		files = append(files, filepath.ToSlash(filepath.Join(holyMapperel.dest, holyMapperel.file)))
	} else {
		fmt.Printf("%s: 取得中\n", holyMapperel.name)
		got, note, err := fetchArchive(holyMapperel, dir)
		if err != nil {
			return fmt.Errorf("%s: %w", holyMapperel.name, err)
		}
		files = append(files, got...)
		if note != "" {
			notes = append(notes, note)
		}
	}

	files = dedup(files)
	res, err := verify(dir, files, table, updateHashes)
	if err != nil {
		return err
	}

	if updateHashes {
		if err := saveHashes(hashesPath, table); err != nil {
			return err
		}
		fmt.Printf("ハッシュ表を更新した: %s（%d 項目、うち新規または変更 %d）\n",
			hashesPath, len(table), res.added)
	}

	for _, n := range notes {
		fmt.Printf("注意: %s\n", n)
	}

	return report(res, hashesPath, updateHashes)
}

// runVerifyOnly は配置済みのファイルだけを照合する。
func runVerifyOnly(dir string, table hashTable) error {
	if len(table) == 0 {
		return fmt.Errorf("ハッシュ表が空である。先に -update-hashes で作る")
	}
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var present, absent []string
	for _, k := range keys {
		if exists(dir, k) {
			present = append(present, k)
		} else {
			absent = append(absent, k)
		}
	}
	res, err := verify(dir, present, table, false)
	if err != nil {
		return err
	}
	fmt.Printf("照合: %d 件（未配置 %d 件）\n", res.checked, len(absent))
	if len(res.mismatch) > 0 {
		for _, m := range res.mismatch {
			fmt.Fprintf(os.Stderr, "  不一致: %s\n", m)
		}
		return fmt.Errorf("%d 件が一致しない", len(res.mismatch))
	}
	return nil
}

// report は照合の結果を表示し、問題があればエラーを返す。
func report(res verifyResult, hashesPath string, updated bool) error {
	fmt.Printf("照合: %d 件\n", res.checked)

	if len(res.mismatch) > 0 {
		for _, m := range res.mismatch {
			fmt.Fprintf(os.Stderr, "  不一致: %s\n", m)
		}
		return fmt.Errorf("%d 件が %s と一致しない", len(res.mismatch), hashesPath)
	}
	if len(res.missing) > 0 && !updated {
		for _, m := range res.missing {
			fmt.Fprintf(os.Stderr, "  表に無い: %s\n", m)
		}
		return fmt.Errorf("%d 件が %s に登録されていない。-update-hashes で追加する",
			len(res.missing), hashesPath)
	}
	return nil
}

// nesTestROMsDone は nes-test-roms 由来のファイルが既に配置されているかを調べる。
//
// 判定にハッシュ表を使う。表に登録された項目がすべて配置されているとき、
// 書庫の取得を飛ばす。表が無い最初の実行では常に取得する。
func nesTestROMsDone(dir string, table hashTable, force bool) ([]string, bool) {
	if force || len(table) == 0 {
		return nil, false
	}
	var got []string
	for rel := range table {
		if !nesTestROMs.wants(rel) {
			continue
		}
		if !exists(dir, rel) {
			return nil, false
		}
		got = append(got, rel)
	}
	if len(got) == 0 {
		return nil, false
	}
	sort.Strings(got)
	return got, true
}

// exists は dir/rel が通常のファイルとして存在するかを返す。
func exists(dir, rel string) bool {
	st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && st.Mode().IsRegular()
}

// dedup は重複を取り除いて並べ替える。
func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
