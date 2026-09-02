package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
)

// SQLiteConfig SQLite 连接与性能参数
type SQLiteConfig struct {
	DBPath          string // 数据库文件路径
	MaxOpenConns    int    // 最大打开连接数
	MaxIdleConns    int    // 最大空闲连接数
	ConnMaxLifetime int    // 连接最大存活秒数
}

// DefaultSQLiteConfig 返回 SQLite 的推荐默认配置
func DefaultSQLiteConfig(dbPath string) SQLiteConfig {
	return SQLiteConfig{
		DBPath:          dbPath,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 300,
	}
}

// OpenSQLite 打开 SQLite 数据库并执行性能调优（WAL 模式、连接池等）
func OpenSQLite(cfg SQLiteConfig) (*sql.DB, error) {
	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, constants.FilePermDir); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	db, err := sql.Open("sqlite3", cfg.DBPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 连接池配置
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	// SQLite 性能优化 PRAGMA
	pragmas := map[string]string{
		"journal_mode": "WAL",
		"synchronous":  "NORMAL",
		"cache_size":   "-8000",
		"temp_store":   "MEMORY",
		"mmap_size":    "268435456",
	}
	for key, val := range pragmas {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA %s=%s", key, val)); err != nil {
			log.Warnf("PRAGMA %s=%s 设置失败: %v", key, val, err)
		}
	}

	// 连接验证
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接验证失败: %w", err)
	}

	log.Infof("SQLite 初始化成功: %s", cfg.DBPath)
	return db, nil
}

// CloseSQLite 安全关闭 SQLite 数据库连接
func CloseSQLite(db *sql.DB) {
	if db != nil {
		db.Close()
	}
}
