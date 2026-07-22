// Package retry 提供带指数退避与抖动的可重试执行器。
// 用于邮件发送、Prometheus 查询等可能瞬时失败的操作，保证高可用。
package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// PermanentError 包装一个不可重试的错误，一旦返回即停止重试。
type PermanentError struct {
	Err error
}

func (p *PermanentError) Error() string { return p.Err.Error() }

// Unwrap 支持 errors.Is/As。
func (p *PermanentError) Unwrap() error { return p.Err }

// Permanent 将一个错误标记为不可重试。
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent 判断错误是否为不可重试错误。
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*PermanentError)
	return ok
}

// Options 重试策略配置。
type Options struct {
	Attempts    int                       // 最大尝试次数（含首次），<=0 时默认为 3
	Delay       time.Duration             // 初始退避间隔
	MaxDelay    time.Duration             // 退避上限
	Factor      float64                   // 退避放大系数
	Jitter      bool                      // 是否启用随机抖动，避免惊群
	ShouldRetry func(error) bool          // 自定义是否重试（返回 false 立即停止）
}

// Do 以 ctx 为生命周期边界执行 fn，失败按策略退避重试。
// 返回最后一次执行的错误；若 fn 返回 PermanentError 或 ShouldRetry 判定不重试则立即返回。
func Do(ctx context.Context, fn func() error, opts Options) error {
	if opts.Attempts <= 0 {
		opts.Attempts = 3
	}
	if opts.Delay <= 0 {
		opts.Delay = 200 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 10 * time.Second
	}
	if opts.Factor <= 0 {
		opts.Factor = 2.0
	}

	var lastErr error
	delay := opts.Delay
	for attempt := 1; attempt <= opts.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if IsPermanent(lastErr) {
			return lastErr
		}
		if opts.ShouldRetry != nil && !opts.ShouldRetry(lastErr) {
			return lastErr
		}
		if attempt == opts.Attempts {
			break
		}

		wait := delay
		if opts.Jitter {
			wait += time.Duration(rand.Int63n(int64(delay)))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = time.Duration(math.Min(float64(delay)*opts.Factor, float64(opts.MaxDelay)))
	}
	return lastErr
}
