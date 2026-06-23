# Chan Theory Signal Analysis System

[![Go](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

基于**缠中说禅理论**的实时多级别信号分析引擎。消费 Redis Stream 中的 K 线数据，计算多级别缠论结构并产出结构化买卖点信号。

## 架构概览

系统采用 **M0–M10 模块化架构**，模块间通过通用事件总线（`eventbus.GenericBus`）通信：

```
┌─────────────────────────────────────────────────────────┐
│                      M8 输出网关                         │
│  ├─ Redis Stream XADD (实时信号推送)                      │
│  └─ REST API (快照查询)                                  │
├─────────────────────────────────────────────────────────┤
│  M6 共振引擎     M5 信号引擎      M4 递归级别             │
│  (区间套/跨层共振)  (三类买卖点)    (L1→L2→L3→L4)         │
├─────────────────────────────────────────────────────────┤
│  M3 结构树 (笔/中枢/走势类型的版本化存储)                   │
├─────────────────────────────────────────────────────────┤
│  M2 缠论核心 Pipeline (K线合并→分型→笔→线段→中枢→走势类型)  │
├─────────────────────────────────────────────────────────┤
│  M1 输入网关 (Redis Stream XREADGROUP)   M0 快照层        │
└─────────────────────────────────────────────────────────┘
                         │
                    Redis Stream
                    chan:klines:{symbol}
```

### 模块职责

| 模块 | 职责 |
|------|------|
| **M0** | 快照层 — 定期持久化系统状态，支持故障恢复 |
| **M1** | 输入网关 — 通过 XREADGROUP 消费 Redis Stream K 线 |
| **M2** | 缠论核心 Pipeline — K 线合并 → 分型 → 笔 → 线段 → 中枢 → 走势类型 |
| **M3** | 结构树 — 版本化结构元素存储，支持漂移与回滚 |
| **M4** | 递归级别 — L1→L2→L3→L4 多级别递归构建 |
| **M5** | 信号引擎 — 三类买卖点识别与状态机（candidate→confirmed→invalidated） |
| **M6** | 共振引擎 — 区间套 (G-2)、跨层共振 (G-1)、方向过滤 (A3) |
| **M7** | 状态存储 — 信号/偏移量等运行时状态 |
| **M8** | 输出网关 — Redis Stream XADD + REST API |
| **M10** | 可观测性 — Prometheus 指标 |

## 快速开始

### 前置条件

- [Go 1.26+](https://go.dev/dl/)
- [Redis 7+](https://redis.io/download/)（本地运行需启动 Redis）

### 安装

```bash
git clone <repo-url> && cd chan-go
go mod tidy
go build -o bin/server ./cmd/server/
```

### 启动引擎

```bash
# 启动 Redis（如未运行）
redis-server

# 启动信号分析引擎
go run ./cmd/server/ --symbols BTCUSDT,ETHUSDT
```

### 注入测试数据

```bash
python3 -c '
import subprocess, time
base_ms = int((time.time() - 900) * 1000)
klines = [
    (100,102,99,101), (101,105,100,104), (104,108,103,107),
    (107,110,106,109), (109,112,108,111), (111,113,110,112),
    (112,114,105,106), (106,108,103,104), (104,106,100,101),
    (101,103,98,99),  (99,100,95,96),    (96,98,94,97),
    (97,102,96,101),  (101,106,100,105), (105,109,104,108),
]
for i,(o,h,l,c) in enumerate(klines):
    ts = base_ms + i * 60000
    subprocess.run(["redis-cli","XADD","chan:klines:TEST","*",
        "symbol","TEST","ts",str(ts),
        "open",str(o),"high",str(h),"low",str(l),"close",str(c),"volume","1000"])
'
```

### 验证运行状态

```bash
# 健康检查
curl http://127.0.0.1:8080/v1/health

# 查看结构数据
curl http://127.0.0.1:8080/v1/structure/TEST

# 查看当前信号
curl http://127.0.0.1:8080/v1/signals/TEST/current
```

## 配置

### 命令行参数

| 参数 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `--log-level` | `CL_LOG_LEVEL` | `info` | 日志级别 (debug/info/warn/error) |
| `--log-json` | `CL_LOG_JSON` | `true` | JSON 格式输出日志 |
| `--symbols` | `CL_SYMBOLS` | `BTCUSDT,ETHUSDT` | 交易对列表，逗号分隔 |
| `--redis-addr` | `CL_REDIS_ADDR` | `localhost:6379` | Redis 地址 |
| `--redis-password` | `CL_REDIS_PASSWORD` | `` | Redis 密码 |
| `--redis-db` | `CL_REDIS_DB` | `0` | Redis DB 编号 |
| `--stream-prefix` | `CL_STREAM_PREFIX` | `chan:klines` | K 线输入 Stream 前缀 |
| `--consumer-group` | `CL_CONSUMER_GROUP` | `chan-engine` | 消费组名称 |
| `--http-addr` | `CL_HTTP_ADDR` | `0.0.0.0` | HTTP 监听地址 |
| `--http-port` | `CL_HTTP_PORT` | `8080` | HTTP 端口 |
| `--output-stream-prefix` | `CL_OUTPUT_STREAM_PREFIX` | `chan:signals` | Redis Stream 输出前缀 |
| `--snapshot-dir` | `CL_SNAPSHOT_DIR` | `data/snapshots` | 快照存储目录 |
| `--snapshot-period` | `CL_SNAPSHOT_PERIOD` | `300` | 快照周期（秒） |
| `--snapshot-retain` | `CL_SNAPSHOT_RETAIN` | `24` | 保留最近快照数 |

所有参数均可通过环境变量 `CL_*` 覆盖。

## 输入/输出协议

### 输入：Redis Stream `chan:klines:{symbol}`

每条消息需包含以下字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `symbol` | string | 交易对（如 `BTCUSDT`） |
| `ts` | int64 | K 线开盘时间（Unix 毫秒） |
| `open` | decimal | 开盘价 |
| `high` | decimal | 最高价 |
| `low` | decimal | 最低价 |
| `close` | decimal | 收盘价 |
| `volume` | decimal | 成交量 |

### 输出：Redis Stream `chan:signals:{symbol}`

每条消息包含：

| 字段 | 说明 |
|------|------|
| `type` | 事件类型：`signal.created` / `signal.stateChanged` / `resonance.triggered` |
| `ts` | 事件时间戳（Unix 毫秒） |
| `data` | JSON 序列化的信号对象 |

### REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/health` | 健康检查 |
| GET | `/v1/signals/:symbol/current` | 当前活跃信号列表 |
| GET | `/v1/signals/:symbol/:signalId` | 单个信号详情 |
| GET | `/v1/structure/:symbol` | 缠论结构树（笔/中枢/走势类型） |
| GET | `/v1/levels/:symbol` | 多级别递归统计 |

## 信号系统

### 六类标准买卖点

| 类型 | 名称 | 说明 |
|------|------|------|
| `BUY_1` | 一买 | 趋势背驰后的第一买点 |
| `BUY_2` | 二买 | 一买后回调不破前低的买点 |
| `BUY_3` | 三买 | 突破中枢后回调不进入中枢的买点 |
| `SELL_1` | 一卖 | 趋势背驰后的第一卖点 |
| `SELL_2` | 二卖 | 一卖后反弹不破前高的卖点 |
| `SELL_3` | 三卖 | 跌破中枢后反弹不进入中枢的卖点 |

### 信号状态机

```
candidate ──→ confirmed ──→ superseded
                  │
                  └──→ invalidated
```

### 共振机制

- **G-2 区间套** — 高级别走势类型与低级别信号同向且价格区间重叠
- **G-1 跨层共振** — 多个级别同时出现同向信号
- **A3 方向过滤** — 信号方向与主要级别走势方向对齐

## 技术栈

- **Go 1.26** — 主开发语言
- **gin** — HTTP 框架（REST API）
- **go-redis/v9** — Redis Stream 驱动
- **cobra/viper** — CLI 与配置管理
- **Prometheus** — 可观测性指标
- **shopspring/decimal** — 高精度十进制运算

## 项目结构

```
├── cmd/server/          # 入口点
├── internal/
│   ├── app/             # 系统组装与生命周期
│   ├── cli/             # cobra 命令行定义
│   ├── config/          # 运行时配置
│   ├── eventbus/        # 通用事件总线
│   ├── chanlun/          # 缠论核心算法
│   │   ├── pipeline.go   # K 线处理管道
│   │   ├── contain.go    # K 线包含处理
│   │   ├── fractal.go    # 分型识别
│   │   ├── stroke.go     # 笔识别
│   │   ├── segment.go    # 线段划分
│   │   ├── pivotzone.go  # 中枢识别
│   │   └── divergence.go # 背驰判定
│   ├── ingest/          # M1 输入网关
│   ├── structure/       # M3 结构树
│   ├── levels/          # M4 递归级别
│   ├── signal/          # M5 信号引擎
│   ├── resonance/       # M6 共振引擎
│   ├── state/           # M7 状态存储
│   ├── gateway/         # M8 输出网关
│   ├── observability/   # M10 可观测性
│   ├── snapshot/        # M0 快照层
│   ├── types/           # 共享类型定义
│   └── log/             # 日志工具
├── docs/                # 算法文档
└── data/                # 运行数据
```

## 相关文档

- [系统使用说明](系统使用说明.md)
- `docs/` — 缠论算法详细文档
