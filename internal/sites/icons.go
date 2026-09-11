package sites

import _ "embed"

//go:embed icons/dns.png
var dnsPNG []byte

//go:embed icons/ozon.png
var ozonPNG []byte

//go:embed icons/yandex_market.png
var yandexMarketPNG []byte

// IconPNG возвращает PNG-иконку магазина, если она есть.
func IconPNG(s Site) []byte {
	switch s {
	case DNS:
		return dnsPNG
	case Ozon:
		return ozonPNG
	case YandexMarket:
		return yandexMarketPNG
	default:
		return nil
	}
}

// SitesWithIcons — магазины, для которых есть картинка в списке.
func SitesWithIcons() []Site {
	return []Site{DNS, Ozon, YandexMarket}
}
