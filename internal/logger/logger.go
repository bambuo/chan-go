// Package logger 提供日志记录器的封装，隐藏底层 zap 实现细节。
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 是项目统一的日志记录器，通过方法注入使用，不依赖全局状态。
type Logger struct {
	sugar *zap.SugaredLogger
}

// New 创建一个生产级别的 JSON 格式日志记录器。
// - time 字段使用 ISO 8601 格式（2006-01-02T15:04:05.000+0800）
// - caller 跳过封装层指向真实调用方。
func New() (*Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02T15:04:05.000Z0700")

	l, err := cfg.Build(zap.AddCallerSkip(1))
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
