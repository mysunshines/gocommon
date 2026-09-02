package resilience

import (
	"sync"
	"time"
)

// breaker 是一个轻量熔断器状态机，支持 closed / open / half-open 三态。
// 设计为无第三方依赖、按 serviceKey 单例持有（见 manager 里的 newBreaker）。
//
// 状态流转：
//   - closed：正常放行，统计窗口内错误率超 ErrorThreshold 则转 open；
//   - open：直接拒绝（返回 ErrCircuitOpen），持续 Timeout 后转 half-open；
//   - half-open：放行至多 MaxRequests 个探测请求，成功率达预期则转 closed，
//     否则继续 open。
type breaker struct {
	cfg CircuitConfig

	mu sync.Mutex

	state        state
	openedAt     time.Time // 进入 open 的时间
	halfProbes   int       // half-open 状态下已放行的探测数
	since        time.Time // 当前统计窗口起点
	total, fails int       // 当前窗口内的总请求数与失败数
}

type state int8

const (
	stateClosed state = iota
	stateOpen
	stateHalfOpen
)

// ErrCircuitOpen 表示熔断处于打开态，请求被直接拒绝（走降级或返回错误）。
var ErrCircuitOpen = &circuitError{"circuit breaker is open"}

type circuitError struct{ msg string }

func (e *circuitError) Error() string { return e.msg }

// newBreaker 根据配置构造熔断器。Enabled=false 时 call 永远放行。
func newBreaker(cfg CircuitConfig) *breaker {
	return &breaker{
		cfg:   cfg,
		state: stateClosed,
		since: time.Now(),
	}
}

// Allow 判断当前是否放行一次请求。返回 false 即熔断打开（调用方应走降级/报错）。
func (b *breaker) Allow() bool {
	if !b.cfg.Enabled {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateOpen:
		if time.Since(b.openedAt) >= b.cfg.Timeout {
			b.state = stateHalfOpen
			b.halfProbes = 0
			return true
		}
		return false
	case stateHalfOpen:
		if b.halfProbes >= b.cfg.MaxRequests {
			return false
		}
		b.halfProbes++
		return true
	default: // closed
		return true
	}
}

// Success 记录一次成功调用，用于 half-open 探测恢复。
func (b *breaker) Success() {
	if !b.cfg.Enabled {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateHalfOpen:
		b.state = stateClosed
		b.resetWindow()
	case stateClosed:
		b.total++
		b.rollWindow()
	}
}

// Failure 记录一次失败调用，触发 closed 态错误率判断或维持 open。
func (b *breaker) Failure() {
	if !b.cfg.Enabled {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateHalfOpen:
		b.toOpen()
	case stateClosed:
		b.total++
		b.fails++
		b.rollWindow()
	}
}

func (b *breaker) rollWindow() {
	if time.Since(b.since) >= b.cfg.Interval {
		b.total, b.fails = 1, 1
		b.since = time.Now()
		return
	}
	if b.total > 0 && float64(b.fails)/float64(b.total) >= b.cfg.ErrorThreshold {
		b.toOpen()
	}
}

func (b *breaker) toOpen() {
	b.state = stateOpen
	b.openedAt = time.Now()
}

func (b *breaker) resetWindow() {
	b.total, b.fails = 0, 0
	b.since = time.Now()
}
