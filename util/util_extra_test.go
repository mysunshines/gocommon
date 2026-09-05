package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- 多算法哈希 ----

func TestNewHasher(t *testing.T) {
	for _, algo := range []string{"MD5", "sha1", "SHA256", "sha512"} {
		if h, err := NewHasher(algo); h == nil || err != nil {
			t.Errorf("NewHasher(%q) 失败: %v", algo, err)
		}
	}
	if _, err := NewHasher("des"); err == nil {
		t.Error("不支持的算法应返回错误")
	}
}

func TestHashWith(t *testing.T) {
	got, err := HashWith("MD5", "abc")
	if err != nil || got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("HashWith(MD5) = %q, %v", got, err)
	}
	got, err = HashWith("sha256", "abc")
	if err != nil || got != SHA256("abc") {
		t.Errorf("HashWith(sha256) 应与 SHA256 一致: %q", got)
	}
	if _, err := HashWith("bad", "x"); err == nil {
		t.Error("非法算法应返回错误")
	}
}

func TestHashFileWith(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "f.bin")
	if err := os.WriteFile(tmp, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFileWith("MD5", tmp)
	if err != nil || got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("HashFileWith = %q, %v", got, err)
	}
}

func TestConstantTimeEq(t *testing.T) {
	if !ConstantTimeEq("abc", "abc") {
		t.Error("相同字符串应相等")
	}
	if ConstantTimeEq("abc", "abd") {
		t.Error("不同字符串不应相等")
	}
	if ConstantTimeEq("abc", "abcd") {
		t.Error("不同长度不应相等")
	}
}

// ---- 路径安全 ----

func TestSafeResolve(t *testing.T) {
	base := t.TempDir()
	got, err := SafeResolve(base, "sub/file.txt")
	if err != nil {
		t.Fatalf("SafeResolve 失败: %v", err)
	}
	if !strings.HasSuffix(got, "sub/file.txt") {
		t.Errorf("SafeResolve 结果异常: %s", got)
	}
	if _, err := SafeResolve(base, "../escape.txt"); err == nil {
		t.Error("目录遍历应被拒绝")
	}
}

// ---- data URL ----

func TestDecodeDataURL(t *testing.T) {
	data, mt, err := DecodeDataURL("data:text/plain;base64,aGVsbG8=")
	if err != nil || string(data) != "hello" || mt != "text/plain" {
		t.Errorf("DecodeDataURL(base64) = %q,%q,%v", data, mt, err)
	}
	data, mt, err = DecodeDataURL("data:text/plain,hello%20world")
	if err != nil || string(data) != "hello world" || mt != "text/plain" {
		t.Errorf("DecodeDataURL(url) = %q,%q,%v", data, mt, err)
	}
	if _, _, err := DecodeDataURL("not-a-data-url"); err == nil {
		t.Error("非法 data URL 应返回错误")
	}
}

// ---- 时间 ----

func TestFlexParse(t *testing.T) {
	if FlexParse("2024-01-02 15:04:05").IsZero() {
		t.Error("合法时间应解析成功")
	}
	if !FlexParse("garbage").IsZero() {
		t.Error("非法时间应回退零值")
	}
}

// ===================== Examples =====================

func ExampleSHA256() {
	fmt.Println(SHA256("abc"))
	// Output: ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
}

func ExampleHMACSHA256() {
	// HMAC 结果依赖密钥，这里仅演示用法（不产生 Output 断言）
	_ = HMACSHA256("msg", "secret")
}

func ExampleHashWith() {
	s, _ := HashWith("MD5", "abc")
	fmt.Println(s)
	// Output: 900150983cd24fb0d6963f7d28e17f72
}

func ExampleConstantTimeEq() {
	fmt.Println(ConstantTimeEq("abc", "abc"))
	fmt.Println(ConstantTimeEq("abc", "abd"))
	// Output:
	// true
	// false
}

func ExampleHashPassword() {
	hash, _ := HashPassword("secret")
	fmt.Println(CheckPassword("secret", hash))
	fmt.Println(CheckPassword("wrong", hash))
	// Output:
	// true
	// false
}

func ExampleGenerateUUID() {
	// 内容随机，仅演示用法（长度固定 36）
	_ = GenerateUUID()
}

func ExampleGenerateRandomString() {
	// 内容随机，仅演示用法
	_ = GenerateRandomString(16)
}

func ExampleBase64Encode() {
	fmt.Println(Base64Encode("hello"))
	// Output: aGVsbG8=
}

func ExampleBase64Decode() {
	b, _ := Base64Decode("aGVsbG8=")
	fmt.Println(string(b))
	// Output: hello
}

func ExampleIsValidEmail() {
	fmt.Println(IsValidEmail("a@b.co"))
	fmt.Println(IsValidEmail("bad"))
	// Output:
	// true
	// false
}

func ExampleIsValidUsername() {
	fmt.Println(IsValidUsername("john_doe"))
	fmt.Println(IsValidUsername("bad name"))
	// Output:
	// true
	// false
}

func ExampleIsValidPassword() {
	fmt.Println(IsValidPassword("abc123"))
	fmt.Println(IsValidPassword("abcdef"))
	// Output:
	// true
	// false
}

func ExampleParseTime() {
	t, err := ParseTime("2024-01-02 15:04:05")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(FormatTime(t))
	// Output: 2024-01-02 15:04:05
}

func ExampleFormatTime() {
	fmt.Println(FormatTime(time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)))
	// Output: 2024-01-02 15:04:05
}

func ExampleGetDaysBetween() {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC)
	fmt.Println(GetDaysBetween(start, end))
	// Output: 10
}

func ExampleGetDaysBetweenDays() {
	n, _ := GetDaysBetweenDays("2024-01-01", "2024-01-11")
	fmt.Println(n)
	// Output: 10
}

func ExampleContains() {
	fmt.Println(Contains([]string{"a", "b"}, "b"))
	fmt.Println(Contains([]string{"a"}, "c"))
	// Output:
	// true
	// false
}

func ExampleRemoveDuplicates() {
	fmt.Println(strings.Join(RemoveDuplicates([]string{"a", "a", "b", "b", "c"}), " "))
	// Output: a b c
}

func ExampleInSlice() {
	fmt.Println(InSlice([]int{1, 2, 3}, 2))
	fmt.Println(InSlice([]int{1}, 9))
	// Output:
	// true
	// false
}

func ExampleMaxInt() {
	fmt.Println(MaxInt(1, 2))
	fmt.Println(MinInt(3, 1))
	fmt.Println(InRange(5, 1, 10))
	// Output:
	// 2
	// 1
	// true
}

func ExampleDecodeDataURL() {
	data, mt, _ := DecodeDataURL("data:text/plain;base64,aGVsbG8=")
	fmt.Println(string(data))
	fmt.Println(mt)
	// Output:
	// hello
	// text/plain
}

func ExampleSafeResolve() {
	abs, _ := SafeResolve("/tmp", "file.txt")
	fmt.Println(abs)
	// Output: /tmp/file.txt
}

func ExampleFlexParse() {
	t := FlexParse("2024-01-02 15:04:05")
	fmt.Println(FormatTime(t))
	// Output: 2024-01-02 15:04:05
}
