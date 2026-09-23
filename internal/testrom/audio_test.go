package testrom_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/audio"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// sampleCollector はリサンプラの出力を集める。
type sampleCollector struct{ out []int16 }

func (c *sampleCollector) WriteSample(v int16) bool {
	c.out = append(c.out, v)
	return true
}

// stats は波形の最小値・最大値・二乗平均平方根を返す。
func (c *sampleCollector) stats() (min, max int16, rms float64) {
	if len(c.out) == 0 {
		return 0, 0, 0
	}
	min, max = c.out[0], c.out[0]
	var sum float64
	for _, v := range c.out {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += float64(v) * float64(v)
	}
	return min, max, sqrt(sum / float64(len(c.out)))
}

// zeroCrossings は平均値をまたいだ回数を返す。音の高さの目安になる。
//
// 0 ではなく平均をまたぐ回数を数える。フィルタを通さないときは
// 直流成分が残り、波形が 0 をまたがないためである。
func (c *sampleCollector) zeroCrossings() int {
	if len(c.out) == 0 {
		return 0
	}
	var sum float64
	for _, v := range c.out {
		sum += float64(v)
	}
	mean := sum / float64(len(c.out))

	n := 0
	for i := 1; i < len(c.out); i++ {
		if (float64(c.out[i-1]) < mean) != (float64(c.out[i]) < mean) {
			n++
		}
	}
	return n
}

// sqrt は平方根を返す。math を使わずに書くほどのことはないが、
// テストの依存を増やさないために自前で持つ。
func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for range 40 {
		x = (x + v/x) / 2
	}
	return x
}

// audioROMs は音を確かめる ROM と鳴らすフレーム数。
var audioROMs = []struct {
	rom    string
	warmup int
	frames int
}{
	{"apu_mixer/square.nes", 120, 240},
	{"apu_mixer/triangle.nes", 120, 240},
	{"apu_mixer/noise.nes", 120, 240},
	{"apu_mixer/dmc.nes", 120, 240},
}

// TestAudioPipelineProducesSound は APU からリサンプラまでの経路が
// 無音でない波形を出すことを確かめる。
//
// 出力デバイスを使わない。デバイスの有無に関わらず、波形が作られる
// ところまでを機械的に確かめる。環境変数 SHOGUN_AUDIO_WAV を設定すると
// WAV を書き出す。耳で確かめるときに使う。
func TestAudioPipelineProducesSound(t *testing.T) {
	for _, tt := range audioROMs {
		t.Run(tt.rom, func(t *testing.T) {
			path := testrom.RequireROM(t, tt.rom)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rom, err := cart.LoadROM(data)
			if err != nil {
				t.Fatal(err)
			}
			n, err := nes.New(rom, region.NTSC)
			if err != nil {
				t.Fatal(err)
			}

			c := &sampleCollector{}
			r := audio.NewResampler(region.NTSC.CPUClockHz(), audio.SampleRate, audio.ProfileNES, c)
			n.APU.SetOutput(apuSink{r})
			n.PowerOn(nes.Deterministic())

			// 起動直後は音を出さない ROM が多い。立ち上がりの過渡を
			// 波形に含めないために、少し進めてから集める。
			n.RunFrames(tt.warmup)
			c.out = nil
			r.Reset()
			n.RunFrames(tt.frames)

			wantSamples := audio.SampleRate * tt.frames / 60
			if len(c.out) < wantSamples*9/10 {
				t.Fatalf("サンプル数 = %d, 期待 %d 付近", len(c.out), wantSamples)
			}

			min, max, rms := c.stats()
			t.Logf("最小 %d、最大 %d、二乗平均平方根 %.0f、ゼロ交差 %d（%d サンプル）",
				min, max, rms, c.zeroCrossings(), len(c.out))
			if rms < 100 {
				t.Errorf("波形がほぼ無音である（二乗平均平方根 %.0f）", rms)
			}
			if min == max {
				t.Error("波形が一定値である")
			}

			if dir := os.Getenv("SHOGUN_AUDIO_WAV"); dir != "" {
				name := filepath.Base(tt.rom[:len(tt.rom)-4]) + ".wav"
				if err := writeWAV(filepath.Join(dir, name), c.out); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// apuSink は apu.Output を audio.Resampler へつなぐ。
type apuSink struct{ r *audio.Resampler }

func (s apuSink) WriteSample(v float32) { s.r.WriteSample(v) }

// writeWAV は 16 bit モノラルの WAV を書く。
func writeWAV(path string, samples []int16) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const channels = 1
	const bitsPerSample = 16
	dataSize := len(samples) * 2
	byteRate := audio.SampleRate * channels * bitsPerSample / 8

	w := func(v any) {
		binary.Write(f, binary.LittleEndian, v)
	}
	f.WriteString("RIFF")
	w(uint32(36 + dataSize))
	f.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(channels))
	w(uint32(audio.SampleRate))
	w(uint32(byteRate))
	w(uint16(channels * bitsPerSample / 8))
	w(uint16(bitsPerSample))
	f.WriteString("data")
	w(uint32(dataSize))
	for _, s := range samples {
		w(s)
	}
	return f.Sync()
}

// toneCases は鳴らす音の指定と期待する周波数。
//
// 周波数は調査文書 04 編の式から求める。Pulse は
// f = f_CPU / (16 × (t + 1))、Triangle は f = f_CPU / (32 × (t + 1))。
var toneCases = []struct {
	name    string
	writes  [][2]uint16 // アドレスと値
	timer   int
	divisor float64
}{
	{
		name: "Pulse 1 の 440 Hz",
		writes: [][2]uint16{
			{0x4015, 0x01},
			{0x4000, 0xBF}, // デューティ 2（50%）、halt、定音量 15
			{0x4001, 0x08}, // スイープ無効。negate を立ててオーバーフローを防ぐ
			{0x4002, 253 & 0xFF},
			{0x4003, 253 >> 8},
		},
		timer:   253,
		divisor: 16,
	},
	{
		name: "Pulse 2 の 1 オクターブ上",
		writes: [][2]uint16{
			{0x4015, 0x02},
			{0x4004, 0xBF},
			{0x4005, 0x08},
			{0x4006, 126 & 0xFF},
			{0x4007, 126 >> 8},
		},
		timer:   126,
		divisor: 16,
	},
	{
		name: "Triangle の 440 Hz",
		writes: [][2]uint16{
			{0x4015, 0x04},
			{0x4008, 0xFF}, // control をセットしてリニアカウンタを止めない
			{0x400A, 126 & 0xFF},
			{0x400B, 126 >> 8},
		},
		timer:   126,
		divisor: 32,
	},
}

// TestAPUToneFrequency は鳴らした音の周波数が式どおりであることを確かめる。
//
// APU のタイマーからミキサー、リサンプラまでの経路を通して測る。
// 経路のどこかで 2 倍または半分になっていれば、ここで分かる。
func TestAPUToneFrequency(t *testing.T) {
	for _, tt := range toneCases {
		t.Run(tt.name, func(t *testing.T) {
			path := testrom.RequireROM(t, "other/nestest.nes")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rom, err := cart.LoadROM(data)
			if err != nil {
				t.Fatal(err)
			}
			n, err := nes.New(rom, region.NTSC)
			if err != nil {
				t.Fatal(err)
			}

			c := &sampleCollector{}
			r := audio.NewResampler(region.NTSC.CPUClockHz(), audio.SampleRate, audio.ProfileNone, c)
			n.APU.SetOutput(apuSink{r})
			n.PowerOn(nes.Deterministic())

			for _, w := range tt.writes {
				n.Bus.Write(w[0], uint8(w[1]))
			}

			// 0.5 秒ぶん進める。立ち上がりを除くために前半を捨てる。
			const seconds = 0.5
			cycles := int(region.NTSC.CPUClockHz() * seconds)
			for range cycles {
				n.Bus.Read(0x0000)
			}
			c.out = c.out[len(c.out)/2:]

			want := region.NTSC.CPUClockHz() / (tt.divisor * float64(tt.timer+1))
			// ゼロ交差は 1 周期に 2 回起こる。
			got := float64(c.zeroCrossings()) / 2 / (seconds / 2)

			if got < want*0.97 || got > want*1.03 {
				t.Errorf("周波数 = %.1f Hz, 期待 %.1f Hz", got, want)
			}
			t.Logf("周波数 %.1f Hz（期待 %.1f Hz）", got, want)
		})
	}
}
