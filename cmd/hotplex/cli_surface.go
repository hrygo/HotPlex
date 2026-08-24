package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const internalCLISurfaceFlag = "--internal-generate-cli-surface"

func runInternalCLISurface(args []string) (bool, error) {
	if len(args) == 0 || args[0] != internalCLISurfaceFlag {
		return false, nil
	}

	flags := flag.NewFlagSet("internal-generate-cli-surface", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputPath := flags.String("output", "-", "output path")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	rendered, err := renderPublicCLISurface(newRootCmd())
	if err != nil {
		return true, err
	}
	if *outputPath == "-" {
		_, err = os.Stdout.Write(rendered)
		return true, err
	}
	if err := os.WriteFile(*outputPath, rendered, 0o644); err != nil {
		return true, fmt.Errorf("write CLI surface %q: %w", *outputPath, err)
	}
	return true, nil
}

func renderPublicCLISurface(root *cobra.Command) ([]byte, error) {
	if root == nil {
		return nil, fmt.Errorf("nil root command")
	}

	commands := make([]*cobra.Command, 0)
	walkPublicCommands(root, &commands)
	sort.Slice(commands, func(i, j int) bool {
		return commandPath(commands[i]) < commandPath(commands[j])
	})

	var out strings.Builder
	out.WriteString("# HotPlex CLI 公共命令面\n\n")
	out.WriteString("由公开 Cobra 命令树生成。语法、默认值和可用性以已安装命令的帮助输出为最终准则；未映射的命令说明保留实际 CLI help 的原文。\n\n")
	for _, command := range commands {
		out.WriteString("## ")
		out.WriteString(commandPath(command))
		out.WriteByte('\n')
		if purpose := publicPurpose(command); purpose != "" {
			out.WriteString(localizeCLISurfacePurpose(purpose))
			out.WriteByte('\n')
		}
		if aliases := publicAliases(command); len(aliases) > 0 {
			out.WriteString("别名：")
			out.WriteString(strings.Join(aliases, " "))
			out.WriteByte('\n')
		}
		flags := publicFlags(command)
		if len(flags) == 0 {
			out.WriteString("\n")
			continue
		}
		out.WriteString("\n选项：")
		for i, flag := range flags {
			if i > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(flagShape(flag))
		}
		out.WriteString("\n\n")
	}
	return []byte(strings.TrimRight(out.String(), "\n") + "\n"), nil
}

var cliSurfacePurposeTranslations = map[string]string{
	"HotPlex Worker Gateway":                                     "HotPlex Worker 网关",
	"Audit log chain operations":                                 "审计日志链操作",
	"Re-anchor the audit hash chain at a surviving row (repair)": "在仍存的记录上重新锚定审计哈希链（修复）",
	"Verify the audit hash chain integrity (read-only)":          "验证审计哈希链完整性（只读）",
	"Manage configuration":                                       "管理配置",
	"Validate configuration file":                                "验证配置文件",
	"Cron job management":                                        "Cron 任务管理",
	"Create a cron job":                                          "创建 Cron 任务",
	"Delete a cron job":                                          "删除 Cron 任务",
	"Get cron job details":                                       "获取 Cron 任务详情",
	"Show execution history for a cron job":                      "显示 Cron 任务执行历史",
	"List cron jobs":                                             "列出 Cron 任务",
	"Trigger a cron job execution":                               "触发 Cron 任务执行",
	"Update a cron job":                                          "更新 Cron 任务",
	"Quick start in development mode":                            "快速启动开发模式",
	"Run diagnostic checks":                                      "运行诊断检查",
	"Manage the gateway server":                                  "管理 Gateway 服务",
	"Restart the gateway server":                                 "重启 Gateway 服务",
	"Start the gateway server":                                   "启动 Gateway 服务",
	"Stop the running gateway server":                            "停止运行中的 Gateway 服务",
	"Install hotplex binary to PATH":                             "将 hotplex 二进制安装到 PATH",
	"Interactive configuration wizard":                           "交互式配置向导",
	"Runtime operations: inspect and resolve fenced executions":  "运行时操作：检查并处理 fenced execution",
	"List and decide fenced executions (via Admin API)":          "列出并处理 fenced execution（通过 Admin API）",
	"Abandon a fenced execution: fail it with OPERATOR_ABANDONED and unblock the session": "放弃 fenced execution：以 OPERATOR_ABANDONED 标记失败并解除 session 阻塞",
	"List fenced executions blocking fresh input":                                         "列出阻塞新输入的 fenced execution",
	"Resolve a fence: clear it, keep runtime=unknown, and unblock the session":            "处理 fence：清除它、保留 runtime=unknown 并解除 session 阻塞",
	"Run security audit":                               "运行安全审计",
	"Manage system service":                            "管理系统服务",
	"Install as system service":                        "安装为系统服务",
	"View service logs":                                "查看服务日志",
	"Restart system service":                           "重启系统服务",
	"Start system service":                             "启动系统服务",
	"Check service status":                             "检查服务状态",
	"Stop system service":                              "停止系统服务",
	"Uninstall system service":                         "卸载系统服务",
	"Manage built-in agent skills":                     "管理内置 Agent Skill",
	"Remove managed built-in skill projections":        "删除受管理的内置 Skill projection",
	"Inspect built-in skill inventory and projections": "检查内置 Skill inventory 和 projection",
	"Synchronize built-in skills to worker roots":      "将内置 Skill 同步到 Worker 根目录",
	"Slack messaging operations":                       "Slack 消息操作",
	"Manage channel bookmarks":                         "管理频道书签",
	"Add a bookmark":                                   "添加书签",
	"List bookmarks":                                   "列出书签",
	"Remove a bookmark":                                "移除书签",
	"Delete a file from Slack":                         "从 Slack 删除文件",
	"Download a file from Slack":                       "从 Slack 下载文件",
	"List channels and DMs":                            "列出频道和私聊",
	"Add or remove emoji reactions":                    "添加或移除表情回复",
	"Add a reaction":                                   "添加表情回复",
	"Remove a reaction":                                "移除表情回复",
	"Schedule a message for future delivery":           "安排消息稍后投递",
	"Send a text message":                              "发送文本消息",
	"Update an existing message":                       "更新现有消息",
	"Upload a file to Slack":                           "上传文件到 Slack",
	"Check gateway status":                             "检查 Gateway 状态",
	"Update hotplex to the latest version":             "将 hotplex 更新到最新版本",
	"Print version information":                        "打印版本信息",
}

func localizeCLISurfacePurpose(purpose string) string {
	if translated, ok := cliSurfacePurposeTranslations[purpose]; ok {
		return translated
	}
	return purpose
}

func publicPurpose(command *cobra.Command) string {
	if command == nil {
		return ""
	}
	purpose := strings.TrimSpace(strings.ReplaceAll(command.Short, "\n", " "))
	if purpose == "" || strings.ContainsAny(purpose, "/\\") || strings.Contains(purpose, "~") {
		return ""
	}
	upper := strings.ToUpper(purpose)
	for _, fragment := range []string{"GATEWAY_", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(upper, fragment) {
			return ""
		}
	}
	return purpose
}

func publicAliases(command *cobra.Command) []string {
	if command == nil {
		return nil
	}
	aliases := make([]string, 0, len(command.Aliases))
	for _, alias := range command.Aliases {
		if alias == "" || strings.ContainsAny(alias, "/\\") || strings.Contains(alias, " ") {
			continue
		}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func flagShape(flag *pflag.Flag) string {
	if flag == nil {
		return ""
	}
	if flag.NoOptDefVal != "" {
		return "--" + flag.Name
	}
	valueType := strings.TrimSpace(flag.Value.Type())
	if valueType == "" {
		return "--" + flag.Name
	}
	return "--" + flag.Name + " <" + valueType + ">"
}

func walkPublicCommands(command *cobra.Command, commands *[]*cobra.Command) {
	if command == nil || command.Hidden {
		return
	}
	*commands = append(*commands, command)
	children := append([]*cobra.Command(nil), command.Commands()...)
	sort.Slice(children, func(i, j int) bool {
		return commandUse(children[i]) < commandUse(children[j])
	})
	for _, child := range children {
		walkPublicCommands(child, commands)
	}
}

func publicFlags(command *cobra.Command) []*pflag.Flag {
	byName := make(map[string]*pflag.Flag)
	collect := func(set *pflag.FlagSet) {
		if set == nil {
			return
		}
		set.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			byName[flag.Name] = flag
		})
	}
	collect(command.LocalNonPersistentFlags())
	collect(command.PersistentFlags())
	collect(command.InheritedFlags())

	flags := make([]*pflag.Flag, 0, len(byName))
	for _, flag := range byName {
		flags = append(flags, flag)
	}
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})
	return flags
}

func commandPath(command *cobra.Command) string {
	parts := make([]string, 0, 4)
	for current := command; current != nil; current = current.Parent() {
		parts = append(parts, commandUse(current))
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, " ")
}

func commandUse(command *cobra.Command) string {
	if command == nil {
		return ""
	}
	fields := strings.Fields(command.Use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
