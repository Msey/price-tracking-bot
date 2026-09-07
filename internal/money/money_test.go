package money

import "testing"

func TestFormatKopecks(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0\u00a0₽"},
		{99, "0,99\u00a0₽"},
		{100, "1\u00a0₽"},
		{15999900, "159\u00a0999\u00a0₽"},
		{-500, "0\u00a0₽"},
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
