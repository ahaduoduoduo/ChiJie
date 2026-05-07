package util

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

// LogLevel 表示日志级别。
type LogLevel int32

const (
	// LogLevelDebug 输出调试细节。
	LogLevelDebug LogLevel = iota
	// LogLevelInfo 输出常规运行信息（默认级别）。
	LogLevelInfo
	// LogLevelWarn 输出警告。
	LogLevelWarn
	// LogLevelError 仅输出错误。
	LogLevelError
)

// currentLevel 全局原子变量，避免并发读写需要锁。
var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(LogLevelInfo))
}

// SetLogLevel 通过 yaml 配置中的字符串设置日志级别。未识别值视为 info。
func SetLogLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		currentLevel.Store(int32(LogLevelDebug))
	case "warn", "warning":
		currentLevel.Store(int32(LogLevelWarn))
	case "error":
		currentLevel.Store(int32(LogLevelError))
	default:
		currentLevel.Store(int32(LogLevelInfo))
	}
}

// CurrentLogLevel 返回当前生效的日志级别。
func CurrentLogLevel() LogLevel {
	return LogLevel(currentLevel.Load())
}

// LogLevelName 返回当前级别的配置名。
func LogLevelName() string {
	switch CurrentLogLevel() {
	case LogLevelDebug:
		return "debug"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "error"
	default:
		return "info"
	}
}

// Debugf 仅当级别 ≤ Debug 时输出。
func Debugf(format string, args ...any) {
	if LogLevel(currentLevel.Load()) <= LogLevelDebug {
		_ = log.Output(2, "[DEBUG] "+fmt.Sprintf(format, args...))
	}
}

// Infof 仅当级别 ≤ Info 时输出。
func Infof(format string, args ...any) {
	if LogLevel(currentLevel.Load()) <= LogLevelInfo {
		_ = log.Output(2, fmt.Sprintf(format, args...))
	}
}

// Warnf 仅当级别 ≤ Warn 时输出。
func Warnf(format string, args ...any) {
	if LogLevel(currentLevel.Load()) <= LogLevelWarn {
		_ = log.Output(2, "[WARN] "+fmt.Sprintf(format, args...))
	}
}

// Errorf 始终输出错误。
func Errorf(format string, args ...any) {
	_ = log.Output(2, "[ERROR] "+fmt.Sprintf(format, args...))
}
