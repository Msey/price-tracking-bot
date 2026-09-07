package sites

import (
	"errors"
	"testing"
)

func TestParseDNS(t *testing.T) {
	const (
		productURL = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/"
		key        = "9ee3a4f41358d9cb"
	)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"обычная ссылка", productURL, productURL},
		{"utm-метки отбрасываются", productURL + "?utm_source=telegram&city=msk", productURL},
		{"фрагмент отбрасывается", productURL + "#opinion", productURL},
		{"без www", "https://dns-shop.ru/product/" + key + "/noutbuk/", "https://www.dns-shop.ru/product/" + key + "/noutbuk/"},
		{"без завершающего слеша", "https://www.dns-shop.ru/product/" + key + "/noutbuk", "https://www.dns-shop.ru/product/" + key + "/noutbuk/"},
		{"ссылка внутри текста", "смотри что нашёл " + productURL + " норм?", productURL},
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
			if ref.URL != tt.want {
				t.Errorf("URL = %q, ожидался %q", ref.URL, tt.want)
			}
		})
	}
}

// Один и тот же товар, присланный по-разному, обязан дать один ключ,
// иначе в базе появятся дубликаты и мы будем дёргать магазин лишний раз.
func TestParseIsStable(t *testing.T) {
	variants := []string{
		"https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/",
		"https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/?utm_medium=cpc",
		"https://dns-shop.ru/product/9ee3a4f41358d9cb/staryj-slug-tovara/",
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
		if ref.ExternalKey != first.ExternalKey {
			t.Errorf("Parse(%q).ExternalKey = %q, ожидался %q", v, ref.ExternalKey, first.ExternalKey)
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
