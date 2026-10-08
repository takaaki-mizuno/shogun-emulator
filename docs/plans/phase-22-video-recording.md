# フェーズ 22: 動画の書き出し（録画）実装計画

> **エージェント向け:** このフェーズは superpowers:subagent-driven-development（推奨）または superpowers:executing-plans でタスクごとに進める。各手順はチェックボックス（`- [ ]`）で進み具合を記録する。

- 作成日: 2026-10-07
- 前提フェーズ: 21
- 完了条件: 遊びながらの録画、操作の記録からの書き出し、`--record-video` の 3 経路で MP4 を書き出せ、録画しても状態のハッシュが変わらず、書き出した MP4 を macOS の QuickTime Player で再生できる

**目的:** エミュレーションの画と音を、手元のプレイヤーで再生できる MP4（Motion JPEG + PCM）に書き出す。あわせて「ムービー」メニューを「記録」メニューに改める。

**構成:** 新しいパッケージ `internal/video/mp4rec` が MP4 を書く（`mux.go` が入れ物、`writer.go` が画の変換と圧縮と音声の量の調整）。`internal/emu` は APU の出力を録画専用のリサンプラへ分けて渡し、完成したフレームごとに `mp4rec.Writer` へ渡す。UI と CLI は `emu` の API を呼ぶ。

**技術:** Go 1.25、標準ライブラリ `image/jpeg`、`github.com/Eyevinn/mp4ff` v0.59.0（MIT）、Fyne。

**設計書:** `docs/specifications/08-savestate-and-movie.md` §8.8（主）、10 編 §10.1・§10.5、11 編 §11.2・§11.3.1・§11.5、01 編 §1.4、13 編 §13.9

## 全体の制約

- `go.mod` の `go` 指令は `1.25.0` のまま。mp4ff は v0.59.0（`go 1.23`）に固定する
- `internal/video/mp4rec` は `internal/video` と mp4ff だけを参照する（01 編 §1.4）。`internal/video` 本体は mp4ff を参照しない
- MP4 の形式は 08 編 §8.8.1 の表のとおり（`jpeg` の映像、`sowt` の音声、48,000 Hz ステレオ 16 ビット、映像の時間単位 1,000,000、JPEG 品質 90、`moov` は末尾）
- 録画は Machine State を変えない（状態のハッシュが一致する）
- 利用者に見せる文言はすべて `internal/ui/i18n` の表に置く（`internal/arch` の `TestUITextLivesInCatalog` が検査する）
- このプロジェクトでは git を操作しない。コミットは人が行う
- 各タスクの終わりに `go vet ./...` と、変えたパッケージの `go test` を通す

## 確認の重点（どのタスクのテストにも現れにくく、使う人が当たりやすいもの）

1. **録画中に ROM を閉じる・開き直す・アプリを終了する** → 書きかけで終わらず、再生できるファイルとして閉じる（タスク 4 のテスト `TestVideoClosesOnROMChange`）
2. **巻き戻しやステートの読み込みの直後** → 音声が途切れても映像とずれない（タスク 2 のテスト `TestWriterPadsMissingAudio`）
3. **書き込みに失敗したとき（保存先が書けない・ディスクが満杯）** → 録画が止まり、エラーが利用者に伝わり、エミュレーションは止まらない（タスク 4 のテスト `TestVideoStopsOnWriteError`）
4. **操作の記録と違う ROM を開いたまま書き出す** → 書き出さずにエラーになり、空のファイルが残らない（タスク 5 のテスト `TestExportVideoRejectsOtherROM`）
5. **書き出しを途中で取り消す** → 書きかけのファイルが消える（タスク 5 のテスト `TestExportVideoCancelRemovesFile`）

---

### タスク 1: MP4 の入れ物を書く（`mp4rec` の muxer と読み取り）

**ファイル:**
- 作る: `internal/video/mp4rec/doc.go`
- 作る: `internal/video/mp4rec/mux.go`
- 作る: `internal/video/mp4rec/inspect.go`
- テスト: `internal/video/mp4rec/mux_test.go`
- 変える: `go.mod`・`go.sum`（mp4ff を追加）

**受け渡し:**
- 使う: なし
- 作る:
  - `func newMuxer(path string, width, height int) (*muxer, error)`
  - `func (m *muxer) addFrame(jpeg []byte, dur uint32, pcm []byte) error` — `pcm` は 16 ビット・ステレオ・リトルエンディアンの PCM（4 バイトで 1 サンプル）
  - `func (m *muxer) close() error`
  - `func (m *muxer) abort()` — ファイルを閉じて消す
  - `type Info struct { Width, Height int; VideoFrames int; AudioSamples int; Duration time.Duration; FrameSizes []int }`
  - `func Inspect(path string) (Info, error)` — テストと利用者向けの確認に使う

- [x] **手順 1: mp4ff を加える**

```sh
go get github.com/Eyevinn/mp4ff@v0.59.0
grep '^go ' go.mod   # go 1.25.0 のままであること
```

- [x] **手順 2: 失敗するテストを書く**

`internal/video/mp4rec/mux_test.go`:

```go
package mp4rec

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMuxerRoundTrip は書いた MP4 を読み直し、フレーム数・大きさ・長さ・
// 音声のサンプル数と、各サンプルのデータの位置を確かめる（設計書 08 編 §8.8.1）。
func TestMuxerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	m, err := newMuxer(path, 512, 480)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{[]byte("jpeg-0"), []byte("jpeg-one"), []byte("j2")}
	durs := []uint32{16639, 16639, 16640}
	for i, f := range frames {
		pcm := bytes.Repeat([]byte{byte(i), 0, byte(i), 0}, 800)
		if err := m.addFrame(f, durs[i], pcm); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.close(); err != nil {
		t.Fatal(err)
	}

	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 512 || info.Height != 480 {
		t.Errorf("大きさ = %dx%d", info.Width, info.Height)
	}
	if info.VideoFrames != 3 || info.AudioSamples != 2400 {
		t.Errorf("フレーム %d・音声 %d、期待 3・2400", info.VideoFrames, info.AudioSamples)
	}
	if want := 49918 * time.Microsecond; info.Duration != want {
		t.Errorf("長さ = %v、期待 %v", info.Duration, want)
	}
	for i, f := range frames {
		if info.FrameSizes[i] != len(f) {
			t.Errorf("フレーム %d の大きさ = %d、期待 %d", i, info.FrameSizes[i], len(f))
		}
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("jpeg-one")) {
		t.Error("映像のデータが書かれていない")
	}
}

// TestMuxerAbortRemovesFile は abort でファイルが消えることを確かめる。
func TestMuxerAbortRemovesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	m, err := newMuxer(path, 256, 240)
	if err != nil {
		t.Fatal(err)
	}
	m.abort()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("ファイルが残っている: %v", err)
	}
}
```

- [x] **手順 3: 失敗を確かめる**

Run: `go test ./internal/video/mp4rec/`
Expected: FAIL（`newMuxer`・`Inspect` が無い）

- [x] **手順 4: 実装する**

`internal/video/mp4rec/doc.go`:

```go
// Package mp4rec はエミュレーションの画と音を MP4 に書き出す
// （設計書 08 編 §8.8）。
//
// 映像は Motion JPEG、音声は無圧縮の PCM とする。Go だけで書け、
// macOS の QuickTime Player で再生できる。internal/video と mp4ff だけを
// 参照する（設計書 01 編 §1.4）。
package mp4rec
```

`internal/video/mp4rec/mux.go`:

```go
package mp4rec

import (
	"encoding/binary"
	"errors"
	"os"

	"github.com/Eyevinn/mp4ff/mp4"
)

// MP4 の時間単位と音声の形式（設計書 08 編 §8.8.1）。
const (
	videoTimescale = 1_000_000
	movieTimescale = 1000
	// SampleRate は音声のサンプリングレート。
	SampleRate = 48000
	// pcmFrameBytes は PCM の 1 サンプル（16 ビット × 2 チャンネル）の大きさ。
	pcmFrameBytes = 4
	// mdatHeaderSize は 64 ビットの大きさを持つ mdat の見出しの大きさ。
	mdatHeaderSize = 16
)

// trackLog はトラックのチャンクとサンプルの記録。データ本体は持たない。
type trackLog struct {
	chunkOffsets []uint64
	chunkSamples []uint32
	sizes        []uint32 // 映像のサンプルの大きさ。音声は固定長のため使わない
	durations    []uint32 // 映像のサンプルの長さ
}

// muxer は MP4 の入れ物を書く。サンプルのデータを mdat へ追記し、大きさと
// 位置だけを持って、close で末尾に moov を書く（設計書 08 編 §8.8.3）。
// 映像のデータをメモリにため込まないため、録画の長さでメモリが増えない。
type muxer struct {
	f             *os.File
	path          string
	mdatPos       uint64
	off           uint64
	width, height int
	video, audio  trackLog
}

// newMuxer はファイルを作り、ftyp と mdat の見出しを書く。
func newMuxer(path string, width, height int) (*muxer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	m := &muxer{f: f, path: path, width: width, height: height}
	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "iso2", "mp41"})
	if err := ftyp.Encode(f); err != nil {
		m.abort()
		return nil, err
	}
	m.mdatPos = ftyp.Size()
	// 大きさ 1 は「64 ビットの大きさが続く」ことを表す。大きさは close で書く。
	hdr := []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := f.Write(hdr); err != nil {
		m.abort()
		return nil, err
	}
	m.off = m.mdatPos + mdatHeaderSize
	return m, nil
}

// addFrame は 1 フレームの映像と、それに対応する音声を 1 チャンクずつ書く。
func (m *muxer) addFrame(jpeg []byte, dur uint32, pcm []byte) error {
	if len(pcm)%pcmFrameBytes != 0 {
		return errors.New("mp4rec: PCM の長さが 4 の倍数でない")
	}
	if err := m.write(jpeg); err != nil {
		return err
	}
	m.video.chunkOffsets = append(m.video.chunkOffsets, m.off-uint64(len(jpeg)))
	m.video.chunkSamples = append(m.video.chunkSamples, 1)
	m.video.sizes = append(m.video.sizes, uint32(len(jpeg)))
	m.video.durations = append(m.video.durations, dur)
	if len(pcm) == 0 {
		return nil
	}
	if err := m.write(pcm); err != nil {
		return err
	}
	m.audio.chunkOffsets = append(m.audio.chunkOffsets, m.off-uint64(len(pcm)))
	m.audio.chunkSamples = append(m.audio.chunkSamples, uint32(len(pcm)/pcmFrameBytes))
	return nil
}

func (m *muxer) write(b []byte) error {
	if _, err := m.f.Write(b); err != nil {
		return err
	}
	m.off += uint64(len(b))
	return nil
}

// close は mdat の大きさを書き直し、末尾に moov を書いて閉じる。
func (m *muxer) close() error {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], m.off-m.mdatPos)
	if _, err := m.f.WriteAt(size[:], int64(m.mdatPos)+8); err != nil {
		m.f.Close()
		return err
	}
	if err := m.moov().Encode(m.f); err != nil {
		m.f.Close()
		return err
	}
	return m.f.Close()
}

// abort はファイルを閉じて消す。書きかけのファイルは moov が無く再生できない。
func (m *muxer) abort() {
	m.f.Close()
	os.Remove(m.path)
}

// moov はサンプルの記録から moov を組み立てる。
func (m *muxer) moov() *mp4.MoovBox {
	var videoDur uint64
	for _, d := range m.video.durations {
		videoDur += uint64(d)
	}
	movieDur := videoDur * movieTimescale / videoTimescale

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = movieTimescale
	mvhd.Duration = movieDur
	mvhd.NextTrackID = 3
	moov.AddChild(mvhd)

	vt := mp4.CreateEmptyTrak(1, videoTimescale, "video", "und")
	vt.Tkhd.Width = mp4.Fixed32(uint32(m.width) << 16)
	vt.Tkhd.Height = mp4.Fixed32(uint32(m.height) << 16)
	vt.Tkhd.Duration = movieDur
	vt.Mdia.Mdhd.Duration = videoDur
	vse := mp4.CreateVisualSampleEntryBox("jpeg", uint16(m.width), uint16(m.height), nil)
	vse.CompressorName = "Photo - JPEG"
	vt.Mdia.Minf.Stbl.Stsd.AddChild(vse)
	stbl := vt.Mdia.Minf.Stbl
	runLengths(m.video.durations, stbl.Stts)
	stbl.Stsz.SampleNumber = uint32(len(m.video.sizes))
	stbl.Stsz.SampleSize = m.video.sizes
	fillChunks(stbl, m.video)
	moov.AddChild(vt)

	var audioSamples uint64
	for _, n := range m.audio.chunkSamples {
		audioSamples += uint64(n)
	}
	at := mp4.CreateEmptyTrak(2, SampleRate, "audio", "und")
	at.Tkhd.Duration = audioSamples * movieTimescale / SampleRate
	at.Mdia.Mdhd.Duration = audioSamples
	at.Mdia.Minf.Stbl.Stsd.AddChild(mp4.CreateAudioSampleEntryBox("sowt", 2, 16, SampleRate, nil))
	astbl := at.Mdia.Minf.Stbl
	if audioSamples > 0 {
		astbl.Stts.SampleCount = []uint32{uint32(audioSamples)}
		astbl.Stts.SampleTimeDelta = []uint32{1}
	}
	astbl.Stsz.SampleUniformSize = pcmFrameBytes
	astbl.Stsz.SampleNumber = uint32(audioSamples)
	fillChunks(astbl, m.audio)
	moov.AddChild(at)
	return moov
}

// runLengths は長さの並びを stts の (数, 長さ) の組にまとめる。
func runLengths(durs []uint32, stts *mp4.SttsBox) {
	for _, d := range durs {
		n := len(stts.SampleTimeDelta)
		if n > 0 && stts.SampleTimeDelta[n-1] == d {
			stts.SampleCount[n-1]++
			continue
		}
		stts.SampleCount = append(stts.SampleCount, 1)
		stts.SampleTimeDelta = append(stts.SampleTimeDelta, d)
	}
}

// fillChunks は stsc とチャンクの位置を書く。位置は 64 ビット（co64）で表し、
// 4 GiB を超えるファイルにも対応する。
func fillChunks(stbl *mp4.StblBox, log trackLog) {
	prev := uint32(0)
	for i, n := range log.chunkSamples {
		if n != prev {
			_ = stbl.Stsc.AddEntry(uint32(i+1), n, 1)
			prev = n
		}
	}
	co64 := &mp4.Co64Box{ChunkOffset: log.chunkOffsets}
	for i, c := range stbl.Children {
		if _, ok := c.(*mp4.StcoBox); ok {
			stbl.Children[i] = co64
		}
	}
	stbl.Stco = nil
	stbl.Co64 = co64
}
```

`internal/video/mp4rec/inspect.go`:

```go
package mp4rec

import (
	"errors"
	"os"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Info は書き出した MP4 の要約。
type Info struct {
	Width, Height int
	VideoFrames   int
	AudioSamples  int
	Duration      time.Duration
	FrameSizes    []int
}

// Inspect は MP4 を読み、映像と音声の要約を返す。サンプルのデータは読まない。
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	file, err := mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return Info{}, err
	}
	if file.Moov == nil {
		return Info{}, errors.New("mp4rec: moov が無い")
	}
	var info Info
	for _, trak := range file.Moov.Traks {
		stbl := trak.Mdia.Minf.Stbl
		switch trak.Mdia.Hdlr.HandlerType {
		case "vide":
			info.Width = int(trak.Tkhd.Width >> 16)
			info.Height = int(trak.Tkhd.Height >> 16)
			info.VideoFrames = int(stbl.Stsz.SampleNumber)
			for _, s := range stbl.Stsz.SampleSize {
				info.FrameSizes = append(info.FrameSizes, int(s))
			}
			info.Duration = time.Duration(trak.Mdia.Mdhd.Duration) * time.Second / time.Duration(trak.Mdia.Mdhd.Timescale)
		case "soun":
			info.AudioSamples = int(stbl.Stsz.SampleNumber)
		}
	}
	return info, nil
}
```

- [x] **手順 5: 通ることを確かめる**

Run: `go test ./internal/video/mp4rec/ && go vet ./internal/video/mp4rec/`
Expected: PASS

- [x] **手順 6: QuickTime で開けることを手で確かめる**

手順 2 のテストは JPEG でないデータを入れるため再生できない。再生の確認はタスク 2 の手順 6 で行う。ここでは `ffprobe -v error -show_streams <テストで作ったファイル>` で、2 本のストリーム（`mjpeg`・`pcm_s16le`）が見えることだけを確かめる（`t.TempDir()` を一時的に固定のパスへ変えて確かめ、元に戻す）。

---

### タスク 2: 画の変換と圧縮・音声の量の調整（`mp4rec.Writer`）

**ファイル:**
- 作る: `internal/video/mp4rec/writer.go`
- テスト: `internal/video/mp4rec/writer_test.go`

**受け渡し:**
- 使う: タスク 1 の `newMuxer`・`addFrame`・`close`・`abort`・`Inspect`
- 作る:
  - `type Options struct { Scale int; FrameRate float64; Overscan video.Overscan; PictureHeight int }`
  - `func Create(path string, pal *video.Palette, opts Options) (*Writer, error)`
  - `func (w *Writer) WriteFrame(f *video.Frame, pcm []int16) error` — `pcm` は 48,000 Hz のモノラル
  - `func (w *Writer) Close() error`
  - `func (w *Writer) Abort()`
  - `func (w *Writer) Frames() uint64`
  - `const MinScale = 1`、`const MaxScale = 3`

- [x] **手順 1: 失敗するテストを書く**

`internal/video/mp4rec/writer_test.go`:

```go
package mp4rec

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

const ntscRate = 60.0988

func testFrame(n int) *video.Frame {
	f := video.NewFrame()
	for y := range video.Height {
		for x := range video.Width {
			f.Set(x, y, uint16((x/16+y/16+n)%64))
		}
	}
	return f
}

func writeFrames(t *testing.T, path string, n int, pcmPerFrame int) {
	t.Helper()
	w, err := Create(path, video.DefaultPalette(), Options{Scale: 2, FrameRate: ntscRate,
		Overscan: video.DefaultOverscan(), PictureHeight: 240})
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := w.WriteFrame(testFrame(i), make([]int16, pcmPerFrame)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWriterSizeAndCounts は大きさ（オーバースキャンを除いて 2 倍）・フレーム数・
// 音声の量を確かめる（設計書 08 編 §8.8.1・§8.8.2）。
func TestWriterSizeAndCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	writeFrames(t, path, 120, 800)
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 512 || info.Height != 448 {
		t.Errorf("大きさ = %dx%d、期待 512x448", info.Width, info.Height)
	}
	if info.VideoFrames != 120 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
	if want := int(math.Round(120 * SampleRate / ntscRate)); info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
}

// TestWriterPadsMissingAudio は音声が届かないフレームを無音で埋め、映像と
// 音声の長さがそろうことを確かめる（巻き戻しやステートの読み込みの直後）。
func TestWriterPadsMissingAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	writeFrames(t, path, 60, 0)
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := int(math.Round(60 * SampleRate / ntscRate)); info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
}

// TestWriterIsDeterministic は同じ入力から同じバイト列ができることを確かめる。
func TestWriterIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")
	writeFrames(t, a, 30, 801)
	writeFrames(t, b, 30, 801)
	da, _ := os.ReadFile(a)
	db, _ := os.ReadFile(b)
	if !bytes.Equal(da, db) {
		t.Error("同じ入力から違うファイルができた")
	}
}

// TestWriterRejectsBadScale は 1–3 以外の倍率を断ることを確かめる。
func TestWriterRejectsBadScale(t *testing.T) {
	for _, s := range []int{0, 4} {
		_, err := Create(filepath.Join(t.TempDir(), "a.mp4"), video.DefaultPalette(),
			Options{Scale: s, FrameRate: ntscRate, PictureHeight: 240})
		if err == nil {
			t.Errorf("倍率 %d を受け付けた", s)
		}
	}
}
```

- [x] **手順 2: 失敗を確かめる**

Run: `go test ./internal/video/mp4rec/ -run Writer`
Expected: FAIL（`Create` が無い）

- [x] **手順 3: 実装する**

`internal/video/mp4rec/writer.go`:

```go
package mp4rec

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"math"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// 倍率の範囲と圧縮の設定（設計書 08 編 §8.8.1）。
const (
	MinScale    = 1
	MaxScale    = 3
	jpegQuality = 90
	// queueFrames は書き出し用のゴルーチンへの受け渡しの列の長さ。満ちたときは
	// WriteFrame が待つ。フレームを捨てると動画が途切れるためである。
	queueFrames = 8
	// maxCarry は次のフレームへ回す音声の上限（0.1 秒）。リサンプラの出力が
	// 続けて多いときに、遅れがたまり続けないようにする。
	maxCarry = SampleRate / 10
)

// Options は動画の書き出しの設定。
type Options struct {
	// Scale は拡大率（MinScale–MaxScale）。最近傍で拡大する。
	Scale int
	// FrameRate はリージョンのフレームレート。
	FrameRate float64
	// Overscan は画面の設定と同じだけ削る量。
	Overscan video.Overscan
	// PictureHeight は表示する画の高さ。
	PictureHeight int
}

// job は書き出し用のゴルーチンへ渡す 1 フレーム分の材料。
type job struct {
	frame video.Frame
	dur   uint32
	pcm   []byte
}

// Writer は動画の書き出し。WriteFrame はエミュレーションゴルーチンから、
// 圧縮と書き込みは専用のゴルーチンで行う（設計書 08 編 §8.8.3）。
type Writer struct {
	pal   *video.Palette
	opts  Options
	mux   *muxer
	queue chan job
	done  chan struct{}

	mu  sync.Mutex
	err error

	frames    uint64
	audioSent uint64
	carry     []int16
}

// Create はファイルを作り、書き出しを始める。
func Create(path string, pal *video.Palette, opts Options) (*Writer, error) {
	if opts.Scale < MinScale || opts.Scale > MaxScale {
		return nil, fmt.Errorf("mp4rec: 倍率 %d は %d–%d の範囲にない", opts.Scale, MinScale, MaxScale)
	}
	if opts.FrameRate <= 0 || opts.PictureHeight <= 0 {
		return nil, fmt.Errorf("mp4rec: フレームレート %v・画の高さ %d が不正", opts.FrameRate, opts.PictureHeight)
	}
	rect := opts.Overscan.Rect(opts.PictureHeight)
	mux, err := newMuxer(path, rect.Dx()*opts.Scale, rect.Dy()*opts.Scale)
	if err != nil {
		return nil, err
	}
	w := &Writer{pal: pal, opts: opts, mux: mux, queue: make(chan job, queueFrames), done: make(chan struct{})}
	go w.run()
	return w, nil
}

// Frames は受け取ったフレーム数を返す。
func (w *Writer) Frames() uint64 { return w.frames }

// WriteFrame は 1 フレームの画と、そのフレームの間に作られた音声を受け取る。
// 書き出しが失敗していればそのエラーを返す。
func (w *Writer) WriteFrame(f *video.Frame, pcm []int16) error {
	if err := w.failed(); err != nil {
		return err
	}
	i := w.frames
	t0 := math.Round(float64(i) * videoTimescale / w.opts.FrameRate)
	t1 := math.Round(float64(i+1) * videoTimescale / w.opts.FrameRate)
	target := uint64(math.Round(float64(i+1) * SampleRate / w.opts.FrameRate))
	need := int(target - w.audioSent)

	// 足りない分は無音で埋め、多い分は次のフレームへ回す（設計書 08 編 §8.8.2）。
	w.carry = append(w.carry, pcm...)
	take := min(need, len(w.carry))
	out := make([]byte, need*pcmFrameBytes)
	for k, v := range w.carry[:take] {
		u := uint16(v)
		out[k*4], out[k*4+1], out[k*4+2], out[k*4+3] = byte(u), byte(u>>8), byte(u), byte(u>>8)
	}
	w.carry = append(w.carry[:0], w.carry[take:]...)
	if len(w.carry) > maxCarry {
		w.carry = w.carry[len(w.carry)-maxCarry:]
	}
	w.audioSent = target

	j := job{dur: uint32(t1 - t0), pcm: out}
	j.frame.CopyFrom(f)
	w.queue <- j
	w.frames++
	return nil
}

// run は受け取ったフレームを圧縮して書く。失敗した後は捨てる。
func (w *Writer) run() {
	defer close(w.done)
	var buf bytes.Buffer
	for j := range w.queue {
		if w.failed() != nil {
			continue
		}
		img := video.ScaleNearest(video.Image(&j.frame, w.pal, w.opts.Overscan, w.opts.PictureHeight), w.opts.Scale)
		buf.Reset()
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			w.fail(err)
			continue
		}
		if err := w.mux.addFrame(buf.Bytes(), j.dur, j.pcm); err != nil {
			w.fail(err)
		}
	}
}

func (w *Writer) failed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *Writer) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

// Close は残りを書き終え、moov を書いて閉じる。書き出しが失敗していたときは
// ファイルを消してそのエラーを返す。
func (w *Writer) Close() error {
	close(w.queue)
	<-w.done
	if err := w.failed(); err != nil {
		w.mux.abort()
		return err
	}
	return w.mux.close()
}

// Abort は書き出しを止め、ファイルを消す。
func (w *Writer) Abort() {
	close(w.queue)
	<-w.done
	w.mux.abort()
}
```

注意: `muxer.addFrame` は JPEG のバイト列を `f.Write` で書くだけで保持しないため、`buf` を使い回してよい。

- [x] **手順 4: 通ることを確かめる**

Run: `go test -race ./internal/video/mp4rec/ && go vet ./internal/video/mp4rec/`
Expected: PASS

- [x] **手順 5: 依存規則の検査を足す**

`internal/arch/arch_test.go` の `dependencyRules` に、mp4ff を `internal/video/mp4rec` だけに閉じる規則を加える。既存の検査に外部モジュールを扱う仕組みが無ければ、次の関数を同じファイルに足す。

```go
// TestMP4FFOnlyInMP4Rec は mp4ff を参照するのが internal/video/mp4rec だけで
// あることを検証する（設計書 01 編 §1.4）。
func TestMP4FFOnlyInMP4Rec(t *testing.T) {
	m := loadModule(t)
	for _, p := range m.matching("...") {
		if strings.HasSuffix(p.ImportPath, "internal/video/mp4rec") {
			continue
		}
		for _, imp := range p.imports() {
			if strings.HasPrefix(imp, "github.com/Eyevinn/mp4ff") && !strings.HasSuffix(p.ImportPath, "_test") {
				t.Errorf("%s が %s を参照している（internal/video/mp4rec に閉じる）", p.ImportPath, imp)
			}
		}
	}
}
```

`loadModule`・`matching`・`imports` の正確な名前は同じファイルの `TestAssetsHoldDataOnly` に合わせる。テストのファイル（`_test.go`）からの参照は対象外とする（タスク 4 以降のテストは `mp4rec.Inspect` を使い、mp4ff を直接参照しない）。

Run: `go test ./internal/arch/`
Expected: PASS

- [x] **手順 6: QuickTime で再生できることを手で確かめる**

`TestWriterSizeAndCounts` の出力先を一時的に `/tmp/shogun-check.mp4` にして実行し、`open /tmp/shogun-check.mp4` で QuickTime Player が再生できること（縞模様が動き、2 秒で終わる）を確かめてから元に戻す。

---

### タスク 3: 設定と保存先（`video.recordScale`・`paths.videoDir`）

**ファイル:**
- 変える: `internal/config/config.go`（`VideoConfig.RecordScale`、`PathsConfig.VideoDir`、既定値）
- 変える: `internal/config/validate.go`（範囲）
- 変える: `internal/config/env.go`（`SHOGUN_VIDEO_DIR`）
- 変える: `internal/config/paths.go`（`Paths.Videos`、`VideoDir`）
- テスト: `internal/config/paths_test.go`・`internal/config/config_test.go`（既存のテストに足す）

**受け渡し:**
- 作る: `cfg.Video.RecordScale int`（既定 2）、`cfg.Paths.VideoDir string`、`func (p Paths) VideoDir(override string) string`、`const MinRecordScale = 1`、`const MaxRecordScale = 3`

- [x] **手順 1: 失敗するテストを書く**

`internal/config/paths_test.go` の `TestStandardPathsPerOS` の各期待値に `Videos` を足す。

```go
// darwin
Videos: j("/Users/u", "Movies", "ShogunEmulator"),
// windows
Videos: j(`C:\Users\u`, "Videos", "ShogunEmulator"),
// linux（2 件とも）
Videos: j("/home/u", "Videos", "ShogunEmulator"),
```

`TestPortableAndOverrides` の `want` に `Videos: filepath.Join("/opt/shogun", "videos")` を足し、次を加える。

```go
	if d := got.VideoDir(""); d != filepath.Join("/opt/shogun", "videos") {
		t.Errorf("動画の保存先 = %s", d)
	}
	if d := got.VideoDir("/v"); d != "/v" {
		t.Errorf("上書きした動画の保存先 = %s", d)
	}
```

設定の既定値と範囲のテスト（`config_test.go` の既定値・範囲の検査に合わせる）に次を足す。

```go
func TestRecordScaleDefaultAndRange(t *testing.T) {
	c := Default()
	if c.Video.RecordScale != 2 {
		t.Errorf("既定の録画の倍率 = %d", c.Video.RecordScale)
	}
	c.Video.RecordScale = 9
	Validate(c) // 既存の関数名に合わせる
	if c.Video.RecordScale != 2 {
		t.Errorf("範囲外の倍率が既定に戻らない: %d", c.Video.RecordScale)
	}
}
```

既存のテストで `Default`・`Validate` の名前と戻り値が違うときは、それに合わせる（`internal/config/validate.go` の `intRange("video.scale", ...)` を呼んでいる関数が範囲の検査である）。

- [x] **手順 2: 失敗を確かめる**

Run: `go test ./internal/config/`
Expected: FAIL

- [x] **手順 3: 実装する**

`config.go`:

```go
	// RecordScale は録画の拡大率（設計書 08 編 §8.8.1）。
	RecordScale int `json:"recordScale"`
```

を `VideoConfig` の末尾に、`VideoDir string \`json:"videoDir"\`` を `PathsConfig` の末尾に足す。既定値の `Video` に `RecordScale: 2,` を足す。定数を `MinScale`・`MaxScale` の隣に足す。

```go
	MinRecordScale = 1
	MaxRecordScale = 3
```

`validate.go` の `intRange("video.scale", ...)` の次に:

```go
	intRange("video.recordScale", &v.RecordScale, d.Video.RecordScale, MinRecordScale, MaxRecordScale)
```

`env.go` の `SHOGUN_MOVIE_DIR` の次に:

```go
	{"VIDEO_DIR", func(c *Config, v string) error { c.Paths.VideoDir = v; return nil }},
```

（既存の `MOVIE_DIR` の項目の書き方に合わせる。）

`paths.go`:
- `Paths` に `Videos string // 録画した動画` を足す
- `portablePaths` に `Videos: filepath.Join(dir, "videos"),`
- `standardPaths` の Screenshots を決める箇所を次に変える

```go
	if homeErr == nil {
		p.Screenshots = filepath.Join(home, "Pictures", AppDirName)
		movies := "Videos"
		if pf.goos == "darwin" {
			movies = "Movies"
		}
		p.Videos = filepath.Join(home, movies, AppDirName)
	} else {
		p.Screenshots = filepath.Join(p.Data, "screenshots")
		p.Videos = filepath.Join(p.Data, "videos")
	}
```

- `ScreenshotDir` の次に:

```go
// VideoDir は録画した動画の保存先を返す。
func (p Paths) VideoDir(override string) string {
	if override != "" {
		return override
	}
	return p.Videos
}
```

- [x] **手順 4: 通ることを確かめる**

Run: `go test ./internal/config/ && go vet ./internal/config/`
Expected: PASS

---

### タスク 4: 録画（`internal/emu`）

**ファイル:**
- 作る: `internal/emu/videorec.go`
- 変える: `internal/emu/run.go`（`apuOutput`・`publishFrame`・`cmdLoadMachine`・`cmdUnload` の処理）
- 変える: `internal/emu/command.go`（コマンドの型）
- 変える: `internal/emu/emulator.go`（`Status.Video`・`Stop`）
- テスト: `internal/emu/videorec_test.go`

**受け渡し:**
- 使う: タスク 2 の `mp4rec.Create`・`WriteFrame`・`Close`・`Abort`・`Options`、タスク 1 の `mp4rec.Inspect`（テスト）
- 作る:
  - `func (e *Emulator) StartRecordingVideo(path string, pal *video.Palette, scale int, overscan video.Overscan) error`
  - `func (e *Emulator) StopRecordingVideo() error`
  - `type VideoStatus struct { Recording bool; Frames uint64; FrameRate float64 }`、`Status.Video VideoStatus`
  - `func frameRate(r *region.Region) float64`
  - 内部: `e.video *videoRecorder`、`func (e *Emulator) closeVideo(abort bool) error`

- [x] **手順 1: 失敗するテストを書く**

`internal/emu/videorec_test.go`:

```go
package emu

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// runWithInputs は決まった入力で n フレーム進める。
func runWithInputs(t *testing.T, e *Emulator, n int) {
	t.Helper()
	for i, b := range []uint8{0, input.ButtonA, input.ButtonRight, 0} {
		e.Input.Set(0, b)
		runFrames(t, e, n/4+i%2)
	}
}

// TestVideoRecordingKeepsStateAndCounts は録画しても状態のハッシュが変わらず、
// 進めたフレーム数と音声の量が動画に入ることを確かめる（設計書 08 編 §8.8.2）。
func TestVideoRecordingKeepsStateAndCounts(t *testing.T) {
	plain := newPausedEmulator(t)
	runWithInputs(t, plain, 120)

	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	before := framesOf(t, e)
	runWithInputs(t, e, 120)
	recorded := framesOf(t, e) - before
	if !e.Status().Video.Recording {
		t.Error("録画中の表示になっていない")
	}
	if err := e.StopRecordingVideo(); err != nil {
		t.Fatal(err)
	}
	if hashOf(t, e) != hashOf(t, plain) {
		t.Error("録画すると状態のハッシュが変わる")
	}
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.VideoFrames) != recorded {
		t.Errorf("動画のフレーム数 = %d、進めたフレーム数 %d", info.VideoFrames, recorded)
	}
	want := int(math.Round(float64(recorded) * mp4rec.SampleRate / e.Status().Video.FrameRate))
	if info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
	if info.Width != 256 || info.Height != 240 {
		t.Errorf("大きさ = %dx%d", info.Width, info.Height)
	}
}

// TestVideoClosesOnROMChange は録画中に ROM を閉じても、再生できる
// ファイルとして閉じることを確かめる（設計書 08 編 §8.8.4）。
func TestVideoClosesOnROMChange(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 10)
	if err := e.Unload(); err != nil {
		t.Fatal(err)
	}
	if e.Status().Video.Recording {
		t.Error("ROM を閉じても録画中のまま")
	}
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatalf("閉じたファイルを読めない: %v", err)
	}
	if info.VideoFrames != 10 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
}

// TestVideoStopsOnWriteError は書き出せない場所を選んだときにエラーを返し、
// エミュレーションが続くことを確かめる。
func TestVideoStopsOnWriteError(t *testing.T) {
	e := newPausedEmulator(t)
	bad := filepath.Join(t.TempDir(), "no-such-dir", "a.mp4")
	if err := e.StartRecordingVideo(bad, video.DefaultPalette(), 1, video.Overscan{}); err == nil {
		t.Fatal("書けない場所で録画を始められた")
	}
	if e.Status().Video.Recording {
		t.Error("失敗したのに録画中になっている")
	}
	runFrames(t, e, 5) // 止まっていない
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Error("ファイルが作られている")
	}
}

// TestVideoRejectsDoubleStart は録画中に重ねて始められないことを確かめる。
func TestVideoRejectsDoubleStart(t *testing.T) {
	e := newPausedEmulator(t)
	dir := t.TempDir()
	if err := e.StartRecordingVideo(filepath.Join(dir, "a.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	if err := e.StartRecordingVideo(filepath.Join(dir, "b.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err == nil {
		t.Error("録画中に 2 本目を始められた")
	}
	_ = e.StopRecordingVideo()
}
```

`e.Unload` の名前は `internal/emu/emulator.go:597` の `cmdUnload` を送る公開メソッドに合わせる。

- [x] **手順 2: 失敗を確かめる**

Run: `go test ./internal/emu/ -run Video`
Expected: FAIL（`StartRecordingVideo` が無い）

- [x] **手順 3: 実装する**

`internal/emu/videorec.go`:

```go
package emu

import (
	"errors"

	"github.com/takaakimizuno/shogun-emulator/internal/audio"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// VideoStatus は録画の状態。
type VideoStatus struct {
	// Recording は録画中かを表す。
	Recording bool
	// Frames は記録したフレーム数。
	Frames uint64
	// FrameRate は記録のフレームレート。経過時間の表示に使う。
	FrameRate float64
}

var errAlreadyRecordingVideo = errors.New("emu: すでに録画している")

// videoRecorder は録画の状態（設計書 08 編 §8.8.2）。エミュレーションゴルーチン
// だけが触る。
type videoRecorder struct {
	w         *mp4rec.Writer
	resampler *audio.Resampler
	pcm       []int16
	frameRate float64
}

// WriteSample は APU の出力を録画専用のリサンプラへ渡す（apu.Output）。
func (r *videoRecorder) WriteSample(v float32) { r.resampler.WriteSample(v) }

// pcmSink はリサンプラの出力を受け取る（audio.Sink）。
type pcmSink struct{ r *videoRecorder }

func (s pcmSink) WriteSample(v int16) bool {
	s.r.pcm = append(s.r.pcm, v)
	return true
}

// apuTee は APU の出力を 2 つへ分ける。
type apuTee struct{ a, b apu.Output }

func (t apuTee) WriteSample(v float32) {
	t.a.WriteSample(v)
	t.b.WriteSample(v)
}

// frameRate はリージョンの平均のフレームレートを返す。NTSC は描画中の
// 奇数フレームで 1 ドット短くなるため、2 種類の平均とする。
func frameRate(r *region.Region) float64 {
	return (r.FrameRateHz(false) + r.FrameRateHz(true)) / 2
}

type cmdStartVideo struct {
	path     string
	pal      *video.Palette
	scale    int
	overscan video.Overscan
	done     chan error
}

type cmdStopVideo struct {
	abort bool
	done  chan error
}

func (cmdStartVideo) isCommand() {}
func (cmdStopVideo) isCommand()  {}

// StartRecordingVideo は次のフレームから録画を始める（設計書 08 編 §8.8.4）。
func (e *Emulator) StartRecordingVideo(path string, pal *video.Palette, scale int, overscan video.Overscan) error {
	return e.apply(cmdStartVideo{path: path, pal: pal, scale: scale, overscan: overscan, done: make(chan error, 1)})
}

// StopRecordingVideo は録画を止めてファイルを閉じる。録画していないときは何もしない。
func (e *Emulator) StopRecordingVideo() error {
	return e.apply(cmdStopVideo{done: make(chan error, 1)})
}

// startVideo はエミュレーションゴルーチンで録画を始める。
func (e *Emulator) startVideo(c cmdStartVideo) error {
	if e.machine == nil {
		return errNoROM
	}
	if e.video != nil {
		return errAlreadyRecordingVideo
	}
	r := e.machine.Region
	rate := frameRate(r)
	w, err := mp4rec.Create(c.path, c.pal, mp4rec.Options{
		Scale: c.scale, FrameRate: rate, Overscan: c.overscan, PictureHeight: r.PictureHeight,
	})
	if err != nil {
		return err
	}
	rec := &videoRecorder{w: w, frameRate: rate}
	// 速度倍率を 1 に固定し、消音を適用しないリサンプラ（設計書 08 編 §8.8.2）。
	rec.resampler = audio.NewResampler(r.CPUClockHz(), mp4rec.SampleRate, e.cfg.Audio.FilterProfile, pcmSink{rec})
	e.video = rec
	e.machine.APU.SetOutput(e.apuOutput())
	e.setVideoStatus()
	return nil
}

// recordFrame は完成したフレームを録画へ渡す。失敗したときは録画を止めて知らせる。
func (e *Emulator) recordFrame(f *video.Frame) {
	pcm := e.video.pcm
	e.video.pcm = e.video.pcm[:0]
	if err := e.video.w.WriteFrame(f, pcm); err != nil {
		e.closeVideo(true)
		e.notifyError(err)
		return
	}
	e.setVideoStatus()
}

// closeVideo は録画を終える。abort のときはファイルを消す。
func (e *Emulator) closeVideo(abort bool) error {
	if e.video == nil {
		return nil
	}
	w := e.video.w
	e.video = nil
	if e.machine != nil {
		e.machine.APU.SetOutput(e.apuOutput())
	}
	e.setVideoStatus()
	if abort {
		w.Abort()
		return nil
	}
	return w.Close()
}

// setVideoStatus は録画の状態を Status へ写す。
func (e *Emulator) setVideoStatus() {
	var st VideoStatus
	if e.video != nil {
		st = VideoStatus{Recording: true, Frames: e.video.w.Frames(), FrameRate: e.video.frameRate}
	}
	e.statusMu.Lock()
	e.status.Video = st
	e.statusMu.Unlock()
}
```

`internal/emu/run.go`:

`apuOutput` を次に置き換える。

```go
func (e *Emulator) apuOutput() apu.Output {
	var out apu.Output
	if e.audio != nil {
		out = e.audio
	}
	if e.video == nil {
		return out
	}
	if out == nil {
		return e.video
	}
	return apuTee{out, e.video}
}
```

`publishFrame` の `e.Frames.Put(f)` の後に:

```go
	if e.video != nil {
		e.recordFrame(f)
	}
```

`handle` の `switch` に:

```go
	case cmdStartVideo:
		v.done <- e.startVideo(v)
	case cmdStopVideo:
		v.done <- e.closeVideo(v.abort)
```

`case cmdLoadMachine:` と `case cmdUnload:` の先頭（`e.cancelPending()` の前）に `e.notifyError(e.closeVideo(false))` を足す。ROM を替える前に閉じないと、リージョンの違う画が同じ動画に混ざる。

`internal/emu/command.go` の、コマンドから `done` を取り出す `switch`（`case cmdPlayMovie: return v.done` の並び）に `cmdStartVideo`・`cmdStopVideo` を足す。

`internal/emu/emulator.go`:
- `Emulator` に `video *videoRecorder // 録画（設計書 08 編 §8.8）。nil のとき録画していない` を足す
- `Status` に `Video VideoStatus // 録画の状態` を足す
- `Stop` の中で、エミュレーションゴルーチンを止める前に録画を閉じる。`Stop` がゴルーチンの終了を待つ箇所の直前に次を足す（ゴルーチンが動いている間にコマンドで閉じる）。

```go
		// 録画を閉じてから止める。閉じずに終わると moov が書かれず再生できない。
		_ = e.StopRecordingVideo()
```

`Stop` の現在の並び（`close(e.stop)` が先頭）だとコマンドが届かないため、`e.stopOnce.Do` の中の最初の行として足す。`apply` が停止後に呼ばれても待ち続けないこと（`errStopped` を返すこと）を `internal/emu/command.go` の `apply` で確かめる。

- [x] **手順 4: 通ることを確かめる**

Run: `go test -race ./internal/emu/ -run 'Video|Movie' -count=3 && go vet ./internal/emu/`
Expected: PASS

- [x] **手順 5: 既存のテストと決定論の検査が変わらないことを確かめる**

Run: `go test -short ./internal/emu/ ./internal/testrom/ -run 'Determinism|Rewind|State'`
Expected: PASS（録画していないとき `apuOutput` は以前と同じ値を返す）

---

### タスク 5: 一時停止したままの再生開始と、操作の記録からの書き出し

**ファイル:**
- 変える: `internal/emu/movieapi.go`（`PlayMovieFilePaused`）
- 作る: `internal/emu/videoexport.go`
- 変える: `internal/emu/movie_test.go`（補助関数を新しい API に置き換える）
- テスト: `internal/emu/videoexport_test.go`

**受け渡し:**
- 使う: タスク 4 の `StartRecordingVideo`・`closeVideo`
- 作る:
  - `func (e *Emulator) PlayMovieFilePaused(path string) error` — 再生を始め、エミュレーションゴルーチンの中でそのまま一時停止する
  - `type VideoExport struct { ROMPath, MoviePath, OutPath string; Palette *video.Palette; Scale int; Overscan video.Overscan; Progress func(done, total uint64) }`
  - `func (e *Emulator) ExportVideo(ctx context.Context, x VideoExport) error`

- [x] **手順 1: 失敗するテストを書く**

`internal/emu/videoexport_test.go`:

```go
package emu

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// recordedMovie は ROM と、その ROM で記録したムービーのパスを返す。
func recordedMovie(t *testing.T) (e *Emulator, rom, moviePath string, total uint64) {
	t.Helper()
	rom = writePollingROM(t)
	e = New(stateTestConfig(t))
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	moviePath = filepath.Join(t.TempDir(), "a.movie")
	recordSession(t, e, moviePath)
	data, err := os.ReadFile(moviePath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return e, rom, moviePath, m.Header.TotalFrames
}

// TestExportVideoFromMovie は操作の記録から書いた動画のフレーム数が記録の
// 総フレーム数と一致し、表示中のエミュレータが変わらないことを確かめる
// （設計書 08 編 §8.8.4・§8.8.5）。
func TestExportVideoFromMovie(t *testing.T) {
	e, rom, moviePath, total := recordedMovie(t)
	before := hashOf(t, e)
	out := filepath.Join(t.TempDir(), "a.mp4")
	var last uint64
	err := e.ExportVideo(context.Background(), VideoExport{
		ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
		Progress: func(done, all uint64) { last = done },
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := mp4rec.Inspect(out)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.VideoFrames) != total {
		t.Errorf("フレーム数 = %d、記録 %d", info.VideoFrames, total)
	}
	if last != total {
		t.Errorf("最後の進み具合 = %d、期待 %d", last, total)
	}
	if hashOf(t, e) != before {
		t.Error("書き出しで表示中のエミュレータの状態が変わった")
	}
}

// TestExportVideoRejectsOtherROM は記録と違う ROM では書き出さず、ファイルを
// 残さないことを確かめる。
func TestExportVideoRejectsOtherROM(t *testing.T) {
	e, _, moviePath, _ := recordedMovie(t)
	other := writeLoopROMForExport(t)
	out := filepath.Join(t.TempDir(), "a.mp4")
	err := e.ExportVideo(context.Background(), VideoExport{
		ROMPath: other, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
	})
	if err == nil {
		t.Fatal("違う ROM で書き出せた")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("ファイルが残っている")
	}
}

// TestExportVideoCancelRemovesFile は取り消すと書きかけのファイルを消す
// ことを確かめる。
func TestExportVideoCancelRemovesFile(t *testing.T) {
	e, rom, moviePath, _ := recordedMovie(t)
	out := filepath.Join(t.TempDir(), "a.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	err := e.ExportVideo(ctx, VideoExport{
		ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
		Progress: func(done, all uint64) { cancel() },
	})
	if err == nil {
		t.Fatal("取り消してもエラーにならない")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("書きかけのファイルが残っている")
	}
}
```

`writeLoopROMForExport` は `writePollingROM` と同じ作りで、PRG の 1 バイト（例: 末尾の未使用領域）だけを変えた ROM を書く補助関数として `videoexport_test.go` に置く。`writePollingROM`（`internal/emu/state_test.go:16`）を写し、`prg[0x100] = 0xEA` を足したものにする。

- [x] **手順 2: 失敗を確かめる**

Run: `go test ./internal/emu/ -run Export`
Expected: FAIL

- [x] **手順 3: 実装する**

`internal/emu/movieapi.go` に:

```go
// PlayMovieFilePaused はムービーの再生を始め、そのまま一時停止する。
//
// PlayMovieFile は再生を始めると一時停止を解く。その後に SetPaused(true) を
// 送ると、届くまでに進むフレーム数が実行のたびに変わる。headless の実行と
// 動画の書き出しは、再生の開始の位置から数えて正確に進める必要がある。
func (e *Emulator) PlayMovieFilePaused(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := movie.Decode(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	var playErr error
	if !e.WithMachine(func(*nes.NES) {
		if playErr = e.startPlayback(m); playErr == nil {
			e.paused = true
			e.setPaused(true)
		}
	}) {
		return errStopped
	}
	return playErr
}
```

（Repro のディレクトリを受け付ける処理は `PlayMovieFile` の先頭と同じものを共通の関数 `readMovieFile(path) (*movie.Movie, error)` に切り出して両方から呼ぶ。）

`internal/emu/movie_test.go` の `playFilePaused` を `e.PlayMovieFilePaused(path)` を呼ぶだけに直す。

`internal/emu/videoexport.go`:

```go
package emu

import (
	"context"
	"os"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// exportChunk は書き出しで 1 回に進めるフレーム数。進み具合の更新と取り消しの
// 確認の単位になる。
const exportChunk = 60

// VideoExport は操作の記録から動画を書き出す指定（設計書 08 編 §8.8.4）。
type VideoExport struct {
	ROMPath, MoviePath, OutPath string
	Palette                     *video.Palette
	Scale                       int
	Overscan                    video.Overscan
	// Progress は進み具合を知らせる。nil のとき知らせない。呼び出し側の
	// ゴルーチンから呼ぶ。
	Progress func(done, total uint64)
}

// ExportVideo は表示中のエミュレータとは別の Emulator で操作の記録を再生し、
// 動画を書き出す。音声デバイスは開かず、速度の上限なしで進める。
// 取り消したとき・失敗したときは書きかけのファイルを消す。
func (e *Emulator) ExportVideo(ctx context.Context, x VideoExport) error {
	tmp, err := os.MkdirTemp("", "shogun-export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	cfg := e.cfg
	cfg.Audio.Enabled = false
	cfg.NewPacer = func(*region.Region) Pacer { return NewNoPacer() }
	cfg.StartPaused = true
	cfg.Notify, cfg.OnBreak, cfg.LoadSymbols = nil, nil, nil
	// 利用者のセーブデータと名前の保存先を読み書きしない（Agent Interface の
	// headless の Instance と同じ扱い）。
	cfg.Paths.SaveDir = tmp
	cfg.SymbolsDir = tmp
	child := New(cfg)
	child.Start()
	defer child.Stop()

	if err := child.LoadROM(x.ROMPath); err != nil {
		return err
	}
	if err := child.PlayMovieFilePaused(x.MoviePath); err != nil {
		return err
	}
	total := child.Status().Movie.Total
	if err := child.StartRecordingVideo(x.OutPath, x.Palette, x.Scale, x.Overscan); err != nil {
		return err
	}
	abort := func(err error) error {
		child.apply(cmdStopVideo{abort: true, done: make(chan error, 1)})
		return err
	}
	for done := uint64(0); done < total; {
		if err := ctx.Err(); err != nil {
			return abort(err)
		}
		n := min(exportChunk, total-done)
		child.StepFrames(int(n))
		child.WithMachine(func(*nes.NES) {})
		done += n
		if err := child.DesyncError(); err != nil {
			return abort(err)
		}
		if x.Progress != nil {
			x.Progress(done, total)
		}
	}
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	return child.StopRecordingVideo()
}
```

`cfg.Paths.SaveDir`・`cfg.SymbolsDir` の名前が `emu.Config` と違うときは `internal/agent/host.go:385-389` に合わせる。違う ROM はムービーのヘッダの照合（`p.VerifyHeader`）で `PlayMovieFilePaused` がエラーを返す。返さない場合（警告だけの場合）は、`ExportVideo` で `child.Status()` とムービーの ROM のハッシュを比べてエラーにする処理を足す。

- [x] **手順 4: 通ることを確かめる**

Run: `go test -race ./internal/emu/ -run 'Export|Movie' -count=3 && go vet ./internal/emu/`
Expected: PASS

---

### タスク 6: CLI の `--record-video`

**ファイル:**
- 変える: `cmd/shogun/flags.go`（オプション・ヘルプの分類）
- 変える: `cmd/shogun/headless.go`（`PlayMovieFilePaused`、録画）
- 変える: `cmd/shogun/main.go`（GUI 版の起動時の録画）
- テスト: `cmd/shogun/headless_test.go`

**受け渡し:**
- 使う: タスク 4・5 の `StartRecordingVideo`・`StopRecordingVideo`・`PlayMovieFilePaused`、タスク 3 の `cfg.Video.RecordScale`
- 作る: `options.recordVideo string`、`func recordPalette(cfg *config.Config) *video.Palette`

- [x] **手順 1: 失敗するテストを書く**

`cmd/shogun/headless_test.go` に:

```go
// TestHeadlessRecordsVideoFromMovie は --movie と --record-video でムービーの
// 終わりまで録画することを確かめる（設計書 11 編 §11.5.2）。
func TestHeadlessRecordsVideoFromMovie(t *testing.T) {
	rom := writeLoopROM(t)
	dir := t.TempDir()
	moviePath := filepath.Join(dir, "test.movie")
	if code, _, stderr := runCLI(t, "--headless", "--deterministic", "--frames", "40",
		"--state-dir", dir, "--record-movie", moviePath, rom); code != exitOK {
		t.Fatalf("記録の終了コード = %d（%s）", code, stderr)
	}
	data, _ := os.ReadFile(moviePath)
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.mp4")
	code, _, stderr := runCLI(t, "--headless", "--deterministic", "--state-dir", dir,
		"--movie", moviePath, "--record-video", out, rom)
	if code != exitOK {
		t.Fatalf("録画の終了コード = %d（%s）", code, stderr)
	}
	info, err := mp4rec.Inspect(out)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.VideoFrames) != m.Header.TotalFrames {
		t.Errorf("フレーム数 = %d、ムービー %d", info.VideoFrames, m.Header.TotalFrames)
	}
	if info.Width != 512 {
		t.Errorf("幅 = %d、既定の 2 倍（512）でない", info.Width)
	}
}
```

- [x] **手順 2: 失敗を確かめる**

Run: `go test ./cmd/shogun/ -run RecordsVideo`
Expected: FAIL（`--record-video` を知らない）

- [x] **手順 3: 実装する**

`flags.go`:
- `options` の「ステートとムービー」に `recordVideo string` を足す
- 分類の表の `"ステートとムービー"` の並びの `"record-movie"` の後に `"record-video"` を足す
- `fs.StringVar(&opts.recordVideo, "record-video", "", "動画（MP4）を録画する")` を `record-movie` の次に足す

`headless.go`:
- `e.PlayMovieFile(opts.moviePath)` を `e.PlayMovieFilePaused(opts.moviePath)` に変える
- `opts.recordMovie` の開始の次に:

```go
	if opts.recordVideo != "" {
		if err := e.StartRecordingVideo(opts.recordVideo, recordPalette(cfg), cfg.Video.RecordScale, videoOverscan(cfg)); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitBadArgs
		}
	}
```

- `opts.recordMovie` の停止の次に:

```go
	if opts.recordVideo != "" {
		if err := e.StopRecordingVideo(); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
```

`cmd/shogun/run.go`（または `main.go` の補助関数の並び）に:

```go
// recordPalette は録画に使うパレットを返す。設定のパレットを読めないときは
// 既定のパレットを使う（画面の表示と同じ扱い）。
func recordPalette(cfg *config.Config) *video.Palette {
	if cfg.Video.PaletteFile != "" {
		if p, err := video.LoadPaletteFile(cfg.Video.PaletteFile); err == nil {
			return p
		}
	}
	return video.DefaultPalette()
}

// videoOverscan は設定のオーバースキャンを返す。
func videoOverscan(cfg *config.Config) video.Overscan {
	v := cfg.Video
	return video.Overscan{Top: v.OverscanTop, Bottom: v.OverscanBottom, Left: v.OverscanLeft, Right: v.OverscanRight}
}
```

`main.go` の `startGUI` で、ROM を読み込んだ後・`u.Run()` の前に:

```go
	if opts.recordVideo != "" {
		if err := e.StartRecordingVideo(opts.recordVideo, recordPalette(cfg), cfg.Video.RecordScale, videoOverscan(cfg)); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
	}
```

（GUI の終了時はタスク 4 の `Stop` が録画を閉じる。）

- [x] **手順 4: 通ることを確かめる**

Run: `go test ./cmd/shogun/ && go vet ./cmd/shogun/`
Expected: PASS（既存の `TestHeadlessRecordsAndReplaysMovie`・`TestHeadlessDetectsDesync` も通る）

---

### タスク 7: 「記録」メニュー・録画の表示・書き出しのダイアログ・設定画面（`internal/ui`）

**ファイル:**
- 変える: `internal/ui/state.go`（`movieMenu` → `recordMenu`、ダイアログ）
- 作る: `internal/ui/videorec.go`（録画の開始・停止・書き出し）
- 変える: `internal/ui/menu.go`（メニューバーの並び）
- 変える: `internal/ui/statusbar.go`（録画中の表示）
- 変える: `internal/ui/settings.go`（`video.recordScale`・`paths.videoDir`）
- 変える: `internal/ui/i18n/ids.go`・`internal/ui/i18n/ja.go`
- テスト: `internal/ui/menu_test.go`・`internal/ui/statusbar_test.go`（既存のものに足す）

**受け渡し:**
- 使う: タスク 4 の `StartRecordingVideo`・`StopRecordingVideo`・`Status().Video`、タスク 5 の `ExportVideo`、タスク 3 の `Paths.VideoDir`・`cfg.Video.RecordScale`
- 作る: `func (u *UI) recordMenu() *fyne.Menu`、`func videoText(s emu.Status) string`

- [x] **手順 1: 文言を決める**

`ids.go` と `ja.go` を次のとおりにする（既存の ID は値だけ変える）。

| ID | 文言 |
|---|---|
| `MenuMovie`（既存） | `記録` |
| `MenuOperationRecord`（新） | `操作の記録` |
| `MenuOperationPlay`（新） | `操作の再生` |
| `MenuVideo`（新） | `録画` |
| `MenuStartRecording`（既存） | `開始…` |
| `MenuStopRecording`（既存） | `停止` |
| `MenuPlayMovie`（既存） | `再生…` |
| `MenuVideoStart`（新） | `開始…` |
| `MenuVideoStop`（新） | `停止` |
| `MenuVideoExport`（新） | `操作の記録から書き出す…` |
| `SetPathMovie`（既存） | `操作の記録` |
| `SetChecksumInterval`（既存） | `操作の記録のチェックサムの間隔（フレーム）` |
| `DialogRecordMovie`（既存） | `操作の記録を保存` |
| `DialogPlayMovie`（既存） | `操作の記録を再生` |
| `StatusRecordingStarted`（既存） | `操作の記録を始めました` |
| `StatusPlaybackStarted`（既存） | `操作の再生を始めました` |
| `StatusRecording`（既存） | `操作を記録中 %d フレーム` |
| `DialogVideoSave`（新） | `録画を保存` |
| `DialogVideoExportSource`（新） | `書き出す操作の記録を選ぶ` |
| `FilterMP4`（新） | `MP4 動画` |
| `StatusVideoRecording`（新） | ` │ ● 録画中 %d:%02d` |
| `StatusVideoStarted`（新） | `録画を始めました` |
| `StatusVideoSaved`（新） | `録画を保存しました: %s` |
| `VideoExportTitle`（新） | `動画を書き出し中` |
| `VideoExportProgress`（新） | `%d / %d フレーム` |
| `VideoExportDone`（新） | `動画を書き出しました: %s` |
| `VideoNeedROM`（新） | `操作の記録を記録したときの ROM を開いてから書き出してください` |
| `SetRecordScale`（新） | `録画の拡大率` |
| `SetRecordScaleDesc`（新） | `録画する動画の大きさ。2 倍で幅が 512 になる` |
| `SetPathVideo`（新） | `録画` |

- [x] **手順 2: 失敗するテストを書く**

`internal/ui/statusbar_test.go` に:

```go
// TestVideoText は録画中の経過時間の表示を確かめる（設計書 10 編 §10.1）。
func TestVideoText(t *testing.T) {
	s := emu.Status{Video: emu.VideoStatus{Recording: true, Frames: 3725, FrameRate: 60}}
	if got, want := videoText(s), i18n.T(i18n.StatusVideoRecording, 1, 2); got != want {
		t.Errorf("videoText = %q、期待 %q", got, want)
	}
	if got := videoText(emu.Status{}); got != "" {
		t.Errorf("録画していないときの表示 = %q", got)
	}
}
```

`internal/ui/menu_test.go` の、メニューの名前を並べて確かめるテストに、`記録` メニューが `操作の記録`・`操作の再生`・`録画` の 3 つの子メニューを持ち、`録画` の中に `開始…`・`停止`・`操作の記録から書き出す…` があることを足す（既存のテストがメニューを組み立てて項目の文言を比べる形に合わせる）。

- [x] **手順 3: 失敗を確かめる**

Run: `go test ./internal/ui/ -run 'VideoText|Menu'`
Expected: FAIL

- [x] **手順 4: 実装する**

`statusbar.go` の `text` で、`movieText` の後に:

```go
	sb.WriteString(videoText(s))
```

と、次の関数を足す。

```go
// videoText は録画中の経過時間を返す。録画していないとき空。
func videoText(s emu.Status) string {
	v := s.Video
	if !v.Recording || v.FrameRate <= 0 {
		return ""
	}
	sec := int(float64(v.Frames) / v.FrameRate)
	return i18n.T(i18n.StatusVideoRecording, sec/60, sec%60)
}
```

`state.go` の `movieMenu` を `recordMenu` に改める。

```go
// recordMenu は「記録」メニューを作る（設計書 10 編 §10.5）。「操作の記録」は
// 入力ムービー、「録画」は動画の書き出しである。
func (u *UI) recordMenu() *fyne.Menu {
	op := fyne.NewMenuItem(i18n.T(i18n.MenuOperationRecord), nil)
	op.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem(i18n.T(i18n.MenuStartRecording), u.startRecordingMovie),
		fyne.NewMenuItem(i18n.T(i18n.MenuStopRecording), func() { u.showError(u.emu.StopRecordingMovie()) }),
	)
	play := fyne.NewMenuItem(i18n.T(i18n.MenuOperationPlay), nil)
	play.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem(i18n.T(i18n.MenuPlayMovie), u.playMovie),
		fyne.NewMenuItem(i18n.T(i18n.MenuStop), func() { u.showError(u.emu.StopMovie()) }),
	)
	vid := fyne.NewMenuItem(i18n.T(i18n.MenuVideo), nil)
	vid.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem(i18n.T(i18n.MenuVideoStart), u.startRecordingVideo),
		fyne.NewMenuItem(i18n.T(i18n.MenuVideoStop), u.stopRecordingVideo),
		fyne.NewMenuItem(i18n.T(i18n.MenuVideoExport), u.exportVideo),
	)
	return fyne.NewMenu(i18n.T(i18n.MenuMovie), op, play, vid)
}
```

`menu.go` の `u.movieMenu(),` を `u.recordMenu(),` に変える。

`internal/ui/videorec.go`:

```go
package ui

import (
	"context"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// videoDir は録画の保存先を返す。無ければ作る（設計書 11 編 §11.2）。
func (u *UI) videoDir() string {
	dir := u.store.Paths.VideoDir(u.cfg.Paths.VideoDir)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// videoOverscan は画面の設定と同じオーバースキャンを返す。
func (u *UI) videoOverscan() video.Overscan {
	v := u.cfg.Video
	return video.Overscan{Top: v.OverscanTop, Bottom: v.OverscanBottom, Left: v.OverscanLeft, Right: v.OverscanRight}
}

// askVideoPath は録画の保存先を尋ねる。取り消したとき空を返す。
func (u *UI) askVideoPath() string {
	path, err := zenity.SelectFileSave(
		zenity.Title(i18n.T(i18n.DialogVideoSave)),
		zenity.ConfirmOverwrite(),
		zenity.Filename(filepath.Join(u.videoDir(), u.defaultSaveName(".mp4"))),
		zenity.FileFilter{Name: i18n.T(i18n.FilterMP4), Patterns: []string{"*.mp4"}},
	)
	if !u.dialogPath(path, err) {
		return ""
	}
	return path
}

// startRecordingVideo は保存先を尋ねて録画を始める（設計書 08 編 §8.8.4）。
func (u *UI) startRecordingVideo() {
	path := u.askVideoPath()
	if path == "" {
		return
	}
	if err := u.emu.StartRecordingVideo(path, u.pal, u.cfg.Video.RecordScale, u.videoOverscan()); err != nil {
		u.showError(err)
		return
	}
	u.videoPath = path
	u.status.notify(i18n.T(i18n.StatusVideoStarted))
}

// stopRecordingVideo は録画を止める。
func (u *UI) stopRecordingVideo() {
	if !u.emu.Status().Video.Recording {
		return
	}
	if err := u.emu.StopRecordingVideo(); err != nil {
		u.showError(err)
		return
	}
	u.status.notify(i18n.T(i18n.StatusVideoSaved, filepath.Base(u.videoPath)))
}

// exportVideo は操作の記録を選び、動画を書き出す。進み具合をダイアログに出し、
// 取り消せる（設計書 08 編 §8.8.4）。
func (u *UI) exportVideo() {
	rom := u.agentUI.romPath
	if rom == "" {
		u.showError(errors.New(i18n.T(i18n.VideoNeedROM)))
		return
	}
	src, err := zenity.SelectFile(
		zenity.Title(i18n.T(i18n.DialogVideoExportSource)),
		zenity.FileFilter{Name: i18n.T(i18n.SetPathMovie), Patterns: []string{"*.movie", "*.shgm"}},
	)
	if !u.dialogPath(src, err) {
		return
	}
	out := u.askVideoPath()
	if out == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	bar := widget.NewProgressBar()
	label := widget.NewLabel("")
	d := dialog.NewCustom(i18n.T(i18n.VideoExportTitle), i18n.T(i18n.CommonCancel),
		container.NewVBox(label, bar), u.win)
	d.SetOnClosed(cancel)
	d.Show()
	go func() {
		err := u.emu.ExportVideo(ctx, emu.VideoExport{
			ROMPath: rom, MoviePath: src, OutPath: out, Palette: u.pal,
			Scale: u.cfg.Video.RecordScale, Overscan: u.videoOverscan(),
			Progress: func(done, total uint64) {
				u.post(func() {
					bar.SetValue(float64(done) / float64(max(total, 1)))
					label.SetText(i18n.T(i18n.VideoExportProgress, done, total))
				})
			},
		})
		u.post(func() {
			d.Hide()
			if err != nil {
				if ctx.Err() == nil {
					u.showError(err)
				}
				return
			}
			u.status.notify(i18n.T(i18n.VideoExportDone, filepath.Base(out)))
		})
	}()
}
```

- `u.post` は UI スレッドで関数を実行する既存の関数（`internal/ui/dispatch.go`。`fyne.Do` を呼ぶのはこのファイルだけという規則がある）の名前に合わせる。
- `u.store`・`u.pal`・`u.agentUI.romPath`・`i18n.CommonCancel` の名前は既存のものに合わせる（`u.pal` は `newScreen` に渡しているパレット）。`CommonCancel` が無ければ `ids.go`・`ja.go` に `キャンセル` として足す。
- `UI` に `videoPath string // 録画中の動画のパス。保存したことを知らせるのに使う` を足す。
- import に `errors` と `fyne.io/fyne/v2/container` を足す。

`settings.go`:
- 映像のタブの `MenuScale` の欄の次に:

```go
		f.intField(i18n.T(i18n.SetRecordScale), i18n.T(i18n.SetRecordScaleDesc), config.MinRecordScale, config.MaxRecordScale,
			func() int { return v.RecordScale }, func(n int) { v.RecordScale = n },
			func(c *config.Config) any { return c.Video.RecordScale }),
```

- 保存先のタブの `SetPathScreenshot` の欄の次に:

```go
		field(i18n.T(i18n.SetPathVideo), &p.VideoDir, func(c *config.Config) any { return c.Paths.VideoDir }),
```

- [x] **手順 5: 通ることを確かめる**

Run: `go test ./internal/ui/ ./internal/arch/ && go vet ./internal/ui/`
Expected: PASS（`TestUITextLivesInCatalog` が直書きの文言を見つけない）

- [ ] **手順 6: 手で確かめる**

`go run ./cmd/shogun <ROM>` で起動し、次を確かめる。

| 操作 | 期待 |
|---|---|
| メニューバー | 「記録」があり、「操作の記録」「操作の再生」「録画」の子メニューがある |
| 録画 → 開始… | 保存先のダイアログが `~/Movies/ShogunEmulator` で開き、名前が `<ROM 名>-<日時>.mp4` |
| 録画中 | ステータスバーに「● 録画中 0:05」のように経過時間が出る |
| 録画 → 停止 | 「録画を保存しました」が出て、QuickTime Player で画と音が再生できる |
| 録画中に巻き戻し・ステートの読み込み | 再生した動画で、その後の音と画がずれない |
| 操作の記録 → 開始… で記録し、録画 → 操作の記録から書き出す… | 進み具合のダイアログが出て、終わると動画が保存される。キャンセルするとファイルが残らない |
| 設定 → 映像 → 録画の拡大率を 3 にして録画 | 768×672（オーバースキャン 8/8 のとき）の動画になる |

---

### タスク 8: 利用者向けの文書と計画の一覧

**ファイル:**
- 変える: `packaging/usage.md`
- 変える: `README.md`
- 変える: `docs/plans/README.md`

- [x] **手順 1: `packaging/usage.md` を直す**

「基本の操作」の後に「録画と操作の記録」の節を足す。

```markdown
## 録画と操作の記録

「記録」メニューに 3 つの機能があります。

| 機能 | 内容 |
|---|---|
| 操作の記録・操作の再生 | ボタンの操作だけを記録したファイル（`.movie`）を作り、再生します。ファイルは小さく、このエミュレータと同じ ROM で再生すると、画も音もそのとおりに再現します。動画プレイヤーでは開けません |
| 録画 | 画面と音を動画（MP4）に保存します。QuickTime Player などで再生できます。拡大率は設定の「録画の拡大率」（1〜3 倍、既定 2 倍）で変えられます |
| 操作の記録から書き出す | 保存した操作の記録を、実時間より速く動画にします。記録したときと同じ ROM を開いてから使います |

動画は Motion JPEG という形式で、ファイルが大きくなります（2 倍で 1 分あたり数十 MB）。早送りやスローで遊んだ部分も、動画では等速になります。

コマンドラインからは `shogun --headless --movie 記録.movie --record-video 出力.mp4 ゲーム.nes` で、画面を出さずに操作の記録から動画を作れます。
```

「設定とセーブデータの保存先」の表に行を足す。

```markdown
| 録画した動画 | `~/Movies/ShogunEmulator/` | `%UserProfile%\Videos\ShogunEmulator\` | `~/Videos/ShogunEmulator/` |
```

「コマンドライン」の表に `| --record-video PATH | 動画（MP4）を録画する |` を足す。

- [x] **手順 2: `README.md` を直す**

英語の Features の表の Play の行に `video recording to MP4 (Motion JPEG and PCM)` を、日本語の機能の表のプレイの行に `動画（MP4）の録画` を足す。

- [x] **手順 3: 計画の一覧を更新する**

`docs/plans/README.md` のフェーズの表に、次の行を足す（完了したら状態を「完了」にする）。

```markdown
| [22](phase-22-video-recording.md) | 動画の書き出し（録画・操作の記録からの書き出し・`--record-video`）と「記録」メニュー | 3 つの経路で MP4 を書き出せ、録画しても状態のハッシュが変わらず、QuickTime Player で再生できる | 進行中 |
```

- [x] **手順 4: 全体を確かめる**

Run: `gofmt -l . && go vet ./... && go test -short ./...`
Expected: 何も出力されず（gofmt）、すべて PASS

---

## 自己点検の結果

| 設計書の項目 | タスク |
|---|---|
| 08 編 §8.8.1 ファイル形式 | 1・2 |
| §8.8.2 記録する内容（専用リサンプラ・音声の量・状態を変えない） | 2・4 |
| §8.8.3 書き出しの経路（API・別ゴルーチン・列・末尾の moov・失敗時） | 1・2・4 |
| §8.8.4 録画の操作（開始・停止・自動で閉じる・表示・書き出し・CLI） | 4・5・6・7 |
| §8.8.5 テスト | 1・2・4・5・6 |
| 10 編（メニュー・ステータスバー） | 7 |
| 11 編（設定・保存先・環境変数・CLI） | 3・6 |
| 01 編（依存の規則） | 2（手順 5） |
| 13 編（依存モジュール） | 1（手順 1） |
