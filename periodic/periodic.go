// Package periodic 提供「周期调度 + 多实例单飞」的通用任务组件。
//
// 背景：某些任务必须由业务服务自己执行（只有它拥有数据语义），例如按 DB 快照
// 重建缓存/榜单；但服务多实例部署时，若每台都各跑一遍，会带来 N 倍 DB 压力，
// 甚至互相覆盖（后写覆盖先写）。因此需要：周期调度 + 分布式锁单飞 + 单次超时 +
// 失败日志。这些逻辑与具体业务无关，收敛到本包供各服务复用，业务方只需提供
// "做什么"（一个 func(ctx) error）。
//
// 典型用法：
//
//	go periodic.Run(ctx, periodic.Options{
//	    Name:           "ranking:backfill",
//	    Interval:       time.Hour,
//	    Timeout:        time.Minute,
//	    LockTTL:        10 * time.Minute,
//	    RunImmediately: true,
//	    LeaderOnly:     true, // 重任务：多实例只跑一个
//	}, func(ctx context.Context) error {
//	    return svc.BackfillRanking(ctx)
//	})
package periodic

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mysunshines/gocommon/cache"
	"github.com/mysunshines/gocommon/log"
)

// Options 周期任务配置。
type Options struct {
	// Name 任务唯一名，用于分布式锁 key 与日志标识（如 "ranking:backfill"）。必填。
	Name string

	// Interval 执行间隔。必填且必须 > 0。
	Interval time.Duration

	// Timeout 单次执行超时，0 表示不限制。建议设置，避免任务挂死后长期占锁。
	Timeout time.Duration

	// LockTTL 分布式锁存活时间，仅 LeaderOnly 时生效。
	// 必须大于 Timeout，否则任务尚未跑完锁就过期，其他实例会并发进入。
	// 为 0 时自动取 Timeout*2；若 Timeout 也为 0，则取 5 分钟。
	LockTTL time.Duration

	// RunImmediately 为 true 时在启动后立即执行一次，不等第一个 Interval
	//（用于冷启动快速就绪，例如服务刚起来就要把榜单建好）。
	RunImmediately bool

	// LeaderOnly 为 true 时，同一时刻只有一个实例执行（基于 Redis 分布式锁）。
	// 适用于全量重建/回填这类重任务；幂等且轻量的任务可设为 false，
	// 让任一实例都能尽快完成（例如重注册一份配置）。
	LeaderOnly bool
}

// validate 校验必填项，返回可读的错误（配置错误属于编程错误，直接告警并返回）。
func (o Options) validate() error {
	if o.Name == "" {
		return fmt.Errorf("Name is required")
	}
	if o.Interval <= 0 {
		return fmt.Errorf("Interval must be > 0 (got %v)", o.Interval)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("Timeout must be >= 0 (got %v)", o.Timeout)
	}
	return nil
}

// effectiveLockTTL 计算实际使用的锁 TTL，保证 > Timeout。
func (o Options) effectiveLockTTL() time.Duration {
	if o.LockTTL > 0 {
		return o.LockTTL
	}
	if o.Timeout > 0 {
		return o.Timeout * 2
	}
	return 5 * time.Minute
}

// Run 启动周期任务并阻塞，直到 ctx 取消。应在 goroutine 中调用。
//
// 单次任务失败只记录日志，不会中断调度（下一个周期自动重试）。
// LeaderOnly 模式下，抢不到锁的实例直接跳过本轮（记 Debug 日志）。
func Run(ctx context.Context, opts Options, fn func(ctx context.Context) error) {
	if err := opts.validate(); err != nil {
		log.Errorf("[periodic] invalid options: %v", err)
		return
	}

	instanceID := instanceID()
	lockKey := "periodic:lock:" + opts.Name
	lockTTL := opts.effectiveLockTTL()

	runOnce := func() {
		execCtx := ctx
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}

		if opts.LeaderOnly {
			ok, err := cache.TryLock(execCtx, lockKey, instanceID, lockTTL)
			if err != nil {
				log.Warnf("[periodic] %s: acquire lock failed: %v", opts.Name, err)
				return
			}
			if !ok {
				// 已有实例在执行：本轮跳过，属预期行为（Debug 级，避免日志噪音）。
				log.Debugf("[periodic] %s: skipped, another instance is running", opts.Name)
				return
			}
			defer func() {
				// 解锁使用独立 ctx：execCtx 可能已因超时被取消，但解锁仍应尝试。
				uCtx, uCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer uCancel()
				if err := cache.Unlock(uCtx, lockKey, instanceID); err != nil {
					log.Warnf("[periodic] %s: release lock failed: %v", opts.Name, err)
				}
			}()
		}

		if err := fn(execCtx); err != nil {
			log.Warnf("[periodic] %s: run failed: %v", opts.Name, err)
			return
		}
		log.Infof("[periodic] %s: run ok", opts.Name)
	}

	if opts.RunImmediately {
		runOnce()
	}

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Infof("[periodic] %s: stopped", opts.Name)
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// instanceID 生成实例唯一标识，用于分布式锁归属校验：
// cache.Unlock 会校验该值，保证只释放自己持有的锁（不会误删其他实例的锁）。
func instanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
