//go:build ignore

// Test script for verifying all worker types and persistent session features.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hotplex/hotplex-go-client"
)

const (
	gatewayURL = "ws://localhost:8888/ws"
	signingKey = "J+PIsTFxFJVK5e4NdutNKY29FjmjT2maAlwpFh1vR5o="
	apiKey     = "test-api-key" // Required by server auth
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Generate token
	gen, err := client.NewTokenGenerator(signingKey)
	if err != nil {
		log.Fatalf("create token generator: %v", err)
	}
	token, err := gen.Generate("test-user", []string{"read", "write"}, 1*time.Hour)
	if err != nil {
		log.Fatalf("generate token: %v", err)
	}

	// Test each worker type
	workers := []string{"claude_code", "opencode_cli", "opencode_server"}

	for _, workerType := range workers {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("Testing worker type: %s\n", workerType)
		fmt.Printf("%s\n", strings.Repeat("=", 60))

		testWorker(ctx, workerType, token, "session-"+workerType)
		time.Sleep(1 * time.Second) // Brief pause between tests
	}
}

func testWorker(ctx context.Context, workerType, token, clientSessionID string) {
	// Create client
	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType(workerType),
		client.AuthToken(token),
		client.APIKey(apiKey),
		client.ClientSessionID(clientSessionID),
	)
	if err != nil {
		log.Printf("[%s] create client: %v", workerType, err)
		return
	}
	defer c.Close()

	// Connect
	fmt.Printf("[%s] Connecting with clientSessionID=%s...\n", workerType, clientSessionID)
	ack, err := c.Connect(ctx)
	if err != nil {
		log.Printf("[%s] connect failed: %v", workerType, err)
		return
	}
	fmt.Printf("[%s] Connected! SessionID=%s, State=%s, Worker=%s\n",
		workerType, ack.SessionID, ack.State, ack.ServerCaps.WorkerType)

	// Handle events in background
	eventCh := c.Events()
	doneCh := make(chan struct{})

	go func() {
		for {
			select {
			case <-doneCh:
				return
			case evt, ok := <-eventCh:
				if !ok {
					return
				}
				handleEvent(workerType, evt)
			}
		}
	}()

	// Send a simple input
	fmt.Printf("[%s] Sending input...\n", workerType)
	if err := c.SendInput(ctx, "Hello, respond with a brief greeting."); err != nil {
		log.Printf("[%s] send input: %v", workerType, err)
		close(doneCh)
		return
	}

	// Wait for done or timeout
	select {
	case <-ctx.Done():
	case <-time.After(60 * time.Second):
		fmt.Printf("[%s] Timeout waiting for response\n", workerType)
	}

	close(doneCh)
	time.Sleep(500 * time.Millisecond)

	// Test SendReset if session is still running
	state := c.State()
	fmt.Printf("[%s] Current state: %s\n", workerType, state)

	if state == client.StateRunning || state == client.StateIdle {
		fmt.Printf("[%s] Testing SendReset...\n", workerType)
		if err := c.SendReset(ctx, "test_reset"); err != nil {
			log.Printf("[%s] send reset: %v", workerType, err)
		} else {
			fmt.Printf("[%s] SendReset succeeded\n", workerType)
		}
		time.Sleep(3 * time.Second)
	}

	// Test SendGC
	state = c.State()
	if state == client.StateRunning || state == client.StateIdle || state == client.StateTerminated {
		fmt.Printf("[%s] Testing SendGC...\n", workerType)
		if err := c.SendGC(ctx, "test_gc"); err != nil {
			log.Printf("[%s] send gc: %v", workerType, err)
		} else {
			fmt.Printf("[%s] SendGC succeeded\n", workerType)
		}
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("[%s] Test completed\n", workerType)
}

func handleEvent(workerType string, evt client.Event) {
	switch evt.Type {
	case client.EventMessageDelta:
		if content := fieldStr(evt.Data, "content"); content != "" {
			fmt.Printf("[%s] delta: %s\n", workerType, truncate(content, 100))
		}
	case client.EventMessageStart:
		role := fieldStr(evt.Data, "role")
		fmt.Printf("[%s] message start: %s\n", workerType, role)
	case client.EventMessageEnd:
		fmt.Printf("[%s] message end\n", workerType)
	case client.EventState:
		state := fieldStr(evt.Data, "state")
		msg := fieldStr(evt.Data, "message")
		fmt.Printf("[%s] state: %s (message: %s)\n", workerType, state, msg)
	case client.EventDone:
		success := fieldBool(evt.Data, "success")
		fmt.Printf("[%s] done! success=%v\n", workerType, success)
	case client.EventError:
		code := fieldStr(evt.Data, "code")
		msg := fieldStr(evt.Data, "message")
		fmt.Printf("[%s] ERROR %s: %s\n", workerType, code, msg)
	case client.EventToolCall:
		name := fieldStr(evt.Data, "name")
		fmt.Printf("[%s] tool call: %s\n", workerType, name)
	case client.EventControl:
		action := fieldStr(evt.Data, "action")
		fmt.Printf("[%s] control: %s\n", workerType, action)
	}
}

func fieldStr(data any, key string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func fieldBool(data any, key string) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
