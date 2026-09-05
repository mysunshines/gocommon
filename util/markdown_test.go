package util

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	// 基本渲染
	out := RenderMarkdown("# Hi")
	if !strings.Contains(out, "<h1>Hi</h1>") {
		t.Errorf("RenderMarkdown 基本渲染失败: %s", out)
	}

	// XSS：<script> 应被转义为纯文本，绝不输出原始标签
	xss := RenderMarkdown("<script>alert(1)</script>")
	if strings.Contains(xss, "<script>") {
		t.Errorf("RenderMarkdown 未转义 script: %s", xss)
	}
	if !strings.Contains(xss, "&lt;script&gt;") {
		t.Errorf("RenderMarkdown 缺少转义实体: %s", xss)
	}

	// XSS：危险协议链接应被整体剥离（goldmark WithUnsafe=false 已做沙箱处理），
	// 输出中不应再出现可执行的 javascript: 协议。
	link := RenderMarkdown("[click](javascript:alert(1))")
	if strings.Contains(link, "javascript:") {
		t.Errorf("RenderMarkdown 未清理危险协议: %s", link)
	}
}

func ExampleRenderMarkdown() {
	fmt.Print(RenderMarkdown("# Hi"))
	// Output: <h1>Hi</h1>
}
