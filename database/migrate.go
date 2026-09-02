package database

import (
	"database/sql"
	"fmt"
	"log"
)

// Migrate 执行数据库迁移（通用函数，支持建表、索引、数据补全等场景）
// 参数：
//
//	db: 数据库连接（*sql.DB）
//	migrations: 迁移SQL列表（如建表语句）
//	indexes: 索引SQL列表（可选，若为nil则不执行索引创建）
//
// 返回：
//
//	错误信息（若所有迁移成功则返回nil）
func Migrate(db *sql.DB, migrations []string, indexes []string) error {
	// 执行迁移SQL（建表、数据补全）
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("执行迁移失败: %w\nSQL: %s", err, m)
		}
	}

	// 执行索引创建（可选）
	if indexes != nil {
		for _, idx := range indexes {
			if _, err := db.Exec(idx); err != nil {
				log.Printf("创建索引警告: %v\nSQL: %s", err, idx)
			}
		}
	}

	return nil
}
