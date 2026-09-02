// Package pool 提供轻量级 goroutine 池，支持并行执行、串行执行、分批混合执行与 Future 模式。
package pool

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// ============================================================================
// 类型定义
// ============================================================================

// Task 表示一个待执行的异步任务。
// 返回 value 和 error，由调用方按需解包。
type Task func(ctx context.Context) (interface{}, error)

// Result 表示单个任务的执行结果。
type Result struct {
	Index int         // 任务在传入切片中的位置
	Value interface{} // 任务返回值（成功时有效）
	Err   error       // 任务错误（失败时有效）
}

// Future 表示一个异步任务的结果句柄，调用 Get 阻塞等待直到任务完成。
type Future struct {
	done  chan struct{} // 完成信号，关闭表示任务已结束
	value interface{}   // 任务返回值（成功时有效）
	err   error         // 任务错误（失败时有效）
	once  sync.Once     // 保证结果仅被设置一次（由 worker 调用）
}

// Get 阻塞等待任务完成，返回结果与错误。
// 如果 ctx 被取消，会立即返回 context 错误。
func (f *Future) Get(ctx context.Context) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return f.value, f.err
	}
}

// setResult 内部方法，仅允许调用一次（由 worker 调用）。
func (f *Future) setResult(v interface{}, e error) {
	f.once.Do(func() {
		f.value = v
		f.err = e
		close(f.done)
	})
}

// Stats 池运行统计信息。
type Stats struct {
	ActiveWorkers  int64 // 当前正在执行的 goroutine 数
	TotalSubmitted int64 // 累计提交任务数
	TotalCompleted int64 // 累计完成任务数
	TotalFailed    int64 // 累计失败任务数
}

// ============================================================================
// Pool 实现
// ============================================================================

// Pool 是一个有界 goroutine 池，通过 semaphore 控制并发数。
type Pool struct {
	maxWorkers int           // 最大并发 worker 数
	sem        chan struct{} // 信号量，获取后才允许执行任务

	active    atomic.Int64 // 当前正在执行的任务数
	submitted atomic.Int64 // 累计提交任务数
	completed atomic.Int64 // 累计完成任务数
	failed    atomic.Int64 // 累计失败任务数
}

// Option 函数式选项。
type Option func(*Pool)

// WithMaxWorkers 设置最大并发 worker 数，<=0 则取 runtime.GOMAXPROCS(0)*2。
func WithMaxWorkers(n int) Option {
	return func(p *Pool) { p.maxWorkers = n }
}

// New 创建一个 goroutine 池。默认并发数 = GOMAXPROCS * 2。
func New(opts ...Option) *Pool {
	p := &Pool{maxWorkers: runtime.GOMAXPROCS(0) * 2}
	for _, o := range opts {
		o(p)
	}
	if p.maxWorkers <= 0 {
		p.maxWorkers = 1
	}
	p.sem = make(chan struct{}, p.maxWorkers)
	return p
}

// MaxWorkers 返回最大并发数。
func (p *Pool) MaxWorkers() int { return p.maxWorkers }

// Stats 返回当前运行统计快照。
func (p *Pool) Stats() Stats {
	return Stats{
		ActiveWorkers:  p.active.Load(),
		TotalSubmitted: p.submitted.Load(),
		TotalCompleted: p.completed.Load(),
		TotalFailed:    p.failed.Load(),
	}
}

// ============================================================================
// 并行执行 — 所有 task 并发执行，结果按原始顺序返回
// ============================================================================

// Parallel 并行执行 tasks 切片中的所有任务。
// 返回值与传入 tasks 一一对应（Index = 下标），调用方按 Index 定位。
//
// 内部使用 worker-pool 模式：仅创建 min(maxWorkers, len(tasks)) 个 goroutine，
// 各个 worker 从任务通道消费任务，避免 per-task goroutine 在高 QPS 下的创建开销。
//
// ctx 取消时，尚未开始的任务会被跳过（填充 ctx.Err()），
// 正在执行的任务会继续直到完成。
func (p *Pool) Parallel(ctx context.Context, tasks ...Task) []Result {
	results := make([]Result, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	// 确定 worker 数量，不超过任务数
	workers := p.maxWorkers
	if workers > len(tasks) {
		workers = len(tasks)
	}

	// 带索引的任务项
	type taskItem struct {
		idx  int
		task Task
	}

	// 将任务推入通道，让 worker 消费
	taskCh := make(chan taskItem, len(tasks))
	for i, t := range tasks {
		p.submitted.Add(1)
		taskCh <- taskItem{idx: i, task: t}
	}
	close(taskCh)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for item := range taskCh {
				// 每处理一个任务前检查 context 是否已取消
				if err := ctx.Err(); err != nil {
					results[item.idx] = Result{Index: item.idx, Err: err}
					continue
				}

				p.active.Add(1)
				val, err := item.task(ctx)
				results[item.idx] = Result{Index: item.idx, Value: val, Err: err}
				p.active.Add(-1)
				p.completed.Add(1)
				if err != nil {
					p.failed.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	return results
}

// ============================================================================
// 串行执行 — 按传入顺序逐个执行
// ============================================================================

// Serial 按顺序串行执行 tasks 切片中的所有任务。
// 任何一个任务返回 error 后，后续任务仍会继续执行（非 fail-fast）。
// ctx 取消时，剩余任务将被跳过并填充 ctx.Err()。
func (p *Pool) Serial(ctx context.Context, tasks ...Task) []Result {
	results := make([]Result, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	for i, t := range tasks {
		select {
		case <-ctx.Done():
			for j := i; j < len(tasks); j++ {
				results[j] = Result{Index: j, Err: ctx.Err()}
			}
			return results
		default:
		}

		p.submitted.Add(1)
		p.active.Add(1)

		val, err := t(ctx)
		results[i] = Result{Index: i, Value: val, Err: err}

		p.active.Add(-1)
		p.completed.Add(1)
		if err != nil {
			p.failed.Add(1)
		}
	}

	return results
}

// ============================================================================
// 混合执行 — 组间并行，组内串行
// ============================================================================

// Mixed 按分组执行：不同 group 之间并行，同一 group 内的 task 串行。
// 典型场景：多表写入，每张表的 SQL 必须有序执行，但多张表可以并发写入。
// 返回 [][]Result，外层对应传入的 groups，内层对应该 group 内每个 task 的顺序。
func (p *Pool) Mixed(ctx context.Context, groups ...[]Task) [][]Result {
	allResults := make([][]Result, len(groups))
	if len(groups) == 0 {
		return allResults
	}

	var wg sync.WaitGroup
	wg.Add(len(groups))

	for gi, group := range groups {
		gIdx := gi
		g := group

		select {
		case <-ctx.Done():
			for j := gIdx; j < len(groups); j++ {
				allResults[j] = []Result{{Index: 0, Err: ctx.Err()}}
				wg.Done()
			}
			return allResults
		default:
		}

		select {
		case <-ctx.Done():
			for j := gIdx; j < len(groups); j++ {
				allResults[j] = []Result{{Index: 0, Err: ctx.Err()}}
				wg.Done()
			}
			return allResults
		case p.sem <- struct{}{}:
		}

		p.submitted.Add(int64(len(g)))

		go func() {
			defer func() {
				<-p.sem
				wg.Done()
			}()

			groupResults := make([]Result, len(g))
			for ti, task := range g {
				select {
				case <-ctx.Done():
					for j := ti; j < len(g); j++ {
						groupResults[j] = Result{Index: j, Err: ctx.Err()}
					}
					allResults[gIdx] = groupResults
					return
				default:
				}

				p.active.Add(1)
				val, err := task(ctx)
				groupResults[ti] = Result{Index: ti, Value: val, Err: err}
				p.active.Add(-1)
				p.completed.Add(1)
				if err != nil {
					p.failed.Add(1)
				}
			}
			allResults[gIdx] = groupResults
		}()
	}

	wg.Wait()
	return allResults
}

// ============================================================================
// Future 模式 — 单任务异步提交，按需等待
// ============================================================================

// Submit 提交一个任务并立即返回 Future 句柄，不会阻塞。
// 任务可能在任意 goroutine 中执行，调用方通过 Future.Get(ctx) 获取结果。
// 如果 semaphore 已满，Submit 会阻塞直到有空位或 ctx 取消。
func (p *Pool) Submit(ctx context.Context, task Task) (*Future, error) {
	f := &Future{done: make(chan struct{})}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.sem <- struct{}{}:
	}

	p.submitted.Add(1)
	p.active.Add(1)

	go func() {
		defer func() {
			<-p.sem
			p.active.Add(-1)
		}()

		val, err := task(ctx)
		f.setResult(val, err)
		p.completed.Add(1)
		if err != nil {
			p.failed.Add(1)
		}
	}()

	return f, nil
}

// ============================================================================
// 批量 Future — 提交多个任务，全部返回 Future 句柄
// ============================================================================

// SubmitAll 批量提交任务，每个任务返回一个 Future 句柄。
// 如果 ctx 取消，尚未入队的任务不会提交，返回 error。
func (p *Pool) SubmitAll(ctx context.Context, tasks ...Task) ([]*Future, error) {
	futures := make([]*Future, len(tasks))
	for i, t := range tasks {
		f, err := p.Submit(ctx, t)
		if err != nil {
			return futures, fmt.Errorf("submit task[%d]: %w", i, err)
		}
		futures[i] = f
	}
	return futures, nil
}

// ============================================================================
// 便捷函数 — 无需显式创建 Pool，使用默认池
// ============================================================================

var defaultPool = New()

// Default 返回默认全局池（GOMAXPROCS*2 并发度）。
// 适用于不想自己管理 Pool 生命周期的简单场景。
func Default() *Pool { return defaultPool }

// Go 使用默认池并行执行，等同于 Default().Parallel(ctx, tasks...)。
func Go(ctx context.Context, tasks ...Task) []Result {
	return defaultPool.Parallel(ctx, tasks...)
}

// GoSerial 使用默认池串行执行，等同于 Default().Serial(ctx, tasks...)。
func GoSerial(ctx context.Context, tasks ...Task) []Result {
	return defaultPool.Serial(ctx, tasks...)
}

// GoMixed 使用默认池混合执行，等同于 Default().Mixed(ctx, groups...)。
func GoMixed(ctx context.Context, groups ...[]Task) [][]Result {
	return defaultPool.Mixed(ctx, groups...)
}
