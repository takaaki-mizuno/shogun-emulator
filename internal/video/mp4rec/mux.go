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
