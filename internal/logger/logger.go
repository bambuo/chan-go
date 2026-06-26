// Package logger 提供日志记录器的封装，隐藏底层 zap 实现细节。
package logger

import "go.uber.org/zap"

// Logger 是项目统一的日志记录器，通过方法注入使用，不依赖全局状态。
type Logger struct {
	sugar *zap.SugaredLogger
}

// New 创建一个生产级别的 JSON 格式日志记录器。
func New() (*Logger, error) {
	l, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	return &Logger{sugar: l.Sugar()}, nil
}

// Sync 刷新缓冲的日志，应在进程退出前调用。
func (l *Logger) Sync() error {
	return l.sugar.Sync()
}

// With 返回一个附加了固定字段的新 Logger，用于子模块上下文。
func (l *Logger) With(args ...any) *Logger {
	return &Logger{sugar: l.sugar.With(args...)}
}

// Info 记录 info 级别日志。
func (l *Logger) Info(msg string, keysAndValues ...any) {
	l.sugar.Infow(msg, keysAndValues...)
}

// Warn 记录 warn 级别日志。
func (l *Logger) Warn(msg string, keysAndValues ...any) {
	l.sugar.Warnw(msg, keysAndValues...)
}

// Error 记录 error 级别日志。
func (l *Logger) Error(msg string, keysAndValues ...any) {
	l.sugar.Errorw(msg, keysAndValues...)
}
