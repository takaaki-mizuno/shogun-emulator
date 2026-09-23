package testrom

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"crypto/sha256"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// FrameHash はフレームバッファの内容のハッシュを返す。
//
// パレットインデックスとエンファシスをそのまま並べて取る。RGB へ変換した
// 後ではパレットファイルの差し替えでハッシュが変わってしまう。
func FrameHash(f *video.Frame) string {
	h := sha256.New()
	var buf [2]byte
	for _, v := range f.Pixels {
		binary.LittleEndian.PutUint16(buf[:], v)
		h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// frameHashFile はフレームハッシュの期待値を置くファイル。
const frameHashFile = "testdata/golden/framehashes.json"

// LoadFrameHashes は期待値の表を読む。ファイルが無いときは空の表を返す。
func LoadFrameHashes(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// SaveFrameHashes は期待値の表を書く。
//
// キーを並べ替えて書くのは、内容が同じなら差分が出ないようにするため。
func SaveFrameHashes(path string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]byte, 0, 64*len(keys)+4)
	out = append(out, "{\n"...)
	for i, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return err
		}
		out = append(out, "  "...)
		out = append(out, kb...)
		out = append(out, ": \""...)
		out = append(out, m[k]...)
		out = append(out, '"')
		if i != len(keys)-1 {
			out = append(out, ',')
		}
		out = append(out, '\n')
	}
	out = append(out, "}\n"...)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// FrameHashPath は期待値の表のパスを返す。
func FrameHashPath(root string) string {
	return filepath.Join(root, frameHashFile)
}

// WritePNG はフレームを PNG として書き出す。
//
// golden を確定させる前に、画面の内容を目で確かめるために使う。
// 既定のパレットを適用し、オーバースキャンでは何も隠さない。テストの
// 対象は画面の端も含めた 240 行すべてであるため、表示のために隠す範囲を
// ここで適用すると確認できない部分が生じる。
func WritePNG(path string, f *video.Frame) error {
	return video.SavePNG(f, video.DefaultPalette(), video.Overscan{}, video.Height, path)
}
