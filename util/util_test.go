package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFile 测试文件复制功能
func TestCopyFile(t *testing.T) {
	// 创建临时源文件
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	// 写入测试数据
	testData := []byte("Hello, World!")
	if err := os.WriteFile(srcPath, testData, 0644); err != nil {
		t.Fatalf("创建源文件失败: %v", err)
	}

	// 复制文件
	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile() 失败: %v", err)
	}

	// 验证目标文件存在且内容正确
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Errorf("读取目标文件失败: %v", err)
	}
	if string(data) != string(testData) {
		t.Errorf("文件内容不匹配: 期望 %s, 得到 %s", string(testData), string(data))
	}
}

// TestCopyFile_Error 测试复制不存在的源文件
func TestCopyFile_Error(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	// 尝试复制不存在的文件
	err := CopyFile(srcPath, dstPath)
	if err == nil {
		t.Error("CopyFile() 期望返回错误，但未返回")
	}
}

// TestLoadJSONFilesFromDir 测试从目录加载JSON文件
func TestLoadJSONFilesFromDir(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()

	// 创建测试结构体
	type TestItem struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	// 创建第一个JSON文件
	file1 := filepath.Join(tmpDir, "file1.json")
	data1 := []TestItem{
		{Name: "item1", Value: 1},
		{Name: "item2", Value: 2},
	}
	jsonData1, _ := json.Marshal(data1)
	if err := os.WriteFile(file1, jsonData1, 0644); err != nil {
		t.Fatalf("创建JSON文件失败: %v", err)
	}

	// 创建第二个JSON文件
	file2 := filepath.Join(tmpDir, "file2.json")
	data2 := []TestItem{
		{Name: "item3", Value: 3},
	}
	jsonData2, _ := json.Marshal(data2)
	if err := os.WriteFile(file2, jsonData2, 0644); err != nil {
		t.Fatalf("创建JSON文件失败: %v", err)
	}

	// 创建一个非JSON文件（应被忽略）
	txtFile := filepath.Join(tmpDir, "readme.txt")
	if err := os.WriteFile(txtFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("创建文本文件失败: %v", err)
	}

	// 加载JSON文件
	results, err := LoadJSONFilesFromDir[TestItem](tmpDir)
	if err != nil {
		t.Fatalf("LoadJSONFilesFromDir() 失败: %v", err)
	}

	// 验证结果
	if len(results) != 3 {
		t.Errorf("期望 3 个项目，得到 %d", len(results))
	}

	// 验证内容
	expected := map[string]int{
		"item1": 1,
		"item2": 2,
		"item3": 3,
	}
	for _, item := range results {
		if expected[item.Name] != item.Value {
			t.Errorf("项目 %s 的值不匹配: 期望 %d, 得到 %d", item.Name, expected[item.Name], item.Value)
		}
	}
}

// TestLoadJSONFilesFromDir_EmptyDir 测试空目录
func TestLoadJSONFilesFromDir_EmptyDir(t *testing.T) {
	// 创建空临时目录
	tmpDir := t.TempDir()

	// 加载JSON文件（应返回空切片）
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
	// 创建临时目录
	tmpDir := t.TempDir()

	// 创建无效的JSON文件
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidFile, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("创建无效JSON文件失败: %v", err)
	}

	// 加载JSON文件（应跳过无效文件）
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
		// 超长文件名会被截断到100字符
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
