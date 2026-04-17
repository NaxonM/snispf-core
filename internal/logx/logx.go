package logx

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

type Level int32

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(LevelInfo))
}

func ParseLevel(text string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "info":
		return LevelInfo, true
	case "error", "err":
		return LevelError, true
	case "warn", "warning":
		return LevelWarn, true
	case "debug":
		return LevelDebug, true
	default:
		return LevelInfo, false
	}
}

func SetLevel(level Level) {
	currentLevel.Store(int32(level))
}

func SetLevelString(text string) error {
	level, ok := ParseLevel(text)
	if !ok {
		return fmt.Errorf("invalid log level %q", strings.TrimSpace(text))
	}
	SetLevel(level)
	return nil
}

func LevelString() string {
	return levelToString(Level(currentLevel.Load()))
}

func Errorf(format string, args ...interface{}) {
	logAt(LevelError, format, args...)
}

func Warnf(format string, args ...interface{}) {
	logAt(LevelWarn, format, args...)
}

func Infof(format string, args ...interface{}) {
	logAt(LevelInfo, format, args...)
}

func Debugf(format string, args ...interface{}) {
	logAt(LevelDebug, format, args...)
}

func logAt(level Level, format string, args ...interface{}) {
	if level > Level(currentLevel.Load()) {
		return
	}
	log.Printf("[%s] %s", levelToString(level), fmt.Sprintf(format, args...))
}

func levelToString(level Level) string {
	switch level {
	case LevelError:
		return "ERROR"
	case LevelWarn:
		return "WARN"
	case LevelDebug:
		return "DEBUG"
	default:
		return "INFO"
	}
}
