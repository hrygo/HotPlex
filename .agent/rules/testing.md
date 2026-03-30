---
paths:
  - "**/*_test.go"
---

# 测试规范

## Test Table 模式
重复测试逻辑用 test table：
```go
tests := []struct {
    name  string
    input string
    want  string
}{
    {"idle timeout", "30m", "TERMINATED"},
    {"max lifetime", "24h", "TERMINATED"},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got := process(tt.input)
        require.Equal(t, tt.want, got)
    })
}
```

## 断言库
- 使用 `testify/require` 而非 `t.Fatal`（更细粒度）
- 使用 `testify/mock` 管理 mock

## 覆盖率
```bash
go test -coverprofile=c.out ./...
go tool cover -html=c.out
```

## Race 检测
所有测试通过 `go test -race ./...`，无竞争条件。
