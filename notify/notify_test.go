package notify

import (
	"context"
	"strings"
	"testing"
)

func TestFormatAddr(t *testing.T) {
	if formatAddr("", "a@b.com") != "a@b.com" {
		t.Fatal("empty name should return raw addr")
	}
	if formatAddr("Bob", "a@b.com") != "Bob <a@b.com>" {
		t.Fatalf("formatAddr wrong: %q", formatAddr("Bob", "a@b.com"))
	}
}

func TestIsASCII(t *testing.T) {
	if !isASCII("hello world") {
		t.Fatal("ascii string should be true")
	}
	if isASCII("你好") {
		t.Fatal("non-ascii should be false")
	}
}

func TestEncodeSubject(t *testing.T) {
	if encodeSubject("Hello") != "Hello" {
		t.Fatal("ascii subject should not be encoded")
	}
	if encodeSubject("测试标题") == "测试标题" {
		t.Fatal("non-ascii subject should be encoded")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "a", "b") != "a" {
		t.Fatal("should return first non-empty")
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("all empty should return empty")
	}
}

func TestSendValidation(t *testing.T) {
	// 缺少发件人
	if err := Send(context.Background(), Config{Host: "h", Port: 25}, Message{To: []string{"x@y.com"}}); err == nil {
		t.Fatal("expected missing sender error")
	}
	// 缺少收件人
	if err := Send(context.Background(), Config{Host: "h", Port: 25, From: "f@x.com"}, Message{}); err == nil {
		t.Fatal("expected missing recipients error")
	}
}

func TestSendBuildsMessage(t *testing.T) {
	// 校验通过后进入 SMTP 阶段，目标地址不可达应返回网络错误而非校验错误。
	err := Send(context.Background(), Config{Host: "127.0.0.1", Port: 1, From: "f@x.com"},
		Message{To: []string{"t@y.com"}, Subject: "Hi", TextBody: "hello"})
	if err == nil {
		t.Fatal("expected network error")
	}
	if strings.Contains(err.Error(), "missing") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func ExampleConfig() {
	cfg := Config{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "user",
		Password: "pass",
		From:     "noreply@example.com",
		FromName: "Blog Report",
		UseTLS:   false,
	}
	_ = cfg
}

func ExampleMessage() {
	msg := Message{
		From:     "noreply@example.com",
		FromName: "Blog",
		To:       []string{"user@example.com"},
		Subject:  "Weekly Report",
		TextBody: "plain text",
		HTMLBody: "<h1>Report</h1>",
		Images: []Image{
			{CID: "chart1", Name: "chart.png", Data: []byte("binary")},
		},
	}
	_ = msg
}
