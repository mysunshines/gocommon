package database

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenSQLiteAndClose 测试SQLite数据库连接和关闭
func TestOpenSQLiteAndClose(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// 测试打开数据库
	cfg := DefaultSQLiteConfig(dbPath)
	db, err := OpenSQLite(cfg)
	if err != nil {
		t.Fatalf("OpenSQLite() 失败: %v", err)
	}

	// 验证数据库是否可用（执行简单查询）
	var result int
	err = db.QueryRow("SELECT 1").Scan(&result)
	if err != nil {
		t.Errorf("数据库查询失败: %v", err)
	}

	// 测试关闭数据库
	CloseSQLite(db)
}

// TestDefaultSQLiteConfig 测试默认配置生成
func TestDefaultSQLiteConfig(t *testing.T) {
	dbPath := "/tmp/test.db"
	cfg := DefaultSQLiteConfig(dbPath)

	if cfg.DBPath != dbPath {
		t.Errorf("DefaultSQLiteConfig() DBPath = %s, 期望 %s", cfg.DBPath, dbPath)
	}
	if cfg.MaxOpenConns != 10 {
		t.Errorf("DefaultSQLiteConfig() MaxOpenConns = %d, 期望 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("DefaultSQLiteConfig() MaxIdleConns = %d, 期望 5", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 300 {
		t.Errorf("DefaultSQLiteConfig() ConnMaxLifetime = %d, 期望 300", cfg.ConnMaxLifetime)
	}
}

// TestOpenSQLite_InvalidPath 测试无效路径的数据库打开
func TestOpenSQLite_InvalidPath(t *testing.T) {
	// 使用一个无法创建文件的路径（如根目录下的只读路径）
	invalidPath := "/invalid_dir/test.db"
	cfg := DefaultSQLiteConfig(invalidPath)
	_, err := OpenSQLite(cfg)
	if err == nil {
		t.Error("OpenSQLite() 期望返回错误，但未返回")
	}
}

// 清理测试后的临时文件
func cleanupTestDB(dbPath string) {
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
}
