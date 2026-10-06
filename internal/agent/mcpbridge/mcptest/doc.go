// Package mcptest は MCP ブリッジの結合テストを持つ。
//
// テストは Host を作るために internal/agent を参照する。ブリッジ本体
// （internal/agent/mcpbridge）が internal/agent を直接参照しないという規則
// （設計書 14 編 §14.2.2）を、テストのインポートも含めて保つため、別の
// パッケージに置く。
package mcptest
