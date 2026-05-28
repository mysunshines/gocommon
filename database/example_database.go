package database

import (
	"context"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/config"

	"gorm.io/gorm"
)

// ============================================================================
// 数据库初始化与使用示例
// ============================================================================

// ExampleInitAndPing 演示数据库初始化 (Init 内部自动 Ping 验证连通性)
func ExampleInitAndPing() {
	// 1. 加载配置
	conf, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return
	}

	// 2. 初始化数据库连接——Init 会先 Open 再 Ping 确认连通
	cfg := conf.Database
	if err := Init(&cfg, ""); err != nil {
		fmt.Printf("init database failed: %v\n", err)
		return
	}
	defer Close()
	fmt.Println("database connected and ping verified")
}

// ============================================================================
// CRUD 操作示例
// ============================================================================

// ExampleCRUD 演示基本的增删改查
func ExampleCRUD(dbCfg *config.DatabaseConfig) {
	if err := Init(dbCfg, ""); err != nil {
		fmt.Printf("init failed: %v\n", err)
		return
	}
	defer Close()

	// 获取数据库实例
	d := GetDB()

	// ---- AutoMigrate ----
	d.AutoMigrate(&User{})

	// ---- Create ----
	user := User{Name: "张三", Email: "zhangsan@example.com"}
	d.Create(&user)
	fmt.Printf("created user id=%d\n", user.ID)

	// ---- Read ----
	var found User
	d.First(&found, user.ID)
	fmt.Printf("found: id=%d name=%s\n", found.ID, found.Name)

	// ---- Update ----
	d.Model(&found).Update("Email", "newemail@example.com")
	fmt.Println("updated email")

	// ---- Delete ----
	d.Delete(&found)
	fmt.Println("deleted user")
}

// ============================================================================
// 带 Context 的查询示例
// ============================================================================

// ExampleWithContext 演示超时控制
func ExampleWithContext(dbCfg *config.DatabaseConfig) {
	if err := Init(dbCfg, ""); err != nil {
		return
	}
	defer Close()

	// 创建带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var users []User
	if err := WithContext(ctx).Where("status = ?", 1).Find(&users).Error; err != nil {
		fmt.Printf("query failed: %v\n", err)
		return
	}
	fmt.Printf("found %d active users\n", len(users))
}

// ============================================================================
// 事务示例
// ============================================================================

// ExampleTransaction 演示事务操作
func ExampleTransaction(dbCfg *config.DatabaseConfig) {
	if err := Init(dbCfg, ""); err != nil {
		return
	}
	defer Close()

	err := Transaction(func(tx *gorm.DB) error {
		// 在事务内创建用户
		user := User{Name: "李四", Email: "lisi@example.com"}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("create user failed: %w", err)
		}

		// 同时创建关联的配置记录
		profile := Profile{UserID: user.ID, Bio: "新用户"}
		if err := tx.Create(&profile).Error; err != nil {
			return fmt.Errorf("create profile failed: %w", err)
		}

		fmt.Printf("transaction committed: user=%d profile=%d\n", user.ID, profile.ID)
		return nil
	})
	if err != nil {
		fmt.Printf("transaction rolled back: %v\n", err)
	}
}

// ============================================================================
// 慢查询日志示例
// ============================================================================

// ExampleSlowQuery 演示使用自定义慢查询 Logger（需在 Init 时将 gorm.Config.Logger 替换为 &SlowQueryLogger{}）
func ExampleSlowQuery() {
	// 超过 100ms 的查询会被 SlowQueryLogger.Trace 记录并上报 Prometheus
	fmt.Println("slow queries (>100ms) are automatically logged and reported to metrics")
}

// ============================================================================
// 定义示例模型 (实际使用时定义在 model 包中)
// ============================================================================

type User struct {
	ID    uint   `gorm:"primaryKey"`
	Name  string `gorm:"size:64"`
	Email string `gorm:"size:128;uniqueIndex"`
}

type Profile struct {
	ID     uint   `gorm:"primaryKey"`
	UserID uint   `gorm:"uniqueIndex"`
	Bio    string `gorm:"size:256"`
}
