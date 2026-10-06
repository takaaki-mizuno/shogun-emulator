package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
)

// ctl の終了コード（設計書 11 編 §11.5.4）。
const (
	ctlOK          = 0
	ctlCommandErr  = 1
	ctlConnectFail = 2
)

// runCtl は起動中のエミュレータへ Agent Command を 1 つ送り、結果を表示する
// （設計書 14 編 §14.5.3）。
func runCtl(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var o subOptions
	fs := subFlagSet("ctl", &o, stderr)
	fs.IntVar(&o.pid, "pid", 0, "接続するプロセスの ID。省略すると GUI で最も新しいもの")
	fs.BoolVar(&o.text, "text", false, "結果を人が読む形で表示する")
	fs.StringVar(&o.out, "out", "", "結果の画像を書き出すパス")
	fs.StringVar(&o.json, "json", "", "引数を JSON で渡す。他の引数で上書きする")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ctlOK
		}
		return exitBadArgs
	}
	reg := agent.NewDefaultRegistry()
	rest := fs.Args()

	// 語の並びのうち、登録簿にある最も長いものを Agent Command とする。
	var words []string
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			break
		}
		words = append(words, a)
	}
	if len(words) == 2 && words[0] == "events" && words[1] == "watch" {
		return runEventsWatch(ctx, o, rest[2:], stdout, stderr)
	}
	var spec *agent.CommandSpec
	n := 0
	for k := len(words); k >= 1; k-- {
		if s, ok := reg.LookupCLI(words[:k]); ok {
			spec, n = s, k
			break
		}
	}
	wantHelp := o.help || slices.Contains(rest, "--help") || slices.Contains(rest, "-h")
	if spec == nil {
		if wantHelp || len(words) == 0 {
			printCtlHelp(stdout, reg, words)
			if len(words) == 0 && !wantHelp {
				return exitBadArgs
			}
			return ctlOK
		}
		fmt.Fprintf(stderr, "%s: Agent Command %q は無い\n", appName, strings.Join(words, " "))
		if cands := ctlCandidates(reg, words); len(cands) > 0 {
			fmt.Fprintf(stderr, "候補: %s\n", strings.Join(cands, ", "))
		}
		return exitBadArgs
	}
	if wantHelp {
		printCtlCommandHelp(stdout, spec)
		return ctlOK
	}
	params, err := ctlParams(spec, rest[n:], o.json)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return exitBadArgs
	}

	store, code, ok := subStore(o, stderr)
	if !ok {
		return code
	}
	d, err := rpc.FindDiscovery(store.Paths.Cache, o.pid)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return ctlConnectFail
	}
	c, _, err := rpc.Connect(ctx, d, "shogun-ctl", nil)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s へ接続できない: %v\n", appName, d.Endpoint, err)
		return ctlConnectFail
	}
	defer c.Close()

	raw, err := c.CallRaw(ctx, spec.Name, params)
	if err != nil {
		var ae *agent.Error
		if errors.As(err, &ae) {
			fmt.Fprintf(stderr, "エラー [%s]: %s\n", ae.Kind, ae.Message)
		} else {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
		return ctlCommandErr
	}
	if err := printCtlResult(stdout, raw, o.text, o.out); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return ctlCommandErr
	}
	return ctlOK
}

// ctlParams は CLI の引数を JSON-RPC の params にする（設計書 14 編 §14.5.3）。
//
// 引数の型は登録簿の JSON Schema で決める。スキーマに無い引数は、数値に
// 見えれば数値、true・false は真偽値、それ以外は文字列とする。
func ctlParams(spec *agent.CommandSpec, args []string, jsonArg string) (map[string]any, error) {
	params := map[string]any{}
	if jsonArg != "" {
		if err := json.Unmarshal([]byte(jsonArg), &params); err != nil {
			return nil, fmt.Errorf("--json を解釈できない: %v", err)
		}
	}
	schema := spec.Schema()
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		name = strings.ReplaceAll(name, "-", "_")
		if name == "json" {
			// 名前空間の後ろに書いた --json も受け付け、それまでの引数に重ねる。
			if !hasVal && i+1 < len(args) {
				val = args[i+1]
				i++
			}
			var extra map[string]any
			if err := json.Unmarshal([]byte(val), &extra); err != nil {
				return nil, fmt.Errorf("--json を解釈できない: %v", err)
			}
			for k, v := range extra {
				params[k] = v
			}
			continue
		}
		prop, known := schema.Property(name)
		if !hasVal {
			isBool := known && prop.Type == "boolean"
			if !isBool && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				val = args[i+1]
				i++
			} else {
				val = "true"
			}
		}
		v, err := ctlValue(prop, known, val)
		if err != nil {
			return nil, fmt.Errorf("--%s: %v", name, err)
		}
		params[name] = v
	}
	if len(positional) > len(spec.Positional) {
		return nil, fmt.Errorf("位置引数が多すぎる（%s は %d 個まで: %s）", spec.Name,
			len(spec.Positional), strings.Join(spec.Positional, " "))
	}
	for i, val := range positional {
		name := spec.Positional[i]
		prop, known := schema.Property(name)
		v, err := ctlValue(prop, known, val)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		params[name] = v
	}
	return params, nil
}

// ctlValue は文字列を引数の型に直す。
func ctlValue(prop *agent.Schema, known bool, val string) (any, error) {
	typ := ""
	if known {
		typ = prop.Type
	}
	switch typ {
	case "string":
		return val, nil
	case "boolean":
		return strconv.ParseBool(val)
	case "integer":
		return strconv.ParseInt(val, 0, 64)
	case "number":
		return strconv.ParseFloat(val, 64)
	case "array", "object":
		var v any
		if err := json.Unmarshal([]byte(val), &v); err != nil {
			return nil, fmt.Errorf("JSON を解釈できない: %v", err)
		}
		return v, nil
	}
	if strings.HasPrefix(val, "[") || strings.HasPrefix(val, "{") {
		// 型の決まっていない引数（値のバイト列など）は JSON として読む。
		var v any
		if err := json.Unmarshal([]byte(val), &v); err == nil {
			return v, nil
		}
	}
	if b, err := strconv.ParseBool(val); err == nil && (val == "true" || val == "false") {
		return b, nil
	}
	if n, err := strconv.ParseInt(val, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f, nil
	}
	return val, nil
}

// ctlCandidates は語の並びに近い Agent Command の CLI の名前を返す。
func ctlCandidates(reg *agent.Registry, words []string) []string {
	var out []string
	for _, s := range reg.All() {
		cli := agent.CLIWords(s.Name)
		if len(words) > 0 && cli[0] == words[0] {
			out = append(out, strings.Join(cli, " "))
		}
	}
	return out
}

// printCtlHelp は Agent Command の一覧を表示する。words を与えたときは、
// その名前空間のものだけを並べる。
func printCtlHelp(w io.Writer, reg *agent.Registry, words []string) {
	fmt.Fprintf(w, "使い方: %s ctl [--pid PID] [--text] [--out PATH] [--json JSON] <名前空間> <操作> [引数...]\n\n", appName)
	fmt.Fprintf(w, "Agent Command:\n")
	for _, s := range reg.All() {
		cli := agent.CLIWords(s.Name)
		if len(words) > 0 && cli[0] != words[0] {
			continue
		}
		fmt.Fprintf(w, "  %-24s %s\n", strings.Join(cli, " "), s.DescJA)
	}
	fmt.Fprintf(w, "\n%s ctl <名前空間> <操作> --help で引数を表示する。\n", appName)
}

// printCtlCommandHelp は Agent Command 1 つの引数を表示する。
func printCtlCommandHelp(w io.Writer, s *agent.CommandSpec) {
	cli := strings.Join(agent.CLIWords(s.Name), " ")
	usage := fmt.Sprintf("%s ctl %s", appName, cli)
	for _, p := range s.Positional {
		usage += " [" + p + "]"
	}
	fmt.Fprintf(w, "使い方: %s [--引数 値...]\n\n%s\n", usage, s.DescJA)
	schema := s.Schema()
	if len(schema.Properties) == 0 {
		return
	}
	fmt.Fprintf(w, "\n引数:\n")
	for _, p := range schema.Properties {
		req := ""
		if slices.Contains(schema.Required, p.Name) {
			req = "（必須）"
		}
		fmt.Fprintf(w, "  --%-20s %-8s %s%s\n", strings.ReplaceAll(p.Name, "_", "-"), p.Schema.Type, p.Schema.Description, req)
	}
}

// printCtlResult は結果を表示する。画像（mime と data を持つ値）は --out の
// ときだけファイルに書き、それ以外は大きさだけを示す。
func printCtlResult(w io.Writer, raw json.RawMessage, text bool, out string) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	written := false
	var firstErr error
	v = replaceImages(v, func(data []byte) string {
		if out != "" && !written {
			written = true
			if err := os.WriteFile(out, data, 0o644); err != nil {
				firstErr = err
				return fmt.Sprintf("<画像 %d バイト（書き出せない: %v）>", len(data), err)
			}
			return fmt.Sprintf("<画像 %d バイトを %s に書いた>", len(data), out)
		}
		return fmt.Sprintf("<画像 %d バイト>", len(data))
	})
	if firstErr != nil {
		return firstErr
	}
	if text {
		writeText(w, v, "")
		return nil
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// replaceImages は mime と data を持つ object の data を fn の結果に置き換える。
func replaceImages(v any, fn func([]byte) string) any {
	switch x := v.(type) {
	case map[string]any:
		if mime, ok := x["mime"].(string); ok && strings.HasPrefix(mime, "image/") {
			if s, ok := x["data"].(string); ok {
				if data, err := base64.StdEncoding.DecodeString(s); err == nil {
					x["data"] = fn(data)
					return x
				}
			}
		}
		for k, e := range x {
			x[k] = replaceImages(e, fn)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = replaceImages(e, fn)
		}
		return x
	}
	return v
}

// writeText は値を「キー: 値」の形で字下げして書く。キーは名前順に並べる。
func writeText(w io.Writer, v any, indent string) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			switch e := x[k].(type) {
			case map[string]any, []any:
				fmt.Fprintf(w, "%s%s:\n", indent, k)
				writeText(w, e, indent+"  ")
			default:
				fmt.Fprintf(w, "%s%s: %v\n", indent, k, e)
			}
		}
	case []any:
		for i, e := range x {
			switch e.(type) {
			case map[string]any, []any:
				fmt.Fprintf(w, "%s- [%d]\n", indent, i)
				writeText(w, e, indent+"  ")
			default:
				fmt.Fprintf(w, "%s- %v\n", indent, e)
			}
		}
	default:
		fmt.Fprintf(w, "%s%v\n", indent, x)
	}
}
