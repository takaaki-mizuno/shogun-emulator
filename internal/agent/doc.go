// Package agent は AI エージェントやプログラムがエミュレータを操作・観測・
// デバッグする窓口（Agent Interface）の中核を持つ（設計書 14 編）。
//
// Agent Command の登録簿、Instance を管理する Host、Control を置く。
// Transport（JSON-RPC・MCP・CLI）はこのパッケージの外にあり、すべて
// Host.Dispatch を通して Agent Command を実行する。
//
// GUI を参照しない。headless の Host と GUI 版の Host で同じコードを使う
// ためである（設計書 14 編 §14.2.2）。
package agent
