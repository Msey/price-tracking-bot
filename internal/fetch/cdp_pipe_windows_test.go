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

func TestForgetDropsPending(t *testing.T) {
	pc := &pipeChrome{pending: map[int]chan cdpMsg{7: make(chan cdpMsg, 1)}}
	pc.forget(7)
	if _, ok := pc.pending[7]; ok {
		t.Fatal("таймаут CDP не должен оставлять pending")
	}
}
