package config

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

// logCategoryNames は debug.logCategories に書けるカテゴリ名（設計書 09 編 §9.8）。
//
// internal/debug は config を参照できない向きの依存であるため、名前をここにも
// 持つ。両者が一致することは internal/debug のテストで確かめる。
var logCategoryNames = []string{
	"trace.cpu", "trace.cpu.bus", "ppu.register", "ppu.timing",
	"apu.register", "apu.frame", "mapper", "dma", "input",
	"warn.compat", "error",
}

// LogCategoryNames はログカテゴリの名前を返す。
func LogCategoryNames() []string { return slices.Clone(logCategoryNames) }

// Validate は範囲外の値を既定値に戻し、戻した項目を警告として返す
// （設計書 11 編 §11.3.1）。
func (c *Config) Validate() []string {
	d := Default()
	var w []string
	choice := func(name string, v *string, def string, allowed ...string) {
		if !slices.Contains(allowed, *v) {
			w = append(w, fmt.Sprintf("%s の値 %q は使えないため %q にする", name, *v, def))
			*v = def
		}
	}
	intRange := func(name string, v *int, def, lo, hi int) {
		if *v < lo || *v > hi {
			w = append(w, fmt.Sprintf("%s の値 %d は範囲 %d-%d の外にあるため %d にする", name, *v, lo, hi, def))
			*v = def
		}
	}
	floatRange := func(name string, v *float64, def float64) {
		if *v < 0 || *v > 1 {
			w = append(w, fmt.Sprintf("%s の値 %g は範囲 0-1 の外にあるため %g にする", name, *v, def))
			*v = def
		}
	}

	e := &c.Emulation
	choice("emulation.region", &e.Region, d.Emulation.Region, RegionAuto, RegionNTSC, RegionPAL, RegionDendy)
	choice("emulation.ramInitPattern", &e.RAMInitPattern, d.Emulation.RAMInitPattern, "zero", "ff", "pattern", "random")
	intRange("emulation.cpuPpuAlignment", &e.CPUPPUAlignment, d.Emulation.CPUPPUAlignment, 0, 2)
	intRange("emulation.dmaGetPutPhase", &e.DMAGetPutPhase, d.Emulation.DMAGetPutPhase, 0, 1)
	choice("emulation.mmc3IrqVariant", &e.MMC3IRQVariant, d.Emulation.MMC3IRQVariant, "sharp", "nec")
	choice("emulation.busConflicts", &e.BusConflicts, d.Emulation.BusConflicts, "auto", "always", "never")

	v := &c.Video
	intRange("video.scale", &v.Scale, d.Video.Scale, MinScale, MaxScale)
	intRange("video.overscanTop", &v.OverscanTop, d.Video.OverscanTop, 0, MaxOverscan)
	intRange("video.overscanBottom", &v.OverscanBottom, d.Video.OverscanBottom, 0, MaxOverscan)
	intRange("video.overscanLeft", &v.OverscanLeft, d.Video.OverscanLeft, 0, MaxOverscan)
	intRange("video.overscanRight", &v.OverscanRight, d.Video.OverscanRight, 0, MaxOverscan)
	choice("video.filter", &v.Filter, d.Video.Filter, FilterNearest, FilterLinear)

	a := &c.Audio
	if a.SampleRate != FixedSampleRate {
		w = append(w, fmt.Sprintf("audio.sampleRate は %d に固定しているため %d は使わない", FixedSampleRate, a.SampleRate))
		a.SampleRate = FixedSampleRate
	}
	intRange("audio.bufferMilliseconds", &a.BufferMilliseconds, d.Audio.BufferMilliseconds, 5, 200)
	if a.RingHighWaterMultiplier < 2 {
		a.RingHighWaterMultiplier = 2
	}
	floatRange("audio.masterVolume", &a.MasterVolume, d.Audio.MasterVolume)
	if a.ChannelVolumes == nil {
		a.ChannelVolumes = defaultChannelVolumes()
	}
	for _, name := range ChannelNames {
		vol, ok := a.ChannelVolumes[name]
		if !ok {
			a.ChannelVolumes[name] = 1.0
			continue
		}
		floatRange("audio.channelVolumes."+name, &vol, 1.0)
		a.ChannelVolumes[name] = vol
	}
	for name := range a.ChannelVolumes {
		if !slices.Contains(ChannelNames, name) {
			w = append(w, fmt.Sprintf("audio.channelVolumes の知らないチャンネル %q を無視する", name))
			delete(a.ChannelVolumes, name)
		}
	}
	choice("audio.filterProfile", &a.FilterProfile, d.Audio.FilterProfile, "nes", "famicom", "none")

	in := &c.Input
	choice("input.port1Device", &in.Port1Device, d.Input.Port1Device, DeviceStandard, DeviceNone)
	choice("input.port2Device", &in.Port2Device, d.Input.Port2Device, DeviceStandard, DeviceNone)
	intRange("input.turboRateHz", &in.TurboRateHz, d.Input.TurboRateHz, 1, 30)

	dbg := &c.Debug
	choice("debug.logOutput", &dbg.LogOutput, d.Debug.LogOutput, LogNone, LogStderr, LogStdout, LogFile)
	cats := dbg.LogCategories[:0:0]
	for _, name := range dbg.LogCategories {
		if slices.Contains(logCategoryNames, name) {
			cats = append(cats, name)
		} else {
			w = append(w, fmt.Sprintf("debug.logCategories の知らないカテゴリ %q を除く", name))
		}
	}
	dbg.LogCategories = cats
	if dbg.LogMaxBytes < 1<<16 {
		w = append(w, fmt.Sprintf("debug.logMaxBytes の値 %d は小さすぎるため %d にする", dbg.LogMaxBytes, d.Debug.LogMaxBytes))
		dbg.LogMaxBytes = d.Debug.LogMaxBytes
	}
	intRange("debug.logGenerations", &dbg.LogGenerations, d.Debug.LogGenerations, 1, 20)
	intRange("debug.traceRingSize", &dbg.TraceRingSize, d.Debug.TraceRingSize, 1000, 10000000)
	intRange("debug.changeDecayFrames", &dbg.ChangeDecayFrames, d.Debug.ChangeDecayFrames, 1, 600)

	st := &c.State
	intRange("state.slots", &st.Slots, d.State.Slots, 1, 10)
	intRange("state.rewindSeconds", &st.RewindSeconds, d.State.RewindSeconds, 1, 600)
	intRange("state.rewindIntervalFrames", &st.RewindIntervalFrames, d.State.RewindIntervalFrames, 1, 60)
	intRange("movie.checksumIntervalFrames", &c.Movie.ChecksumIntervalFrames, d.Movie.ChecksumIntervalFrames, 1, 3600)

	ui := &c.UI
	choice("ui.viewerLayout", &ui.ViewerLayout, d.UI.ViewerLayout, LayoutWindows, LayoutDocked)
	choice("ui.language", &ui.Language, d.UI.Language, LanguageJapanese)
	choice("ui.theme", &ui.Theme, d.UI.Theme, ThemeAuto, ThemeLight, ThemeDark)
	if ui.Windows == nil {
		ui.Windows = map[string]WindowState{}
	}
	if ui.RecentROMs == nil {
		ui.RecentROMs = []RecentROM{}
	}
	if len(ui.RecentROMs) > MaxRecentROMs {
		ui.RecentROMs = ui.RecentROMs[:MaxRecentROMs]
	}

	ag := &c.Agent
	if !ValidAgentListen(ag.Listen) {
		w = append(w, fmt.Sprintf("agent.listen の値 %q は使えないため %q にする", ag.Listen, d.Agent.Listen))
		ag.Listen = d.Agent.Listen
	}
	intRange("agent.maxInstances", &ag.MaxInstances, d.Agent.MaxInstances, 1, 64)
	choice("agent.romWatchAction", &ag.RomWatchAction, d.Agent.RomWatchAction, RomWatchNotify, RomWatchReload)
	intRange("agent.observeImageScale", &ag.ObserveImageScale, d.Agent.ObserveImageScale, 1, 4)
	return w
}

// ValidAgentListen は agent.listen に書ける値かを返す（設計書 11 編 §11.3.1）。
//
// TCP は 127.0.0.1 と ::1 に限る。他のアドレスで待ち受けると、同じ
// ネットワークの他の機械から操作できてしまう（設計書 14 編 §14.21）。
func ValidAgentListen(v string) bool {
	if v == AgentListenUnix {
		return true
	}
	rest, ok := strings.CutPrefix(v, "tcp:")
	if !ok {
		return false
	}
	host, port, err := net.SplitHostPort(rest)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 0 && n <= 65535
}
