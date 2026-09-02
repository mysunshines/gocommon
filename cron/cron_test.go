package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestValidateCronExpr 测试Cron表达式校验功能
func TestValidateCronExpr(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"合法表达式-每秒", "* * * * * *", false},
		{"合法表达式-每分钟", "0 * * * * *", false},
		{"合法表达式-每天8点", "0 0 8 * * *", false},
		{"合法表达式-工作日9点", "0 0 9 * * 1-5", false},
		{"非法表达式-缺少字段", "* * * *", true},
		{"非法表达式-无效字符", "abc", true},
		{"非法表达式-超出范围", "60 * * * * *", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCronExpr(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCronExpr() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// mockExecutor 模拟任务执行器
type mockExecutor struct {
	execCount atomic.Int32
}

func (m *mockExecutor) Execute(_ context.Context, _ int64, _ string, _ string) error {
	m.execCount.Add(1)
	return nil
}

// mockStore 模拟任务存储
type mockStore struct {
	jobs []JobRecord
}

func (m *mockStore) LoadEnabledJobs() ([]JobRecord, error) {
	return m.jobs, nil
}

func (m *mockStore) UpdateLastRun(_ int64, _ time.Time) error {
	return nil
}

// TestCronService_AddJob 测试任务添加功能
func TestCronService_AddJob(t *testing.T) {
	executor := &mockExecutor{}
	store := &mockStore{}
	svc := NewCronService(executor, store)
	defer svc.Stop()

	// 添加合法任务
	err := svc.AddJob(1, "0 * * * * *", "test-cmd", "test-job")
	if err != nil {
		t.Errorf("AddJob() error = %v", err)
	}

	// 添加非法表达式任务
	err = svc.AddJob(2, "invalid-expr", "test-cmd", "invalid-job")
	if err == nil {
		t.Error("AddJob() 期望返回错误，但未返回")
	}
}

// TestCronService_RemoveJob 测试任务移除功能
func TestCronService_RemoveJob(t *testing.T) {
	executor := &mockExecutor{}
	store := &mockStore{}
	svc := NewCronService(executor, store)
	defer svc.Stop()

	// 添加任务
	svc.AddJob(1, "0 * * * * *", "test-cmd", "test-job")

	// 移除任务
	svc.RemoveJob(1)

	// 验证任务已移除（通过GetEntryID查询）
	_, ok := svc.GetEntryID(1)
	if ok {
		t.Error("RemoveJob() 任务未成功移除")
	}
}

// TestCronService_ExecuteJob 测试任务执行功能
func TestCronService_ExecuteJob(t *testing.T) {
	executor := &mockExecutor{}
	store := &mockStore{}
	svc := NewCronService(executor, store)
	defer svc.Stop()

	// 添加任务并等待执行（使用较短的间隔）
	svc.AddJob(1, "*/1 * * * * *", "test-cmd", "test-job")

	// 等待2秒，让任务有机会执行
	time.Sleep(2 * time.Second)

	// 验证执行次数（至少1次）
	if executor.execCount.Load() < 1 {
		t.Errorf("ExecuteJob() 执行次数 = %d, 期望 >= 1", executor.execCount.Load())
	}
}
