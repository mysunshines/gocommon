package util

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/mysunshines/gocommon/constants"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// MD5 返回字符串的 MD5 十六进制摘要。
func MD5(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

// SHA256 返回字符串的 SHA-256 十六进制摘要。
func SHA256(str string) string {
	h := sha256.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

// HMACSHA256 返回基于 SHA-256 的 HMAC 十六进制签名，常用于消息完整性/防篡改校验。
func HMACSHA256(message, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// ============================================================================
// 多算法哈希（可指定算法）
// ============================================================================

// NewHasher 根据算法名返回对应的 hash.Hash。支持 MD5 / SHA1 / SHA256 / SHA512
// （大小写不敏感）。调用方负责 Write/Sum。不支持的算法返回 error，便于显式处理。
func NewHasher(algo string) (hash.Hash, error) {
	switch strings.ToUpper(algo) {
	case "MD5":
		return md5.New(), nil
	case "SHA1":
		return sha1.New(), nil
	case "SHA256":
		return sha256.New(), nil
	case "SHA512":
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("不支持的哈希算法: %s", algo)
	}
}

// HashWith 使用指定算法对字符串求哈希，返回十六进制编码结果。
func HashWith(algo, s string) (string, error) {
	h, err := NewHasher(algo)
	if err != nil {
		return "", err
	}
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashFileWith 使用指定算法对文件求哈希，返回十六进制编码结果。
func HashFileWith(algo, path string) (string, error) {
	h, err := NewHasher(algo)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ConstantTimeEq 以常量时间比较两个字符串，避免时序侧信道（用于哈希/令牌校验）。
func ConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// HashPassword 使用 bcrypt 对密码哈希，返回可安全存储的哈希串（含盐，每次结果不同）。
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword 以常量时间校验密码与 bcrypt 哈希是否匹配。
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateUUID 返回 RFC 4122 v4 的 UUID 字符串。
func GenerateUUID() string {
	return uuid.New().String()
}

// GenerateToken 生成指定长度的随机令牌（语义等价于 GenerateRandomString）。
func GenerateToken(length int) string {
	return GenerateRandomString(length)
}

// charset 随机字符串可用字符集（大小写字母与数字）。
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateRandomString 生成指定长度的随机字符串（字符集为大小写字母与数字）。
func GenerateRandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// Base64Encode 使用标准 base64 编码字符串。
func Base64Encode(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

// Base64Decode 解码标准 base64 字符串，返回原始字节。
func Base64Decode(str string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(str)
}

// GetClientIP 从 gin 上下文中提取客户端真实 IP：
// 优先取 X-Forwarded-For 的首个地址，其次 X-Real-IP，最后回退 RemoteAddr。
func GetClientIP(c *gin.Context) string {
	xff := c.GetHeader(constants.HeaderXForwardedFor)
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	xri := c.GetHeader(constants.HeaderXRealIP)
	if xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	return ip
}

// IsValidEmail 按长度与基本格式校验邮箱（仅含一个 @ 且域名部分含点）。
func IsValidEmail(email string) bool {
	if len(email) < constants.MinEmailLen || len(email) > constants.MaxEmailLen {
		return false
	}
	at := strings.Index(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return false
	}
	domain := email[at+1:]
	if !strings.Contains(domain, ".") {
		return false
	}
	return true
}

// IsValidUsername 校验用户名：长度合规且只含字母/数字/_/-。
func IsValidUsername(username string) bool {
	if len(username) < constants.MinUsernameLen || len(username) > constants.MaxUsernameLen {
		return false
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// IsValidPassword 校验密码：长度合规且同时包含字母与数字。
func IsValidPassword(password string) bool {
	if len(password) < constants.MinPasswordLen || len(password) > constants.MaxPasswordLen {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// ParseTime 按多种常见格式依次尝试解析时间字符串，全部失败返回 error。
func ParseTime(timeStr string) (time.Time, error) {
	formats := []string{
		constants.DateTimeFormat,
		constants.DateTimeISO8601,
		constants.DateTimeWithTZ,
		constants.DateTimeSlash,
		constants.DateFormat,
	}
	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

// FormatTime 将时间格式化为 "2006-01-02 15:04:05"。
func FormatTime(t time.Time) string {
	return t.Format(constants.DateTimeFormat)
}

// GetDaysBetween 返回起止时间相差的整天数（按小时/24 向下取整，忽略不足一天的部分）。
func GetDaysBetween(start, end time.Time) int {
	diff := end.Sub(start)
	return int(diff.Hours() / 24)
}

// GetDaysBetweenDays 按 "2006-01-02" 解析两个日期字符串并返回相差天数。
func GetDaysBetweenDays(startDay, endDay string) (int, error) {
	start, err := time.Parse(constants.DateFormat, startDay)
	if err != nil {
		return 0, err
	}
	end, err := time.Parse(constants.DateFormat, endDay)
	if err != nil {
		return 0, err
	}
	return GetDaysBetween(start, end), nil
}

// Contains 判断字符串切片是否包含指定元素。
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// RemoveDuplicates 去除字符串切片中的重复元素，保持元素首次出现的顺序。
func RemoveDuplicates(slice []string) []string {
	seen := make(map[string]struct{})
	result := []string{}
	for _, item := range slice {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// InSlice 判断 int 切片是否包含指定元素。
func InSlice(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// MaxInt 返回两整数中的较大值。
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MinInt 返回两整数中的较小值。
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// InRange 判断 value 是否落在 [min, max] 闭区间内。
func InRange(value, min, max int) bool {
	return value >= min && value <= max
}

// ============================================================================
// 文件路径 & 安全工具
// ============================================================================

// SafeResolve 解析路径并确保在 baseDir 内，防止目录遍历攻击
func SafeResolve(baseDir, subPath string) (string, error) {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filepath.Join(baseDir, subPath))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absBase) {
		return "", os.ErrPermission
	}
	return absPath, nil
}

// SafeResolveOrAbort 安全解析路径，失败时直接返回 403 并终止请求
func SafeResolveOrAbort(c *gin.Context, baseDir, subPath string) (string, bool) {
	abs, err := SafeResolve(baseDir, subPath)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "禁止访问"})
		return "", false
	}
	return abs, true
}

// SanitizeFilename 清理文件名中的非法字符
func SanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "-", "\"", "-", "<", "-", ">", "-", "|", "-",
	)
	name = replacer.Replace(name)
	// 去除不可见字符
	name = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == ' ' {
			return r
		}
		return -1
	}, name)
	if len(name) > 100 {
		name = name[:100]
	}
	return strings.TrimSpace(name)
}

// CopyFile 跨设备安全复制文件（通用文件操作）
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ============================================================================
// 通用文件 & JSON 操作
// ============================================================================

// DecodeDataURL 解析 "data:[<mediatype>][;base64],<data>" 形式的 data URL，
// 返回解码后的字节与媒体类型。用于图片/文件上传等场景。
// 非 base64 编码时按 URL 编码文本解码。
func DecodeDataURL(s string) ([]byte, string, error) {
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return nil, "", fmt.Errorf("不是合法的 data URL")
	}
	comma := strings.Index(s, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("非法的 data URL：缺少逗号")
	}
	meta := s[len(prefix):comma]
	data := s[comma+1:]
	mediaType := "text/plain"
	isBase64 := false
	if idx := strings.Index(meta, ";"); idx >= 0 {
		mediaType = meta[:idx]
		isBase64 = strings.EqualFold(meta[idx+1:], "base64")
	} else if meta != "" {
		mediaType = meta
	}
	if isBase64 {
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, "", err
		}
		return b, mediaType, nil
	}
	decoded, err := url.QueryUnescape(data)
	if err != nil {
		return nil, "", err
	}
	return []byte(decoded), mediaType, nil
}

// LoadJSONFilesFromDir 从目录中读取所有JSON文件并解析到切片（泛型版本，需要Go 1.18+）
// 使用示例：holidays, err := util.LoadJSONFilesFromDir[models.Holiday]("storage/holidays")
func LoadJSONFilesFromDir[T any](dir string) ([]T, error) {
	var result []T
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		var items []T
		if err := json.Unmarshal(data, &items); err != nil {
			continue
		}
		result = append(result, items...)
	}
	return result, nil
}

// ============================================================================
// 时间解析 & 格式化增强
// ============================================================================

// FlexParse 支持多种格式的灵活时间解析，解析失败时返回兜底默认值
func FlexParse(s string) time.Time {
	formats := []string{
		constants.DateTimeFormat,
		constants.DateTimeISO8601,
		constants.DateTimeWithTZ,
		constants.DateFormat,
		"2006-01-02T15:04:05+08:00",
		"2006-01-02T15:04:05-07:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	// 兜底：返回零值时间，调用方自行判断
	return time.Time{}
}

// NowFmt 返回当前时间的 YYYY-MM-DD HH:MM:SS 格式字符串
func NowFmt() string {
	return FormatTime(time.Now())
}

// ============================================================================
// Gin 上下文工具（预留）
// ============================================================================

type ginCtx struct{}

// GetGinContext 返回 gin 上下文工具占位实例（预留扩展，当前无实际能力）。
func GetGinContext() *ginCtx {
	return &ginCtx{}
}

func (g *ginCtx) Default() {
}
