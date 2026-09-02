package httpclient

import (
	"context"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/constants"

	"github.com/valyala/fasthttp"
)

// ExampleFast_main 演示 FastClient（fasthttp）的各种用法
// 与 Example_main 的 API 完全一致，仅客户端创建方式不同：
//
//	New(...)     → NewFast(...)
//	WithXxx      → WithFastXxx
func ExampleNewFast_main() {
	ctx := context.Background()

	// 创建 fasthttp 客户端（比原生 net/http 快 2-3x）
	client := NewFast(
		WithFastBaseURL(targetServer),
		WithFastTimeout(5*time.Second),
	)

	// 示例1：创建用户
	fastCreateUser(ctx, client)

	// 示例2：获取用户
	fastGetUser(ctx, client, "user123")

	// 示例3：用户登录
	fastUserLogin(ctx, client)

	// 示例4：获取文章列表
	fastGetArticles(ctx, client)

	// 示例5：POST请求带JSON Body
	fastCreateArticle(ctx, client)

	// 示例6：使用自定义Header
	fastRequestWithAuth(ctx, client)
}

// ExampleFast_quickMethods 演示 FastClient 便捷方法
func ExampleNewFast_quickMethods() {
	ctx := context.Background()

	// QuickGet — 一行发起 GET 请求
	resp, err := QuickGet(ctx, targetServer+articleAPI+"/list", map[string]string{
		"page":  "1",
		"limit": "10",
	})
	if err != nil {
		fmt.Printf("QuickGet 失败: %v\n", err)
		return
	}
	fmt.Printf("QuickGet 状态码: %d, 响应: %s\n", resp.StatusCode, resp.String())

	// QuickGetJSON — GET + 自动解析 JSON
	var articles []map[string]interface{}
	err = QuickGetJSON(ctx, targetServer+articleAPI+"/list", map[string]string{"page": "1"}, &articles)
	if err != nil {
		fmt.Printf("QuickGetJSON 失败: %v\n", err)
	}
	fmt.Printf("获取到 %d 篇文章\n", len(articles))

	// QuickPostJSON — POST + 自动解析 JSON
	var result map[string]interface{}
	err = QuickPostJSON(ctx, targetServer+userAPI+"/login", map[string]string{
		"username": "testuser",
		"password": "password123",
	}, &result)
	if err != nil {
		fmt.Printf("QuickPostJSON 失败: %v\n", err)
	}
	fmt.Printf("登录结果: %v\n", result)
}

// ExampleFast_connectionPool 演示连接池与性能调优
func ExampleNewFast_connectionPool() {
	ctx := context.Background()

	client := NewFast(
		WithFastBaseURL(targetServer),
		WithFastTimeout(10*time.Second),
		WithFastMaxConns(200), // 最大并发连接数，适合高 QPS 场景
	)

	resp, err := client.Get(ctx, articleAPI, nil)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	fmt.Printf("状态码: %d\n", resp.StatusCode)
}

// ExampleFast_switching 演示如何从原生 Client 切换到 FastClient
func ExampleNewFast_switching() {
	ctx := context.Background()

	// 原生 net/http 客户端
	nativeClient := New(
		WithBaseURL(targetServer),
		WithTimeout(5*time.Second),
	)

	// fasthttp 客户端 — API 完全一致，仅构造函数和 Option 函数名不同
	fastClient := NewFast(
		WithFastBaseURL(targetServer),
		WithFastTimeout(5*time.Second),
	)

	// 两者用法完全相同
	nativeResp, _ := nativeClient.Get(ctx, userAPI, nil)
	fastResp, _ := fastClient.Get(ctx, userAPI, nil)

	fmt.Printf("native: %d, fast: %d\n", nativeResp.StatusCode, fastResp.StatusCode)
	// 返回值类型都是 *Response，可互换
}

// ============================================================================
// 私有示例方法
// ============================================================================

func fastCreateUser(ctx context.Context, client *FastClient) {
	fmt.Println("=== [FastClient] 创建用户 ===")

	body := map[string]interface{}{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "password123",
	}

	resp, err := client.Post(ctx, userAPI+"/register", body)
	if err != nil {
		fmt.Printf("创建用户失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}

func fastGetUser(ctx context.Context, client *FastClient, userID string) {
	fmt.Println("=== [FastClient] 获取用户信息 ===")

	params := map[string]string{
		"include": "profile",
	}

	resp, err := client.Get(ctx, userAPI+"/"+userID, params)
	if err != nil {
		fmt.Printf("获取用户失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}

func fastUserLogin(ctx context.Context, client *FastClient) {
	fmt.Println("=== [FastClient] 用户登录 ===")

	body := map[string]interface{}{
		"username": "testuser",
		"password": "password123",
	}

	resp, err := client.Post(ctx, userAPI+"/login", body)
	if err != nil {
		fmt.Printf("登录失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}

func fastGetArticles(ctx context.Context, client *FastClient) {
	fmt.Println("=== [FastClient] 获取文章列表 ===")

	params := map[string]string{
		"page":  "1",
		"limit": "10",
		"sort":  "created_at",
		"order": "desc",
	}

	resp, err := client.Get(ctx, articleAPI+"/list", params)
	if err != nil {
		fmt.Printf("获取文章列表失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}

func fastCreateArticle(ctx context.Context, client *FastClient) {
	fmt.Println("=== [FastClient] 发布文章 ===")

	body := map[string]interface{}{
		"title":   "Go微服务实战 - fasthttp 版",
		"content": "使用 fasthttp 客户端实现高性能微服务通信...",
		"tags":    []string{"go", "microservice", "fasthttp"},
		"author":  "testuser",
	}

	resp, err := client.Post(ctx, articleAPI+"/create", body)
	if err != nil {
		fmt.Printf("发布文章失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}

func fastRequestWithAuth(ctx context.Context, client *FastClient) {
	fmt.Println("=== [FastClient] 带认证的请求 ===")

	resp, err := client.SendRequest(
		ctx,
		"DELETE",
		userAPI+"/profile",
		nil,
		func(req *fasthttp.Request) {
			req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...")
			req.Header.Set("X-Request-ID", "req-789012")
			req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)
		},
	)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}

	fmt.Printf("状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应: %s\n", resp.String())
	fmt.Println()
}
