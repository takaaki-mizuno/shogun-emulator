package rpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
)

// MaxMessageSize は 1 つのメッセージの上限（設計書 14 編 §14.5.1）。
const MaxMessageSize = 16 << 20

// Request は要求または通知。ID が無いものは通知である。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response は応答。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject は JSON-RPC のエラー。
type ErrorObject struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    *ErrorData `json:"data,omitempty"`
}

// ErrorData は error.data。種類と式の誤りの位置を入れる。
type ErrorData struct {
	Kind     string `json:"kind"`
	Position *int   `json:"position,omitempty"`
}

// toErrorObject は Agent Command のエラーを JSON-RPC のエラーにする。
func toErrorObject(e *agent.Error) *ErrorObject {
	d := &ErrorData{Kind: e.Kind}
	if e.Position >= 0 {
		pos := e.Position
		d.Position = &pos
	}
	return &ErrorObject{Code: e.Code, Message: e.Message, Data: d}
}

// AgentError は JSON-RPC のエラーを Agent Command のエラーに戻す。
func (o *ErrorObject) AgentError() *agent.Error {
	kind := agent.KindInternalError
	pos := -1
	if o.Data != nil {
		kind = o.Data.Kind
		if o.Data.Position != nil {
			pos = *o.Data.Position
		}
	}
	return &agent.Error{Code: o.Code, Kind: kind, Message: o.Message, Position: pos}
}

// errTooLarge は上限を超えたメッセージを受け取ったことを表す。
var errTooLarge = errors.New("rpc: メッセージが 16 MiB を超えた")

// lineReader は改行区切りのメッセージを読む。
type lineReader struct {
	r *bufio.Reader
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// next は次の 1 行を読む。空の行は読み飛ばす。上限を超えたら errTooLarge。
func (l *lineReader) next() ([]byte, error) {
	for {
		var buf []byte
		for {
			chunk, err := l.r.ReadSlice('\n')
			buf = append(buf, chunk...)
			if len(buf) > MaxMessageSize+1 {
				return nil, errTooLarge
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if err == io.EOF && len(bytes.TrimSpace(buf)) > 0 {
					return bytes.TrimSpace(buf), nil
				}
				return nil, err
			}
			break
		}
		line := bytes.TrimSpace(buf)
		if len(line) > 0 {
			return line, nil
		}
	}
}

// lineWriter は改行区切りのメッセージを書く。応答と通知が混ざらないよう
// 書き込みを直列化する。
type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// write は v を 1 行の JSON にして書く。
func (l *lineWriter) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(data)
	return err
}
