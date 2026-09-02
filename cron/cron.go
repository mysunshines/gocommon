// Package cron 提供通用的定时任务调度功能
// 基于 robfig/cron 实现，支持从数据库加载任务、动态添加/删除任务等
package cron

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ValidateCronExpr 校验 Cron 表达式是否合法（支持秒级可选表达式）
func ValidateCronExpr(expr string) error {
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour |
			cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	_, err := parser.Parse(expr)
	return err
}

// JobExecutor 任务执行器接口，由业务层实现具体的任务执行逻辑
type JobExecutor interface {
	// Execute 执行任务，返回执行结果和错误
	Execute(ctx context.Context, jobID int64, name, command string) error
}

// JobStore 任务存储接口，由业务层实现具体的数据库操作
type JobStore interface {
	// LoadEnabledJobs 加载所有已启用的任务
	LoadEnabledJobs() ([]JobRecord, error)

	// UpdateLastRun 更新任务最后执行时间
	UpdateLastRun(jobID int64, lastRun time.Time) error
}

// JobRecord 任务记录，通用任务数据结构
type JobRecord struct {
	ID      int64  // 任务 ID
	Name    string // 任务名称
	Expr    string // cron 表达式
	Command string // 命令或任务参数
	Enabled bool   // 是否启用
}

// CronService 通用定时任务服务
type CronService struct {
	engine   *cron.Cron             // cron 引擎
	executor JobExecutor            // 任务执行器
	store    JobStore               // 任务存储
	jobMap   map[int64]cron.EntryID // 任务 ID -> Entry ID 映射
	mu       sync.RWMutex           // 保护 jobMap 并发安全
}

// NewCronService 创建定时任务服务
func NewCronService(executor JobExecutor, store JobStore) *CronService {
	svc := &CronService{
		engine:   cron.New(cron.WithSeconds()),
		executor: executor,
		store:    store,
		jobMap:   make(map[int64]cron.EntryID),
	}
	svc.loadEnabledJobs()
	svc.engine.Start()
	return svc
}

// Stop 停止定时任务服务
func (s *CronService) Stop() {
	s.engine.Stop()
}

// loadEnabledJobs 从存储加载已启用的任务
func (s *CronService) loadEnabledJobs() {
	jobs, err := s.store.LoadEnabledJobs()
	if err != nil {
		return
	}
	for _, job := range jobs {
		s.addToEngine(job.ID, job.Expr, job.Command, job.Name)
	}
}

// AddJob 添加任务到引擎
func (s *CronService) AddJob(id int64, expr, command, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果已存在，先移除
	if oldID, ok := s.jobMap[id]; ok {
		s.engine.Remove(oldID)
	}

	entryID, err := s.engine.AddFunc(expr, func() {
		s.executeJob(id, name, command)
	})
	if err != nil {
		return err
	}
	s.jobMap[id] = entryID
	return nil
}

// RemoveJob 从引擎移除任务
func (s *CronService) RemoveJob(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entryID, ok := s.jobMap[id]; ok {
		s.engine.Remove(entryID)
		delete(s.jobMap, id)
	}
}

// GetEntryID 获取任务对应的 Entry ID
func (s *CronService) GetEntryID(jobID int64) (cron.EntryID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entryID, ok := s.jobMap[jobID]
	return entryID, ok
}

// GetEntry 获取任务对应的 Entry
func (s *CronService) GetEntry(entryID cron.EntryID) cron.Entry {
	return s.engine.Entry(entryID)
}

// addToEngine 内部方法：添加任务到引擎
func (s *CronService) addToEngine(id int64, expr, command, name string) {
	entryID, err := s.engine.AddFunc(expr, func() {
		s.executeJob(id, name, command)
	})
	if err == nil {
		s.jobMap[id] = entryID
	}
}

// executeJob 执行具体任务
func (s *CronService) executeJob(id int64, name, command string) {
	// 更新最后执行时间
	_ = s.store.UpdateLastRun(id, time.Now())

	// 执行任务
	ctx := context.Background()
	if err := s.executor.Execute(ctx, id, name, command); err != nil {
		// 执行失败，可以记录日志
	}
}
