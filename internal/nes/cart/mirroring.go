package cart

// MapNametableWith はミラーリングの配置に従って $2000-$2FFF のアクセス先を返す。
//
// 解決表は設計書 04 編 §4.9 のとおり。各マッパーはこの関数を使い、
// 自分でアドレス計算を書かない。
func MapNametableWith(m Mirroring, addr uint16) NametableTarget {
	a := addr & 0x0FFF
	switch m {
	case MirrorHorizontal:
		// 上下 2 段。CIRAM A10 に PPU A11 を与える。
		return NametableTarget{Kind: NametableCIRAM, Offset: uint32(a&0x03FF | (a&0x0800)>>1)}
	case MirrorVertical:
		// 左右 2 面。CIRAM A10 に PPU A10 を与える。
		return NametableTarget{Kind: NametableCIRAM, Offset: uint32(a & 0x07FF)}
	case MirrorSingleA:
		return NametableTarget{Kind: NametableCIRAM, Offset: uint32(a & 0x03FF)}
	case MirrorSingleB:
		return NametableTarget{Kind: NametableCIRAM, Offset: uint32(a&0x03FF | 0x0400)}
	case MirrorFourScreen:
		return NametableTarget{Kind: NametableCart, Offset: uint32(a)}
	}
	return NametableTarget{Kind: NametableCIRAM, Offset: uint32(a & 0x07FF)}
}
