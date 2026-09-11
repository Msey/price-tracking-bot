// Package telegram реализует пользовательский интерфейс бота.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"time"

	telebot "gopkg.in/telebot.v3"

	"github.com/Msey/price-tracking-bot/internal/config"
	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
)

const (
	requestTimeout = 5 * time.Second
	unsubUnique    = "unsub"
	// listBudget — сколько байт списка отдаём под товары. У Telegram предел
	// сообщения 4096 символов, остаток оставлен на заголовок и хвост.
	listBudget = 3600
)

const helpText = `Я слежу за ценами и пишу, когда они меняются.

Просто пришлите ссылку на товар — этого достаточно.

<b>Команды</b>
/list — что я отслеживаю
/del &lt;номер&gt; — снять товар с отслеживания
/help — эта справка

<b>Магазины</b>
Сейчас работают DNS, Яндекс.Маркет и Ozon. DNS и Маркет проверяю раз в сутки, Ozon — раз в час; между карточками пауза, иначе магазин банит.

Wildberries на очереди.`

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

// Notify отправляет HTML-сообщение в личку. Нужен трекеру цен.
func (b *Bot) Notify(_ context.Context, chatID int64, message string) error {
	b.log.Info("отправка в Telegram", "chat_id", chatID, "bytes", len(message))
	_, err := b.bot.Send(telebot.ChatID(chatID), message, telebot.NoPreview)
	return err
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
		chat := c.Chat()
		if chat == nil || chat.Type != telebot.ChatPrivate {
			return c.Send("Я работаю только в личных сообщениях.")
		}
		return next(c)
	}
}

func (b *Bot) handleStart(c telebot.Context) error {
	b.log.Info("команда /start", "chat_id", c.Chat().ID, "user_id", c.Sender().ID)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if _, err := b.store.EnsureUser(ctx, c.Chat().ID); err != nil {
		return err
	}
	return c.Send(helpText, telebot.NoPreview)
}

func (b *Bot) handleHelp(c telebot.Context) error {
	b.log.Info("команда /help", "chat_id", c.Chat().ID)
	return c.Send(helpText, telebot.NoPreview)
}

func (b *Bot) handleText(c telebot.Context) error {
	text := strings.TrimSpace(c.Text())
	b.log.Info("сообщение в чат", "chat_id", c.Chat().ID, "text", clipLog(text, 180))
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
	b.log.Info("команда /add", "chat_id", c.Chat().ID, "args", clipLog(args, 180))
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

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	product, created, err := b.store.AddSubscription(
		ctx, c.Chat().ID,
		string(ref.Site), ref.ExternalKey, ref.URL, b.cfg.DefaultCity)
	if errors.Is(err, storage.ErrTooManySubscriptions) {
		return c.Send(fmt.Sprintf(
			"Уже отслеживаю %d товаров — это максимум. Снимите что-нибудь через /list или /del.",
			storage.MaxSubscriptions), telebot.NoPreview)
	}
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
		linkTo(product), html.EscapeString(ref.Site.Title()), html.EscapeString(b.cfg.DefaultCity), humanDuration(ref.Site.CheckInterval()),
	), telebot.NoPreview)
}

func (b *Bot) handleList(c telebot.Context) error {
	b.log.Info("команда /list", "chat_id", c.Chat().ID)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	text, markup, err := b.listContent(ctx, c.Chat().ID)
	if err != nil {
		return err
	}
	if markup == nil {
		return c.Send(text, telebot.NoPreview)
	}
	return c.Send(text, markup, telebot.NoPreview)
}

func (b *Bot) handleDelete(c telebot.Context) error {
	args := c.Args()
	b.log.Info("команда /del", "chat_id", c.Chat().ID, "args", strings.Join(args, " "))
	if len(args) == 0 {
		return c.Send("Использование: /del &lt;номер из /list&gt;", telebot.NoPreview)
	}

	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n < 1 {
		return c.Send("Номер должен быть положительным числом. Посмотрите /list.", telebot.NoPreview)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	items, err := b.store.ListSubscriptions(ctx, c.Chat().ID)
	if err != nil {
		return err
	}
	if n > len(items) {
		return c.Send(fmt.Sprintf("У вас всего %d товаров. Посмотрите /list.", len(items)), telebot.NoPreview)
	}

	item := items[n-1]
	removed, err := b.store.DeleteSubscription(ctx, c.Chat().ID, item.Product.ID)
	if err != nil {
		return err
	}
	if !removed {
		return c.Send("Этого товара уже нет в списке. Посмотрите /list.", telebot.NoPreview)
	}
	b.log.Info("подписка снята", "chat_id", c.Chat().ID, "product", item.Product.ID)
	return c.Send("Снял с отслеживания:\n"+linkTo(item.Product), telebot.NoPreview)
}

func (b *Bot) handleUnsubButton(c telebot.Context) error {
	productID, err := strconv.ParseInt(c.Data(), 10, 64)
	b.log.Info("кнопка отписки", "chat_id", c.Chat().ID, "product", productID)
	if err != nil || productID < 1 {
		return c.Respond(&telebot.CallbackResponse{Text: "Не понял, какой это товар", ShowAlert: true})
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	removed, err := b.store.DeleteSubscription(ctx, c.Chat().ID, productID)
	if err != nil {
		return err
	}

	if !removed {
		// Повторный щелчок по той же корзине: список на экране уже верный.
		// Если его переслать, Telegram ответит «message is not modified»,
		// и человек без причины получит «что-то сломалось».
		return c.Respond(&telebot.CallbackResponse{Text: "Этого товара уже нет в списке"})
	}
	if err := c.Respond(&telebot.CallbackResponse{Text: "Снято с отслеживания"}); err != nil {
		return err
	}

	listText, markup, err := b.listContent(ctx, c.Chat().ID)
	if err != nil {
		return err
	}
	if markup == nil {
		return c.Edit(listText, telebot.NoPreview)
	}
	return c.Edit(listText, markup, telebot.NoPreview)
}

func (b *Bot) listContent(ctx context.Context, chatID int64) (string, *telebot.ReplyMarkup, error) {
	items, err := b.store.ListSubscriptions(ctx, chatID)
	if err != nil {
		return "", nil, err
	}
	if len(items) == 0 {
		return "Список пуст. Пришлите ссылку на товар, чтобы начать.", nil, nil
	}

	markup := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	var sb strings.Builder

	fmt.Fprintf(&sb, "Отслеживаю товаров: %d\n", len(items))
	shown := 0
	for i, item := range items {
		entry := fmt.Sprintf("\n%d. %s\n   %s", i+1, linkTo(item.Product), html.EscapeString(describePrice(item)))
		// Названия приходят из магазинов и бывают длинными: на пределе
		// подписок список перестаёт влезать в сообщение, Telegram отвечает
		// 400, и человек больше не может ни посмотреть список, ни удалить
		// из него товар. Поэтому длина считается по факту.
		if sb.Len()+len(entry) > listBudget {
			break
		}
		sb.WriteString(entry)
		shown++
		rows = append(rows, markup.Row(markup.Data(
			fmt.Sprintf("🗑 %d", i+1), unsubUnique, strconv.FormatInt(item.Product.ID, 10))))
	}
	if shown < len(items) {
		fmt.Fprintf(&sb, "\n\nПоказаны первые %d из %d. Удалите лишние через 🗑 или /del.", shown, len(items))
	}
	markup.Inline(rows...)
	return sb.String(), markup, nil
}

func explainParseError(err error) string {
	switch {
	case errors.Is(err, sites.ErrNotSupported):
		name := strings.TrimPrefix(err.Error(), sites.ErrNotSupported.Error()+": ")
		return "Этот магазин я пока не умею: " + html.EscapeString(name) +
			".\nСейчас работают DNS, Яндекс.Маркет и Ozon."
	case errors.Is(err, sites.ErrUnknownSite):
		return "Не узнаю этот магазин. Сейчас работают DNS, Яндекс.Маркет и Ozon."
	case errors.Is(err, sites.ErrNotAProduct):
		return "Похоже, это не карточка товара. Нужна ссылка на товар DNS, Яндекс.Маркета или Ozon."
	default:
		return "Это не похоже на ссылку. Пришлите адрес карточки товара."
	}
}

func linkTo(p storage.Product) string { return view.TelegramLink(p) }

func describePrice(t storage.Tracked) string {
	if !t.LastPriceKopecks.Valid {
		return "цена ещё не проверялась"
	}
	price := money.FormatKopecks(t.LastPriceKopecks.Int64)
	if !t.LastCheckedAt.Valid {
		return price
	}
	return price + " (проверено " + t.LastCheckedAt.String + ")"
}

func humanDuration(d time.Duration) string {
	switch {
	case d == 24*time.Hour:
		return "сутки"
	case d == time.Hour:
		return "час"
	case d%time.Hour == 0:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	default:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	}
}

func clipLog(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
