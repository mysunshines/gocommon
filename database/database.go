package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"
	"github.com/mysunshines/gocommon/middleware"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once
)

// Config 数据库配置
type Config struct {
	Host         string // 数据库主机
	Port         int    // 数据库端口
	User         string // 用户名
	Password     string // 密码
	DBName       string // 数据库名
	MaxOpenConns int    // 最大打开连接数
	MaxIdleConns int    // 最大空闲连接数
	MaxLifeTime  int    // 连接最大存活时间（秒）
}

// Init 初始化全局数据库连接，使用 sync.Once 保证仅执行一次，并按环境设置日志级别、校验连通性后返回错误。
func Init(cfg *config.DatabaseConfig, env string) error {
	var initErr error
	once.Do(func() {
		gormLogger := logger.Interface(&SlowQueryLogger{LogLevel: logger.Warn})
		if env == constants.EnvDevelopment {
			gormLogger = gormLogger.LogMode(logger.Info)
		}

		var err error
		db, err = gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
			Logger: gormLogger,
			// 不使用数据库外键约束，关联关系在代码层（service/repository）约束。
			DisableForeignKeyConstraintWhenMigrating: true,
			NowFunc: func() time.Time {
				return time.Now().Local()
			},
		})
		if err != nil {
			initErr = fmt.Errorf("failed to connect database: %w", err)
			return
		}

		sqlDB, err := db.DB()
		if err != nil {
			initErr = fmt.Errorf("failed to get sql.DB: %w", err)
			return
		}

		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

		// 验证数据库连接是否可用
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			initErr = fmt.Errorf("failed to ping database: %w", err)
			return
		}

		log.Info("Database initialized successfully")
	})
	return initErr
}

// GetDB 返回全局数据库连接实例（*gorm.DB）。
func GetDB() *gorm.DB {
	return db
}

// GetDBConn 返回数据库连接（用于外部包访问）
func GetDBConn() *gorm.DB {
	return db
}

// SlowQueryLogger 实现 gorm 的 logger.Interface，用于按级别记录 SQL 并统计慢查询。
type SlowQueryLogger struct {
	LogLevel logger.LogLevel // 日志级别过滤
}

// LogMode 基于当前 logger 设置日志级别，并返回新的 logger.Interface 实例（gorm 要求不可变）。
func (s *SlowQueryLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *s
	newLogger.LogLevel = level
	return &newLogger
}

// Error 记录 SQL 执行错误日志，附带 traceID 便于链路追踪（不受日志级别过滤）。
func (s *SlowQueryLogger) Error(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Errorf("[MySQL] traceID=%v | err=%v", traceID, values)
	}
}

// Info 记录 Info 级别 SQL 日志，受 LogLevel 过滤并附带 traceID。
func (s *SlowQueryLogger) Info(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 && s.LogLevel >= logger.Info {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Infof("[MySQL] traceID=%v | %v", traceID, values)
	}
}

// Warn 记录 Warn 级别 SQL 日志，受 LogLevel 过滤并附带 traceID。
func (s *SlowQueryLogger) Warn(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 && s.LogLevel >= logger.Warn {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Warnf("[MySQL] traceID=%v | %v", traceID, values)
	}
}

// Trace 记录每次 SQL 执行耗时与影响行数，统计 DB 操作指标并对慢查询告警。
func (s *SlowQueryLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	traceID := middleware.GetTraceIDFromContext(ctx)

	// 每次 SQL 都计入 DB 操作总量（按 SQL 前缀粗分），驱动 db_operations_total 指标。
	// 必须在最开头（含 early return 之前）统计，确保全部 SQL 都被计数。
	op := classifySQL(sql)
	status := "success"
	if err != nil && err != gorm.ErrRecordNotFound {
		status = "error"
	}
	metrics.RecordDBOperation(op, status)

	// 出错时始终记录（不受 LogLevel 过滤），便于通过 traceID 排查
	if err != nil {
		log.Errorf("[MySQL] traceID=%v | sql=%s | duration=%v | err=%v",
			traceID, sql, elapsed, err)
		return
	}

	// Info 级别：记录所有 SQL 查询（开发环境）
	if s.LogLevel >= logger.Info {
		log.Infof("[MySQL] traceID=%v | sql=%s | duration=%v | rows=%d",
			traceID, sql, elapsed, rows)
		return
	}

	// Warn 级别：只记录 >100ms 的慢查询（生产环境默认）
	if elapsed > 100*time.Millisecond {
		metrics.RecordSlowQuery(sql, elapsed)
		log.Warnf("[MySQL] traceID=%v | sql=%s | duration=%v | rows=%d | slow_query=true",
			traceID, sql, elapsed, rows)
	}
}

// Print 实现 gorm logger.Interface，在触发慢查询时记录指标与告警。
func (s *SlowQueryLogger) Print(values ...interface{}) {
	if len(values) > 0 {
		if sq, ok := values[0].(string); ok && sq == "slow query" {
			metrics.RecordSlowQuery(values[1].(string), values[2].(time.Duration))
			log.Warnf("Slow query: %s, duration: %v", values[3], values[2])
		}
	}
}

// WithContext 返回携带指定 context 的 *gorm.DB，便于链路追踪与超时控制。
func WithContext(ctx context.Context) *gorm.DB {
	return db.WithContext(ctx)
}

// Close 关闭全局数据库连接，未初始化时直接返回 nil。
func Close() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Ping 探测数据库连接可用性，供 metrics.StartHealthReporter 等健康上报使用。
func Ping(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// TxFunc 表示在事务中执行的处理函数，接收事务 *gorm.DB 并返回错误。
type TxFunc func(*gorm.DB) error

// Transaction 在数据库事务中执行给定的 TxFunc，由 GORM 自动提交或回滚。
func Transaction(fn TxFunc) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}

// classifySQL 按 SQL 前缀粗分操作类型，用于 db_operations_total 的 operation label。
func classifySQL(sql string) string {
	s := strings.TrimSpace(strings.ToUpper(sql))
	switch {
	case strings.HasPrefix(s, "SELECT"):
		return "query"
	case strings.HasPrefix(s, "INSERT"):
		return "insert"
	case strings.HasPrefix(s, "UPDATE"):
		return "update"
	case strings.HasPrefix(s, "DELETE"):
		return "delete"
	default:
		return "other"
	}
}
