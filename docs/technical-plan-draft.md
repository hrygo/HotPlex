# HotPlex 技术方案

<div align="center">

**版本**: v2.0 (待实施版)  
**日期**: 2026-02-23  
**状态**: ⏳ 待实施

</div>

---

## 📋 目录

- [一、背景与目标](#一背景与目标)
- [二、方案总览](#二方案总览)
- [三、详细方案](#三详细方案)
  - [3.1 Session 池优化](#31-session-池优化)
  - [3.2 WAF 重构 + 审计](#32-waf-重构--审计)
  - [3.3 DX 改进](#33-dx-改进)
  - [3.4 安全增强](#34-安全增强)
- [四、数据模型](#四数据模型)
- [五、兼容性策略](#五兼容性策略)
- [六、里程碑](#六里程碑)
- [七、风险与对策](#七风险与对策)
- [八、验收标准](#八验收标准)
- [九、质量门清单](#九质量门清单)
- [十、技术选型总结](#十技术选型总结)
- [十一、代码库一致性分析](#十一代码库一致性分析)
- [十二、实施优先级建议](#十二实施优先级建议)
- [十三、技术实现映射](#十三技术实现映射)
- [十四、已知风险与缓解](#十四已知风险与缓解)
- [附录 A：错误码完整列表](#附录-a错误码完整列表)
- [附录 B：变更日志](#附录-b变更日志)

---

## 一、背景与目标

### 1.1 当前状态

| 维度 | 状态 |
|------|------|
| **阶段** | PMF 阶段 |
| **核心能力** | Provider 抽象、WS/HTTP 协议、安全隔离、Session 池、Hook 系统 |
| **验证** | 1000+ 并发压测通过 |
| **短板** | Session 池锁竞争、WAF 静态规则、调试困难 |

### 1.2 Q1 目标

| 维度 | 目标 |
|------|------|
| **性能** | 支持 1000+ 并发无明显锁竞争 |
| **安全** | WAF 规则可动态管理，审计日志可追溯 |
| **DX** | 错误码秒定位，Debug CLI 实时排查 |

---

## 二、方案总览

### 2.1 任务拆分

| 类别 | 任务 | 工作量 | 优先级 |
|------|------|--------|--------|
| **A. Session 池** | A1 分片、A2 读写分离、A3 冷热分离 | 3-4 天 | P0 |
| **B. WAF + 审计** | B1 规则重构、B2 SQLite、B3 告警、B4 动态API | 4-5 天 | P0 |
| **C. DX 改进** | C1 错误码、C2 CLI、C3 错误页 | 4-5 天 | P1 |

**总计: 约 11-14 天**

### 2.2 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                         hotplexd                                 │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐   │
│  │   Engine   │  │  Detector   │  │   SessionPool       │   │
│  │             │  │  (WAF)      │  │   (8 Shards)        │   │
│  └─────────────┘  └─────────────┘  └─────────────────────┘   │
│         │                │                     │                │
│         └────────────────┼─────────────────────┘                │
│                          ↓                                      │
│                 ┌─────────────────┐                             │
│                 │   SQLite DB     │                             │
│                 │  - rules        │                             │
│                 │  - audit_log    │                             │
│                 └─────────────────┘                             │
└─────────────────────────────────────────────────────────────────┘
```

---

## 三、详细方案

### 3.1 A. Session 池优化

#### A1. 分片机制（8 Shard）

**目标**: 消除全局锁竞争

**优化点**:
- 分片数从 6 调整为 8（更均衡）
- 哈希算法从 fnv32a 改为 xxhash（分布更均匀）
- 新增分片数量可配置选项

**实现**:

```go
type SessionPool struct {
    shards     []*SessionShard    // 8 个分片，可配置
    shardCount int                // 默认: 8 或 runtime.NumCPU()
    opts       *EngineOptions
}

type SessionShard struct {
    sessions map[string]*Session
    mu       sync.RWMutex
}

// Key 算法: xxhash(sessionID) % shardCount
func (p *SessionPool) getShard(sessionID string) *SessionShard {
    h := xxhash3.Hash([]byte(sessionID))
    idx := int(h) % p.shardCount
    return p.shards[idx]
}
```

**EngineOptions 新增字段**:

```go
type EngineOptions struct {
    // ... 现有字段 ...
    
    // ShardCount 分片数量
    // 默认值: 8 或 runtime.NumCPU() * 2
    // 推荐值: 8-16
    ShardCount *int `json:"shard_count,omitempty"`
}
```

**兼容性**: 内部实现，无 API 变更 ✅

---

#### A2. 读写分离

**目标**: 读操作无锁，写操作仅锁目标 Shard

**实现**:

```go
// 读操作 - RLock 仅锁目标 Shard
func (p *SessionPool) GetSession(sessionID string) (*Session, bool) {
    shard := p.getShard(sessionID)
    shard.mu.RLock()
    defer shard.mu.RUnlock()
    sess, ok := shard.sessions[sessionID]
    return sess, ok
}

// 写操作 - Lock 仅锁目标 Shard
func (p *SessionPool) CreateSession(...) (*Session, error) {
    shard := p.getShard(sessionID)
    shard.mu.Lock()
    defer shard.mu.Unlock()
    // ...
}
```

**兼容性**: 内部实现，无 API 变更 ✅

---

#### A3. 冷热分离

**目标**: 差异化 Session 生命周期

**Config 新增字段**:

```go
type Config struct {
    // ... 现有字段 ...

    // SessionHotLevel 可选参数
    // 0: 普通 session（默认，30min 空闲销毁）
    // 1: 温 session（60min 空闲销毁）
    // 2: 热 session（24h 空闲销毁）
    SessionHotLevel *int `json:"session_hot_level,omitempty"`
}
```

**处理逻辑**:

```go
func (p *SessionPool) cleanupIdleSessions() {
    for _, shard := range p.shards {
        shard.mu.Lock()
        for id, sess := range shard.sessions {
            timeout := p.opts.IdleTimeout // 默认 30min
            if sess.HotLevel == 1 {
                timeout = 60 * time.Minute
            } else if sess.HotLevel == 2 {
                timeout = 24 * time.Hour
            }
            // ...
        }
        shard.mu.Unlock()
    }
}
```

**兼容性**: 
- 用户不设置 → 默认为 0，行为与现有一致 ✅
- 用户设置 1/2 → 差异化超时 ✅

---

### 3.2 B. WAF 重构 + 审计

#### B1. 规则从代码剥离

**数据库表结构**:

```sql
CREATE TABLE security_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern TEXT NOT NULL,              -- 正则表达式
    description TEXT NOT NULL,          -- 规则描述
    level INTEGER NOT NULL DEFAULT 2,   -- 0=critical,1=high,2=moderate
    category TEXT NOT NULL,             -- 类别 (file_delete, injection, etc.)
    enabled INTEGER NOT NULL DEFAULT 1, -- 0=禁用,1=启用
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,                    -- 创建者
    version INTEGER DEFAULT 1,          -- 版本号（用于乐观锁）
    UNIQUE(pattern)
);

CREATE INDEX idx_rules_enabled ON security_rules(enabled);
CREATE INDEX idx_rules_category ON security_rules(category);
CREATE INDEX idx_rules_level ON security_rules(level);
```

**初始化**: 启动时从代码加载 80+ 默认规则写入 SQLite

---

#### B2. SQLite 配置与 Detector 重构

**SQLite 配置结构**:

```go
type SQLiteConfig struct {
    Path            string        // 数据库文件路径 (默认: ~/.hotplex/hotplex.db)
    MaxOpenConns   int           // 最大打开连接数 (默认: 10)
    MaxIdleConns   int           // 最大空闲连接数 (默认: 5)
    BusyTimeout    time.Duration // 忙等待超时 (默认: 5s)
    JournalMode    string        // 日志模式 (默认: WAL)
    CacheSize      int           // 缓存大小 (默认: -64000 = 64MB)
    RetentionDays  int           // 审计日志保留天数 (默认: 90)
}

type EngineOptions struct {
    // ... 现有字段 ...
    
    // SQLite 配置
    SQLite *SQLiteConfig `json:"sqlite,omitempty"`
}
```

**WAL 模式配置**:

```go
func initSQLite(cfg *SQLiteConfig) (*sql.DB, error) {
    db, err := sql.Open("sqlite3", cfg.Path)
    if err != nil {
        return nil, err
    }
    
    // 启用 WAL 模式（更好的并发性能）
    _, err = db.Exec("PRAGMA journal_mode=WAL")
    if err != nil {
        return nil, err
    }
    
    // 设置 busy timeout
    _, err = db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", cfg.BusyTimeout.Milliseconds()))
    
    // 连接池配置
    db.SetMaxOpenConns(cfg.MaxOpenConns)
    db.SetMaxIdleConns(cfg.MaxIdleConns)
    
    return db, nil
}
```

**Detector 重构**:

```go
type Detector struct {
    mu           sync.RWMutex
    rules        []SecurityRule    // 内存缓存
    ruleVersions map[int64]int64   // 规则版本缓存
    db           *sql.DB           // SQLite 连接
    logger       *slog.Logger
    auditLogger  *AuditLogger      // 审计日志
}

func (d *Detector) AddRule(pattern, desc string, level int, category string) error {
    // 验证正则安全性 (ReDoS 防护)
    if err := ValidateRegexPattern(pattern); err != nil {
        return fmt.Errorf("invalid regex pattern: %w", err)
    }
    
    // 写入 SQLite
    // 刷新内存缓存
}
```

---

#### B3. 审计日志

**数据库表结构**:

```sql
CREATE TABLE security_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    session_id TEXT,
    operation TEXT,              -- 触发的命令
    pattern_matched TEXT,        -- 匹配的正则
    level INTEGER,               -- 危险级别 (0=critical,1=high,2=moderate)
    category TEXT,
    blocked INTEGER DEFAULT 1,   -- 1=已拦截,0=仅记录
    source_ip TEXT,
    user_agent TEXT,
    trace_id TEXT,               -- OpenTelemetry Trace ID
    request_id TEXT             -- 请求 ID
);

CREATE INDEX idx_audit_session ON security_audit_log(session_id);
CREATE INDEX idx_audit_timestamp ON security_audit_log(timestamp);
CREATE INDEX idx_audit_level ON security_audit_log(level);
CREATE INDEX idx_audit_trace ON security_audit_log(trace_id);
```

**审计日志接口**:

```go
type AuditLogger interface {
    Log(ctx context.Context, event *AuditEvent) error
    Query(ctx context.Context, query AuditQuery) ([]AuditEvent, error)
    Export(ctx context.Context, format string, w io.Writer) error
    Cleanup(retentionDays int) error
}

type AuditEvent struct {
    Timestamp    time.Time `json:"timestamp"`
    TraceID      string    `json:"trace_id,omitempty"`
    RequestID    string    `json:"request_id,omitempty"`
    SessionID    string    `json:"session_id,omitempty"`
    Operation    string    `json:"operation"`
    PatternMatch string    `json:"pattern_matched,omitempty"`
    Level        int      `json:"level"`
    Category     string    `json:"category"`
    Blocked      bool     `json:"blocked"`
    SourceIP     string    `json:"source_ip,omitempty"`
    UserAgent    string    `json:"user_agent,omitempty"`
}
```

**审计日志保留策略**:
- 默认保留 90 天
- 可通过 `SQLiteConfig.RetentionDays` 配置
- 每天 UTC 0 点执行清理任务

---

#### B4. API 设计

**API 认证配置**:

```go
type APIAuthConfig struct {
    // 认证方式: "none", "api-key", "bearer"
    Method string `json:"method,omitempty"`
    
    // API Keys (当 method="api-key" 时)
    APIKeys []string `json:"api_keys,omitempty"`
    
    // Bearer Token (当 method="bearer" 时)
    BearerToken string `json:"bearer_token,omitempty"`
    
    // 速率限制 (请求/分钟)
    RateLimit int `json:"rate_limit,omitempty"`
    
    // 允许的 IP 白名单
    AllowedIPs []string `json:"allowed_ips,omitempty"`
}
```

**API 端点**:

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/security/rules` | 添加规则 |
| DELETE | `/api/v1/security/rules/{id}` | 删除规则 |
| GET | `/api/v1/security/rules` | 查询规则 |
| PATCH | `/api/v1/security/rules/{id}` | 启用/禁用规则 |
| GET | `/api/v1/audit/logs` | 查询审计日志 |
| GET | `/api/v1/audit/logs/export` | 导出审计日志 (CSV) |
| GET | `/health` | 健康检查 |

**兼容性**: 新增 `/api/v1/` 前缀，现有 API 不变 ✅

---

### 3.3 C. DX 改进

#### C1. 错误码体系

**错误码定义**:

```go
const (
    // WAF 相关 (HP_0xx)
    ErrCodeDangerBlocked     ErrorCode = "HP_001" // WAF 拦截
    ErrCodePatternInvalid    ErrorCode = "HP_002" // 规则正则无效
    ErrCodePatternReDoS      ErrorCode = "HP_003" // 正则 ReDoS 风险
    
    // 配置相关 (HP_1xx)
    ErrCodeInvalidConfig    ErrorCode = "HP_101" // 配置无效
    ErrCodeMissingWorkDir   ErrorCode = "HP_102" // 缺少 WorkDir
    ErrCodeInvalidSessionID ErrorCode = "HP_103" // SessionID 无效
    
    // Session 相关 (HP_2xx)
    ErrCodeSessionNotFound  ErrorCode = "HP_201" // Session 不存在
    ErrCodeSessionDead      ErrorCode = "HP_202" // Session 已终止
    ErrCodeSessionCreate    ErrorCode = "HP_203" // Session 创建失败
    ErrCodeSessionTimeout   ErrorCode = "HP_204" // Session 超时
    
    // 进程相关 (HP_3xx)
    ErrCodeProcessStart     ErrorCode = "HP_301" // 进程启动失败
    ErrCodeProcessExit      ErrorCode = "HP_302" // 进程异常退出
    ErrCodeProcessKilled    ErrorCode = "HP_303" // 进程被终止
    
    // Provider 相关 (HP_4xx)
    ErrCodeProviderNotFound ErrorCode = "HP_401" // Provider 不存在
    ErrCodeProviderInit     ErrorCode = "HP_402" // Provider 初始化失败
    ErrCodeProviderExec    ErrorCode = "HP_403" // Provider 执行失败
    
    // 数据库相关 (HP_5xx)
    ErrCodeDBConnect       ErrorCode = "HP_501" // 数据库连接失败
    ErrCodeDBQuery         ErrorCode = "HP_502" // 数据库查询失败
    ErrCodeDBAuth         ErrorCode = "HP_503" // 数据库认证失败
    
    // API 相关 (HP_6xx)
    ErrCodeAPIUnauthorized ErrorCode = "HP_601" // API 未授权
    ErrCodeAPIRateLimit   ErrorCode = "HP_602" // API 限流
    ErrCodeAPIInvalidReq  ErrorCode = "HP_603" // API 请求无效
    
    // 内部错误 (HP_9xx)
    ErrCodeInternal       ErrorCode = "HP_901" // 内部错误
    ErrCodeNotImplemented ErrorCode = "HP_902" // 功能未实现
)

// 错误响应格式
type ErrorResponse struct {
    Code      ErrorCode `json:"code"`
    Message   string   `json:"message"`
    Reason    string   `json:"reason,omitempty"`
    Solution  string   `json:"solution,omitempty"`
    DocLink   string   `json:"doc_link,omitempty"`
    TraceID   string   `json:"trace_id,omitempty"`
}
```

**示例响应**:

```json
{
    "error": {
        "code": "HP_001",
        "message": "danger event blocked",
        "reason": "input matched forbidden pattern: rm -rf /",
        "solution": "使用交互模式 rm -i 或移动文件到临时目录",
        "doc_link": "https://docs.hotplex.dev/errors/HP_001",
        "trace_id": "abc123"
    }
}
```

---

#### C2. Debug CLI (`hotplexctl`)

**CLI 框架选型**: Cobra + Viper

```bash
# Session 管理
$ hotplexctl session list
$ hotplexctl session info <session_id>
$ hotplexctl session logs <session_id> -f

# 审计日志
$ hotplexctl audit list --level critical

# 规则管理
$ hotplexctl rules list --category file_delete
$ hotplexctl rules add --pattern "rm -rf /" --level critical
$ hotplexctl rules delete <id>

# 健康检查
$ hotplexctl health

# 配置管理
$ hotplexctl config get
$ hotplexctl config set shard_count 16
```

---

#### C3. 错误页优化

```go
type HTTPErrorResponse struct {
    Error     ErrorResponse `json:"error"`
    RequestID string       `json:"request_id,omitempty"`
}
```

---

### 3.4 安全增强

#### 3.4.1 ReDoS 防护

```go
const (
    MaxRegexLength     = 500      // 最大正则长度
    MaxRegexGroups     = 10       // 最大捕获组数量
    MaxRegexAlternation = 5       // 最大选择数量
    CompileTimeout     = 100 * time.Millisecond
)

func ValidateRegexPattern(pattern string) error {
    // 长度检查
    if len(pattern) > MaxRegexLength {
        return fmt.Errorf("pattern too long (max %d chars)", MaxRegexLength)
    }
    
    // 编译超时测试
    done := make(chan error, 1)
    go func() {
        _, err := regexp.Compile(pattern)
        done <- err
    }()
    
    select {
    case err := <-done:
        if err != nil {
            return fmt.Errorf("invalid regex: %w", err)
        }
    case <-time.After(CompileTimeout):
        return fmt.Errorf("regex compilation timeout (possible ReDoS)")
    }
    
    // 复杂度检查
    groups := strings.Count(pattern, "(")
    if groups > MaxRegexGroups {
        return fmt.Errorf("too many capture groups (max %d)", MaxRegexGroups)
    }
    
    alternation := strings.Count(pattern, "|")
    if alternation > MaxRegexAlternation {
        return fmt.Errorf("too many alternations (max %d)", MaxRegexAlternation)
    }
    
    return nil
}
```

---

#### 3.4.2 规则仓库接口

```go
type RuleRepository interface {
    List(ctx context.Context, filter RuleFilter) ([]SecurityRule, error)
    Get(ctx context.Context, id int64) (*SecurityRule, error)
    Create(ctx context.Context, rule *SecurityRule) (int64, error)
    Update(ctx context.Context, id int64, rule *SecurityRule) error
    Delete(ctx context.Context, id int64) error
    BatchUpdateEnabled(ctx context.Context, ids []int64, enabled bool) error
}

type RuleFilter struct {
    Enabled   *bool
    Level     *int
    Category  *string
    Keyword   *string
    Limit     int
    Offset    int
}
```

---

## 四、数据模型

### 4.1 SQLite 表

```sql
-- 规则表
CREATE TABLE security_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern TEXT NOT NULL,
    description TEXT NOT NULL,
    level INTEGER NOT NULL DEFAULT 2,
    category TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT,
    version INTEGER DEFAULT 1,
    UNIQUE(pattern)
);

-- 审计日志表
CREATE TABLE security_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    session_id TEXT,
    operation TEXT,
    pattern_matched TEXT,
    level INTEGER,
    category TEXT,
    blocked INTEGER DEFAULT 1,
    source_ip TEXT,
    user_agent TEXT,
    trace_id TEXT,
    request_id TEXT
);
```

### 4.2 EngineOptions 完整配置

```go
type EngineOptions struct {
    // Session 池配置
    Timeout       time.Duration
    IdleTimeout   time.Duration
    MaxSessions  int
    
    // 分片配置
    ShardCount   *int `json:"shard_count,omitempty"`
    
    // 安全配置
    AllowedTools  []string
    PermissionMode string
    
    // SQLite 配置
    SQLite *SQLiteConfig `json:"sqlite,omitempty"`
    
    // API 认证配置
    APIAuth *APIAuthConfig `json:"api_auth,omitempty"`
}
```

---

## 五、兼容性策略

| 场景 | 处理方式 |
|------|----------|
| 现有 API | 完全不变 |
| 新增 API | `/api/v1/` 前缀 |
| Config 新字段 | 可选，不设置不影响现有功能 |
| SQLite | 新增，不影响现有数据 |
| 分片数调整 | 向后兼容，默认 8 |

---

## 六、里程碑

| 周次 | 任务 | 交付 |
|------|------|------|
| **Week 1** | A1 分片 + A2 读写分离 | Session 池支持 8 Shard |
| **Week 2** | A3 冷热分离 + B1 规则重构 | Config 支持 SessionHotLevel |
| **Week 3** | B2 SQLite + B3 审计 | 审计日志可查询 |
| **Week 4** | B4 动态API + C1 错误码 | 规则可动态管理 |
| **Week 5** | C2 CLI + C3 错误页 | hotplexctl 工具 |

**关键路径**: A1 → A2 → A3 → B1 → B2 → B3 → B4 → C1 → C2

---

## 七、风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| 分片后 Session 迁移 | 旧 session 无法访问 | 兼容期支持按 Key 查找所有 Shard |
| SQLite 并发写入 | 写入性能瓶颈 | WAL 模式 + 连接池 + 批量写入 |
| 规则正则 ReDoS | 规则恶意正则导致 CPU 100% | 编译超时 + 复杂度限制 + 验证器 |
| API 未授权访问 | 安全漏洞 | API Key/Bearer 认证 + IP 白名单 |
| 审计日志无限增长 | 磁盘耗尽 | 自动清理 + 保留策略配置 |
| 错误规则部署 | 服务异常 | 规则预览 + 乐观锁版本控制 |

---

## 八、验收标准

### 功能验收

- [ ] 1000+ 并发压测通过，无明显锁竞争
- [ ] WAF 规则可通过 API 动态增删
- [ ] 审计日志可查询，可导出
- [ ] 错误响应包含错误码和解决方案
- [ ] `hotplexctl` CLI 工具可用
- [ ] 现有 API 完全兼容

### 质量验收

- [ ] 单元测试覆盖 > 70%
- [ ] 分片迁移测试（向后兼容）
- [ ] ReDoS 规则编译超时测试
- [ ] SQLite WAL 模式性能测试
- [ ] API 认证测试
- [ ] 审计日志清理测试

### 性能基准

- [ ] 1000 并发 Session 操作延迟 P99 < 100ms
- [ ] SQLite 写入 QPS > 1000
- [ ] 分片后锁竞争减少 > 80%

---

## 九、质量门清单

### 设计阶段

- [ ] **API 认证设计** - 已指定并通过安全评审
- [ ] **数据库 Schema 最终版** - 迁移脚本已编写
- [ ] **分片哈希函数** - 已基准测试分布均匀性
- [ ] **审计日志保留策略** - 已文档化
- [ ] **错误码注册表** - 所有 50+ 错误码已定义
- [ ] **CLI 通信协议** - hotplexctl 如何与 hotplexd 通信
- [ ] **ReDoS 验证器** - 实施方案已确定并测试

### 实现阶段

- [ ] **单元测试** - 每个模块 > 70% 覆盖
- [ ] **集成测试** - API 端到端测试
- [ ] **性能测试** - 1000+ 并发基准测试
- [ ] **安全测试** - ReDoS 防护测试
- [ ] **文档更新** - API 文档 + SDK 文档

### 部署阶段

- [ ] **配置默认值** - 经过验证的默认配置
- [ ] **回滚策略** - 规则和 schema 变更的回滚
- [ ] **监控告警** - 关键指标已接入
- [ ] **发布说明** - 更新日志已编写

---

## 十、技术选型总结

| 组件 | 选型 | 原因 |
|------|------|------|
| 分片算法 | xxhash | 分布均匀，性能优秀 |
| 分片数量 | 8 (可配置) | 推荐 8-16 |
| SQLite 驱动 | modernc.org/sqlite | 纯 Go，无 CGO 依赖 |
| CLI 框架 | Cobra + Viper | 行业标准，功能完善 |
| 日志框架 | log/slog | Go 1.21+ 内置，OTel 兼容 |
| 文件监听 | fsnotify | 规则热加载 |

---

## 十一、代码库一致性分析

### 现有实现对照

| 方案模块 | 代码位置 | 现状 | 一致性 |
|----------|----------|------|--------|
| Session 池 | `internal/engine/pool.go` | 单一 RWMutex + 双重检查锁定 + pending map | ✅ 基础已具备 |
| 安全检测 | `internal/security/detector.go` | 80+ 正则规则，SecurityRule 接口 | ✅ 基本一致 |
| 错误定义 | `types/errors.go` | 仅 `ErrDangerBlocked` | ⚠️ 需扩展 |
| 配置结构 | `internal/engine/types.go` | EngineOptions 定义完整 | ⚠️ 需扩展 |

### 需新增模块

| 模块 | 文件位置(预估) | 工作量 | 优先级 |
|------|----------------|--------|--------|
| SQLite 配置 | `internal/db/sqlite.go` | 1 天 | P0 |
| 审计日志 | `internal/security/audit.go` | 1 天 | P0 |
| API 路由 | `internal/server/api.go` | 2 天 | P1 |
| 错误码定义 | `types/errors.go` | 1 天 | P1 |

---

## 十二、实施优先级建议

### 阶段划分

| 阶段 | 任务 | 周期 | 风险 | 理由 |
|------|------|------|------|------|
| **Phase 1** | A1 + A2 (分片) | 2 天 | 🟢 低 | 现有代码基础好，改动最小，收益明确 |
| **Phase 2** | A3 (冷热分离) | 1 天 | 🟢 低 | 依赖分片完成，配置扩展 |
| **Phase 3** | B1 + B2 (SQLite + 规则) | 3 天 | 🟡 中 | 需引入新依赖，设计数据库 Schema |
| **Phase 4** | B3 (审计日志) | 1 天 | 🟡 中 | 依赖 SQLite |
| **Phase 5** | B4 (API) | 2 天 | 🔴 高 | 全新路由层，安全敏感 |
| **Phase 6** | C1 + C2 + C3 (DX) | 3 天 | 🟢 低 | 错误码 + CLI，独立可并行 |

### 推荐实施顺序

```
Phase 1 (Week 1)  Phase 2 (Week 2)  Phase 3 (Week 3)  Phase 4 (Week 4)  Phase 5 (Week 5)
+----------------+ +---------------+ +----------------+ +---------------+ +----------------+
| A1 分片优化    |->| A3 冷热分离   |->| B1 规则接口    |->| B3 审计日志    |->| B4 动态API     |
| A2 读写分离    |  | B1 规则结构   |  | B2 SQLite      |  |                |  |                |
+----------------+ +---------------+ +----------------+ +---------------+ +----------------+
                                                                                |
                                                                                v
                                              Phase 6 (可并行)
                                              +----------------+
                                              | C1 错误码      |
                                              | C2 CLI         |
                                              | C3 错误页      |
                                              +----------------+
```

---

## 十三、技术实现映射

### 现有文件修改清单

| 文件 | 修改内容 | 类型 |
|------|----------|------|
| `internal/engine/pool.go` | 添加分片逻辑 | 重构 |
| `internal/engine/types.go` | 添加 ShardCount, SessionHotLevel | 扩展 |
| `internal/security/detector.go` | 添加 SQLite/审计支持 | 扩展 |
| `types/errors.go` | 添加 50+ 错误码 | 扩展 |
| `engine/runner.go` | 添加 SQLite/认证配置 | 扩展 |

### 新增文件清单

| 文件 | 职责 |
|------|------|
| `internal/db/sqlite.go` | SQLite 连接池、WAL 配置 |
| `internal/security/audit.go` | 审计日志接口实现 |
| `internal/security/rules/repo.go` | 规则仓库接口 |
| `internal/server/api.go` | REST API 路由 |
| `cmd/hotplexctl/main.go` | CLI 入口 |

---

## 十四、已知风险与缓解

### 高风险项

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| SQLite 依赖引入 | 运行时错误 | 使用纯 Go 驱动 (modernc.org/sqlite)，无 CGO 依赖 |
| API 认证设计缺陷 | 安全漏洞 | 参考 OAuth 2.0 最佳实践，代码评审 |
| 分片后数据迁移 | 兼容性问题 | 兼容期支持全分片查找，后续废弃 |

### 中风险项

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 错误码数量过多 | 维护困难 | 按类别分组 (HP_0xx ~ HP_9xx)，文档自动生成 |
| 审计日志磁盘占用 | 存储耗尽 | 自动清理任务 + 保留策略配置 |

---

## 附录 A：错误码完整列表

| 类别 | 错误码 | 描述 |
|------|--------|------|
| WAF | HP_001 | WAF 拦截 |
| WAF | HP_002 | 规则正则无效 |
| WAF | HP_003 | 正则 ReDoS 风险 |
| 配置 | HP_101 | 配置无效 |
| 配置 | HP_102 | 缺少 WorkDir |
| 配置 | HP_103 | SessionID 无效 |
| Session | HP_201 | Session 不存在 |
| Session | HP_202 | Session 已终止 |
| Session | HP_203 | Session 创建失败 |
| Session | HP_204 | Session 超时 |
| 进程 | HP_301 | 进程启动失败 |
| 进程 | HP_302 | 进程异常退出 |
| 进程 | HP_303 | 进程被终止 |
| Provider | HP_401 | Provider 不存在 |
| Provider | HP_402 | Provider 初始化失败 |
| Provider | HP_403 | Provider 执行失败 |
| 数据库 | HP_501 | 数据库连接失败 |
| 数据库 | HP_502 | 数据库查询失败 |
| 数据库 | HP_503 | 数据库认证失败 |
| API | HP_601 | API 未授权 |
| API | HP_602 | API 限流 |
| API | HP_603 | API 请求无效 |
| 内部 | HP_901 | 内部错误 |
| 内部 | HP_902 | 功能未实现 |

---

## 附录 B：变更日志

| 版本 | 日期 | 变更内容 |
|------|------|----------|
| v1.0 | 2026-02-23 | 初始版本 |
| v1.1 | 2026-02-23 | 完善版：补充 API 认证、SQLite 配置、ReDoS 防护 |
| v1.2 | 2026-02-23 | 代码库交叉分析版：增加一致性分析、实施优先级建议 |
| v2.0 | 2026-02-23 | 正式发布版：优化目录结构，添加附录 |

---

<div align="center">

**文档版本**: v2.0 (待实施版)  
**更新日期**: 2026-02-23  
**文档状态**: ⏳ 待实施

</div>
