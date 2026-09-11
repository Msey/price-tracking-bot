package gui

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

var (
	iconBG     = color.RGBA{R: 0x21, G: 0x1c, B: 0x16, A: 0xff}
	iconGold   = color.RGBA{R: 0xe2, G: 0xb6, B: 0x57, A: 0xff}
	iconSeries = []int64{40, 55, 48, 70, 63, 88}
	// trashMuted — бронза, чтобы контур не спорил ни с тёмным фоном
	// строки, ни с золотыми узлами графика.
	trashMuted = color.RGBA{R: 0x8c, G: 0x78, B: 0x5e, A: 0xff}
)

// AppImage рисует иконку приложения: золотой график цены на тёмном фоне.
// Один и тот же рисунок идёт и в окно с треем, и в ресурс exe, чтобы
// в диспетчере задач была та же иконка, что в трее.
func AppImage(n int) image.Image {
	if n < 1 {
		n = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: iconBG}, image.Point{}, draw.Src)

	inset := n / 8
	pts := sparkline(n-inset*2, n/2, iconSeries)
	thick := n / 16
	if thick < 1 {
		thick = 1
	}
	for i := 1; i < len(pts); i++ {
		stroke(img,
			pts[i-1].X+inset, pts[i-1].Y+n/4,
			pts[i].X+inset, pts[i].Y+n/4,
			iconGold, thick)
	}
	return img
}

func trayImage() image.Image { return AppImage(32) }

// trashImage — контур урны как на значке: ручка, скруглённый бак, три прорези.
// Фон прозрачный, чтобы иконка легла на коричневую строку.
func trashImage(n int, c color.RGBA) image.Image {
	if n < 16 {
		n = 16
	}
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	thick := n / 12
	if thick < 2 {
		thick = 2
	}
	rad := n / 8
	if rad < 2 {
		rad = 2
	}

	hx0, hx1 := n*36/100, n*64/100
	hy0, hy1 := n*6/100, n*20/100
	roundRect(img, hx0, hy0, hx1-hx0, hy1-hy0, rad/2, c, thick)

	bx0, bx1 := n*22/100, n*78/100
	by0, by1 := n*22/100, n*92/100
	roundRect(img, bx0, by0, bx1-bx0, by1-by0, rad, c, thick)

	slotY0, slotY1 := n*38/100, n*76/100
	for _, fx := range []int{38, 50, 62} {
		x := n * fx / 100
		stroke(img, x, slotY0, x, slotY1, c, thick)
	}
	return img
}

func roundRect(img *image.RGBA, x, y, w, h, rad int, c color.RGBA, thick int) {
	if w < 2 || h < 2 {
		return
	}
	if rad < 1 {
		rad = 1
	}
	if rad*2 > w {
		rad = w / 2
	}
	if rad*2 > h {
		rad = h / 2
	}
	x2, y2 := x+w-1, y+h-1
	stroke(img, x+rad, y, x2-rad, y, c, thick)
	stroke(img, x+rad, y2, x2-rad, y2, c, thick)
	stroke(img, x, y+rad, x, y2-rad, c, thick)
	stroke(img, x2, y+rad, x2, y2-rad, c, thick)
	arc(img, x+rad, y+rad, rad, math.Pi, math.Pi*1.5, c, thick)
	arc(img, x2-rad, y+rad, rad, math.Pi*1.5, math.Pi*2, c, thick)
	arc(img, x2-rad, y2-rad, rad, 0, math.Pi*0.5, c, thick)
	arc(img, x+rad, y2-rad, rad, math.Pi*0.5, math.Pi, c, thick)
}

func arc(img *image.RGBA, cx, cy, r int, from, to float64, c color.RGBA, thick int) {
	if r < 1 {
		return
	}
	steps := r * 6
	if steps < 8 {
		steps = 8
	}
	span := to - from
	for i := 0; i <= steps; i++ {
		a := from + span*float64(i)/float64(steps)
		px := cx + int(math.Round(math.Cos(a)*float64(r)))
		py := cy + int(math.Round(math.Sin(a)*float64(r)))
		dot(img, px, py, c, thick)
	}
}

// stroke — отрезок Брезенхэма толщиной thick пикселей.
func stroke(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, thick int) {
	if thick < 1 {
		thick = 1
	}
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		dot(img, x0, y0, c, thick)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func dot(img *image.RGBA, x, y int, c color.RGBA, thick int) {
	for oy := 0; oy < thick; oy++ {
		for ox := 0; ox < thick; ox++ {
			img.SetRGBA(x+ox, y+oy, c)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
