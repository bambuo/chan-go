package cli

import (
	"fmt"
	"os"
	"strings"

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

// helpCmd 替换默认的 help 命令，提供中文描述。
var helpCmd = &cobra.Command{
	Use:                   "help [command]",
	Short:                 "查看命令帮助信息",
	Long:                  "查看指定命令的详细帮助信息。",
	DisableFlagsInUseLine: true,
	Run: func(c *cobra.Command, args []string) {
		cmd, _, e := c.Root().Find(args)
		if cmd == nil || e != nil {
			c.Printf("未知帮助主题: %q\n", strings.Join(args, " "))
			_ = c.Root().Usage()
		} else {
			_ = cmd.Help()
		}
	},
}

const usageTemplate = `用法：{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

别名：
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例：
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

可用命令：{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

标志：
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局标志：
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

更多帮助主题：{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

使用 "{{.CommandPath}} [command] --help" 查看命令更多信息。{{end}}
`

const helpTemplate = `{{with or .Long .Short}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

func init() {
	// 覆盖默认 -h 标志描述："help for trade" → "显示帮助信息"
	rootCmd.Flags().BoolP("help", "h", false, "显示帮助信息")

	rootCmd.AddCommand(serverCmd)
	rootCmd.SetHelpCommand(helpCmd)
	rootCmd.SetUsageTemplate(usageTemplate)
	rootCmd.SetHelpTemplate(helpTemplate)
}

// Execute 启动 CLI，由 main 函数调用。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
