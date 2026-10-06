package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// asm はテスト用 ROM を組み立てる小さな組み立て器。ラベルの位置を後から
// 埋める。
type asm struct {
	org    int
	b      []uint8
	labels map[string]int
	fixups []fixup
}

type fixup struct {
	at    int
	label string
	rel   bool
}

func newAsm(org int) *asm { return &asm{org: org, labels: map[string]int{}} }

func (a *asm) pc() int           { return a.org + len(a.b) }
func (a *asm) label(name string) { a.labels[name] = a.pc() }
func (a *asm) op(b ...uint8)     { a.b = append(a.b, b...) }
func (a *asm) abs(op uint8, l string) {
	a.fixups = append(a.fixups, fixup{len(a.b) + 1, l, false})
	a.op(op, 0, 0)
}
func (a *asm) rel(op uint8, l string) {
	a.fixups = append(a.fixups, fixup{len(a.b) + 1, l, true})
	a.op(op, 0)
}

// done はラベルを埋めた機械語を返す。
func (a *asm) done(t *testing.T) []uint8 {
	t.Helper()
	for _, f := range a.fixups {
		target, ok := a.labels[f.label]
		if !ok {
			t.Fatalf("ラベル %s が無い", f.label)
		}
		if f.rel {
			off := target - (a.org + f.at + 1)
			if off < -128 || off > 127 {
				t.Fatalf("分岐 %s が遠すぎる", f.label)
			}
			a.b[f.at] = uint8(int8(off))
			continue
		}
		a.b[f.at], a.b[f.at+1] = uint8(target), uint8(target>>8)
	}
	return a.b
}

// nmiROM はテスト用 ROM のラベルの位置。
var nmiROM map[string]int

// writeNMIROM は Agent Interface の進行と観測の検証に使う NROM の ROM を書く。
//
//	reset: VBlank を 2 回待ち（PPU の起動直後は $2000 への書き込みが効かない）、
//	       OAM の写し $0200 を $FF で埋め、スプライト 0 を (X $40, Y $50,
//	       タイル $21, 属性 $01) に置き、NMI を有効にする
//	loop:  JMP loop
//	nmi:   INC $10（フレームの数）
//	       コントローラ 1 を読み、$11 へ（bit 7 が A、bit 0 が Right）
//	       $2005 に 0 を 2 回書き、$4014 に $02 を書いて OAM DMA
//	       RTI
func writeNMIROM(t *testing.T) string {
	t.Helper()
	a := newAsm(0x8000)
	a.label("reset")
	a.op(0x78, 0xD8, 0xA2, 0xFF, 0x9A) // SEI; CLD; LDX #$FF; TXS
	a.label("vw1")
	a.abs(0x2C, "ppustatus") // BIT $2002
	a.rel(0x10, "vw1")       // BPL vw1
	a.label("vw2")
	a.abs(0x2C, "ppustatus")
	a.rel(0x10, "vw2")
	a.op(0xA9, 0xFF, 0xA2, 0x00) // LDA #$FF; LDX #0
	a.label("clear")
	a.op(0x9D, 0x00, 0x02, 0xE8)       // STA $0200,X; INX
	a.rel(0xD0, "clear")               // BNE clear
	a.op(0xA9, 0x50, 0x8D, 0x00, 0x02) // LDA #$50; STA $0200
	a.op(0xA9, 0x21, 0x8D, 0x01, 0x02) // LDA #$21; STA $0201
	a.op(0xA9, 0x01, 0x8D, 0x02, 0x02) // LDA #$01; STA $0202
	a.op(0xA9, 0x40, 0x8D, 0x03, 0x02) // LDA #$40; STA $0203
	a.op(0xA9, 0x80, 0x8D, 0x00, 0x20) // LDA #$80; STA $2000
	a.label("loop")
	a.abs(0x4C, "loop") // JMP loop
	a.label("nmi")
	a.op(0xE6, 0x10) // INC $10
	a.label("after_inc")
	a.op(0xA9, 0x01, 0x8D, 0x16, 0x40) // LDA #1; STA $4016
	a.op(0xA9, 0x00, 0x8D, 0x16, 0x40) // LDA #0; STA $4016
	a.op(0xA2, 0x08)                   // LDX #8
	a.label("read")
	a.op(0xAD, 0x16, 0x40, 0x4A, 0x26, 0x11, 0xCA)       // LDA $4016; LSR A; ROL $11; DEX
	a.rel(0xD0, "read")                                  // BNE read
	a.op(0xA9, 0x00, 0x8D, 0x05, 0x20, 0x8D, 0x05, 0x20) // LDA #0; STA $2005; STA $2005
	a.op(0xA9, 0x02, 0x8D, 0x14, 0x40)                   // LDA #2; STA $4014
	a.label("rti")
	a.op(0x40) // RTI
	a.labels["ppustatus"] = 0x2002
	code := a.done(t)
	nmiROM = a.labels

	prg := make([]uint8, 32*1024)
	copy(prg, code)
	put := func(at int, l string) { prg[at], prg[at+1] = uint8(a.labels[l]), uint8(a.labels[l]>>8) }
	put(0x7FFA, "nmi")
	put(0x7FFC, "reset")
	put(0x7FFE, "rti")
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "nmi.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// romAddr はテスト用 ROM のラベルの位置を "$8028" の形で返す。
func romAddr(l string) string { return hex16(uint16(nmiROM[l])) }

// agentSession は 1 つの接続から Agent Command を送るための補助。
type agentSession struct {
	t    *testing.T
	h    *Host
	conn *Conn
}

// newSession は headless の Host と接続を作り、NMI の ROM を読み込んだ
// Instance を作る。
func newSession(t *testing.T) *agentSession {
	t.Helper()
	h := newTestHost(t, KindServe)
	s := &agentSession{t: t, h: h, conn: h.Connect()}
	s.must("instance.create", map[string]any{"rom": writeNMIROM(t), "deterministic": true}, nil)
	return s
}

func (s *agentSession) call(method string, params any, v any) *Error {
	s.t.Helper()
	return call(s.t, s.h, s.conn, method, params, v)
}

func (s *agentSession) must(method string, params any, v any) {
	s.t.Helper()
	mustCall(s.t, s.h, s.conn, method, params, v)
}

// raw は結果を JSON のまま返す。
func (s *agentSession) raw(method string, params any) json.RawMessage {
	s.t.Helper()
	var p json.RawMessage
	if params != nil {
		p, _ = json.Marshal(params)
	}
	res, err := s.h.Dispatch(context.Background(), s.conn, method, p)
	if err != nil {
		s.t.Fatalf("%s: %v", method, err)
	}
	data, merr := json.Marshal(res)
	if merr != nil {
		s.t.Fatal(merr)
	}
	return data
}

// readByte は CPU アドレスの 1 バイトを mem.read で読む。
func (s *agentSession) readByte(loc string) int {
	s.t.Helper()
	var r struct {
		Bytes []int `json:"bytes"`
	}
	s.must("mem.read", map[string]any{"loc": loc}, &r)
	return r.Bytes[0]
}
