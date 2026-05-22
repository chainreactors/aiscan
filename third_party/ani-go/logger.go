package ani

import (
	"fmt"
	"io"
	"os"
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

func NopLogger() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Debugf(string, ...any) {}
func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Warnf(string, ...any)  {}
func (nopLogger) Errorf(string, ...any) {}

func StdLogger(out io.Writer) Logger {
	if out == nil {
		out = os.Stderr
	}
	return &stdLogger{out: out}
}

type stdLogger struct{ out io.Writer }

func (l *stdLogger) emit(level, format string, args ...any) {
	fmt.Fprintf(l.out, "[ani][%s] %s\n", level, fmt.Sprintf(format, args...))
}
func (l *stdLogger) Debugf(format string, args ...any) { l.emit("debug", format, args...) }
func (l *stdLogger) Infof(format string, args ...any)  { l.emit("info", format, args...) }
func (l *stdLogger) Warnf(format string, args ...any)  { l.emit("warn", format, args...) }
func (l *stdLogger) Errorf(format string, args ...any) { l.emit("error", format, args...) }
