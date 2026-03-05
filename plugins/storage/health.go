package storage

import (
	"context"
	"time"
)

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	Status    string           `json:"status"` // healthy, degraded, unhealthy
	Latency   time.Duration    `json:"latency"`
	Timestamp time.Time        `json:"timestamp"`
	Checks    map[string]Check `json:"checks"`
}

// Check 单项检查结果
type Check struct {
	Status  string        `json:"status"` // pass, fail, warn
	Message string        `json:"message"`
	Latency time.Duration `json:"latency"`
}

// HealthChecker 健康检查接口
type HealthChecker interface {
	HealthCheck(ctx context.Context) (*HealthCheckResult, error)
}

// DefaultHealthCheck 执行默认健康检查
func DefaultHealthCheck(store ChatAppMessageStore) *HealthCheckResult {
	result := &HealthCheckResult{
		Status:    "healthy",
		Timestamp: time.Now(),
		Checks:    make(map[string]Check),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := store.List(ctx, &MessageQuery{Limit: 1})
	latency := time.Since(start)

	if err != nil {
		result.Checks["query"] = Check{
			Status:  "fail",
			Message: err.Error(),
			Latency: latency,
		}
		result.Status = "unhealthy"
	} else {
		result.Checks["query"] = Check{
			Status:  "pass",
			Message: "Query executed successfully",
			Latency: latency,
		}
	}

	result.Latency = latency
	return result
}

// StorageMetrics 存储指标
type StorageMetrics struct {
	TotalMessages    int64         `json:"total_messages"`
	TotalSessions    int64         `json:"total_sessions"`
	StorageSizeBytes int64         `json:"storage_size_bytes"`
	Uptime           time.Duration `json:"uptime"`
}

// GetMetrics 获取存储指标
func GetMetrics(store ChatAppMessageStore) (*StorageMetrics, error) {
	ctx := context.Background()

	// Count total messages
	totalMessages, err := store.Count(ctx, &MessageQuery{})
	if err != nil {
		return nil, err
	}

	// Note: Actual storage size would require database-specific queries
	// This is a simplified implementation
	return &StorageMetrics{
		TotalMessages:    totalMessages,
		TotalSessions:    0, // Would need ListUserSessions aggregation
		StorageSizeBytes: 0,
		Uptime:           0,
	}, nil
}
