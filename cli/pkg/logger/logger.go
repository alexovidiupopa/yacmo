// Package logger provides a simple structured logger for YACMO.
package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Level represents log severity.
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

// Logger is a simple leveled logger.
type Logger struct {
	level  Level
	logger *log.Logger
}

// New creates a new Logger with the given level string (debug, info, warn, error).
func New(level string) *Logger {
	l := &Logger{
		level:  parseLevel(level),
		logger: log.New(os.Stdout, "", 0),
	}
	return l
}

func parseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn", "warning":
		return WARN
	case "error":
		return ERROR
	default:
		return INFO
	}
}

func (l *Logger) log(level Level, prefix string, format string, args ...interface{}) {
	if level < l.level {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	l.logger.Printf("%s [%s] %s", ts, prefix, msg)
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, "DEBUG", format, args...)
}

// Info logs an info message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, "INFO ", format, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, "WARN ", format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, "ERROR", format, args...)
}
