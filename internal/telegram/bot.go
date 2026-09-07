// Package telegram реализует пользовательский интерфейс бота.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	telebot "gopkg.in/telebot.v3"

	"github.com/Msey/price-tracking-bot/internal/config"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

const helpText = `Я слежу за ценами и пишу, когда они меняются.

Просто пришлите ссылку на товар — этого достаточно.

<b>Команды</b>
/list — что я отслеживаю
/del &lt;номер&gt; — снять товар с отслеживания
/help — эта справка

<b>Магазины</b>
Сейчас работает DNS. Wildberries, Ozon и Яндекс.Маркет на очереди.`

type Bot struct {
	bot   *telebot.Bot
	store *storage.Store
	cfg   config.Config
	log   *slog.Logger
}

// New собирает бота на long polling: вебхук потребовал бы публичного адреса
// и сертификата, а бот рассчитан на запуск с домашней машины.
func New(cfg config.Config, store *storage.Store, log *slog.Logger) (*Bot, error) {
	b := &Bot{store: store, cfg: cfg, log: log}

	tb, err := telebot.NewBot(telebot.Settings{
		Token:     cfg.BotToken,
		Poller:    &telebot.LongPoller{Timeout: 10 * time.Second},
		ParseMode: telebot.ModeHTML,
		OnError: func(err error, c telebot.Context) {
			log.Error("необработанная ошибка обработчика", "error", err)
			if c != nil {
				_ = c.Send("Что-то сломалось на моей стороне. Попробуйте ещё раз.")
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("telegram: подключение к API: %w", err)
	}
	b.bot = tb

	b.routes()
	return b, nil
}

// Username — имя бота, полезно для логов при старте.
func (b *Bot) Username() string { return b.bot.Me.Username }

func (b *Bot) routes() {
	b.bot.Use(b.accessMiddleware)

	b.bot.Handle("/start", b.handleStart)
	b.bot.Handle("/help", b.handleHelp)
	b.bot.Handle("/list", b.handleList)
	b.bot.Handle("/del", b.handleDelete)
	b.bot.Handle("/add", b.handleAdd)
	b.bot.Handle(telebot.OnText, b.handleText)

	// Кнопка «удалить» под каждым товаром в /list.
	unsub := (&telebot.ReplyMarkup{}).Data("", unsubUnique)
	b.bot.Handle(&unsub, b.handleUnsubButton)
}

// Start блокируется до отмены контекста.
func (b *Bot) Start(ctx context.Context) {
	go func() {
		<-ctx.Done()
		b.bot.Stop()
	}()
	b.bot.Start()
}

func (b *Bot) accessMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		sender := c.Sender()
		if sender == nil {
			return nil
		}
		if !b.cfg.Allowed(sender.ID) {
			b.log.Warn("отказано в доступе", "user_id", sender.ID, "username", sender.Username)
			return c.Send("Этот бот приватный.")
		}
		return next(c)
	}
}

func (b *Bot) handleStart(c telebot.Context) error {
	if _, err := b.store.EnsureUser(context.Background(), c.Chat().ID); err != nil {
		return err
	}
	return c.Send(helpText, telebot.NoPreview)
}

func (b *Bot) handleHelp(c telebot.Context) error {
	return c.Send(helpText, telebot.NoPreview)
}

// handleText принимает ссылку, присланную без команды.
func (b *Bot) handleText(c telebot.Context) error {
	text := strings.TrimSpace(c.Text())
	if text == "" {
		return nil
	}
	if !strings.Contains(text, "http://") && !strings.Contains(text, "https://") {
		return c.Send("Пришлите ссылку на товар или посмотрите /help.", telebot.NoPreview)
	}
	return b.add(c, text)
}

func (b *Bot) handleAdd(c telebot.Context) error {
	args := strings.TrimSpace(strings.Join(c.Args(), " "))
	if args == "" {
		return c.Send("Использование: /add &lt;ссылка на товар&gt;", telebot.NoPreview)
	}
	return b.add(c, args)
}

func (b *Bot) add(c telebot.Context, raw string) error {
	ref, err := sites.Parse(raw)
	if err != nil {
		return c.Send(explainParseError(err), telebot.NoPreview)
	}

	product, created, err := b.store.AddSubscription(
		context.Background(), c.Chat().ID,
		string(ref.Site), ref.ExternalKey, ref.URL, b.cfg.DefaultCity)
	if err != nil {
		return err
	}

	if !created {
		return c.Send("Этот товар уже в списке. Посмотреть всё — /list", telebot.NoPreview)
	}

	b.log.Info("добавлена подписка",
		"chat_id", c.Chat().ID, "site", ref.Site, "key", ref.ExternalKey)

	return c.Send(fmt.Sprintf(
		"Добавил в отслеживание.\n\n%s\nМагазин: %s\nГород: %s\n\n"+
			"Проверяю раз в %s и напишу, когда цена изменится.",
		linkTo(product), ref.Site.Title(), b.cfg.DefaultCity, humanDuration(b.cfg.CheckInterval),
	), telebot.NoPreview)
}

func (b *Bot) handleList(c telebot.Context) error {
	items, err := b.store.ListSubscriptions(context.Background(), c.Chat().ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return c.Send("Список пуст. Пришлите ссылку на товар, чтобы начать.", telebot.NoPreview)
	}

	markup := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	var sb strings.Builder

	fmt.Fprintf(&sb, "Отслеживаю товаров: %d\n", len(items))
	for i, item := range items {
		fmt.Fprintf(&sb, "\n%d. %s\n   %s", i+1, linkTo(item.Product), describePrice(item))
		rows = append(rows, markup.Row(markup.Data(
			fmt.Sprintf("🗑 %d", i+1), unsubUnique, strconv.FormatInt(item.Product.ID, 10))))
	}
	markup.Inline(rows...)

	return c.Send(sb.String(), markup, telebot.NoPreview)
}

// handleDelete удаляет товар по порядковому номеру из /list.
func (b *Bot) handleDelete(c telebot.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("Использование: /del &lt;номер из /list&gt;", telebot.NoPreview)
	}

	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n < 1 {
		return c.Send("Номер должен быть положительным числом. Посмотрите /list.", telebot.NoPreview)
	}

	items, err := b.store.ListSubscriptions(context.Background(), c.Chat().ID)
	if err != nil {
		return err
	}
	if n > len(items) {
		return c.Send(fmt.Sprintf("У вас всего %d товаров. Посмотрите /list.", len(items)), telebot.NoPreview)
	}

	item := items[n-1]
	if _, err := b.store.DeleteSubscription(context.Background(), c.Chat().ID, item.Product.ID); err != nil {
		return err
	}
	return c.Send("Снял с отслеживания:\n"+linkTo(item.Product), telebot.NoPreview)
}

const unsubUnique = "unsub"

func (b *Bot) handleUnsubButton(c telebot.Context) error {
	productID, err := strconv.ParseInt(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Не понял, какой это товар", ShowAlert: true})
	}

	removed, err := b.store.DeleteSubscription(context.Background(), c.Chat().ID, productID)
	if err != nil {
		return err
	}
	if !removed {
		return c.Respond(&telebot.CallbackResponse{Text: "Этого товара уже нет в списке"})
	}

	if err := c.Respond(&telebot.CallbackResponse{Text: "Снято с отслеживания"}); err != nil {
		return err
	}
	// Перерисовываем список, чтобы номера и кнопки не разъезжались с реальностью.
	return b.handleList(c)
}

func explainParseError(err error) string {
	switch {
	case errors.Is(err, sites.ErrNotSupported):
		return "Этот магазин я пока не умею: " + strings.TrimPrefix(err.Error(), sites.ErrNotSupported.Error()+": ") +
			".\nСейчас работает только DNS."
	case errors.Is(err, sites.ErrUnknownSite):
		return "Не узнаю этот магазин. Сейчас работает только DNS."
	case errors.Is(err, sites.ErrNotAProduct):
		return "Похоже, это не карточка товара. Нужна ссылка вида\nhttps://www.dns-shop.ru/product/..."
	default:
		return "Это не похоже на ссылку. Пришлите адрес карточки товара."
	}
}

func linkTo(p storage.Product) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, p.URL, escapeHTML(p.Title()))
}

func describePrice(t storage.Tracked) string {
	if !t.LastPriceKopecks.Valid {
		return "цена ещё не проверялась"
	}
	price := formatKopecks(t.LastPriceKopecks.Int64)
	if !t.LastCheckedAt.Valid {
		return price
	}
	return price + " (проверено " + t.LastCheckedAt.String + ")"
}

// formatKopecks печатает цену с разделением разрядов: 15999900 -> "159 999 ₽".
func formatKopecks(kopecks int64) string {
	whole := kopecks / 100
	digits := strconv.FormatInt(whole, 10)

	var sb strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			sb.WriteString("\u00a0")
		}
		sb.WriteRune(d)
	}
	if rem := kopecks % 100; rem != 0 {
		fmt.Fprintf(&sb, ",%02d", rem)
	}
	sb.WriteString("\u00a0₽")
	return sb.String()
}

func humanDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d ч", int(d.Hours()))
	}
	return fmt.Sprintf("%d мин", int(d.Minutes()))
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}
