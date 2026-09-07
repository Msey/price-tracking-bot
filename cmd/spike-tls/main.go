// Command spike-tls checks whether DNS-Shop's QRATOR protection can be passed
// with a spoofed Chrome TLS fingerprint, which would let us skip a headless
// browser on the hot path.
package main

import (
	"fmt"
	"io"
	"regexp"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	homeURL    = "https://www.dns-shop.ru/"
	productURL = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14-5301anxefmb-p-seryj/"
	chromeUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
)

var (
	ldPriceRe  = regexp.MustCompile(`"price"\s*:\s*(\d+)`)
	cssPriceRe = regexp.MustCompile(`product-buy__price[^>]*>([^<]{1,40})`)
)

type attempt struct {
	name    string
	profile profiles.ClientProfile
	warmUp  bool // visit the homepage first to collect cookies
}

func main() {
	attempts := []attempt{
		{"Chrome_144", profiles.Chrome_144, false},
		{"Chrome_144 + warm-up", profiles.Chrome_144, true},
		{"Chrome_152", profiles.Chrome_152, false},
		{"Chrome_152 + warm-up", profiles.Chrome_152, true},
		{"Chrome_133 + warm-up", profiles.Chrome_133, true},
		{"Chrome_120 + warm-up", profiles.Chrome_120, true},
	}

	for _, a := range attempts {
		run(a)
		time.Sleep(3 * time.Second)
	}
}

func run(a attempt) {
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(a.profile),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
	)
	if err != nil {
		fmt.Printf("%-24s CLIENT ERROR %v\n", a.name, err)
		return
	}

	if a.warmUp {
		if code, _, err := get(client, homeURL, ""); err != nil {
			fmt.Printf("%-24s warm-up error: %v\n", a.name, err)
		} else {
			fmt.Printf("%-24s warm-up %s -> %d\n", a.name, homeURL, code)
		}
		time.Sleep(2 * time.Second)
	}

	start := time.Now()
	code, body, err := get(client, productURL, homeURL)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("%-24s ERROR %v\n\n", a.name, err)
		return
	}

	result := "BLOCKED"
	if m := ldPriceRe.FindStringSubmatch(body); m != nil {
		result = "JSON-LD price=" + m[1]
	} else if m := cssPriceRe.FindStringSubmatch(body); m != nil {
		result = "CSS price=" + m[1]
	}

	fmt.Printf("%-24s status=%d bytes=%d in %v -> %s\n\n",
		a.name, code, len(body), elapsed.Round(time.Millisecond), result)
}

func get(client tls_client.HttpClient, url, referer string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}

	req.Header = http.Header{
		"sec-ch-ua":                 {`"Chromium";v="144", "Google Chrome";v="144", "Not?A_Brand";v="24"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {chromeUA},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {"ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7"},
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest",
			"referer", "accept-encoding", "accept-language", "cookie",
		},
		http.PHeaderOrderKey: {":method", ":authority", ":scheme", ":path"},
	}
	if referer != "" {
		req.Header.Set("referer", referer)
		req.Header.Set("sec-fetch-site", "same-origin")
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}
