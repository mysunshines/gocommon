package database

import (
	"path/filepath"
	"testing"
)

// TestMigrate_Success 测试Successful migration
func TestMigrate_Success(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migrate.db")

	// 打开数据库连接
	cfg := DefaultSQLiteConfig(dbPath)
	db, err := OpenSQLite(cfg)
	if err != nil {
		t.Fatalf("OpenSQLite() 失败: %v", err)
	}
	defer CloseSQLite(db)

	// 定义迁移SQL（建表）
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS test_table (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)`,
	}

	// 定义索引SQL
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_test_name ON test_table(name)`,
	}

	// 执行迁移
	err = Migrate(db, migrations, indexes)
	if err != nil {
		t.Errorf("Migrate() 失败: %v", err)
	}

	// 验证表是否创建成功
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='test_table'").Scan(&tableName)
	if err != nil {
		t.Errorf("表 test_table 未创建成功: %v", err)
	}

	// 验证索引是否创建成功
	var indexName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_test_name'").Scan(&indexName)
	if err != nil {
		t.Errorf("索引 idx_test_name 未创建成功: %v", err)
	}
}

// TestMigrate_FailedMigration 测试迁移失败场景
func TestMigrate_FailedMigration(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migrate_fail.db")

	// 打开数据库连接
	cfg := DefaultSQLiteConfig(dbPath)
	db, err := OpenSQLite(cfg)
	if err != nil {
		t.Fatalf("OpenSQLite() 失败: %v", err)
	}
	defer CloseSQLite(db)

	// 定义错误的迁移SQL（语法错误）
	migrations := []string{
		`CREATE TABLE invalid syntax`,
	}

	// 执行迁移，期望返回错误
	err = Migrate(db, migrations, nil)
	if err == nil {
		t.Error("Migrate() 期望返回错误，但未返回")
	}
}

// TestMigrate_IndexWarning 测试索引创建警告（如表不存在时创建索引）
func TestMigrate_IndexWarning(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_index_warn.db")

	// 打开数据库连接
	cfg := DefaultSQLiteConfig(dbPath)
	db, err := OpenSQLite(cfg)
	if err != nil {
		t.Fatalf("OpenSQLite() 失败: %v", err)
	}
	defer CloseSQLite(db)

	// 定义迁移SQL（建表）
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS test_table (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)`,
	}

	// 定义索引SQL（在表存在的情况下创建索引，不应有警告）
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_test_name ON test_table(name)`,
	}

	// 执行迁移
	err = Migrate(db, migrations, indexes)
	if err != nil {
		t.Errorf("Migrate() 失败: %v", err)
	}
}

// TestMigrate_EmptyMigrations 测试空迁移列表
func TestMigrate_EmptyMigrations(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_empty_migrate.db")

	// 打开数据库连接
	cfg := DefaultSQLiteConfig(dbPath)
	db, err := OpenSQLite(cfg)
	if err != nil {
		t.Fatalf("OpenSQLite() 失败: %v", err)
	}
	defer CloseSQLite(db)

	// 执行空迁移列表
	err = Migrate(db, []string{}, nil)
	if err != nil {
		t.Errorf("Migrate() 失败: %v", err)
	}
}
