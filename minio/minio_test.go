package minio

import (
	"fmt"
	"testing"
)

func ExampleClient_GetPublicURL() {
	c := &Client{cfg: Config{PublicBaseURL: "http://cdn.example.com"}, bucket: "blog"}
	fmt.Println(c.GetPublicURL("a.png"))
	// Output: http://cdn.example.com/blog/a.png
}

func TestGetPublicURLTrailingSlash(t *testing.T) {
	c := &Client{cfg: Config{PublicBaseURL: "http://cdn.example.com/"}, bucket: "blog"}
	if got := c.GetPublicURL("a.png"); got != "http://cdn.example.com/blog/a.png" {
		t.Errorf("应去除末尾斜杠: %q", got)
	}
}

func TestGetPublicURLNoBase(t *testing.T) {
	c := &Client{cfg: Config{Endpoint: "127.0.0.1:9000"}, bucket: "blog"}
	if got := c.GetPublicURL("a.png"); got != "http://127.0.0.1:9000/blog/a.png" {
		t.Errorf("无 PublicBaseURL 应兜底用 Endpoint: %q", got)
	}
}
