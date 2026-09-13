package util

import (
	"github.com/microcosm-cc/bluemonday"
)

// htmlPolicy 富文本 HTML 白名单策略（供 SanitizeHTML 使用）。
//
// 背景：富文本编辑器（Tiptap / ProseMirror 一类）产出的是 HTML 而非 Markdown。
// 这类内容入库前必须净化，否则 <script>、on* 事件属性、javascript: 协议等会被
// 原样存库，并在前端 innerHTML 渲染时执行 —— 典型的存储型 XSS。
//
// 策略以 bluemonday 的 UGCPolicy（面向用户生成内容）为基线，并按本项目的
// 文章排版需要做了少量放开：
//   - 保留标题、段落、引用、列表、表格、代码块等排版标签；
//   - 保留 a 的 target / rel，以及代码高亮所需的 class；
//   - 事件属性（on*）与危险协议（javascript: / vbscript: / data:）由 UGCPolicy 默认剔除。
var htmlPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// 文章正文是站内可信内容，不需要强制 nofollow；但保留 rel 以便加 noopener。
	p.RequireNoFollowOnLinks(false)
	p.AllowAttrs("target", "rel").OnElements("a")

	// 代码块高亮 / 文本对齐等依赖 class（如 language-go、text-center）。
	p.AllowAttrs("class").OnElements("span", "code", "pre", "div", "p", "td", "th", "ul", "ol", "li")

	// Tiptap 任务列表（taskList/taskItem）用 data-* 承载结构信息。
	p.AllowAttrs("data-type").OnElements("ul", "li")
	p.AllowAttrs("data-checked").OnElements("li")
	p.AllowAttrs("data-level").Matching(bluemonday.Integer).OnElements("li")

	return p
}()

// SanitizeHTML 净化富文本编辑器产出的 HTML，返回可安全存储与渲染的字符串。
//
// 与 RenderMarkdown 的分工：
//   - RenderMarkdown：输入是 Markdown，先整体 HTML 转义再渲染，因此把 HTML 交给它
//     会被转义成纯文本（标签显示为字面量），不能用于处理富文本；
//   - SanitizeHTML：输入是 HTML，按白名单保留标签与属性、剔除脚本与危险协议。
//
// 建议的使用方式（双重净化）：
//  1. 富文本内容入库前调用一次，确保库中只存安全 HTML；
//  2. 出库返回给前端前再调用一次，即使库中存在历史脏数据也不会被执行。
func SanitizeHTML(content string) string {
	if content == "" {
		return ""
	}
	return htmlPolicy.Sanitize(content)
}
