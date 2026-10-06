package rpc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
)

// concurrentMethods は先行する要求の完了を待たずに処理する Agent Command
// （設計書 14 編 §14.2.4）。exec.run_until の実行中に止めるため、および
// イベントを取りに来るためである。
var concurrentMethods = []string{"exec.cancel", "events.poll"}

// Server は JSON-RPC のサーバ。
type Server struct {
	host  *agent.Host
	token string

	mu        sync.Mutex
	listeners []net.Listener
	conns     []net.Conn
	closed    bool
	wg        sync.WaitGroup
}

// NewServer はサーバを作る。token は session.hello で照合するトークン。
func NewServer(host *agent.Host, token string) *Server {
	return &Server{host: host, token: token}
}

// Serve は l で接続を受け付ける。受け付けゴルーチンとして呼ぶ。
// Close で l を閉じると戻る。
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		l.Close()
		return net.ErrClosed
	}
	s.listeners = append(s.listeners, l)
	s.mu.Unlock()
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.ServeConn(c)
		}()
	}
}

// Close は待ち受けとすべての接続を閉じ、接続ゴルーチンの終わりを待つ。
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	ls, cs := s.listeners, s.conns
	s.listeners, s.conns = nil, nil
	s.mu.Unlock()
	for _, l := range ls {
		l.Close()
	}
	for _, c := range cs {
		c.Close()
	}
	s.wg.Wait()
}

// DisconnectAll は待ち受けを続けたまま、すべての接続を切る（GUI の
// 「すべての接続を切る」）。切った接続が持っていた Control は返る。
func (s *Server) DisconnectAll() {
	s.mu.Lock()
	cs := slices.Clone(s.conns)
	s.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

// session は 1 つの接続の状態。
type session struct {
	s    *Server
	nc   net.Conn
	conn *agent.Conn
	w    *lineWriter
	ctx  context.Context

	// mu は waiting を守る。waiting は受け取ったまま終わっていない順の
	// 要求（処理中と順番待ち）の取り消し。
	mu      sync.Mutex
	waiting []*queued
	// dropped は送信キューからあふれて捨てた通知の数。
	dropped atomic.Uint64
}

// notifyQueueSize は接続ごとの通知の送信キューの大きさ。
const notifyQueueSize = 256

// queued は順に処理する要求。受け取った時点で取り消しを用意する。
// exec.cancel が、処理を始める前の要求も取り消せるようにするためである。
type queued struct {
	req    *Request
	ctx    context.Context
	cancel context.CancelFunc
}

// ServeConn は 1 つの接続を処理する。接続が閉じるまで戻らない。
//
// 1 つの接続の要求は届いた順に処理する。concurrentMethods だけは別の
// ゴルーチンで処理する。
func (s *Server) ServeConn(nc net.Conn) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		nc.Close()
		return
	}
	s.conns = append(s.conns, nc)
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	ss := &session{s: s, nc: nc, conn: s.host.Connect(), w: &lineWriter{w: nc}, ctx: ctx}
	// 通知は送信キューを通して書く。接続の書き込みが詰まっても、通知を積む
	// 側（エミュレーションゴルーチン）を止めないためである。
	notes := make(chan *Request, notifyQueueSize)
	notesDone := make(chan struct{})
	go func() {
		defer close(notesDone)
		for {
			select {
			case n := <-notes:
				if ss.w.write(n) != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	ss.conn.SetNotifier(func(method string, params any) {
		data, err := json.Marshal(params)
		if err != nil {
			return
		}
		n := &Request{JSONRPC: "2.0", Method: method, Params: data}
		for {
			select {
			case notes <- n:
				return
			default:
			}
			// あふれた。古い通知を捨てる。
			select {
			case <-notes:
				ss.dropped.Add(1)
			default:
			}
		}
	})
	var side sync.WaitGroup
	defer func() {
		cancel()
		<-notesDone
		nc.Close()
		side.Wait()
		s.host.Disconnect(ss.conn)
		s.mu.Lock()
		s.conns = slices.DeleteFunc(s.conns, func(x net.Conn) bool { return x == nc })
		s.mu.Unlock()
	}()

	queue := make(chan *queued, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for q := range queue {
			ok := ss.handle(q.req, q.ctx)
			ss.finish(q)
			if !ok {
				// 認証に失敗した。接続を切る。
				nc.Close()
				for range queue {
				}
				return
			}
		}
	}()
	defer func() {
		close(queue)
		<-done
	}()

	r := newLineReader(nc)
	for {
		line, err := r.next()
		if err != nil {
			if errors.Is(err, errTooLarge) {
				_ = ss.w.write(errorResponse(nil, agent.Errorf(agent.KindInvalidRequest, "メッセージが 16 MiB を超えた")))
			}
			return
		}
		req, eresp := parseRequest(line)
		if eresp != nil {
			if ss.w.write(eresp) != nil {
				return
			}
			continue
		}
		if slices.Contains(concurrentMethods, req.Method) && ss.conn.Authenticated() {
			side.Add(1)
			if req.Method == "exec.cancel" {
				// それより前に受け取った要求を、処理中か順番待ちかによらず
				// 取り消す。id を指定したときはその要求だけを取り消す。
				var p struct {
					ID json.RawMessage `json:"id"`
				}
				_ = json.Unmarshal(req.Params, &p)
				ss.cancelWaiting(p.ID)
			}
			go func() {
				defer side.Done()
				ss.handle(req, ss.ctx)
			}()
			continue
		}
		q := ss.enqueue(req)
		select {
		case queue <- q:
		case <-done:
			ss.finish(q)
			return
		}
	}
}

// parseRequest は 1 行を要求として読む。誤りのときは返す応答を作る。
func parseRequest(line []byte) (*Request, *Response) {
	if line[0] == '[' {
		// バッチ要求は受け付けない（設計書 14 編 §14.5.1）。
		return nil, errorResponse(nil, agent.Errorf(agent.KindInvalidRequest, "バッチ要求には対応していない"))
	}
	var req Request
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&req); err != nil {
		return nil, errorResponse(nil, agent.Errorf(agent.KindParseError, "JSON を解釈できない: %v", err))
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return nil, errorResponse(req.ID, agent.Errorf(agent.KindInvalidRequest, "jsonrpc は \"2.0\"、method は空でない文字列とする"))
	}
	return &req, nil
}

// enqueue は順に処理する要求の取り消しを用意して登録する。
func (ss *session) enqueue(req *Request) *queued {
	ctx, cancel := context.WithCancel(ss.ctx)
	q := &queued{req: req, ctx: ctx, cancel: cancel}
	ss.mu.Lock()
	ss.waiting = append(ss.waiting, q)
	ss.mu.Unlock()
	return q
}

// finish は終わった要求を外す。
func (ss *session) finish(q *queued) {
	q.cancel()
	ss.mu.Lock()
	ss.waiting = slices.DeleteFunc(ss.waiting, func(x *queued) bool { return x == q })
	ss.mu.Unlock()
}

// cancelWaiting は受け取ったまま終わっていない要求を取り消す。id が空なら
// すべて、空でなければその id の要求だけを取り消す。
func (ss *session) cancelWaiting(id json.RawMessage) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for _, q := range ss.waiting {
		if len(id) == 0 || string(id) == "null" || bytes.Equal(bytes.TrimSpace(id), bytes.TrimSpace(q.req.ID)) {
			q.cancel()
		}
	}
}

// handle は要求を 1 つ処理して応答を書く。接続を切るべきとき false を返す。
func (ss *session) handle(req *Request, ctx context.Context) bool {
	isNotify := len(req.ID) == 0
	if !ss.conn.Authenticated() {
		if req.Method != "session.hello" {
			ss.reply(req, isNotify, nil, agent.Errorf(agent.KindUnauthorized, "最初に session.hello を送る"))
			return false
		}
		if !ss.tokenMatches(req.Params) {
			ss.reply(req, isNotify, nil, agent.Errorf(agent.KindUnauthorized, "トークンが一致しない"))
			return false
		}
	}

	res, aerr := ss.s.host.Dispatch(ctx, ss.conn, req.Method, req.Params)
	ss.reply(req, isNotify, res, aerr)
	return true
}

// tokenMatches はトークンを照合する。時間で中身を推し量られないよう
// 一定時間の比較を使う。
func (ss *session) tokenMatches(params json.RawMessage) bool {
	var p struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(params, &p) != nil || p.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(p.Token), []byte(ss.s.token)) == 1
}

// reply は応答を書く。通知には応答しない。
func (ss *session) reply(req *Request, isNotify bool, res any, aerr *agent.Error) {
	if isNotify {
		return
	}
	if aerr != nil {
		_ = ss.w.write(errorResponse(req.ID, aerr))
		return
	}
	data, err := json.Marshal(res)
	if err != nil {
		_ = ss.w.write(errorResponse(req.ID, agent.Errorf(agent.KindInternalError, "結果を JSON にできない: %v", err)))
		return
	}
	_ = ss.w.write(Response{JSONRPC: "2.0", ID: req.ID, Result: data})
}

// errorResponse はエラーの応答を作る。id が分からないときは null とする。
func errorResponse(id json.RawMessage, e *agent.Error) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: "2.0", ID: id, Error: toErrorObject(e)}
}
