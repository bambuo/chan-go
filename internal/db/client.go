// Package db 提供存储K线数据的SQLite数据库访问。
// 使用database/sql和SQLite驱动进行运行时操作。
// schema/包中的ent schema可用于未来的代码生成。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"trade/internal/log"
	"trade/internal/types"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"
)

// Client 封装SQLite数据库连接，提供K线存储操作。
type Client struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewClient 创建新的SQLite数据库客户端并执行自动迁移。
// dbPath: SQLite数据库文件路径（例如 "file:data/klines.db?cache=shared&_journal_mode=WAL"）。
func NewClient(ctx context.Context, dbPath string) (*Client, error) {
	logger := log.Component("db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开sqlite: %w", err)
	}

	// 配置SQLite连接池。
	db.SetMaxOpenConns(1) // SQLite只支持一个写入者。
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	c := &Client{
		db:     db,
		logger: logger,
	}

	if err := c.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移: %w", err)
	}

	logger.Info("数据库已初始化", "path", dbPath)
	return c, nil
}

// migrate 如果表不存在则创建kline表和索引。
func (c *Client) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS klines (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		open REAL NOT NULL,
		high REAL NOT NULL,
		low REAL NOT NULL,
		close REAL NOT NULL,
		volume REAL NOT NULL,
		open_time INTEGER NOT NULL,
		close_time INTEGER NOT NULL,
		created_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_klines_symbol_open_time
		ON klines(symbol, open_time);

	CREATE INDEX IF NOT EXISTS idx_klines_symbol_close_time
		ON klines(symbol, close_time);
	`

	_, err := c.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	return nil
}

// InsertKline 将闭合的K线插入数据库。
// 通过唯一索引(symbol, open_time)使用INSERT OR IGNORE实现去重。
// 仅应写入闭合的K线（IsClosed == true）。
func (c *Client) InsertKline(ctx context.Context, k *types.Kline) error {
	if !k.IsClosed {
		return fmt.Errorf("不能插入未闭合的K线: symbol=%s open_time=%d", k.Symbol, k.OpenTime)
	}

	start := time.Now()

	result, err := c.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO klines (symbol, open, high, low, close, volume, open_time, close_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		k.Symbol, k.Open.InexactFloat64(), k.High.InexactFloat64(), k.Low.InexactFloat64(),
		k.Close.InexactFloat64(), k.Volume.InexactFloat64(), k.OpenTime, k.CloseTime,
	)

	elapsed := time.Since(start)
	if err != nil {
		c.logger.Error("插入K线失败",
			"symbol", k.Symbol,
			"open_time", k.OpenTime,
			"error", err,
			"elapsed_ms", elapsed.Milliseconds(),
		)
		return fmt.Errorf("插入K线: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.logger.Debug("重复K线已跳过",
			"symbol", k.Symbol,
			"open_time", k.OpenTime,
		)
		return nil
	}

	c.logger.Debug("K线已插入",
		"symbol", k.Symbol,
		"open_time", k.OpenTime,
		"close", k.Close,
		"elapsed_ms", elapsed.Milliseconds(),
	)
	return nil
}

// InsertClosedKlineHandler 返回一个事件总线处理器，用于插入闭合的K线。
func (c *Client) InsertClosedKlineHandler(ctx context.Context) func(types.KlineEvent) {
	return func(evt types.KlineEvent) {
		if evt.Type != types.EventKlineClosed || evt.Kline == nil {
			return
		}
		if err := c.InsertKline(ctx, evt.Kline); err != nil {
			c.logger.Error("事件处理器插入失败",
				"symbol", evt.Kline.Symbol,
				"open_time", evt.Kline.OpenTime,
				"error", err,
			)
		}
	}
}

// QueryKlines 在时间范围内检索K线。
// 扩展预留点，用于未来的查询需求（如加载历史数据供缠论分析）。
func (c *Client) QueryKlines(ctx context.Context, symbol string, startTime, endTime int64, limit int) ([]*types.Kline, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := c.db.QueryContext(ctx,
		`SELECT symbol, open, high, low, close, volume, open_time, close_time
		 FROM klines
		 WHERE symbol = ? AND open_time >= ? AND open_time <= ?
		 ORDER BY open_time ASC
		 LIMIT ?`,
		symbol, startTime, endTime, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("查询K线: %w", err)
	}
	defer rows.Close()

	var klines []*types.Kline
	for rows.Next() {
		var open, high, low, close, volume float64
		k := &types.Kline{}
		if err := rows.Scan(&k.Symbol, &open, &high, &low, &close, &volume, &k.OpenTime, &k.CloseTime); err != nil {
			return nil, fmt.Errorf("扫描K线行: %w", err)
		}
		k.Open = decimal.NewFromFloat(open)
		k.High = decimal.NewFromFloat(high)
		k.Low = decimal.NewFromFloat(low)
		k.Close = decimal.NewFromFloat(close)
		k.Volume = decimal.NewFromFloat(volume)
		k.IsClosed = true
		klines = append(klines, k)
	}
	return klines, rows.Err()
}

// Close 优雅关闭数据库连接。
func (c *Client) Close() error {
	return c.db.Close()
}
