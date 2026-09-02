// Package scheduler 提供一个轻量级、零外部依赖的定时调度器。
// 支持三种调度规格：
//   - daily HH:MM           每天指定时间执行
//   - weekly <Dow> HH:MM    每周指定星期与时刻执行（Dow 为 Mon..Sun 或 *）
//   - every <duration>      每隔一段时间执行（如 every 6h）
//
// 调度器通过信号量限制并发执行数量，未获取信号的触发会被跳过而非堆积，
// 从而在高并发场景下保持确定性行为。配合外部分布式锁可实现高可用。
package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// JobFunc 是调度任务执行函数。
type JobFunc func(ctx context.Context) error

type specKind int

const (
	kindDaily specKind = iota
	kindWeekly
	kindEvery
)

type spec struct {
	kind    specKind       // 调度类型：daily / weekly / every
	hour    int            // 触发小时（daily/weekly 用）
	minute  int            // 触发分钟（daily/weekly 用）
	weekday int            // 0=Sunday..6=Saturday, -1=任意（weekly 用）
	every   time.Duration  // every 模式的间隔时长
	loc     *time.Location // 解释时间的时区
}

type jobEntry struct {
	name string    // 任务名（用于日志/错误回调）
	fn   JobFunc   // 任务执行函数
	next time.Time // 下一次触发时刻（已计算）
	spec spec      // 调度规格
}

// Scheduler 定时调度器。
type Scheduler struct {
	mu       sync.Mutex           // 保护 jobs 并发读写
	jobs     []*jobEntry          // 已注册任务列表
	sem      chan struct{}        // 并发信号量，限制同时执行的任务数
	stop     chan struct{}        // 停止信号，关闭后退出调度循环
	loc      *time.Location       // 调度时区
	interval time.Duration        // 内部轮询间隔（默认 1 分钟）
	wg       sync.WaitGroup       // 等待正在执行的任务完成
	onError  func(name string, err error) // 任务出错回调
}

// Option 配置项。
type Option func(*Scheduler)

// WithConcurrency 设置最大并发执行数。
func WithConcurrency(n int) Option {
	return func(s *Scheduler) {
		if n > 0 {
			s.sem = make(chan struct{}, n)
		}
	}
}

// WithLocation 设置调度时区（影响 daily/weekly 的解释）。
func WithLocation(loc *time.Location) Option {
	return func(s *Scheduler) {
		if loc != nil {
			s.loc = loc
		}
	}
}

// WithInterval 设置内部轮询间隔（默认 1 分钟）。
func WithInterval(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.interval = d
		}
	}
}

// OnError 设置任务执行出错回调。
func OnError(f func(name string, err error)) Option {
	return func(s *Scheduler) { s.onError = f }
}

// New 创建调度器。
func New(opts ...Option) *Scheduler {
	s := &Scheduler{
		sem:      make(chan struct{}, 8),
		stop:     make(chan struct{}),
		loc:      time.Local,
		interval: time.Minute,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// AddJob 注册一个定时任务，specStr 支持 daily/weekly/every 三种格式。
func (s *Scheduler) AddJob(name, specStr string, fn JobFunc) error {
	sp, err := parseSpec(specStr, s.loc)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &jobEntry{
		name: name,
		fn:   fn,
		spec: sp,
		next: sp.nextFrom(time.Now()),
	})
	return nil
}

// Start 启动调度循环（非阻塞）。
func (s *Scheduler) Start() {
	go s.loop()
}

// Stop 停止调度并等待所有正在执行的任务完成。
func (s *Scheduler) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	s.mu.Lock()
	jobs := make([]*jobEntry, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.Unlock()

	for _, j := range jobs {
		if now.Before(j.next) {
			continue
		}
		due := j.next
		j.next = j.spec.nextFrom(now)
		s.dispatch(j, due)
	}
}

func (s *Scheduler) dispatch(j *jobEntry, due time.Time) {
	select {
	case s.sem <- struct{}{}:
	default:
		if s.onError != nil {
			s.onError(j.name, fmt.Errorf("scheduler at capacity, skip %s (due %v)", j.name, due))
		}
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.sem }()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := j.fn(ctx); err != nil {
			if s.onError != nil {
				s.onError(j.name, err)
			}
		}
	}()
}

// ----------------------------------------------------------------------------
// 规格解析
// ----------------------------------------------------------------------------

func parseSpec(raw string, loc *time.Location) (spec, error) {
	s := strings.TrimSpace(raw)
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "every "):
		d, err := time.ParseDuration(strings.TrimSpace(s[len("every "):]))
		if err != nil {
			return spec{}, fmt.Errorf("invalid every duration %q: %w", raw, err)
		}
		return spec{kind: kindEvery, every: d, loc: loc}, nil
	case strings.HasPrefix(lower, "daily "):
		h, m, err := parseHMinute(strings.TrimSpace(s[len("daily "):]), raw)
		if err != nil {
			return spec{}, err
		}
		return spec{kind: kindDaily, hour: h, minute: m, loc: loc}, nil
	case strings.HasPrefix(lower, "weekly "):
		rest := strings.TrimSpace(s[len("weekly "):])
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			return spec{}, fmt.Errorf("invalid weekly spec %q", raw)
		}
		wd, err := parseWeekday(parts[0])
		if err != nil {
			return spec{}, err
		}
		h, m, err := parseHMinute(parts[1], raw)
		if err != nil {
			return spec{}, err
		}
		return spec{kind: kindWeekly, hour: h, minute: m, weekday: wd, loc: loc}, nil
	default:
		return spec{}, fmt.Errorf("unsupported schedule %q (use 'daily HH:MM', 'weekly <Dow> HH:MM', or 'every <duration>')", raw)
	}
}

func parseHMinute(s, raw string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time %q in spec %q", s, raw)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", raw)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", raw)
	}
	return h, m, nil
}

func parseWeekday(s string) (int, error) {
	m := map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6, "*": -1}
	if v, ok := m[strings.ToLower(s)]; ok {
		return v, nil
	}
	return 0, fmt.Errorf("invalid weekday %q (use Mon..Sun or *)", s)
}

// nextFrom 计算从 now 开始的下一个触发时刻。
func (sp spec) nextFrom(now time.Time) time.Time {
	switch sp.kind {
	case kindEvery:
		return now.Add(sp.every)
	case kindDaily:
		return nextAtTime(now, sp.hour, sp.minute, -1, sp.loc)
	case kindWeekly:
		return nextAtTime(now, sp.hour, sp.minute, sp.weekday, sp.loc)
	default:
		return now
	}
}

func nextAtTime(now time.Time, h, m, wd int, loc *time.Location) time.Time {
	candidate := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
	for i := 0; i < 8; i++ {
		if candidate.After(now) {
			if wd < 0 || int(candidate.Weekday()) == wd {
				return candidate
			}
		}
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}
