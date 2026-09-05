package util

import (
	"testing"
)

// TestLookupDNSByTypeNoNetwork 覆盖不触发网络请求的分支（SOA / 不支持类型）。
func TestLookupDNSByTypeNoNetwork(t *testing.T) {
	if r := LookupDNSByType("example.com", "SOA", nil); len(r.Errors) == 0 {
		t.Fatal("SOA 应返回不支持的错误提示")
	}
	if r := LookupDNSByType("example.com", "FOO", nil); len(r.Errors) == 0 {
		t.Fatal("不支持的记录类型应返回错误提示")
	}
}

// TestLookupPTRInvalid 非法 IP 的 PTR 查询应返回错误。
func TestLookupPTRInvalid(t *testing.T) {
	if _, err := LookupPTR("not-an-ip", nil); err == nil {
		t.Fatal("非法 IP 的 PTR 查询应返回错误")
	}
}

func ExampleLookupDNS() {
	// 实际查询需要网络，这里仅演示用法（不产生 Output 断言）
	result := LookupDNS("example.com", nil)
	_ = result
}

func ExampleLookupDNSByType() {
	// 实际查询需要网络，这里仅演示用法（不产生 Output 断言）
	result := LookupDNSByType("example.com", "MX", nil)
	_ = result
}

func ExampleLookupPTR() {
	// 实际查询需要网络，这里仅演示用法（不产生 Output 断言）
	names, err := LookupPTR("8.8.8.8", nil)
	if err == nil {
		_ = names
	}
}
