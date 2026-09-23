package testrom_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// nestestStartPC は nestest の自動テストの入口。
//
// 電源投入後のベクタではなくここから始めると、画面を見ずに全命令を
// 検証できる。
const nestestStartPC = 0xC000

// TestNestest は nestest.nes のトレースを nestest.log と 1 行ずつ比較する。
//
// CPU の正しさを機械的に判定できる唯一の強い手段である。PC・機械語・
// 逆アセンブル結果・レジスタ・PPU の走査位置・累積サイクル数をすべて
// 含むため、CPU と逆アセンブラとバスのタイミングを同時に検証できる。
func TestNestest(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	logPath := testrom.RequireROM(t, "other/nestest.log")

	n := newNestestNES(t, romPath)
	n.PowerOn(nes.Deterministic())
	n.CPU.PC = nestestStartPC

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("nestest.log を開けない: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256), 256)

	// 直前の行を保持する。不一致のときに前後の文脈を出すためである。
	const contextLines = 10
	recent := make([]string, 0, contextLines)

	matched := 0
	for sc.Scan() {
		want := strings.TrimRight(sc.Text(), " \r")
		if want == "" {
			continue
		}

		got := strings.TrimRight(n.TraceLine(), " ")
		if got != want {
			reportMismatch(t, matched+1, got, want, recent)
			return
		}

		if len(recent) == contextLines {
			recent = recent[1:]
		}
		recent = append(recent, got)

		matched++
		n.StepInstruction()
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("nestest.log の読み出しに失敗した: %v", err)
	}
	t.Logf("%d 行が一致した", matched)
}

// reportMismatch は不一致の箇所を前後の文脈とともに報告する。
func reportMismatch(t *testing.T, line int, got, want string, recent []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("直前の行:\n")
	for _, l := range recent {
		b.WriteString("    " + l + "\n")
	}
	b.WriteString("\n")
	// 桁位置の違いを見分けられるように、桁の目印を添える。
	b.WriteString("          " + ruler(len(want)) + "\n")
	b.WriteString("    実際: " + got + "\n")
	b.WriteString("    期待: " + want + "\n")
	if d := firstDiffColumn(got, want); d >= 0 {
		b.WriteString("    " + strings.Repeat(" ", 10+d) + "^ 桁 " + itoa(d) + " から異なる\n")
	}
	t.Fatalf("nestest.log の %d 行目で不一致\n%s", line, b.String())
}

// ruler は 10 桁ごとの目印を返す。
func ruler(n int) string {
	var b strings.Builder
	for i := range n {
		switch {
		case i%10 == 0:
			b.WriteByte('|')
		case i%5 == 0:
			b.WriteByte('+')
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// firstDiffColumn は最初に異なる桁を返す。一致していれば -1。
func firstDiffColumn(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// newNestestNES は nestest 用にエミュレータを組み立てる。
func newNestestNES(t *testing.T, romPath string) *nes.NES {
	t.Helper()
	data, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatalf("ROM を読めない: %v", err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	for _, w := range rom.Warnings {
		t.Logf("ROM の警告: %s", w)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatalf("エミュレータを組み立てられない: %v", err)
	}
	return n
}
