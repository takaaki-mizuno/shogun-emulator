package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
)

// Client は JSON-RPC のクライアント。
//
// 要求と応答を id で対応付けるため、同時に複数の要求を出せる。
type Client struct {
	nc net.Conn
	w  *lineWriter

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *Response
	err     error

	// onNotify はサーバからの通知を受け取る。nil のとき捨てる。
	onNotify func(method string, params json.RawMessage)
	done     chan struct{}
}

// ErrClosed は接続が閉じていることを表す。
var ErrClosed = errors.New("rpc: 接続が閉じている")

// NewClient は nc の上にクライアントを作り、受信を始める。
// onNotify は通知の受け取り先。nil でもよい。
func NewClient(nc net.Conn, onNotify func(method string, params json.RawMessage)) *Client {
	c := &Client{
		nc: nc, w: &lineWriter{w: nc}, pending: map[int64]chan *Response{},
		onNotify: onNotify, done: make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Connect は発見ファイルの接続先へつなぎ、session.hello を済ませる。
func Connect(ctx context.Context, d Discovery, client string, onNotify func(string, json.RawMessage)) (*Client, agent.HelloResult, error) {
	nc, err := Dial(d.Endpoint)
	if err != nil {
		return nil, agent.HelloResult{}, err
	}
	c := NewClient(nc, onNotify)
	hello, err := c.Hello(ctx, d.Token, client)
	if err != nil {
		c.Close()
		return nil, agent.HelloResult{}, err
	}
	return c, hello, nil
}

// InProcess はプロセス内の Host に net.Pipe でつないだクライアントを作る。
// shogun run と shogun mcp が使う（設計書 14 編 §14.2.1）。返す関数で閉じる。
func InProcess(ctx context.Context, host *agent.Host, client string) (*Client, func(), error) {
	token, err := NewToken()
	if err != nil {
		return nil, nil, err
	}
	srv := NewServer(host, token)
	a, b := net.Pipe()
	served := make(chan struct{})
	go func() {
		defer close(served)
		srv.ServeConn(a)
	}()
	c := NewClient(b, nil)
	closeFn := func() {
		c.Close()
		srv.Close()
		<-served
	}
	if _, err := c.Hello(ctx, token, client); err != nil {
		closeFn()
		return nil, nil, err
	}
	return c, closeFn, nil
}

// Done は接続が閉じたときに閉じるチャネルを返す。
func (c *Client) Done() <-chan struct{} { return c.done }

// ErrorKind はサーバが返したエラーの種類と説明を返す。サーバのエラーで
// なければ ok は false。agent を参照しないパッケージ（MCP ブリッジ）が使う。
func ErrorKind(err error) (kind, message string, ok bool) {
	var ae *agent.Error
	if errors.As(err, &ae) {
		return ae.Kind, ae.Message, true
	}
	return "", "", false
}

// SetNotify は通知の受け取り先を差し替える。
func (c *Client) SetNotify(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onNotify = fn
}

// Hello は session.hello を送る。
func (c *Client) Hello(ctx context.Context, token, client string) (agent.HelloResult, error) {
	var res agent.HelloResult
	err := c.Call(ctx, "session.hello", map[string]any{
		"token": token, "client": client, "api_version": agent.APIVersion,
	}, &res)
	return res, err
}

// Call は要求を送り、応答を result へ読む。result が nil なら読まない。
//
// サーバがエラーを返したときは *agent.Error を返す。
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	raw, err := c.CallRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

// CallRaw は要求を送り、結果の JSON をそのまま返す。
func (c *Client) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var p json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		p = data
	}
	ch := make(chan *Response, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	req := Request{JSONRPC: "2.0", ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: p}
	if err := c.w.write(req); err != nil {
		c.forget(id)
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, c.closedErr()
		}
		if resp.Error != nil {
			return nil, resp.Error.AgentError()
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	}
}

// CallCancelable は要求を送り、結果を待つ。ctx が取り消されたら、この要求だけを
// exec.cancel（id の指定つき）で取り消し、止まった結果を待って返す。MCP の
// キャンセル通知を JSON-RPC へ伝えるために使う（設計書 14 編 §14.5.2）。
func (c *Client) CallCancelable(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var p json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		p = data
	}
	ch := make(chan *Response, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.w.write(Request{JSONRPC: "2.0", ID: rawID, Method: method, Params: p}); err != nil {
		c.forget(id)
		return nil, err
	}
	var resp *Response
	select {
	case resp = <-ch:
	case <-ctx.Done():
		_ = c.Call(context.Background(), "exec.cancel", map[string]any{"id": rawID}, nil)
		resp = <-ch
	}
	if resp == nil {
		return nil, c.closedErr()
	}
	if resp.Error != nil {
		return nil, resp.Error.AgentError()
	}
	return resp.Result, nil
}

// Close は接続を閉じる。
func (c *Client) Close() error {
	err := c.nc.Close()
	<-c.done
	return err
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *Client) closedErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return ErrClosed
}

// readLoop は応答と通知を受け取る。接続が閉じたら待っている要求に
// nil を渡して起こす。
func (c *Client) readLoop() {
	defer close(c.done)
	r := newLineReader(c.nc)
	for {
		line, err := r.next()
		if err != nil {
			c.mu.Lock()
			c.err = ErrClosed
			pend := c.pending
			c.pending = map[int64]chan *Response{}
			c.mu.Unlock()
			for _, ch := range pend {
				ch <- nil
			}
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *ErrorObject    `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if msg.Method != "" && len(msg.ID) == 0 {
			c.mu.Lock()
			fn := c.onNotify
			c.mu.Unlock()
			if fn != nil {
				fn(msg.Method, msg.Params)
			}
			continue
		}
		id, err := strconv.ParseInt(string(msg.ID), 10, 64)
		if err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- &Response{ID: msg.ID, Result: msg.Result, Error: msg.Error}
		}
	}
}
