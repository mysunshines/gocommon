package consul

import (
	"fmt"
	"os"
	"testing"
)

func ExampleInstance_Addr() {
	inst := &Instance{Address: "10.0.0.1", Port: 9000}
	fmt.Println(inst.Addr())
	// Output: 10.0.0.1:9000
}

func TestInstanceHTTPAddr(t *testing.T) {
	withHTTP := &Instance{Address: "10.0.0.1", Port: 9000, HTTPPort: 8080}
	if got := withHTTP.HTTPAddr(0); got != "10.0.0.1:8080" {
		t.Errorf("HTTPAddr 失败: %q", got)
	}
	onlyGRPC := &Instance{Address: "10.0.0.1", Port: 9000}
	if got := onlyGRPC.HTTPAddr(8088); got != "10.0.0.1:8088" {
		t.Errorf("HTTPAddr 回退失败: %q", got)
	}
}

func TestCanaryFromEnv(t *testing.T) {
	t.Setenv("BLOG_CANARY", "true")
	if !CanaryFromEnv() {
		t.Error("BLOG_CANARY=true 应返回 true")
	}
	t.Setenv("BLOG_CANARY", "no")
	if CanaryFromEnv() {
		t.Error("BLOG_CANARY=no 应返回 false")
	}
}

func TestVersionFromEnv(t *testing.T) {
	t.Setenv("SERVICE_VERSION", "v9.9.9")
	if got := VersionFromEnv("default"); got != "v9.9.9" {
		t.Errorf("SERVICE_VERSION 未生效: %q", got)
	}
	os.Unsetenv("SERVICE_VERSION")
	if got := VersionFromEnv("default"); got != "default" {
		t.Errorf("缺失环境变量应回退默认值: %q", got)
	}
}
