package gui

import (
	"bytes"
	"image"
	"image/png"

	"github.com/Msey/price-tracking-bot/internal/sites"
)

func siteImage(site sites.Site) image.Image {
	raw := sites.IconPNG(site)
	if len(raw) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	return img
}
