// Fyne 描画性能スパイク v2
// canvas.Raster の generate 関数が呼ばれた回数 = Fyne が実際にペイントした回数。
// これを数えることで「Refresh がダーティ化するだけ」問題を回避して真の描画レートを測る。
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	nesW = 256
	nesH = 240
)

func main() {
	scale := 3
	mode := "vsync" // vsync: 60Hz で Refresh / max: 全力で Refresh
	durSec := 6
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			scale = v
		}
	}
	if len(os.Args) > 2 {
		mode = os.Args[2]
	}
	if len(os.Args) > 3 {
		if v, err := strconv.Atoi(os.Args[3]); err == nil {
			durSec = v
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, nesW, nesH))

	var paints int64
	var genCost int64 // ns 累積

	a := app.New()
	w := a.NewWindow("Fyne perf spike v2")

	// generate 関数が呼ばれた = 実際にラスタライズされた
	raster := canvas.NewRaster(func(rw, rh int) image.Image {
		t := time.Now()
		atomic.AddInt64(&paints, 1)
		atomic.AddInt64(&genCost, int64(time.Since(t)))
		return img // 256x240 を返し、Fyne 側で rw x rh に拡大させる
	})
	raster.ScaleMode = canvas.ImageScalePixels
	raster.SetMinSize(fyne.NewSize(float32(nesW*scale), float32(nesH*scale)))

	label := widget.NewLabel("measuring...")
	w.SetContent(container.NewBorder(nil, label, nil, nil, raster))

	go func() {
		time.Sleep(1500 * time.Millisecond)

		// ウォームアップ
		wu := time.Now()
		fc := 0
		for time.Since(wu) < 1*time.Second {
			fillFrame(img, fc)
			fyne.Do(raster.Refresh)
			fc++
			time.Sleep(time.Second / 60)
		}

		atomic.StoreInt64(&paints, 0)
		atomic.StoreInt64(&genCost, 0)
		var gaps []time.Duration
		emuFrames := 0
		var cpuStart runtime.MemStats
		runtime.ReadMemStats(&cpuStart)

		t0 := time.Now()
		last := t0
		deadline := t0.Add(time.Duration(durSec) * time.Second)

		if mode == "vsync" {
			ticker := time.NewTicker(time.Second * 1000 / 60088) // 60.088 Hz ≒ NES の 60.0988Hz
			defer ticker.Stop()
			for now := range ticker.C {
				if now.After(deadline) {
					break
				}
				fillFrame(img, emuFrames)
				fyne.Do(raster.Refresh)
				emuFrames++
				gaps = append(gaps, now.Sub(last))
				last = now
			}
		} else {
			for time.Now().Before(deadline) {
				fillFrame(img, emuFrames)
				fyne.DoAndWait(raster.Refresh)
				emuFrames++
				now := time.Now()
				gaps = append(gaps, now.Sub(last))
				last = now
			}
		}

		elapsed := time.Since(t0)
		painted := atomic.LoadInt64(&paints)
		paintFPS := float64(painted) / elapsed.Seconds()
		refreshFPS := float64(emuFrames) / elapsed.Seconds()

		sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
		p := func(q float64) float64 {
			if len(gaps) == 0 {
				return 0
			}
			return float64(gaps[int(float64(len(gaps)-1)*q)].Microseconds()) / 1000
		}

		fmt.Printf("== scale=%dx (%dx%d px) mode=%s duration=%.2fs\n",
			scale, nesW*scale, nesH*scale, mode, elapsed.Seconds())
		fmt.Printf("   Refresh 呼び出し : %.2f /s (%d 回)\n", refreshFPS, emuFrames)
		fmt.Printf("   実ペイント回数   : %.2f /s (%d 回)  <-- これが真の描画レート\n", paintFPS, painted)
		fmt.Printf("   ペイント/Refresh : %.3f （1.0 なら取りこぼしなし）\n", paintFPS/refreshFPS)
		fmt.Printf("   Refresh 間隔     : p50=%.2f p95=%.2f p99=%.2f max=%.2f ms\n",
			p(0.50), p(0.95), p(0.99), p(1.0))
		switch {
		case paintFPS >= 58:
			fmt.Println("   JUDGE: 合格（実ペイントが 58fps 以上）")
		case paintFPS >= 45:
			fmt.Println("   JUDGE: 限界的")
		default:
			fmt.Println("   JUDGE: 不合格")
		}
		a.Quit()
	}()

	w.ShowAndRun()
}

func fillFrame(img *image.RGBA, f int) {
	pal := [4]color.RGBA{
		{0x57, 0x57, 0x57, 0xff},
		{0x4a, 0x9f, 0xff, 0xff},
		{0xf7, 0x6a, 0x63, 0xff},
		{0xbd, 0xee, 0xa2, 0xff},
	}
	for y := 0; y < nesH; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+nesW*4]
		for x := 0; x < nesW; x++ {
			c := pal[((x+f)>>3+(y>>3))&3]
			row[x*4+0] = c.R
			row[x*4+1] = c.G
			row[x*4+2] = c.B
			row[x*4+3] = 0xff
		}
	}
}
