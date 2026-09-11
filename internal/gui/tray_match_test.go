package gui

import "testing"

func TestIsOurNotifyIconPath(t *testing.T) {
	self := `C:\Users\v.s\source\repos\price-tracking-bot\price-tracking-bot.exe`
	cases := []struct {
		stored string
		self   string
		want   bool
	}{
		{self, self, true},
		{`C:\Users\v.s\source\repos\price-tracking-bot\price-tracking-bot.exe`, self, true},
		{`"C:\Users\v.s\source\repos\price-tracking-bot\price-tracking-bot.exe"`, self, true},
		{`C:\Old\price-tracking-bot.exe`, self, true},
		{`c:\old\PRICE-TRACKING-BOT.EXE`, self, true},
		{`C:\Windows\System32\notepad.exe`, self, false},
		{`C:\Users\v.s\chrome.exe`, self, false},
		{"", self, false},
		{self, "", false},
		{`C:\tmp\guipreview.exe`, self, false},
	}
	for _, tc := range cases {
		if got := isOurNotifyIconPath(tc.stored, tc.self); got != tc.want {
			t.Errorf("isOurNotifyIconPath(%q, %q) = %v, want %v", tc.stored, tc.self, got, tc.want)
		}
	}
}
