package gui

import (
	"image"
	"image/color"
	"image/draw"
)

var (
	iconBG     = color.RGBA{R: 0x21, G: 0x1c, B: 0x16, A: 0xff}
	iconGold   = color.RGBA{R: 0xe2, G: 0xb6, B: 0x57, A: 0xff}
	iconSeries = []int64{40, 55, 48, 70, 63, 88}
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
