package llm

import (
	"context"
	"time"
)

// RetryConfig 重试策略配置。
type RetryConfig struct {
	MaxAttempts int           // 最大尝试次数（含首次）
	BaseDelay   time.Duration // 首次重试前的基础延迟
	MaxDelay    time.Duration // 退避上限
	Multiplier  float64       // 退避乘数（指数退底）
}

// DefaultRetryConfig 默认重试策略：最多 3 次尝试，指数退避 400ms → 800ms → 1600ms。
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   400 * time.Millisecond,
	MaxDelay:    5 * time.Second,
	Multiplier:  2.0,
}

// DoWithRetry 按配置重试 f，直到成功、次数耗尽或 context 取消。
// isRetryable 判定当前错误是否值得重试。
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
