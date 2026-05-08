package telemetry

import (
	"io"
	"os"

	"github.com/chainreactors/logs"
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Importantf(format string, args ...any)
}

type LogConfig struct {
	Debug  bool
	Quiet  bool
	Output io.Writer
	Color  bool
}

type logsLogger struct {
	base *logs.Logger
}

func NewLogger(cfg LogConfig) Logger {
	level := logs.WarnLevel
	if cfg.Debug {
		level = logs.DebugLevel
	} else if cfg.Quiet {
		level = logs.ErrorLevel
	}
	base := logs.NewLogger(level)
	if cfg.Output != nil {
		base.SetOutput(cfg.Output)
	} else {
		base.SetOutput(os.Stderr)
	}
	base.SetFormatter(map[logs.Level]string{
		logs.DebugLevel:     "[debug] %s\n",
		logs.InfoLevel:      "[info] %s\n",
		logs.WarnLevel:      "[warn] %s\n",
		logs.ErrorLevel:     "[error] %s\n",
		logs.ImportantLevel: "[info] %s\n",
	})
	base.SetColor(cfg.Color)
	return logsLogger{base: base}
}

func GlobalLogger(cfg LogConfig) Logger {
	logger := NewLogger(cfg)
	if adapter, ok := logger.(logsLogger); ok {
		logs.Log = adapter.base
	}
	return logger
}

func NopLogger() Logger {
	return nopLogger{}
}

type nopLogger struct{}

func (nopLogger) Debugf(string, ...any)     {}
func (nopLogger) Infof(string, ...any)      {}
func (nopLogger) Warnf(string, ...any)      {}
func (nopLogger) Errorf(string, ...any)     {}
func (nopLogger) Importantf(string, ...any) {}

func (l logsLogger) Debugf(format string, args ...any) {
	l.base.Debugf(format, args...)
}

func (l logsLogger) Infof(format string, args ...any) {
	l.base.Infof(format, args...)
}

func (l logsLogger) Warnf(format string, args ...any) {
	l.base.Warnf(format, args...)
}

func (l logsLogger) Errorf(format string, args ...any) {
	l.base.Errorf(format, args...)
}

func (l logsLogger) Importantf(format string, args ...any) {
	l.base.Importantf(format, args...)
}
