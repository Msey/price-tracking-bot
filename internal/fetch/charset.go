package fetch

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// decodeProcessOutput чинит вывод Chrome на русской Windows (CP1251 в UTF-8 логе).
func decodeProcessOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if utf8.ValidString(s) {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if utf8.ValidString(line) {
			b.WriteString(line)
			continue
		}
		out, err := charmap.Windows1251.NewDecoder().String(line)
		if err != nil {
			b.WriteString(line)
			continue
		}
		b.WriteString(out)
	}
	return strings.TrimSpace(b.String())
}
