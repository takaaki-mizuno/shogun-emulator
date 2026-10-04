package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestGeneratedIconsMatchSource はリポジトリに置いた各 OS 向けの形式が、
// 1024 px の原本から作ったものと一致することを確かめる（設計書 13 編 §13.4）。
//
// 原本を変えて go run ./tools/gen-icon -derive を実行し忘れると失敗する。
// 縮小は浮動小数点で計算し、arm64 では積和の融合により amd64 と下位の桁が
// 違うことがある。そのためバイト列ではなく、復号した画素の差で比べる。
func TestGeneratedIconsMatchSource(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := readPNG(filepath.Join(root, png1024))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := derive(src, tmp); err != nil {
		t.Fatal(err)
	}
	files := []string{png256, icoPath, icnsPath}
	for _, size := range linuxSizes {
		files = append(files, filepath.Join(linuxDir, fmt.Sprintf("icon-%d.png", size)))
	}
	for _, f := range files {
		want, err := os.ReadFile(filepath.Join(tmp, f))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s が無い。go run ./tools/gen-icon -derive で作る", f)
			continue
		}
		if err := sameImages(got, want); err != nil {
			t.Errorf("%s が原本から作ったものと違う（%v）。go run ./tools/gen-icon -derive で作り直す", f, err)
		}
	}
}

// TestICOAndICNSLayout は .ico と .icns の見出しの並びを確かめる。
func TestICOAndICNSLayout(t *testing.T) {
	images := [][]byte{[]byte("aaaa"), []byte("bbbbbb")}
	ico := buildICO([]int{16, 256}, images)
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 2 {
		t.Fatalf("ICONDIR = % x", ico[:6])
	}
	if ico[6] != 16 || ico[6+16] != 0 {
		t.Errorf("幅の表し方が違う: %d, %d（256 は 0）", ico[6], ico[6+16])
	}
	off := binary.LittleEndian.Uint32(ico[6+16+12:])
	if string(ico[off:off+6]) != "bbbbbb" {
		t.Errorf("2 番目の要素の位置 %d が違う", off)
	}

	icns := buildICNS([]string{"icp4", "ic10"}, images)
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:])) != len(icns) {
		t.Fatalf("見出し = % x（長さ %d）", icns[:8], len(icns))
	}
	if string(icns[8:12]) != "icp4" || binary.BigEndian.Uint32(icns[12:]) != 12 || string(icns[20:24]) != "ic10" {
		t.Errorf("要素の並びが違う: % x", icns[8:24])
	}
}

// maxPixelDiff は同じとみなす画素の成分の差の上限。
const maxPixelDiff = 2

// sameImages は 2 つのファイルに含まれる画像が同じかを確かめる。PNG は
// そのまま、.ico と .icns は中の PNG を取り出して比べる。
func sameImages(a, b []byte) error {
	ea, eb := embeddedPNGs(a), embeddedPNGs(b)
	if len(ea) != len(eb) {
		return fmt.Errorf("要素の数が違う（%d と %d）", len(ea), len(eb))
	}
	for i := range ea {
		ia, err := png.Decode(bytes.NewReader(ea[i]))
		if err != nil {
			return err
		}
		ib, err := png.Decode(bytes.NewReader(eb[i]))
		if err != nil {
			return err
		}
		if ia.Bounds() != ib.Bounds() {
			return fmt.Errorf("要素 %d の大きさが違う", i)
		}
		r := ia.Bounds()
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				ca := color.NRGBAModel.Convert(ia.At(x, y)).(color.NRGBA)
				cb := color.NRGBAModel.Convert(ib.At(x, y)).(color.NRGBA)
				for _, d := range []int{int(ca.R) - int(cb.R), int(ca.G) - int(cb.G), int(ca.B) - int(cb.B), int(ca.A) - int(cb.A)} {
					if d > maxPixelDiff || d < -maxPixelDiff {
						return fmt.Errorf("要素 %d の (%d, %d) が %v と %v", i, x, y, ca, cb)
					}
				}
			}
		}
	}
	return nil
}

// embeddedPNGs はファイルに含まれる PNG を並べる。PNG のファイルはそれ自体を返す。
func embeddedPNGs(data []byte) [][]byte {
	switch {
	case bytes.HasPrefix(data, []byte("icns")):
		var out [][]byte
		for p := 8; p+8 <= len(data); {
			n := int(binary.BigEndian.Uint32(data[p+4:]))
			out = append(out, data[p+8:p+n])
			p += n
		}
		return out
	case len(data) >= 6 && binary.LittleEndian.Uint16(data[2:]) == 1:
		var out [][]byte
		count := int(binary.LittleEndian.Uint16(data[4:]))
		for i := range count {
			e := data[6+16*i:]
			size := binary.LittleEndian.Uint32(e[8:])
			off := binary.LittleEndian.Uint32(e[12:])
			out = append(out, data[off:off+size])
		}
		return out
	}
	return [][]byte{data}
}
