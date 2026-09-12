//go:build windows

package gui

import (
	"testing"

	"github.com/lxn/walk"
)

func TestButtonFaceOf(t *testing.T) {
	cases := []struct {
		primary, enabled, hover, pressed bool
		want                             buttonFace
	}{
		{true, true, false, false, facePrimary},
		{true, true, true, false, facePrimaryHot},
		{true, true, true, true, facePrimaryPress},
		{true, false, true, false, facePrimaryOff},
		{false, true, false, false, faceIdle},
		{false, true, true, false, faceHot},
		{false, true, true, true, facePress},
		{false, false, true, false, faceOff},
	}
	for _, tc := range cases {
		if got := buttonFaceOf(tc.primary, tc.enabled, tc.hover, tc.pressed); got != tc.want {
			t.Fatalf("primary=%v enabled=%v hover=%v pressed=%v: %v, ждали %v",
				tc.primary, tc.enabled, tc.hover, tc.pressed, got, tc.want)
		}
	}
}

func TestButtonTextColor(t *testing.T) {
	if buttonTextColor(facePrimary) != walk.RGB(22, 20, 16) {
		t.Fatal("главная кнопка — тёмный текст на золоте")
	}
	if buttonTextColor(faceIdle) != walk.RGB(226, 182, 87) {
		t.Fatal("вторичная кнопка — золотой текст")
	}
	if buttonTextColor(faceOff) != walk.RGB(154, 141, 122) {
		t.Fatal("выключенная кнопка — приглушённый текст")
	}
}
