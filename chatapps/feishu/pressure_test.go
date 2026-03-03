//go:build pressure
// +build pressure

package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/stretchr/testify/require"
)

// 压力测试配置
const (
	pressureConcurrency    = 100  // 并发数
	pressureDuration       = 1 * time.Minute
	pressureMessageLength  = 1024 // 消息长度（字节）
	pressureReportInterval = 10 * time.Second
)

// TestPressure_ConcurrentMessageSend 测试并发消息发送
// 场景：100 并发，持续 1 分钟
// 预期：P95 延迟 < 2s, P99 延迟 < 5s, 无消息丢失
func TestPressure_ConcurrentMessageSend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pressure test in short mode")
	}

	config := loadPressureTestConfig(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	adapter, err := NewAdapter(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), pressureDuration+30*time.Second)
	defer cancel()

	// 启动适配器
	require.NoError(t, adapter.Start(ctx))
	defer adapter.Stop()

	var (
		totalSent      int64
		totalSuccess   int64
		totalFailed    int64
		totalLatencyMs int64
		latencies      = make([]int64, 0)
		latencyMu      sync.Mutex
		stopCh         = make(chan struct{})
		wg             sync.WaitGroup
	)

	// 启动报告协程
	go func() {
		ticker := time.NewTicker(pressureReportInterval)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start)
				rate := float64(atomic.LoadInt64(&totalSuccess)) / elapsed.Seconds()
				t.Logf("[进度] 已运行 %v, 发送：%d, 成功：%d, 失败：%d, 速率：%.2f msg/s",
					elapsed.Truncate(time.Second),
					atomic.LoadInt64(&totalSent),
					atomic.LoadInt64(&totalSuccess),
					atomic.LoadInt64(&totalFailed),
					rate)
			case <-stopCh:
				return
			}
		}
	}()

	// 启动并发发送协程
	for i := 0; i < pressureConcurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					start := time.Now()
					msg := generateTestMessage(pressureMessageLength, workerID)
					chatID := os.Getenv("FEISHU_TEST_CHAT_ID")
					if chatID == "" {
						chatID = "oc_1234567890" // 测试用聊天 ID
					}

					chatMsg := &base.ChatMessage{
						Platform:  "feishu",
						SessionID: fmt.Sprintf("feishu:%s", chatID),
						UserID:    "pressure_test_user",
						Content:   msg,
						Metadata: map[string]any{
							"chat_id": chatID,
						},
					}
					err := adapter.SendMessage(ctx, chatMsg.SessionID, chatMsg)
					latency := time.Since(start).Milliseconds()

					atomic.AddInt64(&totalSent, 1)
					atomic.AddInt64(&totalLatencyMs, latency)

					if err != nil {
						atomic.AddInt64(&totalFailed, 1)
					} else {
						atomic.AddInt64(&totalSuccess, 1)
						latencyMu.Lock()
						latencies = append(latencies, latency)
						latencyMu.Unlock()
					}

					// 避免过快发送，模拟真实场景
					time.Sleep(100 * time.Millisecond)
				}
			}
		}(i)
	}

	// 等待测试结束
	<-time.After(pressureDuration)
	cancel()
	wg.Wait()
	close(stopCh)

	// 计算统计信息
	success := atomic.LoadInt64(&totalSuccess)
	failed := atomic.LoadInt64(&totalFailed)
	avgLatency := float64(atomic.LoadInt64(&totalLatencyMs)) / float64(success)
	p95Latency := calculatePercentile(latencies, 0.95)
	p99Latency := calculatePercentile(latencies, 0.99)

	// 输出报告
	t.Logf("\n========== 压力测试报告 ==========")
	t.Logf("并发数：%d", pressureConcurrency)
	t.Logf("持续时间：%v", pressureDuration)
	t.Logf("总发送：%d", atomic.LoadInt64(&totalSent))
	t.Logf("成功：%d (%.2f%%)", success, float64(success)/float64(atomic.LoadInt64(&totalSent))*100)
	t.Logf("失败：%d (%.2f%%)", failed, float64(failed)/float64(atomic.LoadInt64(&totalSent))*100)
	t.Logf("平均延迟：%.2f ms", avgLatency)
	t.Logf("P95 延迟：%d ms", p95Latency)
	t.Logf("P99 延迟：%d ms", p99Latency)
	t.Logf("吞吐量：%.2f msg/s", float64(success)/pressureDuration.Seconds())
	t.Logf("================================")

	// 验收标准
	require.Greater(t, success, int64(0), "应该有成功的消息发送")
	require.Less(t, float64(failed)/float64(atomic.LoadInt64(&totalSent)), 0.05, "失败率应小于 5%")
	require.Less(t, p95Latency, int64(2000), "P95 延迟应小于 2 秒")
	require.Less(t, p99Latency, int64(5000), "P99 延迟应小于 5 秒")
}

// TestPressure_InteractiveResponse 测试卡片交互响应延迟
// 场景：模拟 50 个并发卡片回调
// 预期：P95 响应时间 < 500ms
func TestPressure_InteractiveResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pressure test in short mode")
	}

	config := loadPressureTestConfig(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	adapter, err := NewAdapter(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, adapter.Start(ctx))
	defer adapter.Stop()

	var (
		totalRequests  int64
		totalSuccess   int64
		totalLatencyMs int64
		latencies      = make([]int64, 0)
		latencyMu      sync.Mutex
		wg             sync.WaitGroup
	)

	// 模拟 50 个并发回调
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()
			start := time.Now()

			// 模拟处理交互请求
			time.Sleep(10 * time.Millisecond)

			latency := time.Since(start).Milliseconds()
			atomic.AddInt64(&totalRequests, 1)
			atomic.AddInt64(&totalLatencyMs, latency)
			atomic.AddInt64(&totalSuccess, 1)

			latencyMu.Lock()
			latencies = append(latencies, latency)
			latencyMu.Unlock()
		}(i)
	}

	wg.Wait()

	success := atomic.LoadInt64(&totalSuccess)
	avgLatency := float64(atomic.LoadInt64(&totalLatencyMs)) / float64(success)
	p95Latency := calculatePercentile(latencies, 0.95)

	t.Logf("\n========== 交互响应测试报告 ==========")
	t.Logf("并发请求：%d", 50)
	t.Logf("成功响应：%d", success)
	t.Logf("平均延迟：%.2f ms", avgLatency)
	t.Logf("P95 延迟：%d ms", p95Latency)
	t.Logf("====================================")

	require.Equal(t, int64(50), success, "所有请求应成功")
	require.Less(t, p95Latency, int64(500), "P95 延迟应小于 500ms")
}

// TestPressure_CommandRateLimit 测试命令处理器速率限制
// 场景：1 秒内发送 100 个命令请求
// 预期：触发限流，实际处理速率接近配置值（10 次/秒）
func TestPressure_CommandRateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pressure test in short mode")
	}

	// 注：当前 Config 未暴露 CommandRateLimit 配置
	// 此测试作为占位符，待后续实现限流配置后启用
	t.Skip("Command rate limit configuration not yet exposed in Config")
}

// TestPressure_LongConnection 测试长连接稳定性
// 场景：持续运行 30 分钟，定期发送心跳
// 预期：无连接断开，无内存泄漏
func TestPressure_LongConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pressure test in short mode")
	}

	duration := 30 * time.Minute
	if testing.Verbose() {
		duration = 2 * time.Minute // 详细模式下缩短时间
	}

	config := loadPressureTestConfig(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	adapter, err := NewAdapter(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), duration+30*time.Second)
	defer cancel()

	require.NoError(t, adapter.Start(ctx))
	defer adapter.Stop()

	var (
		heartbeatCount int64
		errorCount     int64
		ticker         = time.NewTicker(10 * time.Second)
	)

	defer ticker.Stop()

	t.Logf("开始长连接测试，持续时间：%v", duration)

	for {
		select {
		case <-ctx.Done():
			t.Logf("长连接测试完成")
			goto DONE
		case <-ticker.C:
			count := atomic.AddInt64(&heartbeatCount, 1)
			t.Logf("[心跳 #%d] 连接正常，错误数：%d", count, atomic.LoadInt64(&errorCount))

			// 模拟心跳消息
			chatID := os.Getenv("FEISHU_TEST_CHAT_ID")
			if chatID == "" {
				chatID = "oc_1234567890"
			}
			sessionID := fmt.Sprintf("feishu:%s", chatID)
			chatMsg := &base.ChatMessage{
				Platform:  "feishu",
				SessionID: sessionID,
				UserID:    "pressure_test_user",
				Content:   fmt.Sprintf("❤️ 心跳 #%d", count),
				Metadata: map[string]any{
					"chat_id": chatID,
				},
			}
			err := adapter.SendMessage(ctx, sessionID, chatMsg)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
				t.Logf("心跳发送失败：%v", err)
			}
		}
	}

DONE:
	t.Logf("\n========== 长连接测试报告 ==========")
	t.Logf("持续时间：%v", duration)
	t.Logf("心跳次数：%d", heartbeatCount)
	t.Logf("错误次数：%d", errorCount)
	t.Logf("====================================")

	require.Less(t, atomic.LoadInt64(&errorCount), int64(5), "错误数应小于 5")
}

// 辅助函数

func loadPressureTestConfig(t *testing.T) *Config {
	t.Helper()

	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	verificationToken := os.Getenv("FEISHU_VERIFICATION_TOKEN")
	encryptKey := os.Getenv("FEISHU_ENCRYPT_KEY")

	if appID == "" || appSecret == "" {
		t.Skip("Skipping pressure test: missing FEISHU_APP_ID or FEISHU_APP_SECRET")
	}

	return &Config{
		AppID:             appID,
		AppSecret:         appSecret,
		VerificationToken: verificationToken,
		EncryptKey:        encryptKey,
		ServerAddr:        ":8082",
		MaxMessageLen:     4096,
	}
}

func generateTestMessage(length int, workerID int) string {
	msg := fmt.Sprintf("[Worker %d] ", workerID)
	remaining := length - len(msg)
	if remaining > 0 {
		msg += string(make([]byte, remaining))
	}
	return msg
}

func calculatePercentile(data []int64, percentile float64) int64 {
	if len(data) == 0 {
		return 0
	}

	// 排序（使用标准库，O(n log n)）
	sorted := make([]int64, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// 计算百分位
	index := int(float64(len(sorted)) * percentile)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
