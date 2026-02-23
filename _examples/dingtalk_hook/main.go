package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hrygo/hotplex/hooks"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	webhookURL := os.Getenv("HOTPLEX_DINGTALK_WEBHOOK_URL")
	secret := os.Getenv("HOTPLEX_DINGTALK_SECRET")
	if webhookURL == "" {
		fmt.Println("❌ 请设置 HOTPLEX_DINGTALK_WEBHOOK_URL 环境变量")
		fmt.Println("   参考 .env.example")
		os.Exit(1)
	}

	mgr := hooks.NewManager(logger, 1000)
	defer mgr.Close()

	dingtalk := hooks.NewDingTalkHook("dingtalk-alert", hooks.DingTalkConfig{
		WebhookURL: webhookURL,
		Secret:     secret,
		Timeout:    5 * time.Second,
		FilterEvents: []hooks.EventType{
			hooks.EventDangerBlocked,
			hooks.EventSessionError,
		},
	}, logger)

	mgr.Register(dingtalk, hooks.HookConfig{
		Enabled: true,
		Async:   false,
		Retry:   3,
	})

	loggingHook := hooks.NewLoggingHook("audit-log", logger, nil)
	mgr.Register(loggingHook, hooks.HookConfig{
		Enabled: true,
		Async:   true,
	})

	fmt.Println("📱 DingTalk Hook 已启动!")
	fmt.Println("发送测试事件...")

	fmt.Println("\n🚨 测试 1: 危险命令拦截通知")
	mgr.Emit(&hooks.Event{
		Type:      hooks.EventDangerBlocked,
		SessionID: "test-session-001",
		Namespace: "production",
		Error:     "检测到危险命令: rm -rf /",
	})

	fmt.Println("⚠️ 测试 2: 会话错误通知")
	mgr.Emit(&hooks.Event{
		Type:      hooks.EventSessionError,
		SessionID: "test-session-002",
		Namespace: "production",
		Error:     "进程崩溃: exit code 137",
	})

	fmt.Println("\n⏳ 等待消息发送...")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-time.After(3 * time.Second):
		fmt.Println("✅ 消息已发送!")
	case <-sigCh:
		fmt.Println("\n👋 收到退出信号")
	}
}
