package emu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// writeBatteryROM は $6000 へ書き続ける NROM の ROM をファイルへ書く。
//
// ヘッダのバッテリービットを立てる。iNES では PRG-RAM のサイズを
// 表せないため、8 KiB の不揮発メモリが割り当てられる。
func writeBatteryROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	code := []uint8{
		0xA9, 0x5A, // LDA #$5A
		0x8D, 0x00, 0x60, // STA $6000
		0xA9, 0xA5, // LDA #$A5
		0x8D, 0xFF, 0x7F, // STA $7FFF
		0x4C, 0x0A, 0x80, // JMP $800A
	}
	copy(prg, code)
	prg[0x7FFC] = 0x00
	prg[0x7FFD] = 0x80

	data := make([]uint8, 0, 16+len(prg)+8*1024)
	// バイト 6 の bit 1 がバッテリー。
	header := []uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	data = append(data, header...)
	data = append(data, prg...)
	data = append(data, make([]uint8, 8*1024)...)

	path := filepath.Join(t.TempDir(), "battery.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// romHashOf は ROM ファイルのハッシュからセーブデータの名前を返す。
func romHashOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	return cart.ROMKeyString(rom.Hash[:]) + batteryExt
}

// TestBatterySavesAndLoads はセーブデータが書き出され、次の起動で
// 読み込まれることを確かめる。
func TestBatterySavesAndLoads(t *testing.T) {
	saveDir := t.TempDir()
	romPath := writeBatteryROM(t)
	savePath := filepath.Join(saveDir, romHashOf(t, romPath))

	cfg := testConfig()
	cfg.Paths = config.PathsConfig{SaveDir: saveDir}

	e := New(cfg)
	e.Start()
	if err := e.LoadROM(romPath); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "PRG-RAM へ書き込まれる", func() bool {
		return peekEmu(e, 0x6000) == 0x5A && peekEmu(e, 0x7FFF) == 0xA5
	})
	// 取り外しで書き出す。3 秒待たずに済ませる。
	if err := e.Unload(); err != nil {
		t.Fatal(err)
	}
	e.Stop()

	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("セーブデータが書き出されていない: %v", err)
	}
	if len(got) != 8*1024 {
		t.Fatalf("セーブデータの大きさ = %d, 期待 8192", len(got))
	}
	if got[0] != 0x5A || got[0x1FFF] != 0xA5 {
		t.Fatalf("内容が違う（先頭 $%02X、末尾 $%02X）", got[0], got[0x1FFF])
	}

	// 別の内容で書き換えてから読み直す。読み込まれていれば Peek に出る。
	want := make([]uint8, 8*1024)
	for i := range want {
		want[i] = 0x3C
	}
	if err := os.WriteFile(savePath, want, 0o644); err != nil {
		t.Fatal(err)
	}

	e2 := New(cfg)
	e2.Start()
	defer e2.Stop()
	if err := e2.LoadROM(romPath); err != nil {
		t.Fatal(err)
	}
	// $6000 と $7FFF はプログラムが上書きする。触らない場所を見る。
	if v := peekEmu(e2, 0x6100); v != 0x3C {
		t.Errorf("$6100 = $%02X, 期待 $3C（セーブデータが読み込まれていない）", v)
	}
}

// TestBatteryFlushesAfterDelay は変化が落ち着いてから書き出すことを
// 確かめる。
func TestBatteryFlushesAfterDelay(t *testing.T) {
	dir := t.TempDir()
	c := &fakeBatteryCart{ram: make([]uint8, 8)}
	b, err := newBattery(dir, []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, c)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Fatal("不揮発メモリを持つのに nil が返った")
	}

	now := time.Unix(1000, 0)
	b.now = func() time.Time { return now }

	// 変化が無ければ書き出さない。
	if err := b.poll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.path); !os.IsNotExist(err) {
		t.Fatal("変化が無いのに書き出した")
	}

	c.ram[3] = 0xFF
	if err := b.poll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.path); !os.IsNotExist(err) {
		t.Fatal("変化した直後に書き出した")
	}

	// 3 秒に満たないうちは書き出さない。
	now = now.Add(batteryFlushDelay - time.Millisecond)
	if err := b.poll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.path); !os.IsNotExist(err) {
		t.Fatal("3 秒経つ前に書き出した")
	}

	now = now.Add(time.Millisecond)
	if err := b.poll(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatalf("書き出されていない: %v", err)
	}
	if got[3] != 0xFF {
		t.Errorf("内容が違う: %v", got)
	}
}

// peekEmu はエミュレーションゴルーチン越しにメモリを読む。
func peekEmu(e *Emulator, addr uint16) uint8 {
	var v uint8
	e.WithMachine(func(n *nes.NES) { v = n.Peek(addr) })
	return v
}

// fakeBatteryCart は不揮発メモリだけを持つ模擬のカートリッジ。
type fakeBatteryCart struct {
	cart.Cartridge
	ram []uint8
}

func (c *fakeBatteryCart) BatteryRAM() []uint8 { return c.ram }

func (c *fakeBatteryCart) SetBatteryRAM(data []uint8) error {
	copy(c.ram, data)
	return nil
}
