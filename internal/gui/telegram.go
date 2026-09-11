package gui

import (
	"regexp"
	"strings"
)

// Telegram: 5–32 символа, начинается с буквы.
var telegramUsernameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

// telegramBotURL собирает https://t.me/<username>. Пустая строка — имя не годится.
func telegramBotURL(username string) string {
	name := strings.TrimPrefix(strings.TrimSpace(username), "@")
	if !telegramUsernameRe.MatchString(name) {
		return ""
	}
	return "https://t.me/" + name
}
