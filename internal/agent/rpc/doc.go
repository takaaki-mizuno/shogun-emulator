// Package rpc は Agent Interface の JSON-RPC 2.0 の Transport を持つ
// （設計書 14 編 §14.5.1）。
//
// 1 行に 1 つのメッセージを書く。待ち受けはローカルソケット（Unix ドメイン
// ソケット、代替として 127.0.0.1・::1 の TCP）に限り、接続ごとに
// session.hello のトークンを確かめる。MCP ブリッジと CLI もこのクライアントを
// 使う。
package rpc
