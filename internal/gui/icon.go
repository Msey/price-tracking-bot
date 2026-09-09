package gui

import (
	"image"
	"image/color"
	"image/draw"
)

func trayImage() image.Image {
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	bg := color.RGBA{R: 0x21, G: 0x1c, B: 0x16, A: 0xff}
	gold := color.RGBA{R: 0xe2, G: 0xb6, B: 0x57, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	pts := sparkline(24, 16, []int64{40, 55, 48, 70, 63, 88})
	for i := 1; i < len(pts); i++ {
		stroke(img, pts[i-1].X+4, pts[i-1].Y+8, pts[i].X+4, pts[i].Y+8, gold)
	}
	return img
}

func stroke(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
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
		img.SetRGBA(x0, y0, c)
		img.SetRGBA(x0+1, y0, c)
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

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
