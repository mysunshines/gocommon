package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mysunshines/gocommon/constants"
)

// 模拟的目标服务（实际使用时替换为真实地址）
const (
	targetServer = "http://localhost:8080"
	userAPI      = "/api/v1/user"
	articleAPI   = "/api/v1/article"
)

// Example_main 是一个示例演示 HTTP 客户端的各种用法
func ExampleNew_main() {
	ctx := context.Background()

	// 创建带基础URL的HTTP客户端
	client := New(
		WithBaseURL(targetServer),
		WithTimeout(5*time.Second),
	)

	// 示例1：创建用户
	createUser(ctx, client)

	// 示例2：获取用户
	getUser(ctx, client, "user123")

	// 示例3：用户登录
	userLogin(ctx, client)

	// 示例4：获取文章列表
	getArticles(ctx, client)

	// 示例5：POST请求带JSON Body
	createArticle(ctx, client)

	// 示例6：使用自定义Header
	requestWithAuth(ctx, client)
}

func createUser(ctx context.Context, client *Client) {
	fmt.Println("=== 创建用户 ===")

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

func getUser(ctx context.Context, client *Client, userID string) {
	fmt.Println("=== 获取用户信息 ===")

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

func userLogin(ctx context.Context, client *Client) {
	fmt.Println("=== 用户登录 ===")

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

func getArticles(ctx context.Context, client *Client) {
	fmt.Println("=== 获取文章列表 ===")

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

func createArticle(ctx context.Context, client *Client) {
	fmt.Println("=== 发布文章 ===")

	body := map[string]interface{}{
		"title":   "Go微服务实战",
		"content": "这是一篇关于Go微服务的文章...",
		"tags":    []string{"go", "microservice"},
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

func requestWithAuth(ctx context.Context, client *Client) {
	fmt.Println("=== 带认证的请求 ===")

	// 使用自定义Header发送请求
	resp, err := client.SendRequest(
		ctx,
		"DELETE",
		userAPI+"/profile",
		nil,
		func(header http.Header) {
			header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...")
			header.Set("X-Request-ID", "req-123456")
			header.Set(constants.HeaderContentType, constants.ContentTypeJSON)
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
