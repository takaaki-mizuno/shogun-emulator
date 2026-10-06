package scenario

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// 結果の出力（設計書 14 編 §14.17.3）。

// 終了コード（設計書 11 編 §11.5.2）。
const (
	ExitOK          = 0
	ExitSetupError  = 1
	ExitFailed      = 5
	ExitInvalidFile = 6
)

// ExitCode は結果から終了コードを決める。ファイルの不正、読み込みの失敗、
// アサーションの失敗、合格の順に優先する。
func ExitCode(results []FileResult) int {
	code := ExitOK
	rank := map[int]int{ExitOK: 0, ExitFailed: 1, ExitSetupError: 2, ExitInvalidFile: 3}
	raise := func(c int) {
		if rank[c] > rank[code] {
			code = c
		}
	}
	for _, f := range results {
		if f.Err != nil {
			raise(ExitInvalidFile)
		}
		for _, c := range f.Cases {
			switch c.Status {
			case Failed:
				raise(ExitFailed)
			case SetupError:
				raise(ExitSetupError)
			}
		}
	}
	return code
}

// statusText は結果の表示。
func statusText(s Status) string {
	switch s {
	case Passed:
		return "PASS"
	case Failed:
		return "FAIL"
	}
	return "ERROR"
}

// WriteFileResult はファイル 1 つの結果を人間向けに書く。
func WriteFileResult(w io.Writer, f FileResult) {
	for _, warn := range f.Warnings {
		fmt.Fprintf(w, "警告: %s\n", warn)
	}
	if f.Err != nil {
		fmt.Fprintf(w, "INVALID %s\n  %s\n", f.Path, f.Err.Error())
		return
	}
	for _, c := range f.Cases {
		fmt.Fprintf(w, "%-5s %s（%s、%.2f 秒、frame %d）\n", statusText(c.Status), c.Name, f.Path, c.Duration.Seconds(), c.Frame)
		for _, n := range c.Notes {
			fmt.Fprintf(w, "  知らせ: %s\n", n)
		}
		if c.Failure != nil {
			for _, line := range failureLines(c.Failure) {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
		for _, u := range c.Updated {
			fmt.Fprintf(w, "  お手本を書いた: %s\n", u)
		}
		if c.Repro != "" {
			fmt.Fprintf(w, "  Repro: %s\n", c.Repro)
		}
		if c.ReproErr != "" {
			fmt.Fprintf(w, "  Repro を書けない: %s\n", c.ReproErr)
		}
	}
}

// failureLines は失敗の内容を行に分ける。
func failureLines(f *Failure) []string {
	var out []string
	if f.Step > 0 {
		out = append(out, fmt.Sprintf("ステップ %d（%d 行）: %s", f.Step, f.Line, f.Text))
	}
	out = append(out, f.Message)
	if f.Expr != "" {
		out = append(out, "式: "+f.Expr)
	}
	if f.Actual != "" {
		out = append(out, "実際の値: "+f.Actual)
	}
	if f.Summary != "" {
		out = append(out, "Observation: "+f.Summary)
	}
	return out
}

// WriteSummary は全体の件数を書く。
func WriteSummary(w io.Writer, results []FileResult) {
	var pass, fail, errs, invalid int
	var updated []string
	for _, f := range results {
		if f.Err != nil {
			invalid++
		}
		for _, c := range f.Cases {
			switch c.Status {
			case Passed:
				pass++
			case Failed:
				fail++
			default:
				errs++
			}
			updated = append(updated, c.Updated...)
		}
	}
	fmt.Fprintf(w, "\n合格 %d、失敗 %d、読み込みの失敗 %d、不正なファイル %d\n", pass, fail, errs, invalid)
	if len(updated) > 0 {
		fmt.Fprintf(w, "書き換えたお手本（%d）:\n", len(updated))
		for _, u := range updated {
			fmt.Fprintf(w, "  %s\n", u)
		}
	}
}

// JUnit XML の要素。

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Errors   int          `xml:"errors,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Errors    int         `xml:"errors,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr,omitempty"`
	Cases     []junitCase `xml:"testcase"`
	SystemErr string      `xml:"system-err,omitempty"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitProblem `xml:"failure,omitempty"`
	Error     *junitProblem `xml:"error,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }

// WriteJUnit は JUnit XML を書く。Scenario ファイル 1 つを testsuite、
// Scenario を testcase とする。不正なファイルは error の testcase 1 つとする。
func WriteJUnit(w io.Writer, results []FileResult) error {
	var root junitSuites
	var total time.Duration
	for _, f := range results {
		s := junitSuite{Name: f.Path, Time: seconds(f.Duration)}
		total += f.Duration
		if len(f.Warnings) > 0 {
			s.SystemErr = strings.Join(f.Warnings, "\n")
		}
		if f.Err != nil {
			s.Tests, s.Errors = 1, 1
			s.Cases = append(s.Cases, junitCase{Name: f.Path, Classname: f.Path, Time: seconds(f.Duration),
				Error: &junitProblem{Message: f.Err.Error(), Type: "invalid_scenario", Text: f.Err.Error()}})
		}
		for _, c := range f.Cases {
			jc := junitCase{Name: c.Name, Classname: f.Path, Time: seconds(c.Duration)}
			var out []string
			out = append(out, c.Notes...)
			for _, u := range c.Updated {
				out = append(out, "お手本を書いた: "+u)
			}
			if c.Repro != "" {
				out = append(out, "Repro: "+c.Repro)
			}
			jc.SystemOut = strings.Join(out, "\n")
			s.Tests++
			if c.Failure != nil {
				msg := c.Failure.Message
				if c.Failure.Step > 0 {
					msg = fmt.Sprintf("ステップ %d: %s: %s", c.Failure.Step, c.Failure.Text, c.Failure.Message)
				}
				p := &junitProblem{Message: msg, Text: strings.Join(failureLines(c.Failure), "\n")}
				if c.Status == Failed {
					p.Type = "assertion_failed"
					jc.Failure = p
					s.Failures++
				} else {
					p.Type = "setup_error"
					jc.Error = p
					s.Errors++
				}
			}
			s.Cases = append(s.Cases, jc)
		}
		root.Tests += s.Tests
		root.Failures += s.Failures
		root.Errors += s.Errors
		root.Suites = append(root.Suites, s)
	}
	root.Time = seconds(total)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
