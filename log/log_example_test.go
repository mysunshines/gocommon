package log

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// 初始化示例
// ============================================================================

// ExampleInitCustom 演示自定义日志初始化（输出到文件）
func ExampleInit() {
	Init("./logs", "debug", "user-service")
	fmt.Println("logger initialized")
}

// ExampleInitDefault 演示默认初始化（GetLogger 自动触发，输出到 stdout）
func ExampleGetLogger() {
	// 不显式调用 Init，GetLogger 自动使用默认配置
	log := GetLogger()
	log.Info("auto-initialized with defaults")
}

// ============================================================================
// 分级日志示例
// ============================================================================

// ExampleLevels 演示各级别日志
func ExampleInfo() {
	Init("", "debug", "my-service") // 空 logDir 则输出到 stdout

	Debug("this is a debug message")
	Debugf("debug: user %d logged in", 1001)

	Info("service started")
	Infof("listening on port %d", 8080)

	Warn("disk usage above 80%")
	Warnf("slow request detected: %dms", 350)

	Error("failed to connect database")
	Errorf("connection timeout after %ds", 30)
}

// ============================================================================
// 结构化字段日志示例
// ============================================================================

// ExampleWithField 演示单字段结构化日志
func ExampleWithField() {
	Init("", "info", "article-service")

	WithField("article_id", 42).Info("article created")
	WithField("user_id", 1001).Warn("permission denied")

	// 链式调用
	WithField("request_id", "abc-123").
		WithField("duration_ms", 150).
		Info("request completed")
}

// ============================================================================
// 多字段日志示例
// ============================================================================

// ExampleWithFields 演示多字段结构化日志
func ExampleWithFields() {
	Init("", "info", "comment-service")

	// 多个字段同时记录
	WithFields(logrus.Fields{
		"comment_id": 233,
		"article_id": 42,
		"user_id":    1001,
		"ip":         "192.168.1.1",
	}).Info("comment posted")

	// 适用于错误上下文
	WithFields(logrus.Fields{
		"function": "CreateArticle",
		"error":    errors.New("title is required"),
	}).Error("validation failed")
}

// ============================================================================
// 错误日志示例
// ============================================================================

// ExampleErrorContext 演示错误日志最佳实践
func ExampleError() {
	Init("", "info", "order-service")

	err := errors.New("database connection refused")

	// 带上下文的错误日志
	WithFields(logrus.Fields{
		"host":     "127.0.0.1",
		"port":     3306,
		"retry":    3,
		"error":    err.Error(),
		"duration": "5s",
	}).Error("db connection failed after retries")
}

// ============================================================================
// 日志轮转示例
// ============================================================================

// ExampleRotateLog 演示按天轮转日志文件
// Init() 已内置自动轮转，无需手动调用 RotateLog
func ExampleRotateLog() {
	Init("./logs", "info", "gateway")

	// 日志会在每天午夜自动轮转到新日期文件
	// 如需手动强制轮转（如收到 SIGHUP 信号）：
	if err := RotateLog("gateway"); err != nil {
		Errorf("log rotation failed: %v", err)
	}
	fmt.Println("log rotated to new date file")
}

// ============================================================================
// 子 logger 示例 — 不同模块使用独立 logger
// ============================================================================

// ExampleSubLogger 演示子 logger（带固定字段的上下文 logger）
func ExampleDebug() {
	Init("", "debug", "sub-svc")

	// 为不同模块创建带固定字段的子 logger
	userLogger := WithField("module", "user")
	articleLogger := WithField("module", "article")

	userLogger.Info("user login")
	articleLogger.Info("article published")

	// 也可用 WithFields 创建多字段的子 logger
	requestLogger := WithFields(logrus.Fields{
		"service": "gateway",
		"version": "1.0.0",
	})
	requestLogger.Info("incoming request")
}
