//go:build windows

package fetch

import (
	"bufio"
	"bytes"
	"testing"
)

func TestWriteReadCDP(t *testing.T) {
	var buf bytes.Buffer
	msg := []byte(`{"id":1}`)
	if err := writeCDP(&buf, msg); err != nil {
		t.Fatal(err)
	}
	if buf.Bytes()[buf.Len()-1] != 0 {
		t.Fatal("ожидался нулевой байт")
	}
	got, err := readCDP(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q", got)
	}
}
