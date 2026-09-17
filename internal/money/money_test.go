package money

import (
	"errors"
	"testing"
)

func TestFormatKopecks(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0\u00a0₽"},
		{99, "0,99\u00a0₽"},
		{100, "1\u00a0₽"},
		{15999900, "159\u00a0999\u00a0₽"},
		// Отрицательная цена — признак сбоя разбора, и она должна быть видна,
		// а не выглядеть правдоподобным нулём.
		{-500, "−5\u00a0₽"},
		{-15999900, "−159\u00a0999\u00a0₽"},
	}
	for _, tt := range tests {
		if got := FormatKopecks(tt.in); got != tt.want {
			t.Errorf("FormatKopecks(%d) = %q, ожидалось %q", tt.in, got, tt.want)
		}
	}
}

func TestRubToKopecks(t *testing.T) {
	if got := RubToKopecks(159999); got != 15999900 {
		t.Errorf("RubToKopecks(159999) = %d", got)
	}
}

func TestParseRubles(t *testing.T) {
	ok := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"15000", 1_500_000},
		{"15 000", 1_500_000},
		{"15\u00a0000 ₽", 1_500_000},
		{"15000 руб", 1_500_000},
		{"15000,5", 1_500_050},
		{"99,99", 9999},
		{"0,01", 1},
	}
	for _, tt := range ok {
		got, err := ParseRubles(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseRubles(%q) = %d, %v, ожидалось %d", tt.in, got, err, tt.want)
		}
	}
	if _, err := ParseRubles("0"); !errors.Is(err, ErrAmountRange) {
		t.Errorf("ноль: %v", err)
	}
	if _, err := ParseRubles("-1"); !errors.Is(err, ErrAmountRange) {
		t.Errorf("минус: %v", err)
	}
	if _, err := ParseRubles("100000000"); !errors.Is(err, ErrAmountRange) {
		t.Errorf("потолок: %v", err)
	}
	if _, err := ParseRubles("12.34.56"); !errors.Is(err, ErrBadAmount) {
		t.Errorf("две точки: %v", err)
	}
	if _, err := ParseRubles("15.567"); !errors.Is(err, ErrBadAmount) {
		t.Errorf("три знака: %v", err)
	}
	if _, err := ParseRubles("цена 15000"); !errors.Is(err, ErrBadAmount) {
		t.Errorf("текст: %v", err)
	}
}
