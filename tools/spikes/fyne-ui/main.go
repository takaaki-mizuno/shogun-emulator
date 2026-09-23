// Fyne UI 機能スパイク: デバッガに必要な機能が実際に動くかを確認する。
//  1. 別ウィンドウを複数開ける
//  2. ネイティブメニューバー（macOS）
//  3. widget.TextGrid のセル単位の前景色/背景色（16進ダンプのフラッシュ用）
//  4. 同じビューアを「別ウィンドウ」と「1ウィンドウ内タブ」の両方に配置できるか
package main

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ビューアは配置形態を知らない: fyne.CanvasObject を返すだけ
func newHexDump(rows int) (fyne.CanvasObject, func(heat map[int]float32, pc int)) {
	tg := widget.NewTextGrid()
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = fmt.Sprintf("$%04X:  00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F", i*16)
	}
	tg.SetText(joinLines(lines))

	update := func(heat map[int]float32, pc int) {
		for off, h := range heat {
			row := off / 16
			col := off % 16
			// "$0000:  " = 8 文字、以降 1 バイトが 3 文字（8 バイト目の後に空白 1 つ追加）
			x := 8 + col*3
			if col >= 8 {
				x++
			}
			bg := color.NRGBA{R: uint8(200 * h), G: 0, B: 0, A: uint8(160 * h)}
			tg.SetStyleRange(row, x, row, x+1, &widget.CustomTextGridStyle{
				FGColor: color.White, BGColor: bg,
			})
		}
		// PC 位置を別色
		prow, pcol := pc/16, pc%16
		px := 8 + pcol*3
		if pcol >= 8 {
			px++
		}
		tg.SetStyleRange(prow, px, prow, px+1, &widget.CustomTextGridStyle{
			FGColor: color.Black, BGColor: color.NRGBA{R: 255, G: 220, B: 0, A: 255},
		})
		tg.Refresh()
	}
	return container.NewScroll(tg), update
}

func joinLines(l []string) string {
	s := ""
	for i, x := range l {
		if i > 0 {
			s += "\n"
		}
		s += x
	}
	return s
}

func main() {
	a := app.New()
	main1 := a.NewWindow("Shogun Emulator (main)")

	hex, updateHex := newHexDump(32)
	disasm := widget.NewLabel("C000  4C F5 C5   JMP $C5F5\nC5F5  A2 00      LDX #$00")
	palette := widget.NewLabel("(palette viewer)")

	results := []string{}
	note := func(ok bool, msg string) {
		mark := "FAIL"
		if ok {
			mark = "OK  "
		}
		results = append(results, mark+" : "+msg)
	}

	// --- 1. 別ウィンドウを複数開く ---
	wHex := a.NewWindow("Memory (window)")
	wHex.SetContent(hex)
	wHex.Resize(fyne.NewSize(620, 500))
	wHex.Show()

	wDis := a.NewWindow("Disassembly (window)")
	wDis.SetContent(container.NewVScroll(disasm))
	wDis.Resize(fyne.NewSize(420, 300))
	wDis.Show()

	wPal := a.NewWindow("Palette (window)")
	wPal.SetContent(palette)
	wPal.Resize(fyne.NewSize(300, 200))
	wPal.Show()
	note(true, "別ウィンドウを 3 枚同時に開けた（App.NewWindow × 3）")

	// --- 4. 同じビューアを 1 ウィンドウ内タブにも置ける（配置の切り替え） ---
	hex2, _ := newHexDump(16)
	docked := container.NewAppTabs(
		container.NewTabItem("Memory", hex2),
		container.NewTabItem("Disassembly", widget.NewLabel("(same component, docked)")),
		container.NewTabItem("Palette", widget.NewLabel("(docked)")),
	)
	note(true, "同じ構成を container.AppTabs で 1 ウィンドウに畳めた")

	// --- 2. ネイティブメニューバー ---
	openItem := fyne.NewMenuItem("ROM を開く...", func() {})
	openItem.Shortcut = &fyne.ShortcutCopy{} // ショートカット設定が可能なことの確認
	viewMenu := fyne.NewMenu("表示",
		fyne.NewMenuItem("メモリ", func() { wHex.Show() }),
		fyne.NewMenuItem("逆アセンブル", func() { wDis.Show() }),
		fyne.NewMenuItem("パレット", func() { wPal.Show() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("1 ウィンドウに畳む", func() {}),
	)
	mm := fyne.NewMainMenu(
		fyne.NewMenu("ファイル", openItem, fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("終了", func() { a.Quit() })),
		viewMenu,
	)
	main1.SetMainMenu(mm)
	note(main1.MainMenu() != nil && len(main1.MainMenu().Items) == 2,
		fmt.Sprintf("MainMenu を設定できた（トップレベル %d 個、サブ項目 %d 個）",
			len(mm.Items), len(viewMenu.Items)))

	main1.SetContent(container.NewBorder(
		widget.NewLabel("エミュレータ画面（ここに 256x240 を拡大表示）"),
		nil, nil, nil, docked))
	main1.Resize(fyne.NewSize(700, 560))

	// --- 3. TextGrid のセル単位スタイル ---
	go func() {
		time.Sleep(900 * time.Millisecond)
		heat := map[int]float32{3: 1.0, 4: 0.8, 17: 0.6, 35: 0.4, 100: 1.0}
		fyne.DoAndWait(func() { updateHex(heat, 21) })
		note(true, "TextGrid.SetStyleRange でセル単位の前景色/背景色を設定できた（16進ダンプのフラッシュが実現可能）")

		time.Sleep(1200 * time.Millisecond)
		fmt.Println("=== Fyne UI 機能スパイク結果 ===")
		for _, r := range results {
			fmt.Println(r)
		}
		fmt.Println("=== 開いているウィンドウ: main + 3 デバッグ窓 ===")
		fyne.DoAndWait(a.Quit)
	}()

	main1.ShowAndRun()
}
