package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNew 测试客户端创建和配置
func TestNew(t *testing.T) {
	// 测试默认配置
	c := New()
	if c == nil {
		t.Error("New() 返回 nil")
	}

	// 测试带选项的配置
	c = New(
		WithBaseURL("https://api.example.com"),
		WithTimeout(10*time.Second),
		WithHeader("Authorization", "Bearer token"),
	)
	if c.baseURL != "https://api.example.com" {
		t.Errorf("WithBaseURL() 失败: %s", c.baseURL)
	}
	if c.timeout != 10*time.Second {
		t.Errorf("WithTimeout() 失败: %v", c.timeout)
	}
	if c.headers["Authorization"] != "Bearer token" {
		t.Errorf("WithHeader() 失败: %s", c.headers["Authorization"])
	}
}

// TestGet 测试GET请求
func TestGet(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和路径
		if r.Method != http.MethodGet {
			t.Errorf("期望 GET 请求，收到 %s", r.Method)
		}
		if r.URL.Path != "/test" {
			t.Errorf("期望路径 /test，收到 %s", r.URL.Path)
		}

		// 验证查询参数
		if r.URL.Query().Get("key") != "value" {
			t.Errorf("期望查询参数 key=value，收到 %s", r.URL.Query().Get("key"))
		}

		// 返回响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送GET请求
	ctx := context.Background()
	resp, err := c.Get(ctx, "/test", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("Get() 失败: %v", err)
	}

	// 验证响应
	if !resp.IsSuccess() {
		t.Errorf("期望成功响应，收到状态码 %d", resp.StatusCode)
	}

	// 验证响应体
	var result map[string]string
	if err := resp.Unmarshal(&result); err != nil {
		t.Errorf("解析响应体失败: %v", err)
	}
	if result["message"] != "success" {
		t.Errorf("期望响应体 message=success，收到 %s", result["message"])
	}
}

// TestPost 测试POST请求
func TestPost(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法和Content-Type
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST 请求，收到 %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("期望 Content-Type=application/json，收到 %s", r.Header.Get("Content-Type"))
		}

		// 解析请求体
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		// 返回响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": body["name"]})
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送POST请求
	ctx := context.Background()
	body := map[string]interface{}{"name": "test"}
	resp, err := c.Post(ctx, "/test", body)
	if err != nil {
		t.Fatalf("Post() 失败: %v", err)
	}

	// 验证响应
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("期望状态码 201，收到 %d", resp.StatusCode)
	}

	// 验证响应体
	var result map[string]interface{}
	if err := resp.Unmarshal(&result); err != nil {
		t.Errorf("解析响应体失败: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("期望响应体 name=test，收到 %s", result["name"])
	}
}

// TestPut 测试PUT请求
func TestPut(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("期望 PUT 请求，收到 %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送PUT请求
	ctx := context.Background()
	body := map[string]interface{}{"name": "updated"}
	resp, err := c.Put(ctx, "/test/1", body)
	if err != nil {
		t.Fatalf("Put() 失败: %v", err)
	}

	// 验证响应
	if !resp.IsSuccess() {
		t.Errorf("期望成功响应，收到状态码 %d", resp.StatusCode)
	}
}

// TestDelete 测试DELETE请求
func TestDelete(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("期望 DELETE 请求，收到 %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送DELETE请求
	ctx := context.Background()
	resp, err := c.Delete(ctx, "/test/1")
	if err != nil {
		t.Fatalf("Delete() 失败: %v", err)
	}

	// 验证响应
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("期望状态码 204，收到 %d", resp.StatusCode)
	}
}

// TestPatch 测试PATCH请求
func TestPatch(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("期望 PATCH 请求，收到 %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送PATCH请求
	ctx := context.Background()
	body := map[string]interface{}{"name": "patched"}
	resp, err := c.Patch(ctx, "/test/1", body)
	if err != nil {
		t.Fatalf("Patch() 失败: %v", err)
	}

	// 验证响应
	if !resp.IsSuccess() {
		t.Errorf("期望成功响应，收到状态码 %d", resp.StatusCode)
	}
}

// TestMiddleware 测试中间件功能
func TestMiddleware(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证中间件是否添加了请求头
		if r.Header.Get("X-Test") != "middleware" {
			t.Errorf("期望请求头 X-Test=middleware，收到 %s", r.Header.Get("X-Test"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建客户端，添加中间件
	c := New(
		WithBaseURL(server.URL),
		WithMiddleware(func(req *http.Request) error {
			req.Header.Set("X-Test", "middleware")
			return nil
		}),
	)

	// 发送GET请求
	ctx := context.Background()
	_, err := c.Get(ctx, "/test", nil)
	if err != nil {
		t.Fatalf("Get() 失败: %v", err)
	}
}

// TestResponseMethods 测试响应方法
func TestResponseMethods(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	}))
	defer server.Close()

	// 创建客户端
	c := New(WithBaseURL(server.URL))

	// 发送GET请求
	ctx := context.Background()
	resp, err := c.Get(ctx, "/test", nil)
	if err != nil {
		t.Fatalf("Get() 失败: %v", err)
	}

	// 测试IsSuccess
	if !resp.IsSuccess() {
		t.Errorf("IsSuccess() 应为 true，收到 false")
	}

	// 测试IsClientError
	if resp.IsClientError() {
		t.Errorf("IsClientError() 应为 false，收到 true")
	}

	// 测试IsServerError
	if resp.IsServerError() {
		t.Errorf("IsServerError() 应为 false，收到 true")
	}

	// 测试String
	str := resp.String()
	if !strings.Contains(str, "success") {
		t.Errorf("String() 应包含 success，收到 %s", str)
	}

	// 测试Unmarshal
	var result map[string]string
	if err := resp.Unmarshal(&result); err != nil {
		t.Errorf("Unmarshal() 失败: %v", err)
	}
}
