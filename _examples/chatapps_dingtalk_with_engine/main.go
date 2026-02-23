package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/chatapps"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Get environment variables
	addr := os.Getenv("HOTPLEX_CHATAPPS_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	workDir := os.Getenv("HOTPLEX_WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/hotplex-chatapps"
	}

	// Ensure work directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		logger.Error("Failed to create work directory", "error", err, "dir", workDir)
		os.Exit(1)
	}

	// Initialize HotPlex Engine
	engineOpts := hotplex.EngineOptions{
		Timeout:     10 * time.Minute,
		IdleTimeout: 5 * time.Minute,
		Logger:      logger,
	}

	engine, err := hotplex.NewEngine(engineOpts)
	if err != nil {
		logger.Error("Failed to create engine", "error", err)
		os.Exit(1)
	}
	defer engine.Close()

	logger.Info("HotPlex Engine initialized")

	// Create DingTalk adapter
	adapter := chatapps.NewDingTalkAdapter(chatapps.DingTalkConfig{
		ServerAddr:    addr,
		MaxMessageLen: 5000, // DingTalk limit
	}, logger)

	// Set up message handler that connects to Engine
	adapter.SetHandler(func(ctx context.Context, msg *chatapps.ChatMessage) error {
		logger.Info("Received message", "user", msg.UserID, "content", msg.Content[:min(50, len(msg.Content))])

		// Create a unique session for this user
		userWorkDir := filepath.Join(workDir, msg.UserID)
		if err := os.MkdirAll(userWorkDir, 0755); err != nil {
			logger.Error("Failed to create user work dir", "error", err, "user", msg.UserID)
			return err
		}

		// Send "thinking" message first
		thinkingMsg := &chatapps.ChatMessage{
			Platform:  "dingtalk",
			SessionID: msg.SessionID,
			Content:   "🤔 正在思考...",
			Metadata:  msg.Metadata,
		}
		if err := adapter.SendMessage(ctx, msg.SessionID, thinkingMsg); err != nil {
			logger.Error("Failed to send thinking message", "error", err)
		}

		// Execute with HotPlex Engine
		cfg := &hotplex.Config{
			WorkDir:          userWorkDir,
			SessionID:        msg.SessionID,
			TaskInstructions: "You are a helpful AI assistant. Respond in Chinese.",
		}

		var responseContent string
		err := engine.Execute(ctx, cfg, msg.Content, func(eventType string, data any) error {
			switch eventType {
			case "thinking":
				// Stream thinking progress
			case "tool_use":
				// Tool execution
			case "answer":
				responseContent += fmt.Sprintf("%v", data)
			case "error":
				return fmt.Errorf("%v", data)
			}
			return nil
		})

		if err != nil {
			logger.Error("Engine execution failed", "error", err)
			responseContent = fmt.Sprintf("❌ 处理失败: %v", err)
		}

		// Send response
		responseMsg := &chatapps.ChatMessage{
			Platform:  "dingtalk",
			SessionID: msg.SessionID,
			Content:   responseContent,
			RichContent: &chatapps.RichContent{
				ParseMode: chatapps.ParseModeMarkdown,
			},
			Metadata: msg.Metadata,
		}

		if err := adapter.SendMessage(ctx, msg.SessionID, responseMsg); err != nil {
			logger.Error("Failed to send response", "error", err)
		} else {
			logger.Info("Response sent", "user", msg.UserID)
		}

		return nil
	})

	// Start adapter
	if err := adapter.Start(context.Background()); err != nil {
		logger.Error("Failed to start adapter", "error", err)
		os.Exit(1)
	}

	fmt.Println("🎉 ChatApps + HotPlex Engine 已启动!")
	fmt.Printf("   监听地址: http://localhost%s\n", addr)
	fmt.Println("   回调端点: /webhook")
	fmt.Println("   健康检查: /health")
	fmt.Printf("   工作目录: %s\n", workDir)
	fmt.Println("\n按 Ctrl+C 退出")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n👋 正在关闭...")
	adapter.Stop()
}

// min returns the smaller of x or y
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
