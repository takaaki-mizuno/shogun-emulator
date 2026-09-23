package debug

// logBus はバスアクセスを該当するカテゴリへ記録する。
//
// どのカテゴリも無効なときは比較だけで戻る。
func (d *Debugger) logBus(addr uint16, v uint8, write bool) {
	cats := d.log.Categories
	if cats&(CatTraceCPUBus|CatPPURegister|CatAPURegister|CatInput|CatMapper|CatDMA) == 0 {
		return
	}
	dir := "R"
	if write {
		dir = "W"
	}
	if cats&CatTraceCPUBus != 0 {
		d.log.Log(CatTraceCPUBus, "%s $%04X = $%02X", dir, addr, v)
	}
	switch {
	case addr >= 0x2000 && addr < 0x4000:
		if cats&CatPPURegister != 0 {
			d.log.Log(CatPPURegister, "%s $%04X（$%04X）= $%02X（スキャンライン %d, ドット %d）",
				dir, 0x2000|addr&0x0007, addr, v, d.n.PPU.Scanline(), d.n.PPU.Dot())
		}
	case addr == 0x4014:
		if write && cats&CatDMA != 0 {
			d.log.Log(CatDMA, "OAM DMA（ページ $%02X）", v)
		}
	case addr == 0x4016 || addr == 0x4017:
		if cats&CatInput != 0 {
			d.log.Log(CatInput, "%s $%04X = $%02X", dir, addr, v)
		}
		if write && addr == 0x4017 && cats&CatAPURegister != 0 {
			d.log.Log(CatAPURegister, "W $4017 = $%02X", v)
		}
	case addr >= 0x4000 && addr < 0x4018:
		if cats&CatAPURegister != 0 {
			d.log.Log(CatAPURegister, "%s $%04X = $%02X", dir, addr, v)
		}
	case addr >= 0x4020 && write:
		if cats&CatMapper != 0 {
			d.log.Log(CatMapper, "W $%04X = $%02X", addr, v)
		}
	}
}

// logTiming はスキャンラインの変わり目を記録する。
func (d *Debugger) logTiming(line int) {
	r := d.n.Region
	switch line {
	case 0:
		d.log.Log(CatPPUTiming, "フレーム %d の描画開始", d.n.Frames())
	case r.VBlankStartScanline():
		d.log.Log(CatPPUTiming, "VBlank 開始（フレーム %d）", d.n.Frames())
	case r.PreRenderScanline():
		d.log.Log(CatPPUTiming, "プリレンダー行（フレーム %d）", d.n.Frames())
	}
}
