package gui

import "testing"

func TestTelegramBotURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"price_change_tracking_bot", "https://t.me/price_change_tracking_bot"},
		{"@price_change_tracking_bot", "https://t.me/price_change_tracking_bot"},
		{"  @price_change_tracking_bot  ", "https://t.me/price_change_tracking_bot"},
		{"", ""},
		{"ab", ""},
		{"https://evil.example/x", ""},
		{"bot/../admin", ""},
		{"бот", ""},
	}
	for _, tt := range tests {
		if got := telegramBotURL(tt.in); got != tt.want {
			t.Errorf("telegramBotURL(%q) = %q, ожидалось %q", tt.in, got, tt.want)
		}
	}
}
