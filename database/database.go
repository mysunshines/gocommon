package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once
)

type Config struct {
	Host         string
	Port         int
	User         string
	Password     string
	DBName       string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifeTime  int
}

func Init(cfg *config.DatabaseConfig, env string) error {
	var initErr error
	once.Do(func() {
		gormLogger := logger.Default
		if env == "development" {
			gormLogger = logger.Default.LogMode(logger.Info)
		}

		var err error
		db, err = gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
			Logger: gormLogger,
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

type SlowQueryLogger struct{}

func (s *SlowQueryLogger) LogMode(level logger.LogLevel) logger.Interface {
	return s
}

func (s *SlowQueryLogger) Error(_ context.Context, _ string, values ...interface{}) {
	if len(values) > 0 {
		log.Errorf("DB Error: %v", values)
	}
}

func (s *SlowQueryLogger) Info(_ context.Context, _ string, values ...interface{}) {
	if len(values) > 0 {
		log.Infof("DB Info: %v", values)
	}
}

func (s *SlowQueryLogger) Warn(_ context.Context, _ string, values ...interface{}) {
	if len(values) > 0 {
		log.Warnf("DB Warn: %v", values)
	}
}

func (s *SlowQueryLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	if elapsed > 100*time.Millisecond {
		metrics.RecordSlowQuery(sql, elapsed)
		log.Warnf("Slow query: %s, duration: %v, rows: %d", sql, elapsed, rows)
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
