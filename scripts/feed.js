const fs = require('fs');
const path = require('path');
const readline = require('readline');

// ====== 用法 ======
//   SYMBOL=BTCUSDT node scripts/feed.js
//   SYMBOL=ETHUSDT PLAY_SPEED=60 END_LINE=50000 node scripts/feed.js

const SYMBOL = process.env.SYMBOL || 'BNBUSDT';
const CSV_FILE = `${SYMBOL}_1m.csv`;
const CSV_PATH = process.env.CSV_PATH || path.join(__dirname, '..', 'docs', 'dataset', CSV_FILE);
const REDIS_URL = process.env.REDIS_URL || 'redis://localhost:6379';
const STREAM_KEY = `chan:klines:${SYMBOL}`;
const BATCH_SIZE = parseInt(process.env.BATCH_SIZE || '100', 10);
const PLAY_SPEED = parseFloat(process.env.PLAY_SPEED || '0');
const START_LINE = parseInt(process.env.START_LINE || '1', 10);
const END_LINE = parseInt(process.env.END_LINE || '0', 10);

let Redis;
try {
  Redis = require('ioredis');
} catch {
  console.error('❌ 需要 ioredis 包，请运行: npm install ioredis');
  process.exit(1);
}

const redis = new Redis(REDIS_URL, {
  maxRetriesPerRequest: null,
  retryStrategy(times) {
    if (times > 10) return null;
    return Math.min(times * 200, 3000);
  },
});

redis.on('error', (err) => console.error('Redis 错误:', err.message));
redis.on('connect', () => console.log('✅ 已连接 Redis:', REDIS_URL));

function parseTimestamp(str) {
  const cleaned = str.trim().replace(/"/g, '');
  const d = new Date(cleaned + (cleaned.includes(':') ? '' : ':00'));
  return d.getTime();
}

function parseLine(line, lineNum) {
  const parts = line.split(',');
  if (parts.length < 6) return null;
  const ts = parseTimestamp(parts[0]);
  if (isNaN(ts)) {
    console.warn(`⚠️ 第 ${lineNum} 行时间解析失败: ${parts[0]}`);
    return null;
  }
  return {
    symbol: SYMBOL,
    ts: String(ts),
    open: parts[1].trim(),
    high: parts[2].trim(),
    low: parts[3].trim(),
    close: parts[4].trim(),
    baseVolume: parts[5]?.trim() || '0',
  };
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function main() {
  if (!fs.existsSync(CSV_PATH)) {
    console.error(`❌ CSV 文件不存在: ${CSV_PATH}`);
    console.error(`   支持的币种: BNBUSDT, BTCUSDT, ETHUSDT, SOLUSDT, XRPUSDT`);
    console.error(`   用法: SYMBOL=SOLUSDT node scripts/feed.js`);
    process.exit(1);
  }

  const info = await redis.exists(STREAM_KEY);
  console.log(`📊 Symbol: ${SYMBOL}`);
  console.log(`📂 CSV: ${CSV_PATH}`);
  console.log(`📤 Stream: ${STREAM_KEY} ${info ? '(已存在)' : '(将新建)'}`);
  console.log(`⚙️  批次: ${BATCH_SIZE}, 倍速: ${PLAY_SPEED > 0 ? PLAY_SPEED+'x' : '最快'}`);
  console.log(`  行范围: ${START_LINE} → ${END_LINE || '文件尾'}`);
  console.log('---');

  let total = 0, pushed = 0, skipped = 0, batch = [];
  let firstTS = 0, lastTS = 0;

  const rl = readline.createInterface({
    input: fs.createReadStream(CSV_PATH, { highWaterMark: 64 * 1024 }),
    crlfDelay: Infinity,
  });

  const startTime = Date.now();

  for await (const line of rl) {
    total++;
    if (!line.trim()) continue;
    if (total < START_LINE) continue;
    if (END_LINE > 0 && total > END_LINE) break;

    const kline = parseLine(line, total);
    if (!kline) { skipped++; continue; }

    if (firstTS === 0) firstTS = parseInt(kline.ts);
    lastTS = parseInt(kline.ts);

    batch.push(kline);
    pushed++;

    if (batch.length >= BATCH_SIZE) {
      await flushBatch(batch);
      const pct = END_LINE > 0 ? ` (${((total / END_LINE) * 100).toFixed(1)}%)` : '';
      process.stdout.write(`\r📤 ${SYMBOL} 已推送: ${pushed} 条${pct}`);
      batch = [];
    }
  }

  if (batch.length > 0) {
    await flushBatch(batch);
    process.stdout.write(`\r📤 ${SYMBOL} 已推送: ${pushed} 条${END_LINE > 0 ? ` (100%)` : ''}`);
  }

  const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
  const days = ((lastTS - firstTS) / 86400000).toFixed(1);

  console.log('\n---');
  console.log(`✅ ${SYMBOL} 投喂完成`);
  console.log(`  总行数: ${total}`);
  console.log(`  推送: ${pushed}`);
  console.log(`  跳过: ${skipped}`);
  console.log(`  耗时: ${elapsed}s`);
  console.log(`  数据跨度: ${days} 天`);
  console.log(`  速率: ${(pushed / elapsed).toFixed(0)} 条/s`);

  await redis.quit();
}

async function flushBatch(batch) {
  const pipeline = redis.pipeline();
  for (const k of batch) {
    pipeline.xadd(STREAM_KEY, '*',
      'symbol', k.symbol,
      'ts', k.ts,
      'open', k.open,
      'high', k.high,
      'low', k.low,
      'close', k.close,
      'baseVolume', k.baseVolume,
    );
  }
  await pipeline.exec();

  if (PLAY_SPEED > 0 && batch.length >= 2) {
    const gap = (parseInt(batch[batch.length-1].ts) - parseInt(batch[0].ts)) / PLAY_SPEED;
    if (gap > 0) await sleep(gap);
  }
}

main().catch(err => {
  console.error('❌ 错误:', err);
  redis.quit().catch(() => {});
  process.exit(1);
});
