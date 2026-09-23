package emu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// ApplySettings は設定画面で保存した設定を反映する（設計書 11 編 §11.3.2）。
//
// 即時に効く項目（音量・バッファ長・早送り時のミュート・Triangle の超音波
// 停止・ログカテゴリ・変更追跡の減衰）はここで反映する。ROM の再読み込み後に
// 効く項目（emulation・paths・state・movie・入力デバイス・フィルタ
// プロファイル）は保持だけを差し替え、次に ROM を読み込むときに使う。
// 再起動後に効く項目（音声の有効・無効、ログの出力先、トレースリングの
// 大きさ）は変えない。
func (e *Emulator) ApplySettings(c *config.Config) {
	c = c.Clone()
	e.WithMachine(func(m *nes.NES) {
		enabled := e.cfg.Audio.Enabled
		e.cfg.Emulation = c.Emulation
		e.cfg.Input = c.Input
		e.cfg.Paths = c.Paths
		e.cfg.State = c.State
		e.cfg.Movie = c.Movie
		e.cfg.Audio = c.Audio
		e.cfg.Audio.Enabled = enabled
		ringSize, logOut := e.cfg.Debug.TraceRingSize, e.cfg.Debug.LogOutput
		e.cfg.Debug = c.Debug
		e.cfg.Debug.TraceRingSize, e.cfg.Debug.LogOutput = ringSize, logOut

		if p := e.audio; p != nil {
			p.out.SetBufferMilliseconds(c.Audio.BufferMilliseconds)
			p.volume = c.Audio.MasterVolume
			if !e.Status().Muted {
				p.out.SetVolume(p.volume)
			}
			p.muteOnFastForward = c.Audio.MuteOnFastForward
			p.profile = c.Audio.FilterProfile
			p.setSpeed(e.speed)
		}
		if m != nil {
			m.APU.SetVolumes(volumes(c.Audio.ChannelVolumes))
			m.APU.SetSilenceUltrasonicTriangle(c.Audio.SilenceUltrasonicTriangle)
		}
		if cats, err := debug.ParseCategories(c.Debug.LogCategories); err == nil {
			e.dbg.SetLogCategories(cats)
		}
		e.dbg.Changes().SetDecay(uint32(c.Debug.ChangeDecayFrames))
	})
}

// Settings は現在の保持している設定の写しを返す。テストで使う。
func (e *Emulator) Settings() Config {
	var c Config
	e.WithMachine(func(*nes.NES) { c = e.cfg })
	return c
}
