// Package log 为整个系统提供结构化日志。
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	serviceName = "chanlun-system"
)

// Config 持有日志配置。
type Config struct {
	Level     string // debug, info, warn, error
	JSON      bool   // true输出JSON格式，false输出文本格式
	AddSource bool   // 是否包含源文件信息
	Output    io.Writer
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Level:     "info",
		JSON:      false,
		AddSource: true,
		Output:    os.Stdout,
	}
}

// Init 初始化全局结构化日志记录器。
func Init(cfg Config) {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: replaceAttr,
	}

	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(cfg.Output, opts)
	} else {
		handler = slog.NewTextHandler(cfg.Output, opts)
	}

	logger := slog.New(handler).With(
		slog.String("service", serviceName),
	)
	slog.SetDefault(logger)
}

// replaceAttr 自定义日志属性输出。
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
	}
	if a.Key == slog.SourceKey {
		if source, ok := a.Value.Any().(*slog.Source); ok {
			a.Value = slog.StringValue(filepath.Base(source.File) + ":" + itoa(source.Line))
		}
	}
	return a
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// WithRequestID 向日志上下文中添加请求ID。
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, reqID)
}

type ctxKey string

const ctxKeyRequestID ctxKey = "request_id"

// WithContext 将上下文值添加到日志记录中。
func WithContext(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	if reqID, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		logger = logger.With("request_id", reqID)
	}
	return logger
}

// Source 返回调用函数的文件与行号。
func Source() string {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return "unknown"
	}
	return filepath.Base(file) + ":" + itoa(line)
}

// Component 为指定组件返回子日志记录器。
func Component(name string) *slog.Logger {
	return slog.Default().With("component", name)
}
