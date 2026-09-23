package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// fpsWindow はフレームレートを測る区間の長さ。
//
// 1 秒ごとに更新する。短くすると表示が落ち着かず、長くすると
// 処理が重くなったことに気づきにくい。
const fpsWindow = time.Second

// statusBar は実行状態の表示。
type statusBar struct {
	label *widget.Label

	// frames は完成フレーム数の取得元。
	frames *emu.FrameBuffer

	// 測定の基準。
	lastAt     time.Time
	lastFrames uint64
	fps        float64
	dropped    uint64

	// last は最後に表示した文字列。同じ内容で更新しないために持つ。
	last string

	// message は一時的に表示する知らせ。
	message   string
	messageAt time.Time
}

// newStatusBar はステータスバーを作る。
func newStatusBar(frames *emu.FrameBuffer) *statusBar {
	b := &statusBar{
		label:  widget.NewLabel(""),
		frames: frames,
		lastAt: time.Now(),
	}
	return b
}

// CanvasObject は表示内容を返す。
func (b *statusBar) CanvasObject() fyne.CanvasObject { return b.label }

// messageDuration は知らせを表示し続ける時間。
const messageDuration = 4 * time.Second

// notify は一時的な知らせを表示する。
func (b *statusBar) notify(msg string) {
	b.message = msg
	b.messageAt = time.Now()
}

// update は表示を更新する。UI スレッドから呼ぶ。
//
// 文字列が変わったときだけ書き換える。毎回書き換えると、内容が同じでも
// Fyne が再描画の対象として扱う。
func (b *statusBar) update(s emu.Status) {
	b.measure()
	text := b.text(s)
	if text == b.last {
		return
	}
	b.last = text
	b.label.SetText(text)
}

// measure は実測のフレームレートを更新する。
//
// エミュレーションが生成したフレーム数を数える。表示が捨てたフレームは
// 別に数え、表示が追いついていないことを区別できるようにする。
func (b *statusBar) measure() {
	produced, dropped := b.frames.Stats()
	elapsed := time.Since(b.lastAt)
	if elapsed < fpsWindow {
		return
	}
	b.fps = float64(produced-b.lastFrames) / elapsed.Seconds()
	b.dropped = dropped
	b.lastFrames = produced
	b.lastAt = time.Now()
}

// text は表示する文字列を組み立てる。
func (b *statusBar) text(s emu.Status) string {
	if !s.Loaded {
		if b.message != "" && time.Since(b.messageAt) <= messageDuration {
			return "ROM を読み込んでいません │ " + b.message
		}
		return "ROM を読み込んでいません"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%.1f fps", b.fps)
	fmt.Fprintf(&sb, " │ %s", speedText(s.Speed))
	fmt.Fprintf(&sb, " │ %s", runStateText(s))
	fmt.Fprintf(&sb, " │ %s", s.ROMName)
	fmt.Fprintf(&sb, " │ %s（マッパー %d）", s.MapperName, s.MapperNumber)
	fmt.Fprintf(&sb, " │ スロット %d", s.Slot)
	if t := movieText(s); t != "" {
		sb.WriteString(" │ ")
		sb.WriteString(t)
	}
	if s.Rewinding {
		sb.WriteString(" │ 巻き戻し中")
	}
	fmt.Fprintf(&sb, " │ %s", s.RegionName)
	fmt.Fprintf(&sb, " │ %s", audioText(s))
	if b.dropped > 0 {
		fmt.Fprintf(&sb, " │ 表示落ち %d", b.dropped)
	}
	if b.message != "" {
		if time.Since(b.messageAt) > messageDuration {
			b.message = ""
		} else {
			fmt.Fprintf(&sb, " │ %s", b.message)
		}
	}
	return sb.String()
}

// audioText は音声の状態の表示を返す。
//
// 音切れが起きたときはその回数を出す。回数が増え続けるなら、処理が
// オーディオの消費に追いついていない。
func audioText(s emu.Status) string {
	switch {
	case !s.AudioEnabled:
		return "音声なし"
	case s.Muted:
		return "消音"
	case s.AudioUnderruns > 0:
		return fmt.Sprintf("音切れ %d", s.AudioUnderruns)
	}
	return "音声あり"
}

// speedText は速度倍率の表示を返す。
//
// 待ちを行わない境界より上では、実際の速さがホストの処理能力で決まる。
// 倍率をそのまま出すと、その数値で動いていると誤解を招く。
func speedText(speed float64) string {
	switch {
	case speed >= emu.UncappedSpeed:
		return "最速"
	case speed == 1:
		return "等速"
	}
	return fmt.Sprintf("%g 倍速", speed)
}

// runStateText は実行状態の表示を返す。
//
// ブレークポイントで止まったときはその理由を、命令の途中で止まって
// いるときはその旨を添える。命令の途中ではセーブステートを取れない
// ため（設計書 08 編 §8.3.2）、利用者に見えるようにする。
func runStateText(s emu.Status) string {
	if !s.Paused {
		return "実行中"
	}
	t := "一時停止"
	if s.Break != "" {
		t = "停止: " + s.Break
	}
	if s.MidInstruction {
		t += "（命令の途中）"
	}
	return t
}

// movieText はムービーの状態を表す文字列を返す。扱っていないとき空。
func movieText(s emu.Status) string {
	switch {
	case s.Movie.Recording:
		return fmt.Sprintf("録画中 %d フレーム", s.Movie.Frame)
	case s.Movie.Playing:
		return fmt.Sprintf("再生中 %d/%d", s.Movie.Frame, s.Movie.Total)
	}
	return ""
}
