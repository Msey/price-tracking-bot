//go:build !windows

package gui

import (
	"context"
	"errors"
)

// ErrUnavailable — на этой ОС нет окна и трея.
var ErrUnavailable = errors.New("графический интерфейс доступен только в Windows")

func Available() bool { return false }

func ActivateExisting() bool { return false }

func Run(context.Context, Options) error {
	return ErrUnavailable
}
