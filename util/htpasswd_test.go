package util

import (
	"fmt"
	"testing"
)

func TestGenerateHtpasswdAndVerify(t *testing.T) {
	for _, algo := range []string{"sha", "apr1", "bcrypt", "plain"} {
		hash, _, err := GenerateHtpasswd("secret", algo)
		if err != nil {
			t.Fatalf("%s: 生成失败 %v", algo, err)
		}
		if !VerifyHtpasswd("secret", hash) {
			t.Errorf("%s: 正确密码应校验通过", algo)
		}
		if VerifyHtpasswd("wrong-password", hash) {
			t.Errorf("%s: 错误密码不应通过", algo)
		}
	}
}

func TestRandomPasswordLength(t *testing.T) {
	if len(RandomPassword(12)) != 12 {
		t.Fatal("长度 12 期望 12")
	}
	if len(RandomPassword(0)) != 12 {
		t.Fatal("长度 0 应被夹到 12")
	}
	if len(RandomPassword(100)) != 64 {
		t.Fatal("长度 100 应被夹到 64")
	}
}

func ExampleGenerateHtpasswd() {
	hash, label, _ := GenerateHtpasswd("secret", "sha")
	fmt.Println(hash)
	fmt.Println(label)
	// Output:
	// {SHA}5en6G6MezRroT3XKqkdPOmY/BfQ=
	// SHA-1
}

func ExampleVerifyHtpasswd() {
	hash, _, _ := GenerateHtpasswd("secret", "bcrypt")
	fmt.Println(VerifyHtpasswd("secret", hash))
	fmt.Println(VerifyHtpasswd("wrong", hash))
	// Output:
	// true
	// false
}

func ExampleRandomPassword() {
	// 内容随机，仅断言长度（范围被夹在 [4,64]）
	fmt.Println(len(RandomPassword(16)))
	// Output: 16
}
