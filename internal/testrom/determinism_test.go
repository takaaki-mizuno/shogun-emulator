package testrom_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// movieDir は golden ムービーを置くディレクトリ。
const movieDir = "testdata/golden/movies"

// determinismCases は決定論の検証に使う ROM とムービー。
//
// マッパーと音源の組み合わせを変える。DPCM を鳴らす ROM を含めるのは、
// DMC の DMA が CPU を止める経路を通すためである。
var determinismCases = []struct {
	rom    string
	movie  string
	frames int
}{
	{"other/nestest.nes", "nestest.movie", 240},
	{"holy-mapperel/testroms/M1_P128K_C128K.nes", "mmc1.movie", 240},
	{"mmc3_test_2/rom_singles/4-scanline_timing.nes", "mmc3.movie", 240},
	{"dpcmletterbox/dpcmletterbox.nes", "dpcm.movie", 240},
}

// scriptedPad はフレーム番号から押下状態を決める供給元。
//
// 記録するムービーに入力の変化を含めるために使う。乱数を使わないのは、
// 生成したムービーが実行ごとに変わらないようにするためである。
type scriptedPad struct {
	buttons [input.PortCount]uint8
}

func (p *scriptedPad) Buttons(port int) uint8 {
	if port < 0 || port >= input.PortCount {
		return 0
	}
	return p.buttons[port]
}

// scriptedButtons はフレーム番号に対する押下状態を返す。
func scriptedButtons(frame uint64) [input.PortCount]uint8 {
	// 8 種類のボタンを順に押す。押す長さを変えて、同じ並びが続かない
	// ようにする。
	bits := []uint8{
		input.ButtonA, input.ButtonB, input.ButtonSelect, input.ButtonStart,
		input.ButtonUp, input.ButtonDown, input.ButtonLeft, input.ButtonRight,
	}
	var out [input.PortCount]uint8
	if frame%5 < 3 {
		out[0] = bits[(frame/5)%uint64(len(bits))]
	}
	if frame%11 == 0 {
		out[1] = input.ButtonStart
	}
	return out
}

// newMovieMachine は決定論の検証に使う本体を組み立てる。
func newMovieMachine(t testing.TB, romName string, init nes.InitState) (*nes.NES, *scriptedPad) {
	t.Helper()
	data, err := os.ReadFile(testrom.RequireROM(t, romName))
	if err != nil {
		t.Fatalf("ROM を読めない: %v", err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatalf("エミュレータを組み立てられない: %v", err)
	}
	pad := &scriptedPad{}
	n.ConnectStandardControllers(pad)
	n.PowerOn(init)
	return n, pad
}

// recordMovie は決められた入力でムービーを記録する。
func recordMovie(t *testing.T, romName string, frames int) *movie.Movie {
	t.Helper()
	init := nes.Deterministic()
	n, pad := newMovieMachine(t, romName, init)

	h := n.Header()
	rec := movie.NewRecorder(movie.Header{
		Version:          h.Version,
		Commit:           h.Commit,
		ROMHash:          h.ROMHash,
		ROMName:          filepath.Base(romName),
		Region:           h.Region,
		Mapper:           h.Mapper,
		Submapper:        h.Submapper,
		Init:             init,
		Start:            movie.StartPowerOn,
		Ports:            [2]string{n.Ports[0].Kind(), n.Ports[1].Kind()},
		ChecksumInterval: 30,
		Comment:          "決定論テスト用に生成した",
	})

	for frame := range uint64(frames) {
		pad.buttons = scriptedButtons(frame)
		rec.BeginFrame(pad.buttons, n.StateHash())
		n.RunFrame()
	}
	return rec.Movie()
}

// replayMovie はムービーを再生し、最後の状態のハッシュを返す。
//
// チェックサムが合わないときはその場で失敗させる。
func replayMovie(t *testing.T, romName string, m *movie.Movie) [8]uint8 {
	t.Helper()
	return replayMovieWith(t, romName, m, nil)
}

// replayMovieWith は本体を組み立てた直後に setup を呼んでからムービーを再生する。
func replayMovieWith(t *testing.T, romName string, m *movie.Movie, setup func(n *nes.NES)) [8]uint8 {
	t.Helper()
	n, pad := newMovieMachine(t, romName, m.Header.Init)
	if setup != nil {
		setup(n)
	}

	want := n.Header()
	p := movie.NewPlayer(m)
	movieWant := movie.Header{
		ROMHash:   want.ROMHash,
		Mapper:    want.Mapper,
		Submapper: want.Submapper,
		Region:    want.Region,
		Version:   want.Version,
		Commit:    want.Commit,
	}
	if _, err := p.VerifyHeader(&movieWant); err != nil {
		t.Fatalf("ムービーを再生できない: %v", err)
	}

	for {
		f, ok := p.BeginFrame()
		if !ok {
			break
		}
		switch {
		case f.HardReset:
			n.PowerOn(m.Header.Init)
		case f.Reset:
			n.Reset()
		}
		pad.buttons = f.Buttons
		if f.Checksum != nil {
			if got := n.StateHash(); got != *f.Checksum {
				t.Fatalf("フレーム %d で状態が一致しない（記録 %x、実際 %x）",
					p.Frame()-1, *f.Checksum, got)
			}
		}
		n.RunFrame()
	}
	return n.StateHash()
}

// goldenMoviePath は golden ムービーのパスを返す。
func goldenMoviePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(testrom.RepoRoot(t), movieDir, name)
}

// loadGoldenMovie は golden ムービーを読む。
//
// 環境変数 SHOGUN_UPDATE_MOVIES を設定すると記録し直す。エミュレーションの
// 挙動を意図して変えたときに使う（設計書 08 編 §8.7.4）。
func loadGoldenMovie(t *testing.T, romName, movieName string, frames int) *movie.Movie {
	t.Helper()
	path := goldenMoviePath(t, movieName)

	if os.Getenv("SHOGUN_UPDATE_MOVIES") != "" {
		m := recordMovie(t, romName, frames)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, m.Encode(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s を記録した（%d フレーム）", movieName, m.Header.TotalFrames)
		return m
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s が無い。SHOGUN_UPDATE_MOVIES=1 で記録する", movieName)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatalf("%s を読めない: %v", movieName, err)
	}
	return m
}

// TestMovieChecksums は記録されたチェックサムと再生した状態が一致する
// ことを確かめる。
//
// エミュレーションの挙動を変えたときに、この検証が失敗することで変更の
// 影響範囲が分かる。
func TestMovieChecksums(t *testing.T) {
	for _, tc := range determinismCases {
		t.Run(tc.rom, func(t *testing.T) {
			testrom.RequireROM(t, tc.rom)
			m := loadGoldenMovie(t, tc.rom, tc.movie, tc.frames)
			h := replayMovie(t, tc.rom, m)
			t.Logf("%s の最終ハッシュ: %x", tc.movie, h)
		})
	}
}

// TestMovieReproducible は同じムービーを 2 回再生して結果が一致する
// ことを確かめる。
//
// ホスト側の非決定性（map のたどり方・ゴルーチン・壁時計・グローバル
// 乱数）が混ざっていれば、2 回目の結果が変わる。
func TestMovieReproducible(t *testing.T) {
	for _, tc := range determinismCases {
		t.Run(tc.rom, func(t *testing.T) {
			testrom.RequireROM(t, tc.rom)
			m := loadGoldenMovie(t, tc.rom, tc.movie, tc.frames)
			first := replayMovie(t, tc.rom, m)
			second := replayMovie(t, tc.rom, m)
			if first != second {
				t.Errorf("2 回の再生で結果が違う（%x と %x）", first, second)
			}
		})
	}
}

// TestDeterminismHashes は各 ROM の最終ハッシュを出力する。
//
// 3 つの OS で同じ値になることを CI が比べる。値をここに固定しないのは、
// エミュレーションの挙動を変えるたびに書き換えることになり、OS 間の
// 比較という目的から離れるためである。
func TestDeterminismHashes(t *testing.T) {
	for _, tc := range determinismCases {
		m := loadGoldenMovie(t, tc.rom, tc.movie, tc.frames)
		h := replayMovie(t, tc.rom, m)
		fmt.Printf("determinism %s %x\n", tc.movie, h)
	}
}
