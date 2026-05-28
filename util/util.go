package util

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/mysunshines/gocommon/constants"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func MD5(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

func SHA256(str string) string {
	h := sha256.New()
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}

func HMACSHA256(message, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateUUID() string {
	return uuid.New().String()
}

// charset 随机字符串可用字符集
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateToken(length int) string {
	return GenerateRandomString(length)
}

func GenerateRandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func Base64Encode(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func Base64Decode(str string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(str)
}

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

func FormatTime(t time.Time) string {
	return t.Format(constants.DateTimeFormat)
}

func GetDaysBetween(start, end time.Time) int {
	diff := end.Sub(start)
	return int(diff.Hours() / 24)
}

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

func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

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

func InSlice(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func InRange(value, min, max int) bool {
	return value >= min && value <= max
}

type ginCtx struct{}

func GetGinContext() *ginCtx {
	return &ginCtx{}
}

func (g *ginCtx) Default() {
}
