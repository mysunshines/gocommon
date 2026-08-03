package configcenter_test

import (
	"fmt"

	"github.com/mysunshines/gocommon/configcenter"
)

// ExampleKey 演示如何拼出某服务在某环境下的完整 Consul KV key。
func ExampleKey() {
	key := configcenter.Key("article-service", "production")
	fmt.Println(key)
	// Output: config/article-service/production
}

// ExampleInit 演示某服务接入配置中心的完整流程：初始化、拉取、后台监听、读取。
func ExampleInit() {
	// 假设 Consul 地址与当前服务名/环境
	sc := configcenter.Init("consul:8500", "article-service", "production")

	// 1) 启动时拉取一次（KV 不存在会返回 configcenter.ErrNotFound，可忽略）
	if err := sc.Load(); err != nil && err != configcenter.ErrNotFound {
		fmt.Println("load hot config failed:", err)
	}

	// 2) 后台监听变更（阻塞式，放 goroutine）
	go sc.Watch()

	// 3) 业务代码中任意位置读取最新热更值
	cfg := sc.Get()
	fmt.Printf("qps=%d burst=%d\n", cfg.RateLimit.QPS, cfg.RateLimit.Burst)

	// 进程退出前停止监听
	defer sc.Stop()
}
