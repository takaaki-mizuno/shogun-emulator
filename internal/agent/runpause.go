package agent

import "encoding/json"

// exec.run と exec.pause（設計書 14 編 §14.7.2）。GUI 版だけで使う。

func registerRunPause(r *Registry) {
	r.Register(CommandSpec{
		Name: "exec.run", Class: ClassAdvance, Params: InstanceParam{},
		DescJA: "Control を持ったまま実時間で走らせる（ゲームの様子を人間に見せる）。GUI 版のみ",
		DescEN: "GUI only: run in real time while keeping control (keyboard input stays disabled) so a human can watch.",
		GUI:    true, Target: true, Handler: handleRun,
	})
	r.Register(CommandSpec{
		Name: "exec.pause", Class: ClassAdvance, Params: InstanceParam{},
		DescJA: "一時停止して Agent-Paced に戻す。GUI 版のみ",
		DescEN: "GUI only: pause and return to agent-paced stepping.",
		GUI:    true, Target: true, Handler: handlePause,
	})
}

func handleRun(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	// 入力はエージェントの経路のまま（キーボードを使わない）にして走らせる。
	c.Instance.setMode(ModeRealTime)
	c.Instance.Emu.Resume()
	return c.Instance.ControlStatus(), nil
}

func handlePause(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	c.Instance.Emu.Pause()
	c.Instance.setMode(ModeAgentPaced)
	return c.Host.observe(c.Instance, c.Conn, observeSpec{})
}
