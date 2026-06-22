package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Kline 持有Kline实体的schema定义。
// 存储从币安接收的1分钟闭合K线数据。
type Kline struct {
	ent.Schema
}

// Fields of the Kline.
func (Kline) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").
			Unique().
			Immutable().
			Comment("自增主键"),
		field.String("symbol").
			NotEmpty().
			Comment("交易对符号，例如 BTCUSDT"),
		field.Float("open").
			Comment("开盘价"),
		field.Float("high").
			Comment("最高价"),
		field.Float("low").
			Comment("最低价"),
		field.Float("close").
			Comment("收盘价"),
		field.Float("volume").
			Comment("成交量"),
		field.Int64("open_time").
			Comment("K线开盘时间，Unix毫秒"),
		field.Int64("close_time").
			Comment("K线收盘时间，Unix毫秒"),
		field.Time("created_at").
			Comment("记录创建时间"),
	}
}

// Indexes of the Kline.
func (Kline) Indexes() []ent.Index {
	return []ent.Index{
		// (symbol, open_time)唯一索引，用于去重。
		index.Fields("symbol", "open_time").
			Unique(),
		// 用于高效时间范围查询的索引。
		index.Fields("symbol", "close_time"),
	}
}
