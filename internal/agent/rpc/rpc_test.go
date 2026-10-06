package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newHost は headless の Host を作る。
func newHost(t *testing.T) *agent.Host {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	h := agent.NewHost(agent.Options{
		Kind: agent.KindServe, Server: "shogun test",
		EmuConfig: emu.Config{
			Emulation: cfg.Emulation, Input: cfg.Input, Paths: cfg.Paths, State: cfg.State,
			Movie: cfg.Movie, Debug: cfg.Debug,
			Dirs: config.Paths{Config: dir, Data: dir, Cache: dir, Logs: dir, Screenshots: dir},
		},
	})
	t.Cleanup(h.Close)
	return h
}

// rawConn はサーバへ net.Pipe でつないだ生の接続。行を読み書きする。
type rawConn struct {
	t  *testing.T
	nc net.Conn
	r  *bufio.Reader
}

func dialRaw(t *testing.T, h *agent.Host) *rawConn {
	t.Helper()
	srv := NewServer(h, testToken)
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeConn(a)
	}()
	t.Cleanup(func() {
		b.Close()
		srv.Close()
		<-done
	})
	return &rawConn{t: t, nc: b, r: bufio.NewReader(b)}
}

func (c *rawConn) send(line string) {
	c.t.Helper()
	c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.nc.Write([]byte(line + "\n")); err != nil {
		c.t.Fatalf("書けない: %v", err)
	}
}

// recv は応答を 1 つ読む。接続が閉じたら nil を返す。
func (c *rawConn) recv() *Response {
	c.t.Helper()
	c.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return nil
	}
	var r Response
	if err := json.Unmarshal(line, &r); err != nil {
		c.t.Fatalf("応答を解釈できない: %s", line)
	}
	return &r
}

// closed は接続が閉じられたかを返す。
func (c *rawConn) closed() bool {
	c.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := c.r.ReadByte()
	return err != nil
}

func hello(token string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"session.hello","params":{"token":"` + token + `","client":"test"}}`
}

func expectError(t *testing.T, r *Response, code int, kind string) {
	t.Helper()
	if r == nil {
		t.Fatalf("応答が無い（期待 %d %s）", code, kind)
	}
	if r.Error == nil || r.Error.Code != code || r.Error.Data == nil || r.Error.Data.Kind != kind {
		t.Fatalf("応答 = %+v %+v, 期待 %d %s", r, r.Error, code, kind)
	}
}

// TestRejectsWithoutToken はトークンなし・誤ったトークン・session.hello の前の
// 要求を断り、接続を切ることを確かめる（設計書 14 編 §14.24）。
func TestRejectsWithoutToken(t *testing.T) {
	h := newHost(t)
	cases := []struct{ name, line string }{
		{"hello の前の要求", `{"jsonrpc":"2.0","id":1,"method":"instance.list"}`},
		{"トークンなし", `{"jsonrpc":"2.0","id":1,"method":"session.hello","params":{}}`},
		{"誤ったトークン", hello(strings.Repeat("f", 64))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dialRaw(t, h)
			c.send(tc.line)
			expectError(t, c.recv(), -32001, agent.KindUnauthorized)
			if !c.closed() {
				t.Error("接続を切らない")
			}
		})
	}
}

// TestHelloAndCall は正しいトークンで要求を処理することを確かめる。
func TestHelloAndCall(t *testing.T) {
	c := dialRaw(t, newHost(t))
	c.send(hello(testToken))
	r := c.recv()
	if r == nil || r.Error != nil {
		t.Fatalf("hello の応答 = %+v", r)
	}
	var res agent.HelloResult
	if err := json.Unmarshal(r.Result, &res); err != nil || res.APIVersion != 1 || res.Kind != agent.KindServe {
		t.Errorf("hello の結果 = %s", r.Result)
	}
	c.send(`{"jsonrpc":"2.0","id":"x","method":"instance.list"}`)
	r = c.recv()
	if r == nil || r.Error != nil || string(r.ID) != `"x"` || string(r.Result) != "[]" {
		t.Errorf("instance.list の応答 = %+v", r)
	}
	// 通知には応答しない。次の要求の応答が先に来る。
	c.send(`{"jsonrpc":"2.0","method":"instance.list"}`)
	c.send(`{"jsonrpc":"2.0","id":3,"method":"instance.list"}`)
	if r = c.recv(); r == nil || string(r.ID) != "3" {
		t.Errorf("通知に応答した: %+v", r)
	}
	c.send(`{"jsonrpc":"2.0","id":4,"method":"session.hello","params":{"token":"` + testToken + `","api_version":2}}`)
	expectError(t, c.recv(), -32602, agent.KindInvalidParams)
}

// TestFraming は改行区切りの読み書きの誤りを確かめる。
func TestFraming(t *testing.T) {
	c := dialRaw(t, newHost(t))
	c.send(hello(testToken))
	c.recv()
	c.send(`{not json`)
	expectError(t, c.recv(), -32700, agent.KindParseError)
	c.send(`[{"jsonrpc":"2.0","id":1,"method":"instance.list"}]`)
	expectError(t, c.recv(), -32600, agent.KindInvalidRequest)
	c.send(`{"jsonrpc":"1.0","id":1,"method":"instance.list"}`)
	expectError(t, c.recv(), -32600, agent.KindInvalidRequest)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"no.such"}`)
	expectError(t, c.recv(), -32601, agent.KindMethodNotFound)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"instance.create","params":{"rom":1}}`)
	expectError(t, c.recv(), -32602, agent.KindInvalidParams)
	// 空の行は読み飛ばす。
	c.send(``)
	c.send(`{"jsonrpc":"2.0","id":9,"method":"instance.list"}`)
	if r := c.recv(); r == nil || string(r.ID) != "9" {
		t.Errorf("空の行の後の応答 = %+v", r)
	}
}

// TestOversizedMessage は 16 MiB を超えるメッセージで接続を切ることを確かめる。
func TestOversizedMessage(t *testing.T) {
	c := dialRaw(t, newHost(t))
	c.send(hello(testToken))
	c.recv()
	go func() {
		big := make([]byte, MaxMessageSize+256<<10)
		for i := range big {
			big[i] = 'a'
		}
		c.nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		c.nc.Write(big)
	}()
	r := c.recv()
	expectError(t, r, -32600, agent.KindInvalidRequest)
	if !c.closed() {
		t.Error("接続を切らない")
	}
}

// TestClientAndDisconnect はクライアントで呼べることと、切断で Control が
// 返ることを確かめる。
func TestClientAndDisconnect(t *testing.T) {
	h := newHost(t)
	ctx := context.Background()
	c, closeFn, err := InProcess(ctx, h, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "instance.create", map[string]any{"rom": writeROM(t)}, nil); err != nil {
		t.Fatal(err)
	}
	var st agent.ControlStatus
	if err := c.Call(ctx, "control.status", nil, &st); err != nil || st.Owner != "agent" || st.Client != "test" {
		t.Fatalf("control.status = %+v, %v", st, err)
	}
	err = c.Call(ctx, "no.such", nil, nil)
	var ae *agent.Error
	if !errors.As(err, &ae) || ae.Kind != agent.KindMethodNotFound {
		t.Errorf("誤りの戻り値 = %v", err)
	}
	closeFn()
	deadline := time.Now().Add(5 * time.Second)
	for h.Instances()[0].ControlStatus().Owner != "none" {
		if time.Now().After(deadline) {
			t.Fatal("切断しても Control が返らない")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.Call(ctx, "instance.list", nil, nil); err == nil {
		t.Error("閉じたクライアントで呼べた")
	}
}

// writeROM は最小の NROM の ROM を書く。
func writeROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	copy(prg, []uint8{0x4C, 0x00, 0x80}) // JMP $8000
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "loop.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestListenRejectsNonLoopback は 127.0.0.1 と ::1 以外での待ち受けを断る
// ことを確かめる。
func TestListenRejectsNonLoopback(t *testing.T) {
	for _, spec := range []string{"tcp:0.0.0.0:0", "tcp:192.168.1.10:0", "tcp::0", "tcp:localhost:0", "udp:127.0.0.1:0", ""} {
		if l, _, err := Listen(spec, t.TempDir()); err == nil {
			l.Close()
			t.Errorf("%q で待ち受けた", spec)
		}
	}
	l, ep, err := Listen("tcp:127.0.0.1:0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if !strings.HasPrefix(ep, "tcp:127.0.0.1:") {
		t.Errorf("endpoint = %q", ep)
	}
}

// shortTempDir は Unix ドメインソケットのパスの上限に収まる一時ディレクトリを作る。
// macOS の t.TempDir() は長すぎることがある。
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestStartWritesDiscovery は待ち受けと発見ファイル（0600）を確かめ、
// 発見ファイルから接続できることを確かめる。
func TestStartWritesDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows のパーミッションと Unix ドメインソケットは未確認")
	}
	cache := shortTempDir(t)
	h := newHost(t)
	r, err := Start(h, "unix", cache, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, p := range []string{r.Discovery, strings.TrimPrefix(r.Endpoint, "unix:")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s のパーミッション = %o, 期待 600", p, fi.Mode().Perm())
		}
	}
	d, err := FindDiscovery(cache, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Token != r.Token || d.Kind != "serve" || d.PID != os.Getpid() {
		t.Errorf("発見ファイル = %+v", d)
	}
	c, hello, err := Connect(context.Background(), d, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if hello.APIVersion != 1 {
		t.Errorf("hello = %+v", hello)
	}
	c.Close()
	if err := r.UpdateROM("game"); err != nil {
		t.Fatal(err)
	}
	if d, _ := FindDiscovery(cache, 0); d.ROM != "game" {
		t.Errorf("rom を書き直していない: %+v", d)
	}
	r.Close()
	if _, err := os.Stat(r.Discovery); !errors.Is(err, os.ErrNotExist) {
		t.Error("終了しても発見ファイルが残る")
	}
	if _, err := FindDiscovery(cache, 0); !errors.Is(err, ErrNoServer) {
		t.Errorf("終了後の発見 = %v", err)
	}
}

// TestFindDiscoveryRemovesStale はプロセスの無い発見ファイルを消し、gui を
// 優先することを確かめる。
func TestFindDiscoveryRemovesStale(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, DirName)
	stale, err := WriteDiscovery(dir, Discovery{PID: 999999, Endpoint: "unix:/nonexistent", Kind: "gui", Started: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	// 同じプロセスの発見ファイルを 2 つ作れないため、PID を変えずに種類だけを
	// 確かめる。
	if _, err := WriteDiscovery(dir, Discovery{PID: os.Getpid(), Endpoint: "unix:/x", Kind: "serve", Started: "2026-01-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	d, err := FindDiscovery(cache, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.PID != os.Getpid() {
		t.Errorf("選んだもの = %+v", d)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Error("プロセスの無い発見ファイルを消していない")
	}
	if _, err := FindDiscovery(cache, 12345678); err == nil {
		t.Error("無い PID を見つけた")
	}
}

// TestCancelRunsConcurrently は exec.cancel が先行する要求の完了を待たずに
// 処理され、先行する要求を取り消すことを確かめる（設計書 14 編 §14.2.4）。
func TestCancelRunsConcurrently(t *testing.T) {
	h := newHost(t)
	h.Registry().Register(agent.CommandSpec{
		Name: "test.block", Class: agent.ClassSession, Headless: true,
		Handler: func(c *agent.Context, _ json.RawMessage) (any, error) {
			<-c.Ctx.Done()
			return nil, agent.Errorf(agent.KindCancelled, "取り消された")
		},
	})
	c := dialRaw(t, h)
	c.send(hello(testToken))
	c.recv()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"test.block"}`)
	c.send(`{"jsonrpc":"2.0","id":2,"method":"exec.cancel"}`)
	got := map[string]*Response{}
	for range 2 {
		r := c.recv()
		if r == nil {
			t.Fatal("応答が無い")
		}
		got[string(r.ID)] = r
	}
	if r := got["2"]; r == nil || r.Error != nil {
		t.Errorf("exec.cancel の応答 = %+v", r)
	}
	expectError(t, got["1"], -32011, agent.KindCancelled)
}

// TestExecCancelStopsRunUntil は run_until の実行中に別の要求として
// exec.cancel を送ると stop_reason: cancelled で返ることを確かめる
// （フェーズ 15 の完了判定）。
func TestExecCancelStopsRunUntil(t *testing.T) {
	h := newHost(t)
	ctx := context.Background()
	c, closeFn, err := InProcess(ctx, h, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if err := c.Call(ctx, "instance.create", map[string]any{"rom": writeROM(t)}, nil); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ob  agent.Observation
		err error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		r.err = c.Call(ctx, "exec.run_until", map[string]any{"condition": "A == $42", "max_frames": 216000}, &r.ob)
		done <- r
	}()
	time.Sleep(100 * time.Millisecond)
	if err := c.Call(ctx, "exec.cancel", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.ob.StopReason != agent.StopCancelled {
			t.Fatalf("結果 = %+v, %v", r.ob, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exec.cancel で止まらない")
	}
}
