package scenario

import "strings"

// operands は assert の式を && と || で項に分け、比較を含む項の両辺のうち
// リテラルでないものを返す。失敗したとき実際の値を示すために使う
// （「game.mode == 'title'」なら game.mode の値を示す）。
func operands(expr string) []string {
	var out []string
	for _, term := range splitTop(expr, []string{"&&", "||"}) {
		term = trimParens(strings.TrimSpace(term))
		sides := splitTop(term, []string{"==", "!=", "<=", ">=", "<", ">"})
		if len(sides) != 2 {
			if !isLiteral(term) && term != "" {
				out = append(out, term)
			}
			continue
		}
		for _, s := range sides {
			s = strings.TrimSpace(s)
			if !isLiteral(s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// splitTop は括弧・角括弧・引用符の外にある区切りで分ける。区切りは長い
// ものから順に試す（<= を < より先に）。シフト（<< と >>）は区切りとしない。
func splitTop(s string, seps []string) []string {
	var parts []string
	depth, start := 0, 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\'' {
				inStr = false
			}
			continue
		}
		switch c {
		case '\'':
			inStr = true
			continue
		case '(', '[':
			depth++
			continue
		case ')', ']':
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if (c == '<' || c == '>') && (i+1 < len(s) && s[i+1] == c || i > 0 && s[i-1] == c) {
			continue
		}
		for _, sep := range seps {
			if strings.HasPrefix(s[i:], sep) {
				parts = append(parts, s[start:i])
				i += len(sep) - 1
				start = i + 1
				break
			}
		}
	}
	return append(parts, s[start:])
}

// trimParens は全体を囲む括弧を外す。
func trimParens(s string) string {
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		depth := 0
		whole := true
		for i := 0; i < len(s)-1; i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				whole = false
				break
			}
		}
		if !whole {
			return s
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// isLiteral は数か文字列のリテラルかを返す。
func isLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return true
	}
	digits := "0123456789"
	switch {
	case s[0] == '$':
		s, digits = s[1:], "0123456789abcdefABCDEF"
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		s, digits = s[2:], "0123456789abcdefABCDEF"
	case s[0] == '%':
		s, digits = s[1:], "01"
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(digits, rune(s[i])) {
			return false
		}
	}
	return true
}
