package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
)

// agentTestUI は ROM を読み込んだ画面を作り、Agent Interface を有効にする。
// 発見ファイルは一時ディレクトリに置く。
func agentTestUI(t *testing.T) (*UI, *rpc.Client) {
	t.Helper()
	test.NewApp()
	u := newTestUI(t)
	u.version = "test"
	u.app = test.NewApp()
	u.bannerObject()
	u.agentUI.cacheDir = shortDir(t)
	u.cfg.Agent.Listen = "unix"

	prg := make([]uint8, 32*1024)
	copy(prg, []uint8{0x4C, 0x00, 0x80}) // JMP $8000
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	rom := filepath.Join(t.TempDir(), "loop.nes")
	if err := os.WriteFile(rom, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := u.emu.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	u.SetAgentEnabled(true, false)
	if !u.AgentEnabled() {
		t.Fatal("有効にできない")
	}
	t.Cleanup(func() { u.SetAgentEnabled(false, false) })
	d, err := rpc.FindDiscovery(u.agentUI.cacheDir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "gui" {
		t.Errorf("発見ファイルの種類 = %q", d.Kind)
	}
	c, _, err := rpc.Connect(context.Background(), d, "claude-code", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return u, c
}

// shortDir は Unix ドメインソケットのパスの上限に収まる一時ディレクトリを作る。
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shgui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s を待てない", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAgentDisabledByDefault は既定では待ち受けないことを確かめる。
func TestAgentDisabledByDefault(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)
	if u.cfg.Agent.Enabled || u.AgentEnabled() {
		t.Error("既定で有効になっている")
	}
}

// TestAgentControlBanner は Control の取得でバナーが出て Agent-Paced になり、
// ゲームの入力キーで取り返すと Real-Time に戻り、control_changed が届くことを
// 確かめる（フェーズ 17 の完了判定）。
func TestAgentControlBanner(t *testing.T) {
	u, c := agentTestUI(t)
	ctx := context.Background()
	u.refreshAgent()
	if !strings.Contains(u.status.agentNote, "AI 接続中（1）") || u.agentUI.banner.Visible() {
		t.Errorf("Control を持たない接続の表示 = %q、バナー %v", u.status.agentNote, u.agentUI.banner.Visible())
	}
	if err := c.Call(ctx, "control.acquire", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "一時停止", func() bool { return u.emu.Status().Paused })
	u.refreshAgent()
	if !u.agentUI.banner.Visible() || !strings.Contains(u.agentUI.bannerLabel.Text, "claude-code") {
		t.Errorf("バナー = %v %q", u.agentUI.banner.Visible(), u.agentUI.bannerLabel.Text)
	}
	// ホットキー（Space の一時停止）では取り返さない。ゲームの入力キー（X は 1P の A）で
	// 取り返す。
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeySpace}) // 一時停止のホットキー
	if !u.agentHasControl() {
		t.Error("ホットキーで取り返した")
	}
	if !u.emu.Status().Paused || !strings.Contains(u.status.message, "無視") {
		t.Errorf("AI の操作中に一時停止のホットキーが効いた: paused %v、知らせ %q", u.emu.Status().Paused, u.status.message)
	}
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyX})
	if u.agentHasControl() {
		t.Fatal("ゲームの入力キーで取り返せない")
	}
	waitUntil(t, "Real-Time で走る", func() bool { return !u.emu.Status().Paused })
	u.refreshAgent()
	if u.agentUI.banner.Visible() {
		t.Error("取り返した後もバナーが出ている")
	}
	var r agent.PollResult
	if err := c.Call(ctx, "events.poll", map[string]any{"kinds": []string{"control_changed"}}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 {
		t.Fatalf("control_changed = %+v", r.Events)
	}
	data, _ := json.Marshal(r.Events[1].Data)
	if !strings.Contains(string(data), `"owner":"human"`) || !strings.Contains(string(data), `"real_time"`) {
		t.Errorf("取り返した後のイベント = %s", data)
	}
}

// TestAgentControlLost は進行中に「取り返す」を押すと stop_reason: control_lost
// で返ることを確かめる。
func TestAgentControlLost(t *testing.T) {
	u, c := agentTestUI(t)
	ctx := context.Background()
	if err := c.Call(ctx, "control.acquire", nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan agent.Observation, 1)
	go func() {
		var ob agent.Observation
		_ = c.Call(ctx, "exec.run_until", map[string]any{"condition": "A == $42", "max_frames": 216000}, &ob)
		done <- ob
	}()
	time.Sleep(100 * time.Millisecond)
	u.takeBackFromAgent()
	select {
	case ob := <-done:
		if ob.StopReason != agent.StopControlLost {
			t.Errorf("stop_reason = %q", ob.StopReason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取り返しても進行が終わらない")
	}
}

// TestAgentBreakpointNotice は Control を持たない AI がブレークポイントを
// 加えると、ステータスバーで知らせることを確かめる。
func TestAgentBreakpointNotice(t *testing.T) {
	u, c := agentTestUI(t)
	if err := c.Call(context.Background(), "debug.bp.add", map[string]any{"kind": "exec", "loc": "$9000"}, nil); err != nil {
		t.Fatal(err)
	}
	u.refreshAgent()
	if !strings.Contains(u.status.message, "ブレークポイント") {
		t.Errorf("知らせ = %q", u.status.message)
	}
}

// TestAgentDisableRemovesFiles は無効にすると接続を切り、発見ファイルと
// ソケットファイルを消すことを確かめる。
func TestAgentDisableRemovesFiles(t *testing.T) {
	u, c := agentTestUI(t)
	disc := u.AgentDiscovery()
	d, _ := rpc.FindDiscovery(u.agentUI.cacheDir, os.Getpid())
	sock := strings.TrimPrefix(d.Endpoint, "unix:")
	u.SetAgentEnabled(false, false)
	for _, p := range []string{disc, sock} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s が残る", p)
		}
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Error("接続が切れない")
	}
	if lines := u.agentClientLines(); len(lines) != 1 || !strings.Contains(lines[0], "許可していない") {
		t.Errorf("一覧 = %v", lines)
	}
}

// TestAgentROMChangedNotify は既定の agent.romWatchAction（notify）で、ROM の
// 変化をステータスバーで知らせるだけにすることを確かめる。
func TestAgentROMChangedNotify(t *testing.T) {
	u, _ := agentTestUI(t)
	before := u.emu.Status().Frames
	u.agentUI.romChanged.Store(true)
	u.refreshAgent()
	if !strings.Contains(u.status.message, "ROM ファイルが変わった") {
		t.Errorf("知らせ = %q", u.status.message)
	}
	if u.emu.Status().Frames < before {
		t.Error("知らせるだけのはずが読み直した")
	}
}
