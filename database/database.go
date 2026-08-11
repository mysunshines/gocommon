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

func GetDB() *gorm.DB {
	return db
}

// GetDBConn 返回数据库连接（用于外部包访问）
func GetDBConn() *gorm.DB {
	return db
}

type SlowQueryLogger struct {
	LogLevel logger.LogLevel // 日志级别过滤
}

func (s *SlowQueryLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *s
	newLogger.LogLevel = level
	return &newLogger
}

func (s *SlowQueryLogger) Error(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Errorf("[MySQL] traceID=%v | err=%v", traceID, values)
	}
}

func (s *SlowQueryLogger) Info(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 && s.LogLevel >= logger.Info {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Infof("[MySQL] traceID=%v | %v", traceID, values)
	}
}

func (s *SlowQueryLogger) Warn(ctx context.Context, _ string, values ...interface{}) {
	if len(values) > 0 && s.LogLevel >= logger.Warn {
		traceID := middleware.GetTraceIDFromContext(ctx)
		log.Warnf("[MySQL] traceID=%v | %v", traceID, values)
	}
}

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

func (s *SlowQueryLogger) Print(values ...interface{}) {
	if len(values) > 0 {
		if sq, ok := values[0].(string); ok && sq == "slow query" {
			metrics.RecordSlowQuery(values[1].(string), values[2].(time.Duration))
			log.Warnf("Slow query: %s, duration: %v", values[3], values[2])
		}
	}
}

func WithContext(ctx context.Context) *gorm.DB {
	return db.WithContext(ctx)
}

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

type TxFunc func(*gorm.DB) error

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
