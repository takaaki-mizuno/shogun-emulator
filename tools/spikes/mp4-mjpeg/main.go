// Command mp4-mjpeg は、Go だけで手元のプレイヤーで再生できる MP4 を作れるかを試す。
//
// 映像は標準ライブラリの image/jpeg で圧縮した Motion JPEG、音声は無圧縮の PCM と
// し、mp4ff で MP4 に格納する。H.264 と AAC のエンコーダを使わずに済むかを見る。
//
// 使い方:
//
//	go run ./mp4-mjpeg <出力ディレクトリ>
//
// 次の組み合わせを書き出す。
//
//	<entry>-frag.mp4  フラグメント形式（moov の後に moof と mdat が続く）
//	<entry>.mp4       通常の形式（Defragment で変換したもの）
//
// <entry> は映像のサンプル記述の種類で、jpeg（QuickTime の Motion JPEG）と
// mjpg（ISO/IEC 23008-12 の JPEG の画像列）の 2 つ。
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

const (
	nesWidth    = 256
	nesHeight   = 240
	scale       = 3
	frames      = 600
	fps         = 60
	sampleRate  = 48000
	channels    = 2
	jpegQuality = 90
	// fragmentFrames は 1 つのフラグメントに入れる映像のフレーム数（1 秒分）。
	fragmentFrames = 60
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "使い方: mp4-mjpeg <出力ディレクトリ>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}

	start := time.Now()
	video := make([][]byte, frames)
	for i := range video {
		video[i] = encodeJPEG(nesFrame(i))
	}
	encodeTime := time.Since(start)
	var videoBytes int
	for _, v := range video {
		videoBytes += len(v)
	}
	audio := squareWave(frames * sampleRate / fps)
	fmt.Printf("JPEG: %d フレームを %v で圧縮（1 フレーム %.2f ms、平均 %d バイト）\n",
		frames, encodeTime.Round(time.Millisecond), float64(encodeTime.Microseconds())/1000/frames, videoBytes/frames)

	for _, entry := range []string{"jpeg", "mjpg"} {
		frag := filepath.Join(dir, entry+"-frag.mp4")
		if err := writeFragmented(frag, entry, video, audio); err != nil {
			panic(err)
		}
		prog := filepath.Join(dir, entry+".mp4")
		if err := defragment(frag, prog); err != nil {
			fmt.Printf("%s: 通常の形式へ変換できない: %v\n", entry, err)
			continue
		}
		for _, p := range []string{frag, prog} {
			fi, _ := os.Stat(p)
			fmt.Printf("%s: %d バイト\n", p, fi.Size())
		}
	}
}

// nesFrame は NES 風の画を描く。スクロールする市松模様の背景と、動く四角を置く。
func nesFrame(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, nesWidth*scale, nesHeight*scale))
	pal := []color.RGBA{{0x5C, 0x94, 0xFC, 0xFF}, {0x00, 0xA8, 0x00, 0xFF}, {0xFC, 0xFC, 0xFC, 0xFF}, {0xD8, 0x28, 0x00, 0xFF}}
	bx, by := 40+(n*2)%160, 100+int(40*math.Sin(float64(n)/15))
	for y := range nesHeight {
		for x := range nesWidth {
			c := pal[((x+n)/16+y/16)%2]
			if y > 200 {
				c = pal[1]
			}
			if x >= bx && x < bx+16 && y >= by && y < by+16 {
				c = pal[3]
			}
			for dy := range scale {
				for dx := range scale {
					img.SetRGBA(x*scale+dx, y*scale+dy, c)
				}
			}
		}
	}
	return img
}

// encodeJPEG は画を JPEG にする。
func encodeJPEG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// squareWave は n 個の PCM フレーム（16 ビット、リトルエンディアン、ステレオ）の
// 矩形波を返す。
func squareWave(n int) []byte {
	out := make([]byte, 0, n*channels*2)
	for i := range n {
		v := int16(6000)
		if (i/(sampleRate/440/2))%2 == 1 {
			v = -6000
		}
		for range channels {
			out = append(out, byte(v), byte(uint16(v)>>8))
		}
	}
	return out
}

// writeFragmented は映像と音声のトラックを持つフラグメント形式の MP4 を書く。
func writeFragmented(path, entry string, video [][]byte, audio []byte) error {
	init := mp4.CreateEmptyInit()
	vtrak := init.AddEmptyTrack(fps, "video", "und")
	if entry == "mjpg" {
		if err := vtrak.SetMJpegDescriptor(nesWidth*scale, nesHeight*scale, nil); err != nil {
			return err
		}
	} else {
		vtrak.Tkhd.Width = mp4.Fixed32(uint32(nesWidth*scale) << 16)
		vtrak.Tkhd.Height = mp4.Fixed32(uint32(nesHeight*scale) << 16)
		vse := mp4.CreateVisualSampleEntryBox("jpeg", nesWidth*scale, nesHeight*scale, nil)
		vse.CompressorName = "Photo - JPEG"
		vtrak.Mdia.Minf.Stbl.Stsd.AddChild(vse)
	}
	atrak := init.AddEmptyTrack(sampleRate, "audio", "und")
	atrak.Mdia.Minf.Stbl.Stsd.AddChild(mp4.CreateAudioSampleEntryBox("sowt", channels, 16, sampleRate, nil))

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := init.Encode(f); err != nil {
		return err
	}

	audioPerFrame := sampleRate / fps
	bytesPerPCM := channels * 2
	for seq, first := uint32(1), 0; first < len(video); seq, first = seq+1, first+fragmentFrames {
		frag, err := mp4.CreateMultiTrackFragment(seq, []uint32{1, 2})
		if err != nil {
			return err
		}
		last := min(first+fragmentFrames, len(video))
		for i := first; i < last; i++ {
			s := mp4.FullSample{
				Sample:     mp4.NewSample(mp4.SyncSampleFlags, 1, uint32(len(video[i])), 0),
				DecodeTime: uint64(i),
				Data:       video[i],
			}
			if err := frag.AddFullSampleToTrack(s, 1); err != nil {
				return err
			}
		}
		// PCM は 1 サンプルを PCM の 1 フレームとする（QuickTime の sowt の扱い）。
		for i := first * audioPerFrame; i < last*audioPerFrame; i++ {
			s := mp4.FullSample{
				Sample:     mp4.NewSample(mp4.SyncSampleFlags, 1, uint32(bytesPerPCM), 0),
				DecodeTime: uint64(i),
				Data:       audio[i*bytesPerPCM : (i+1)*bytesPerPCM],
			}
			if err := frag.AddFullSampleToTrack(s, 2); err != nil {
				return err
			}
		}
		if err := frag.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

// defragment はフラグメント形式を通常の形式（moov の後に 1 つの mdat）に直す。
func defragment(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	parsed, err := mp4.DecodeFile(in)
	if err != nil {
		return err
	}
	if _, err := in.Seek(0, 0); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return mp4.Defragment(parsed, in, out)
}
