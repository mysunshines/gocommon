package upload

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFileHeader 在内存中构造一个 *multipart.FileHeader，便于离线测试 Save。
func newFileHeader(t *testing.T, name, content string) *multipart.FileHeader {
	t.Helper()
	buf := new(bytes.Buffer)
	mw := multipart.NewWriter(buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(int64(len(content)) + 1024); err != nil {
		t.Fatal(err)
	}
	_, hdr, err := req.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}
	return hdr
}

func TestSaveSuccess(t *testing.T) {
	dir := t.TempDir()
	hdr := newFileHeader(t, "cover.png", "imgdata")
	res, err := Save(dir, "http://cdn.example.com", hdr, Options{})
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if res.Filename == "" {
		t.Fatal("Filename 为空")
	}
	if !strings.HasSuffix(res.Filename, ".png") {
		t.Errorf("Filename 应包含扩展名: %s", res.Filename)
	}
	if _, err := os.Stat(filepath.Join(dir, res.Filename)); err != nil {
		t.Fatalf("文件未落盘: %v", err)
	}
	if !strings.HasPrefix(res.URL, "http://cdn.example.com/") {
		t.Errorf("URL 前缀错误: %s", res.URL)
	}
}

func TestSaveRejectExt(t *testing.T) {
	dir := t.TempDir()
	hdr := newFileHeader(t, "evil.exe", "data")
	if _, err := Save(dir, "http://x", hdr, Options{}); err == nil {
		t.Fatal("应拒绝非白名单扩展名")
	}
}

func TestSaveRejectNoExt(t *testing.T) {
	dir := t.TempDir()
	hdr := newFileHeader(t, "noext", "data")
	if _, err := Save(dir, "http://x", hdr, Options{}); err == nil {
		t.Fatal("应拒绝无扩展名文件")
	}
}

func TestSaveRejectSize(t *testing.T) {
	dir := t.TempDir()
	hdr := newFileHeader(t, "big.png", "data")
	hdr.Size = 1 << 30 // 手动覆盖大小，触发超限
	if _, err := Save(dir, "http://x", hdr, Options{MaxBytes: 10}); err == nil {
		t.Fatal("应拒绝超大文件")
	}
}

func ExampleSave() {
	// 注：Save 文件名随机，无法给出确定 Output；此处仅演示调用方式（需真实 *multipart.FileHeader）。
	_ = Save
	fmt.Println("see TestSaveSuccess")
	// Output: see TestSaveSuccess
}
