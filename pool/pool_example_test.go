package pool

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// 示例 1：并行执行 — 同时查询多个数据源
// ============================================================================

func ExamplePool_Parallel() {
	ctx := context.Background()

	// 模拟：同时查询多张表的数据
	tasks := []Task{
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(30 * time.Millisecond)
			return map[string]int{"articles": 100}, nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(20 * time.Millisecond)
			return map[string]int{"comments": 500}, nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(40 * time.Millisecond)
			// 模拟一个数据源出错
			return nil, errors.New("stats service unreachable")
		},
	}

	results := Default().Parallel(ctx, tasks...)

	// 结果按传入顺序返回
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("[%d] error: %v\n", r.Index, r.Err)
		} else {
			fmt.Printf("[%d] value: %v\n", r.Index, r.Value)
		}
	}
	// 输出示例:
	// [0] value: map[articles:100]
	// [1] value: map[comments:500]
	// [2] error: stats service unreachable
}

// ============================================================================
// 示例 2：并行执行 + 超时控制
// ============================================================================

func ExampleNew() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tasks := []Task{
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(50 * time.Millisecond)
			return "fast-task-ok", nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(200 * time.Millisecond) // 超过超时
			return "slow-task", nil
		},
	}

	p := New(WithMaxWorkers(4))
	results := p.Parallel(ctx, tasks...)

	for _, r := range results {
		fmt.Printf("[%d] value=%v err=%v\n", r.Index, r.Value, r.Err)
	}
	// [0] value=fast-task-ok err=<nil>
	// [1] value=<nil> err=context deadline exceeded (慢任务被跳过)
}

// ============================================================================
// 示例 3：串行执行 — 有依赖的任务必须顺序执行
// ============================================================================

func ExamplePool_Serial() {
	ctx := context.Background()

	// 模拟：发文章流程，必须先校验再保存再同步
	var draftID int

	tasks := []Task{
		func(ctx context.Context) (interface{}, error) {
			// 步骤1：保存草稿
			draftID = 42
			return draftID, nil
		},
		func(ctx context.Context) (interface{}, error) {
			// 步骤2：校验内容（依赖 draftID）
			if draftID == 0 {
				return nil, errors.New("invalid draft")
			}
			return "validated", nil
		},
		func(ctx context.Context) (interface{}, error) {
			// 步骤3：同步到搜索引擎
			time.Sleep(10 * time.Millisecond)
			return "synced", nil
		},
	}

	results := Default().Serial(ctx, tasks...)
	for _, r := range results {
		fmt.Printf("[%d] %v (err=%v)\n", r.Index, r.Value, r.Err)
	}
	// [0] 42 (err=<nil>)
	// [1] validated (err=<nil>)
	// [2] synced (err=<nil>)
}

// ============================================================================
// 示例 4：Future 模式 — 先提交后等待，灵活编排
// ============================================================================

func ExampleFuture() {
	ctx := context.Background()
	p := New(WithMaxWorkers(4))

	// 提交 3 个任务，立即拿到 Future
	f1, _ := p.Submit(ctx, func(ctx context.Context) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return "user:alice", nil
	})
	f2, _ := p.Submit(ctx, func(ctx context.Context) (interface{}, error) {
		time.Sleep(30 * time.Millisecond)
		return 42, nil
	})
	f3, _ := p.Submit(ctx, func(ctx context.Context) (interface{}, error) {
		time.Sleep(70 * time.Millisecond)
		return nil, errors.New("third party api failed")
	})

	// 在等待期间可以做其他事...
	fmt.Println("tasks submitted, doing other work...")

	// 按需获取结果
	v1, _ := f1.Get(ctx)
	v2, _ := f2.Get(ctx)
	v3, err := f3.Get(ctx)

	fmt.Printf("f1=%v, f2=%v, f3=%v (err=%v)\n", v1, v2, v3, err)
	// f1=user:alice, f2=42, f3=<nil> (err=third party api failed)
}

// ============================================================================
// 示例 5：混合执行 — 组间并行，组内串行
// ============================================================================

func ExamplePool_Mixed() {
	ctx := context.Background()

	// 场景：发布文章时，需要同时写入 3 张表。
	// 每张表的操作必须有序（先校验再写入），但不同表可以并发。
	groups := [][]Task{
		// 表 A：articles
		{
			func(ctx context.Context) (interface{}, error) {
				return "validate article", nil
			},
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(20 * time.Millisecond)
				return "insert article", nil
			},
		},
		// 表 B：tags
		{
			func(ctx context.Context) (interface{}, error) {
				return "validate tags", nil
			},
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(15 * time.Millisecond)
				return "insert tags", nil
			},
		},
		// 表 C：stats
		{
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(10 * time.Millisecond)
				return "update stats", nil
			},
		},
	}

	p := New(WithMaxWorkers(4))
	allResults := p.Mixed(ctx, groups...)

	for gi, groupResults := range allResults {
		fmt.Printf("Group %d:\n", gi)
		for _, r := range groupResults {
			fmt.Printf("  [%d] %v (err=%v)\n", r.Index, r.Value, r.Err)
		}
	}
	// Group 0:
	//   [0] validate article (err=<nil>)
	//   [1] insert article (err=<nil>)
	// Group 1:
	//   [0] validate tags (err=<nil>)
	//   [1] insert tags (err=<nil>)
	// Group 2:
	//   [0] update stats (err=<nil>)
}

// ============================================================================
// 示例 6：自定义并发度（Pool 级别控制）
// ============================================================================

func ExamplePool() {
	// 创建最大 2 并发的池（适用于内存敏感或下游 QPS 限制场景）
	p := New(WithMaxWorkers(2))
	fmt.Printf("pool max workers: %d\n", p.MaxWorkers()) // pool max workers: 2
	fmt.Printf("stats: %+v\n", p.Stats())

	// 提交 6 个任务，但最多 2 个同时执行
	tasks := make([]Task, 6)
	for i := 0; i < 6; i++ {
		idx := i
		tasks[i] = func(ctx context.Context) (interface{}, error) {
			time.Sleep(20 * time.Millisecond)
			return fmt.Sprintf("task-%d-done", idx), nil
		}
	}

	start := time.Now()
	p.Parallel(context.Background(), tasks...)
	elapsed := time.Since(start)
	fmt.Printf("6 tasks × 20ms with maxWorkers=2 → ~%v\n", elapsed.Round(time.Millisecond))
	// 6 tasks × 20ms with maxWorkers=2 → ~60ms (6/2 * 20ms)

	fmt.Printf("stats: %+v\n", p.Stats())
	// stats: {ActiveWorkers:0 TotalSubmitted:6 TotalCompleted:6 TotalFailed:0}
}

// ============================================================================
// 示例 7：便捷函数 — 直接用包级函数，无需管理 Pool
// ============================================================================

func ExampleGoSerial() {
	ctx := context.Background()

	// Go 并行 — 等价于 Default().Parallel(ctx, tasks...)
	_ = Go(ctx,
		func(ctx context.Context) (interface{}, error) { return "a", nil },
		func(ctx context.Context) (interface{}, error) { return "b", nil },
	)

	// GoSerial 串行
	_ = GoSerial(ctx,
		func(ctx context.Context) (interface{}, error) { return "step1", nil },
		func(ctx context.Context) (interface{}, error) { return "step2", nil },
	)

	// GoMixed 混合
	_ = GoMixed(ctx,
		[]Task{
			func(ctx context.Context) (interface{}, error) { return "g1-a", nil },
			func(ctx context.Context) (interface{}, error) { return "g1-b", nil },
		},
		[]Task{
			func(ctx context.Context) (interface{}, error) { return "g2-a", nil },
		},
	)
}

// ============================================================================
// 示例 8：微服务实际场景 — 聚合多服务数据
// ============================================================================

type Article struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	UserID       int    `json:"user_id"`
	CommentCount int    `json:"comment_count"`
	AuthorName   string `json:"author_name"`
}

// ExampleMicroservice 演示在微服务中聚合多个下游服务数据。
func ExampleGoMixed() {
	ctx := context.Background()

	// 同时向 3 个微服务发起请求
	results := Default().Parallel(ctx,
		// 任务1：查询文章详情
		func(ctx context.Context) (interface{}, error) {
			return &Article{ID: 1, Title: "Hello Go", UserID: 10}, nil
		},
		// 任务2：查询评论数
		func(ctx context.Context) (interface{}, error) {
			return 42, nil
		},
		// 任务3：查询作者昵称
		func(ctx context.Context) (interface{}, error) {
			return "Alice", nil
		},
	)

	// 组装结果
	article := results[0].Value.(*Article)
	article.CommentCount = results[1].Value.(int)
	article.AuthorName = results[2].Value.(string)

	fmt.Printf("article: %+v\n", article)
	// article: &{ID:1 Title:Hello Go UserID:10 CommentCount:42 AuthorName:Alice}
}
