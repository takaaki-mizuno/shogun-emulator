package cart

import "fmt"

// Options はマッパーの挙動のうち、設定で選ぶもの。
//
// 実機に複数の版が存在し、プログラムがどちらかに依存する箇所である。
// 既定値は広く使われている版に合わせる。
type Options struct {
	// BusConflicts は "auto"・"always"・"never" のいずれか。
	BusConflicts string
	// MMC3IRQVariant は "sharp"・"nec" のいずれか。
	MMC3IRQVariant string
}

// DefaultOptions は既定の設定を返す。
func DefaultOptions() Options {
	return Options{BusConflicts: BusConflictsAuto, MMC3IRQVariant: MMC3IRQSharp}
}

// validate は値が受け付けられるものかを確かめる。
func (o Options) validate() error {
	switch o.BusConflicts {
	case "", BusConflictsAuto, BusConflictsAlways, BusConflictsNever:
	default:
		return fmt.Errorf("cart: 知らない busConflicts の値 %q", o.BusConflicts)
	}
	switch o.MMC3IRQVariant {
	case "", MMC3IRQSharp, MMC3IRQNEC:
	default:
		return fmt.Errorf("cart: 知らない mmc3IrqVariant の値 %q", o.MMC3IRQVariant)
	}
	return nil
}
