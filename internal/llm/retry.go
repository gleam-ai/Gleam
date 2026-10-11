package llm

import (
	"context"
	"time"
)

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
}

var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   400 * time.Millisecond,
	MaxDelay:    5 * time.Second,
	Multiplier:  2.0,
}

func DoWithRetry[T any](ctx context.Context, cfg RetryConfig, isRetryable func(error) bool, f func() (T, error)) (T, error) {
	var zero T
	delay := cfg.BaseDelay
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		result, err := f()
		if err == nil {
			return result, nil
		}
		if !isRetryable(err) || ctx.Err() != nil {
			return zero, err
		}
		if attempt == cfg.MaxAttempts-1 {
			return zero, err
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
	return zero, nil
}
