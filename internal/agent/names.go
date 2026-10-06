package agent

import "strings"

// 名前の変換（設計書 14 編 §14.5.4）。
//
// JSON-RPC の名前はドット区切りで、小文字の英字・数字・アンダースコア・
// ドットだけからなる。MCP の名前はドットをアンダースコアに置き換えたもの、
// CLI の語の並びはドットで分けてアンダースコアをハイフンにしたものとする。

// ValidName は Agent Command の名前に使える文字だけからなり、空の区切りを
// 含まないかを返す。
func ValidName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// MCPName は JSON-RPC の名前を MCP のツール名にする。
//
// 名前にアンダースコアを含むため、逆向きは文字の置き換えでは戻せない。
// 戻すときは Registry.LookupMCP で登録簿を引く。
func MCPName(name string) string { return strings.ReplaceAll(name, ".", "_") }

// CLIWords は JSON-RPC の名前を CLI の語の並びにする。
func CLIWords(name string) []string {
	words := strings.Split(name, ".")
	for i, w := range words {
		words[i] = strings.ReplaceAll(w, "_", "-")
	}
	return words
}

// FromCLIWords は CLI の語の並びを JSON-RPC の名前にする。
func FromCLIWords(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.ReplaceAll(w, "-", "_")
	}
	return strings.Join(out, ".")
}
