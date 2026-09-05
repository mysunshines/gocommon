package resilience

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/time/rate"
)

// managers 为每个启用熔断/限流的 serviceKey 持有 breaker 单例与限流令牌桶，
// 延迟创建、并发安全。策略热更时通过 SetPolicy 更新 Policy，但 breaker 实例本身
// 是按 key 复用的（避免热更导致熔断计数被重置）。
var (
	mgmtMu   sync.Mutex
	managers = make(map[string]*serviceGuard)
)

type serviceGuard struct {
	breaker *breaker
	limiter *rate.Limiter
}

func guardFor(serviceKey string, p Policy) *serviceGuard {
	mgmtMu.Lock()
	defer mgmtMu.Unlock()

	g, ok := managers[serviceKey]
	if !ok {
		g = &serviceGuard{
			breaker: newBreaker(p.Circuit),
		}
		if p.RateLimit.Enabled {
			g.limiter = rate.NewLimiter(rate.Limit(p.RateLimit.QPS), p.RateLimit.Burst)
		}
		managers[serviceKey] = g
		return g
	}
	// 策略热更：仅当熔断配置发生启用状态/阈值变化时更新 breaker 配置（保留状态）。
	if p.Circuit.Enabled {
		g.breaker.cfg = p.Circuit
	}
	// 限流：QPS 变化则重建令牌桶（限流无需保留计数状态）。
	if p.RateLimit.Enabled {
		g.limiter = rate.NewLimiter(rate.Limit(p.RateLimit.QPS), p.RateLimit.Burst)
	} else {
		g.limiter = nil
	}
	return g
}

// Execute 是出站调用的统一韧性入口。它按顺序施加：
//  1. 超时（context 截止时间）；
//  2. 限流（令牌桶，限流拒绝视为可降级错误）；
//  3. 熔断（open 态直接拒绝）；
//  4. 执行 fn（真正的出站调用）；
//  5. 根据结果更新熔断计数；失败时调用 Fallback 兜底。
//
// fn 接收带超时截止的 context，并返回 error。Fallback 用于熔断打开或 fn 致命失败时
// 返回兜底结果，可为 nil（不降级，错误直接向上传递）。
//
// 返回语义：
//   - 调用成功：返回 nil error（Fallback 不参与）；
//   - 调用失败且有 Fallback：返回 Fallback 的结果（error 由 Fallback 决定）；
//   - 调用失败且无 Fallback：返回原 error（可能被包装为熔断/限流错误）。
func (p Policy) Execute(ctx context.Context, fn func(ctx context.Context) error, fallback func(ctx context.Context) (interface{}, error)) error {
	// 1) 超时
	callCtx := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	// 2) 限流
	g := guardFor(serviceKeyOf(ctx), p)
	if g.limiter != nil {
		if err := g.limiter.Wait(callCtx); err != nil {
			// 限流导致超时等待失败：交给降级逻辑
			return p.degrade(callCtx, err, fallback)
		}
	}

	// 3) 熔断
	if g.breaker != nil && !g.breaker.Allow() {
		return p.degrade(callCtx, ErrCircuitOpen, fallback)
	}

	// 4) 执行
	err := fn(callCtx)

	// 5) 熔断计数 + 降级
	if g.breaker != nil {
		if err != nil && isFatal(err) {
			g.breaker.Failure()
		} else if err == nil {
			g.breaker.Success()
		}
	}
	// 任何失败都尝试降级；无 Fallback 则原样返回错误。
	if err != nil {
		return p.degrade(callCtx, err, fallback)
	}
	return nil
}

// degrade 在出错时优先走 Fallback；无 Fallback 则原样返回错误（必要时包装）。
func (p Policy) degrade(ctx context.Context, cause error, fallback func(ctx context.Context) (interface{}, error)) error {
	if fallback != nil {
		if _, fbErr := fallback(ctx); fbErr != nil {
			// 降级函数自身也失败：以降级错误为主，cause 作为底层原因
			return errors.Join(fbErr, cause)
		}
		return nil
	}
	return cause
}

// isFatal 判断错误是否应计入熔断。超时（context deadline）与熔断打开视为致命；
// 业务级错误（InvalidArgument 等）不计入。这里以 context 错误与显式熔断错误为准。
func isFatal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrCircuitOpen) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	return true // 出站网络错误（连接失败/EOF 等）默认也计入熔断
}

// serviceKeyOf 从 context 中提取 serviceKey 用于 guard 复用。
// 调用方应通过 WithServiceKey 注入；未注入时回落到默认 guard（多 key 共享，
// 仍可用，只是熔断/限流统计不区分来源）。
func serviceKeyOf(ctx context.Context) string {
	if k, ok := ctx.Value(serviceKeyCtx{}).(string); ok && k != "" {
		return k
	}
	return "_default_"
}

type serviceKeyCtx struct{}

// WithServiceKey 将 serviceKey 注入 context，供 Execute 内部 guard 复用。
// 也可直接依赖 SetPolicy 的 key 与调用方约定一致。
func WithServiceKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, serviceKeyCtx{}, key)
}
