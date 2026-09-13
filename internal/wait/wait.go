// Package wait — пауза, которая умеет прерваться.
package wait

import (
	"context"
	"time"
)

// Sleep ждёт d и сообщает, дождался ли конца: false — контекст отменён и
// продолжать не нужно. Нулевая пауза только проверяет контекст.
func Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
