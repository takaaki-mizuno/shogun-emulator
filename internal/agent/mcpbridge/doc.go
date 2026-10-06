// Package mcpbridge は Agent Interface の MCP ブリッジ（shogun mcp）を持つ
// （設計書 14 編 §14.5.2、docs/adr/0001-jsonrpc-core-with-mcp-bridge.md）。
//
// MCP の stdio サーバとして動き、ツールの呼び出しを JSON-RPC の要求に変換して
// 送る。internal/agent を直接参照せず、internal/agent/rpc のクライアントだけを
// 使う。MCP の経路と JSON-RPC の経路で挙動が分かれないようにするためである。
// MCP SDK を参照するのはこのパッケージだけとする（internal/arch で検査する）。
package mcpbridge
