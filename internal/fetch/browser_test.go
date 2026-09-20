package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestNewBrowserUsesAbsoluteProfile(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: "data/chrome-plain"})
	if !filepath.IsAbs(b.profileDir) {
		t.Fatalf("профиль должен быть абсолютным, получено %q", b.profileDir)
	}
	if !strings.Contains(filepath.ToSlash(b.profileDir), "data/chrome-plain") {
		t.Fatalf("профиль %q", b.profileDir)
	}
}

func TestDecodeProcessOutputCP1251(t *testing.T) {
	raw := "chrome failed to start:\n" + string([]byte{
		0xce, 0xea, 0xed, 0xee, 0x20, 0xe8, 0xeb, 0xe8, 0x20, 0xe2, 0xea, 0xeb, 0xe0, 0xe4, 0xea, 0xe0,
		0x20, 0xee, 0xf2, 0xea, 0xf0, 0xee, 0xfe, 0xf2, 0xf1, 0xff, 0x20, 0xe2, 0x20, 0xf2, 0xe5, 0xea,
		0xf3, 0xf9, 0xe5, 0xec, 0x20, 0xf1, 0xe5, 0xe0, 0xed, 0xf1, 0xe5, 0x20, 0xe1, 0xf0, 0xe0, 0xf3,
		0xe7, 0xe5, 0xf0, 0xe0, 0x2e,
	})
	got := decodeProcessOutput(raw)
	if !strings.Contains(got, "текущем сеансе") {
		t.Fatalf("не раскодировали вывод Chrome: %q", got)
	}
	if !looksLikeExistingSession(got) {
		t.Fatal("не узнали чужой сеанс Chrome")
	}
}

func TestMarkChromeExitedCleanly(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"profile":{"exited_cleanly":false,"exit_type":"Crashed"}}`)
	if err := os.WriteFile(filepath.Join(dir, "Local State"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pref, "Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	markChromeExitedCleanly(dir)
	got, err := os.ReadFile(filepath.Join(dir, "Local State"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, `"exited_cleanly":false`) || strings.Contains(s, `"exit_type":"Crashed"`) {
		t.Fatalf("не почистили краш: %s", s)
	}
	if !strings.Contains(s, `"exited_cleanly":true`) || !strings.Contains(s, `"exit_type":"Normal"`) {
		t.Fatalf("ожидался чистый выход: %s", s)
	}
}

func TestMarkChromeExitedCleanlyDisablesSessionRestore(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"session":{"restore_on_startup":1},"profile":{"exited_cleanly":true}}`)
	if err := os.WriteFile(filepath.Join(pref, "Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	markChromeExitedCleanly(dir)
	got, err := os.ReadFile(filepath.Join(pref, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"restore_on_startup":1`) {
		t.Fatalf("сессия всё ещё восстанавливается: %s", got)
	}
	if !strings.Contains(string(got), `"restore_on_startup":5`) {
		t.Fatalf("нужен restore_on_startup=5: %s", got)
	}
}

func TestCleanProfileJSONInsertsSessionKey(t *testing.T) {
	got, changed := cleanProfileJSON([]byte(`{"profile":{"name":"bot"}}`), true)
	if !changed {
		t.Fatal("ключ restore_on_startup нужно добавить")
	}
	if !strings.Contains(string(got), `"restore_on_startup":5`) {
		t.Fatalf("не вставили restore_on_startup: %s", got)
	}
	if !strings.Contains(string(got), `"name":"bot"`) {
		t.Fatalf("остальные настройки должны остаться: %s", got)
	}
}

// Ключи чужих значений трогать нельзя: строковая замена по всему файлу
// раньше могла попасть в чужую строку и испортить профиль.
func TestCleanProfileJSONLeavesForeignValuesAlone(t *testing.T) {
	raw := []byte(`{"profile":{"exit_type":"Normal","exited_cleanly":true},` +
		`"session":{"restore_on_startup":5},` +
		`"bookmark":{"note":"exit_type\":\"Crashed"}}`)
	if _, changed := cleanProfileJSON(raw, true); changed {
		t.Fatal("менять нечего, файл трогать не нужно")
	}
}

func TestCleanProfileJSONSkipsBrokenFile(t *testing.T) {
	if _, changed := cleanProfileJSON([]byte(`{"profile":`), true); changed {
		t.Fatal("непонятный файл переписывать нельзя")
	}
}

func TestApplyOffscreenPlacement(t *testing.T) {
	got, changed := applyOffscreenPlacement([]byte(`{"profile":{"name":"bot"}}`), -2400, -1200, 1280, 900)
	if !changed {
		t.Fatal("позицию окна нужно записать")
	}
	if !strings.Contains(string(got), `"left":-2400`) || !strings.Contains(string(got), `"maximized":false`) {
		t.Fatalf("placement: %s", got)
	}
	if !strings.Contains(string(got), `"name":"bot"`) {
		t.Fatalf("остальные настройки должны остаться: %s", got)
	}
	if _, changed := applyOffscreenPlacement(got, -2400, -1200, 1280, 900); changed {
		t.Fatal("повторная запись не должна трогать файл")
	}
}

func TestProfileInCommandLine(t *testing.T) {
	cmd := `"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\data\chrome-plain --no-first-run`
	if !profileInCommandLine(cmd, `C:\data\chrome-plain`) {
		t.Fatal("свой профиль")
	}
	if profileInCommandLine(cmd, `C:\Users\me\AppData\Local\Google\Chrome\User Data`) {
		t.Fatal("чужой профиль")
	}
	if profileInCommandLine("", `C:\data\chrome-plain`) || profileInCommandLine(cmd, "") {
		t.Fatal("пустые аргументы")
	}
}

func TestWriteFileAtomicReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Preferences")
	if err := os.WriteFile(path, []byte("старое"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("новое")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "новое" {
		t.Fatalf("содержимое %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("временный файл должен быть убран")
	}
}

func TestSameShopURL(t *testing.T) {
	ozon := "https://www.ozon.ru/product/germetik-2422341064"
	if !sameShopURL(ozon, "https://www.ozon.ru/product/germetik-2422341064/?_bctx=1") {
		t.Fatal("ozon с query")
	}
	if !sameShopURL(ozon, "https://ozon.ru/product/other-123") {
		t.Fatal("другой товар ozon в той же вкладке")
	}
	if !sameShopURL("https://www.ozon.ru/t/WcmKNaP", "https://www.ozon.ru/product/germetik-2422341064") {
		t.Fatal("короткая ozon после редиректа")
	}
	market := "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913"
	if !sameShopURL(market, "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913?nid=1") {
		t.Fatal("market с query")
	}
	wb := "https://www.wildberries.ru/catalog/949425394/detail.aspx"
	if !sameShopURL(wb, "https://www.wildberries.ru/catalog/949425394/detail.aspx?targetUrl=GP") {
		t.Fatal("wildberries с query")
	}
	if !sameShopURL(wb, "https://wildberries.ru/catalog/949425394") {
		t.Fatal("wildberries без www")
	}
	if sameShopURL(ozon, "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/") {
		t.Fatal("чужой магазин")
	}
	if sameShopURL(ozon, "about:blank") {
		t.Fatal("about:blank")
	}
	// Хост сравнивается точно: иначе страница на своём домене отдала бы
	// боту любую цену как цену Ozon.
	for _, spoof := range []string{
		"https://ozon.ru.example.com/product/1",
		"https://evil-ozon.ru.attacker.net/product/1",
		"https://notmarket.yandex.ru.example.com/card/1",
		"https://dns-shop.ru.example.com/product/1",
		"https://wildberries.ru.example.com/catalog/1",
	} {
		if sameShopURL(ozon, spoof) {
			t.Errorf("%s не должен считаться магазином", spoof)
		}
	}
}

func TestManifestBlocksHeavyMedia(t *testing.T) {
	raw, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"declarativeNetRequest"`) {
		t.Fatal("нужен declarativeNetRequest, чтобы резать картинки без CDP")
	}
	if !strings.Contains(s, `"path": "rules.json"`) {
		t.Fatal("нет rules.json в манифесте")
	}
	if !strings.Contains(s, `"document_end"`) {
		t.Fatal("съём цены должен начинаться на document_end, не после idle")
	}
	if strings.Contains(s, `"document_idle"`) {
		t.Fatal("document_idle ждёт тяжёлую загрузку")
	}

	rules, err := extFS.ReadFile("ext/rules.json")
	if err != nil {
		t.Fatal(err)
	}
	rs := string(rules)
	for _, want := range []string{`"block"`, `"image"`, `"media"`, `"font"`, "smartcaptcha", "px-cdn.net", "wildberries.ru"} {
		if !strings.Contains(rs, want) {
			t.Errorf("в rules.json нет %s", want)
		}
	}
}

func TestExtractTakesPriceEarly(t *testing.T) {
	raw, err := extFS.ReadFile("ext/extract.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "MutationObserver") {
		t.Fatal("нужен MutationObserver, чтобы поймать узел цены сразу")
	}
	if strings.Contains(s, "document.documentElement.innerHTML") {
		t.Fatal("полный innerHTML снова сериализует карточку")
	}
	if !strings.Contains(s, "банками ozon") {
		t.Fatal("нужен ценник с Ozon банком, не первый крупный")
	}
	if !strings.Contains(s, "кошельк") {
		t.Fatal("нужен ценник с WB Кошельком, не цена без кошелька")
	}
	if !strings.Contains(s, "productTitle") {
		t.Fatal("имя WB из h2.productTitle, не document.title")
	}
	if !strings.Contains(s, "6000") {
		t.Fatal("без подписи банка нужен запасной ценник из webPrice, иначе вкладка висит минуту")
	}
	if !strings.Contains(s, "isolatePrice") {
		t.Fatal("ценник нужно нормализовать, иначе parseDisplayedPrice молча отказывается")
	}
	// Правила разбора живут в настройках магазина: страница не должна
	// указывать боту, чему на ней верить.
	for _, sent := range []string{"skipLdjson", "bankGraceMs"} {
		if strings.Contains(s, sent) {
			t.Fatalf("страница не задаёт правила разбора, а тут есть %s", sent)
		}
	}
	if !strings.Contains(s, "stopPoll") {
		t.Fatal("после отправки цены секундный опрос страницы нужно глушить")
	}
	if !strings.Contains(s, "ozonResetOnNav") {
		t.Fatal("переход между карточками без перезагрузки должен сбрасывать отсрочку")
	}
	if strings.Count(s, ".innerText") != 1 {
		t.Fatal("innerText считает раскладку страницы: он должен вызываться в одном месте, под кэшем")
	}
}

func TestManifestAllowsLocalhostAnyPort(t *testing.T) {
	raw, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"http://127.0.0.1:*/*"`) {
		t.Fatalf("нужен порт * для локального HTTP, иначе Chrome не пустит :18732: %s", s)
	}
	if strings.Contains(s, `"http://127.0.0.1/*"`) {
		t.Fatal("http://127.0.0.1/* совпадает только с портом 80")
	}
}

func TestReadOrCreateTokenPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ext.token")
	a, err := readOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 16 {
		t.Fatalf("короткий токен %q", a)
	}
	b, err := readOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("токен не сохранился: %q vs %q", a, b)
	}
}

func TestStampManifestBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	src, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stampManifest(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"version": "1.0.0"`) {
		t.Fatalf("версия не сменилась: %s", got)
	}
	if !strings.Contains(string(got), `"http://127.0.0.1:*/*"`) {
		t.Fatal("потеряли host_permissions")
	}
	if !strings.Contains(string(got), `"path": "rules.json"`) && !strings.Contains(string(got), `"path":"rules.json"`) {
		t.Fatal("потеряли rules.json")
	}
}

func TestShowWithoutChromeDoesNotPanic(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir(), ChromePath: t.TempDir()})
	defer b.Close()
	b.Show(storage.Product{URL: "https://www.ozon.ru/t/WcmKNaP", Site: "ozon"})
}

func TestWaitJobSendsCloseWhenIdle(t *testing.T) {
	b := newTestBrowser(t)
	b.closeReq.Store(true)
	got := waitJobJSON(t, b, 2*time.Second)
	if got["action"] != "close" {
		t.Fatalf("ожидался action=close, получено %v", got)
	}
}

func TestWaitJobPrefersNewURLOverClose(t *testing.T) {
	b := newTestBrowser(t)
	b.closeReq.Store(true)
	b.job.Store(&extJob{
		url:  "https://www.ozon.ru/product/1",
		site: "ozon",
		bits: make(chan pageBits, 1),
	})
	got := waitJobJSON(t, b, 2*time.Second)
	if got["action"] == "close" {
		t.Fatal("новая карточка важнее закрытия")
	}
	if got["url"] != "https://www.ozon.ru/product/1" {
		t.Fatalf("url %v", got)
	}
	if got["focus"] == "1" {
		t.Fatal("замер не должен выводить окно на передний план")
	}
	if got["left"] == "" || got["top"] == "" {
		t.Fatalf("нужны координаты за экраном: %v", got)
	}
}

func TestWaitJobSendsFocusForShow(t *testing.T) {
	b := newTestBrowser(t)
	b.job.Store(&extJob{
		url:   "https://www.ozon.ru/product/1",
		site:  "ozon",
		focus: true,
		bits:  make(chan pageBits, 1),
	})
	got := waitJobJSON(t, b, 2*time.Second)
	if got["focus"] != "1" {
		t.Fatalf("щелчок по строке должен поднять окно: %v", got)
	}
}

func TestRequestCloseSkipsWhenWantFocus(t *testing.T) {
	b := newTestBrowser(t)
	b.wantFocus.Store(true)
	b.requestClose()
	if b.closeReq.Load() {
		t.Fatal("открытую человеком карточку закрывать нельзя")
	}
}

func TestChromeAliveAfterLauncherExit(t *testing.T) {
	c := &chromeProc{profileDir: filepath.Join(t.TempDir(), "chrome-plain")}
	c.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	c.chromeDead.Store(true)
	c.wantFocus.Store(true)
	if c.alive() {
		t.Fatal("закрытый Chrome профиля не должен считаться живым")
	}
	if c.wantFocus.Load() {
		t.Fatal("после закрытия окна фокус сбрасывается")
	}
}

func TestChromeAliveWhileStarting(t *testing.T) {
	c := &chromeProc{profileDir: filepath.Join(t.TempDir(), "chrome-plain")}
	c.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	if !c.alive() {
		t.Fatal("пока стартовый процесс жив, Chrome считается живым")
	}
}

func TestChromeProfileRunningEmpty(t *testing.T) {
	if chromeProfileRunning("") {
		t.Fatal("пустой профиль")
	}
	if chromeProfileRunning(filepath.Join(t.TempDir(), "missing-chrome-plain")) {
		t.Fatal("без процессов профиля")
	}
}

func TestPingRequiresAuth(t *testing.T) {
	b := newTestBrowser(t)
	res, err := http.Get("http://" + b.addr + "/ext/ping")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без токена код %d, нужен 401", res.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+b.addr+"/ext/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusNoContent {
		t.Fatalf("со своим токеном код %d", ok.StatusCode)
	}
}

func TestCORSRejectsWebsiteOrigin(t *testing.T) {
	b := newTestBrowser(t)
	req, err := http.NewRequest(http.MethodOptions, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("сайту отдали CORS Origin %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Private-Network"); got != "" {
		t.Fatalf("сайту отдали Private-Network %q", got)
	}
}

func TestCORSAllowsExtensionOrigin(t *testing.T) {
	b := newTestBrowser(t)
	origin := "chrome-extension://abcdefghijklmnopqrstuvwxyzabcdef"
	req, err := http.NewRequest(http.MethodOptions, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Origin %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("Private-Network %q", got)
	}
}

func newTestBrowser(t *testing.T) *Browser {
	t.Helper()
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	b.token = "tokentokentoken1"
	if err := b.serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

// markExtSeen зовут обработчики HTTP параллельно. Канал должен закрыться
// один раз: второй close уронил бы процесс.
func TestMarkExtSeenIsSafeInParallel(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	defer b.Close()
	b.setExtSeen(make(chan struct{}))

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			b.markExtSeen()
			_ = b.extReady()
		}()
	}
	close(start)
	wg.Wait()

	if !b.extReady() {
		t.Fatal("после markExtSeen расширение должно считаться на связи")
	}
	b.setExtSeen(nil)
	b.markExtSeen()
	if b.extReady() {
		t.Fatal("без канала расширение не на связи")
	}
}

func TestWaitJobPickupAlreadySent(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	defer b.Close()
	j := &extJob{}
	j.sent.Store(true)
	if !b.waitJobPickup(j, time.Second) {
		t.Fatal("уже взятая задача должна пройти сразу")
	}
}

func TestWaitJobPickupChromeDead(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	defer b.Close()
	j := &extJob{}
	start := time.Now()
	if b.waitJobPickup(j, time.Second) {
		t.Fatal("без chrome задачу никто не заберёт")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("мёртвый chrome должен быть виден сразу, без ожидания pickup")
	}
	if b.waitJobPickup(j, 0) {
		t.Fatal("нулевой timeout не должен ждать")
	}
}

// Close не должен ждать полный таймаут страницы: ожидание обрывается через
// b.done, иначе выход из бота вставал бы на минуту.
func TestCloseStopsPendingWait(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		select {
		case <-b.done:
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Error("ожидание не оборвалось")
		}
	}()
	b.Close()
	b.Close() // повторный вызов не должен паниковать
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close не разбудил ожидание")
	}
}

func TestPushLatestBitsKeepsNewest(t *testing.T) {
	ch := make(chan pageBits, 2)
	pushLatestBits(ch, pageBits{Title: "1"})
	pushLatestBits(ch, pageBits{Title: "2"})
	pushLatestBits(ch, pageBits{Title: "3"})
	var got []string
drain:
	for {
		select {
		case b := <-ch:
			got = append(got, b.Title)
		default:
			break drain
		}
	}
	if len(got) != 2 || got[len(got)-1] != "3" {
		t.Fatalf("снимки %v, последний должен быть 3", got)
	}
	pushLatestBits(nil, pageBits{Title: "x"})
}

func waitJobJSON(t *testing.T, b *Browser, timeout time.Duration) map[string]string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req = req.WithContext(ctx)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}
