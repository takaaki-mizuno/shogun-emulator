package emu

// このファイルは進行の待ち方を扱う。
//
// オーディオが有効なときは wallClockPacer を使わない。オーディオ
// リングバッファの消費で待つ audioPacer を使う（設計書 02 編 §2.6）。
// 壁時計はホストの時計とオーディオデバイスのクロックのずれを吸収できず、
// 音を出しながら使うとずれが累積して音が途切れる。

import (
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// Pacer は 1 フレーム進み終えたところで呼ばれ、次を始めてよくなるまで待つ。
type Pacer interface {
	// WaitFrame は 1 フレーム分の進行が終わったことを伝える。
	// speed は速度倍率で、1.0 が実時間と同じ速さである。
	WaitFrame(speed float64)

	// Reset は待ちの基準を現在に合わせる。
	Reset()
}

// UncappedSpeed はこれ以上の速度倍率では待たない境界。
//
// 4 倍以上の早送りでは進行速度をホストの処理能力に任せる。待ち時間が
// 1 フレームあたり 4 ミリ秒を下回り、待つ処理そのものの誤差が
// 待ち時間を上回るためである。オーディオを繋いだときも、この倍率から
// 先はリングバッファへ書かず待たない（設計書 02 編 §2.6）。
const UncappedSpeed = 4.0

// wallClockPacer は壁時計でフレーム周期を待つ。
type wallClockPacer struct {
	// period は 1 フレームの長さ。
	period time.Duration
	// next は次のフレームを始めてよい時刻。
	next time.Time
}

// NewWallClockPacer はリージョンのフレーム周期で待つ Pacer を作る。
func NewWallClockPacer(r *region.Region) Pacer {
	return &wallClockPacer{period: framePeriod(r)}
}

// framePeriod はリージョンの 1 フレームの長さを返す。
//
// リージョン定数のクロックとスキャンライン構成から求める。60 や 50 と
// いった値を直接書くと、リージョン定数との対応が取れなくなる。
//
// 奇数フレームの 1 ドットスキップは平均として織り込む。フレームごとに
// 周期を変えても、表示側は次の垂直同期まで待つため差が現れない。
func framePeriod(r *region.Region) time.Duration {
	return time.Duration(float64(time.Second) / r.FrameRateHz(true))
}

// WaitFrame はフレーム周期まで待つ。
func (p *wallClockPacer) WaitFrame(speed float64) {
	if speed >= UncappedSpeed {
		p.next = time.Time{}
		return
	}
	if speed <= 0 {
		speed = 1
	}
	period := time.Duration(float64(p.period) / speed)

	now := time.Now()
	if p.next.IsZero() {
		p.next = now.Add(period)
		return
	}
	if d := p.next.Sub(now); d > 0 {
		time.Sleep(d)
	}
	p.next = p.next.Add(period)

	// 大きく遅れたときは追いつこうとしない。追いつくために待ちを
	// 省き続けると、遅れが解消するまで早送りになる。
	if late := time.Since(p.next); late > period*2 {
		p.next = time.Now().Add(period)
	}
}

// Reset は待ちの基準を現在に合わせる。
func (p *wallClockPacer) Reset() { p.next = time.Time{} }

// noPacer は待たない Pacer。テストと、進行を上限なく回す場面で使う。
type noPacer struct{}

// NewNoPacer は待たない Pacer を返す。
func NewNoPacer() Pacer { return noPacer{} }

// WaitFrame は何もしない。
func (noPacer) WaitFrame(float64) {}

// Reset は何もしない。
func (noPacer) Reset() {}
