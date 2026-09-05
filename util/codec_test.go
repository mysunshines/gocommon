package util

import (
	"fmt"
	"testing"
)

func TestBase64RoundTrip(t *testing.T) {
	// 注意：历史约定 noPadding=false 表示使用 RawStdEncoding（无填充）。
	enc := Base64EncodeEx("Hello, 世界", false, false)
	dec, err := Base64DecodeEx(enc, false, false)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if dec != "Hello, 世界" {
		t.Errorf("Base64 往返失败: got %q", dec)
	}
	// URL-safe + 带填充
	encU := Base64EncodeEx("a/b+c", true, true)
	decU, err := Base64DecodeEx(encU, true, true)
	if err != nil || decU != "a/b+c" {
		t.Errorf("URL-safe Base64 往返失败: got %q err %v", decU, err)
	}
}

func TestURLRoundTrip(t *testing.T) {
	enc := URLEncode("a b&c=d")
	dec, err := URLDecode(enc)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if dec != "a b&c=d" {
		t.Errorf("URL 往返失败: got %q", dec)
	}
	if _, err := URLDecode("%zz"); err == nil {
		t.Error("非法 URL 编码应返回错误")
	}
}

func TestHexRoundTrip(t *testing.T) {
	enc := HexEncode("Hello")
	dec, err := HexDecode(enc)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if dec != "Hello" {
		t.Errorf("Hex 往返失败: got %q", dec)
	}
	if _, err := HexDecode("zz"); err == nil {
		t.Error("非法 Hex 应返回错误")
	}
}

func TestUnicodeUnescapeError(t *testing.T) {
	if _, err := UnicodeUnescape(`\uZZZZ`); err == nil {
		t.Error("非法 Unicode 转义应返回错误")
	}
}

func ExampleBase64EncodeEx() {
	// noPadding=true 带标准填充；noPadding=false 为 Raw 无填充（与历史行为一致）。
	fmt.Println(Base64EncodeEx("Hello", false, true))
	fmt.Println(Base64EncodeEx("Hello", false, false))
	// Output:
	// SGVsbG8=
	// SGVsbG8
}

func ExampleBase64DecodeEx() {
	s, _ := Base64DecodeEx("SGVsbG8=", false, true)
	fmt.Println(s)
	// Output: Hello
}

func ExampleURLEncode() {
	fmt.Println(URLEncode("a b&c=d"))
	// Output: a+b%26c%3Dd
}

func ExampleURLDecode() {
	s, _ := URLDecode("a+b%26c%3Dd")
	fmt.Println(s)
	// Output: a b&c=d
}

func ExampleHexEncode() {
	fmt.Println(HexEncode("Hello"))
	// Output: 48656c6c6f
}

func ExampleHexDecode() {
	s, _ := HexDecode("48656c6c6f")
	fmt.Println(s)
	// Output: Hello
}

func ExampleSHA1() {
	fmt.Println(SHA1("abc"))
	// Output: a9993e364706816aba3e25717850c26c9cd0d89d
}

func ExampleSHA512() {
	fmt.Println(SHA512("abc"))
	// Output: ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f
}

func ExampleReverseString() {
	fmt.Println(ReverseString("hello"))
	fmt.Println(ReverseString("你好"))
	// Output:
	// olleh
	// 好你
}

func ExampleUnicodeEscape() {
	fmt.Println(UnicodeEscape("a中"))
	// Output: a\u4e2d
}

func ExampleUnicodeUnescape() {
	s, _ := UnicodeUnescape("a\\u4e2d")
	fmt.Println(s)
	// Output: a中
}

func ExampleMD5Sum() {
	fmt.Println(MD5Sum("abc"))
	// Output: 900150983cd24fb0d6963f7d28e17f72
}
