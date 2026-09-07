package sites

import (
	"errors"
	"strings"
	"testing"
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
		{"ozon пока не умеем", "https://www.ozon.ru/product/noutbuk-123456/", ErrNotSupported},
		{"яндекс маркет пока не умеем", "https://market.yandex.ru/product--noutbuk/123", ErrNotSupported},
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
