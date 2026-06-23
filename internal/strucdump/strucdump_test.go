package strucdump_test

import (
	"os"
	"path/filepath"
	"testing"

	"trade/internal/chanlun"
	"trade/internal/strucdump"
	"trade/internal/types"

	"github.com/shopspring/decimal"
)

// TestWriter_Write 使用真实 Pipeline 产生输出，验证 strucdump 写入功能。
func TestWriter_Write(t *testing.T) {
	dir := t.TempDir()
	w := strucdump.NewWriter(dir)

	// 用 Pipeline 产生真实输出
	output := produceOutput(t, "TEST")
	if err := w.Write(output); err != nil {
		t.Fatalf("Write() 失败: %v", err)
	}

	// 验证 latest.json 存在
	latestPath := filepath.Join(dir, "TEST", "latest.json")
	if _, err := os.Stat(latestPath); os.IsNotExist(err) {
		t.Fatalf("latest.json 未创建: %s", latestPath)
	}

	// 验证至少有一个带时间戳的文件
	entries, err := os.ReadDir(filepath.Join(dir, "TEST"))
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("期望至少 2 个文件（latest.json + 时间戳文件），实际 %d", len(entries))
	}

	// 验证写入的内容是合法 JSON 且包含关键字段
	data, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("读取 latest.json 失败: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("latest.json 内容为空")
	}
	s := string(data)
	for _, key := range []string{"symbol", "elements", "fractals", "strokes", "segments", "pivotZones", "trendPatterns", "divergences"} {
		if !contains(s, `"`+key+`"`) {
			t.Errorf("JSON 缺少字段: %s", key)
		}
	}
}

// TestWriter_WriteAll 验证多 symbol 写入。
func TestWriter_WriteAll(t *testing.T) {
	dir := t.TempDir()
	w := strucdump.NewWriter(dir)

	outputs := []*chanlun.PipelineOutput{
		produceOutput(t, "BTCUSDT"),
		produceOutput(t, "ETHUSDT"),
	}

	if err := w.WriteAll(outputs); err != nil {
		t.Fatalf("WriteAll() 失败: %v", err)
	}

	for _, sym := range []string{"BTCUSDT", "ETHUSDT"} {
		path := filepath.Join(dir, sym, "latest.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("symbol %s 的 latest.json 未创建", sym)
		}
	}
}

// TestWriter_WriteAllNoChange 验证空输出时不会报错。
func TestWriter_WriteAllNoChange(t *testing.T) {
	dir := t.TempDir()
	w := strucdump.NewWriter(dir)

	// 空列表不应报错
	if err := w.WriteAll(nil); err != nil {
		t.Fatalf("WriteAll(nil) 不应失败: %v", err)
	}
}

// TestBaseDir 验证 BaseDir 返回正确的目录路径。
func TestBaseDir(t *testing.T) {
	dir := "/tmp/test-debug-output"
	w := strucdump.NewWriter(dir)

	if got := w.BaseDir(); got != dir {
		t.Errorf("BaseDir() 期望 %s, 实际 %s", dir, got)
	}
}

// TestWriteAll_EmptyList 验证空的 output 列表不报错。
func TestWriteAll_EmptyList(t *testing.T) {
	dir := t.TempDir()
	w := strucdump.NewWriter(dir)

	if err := w.WriteAll([]*chanlun.PipelineOutput{}); err != nil {
		t.Fatalf("空列表 WriteAll 不应失败: %v", err)
	}
}

// produceOutput 用真实 Pipeline 处理一批 K 线后提取当前状态。
func produceOutput(t *testing.T, symbol string) *chanlun.PipelineOutput {
	t.Helper()

	p := chanlun.NewPipeline()

	// 送 4 根 K 线，足够产生分型
	baseTS := int64(1700000000000)
	klines := []*types.Kline{
		mkline(symbol, baseTS, 100, 90),
		mkline(symbol, baseTS+60000, 95, 80),
		mkline(symbol, baseTS+120000, 85, 75),
		mkline(symbol, baseTS+180000, 90, 80),
	}

	var output *chanlun.PipelineOutput
	for _, k := range klines {
		output = p.Process(k)
	}
	return output
}

func mkline(symbol string, ts int64, h, l float64) *types.Kline {
	return &types.Kline{
		Symbol:     symbol,
		Open:       decimal.NewFromFloat(l + (h-l)*0.3),
		High:       decimal.NewFromFloat(h),
		Low:        decimal.NewFromFloat(l),
		Close:      decimal.NewFromFloat(l + (h-l)*0.7),
		BaseVolume: decimal.NewFromFloat(100),
		OpenTime:   ts,
		CloseTime:  ts + 59999,
		IsClosed:   true,
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
