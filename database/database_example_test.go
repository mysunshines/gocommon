package database

import (
	"context"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/config"

	"gorm.io/gorm"
)

// demoDBConfig 返回用于示例的本地数据库配置（示例仅编译校验 API 用法）。
func demoDBConfig() *config.DatabaseConfig {
	return &config.DatabaseConfig{
		Host:            "127.0.0.1",
		Port:            3306,
		User:            "root",
		Password:        "",
		Name:            "blog",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 3600,
	}
}

// ExampleInit 演示数据库初始化 (Init 内部自动 Ping 验证连通性)
func ExampleInit() {
	if err := Init(demoDBConfig(), ""); err != nil {
		fmt.Printf("init database failed: %v\n", err)
		return
	}
	defer Close()
	fmt.Println("database connected and ping verified")
}

// ExampleGetDB 演示基本的增删改查
func ExampleGetDB() {
	if err := Init(demoDBConfig(), ""); err != nil {
		fmt.Printf("init failed: %v\n", err)
		return
	}
	defer Close()

	d := GetDB()

	d.AutoMigrate(&User{})

	user := User{Name: "张三", Email: "zhangsan@example.com"}
	d.Create(&user)
	fmt.Printf("created user id=%d\n", user.ID)

	var found User
	d.First(&found, user.ID)
	fmt.Printf("found: id=%d name=%s\n", found.ID, found.Name)

	d.Model(&found).Update("Email", "newemail@example.com")
	fmt.Println("updated email")

	d.Delete(&found)
	fmt.Println("deleted user")
}

// ExampleWithContext 演示超时控制
func ExampleWithContext() {
	if err := Init(demoDBConfig(), ""); err != nil {
		return
	}
	defer Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var users []User
	if err := WithContext(ctx).Where("status = ?", 1).Find(&users).Error; err != nil {
		fmt.Printf("query failed: %v\n", err)
		return
	}
	fmt.Printf("found %d active users\n", len(users))
}

// ExampleTransaction 演示事务操作
func ExampleTransaction() {
	if err := Init(demoDBConfig(), ""); err != nil {
		return
	}
	defer Close()

	err := Transaction(func(tx *gorm.DB) error {
		user := User{Name: "李四", Email: "lisi@example.com"}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("create user failed: %w", err)
		}

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

// ExampleSlowQueryLogger 演示慢查询自动记录
func ExampleSlowQueryLogger() {
	fmt.Println("slow queries (>100ms) are automatically logged and reported to metrics")
}

// 定义示例模型 (实际使用时定义在 model 包中)
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
