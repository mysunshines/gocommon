package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"

	"github.com/sirupsen/logrus"
)

var (
	logger      *logrus.Logger
	once        sync.Once
	currentDate string        // 当前日志文件对应的日期
	logDirPath  string        // 日志目录
	serviceName string        // 当前服务名
	stopCh      chan struct{} // 停止轮转信号
)

func Init(logDir, logLevel, svcName string) {
	once.Do(func() {
		logger = logrus.New()
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: constants.DateTimeFormat,
		})

		level, err := logrus.ParseLevel(logLevel)
		if err != nil {
			level = logrus.InfoLevel
		}
		logger.SetLevel(level)

		// 保存参数，供后续轮转使用
		logDirPath = logDir
		serviceName = svcName
		stopCh = make(chan struct{})

		if logDir != "" {
			os.MkdirAll(logDir, constants.FilePermDir)
			today := time.Now().Format(constants.DateFormat)
			currentDate = today
			logFile := filepath.Join(logDir, fmt.Sprintf(constants.LogFileNameFmt, svcName, today))
			file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, constants.FilePermFile)
			if err == nil {
				// 同时输出到 stdout 和文件，确保 Docker logs 和文件都有记录
				logger.SetOutput(io.MultiWriter(os.Stdout, file))
			}
		}

		logger.WithFields(logrus.Fields{
			constants.LogFieldService: svcName,
			constants.LogFieldEnv:     os.Getenv(constants.EnvAppEnv),
		}).Info(constants.LogInitMsg)

		// 启动后台日志轮转（仅在文件输出模式下）
		if logDir != "" {
			go rotateDaemon()
		}
	})
}

func GetLogger() *logrus.Logger {
	if logger == nil {
		Init(constants.DefaultLogDir, constants.DefaultLogLevel, constants.ServiceNameGateway)
	}
	return logger
}

func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}

func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}

// rotateDaemon 后台守护 goroutine，每分钟检查日期是否变化，自动轮转日志文件
func rotateDaemon() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			today := time.Now().Format(constants.DateFormat)
			if today != currentDate {
				if err := doRotate(today); err != nil {
					Errorf("auto log rotation failed: %v", err)
				} else {
					Infof("log rotated to new date: %s", today)
				}
			}
		case <-stopCh:
			return
		}
	}
}

// doRotate 执行实际的日志文件轮转
func doRotate(today string) error {
	if logger == nil || logDirPath == "" {
		return nil
	}
	logFile := filepath.Join(logDirPath, fmt.Sprintf(constants.LogFileNameFmt, serviceName, today))
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, constants.FilePermFile)
	if err != nil {
		return err
	}
	logger.SetOutput(io.MultiWriter(os.Stdout, file))
	currentDate = today
	return nil
}

// RotateLog 手动触发日志轮转（用于特殊场景，如收到 SIGHUP 信号时强制轮转）
func RotateLog(svcName string) error {
	if logger == nil || logDirPath == "" {
		return nil
	}
	today := time.Now().Format(constants.DateFormat)
	if today == currentDate {
		return nil // 同一天，无需轮转
	}
	return doRotate(today)
}

// SetLevel 动态调整运行期日志级别，供配置中心热更等场景即时生效，无需重启。
// level 为 logrus 支持的字符串：debug / info / warn / error / fatal / panic。
// 非法级别会被忽略（保持当前级别）并返回错误。
func SetLevel(level string) error {
	if logger == nil {
		// 尚未初始化时，先以默认参数初始化再设置，避免 nil panic。
		Init(constants.DefaultLogDir, constants.DefaultLogLevel, constants.ServiceNameGateway)
	}
	lv, err := logrus.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("log: invalid level %q: %w", level, err)
	}
	logger.SetLevel(lv)
	return nil
}

// StopRotation 停止后台日志轮转（优雅关闭时调用）
func StopRotation() {
	if stopCh != nil {
		close(stopCh)
	}
}
