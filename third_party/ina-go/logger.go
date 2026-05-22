package ina

import (
	"fmt"
	"io"
	"os"
)

// Logger 是 ina-go 对外暴露的日志接口。aiscan 可以注入自己的 telemetry.Logger
// 适配器, 也可以用内置的 StdLogger / NopLogger。
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// NopLogger 丢弃所有日志, 是 Config 默认值。
func NopLogger() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Debugf(string, ...any) {}
func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Warnf(string, ...any)  {}
func (nopLogger) Errorf(string, ...any) {}

// StdLogger 写到指定 io.Writer, 带 [level] 前缀。out 为 nil 时使用 stderr。
func StdLogger(out io.Writer) Logger {
	if out == nil {
		out = os.Stderr
	}
	return &stdLogger{out: out}
}

type stdLogger struct{ out io.Writer }

func (l *stdLogger) emit(level, format string, args ...any) {
	fmt.Fprintf(l.out, "[ina][%s] %s\n", level, fmt.Sprintf(format, args...))
}
func (l *stdLogger) Debugf(format string, args ...any) { l.emit("debug", format, args...) }
func (l *stdLogger) Infof(format string, args ...any)  { l.emit("info", format, args...) }
func (l *stdLogger) Warnf(format string, args ...any)  { l.emit("warn", format, args...) }
func (l *stdLogger) Errorf(format string, args ...any) { l.emit("error", format, args...) }
