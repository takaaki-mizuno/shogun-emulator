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
