// Package upload 提供与 Web 框架无关的文件上传落盘能力，
// 供各业务服务的 HTTP handler 复用（封面上传、头像上传等）。
//
// 设计原则：
//   - 仅依赖标准库 + gocommon，不绑定 gin / echo 等具体框架；
//     调用方负责从请求中解析出 *multipart.FileHeader 后传入。
//   - 不做响应封装（Success/Fail），由调用方决定如何回包；
//     本包只返回存储后的访问 URL 与文件名，或明确的 error。
package upload

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// DefaultImageExts 默认允许的图片扩展名白名单（小写，含点）。
var DefaultImageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

// DefaultMaxBytes 默认单文件大小上限（10MB）。
const DefaultMaxBytes int64 = 10 << 20

// Options 上传保存的可选项。
type Options struct {
	// MaxBytes 单文件大小上限（字节）。<=0 时使用 DefaultMaxBytes。
	MaxBytes int64
	// AllowedExts 允许的扩展名白名单（小写，含点，如 ".jpg"）。
	// 为空时使用 DefaultImageExts。匹配时忽略 Content-Type，仅看文件扩展名。
	AllowedExts []string
}

// Result 上传保存的结果。
type Result struct {
	// URL 可公开访问的文件地址（由 PublicBaseURL 拼接随机文件名得到）。
	URL string
	// Filename 落盘后的随机文件名（含扩展名，不含目录前缀）。
	Filename string
}

// Save 将 multipart 文件头描述的文件保存到 dir 目录，
// 文件名采用随机串 + 原扩展名（防止覆盖与路径穿越），
// 并返回 Result。publicBaseURL 为文件对外可访问的 URL 前缀（不含末尾斜杠），
// 结果 URL = publicBaseURL + "/" + Filename。
//
// 校验项：
//   - 文件大小不超过 MaxBytes；
//   - 扩展名（取自原始文件名）落在 AllowedExts 白名单内。
func Save(dir, publicBaseURL string, file *multipart.FileHeader, opts Options) (*Result, error) {
	if file == nil {
		return nil, fmt.Errorf("upload: empty file")
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	allowed := opts.AllowedExts
	if len(allowed) == 0 {
		allowed = DefaultImageExts
	}

	if file.Size > maxBytes {
		return nil, fmt.Errorf("upload: file too large (%d > %d bytes)", file.Size, maxBytes)
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		return nil, fmt.Errorf("upload: file has no extension")
	}
	ok := false
	for _, a := range allowed {
		if ext == strings.ToLower(a) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, fmt.Errorf("upload: extension %q not allowed", ext)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("upload: mkdir %s: %w", dir, err)
	}

	name, err := randName(16)
	if err != nil {
		return nil, err
	}
	stored := name + ext
	dst := filepath.Join(dir, stored)

	// 防御性检查：Join 后仍需落在 dir 内（ext 已清洗，理论安全，双重保险）。
	if !strings.HasPrefix(dst, filepath.Clean(dir)+string(os.PathSeparator)) && dst != filepath.Clean(dir) {
		return nil, fmt.Errorf("upload: invalid destination path")
	}

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("upload: open upload: %w", err)
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return nil, fmt.Errorf("upload: create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return nil, fmt.Errorf("upload: write %s: %w", dst, err)
	}

	base := strings.TrimRight(publicBaseURL, "/")
	url := base + "/" + stored
	return &Result{URL: url, Filename: stored}, nil
}

// randName 返回 n 字节随机数的十六进制字符串。
func randName(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("upload: rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}
