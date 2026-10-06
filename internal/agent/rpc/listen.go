package rpc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// maxSocketPath は Unix ドメインソケットのパスの上限（バイト）。macOS の
// sockaddr_un の sun_path は 104 バイトで、終端の NUL を含む。
const maxSocketPath = 103

// DirName はキャッシュディレクトリの下の Agent Interface の置き場所
// （設計書 11 編 §11.2）。
const DirName = "agent"

// NewToken は 32 バイトの乱数から 16 進 64 文字のトークンを作る。
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Listen は spec（unix、tcp:127.0.0.1:PORT、tcp:[::1]:PORT）で待ち受ける。
// dir は Unix ドメインソケットを置くディレクトリ。返す endpoint は
// 発見ファイルに書く形（unix:/path、tcp:127.0.0.1:PORT）である。
func Listen(spec, dir string) (net.Listener, string, error) {
	if !config.ValidAgentListen(spec) {
		return nil, "", fmt.Errorf("rpc: 待ち受け先 %q は使えない（unix、tcp:127.0.0.1:PORT、tcp:[::1]:PORT）", spec)
	}
	if spec == config.AgentListenUnix {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", err
		}
		path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".sock")
		if len(path) > maxSocketPath {
			return nil, "", fmt.Errorf("rpc: ソケットのパスが %d バイトを超える（%s）。キャッシュディレクトリを短い場所にするか tcp:127.0.0.1:0 を使う", maxSocketPath, path)
		}
		// 前回の異常終了で残ったソケットファイルを消す。
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
		l, err := net.Listen("unix", path)
		if err != nil {
			return nil, "", err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			l.Close()
			return nil, "", err
		}
		return l, "unix:" + path, nil
	}
	addr := strings.TrimPrefix(spec, "tcp:")
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return l, "tcp:" + l.Addr().String(), nil
}

// Dial は endpoint（unix:/path、tcp:HOST:PORT）へ接続する。
func Dial(endpoint string) (net.Conn, error) {
	if p, ok := strings.CutPrefix(endpoint, "unix:"); ok {
		return net.DialTimeout("unix", p, 5*time.Second)
	}
	if a, ok := strings.CutPrefix(endpoint, "tcp:"); ok {
		return net.DialTimeout("tcp", a, 5*time.Second)
	}
	return nil, fmt.Errorf("rpc: 接続先 %q の形が分からない", endpoint)
}

// Running は待ち受け中のサーバ。
type Running struct {
	Server    *Server
	Endpoint  string
	Token     string
	Discovery string // 発見ファイルのパス

	listener net.Listener
	disc     Discovery
	sockPath string
	done     chan struct{}
}

// Start は待ち受けを始め、発見ファイルを書く（設計書 14 編 §14.5.1）。
// cacheDir はキャッシュディレクトリ。
func Start(host *agent.Host, listen, cacheDir, rom string) (*Running, error) {
	dir := filepath.Join(cacheDir, DirName)
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	l, endpoint, err := Listen(listen, dir)
	if err != nil {
		return nil, err
	}
	r := &Running{
		Server: NewServer(host, token), Endpoint: endpoint, Token: token,
		listener: l, done: make(chan struct{}),
	}
	if p, ok := strings.CutPrefix(endpoint, "unix:"); ok {
		r.sockPath = p
	}
	r.disc = Discovery{
		PID: os.Getpid(), Endpoint: endpoint, Token: token, Kind: string(host.Kind()),
		ROM: rom, Started: time.Now().Format(time.RFC3339),
	}
	r.Discovery, err = WriteDiscovery(dir, r.disc)
	if err != nil {
		l.Close()
		r.removeSocket()
		return nil, err
	}
	go func() {
		defer close(r.done)
		_ = r.Server.Serve(l)
	}()
	return r, nil
}

// UpdateROM は発見ファイルの rom を書き直す。ROM を読み込むたびに呼ぶ。
func (r *Running) UpdateROM(rom string) error {
	r.disc.ROM = rom
	_, err := WriteDiscovery(filepath.Dir(r.Discovery), r.disc)
	return err
}

// Close は待ち受けを止め、発見ファイルとソケットファイルを消す。
func (r *Running) Close() {
	r.Server.Close()
	<-r.done
	_ = os.Remove(r.Discovery)
	r.removeSocket()
}

func (r *Running) removeSocket() {
	if r.sockPath != "" {
		_ = os.Remove(r.sockPath)
	}
}
