package sites

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseDNS(t *testing.T) {
	const (
		productURL = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/"
		canonical  = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/"
		key        = "9ee3a4f41358d9cb"
	)

	tests := []struct {
		name string
		in   string
	}{
		{"обычная ссылка", productURL},
		{"utm-метки отбрасываются", productURL + "?utm_source=telegram&city=msk"},
		{"фрагмент отбрасывается", productURL + "#opinion"},
		{"без www", "https://dns-shop.ru/product/" + key + "/noutbuk/"},
		{"без завершающего слеша", "https://www.dns-shop.ru/product/" + key + "/noutbuk"},
		{"ссылка внутри текста", "смотри что нашёл " + productURL + " норм?"},
		{"в скобках с точкой", "карточка (" + productURL + ")."},
		{"заглавные буквы в id", "https://www.dns-shop.ru/product/9EE3A4F41358D9CB/Noutbuk/"},
		{"без slug", "https://www.dns-shop.ru/product/" + key},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) вернул ошибку: %v", tt.in, err)
			}
			if ref.Site != DNS {
				t.Errorf("Site = %q, ожидался %q", ref.Site, DNS)
			}
			if ref.ExternalKey != key {
				t.Errorf("ExternalKey = %q, ожидался %q", ref.ExternalKey, key)
			}
			if ref.URL != canonical {
				t.Errorf("URL = %q, ожидался канон %q", ref.URL, canonical)
			}
		})
	}
}

func TestParseRejectsHostTricks(t *testing.T) {
	const path = "/product/9ee3a4f41358d9cb/noutbuk/"
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"чужой поддомен", "https://evil.dns-shop.ru" + path, ErrUnknownSite},
		{"хост с суффиксом", "https://notdns-shop.ru" + path, ErrUnknownSite},
		{"userinfo", "https://evil@www.dns-shop.ru" + path, ErrNotALink},
		{"javascript", "javascript:alert(1)", ErrNotALink},
		{"data uri", "data:text/html,<script>", ErrNotALink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse(%q) вернул %v, ожидалась %v", tt.in, err, tt.want)
			}
		})
	}
}

func TestParseCanonicalURLHasNoUserPayload(t *testing.T) {
	ref, err := Parse(`https://www.dns-shop.ru/product/9ee3a4f41358d9cb/"onclick="alert(1)/`)
	if err != nil {
		t.Fatalf("Parse вернул ошибку: %v", err)
	}
	if strings.ContainsAny(ref.URL, `"<>`) {
		t.Errorf("канонический URL содержит HTML-метасимволы: %q", ref.URL)
	}
	if ref.URL != "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/" {
		t.Errorf("URL = %q", ref.URL)
	}
}

func TestParseIsStable(t *testing.T) {
	variants := []string{
		"https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/",
		"https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/?utm_medium=cpc",
		"https://dns-shop.ru/product/9ee3a4f41358d9cb/staryj-slug-tovara/",
		"https://www.dns-shop.ru/product/9EE3A4F41358D9CB/",
	}

	first, err := Parse(variants[0])
	if err != nil {
		t.Fatalf("Parse вернул ошибку: %v", err)
	}
	for _, v := range variants[1:] {
		ref, err := Parse(v)
		if err != nil {
			t.Fatalf("Parse(%q) вернул ошибку: %v", v, err)
		}
		if ref.ExternalKey != first.ExternalKey || ref.URL != first.URL {
			t.Errorf("Parse(%q) = %+v, ожидалось %+v", v, ref, first)
		}
	}
}

func TestParseYandexMarket(t *testing.T) {
	const (
		productURL = "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913"
		canonical  = "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913"
		key        = "4638722913"
	)

	tests := []struct {
		name string
		in   string
		url  string
	}{
		{"карточка с slug", productURL, canonical},
		{"utm отбрасываются", productURL + "?utm_source=telegram&sku=1", canonical},
		{"фрагмент отбрасывается", productURL + "#reviews", canonical},
		{"www", "https://www.market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913", canonical},
		{"мобильный хост", "https://m.market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913", canonical},
		{"без slug", "https://market.yandex.ru/card/4638722913", "https://market.yandex.ru/card/4638722913"},
		{"старый product--", "https://market.yandex.ru/product--begovaya-dorozhka-sportflag-glow-run-a/4638722913", canonical},
		{"старый /product/id", "https://market.yandex.ru/product/4638722913", "https://market.yandex.ru/card/4638722913"},
		{"внутри текста", "смотри " + productURL + " цена?", canonical},
		{"заглавные в slug", "https://market.yandex.ru/card/Begovaya-Dorozhka-Sportflag-Glow-Run-A/4638722913", canonical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) вернул ошибку: %v", tt.in, err)
			}
			if ref.Site != YandexMarket {
				t.Errorf("Site = %q, ожидался %q", ref.Site, YandexMarket)
			}
			if ref.ExternalKey != key {
				t.Errorf("ExternalKey = %q, ожидался %q", ref.ExternalKey, key)
			}
			if ref.URL != tt.url {
				t.Errorf("URL = %q, ожидался %q", ref.URL, tt.url)
			}
		})
	}
}

func TestParseYandexIsStable(t *testing.T) {
	variants := []string{
		"https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913",
		"https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913?utm_medium=cpc",
		"https://www.market.yandex.ru/product--begovaya-dorozhka-sportflag-glow-run-a/4638722913",
	}

	first, err := Parse(variants[0])
	if err != nil {
		t.Fatalf("Parse вернул ошибку: %v", err)
	}
	for _, v := range variants[1:] {
		ref, err := Parse(v)
		if err != nil {
			t.Fatalf("Parse(%q) вернул ошибку: %v", v, err)
		}
		if ref.ExternalKey != first.ExternalKey || ref.URL != first.URL {
			t.Errorf("Parse(%q) = %+v, ожидалось %+v", v, ref, first)
		}
	}
}

func TestParseYandexRejectsHostTricks(t *testing.T) {
	const path = "/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913"
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"чужой поддомен", "https://evil.market.yandex.ru" + path, ErrUnknownSite},
		{"хост с суффиксом", "https://notmarket.yandex.ru" + path, ErrUnknownSite},
		{"userinfo", "https://evil@market.yandex.ru" + path, ErrNotALink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse(%q) вернул %v, ожидалась %v", tt.in, err, tt.want)
			}
		})
	}
}

func TestParseYandexCanonicalHasNoUserPayload(t *testing.T) {
	ref, err := Parse(`https://market.yandex.ru/card/foo!onclick=alert/4638722913`)
	if err != nil {
		t.Fatalf("Parse вернул ошибку: %v", err)
	}
	if strings.ContainsAny(ref.URL, `"<>!`) {
		t.Errorf("канонический URL содержит лишние символы: %q", ref.URL)
	}
	if ref.ExternalKey != "4638722913" {
		t.Errorf("ExternalKey = %q", ref.ExternalKey)
	}
	if ref.URL != "https://market.yandex.ru/card/4638722913" {
		t.Errorf("небезопасный slug не должен попасть в канон: %q", ref.URL)
	}
}

func TestParseOzon(t *testing.T) {
	const (
		productURL = "https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064"
		canonical  = "https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064"
		key        = "2422341064"
	)
	tests := []struct {
		name string
		in   string
		url  string
	}{
		{"обычная ссылка", productURL, canonical},
		{"utm отбрасываются", productURL + "?from=share&utm_source=telegram", canonical},
		{"фрагмент отбрасывается", productURL + "#reviews", canonical},
		{"без www", "https://ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064", canonical},
		{"мобильный хост", "https://m.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064", canonical},
		{"со слешем", productURL + "/", canonical},
		{"как прислали из приложения", "https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064/", canonical},
		{"без slug", "https://www.ozon.ru/product/2422341064", "https://www.ozon.ru/product/2422341064"},
		{"внутри текста", "вот " + productURL + " глянь", canonical},
		{"заглавные в slug", "https://www.ozon.ru/product/Germetik-Akrilovyy-Moment-420-gr-Belyy-Universalnyy-Morozostoykiy-2422341064", canonical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) вернул ошибку: %v", tt.in, err)
			}
			if ref.Site != Ozon {
				t.Errorf("Site = %q, ожидался %q", ref.Site, Ozon)
			}
			if ref.ExternalKey != key {
				t.Errorf("ExternalKey = %q, ожидался %q", ref.ExternalKey, key)
			}
			if ref.URL != tt.url {
				t.Errorf("URL = %q, ожидался %q", ref.URL, tt.url)
			}
		})
	}
}

func TestParseOzonShortLink(t *testing.T) {
	const (
		canonical = "https://www.ozon.ru/t/WcmKNaP"
		key       = "t:wcmknap"
	)
	tests := []string{
		"https://ozon.ru/t/WcmKNaP",
		"https://www.ozon.ru/t/WcmKNaP",
		"https://m.ozon.ru/t/WcmKNaP/",
		"https://www.ozon.ru/t/WcmKNaP?from=share",
		"держи https://ozon.ru/t/WcmKNaP.",
	}
	for _, in := range tests {
		ref, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if ref.Site != Ozon || ref.ExternalKey != key || ref.URL != canonical {
			t.Errorf("Parse(%q) = %+v, ожидались site=ozon key=%s url=%s", in, ref, key, canonical)
		}
	}
}

func TestParseOzonDoesNotTakeGramsAsID(t *testing.T) {
	ref, err := Parse("https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ExternalKey == "420" {
		t.Fatal("взяли «420» из названия, а не id товара")
	}
	if ref.ExternalKey != "2422341064" {
		t.Fatalf("ExternalKey = %q", ref.ExternalKey)
	}
}

func TestParseOzonIsStable(t *testing.T) {
	variants := []string{
		"https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064",
		"https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064/?from=share",
		"https://ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064",
	}
	first, err := Parse(variants[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variants[1:] {
		ref, err := Parse(v)
		if err != nil {
			t.Fatalf("Parse(%q): %v", v, err)
		}
		if ref.ExternalKey != first.ExternalKey || ref.URL != first.URL {
			t.Errorf("Parse(%q) = %+v, ожидалось %+v", v, ref, first)
		}
	}
}

func TestParseOzonRejectsHostTricks(t *testing.T) {
	const path = "/product/germetik-2422341064"
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"чужой поддомен", "https://evil.ozon.ru" + path, ErrUnknownSite},
		{"хост с суффиксом", "https://notozon.ru" + path, ErrUnknownSite},
		{"userinfo", "https://evil@www.ozon.ru" + path, ErrNotALink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse(%q) вернул %v, ожидалась %v", tt.in, err, tt.want)
			}
		})
	}
}

func TestParseOzonCanonicalHasNoUserPayload(t *testing.T) {
	ref, err := Parse(`https://www.ozon.ru/product/foo!onclick=alert-2422341064`)
	if err != nil {
		t.Fatalf("Parse вернул ошибку: %v", err)
	}
	if strings.ContainsAny(ref.URL, `"<>!`) {
		t.Errorf("канонический URL содержит лишние символы: %q", ref.URL)
	}
	if ref.ExternalKey != "2422341064" {
		t.Errorf("ExternalKey = %q", ref.ExternalKey)
	}
	if ref.URL != "https://www.ozon.ru/product/2422341064" {
		t.Errorf("небезопасный slug не должен попасть в канон: %q", ref.URL)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"пустая строка", "", ErrNotALink},
		{"просто текст", "привет, найди мне ноутбук", ErrNotALink},
		{"не http", "ftp://www.dns-shop.ru/product/9ee3a4f41358d9cb/x/", ErrNotALink},
		{"неизвестный магазин", "https://example.com/product/123/", ErrUnknownSite},
		{"wildberries пока не умеем", "https://www.wildberries.ru/catalog/12345/detail.aspx", ErrNotSupported},
		{"главная ozon", "https://www.ozon.ru/", ErrNotAProduct},
		{"короткий код ozon /t/", "https://ozon.ru/t/ab", ErrNotAProduct},
		{"категория ozon", "https://www.ozon.ru/category/germetiki-12345/", ErrNotAProduct},
		{"короткий id ozon", "https://www.ozon.ru/product/noutbuk-123/", ErrNotAProduct},
		{"короткий id маркета", "https://market.yandex.ru/product--noutbuk/123", ErrNotAProduct},
		{"главная маркета", "https://market.yandex.ru/", ErrNotAProduct},
		{"главная страница dns", "https://www.dns-shop.ru/", ErrNotAProduct},
		{"категория, а не товар", "https://www.dns-shop.ru/catalog/17a892f816404e77/noutbuki/", ErrNotAProduct},
		{"короткий id", "https://www.dns-shop.ru/product/abc/", ErrNotAProduct},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("Parse(%q) вернул %v, ожидалась %v", tt.in, err, tt.want)
			}
		})
	}
}

func TestCheckInterval(t *testing.T) {
	if got := Ozon.CheckInterval(); got != time.Hour {
		t.Errorf("ozon = %s, ожидался час", got)
	}
	if got := DNS.CheckInterval(); got != 24*time.Hour {
		t.Errorf("dns = %s, ожидались сутки", got)
	}
	if got := YandexMarket.CheckInterval(); got != 24*time.Hour {
		t.Errorf("yandex = %s, ожидались сутки", got)
	}
	if got := Wildberries.CheckInterval(); got != 24*time.Hour {
		t.Errorf("wildberries = %s, ожидались сутки", got)
	}
}
