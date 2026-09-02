package util

import (
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestCopyFile 测试文件复制功能
func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatalf("write src failed: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "hello" {
		t.Fatalf("copy content mismatch: %v %s", err, data)
	}
}

// TestLoadJSONFilesFromDir_EmptyDir 测试空目录
func TestLoadJSONFilesFromDir_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	results, err := LoadJSONFilesFromDir[interface{}](tmpDir)
	if err != nil {
		t.Fatalf("LoadJSONFilesFromDir() 失败: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("期望 0 个项目，得到 %d", len(results))
	}
}

// TestLoadJSONFilesFromDir_InvalidJSON 测试无效的JSON文件
func TestLoadJSONFilesFromDir_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidFile, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("创建无效JSON文件失败: %v", err)
	}
	results, err := LoadJSONFilesFromDir[interface{}](tmpDir)
	if err != nil {
		t.Fatalf("LoadJSONFilesFromDir() 失败: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("期望 0 个项目，得到 %d", len(results))
	}
}

// TestSanitizeFilename 测试文件名清理功能
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"正常文件名", "test.txt", "test.txt"},
		{"包含斜杠", "test/file.txt", "test-file.txt"},
		{"包含反斜杠", "test\\file.txt", "test-file.txt"},
		{"包含冒号", "test:file.txt", "test-file.txt"},
		{"包含星号", "test*file.txt", "test-file.txt"},
		{"包含问号", "test?file.txt", "test-file.txt"},
		{"包含引号", "test\"file.txt", "test-file.txt"},
		{"包含尖括号", "test<file>.txt", "test-file-.txt"},
		{"包含竖线", "test|file.txt", "test-file.txt"},
		{"超长文件名", "a very long filename that exceeds 100 characters a very long filename that exceeds 100 characters a very long filename that exceeds 100 characters.txt", "a very long filename that exceeds 100 characters a very long filename that exceeds 100 characters a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%s) = %s, 期望 %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMD5(t *testing.T) {
	if got := MD5("hello"); got != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("MD5 mismatch: %s", got)
	}
}

func TestSHA256(t *testing.T) {
	if got := SHA256("hello"); len(got) != 64 {
		t.Fatalf("SHA256 len = %d, want 64", len(got))
	}
}

func TestHMACSHA256(t *testing.T) {
	if got := HMACSHA256("msg", "secret"); len(got) != 64 {
		t.Fatalf("HMACSHA256 len = %d", len(got))
	}
}

func TestHashPasswordAndCheck(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword err: %v", err)
	}
	if !CheckPassword("password123", hash) {
		t.Fatal("CheckPassword should be true for correct password")
	}
	if CheckPassword("wrong", hash) {
		t.Fatal("CheckPassword should be false for wrong password")
	}
}

func TestGenerateUUID(t *testing.T) {
	if u := GenerateUUID(); len(u) != 36 {
		t.Fatalf("uuid len = %d, want 36", len(u))
	}
}

func TestGenerateRandomString(t *testing.T) {
	if s := GenerateRandomString(16); len(s) != 16 {
		t.Fatalf("GenerateRandomString len = %d, want 16", len(s))
	}
	if s := GenerateToken(8); len(s) != 8 {
		t.Fatalf("GenerateToken len = %d, want 8", len(s))
	}
}

func TestBase64(t *testing.T) {
	enc := Base64Encode("hello")
	if enc != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatal("Base64Encode mismatch")
	}
	dec, err := Base64Decode(enc)
	if err != nil || string(dec) != "hello" {
		t.Fatalf("Base64Decode failed: %v %s", err, dec)
	}
	if _, err := Base64Decode("!!!notbase64"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestGetClientIP(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	c.Request = req
	if ip := GetClientIP(c); ip != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4", ip)
	}

	req.Header.Del("X-Forwarded-For")
	req.Header.Set("X-Real-IP", "9.9.9.9")
	if ip := GetClientIP(c); ip != "9.9.9.9" {
		t.Fatalf("got %q, want 9.9.9.9", ip)
	}

	req.Header.Del("X-Real-IP")
	if ip := GetClientIP(c); ip != "10.0.0.1" {
		t.Fatalf("got %q, want 10.0.0.1", ip)
	}
}

func TestIsValidEmail(t *testing.T) {
	cases := map[string]bool{
		"a@b.co":     true,
		"user@x.com": true,
		"":           false,
		"noatsign":   false,
		"a@b":        false,
		"@x.com":     false,
	}
	for email, want := range cases {
		if got := IsValidEmail(email); got != want {
			t.Fatalf("IsValidEmail(%q) = %v, want %v", email, got, want)
		}
	}
}

func TestIsValidUsername(t *testing.T) {
	if !IsValidUsername("john_doe") || !IsValidUsername("abc-123") {
		t.Fatal("valid usernames rejected")
	}
	if IsValidUsername("a") || IsValidUsername("bad name") || IsValidUsername("bad!name") {
		t.Fatal("invalid usernames accepted")
	}
}

func TestIsValidPassword(t *testing.T) {
	if !IsValidPassword("abc123") {
		t.Fatal("abc123 should be valid")
	}
	if IsValidPassword("abcdef") || IsValidPassword("123456") || IsValidPassword("a1") {
		t.Fatal("invalid passwords accepted")
	}
}

func TestParseAndFormatTime(t *testing.T) {
	got, err := ParseTime("2024-01-02 15:04:05")
	if err != nil {
		t.Fatalf("ParseTime err: %v", err)
	}
	if FormatTime(got) != "2024-01-02 15:04:05" {
		t.Fatalf("FormatTime = %s", FormatTime(got))
	}
	if _, err := ParseTime("not-a-time"); err == nil {
		t.Fatal("expected error for invalid time")
	}
}

func TestGetDaysBetween(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC)
	if n := GetDaysBetween(start, end); n != 10 {
		t.Fatalf("GetDaysBetween = %d, want 10", n)
	}
	n, err := GetDaysBetweenDays("2024-01-01", "2024-01-11")
	if err != nil || n != 10 {
		t.Fatalf("GetDaysBetweenDays = %d, %v", n, err)
	}
	if _, err := GetDaysBetweenDays("bad", "2024-01-11"); err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestSliceHelpers(t *testing.T) {
	if !Contains([]string{"a", "b"}, "b") || Contains([]string{"a"}, "c") {
		t.Fatal("Contains wrong")
	}
	if got := RemoveDuplicates([]string{"a", "a", "b", "b", "c"}); len(got) != 3 {
		t.Fatalf("RemoveDuplicates = %v", got)
	}
	if !InSlice([]int{1, 2, 3}, 2) || InSlice([]int{1}, 9) {
		t.Fatal("InSlice wrong")
	}
}

func TestMathHelpers(t *testing.T) {
	if MaxInt(1, 2) != 2 || MinInt(3, 1) != 1 {
		t.Fatal("MaxInt/MinInt wrong")
	}
	if !InRange(5, 1, 10) || InRange(11, 1, 10) {
		t.Fatal("InRange wrong")
	}
}

func TestGetGinContext(t *testing.T) {
	g := GetGinContext()
	if g == nil {
		t.Fatal("GetGinContext returned nil")
	}
	g.Default()
}

func ExampleMD5() {
	fmt.Println(MD5("hello"))
	// Output: 5d41402abc4b2a76b9719d911017c592
}
