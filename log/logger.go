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
	asyncWriter *AsyncWriter // 异步写入器，包裹 stdout/文件，避免日志 I/O 阻塞请求路径
	once        sync.Once
	currentDate string        // 当前日志文件对应的日期
	logDirPath  string        // 日志目录
	serviceName string        // 当前服务名
	stopCh      chan struct{} // 停止轮转信号
)

// Init 初始化全局日志器：设置日志级别、服务名、输出目标（stdout + 文件）并启动后台日志轮转。
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
				// 同时输出到 stdout 和文件，确保 Docker logs 和文件都有记录。
				// 再用 AsyncWriter 包裹，使日志 I/O 异步化，避免磁盘/管道写入慢阻塞请求 goroutine。
				asyncWriter = NewAsyncWriter(io.MultiWriter(os.Stdout, file), constants.AsyncLogBufferSize)
				logger.SetOutput(asyncWriter)
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

// GetLogger 获取全局日志器实例；未初始化时按默认参数自动初始化。
func GetLogger() *logrus.Logger {
	if logger == nil {
		Init(constants.DefaultLogDir, constants.DefaultLogLevel, constants.ServiceNameGateway)
	}
	return logger
}

// Info 输出信息（info）级别日志。
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Infof 按格式输出信息（info）级别日志。
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Debug 输出调试（debug）级别日志。
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// IsDebug 返回当前日志级别是否开启 debug，调用方可用其前置判断是否执行
// 高开销的日志拼装（如 protojson 序列化 body），避免非 debug 场景下浪费 CPU。
func IsDebug() bool {
	// 尚未初始化时按默认级别（info）处理，视为未开启 debug。
	if logger == nil {
		return constants.DefaultLogLevel == "debug"
	}
	return logger.GetLevel() <= logrus.DebugLevel
}

// Debugf 按格式输出调试（debug）级别日志。
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Warn 输出警告（warn）级别日志。
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Warnf 按格式输出警告（warn）级别日志。
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Error 输出错误（error）级别日志。
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Errorf 按格式输出错误（error）级别日志。
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatal 输出致命（fatal）级别日志并退出进程（退出前先落盘残留日志）。
func Fatal(args ...interface{}) {
	FlushLog() // 退出前先把残留日志落盘，避免崩溃日志丢失
	GetLogger().Fatal(args...)
}

// Fatalf 按格式输出致命（fatal）级别日志并退出进程（退出前先落盘残留日志）。
func Fatalf(format string, args ...interface{}) {
	FlushLog() // 退出前先把残留日志落盘，避免崩溃日志丢失
	GetLogger().Fatalf(format, args...)
}

// WithField 返回一个携带单个字段的日志上下文（*logrus.Entry）。
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// WithFields 返回一个携带多个字段的日志上下文（*logrus.Entry）。
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
	// 热替换异步 writer 的底层目标，保留异步管道，不回退到同步写入。
	asyncWriter.SetDst(io.MultiWriter(os.Stdout, file))
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

// StopRotation 停止后台日志轮转并优雅 drain 异步日志（优雅关闭时调用）。
func StopRotation() {
	if stopCh != nil {
		close(stopCh)
	}
	FlushLog()
}

// FlushLog 优雅关闭异步写入器：阻塞直到残留日志全部写完。
// 进程退出前应调用一次（StopRotation 已自动调用），确保关键日志不丢。
// 多次调用安全（内部用 once 保护）。
func FlushLog() {
	if asyncWriter != nil {
		_ = asyncWriter.Close()
	}
}
