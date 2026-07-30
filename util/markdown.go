package util

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md 为 Goldmark 实例，启用 GitHub 风格扩展（表格、删除线、任务列表、自动链接）。
// 默认（未开启 WithUnsafe）下不会对原始 HTML 放行，配合下方输入转义可彻底杜绝 XSS。
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// dangerousScheme 匹配链接/图片中的危险协议（javascript:/vbscript:/data:）。
var dangerousScheme = regexp.MustCompile(`(?i)(href|src)\s*=\s*["']\s*(javascript|vbscript|data)\s*:`)

// RenderMarkdown 将 Markdown 文本渲染为安全的 HTML 字符串。
//
// 安全策略（所有渲染逻辑均置于后端，前端只负责展示，不再自行拼接 HTML）：
//  1. 先对原始输入做 HTML 转义，使任何注入的 <script> 等标签退化为纯文本；
//  2. 再经 Goldmark 转换为 Markdown 语义 HTML；
//  3. 最后清理链接/图片 URL 中的危险协议（javascript:/vbscript:/data:），
//     防止通过 [text](javascript:...) 形式绕过。
func RenderMarkdown(content string) string {
	// 1. 转义原始 HTML
	escaped := html.EscapeString(content)

	// 2. 渲染 Markdown
	var buf bytes.Buffer
	if err := md.Convert([]byte(escaped), &buf); err != nil {
		// 渲染失败则退化为转义后的纯文本，绝不返回未净化的原始内容
		return escaped
	}
	out := buf.String()

	// 3. 清理危险 URI 协议：将协议后的冒号破坏掉，使其不再被浏览器解析为可执行协议
	out = dangerousScheme.ReplaceAllStringFunc(out, func(m string) string {
		return strings.Replace(m, ":", ":!", 1)
	})

	return out
}
