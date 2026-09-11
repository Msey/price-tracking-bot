// Command mkicon собирает assets/app.ico из того же рисунка, что уходит в трей.
// Файл нужен один раз при смене иконки: из него rsrc делает ресурс для exe,
// чтобы иконка была видна в проводнике и в диспетчере задач.
//
//	go run ./cmd/mkicon
//	go run github.com/akavel/rsrc@v0.10.2 -ico assets/app.ico -manifest assets/app.manifest -arch amd64 -o cmd/bot/rsrc_windows_amd64.syso
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/Msey/price-tracking-bot/internal/gui"
)

// Размеры, которые Windows спрашивает у иконки: список, плитки, Alt-Tab.
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mkicon:", err)
		os.Exit(1)
	}
}

func run() error {
	data, err := buildICO(sizes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll("assets", 0o755); err != nil {
		return err
	}
	path := filepath.Join("assets", "app.ico")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d размеров, %d байт\n", path, len(sizes), len(data))
	return nil
}

// buildICO пишет ICONDIR с PNG-кадрами: Windows понимает их с Vista.
func buildICO(sizes []int) ([]byte, error) {
	frames := make([][]byte, 0, len(sizes))
	for _, n := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, gui.AppImage(n)); err != nil {
			return nil, fmt.Errorf("кадр %dpx: %w", n, err)
		}
		frames = append(frames, buf.Bytes())
	}

	const dirSize, entrySize = 6, 16
	offset := dirSize + entrySize*len(frames)

	out := new(bytes.Buffer)
	writeLE(out, uint16(0), uint16(1), uint16(len(frames)))
	for i, frame := range frames {
		// 256 пикселей в байт не влезают, поэтому записываются нулём.
		side := byte(sizes[i] % 256)
		out.Write([]byte{side, side, 0, 0})
		writeLE(out, uint16(1), uint16(32), uint32(len(frame)), uint32(offset))
		offset += len(frame)
	}
	for _, frame := range frames {
		out.Write(frame)
	}
	return out.Bytes(), nil
}

func writeLE(w *bytes.Buffer, vals ...any) {
	for _, v := range vals {
		_ = binary.Write(w, binary.LittleEndian, v)
	}
}
