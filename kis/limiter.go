package kis

import (
	"context"
	"sync"
	"time"
)

type limiter struct {
	mu       sync.Mutex
	clock    Clock
	interval time.Duration
	next     time.Time
}

func (l *limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.clock.Now()
	deadline := l.next
	if deadline.Before(now) {
		deadline = now
	}
	l.next = deadline.Add(l.interval)
	l.mu.Unlock()
	wait := deadline.Sub(now)
	if wait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.clock.After(wait):
		return nil
	}
}
