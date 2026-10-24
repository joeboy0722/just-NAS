package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Tag       string `json:"tag"`
	Message   string `json:"message"`
}

type RingLogger struct {
	mu      sync.RWMutex
	logs    []LogEntry
	maxSize int
	file    *os.File
}

var globalLogger *RingLogger
var once sync.Once

func GetLogger() *RingLogger {
	once.Do(func() {
		globalLogger = &RingLogger{
			logs:    make([]LogEntry, 0, 500),
			maxSize: 500,
		}
		globalLogger.initFileLog()
	})
	return globalLogger
}

func (l *RingLogger) initFileLog() {
	if runtime.GOOS == "linux" {
		logPath := "/var/log/nas-server.log"
		_ = os.MkdirAll(filepath.Dir(logPath), 0755)
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			l.file = f
			log.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}
}

func (l *RingLogger) Log(level, tag, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	now := time.Now().Format("2006-01-02 15:04:05")

	entry := LogEntry{
		Timestamp: now,
		Level:     strings.ToUpper(level),
		Tag:       strings.ToUpper(tag),
		Message:   msg,
	}

	l.mu.Lock()
	if len(l.logs) >= l.maxSize {
		l.logs = l.logs[1:]
	}
	l.logs = append(l.logs, entry)
	l.mu.Unlock()

	// 原生 log 輸出
	log.Printf("[%s] [%s] %s", entry.Tag, entry.Level, msg)
}

func (l *RingLogger) GetLogs(limit int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > len(l.logs) {
		limit = len(l.logs)
	}

	result := make([]LogEntry, limit)
	copy(result, l.logs[len(l.logs)-limit:])
	return result
}

func (l *RingLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = make([]LogEntry, 0, l.maxSize)
}

// 捷徑輔助函式
func Info(tag, format string, v ...interface{}) {
	GetLogger().Log("INFO", tag, format, v...)
}

func Error(tag, format string, v ...interface{}) {
	GetLogger().Log("ERROR", tag, format, v...)
}

func Warn(tag, format string, v ...interface{}) {
	GetLogger().Log("WARN", tag, format, v...)
}
