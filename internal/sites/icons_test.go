package sites

import (
	"bytes"
	"image/png"
	"testing"
)

func TestIconPNGKnownShops(t *testing.T) {
	for _, site := range []Site{DNS, Ozon, YandexMarket} {
		raw := IconPNG(site)
		if len(raw) == 0 {
			t.Fatalf("нет иконки %s", site)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s png: %v", site, err)
		}
		b := img.Bounds()
		if b.Dx() < 16 || b.Dy() < 16 {
			t.Errorf("%s слишком маленькая иконка: %dx%d", site, b.Dx(), b.Dy())
		}
	}
}

func TestIconPNGUnknown(t *testing.T) {
	if IconPNG("nope") != nil || IconPNG(Wildberries) != nil {
		t.Fatal("у неизвестного сайта иконки быть не должно")
	}
}
