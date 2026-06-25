package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd 是 CLI 的根命令，无实际功能，仅作为子命令的容器。
var rootCmd = &cobra.Command{
	Use:   "trade",
	Short: "缠中说禅信号分析系统",
	Long: `基于缠中说禅理论的实时多级别信号分析引擎。
消费 Redis Stream 中的 K 线数据，计算多级别缠论结构并产出结构化买卖点信号。`,
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}

// Execute 启动 CLI，由 main 函数调用。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
