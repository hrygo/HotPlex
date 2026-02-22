package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/hrygo/hotplex"
)

/*
This example demonstrates how to use the HotPlex Control Plane with the OpenCode provider.
OpenCode is an alternative AI CLI agent that supports multiple LLM providers and
different operational modes (Plan/Build).

HotPlex's Provider abstraction allows you to swap the underlying AI CLI agent
without changing your application logic.
*/

func main() {
	// Configure logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	fmt.Println("=== HotPlex OpenCode Provider Demo ===")

	// 1. Initialize OpenCode Provider
	// OpenCode-specific configuration can be provided via the OpenCode field.
	opencodePrv, err := hotplex.NewOpenCodeProvider(hotplex.ProviderConfig{
		Type:         hotplex.ProviderTypeOpenCode,
		DefaultModel: "zhipu/glm-5-code-plan", // Updated to GLM 5 Code Plan
		OpenCode: &hotplex.OpenCodeConfig{
			PlanMode:   true,  // Start in Planning mode
			UseHTTPAPI: false, // Use CLI mode (default)
		},
	}, logger)
	if err != nil {
		log.Fatalf("Failed to create OpenCode provider: %v", err)
	}

	// 2. Initialize HotPlex Core Engine with the OpenCode provider
	opts := hotplex.EngineOptions{
		Timeout:   5 * time.Minute,
		Logger:    logger,
		Namespace: "opencode_demo",
		Provider:  opencodePrv, // Using OpenCode instead of the default Claude Code
	}

	engine, err := hotplex.NewEngine(opts)
	if err != nil {
		log.Fatalf("Failed to initialize HotPlex: %v", err)
	}
	defer engine.Close()

	// Get current working directory for the workspace
	currentDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}

	// 3. Define Execution Configuration
	cfg := &hotplex.Config{
		WorkDir:          currentDir,
		SessionID:        "opencode-session-fresh-1", // Unique session ID
		TaskInstructions: "Be precise and explain your steps.",
	}

	// 4. Send Prompt
	prompt := "Create a simple Python script that calculates Fibonacci sequence up to 100."

	fmt.Printf("\n--- Sending Prompt to OpenCode ---\n")
	fmt.Printf("%s\n", prompt)
	fmt.Printf("----------------------------------\n\n")

	// 5. Execute the task
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err = engine.Execute(ctx, cfg, prompt, func(eventType string, data any) error {
		switch eventType {
		case "thinking":
			if evt, ok := data.(*hotplex.EventWithMeta); ok {
				fmt.Printf("🤔 Thinking: %s\n", evt.EventData)
			}
		case "tool_use":
			if evt, ok := data.(*hotplex.EventWithMeta); ok {
				fmt.Printf("🛠️ Tool: %s (ID: %s)\n", evt.EventData, evt.Meta.ToolID)
			}
		case "answer":
			if evt, ok := data.(*hotplex.EventWithMeta); ok {
				fmt.Print(evt.EventData)
			}
		case "session_stats":
			fmt.Println("\n\n📊 Task Completed!")
			if stats, ok := data.(*hotplex.SessionStatsData); ok {
				fmt.Printf("- Duration: %d ms\n", stats.TotalDurationMs)
				fmt.Printf("- Model: %s\n", stats.ModelUsed)
				fmt.Printf("- Tools used: %d\n", stats.ToolCallCount)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	// 6. Manual Stats Retrieval
	stats := engine.GetSessionStats(cfg.SessionID)
	if stats != nil {
		fmt.Printf("\n[Manual Stats Check] Total Duration: %dms, Tools: %v\n", stats.TotalDurationMs, stats.ToolsUsed)
	}

	fmt.Println("\n=== Demo Complete ===")
}
