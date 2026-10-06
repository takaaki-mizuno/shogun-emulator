package agent

import (
	"slices"
	"sync"
)

// ConnID は接続の通し番号。1 から振る。
type ConnID uint64

// Conn は Transport の 1 つの接続。Control の持ち主や前回の観測を
// 接続ごとに持つために使う。
type Conn struct {
	ID ConnID

	mu     sync.Mutex
	client string
	authed bool
	// notify はサーバからの通知の送り先。Transport が設定する。待たずに戻る。
	notify func(method string, params any)
	// subscribed は events.subscribe したことを表し、kinds はその種類。
	subscribed bool
	kinds      []string
}

// SetNotifier は通知の送り先を設定する。Transport が接続ごとに呼ぶ。
// fn は待たずに戻ること（エミュレーションゴルーチンから呼ばれる）。
func (c *Conn) SetNotifier(fn func(method string, params any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = fn
}

func (c *Conn) subscribe(kinds []string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notify == nil {
		return false
	}
	c.subscribed, c.kinds = true, append([]string(nil), kinds...)
	return true
}

func (c *Conn) unsubscribe() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribed, c.kinds = false, nil
}

// notifyEvent は購読していればイベントを通知する。
func (c *Conn) notifyEvent(ev Event) {
	c.mu.Lock()
	fn, ok := c.notify, c.subscribed && (len(c.kinds) == 0 || slices.Contains(c.kinds, ev.Kind))
	c.mu.Unlock()
	if ok && fn != nil {
		fn("events.event", ev)
	}
}

// Client は session.hello で名乗った名前を返す。
func (c *Conn) Client() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// Authenticated は session.hello を済ませたかを返す。
func (c *Conn) Authenticated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authed
}

// SetAuthenticated は session.hello を済ませたことを記録する。
func (c *Conn) SetAuthenticated(client string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authed = true
	c.client = client
}
