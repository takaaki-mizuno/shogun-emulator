package bus

// IRQSource は IRQ の発生源。
//
// IRQ はレベル検出であり、複数の発生源の論理和である。発生源ごとにビットを
// 持つことで、ある発生源がクリアされても他の発生源のアサートが残ることを
// 正しく表せる。
type IRQSource uint8

const (
	// IRQAPUFrame は APU のフレームカウンタ。
	IRQAPUFrame IRQSource = 1 << 0
	// IRQAPUDMC は APU の DMC。
	IRQAPUDMC IRQSource = 1 << 1
	// IRQMapper はマッパー。
	IRQMapper IRQSource = 1 << 2
)

// SetIRQ は発生源のアサート状態を設定する。
func (b *Bus) SetIRQ(src IRQSource, asserted bool) {
	if asserted {
		b.irqSources |= src
		return
	}
	b.irqSources &^= src
}

// IRQAsserted はいずれかの発生源がアサートしているかを返す。
//
// マッパーは状態を自分で持つため、ビット集合とは別に問い合わせる。
func (b *Bus) IRQAsserted() bool {
	return b.irqSources != 0 || b.cart.IRQAsserted() || b.apu.IRQAsserted()
}
