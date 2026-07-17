# OpenCode Server Windows 故障排查 Runbook

> 适用场景：Windows 部署下 OpenCode Server（OCS）singleton 进程异常退出，尤其是原生状态码 `0xC0000142`（STATUS_DLL_INIT_FAILED）。
> 关联 issue：[#900](https://github.com/hrygo/hotplex/issues/900)

## 核心概念：原始退出码 vs 归一化退出码

OCS 采用 **singleton 进程模型**——一个 `opencode serve` 进程被所有 OCS session 共享。当该进程崩溃，所有引用它的 session 会同时进入 crash recovery。

HotPlex 在 Bridge 层对 OCS 崩溃做了一次**归一化**：`Worker.Wait()` 统一返回 `exit_code=1`（崩溃未恢复）或 `0`（已恢复）。这个 `1` 是 HotPlex 的内部状态码，**不等于** `opencode` 子进程的原生退出码。

因此日志里同时存在两套语义：

| 字段 | 含义 | 示例 |
|------|------|------|
| `exit_code`（Bridge 日志/metric） | 归一化 Worker 退出码 | `1` |
| `raw_exit_code` / `raw_exit_code_hex` | OCS 子进程**原生**退出码（仅 OCS worker 附加） | `3221225794` / `0xC0000142` |
| `exit_code_hex`（singleton 日志） | 单例进程原生退出码十六进制 | `0xC0000142` |

**排查要点**：看到 `raw_exit_code_hex=0xC0000142` 才是 Windows DLL 初始化失败的证据；`exit_code=1` 本身不能定责。

> v1.37.0 起，OCS singleton 通过 `worker.RawExitCoder` 接口把原始码透传到 Bridge 的日志、`worker_crashes` 指标和用户错误消息。早期版本只有 singleton 自身日志记录原始码。

## Gateway 日志检索

围绕故障时间戳和 session_id 检索以下消息（前后保留 ≥20 行）：

```
opencode-server-singleton: startup phase        # phase: spawn | discover_port | health_check | running
opencode-server-singleton: startup phase failed # 失败阶段 + resolved_executable + pid
opencode-server-singleton: stderr               # OCS 子进程 stderr 逐行
opencode-server-singleton: process crashed      # exit_code + exit_code_hex + refs
opencode-server-singleton: process exited       # 正常退出
bridge: worker exited with non-zero code        # exit_code + raw_exit_code + raw_exit_code_hex
```

`startup phase` 日志会带上 `resolved_executable`（`exec.LookPath` 解析后的实际路径）、`command`（非敏感命令摘要）、`pid` 和分配的 `port`——这些是区分 `.exe` 启动失败与 `.cmd` wrapper 失败的关键。

OCS 子进程没有独立的日志文件。其 stderr 经 `readStderr` 逐行写入 Gateway 日志（消息前缀 `opencode-server-singleton: stderr`）。启用 `log.file` 时，在 Gateway 日志文件按时间线检索即可。

## Windows 原生现场采集

`0xC0000142` 只表示"某个 DLL 的 DllMain 返回了 FALSE"，**不指名是哪个 DLL**。要定位具体 DLL，必须在故障机器、且在 HotPlex 服务运行的**同一 Windows 账户**下采集以下证据。

### 1. Event Viewer 的 faulting module（决定性）

`事件查看器 → Windows 日志 → 应用程序`，按故障时间点过滤：

- **Application Error**（事件 ID 1000）：记录 `Faulting application name`、`Faulting module name` 和 `Exception code`。
- **Windows Error Reporting**（事件 ID 1001）：附加的模块信息。

`Faulting module name` 会**直接点名**是哪个 DLL 初始化失败——这一条出来，根因基本锁定。

### 2. 服务账户下手动复现

在 HotPlex 服务的**同一登录账户**下运行（若是 LocalSystem 或专用服务账户，用 `psexec -s -i` 模拟 SYSTEM，或在该账户上下文的计划任务里跑）：

```powershell
opencode serve --port 19245   # 换一个空闲端口
echo $LASTEXITCODE            # -1073741502 即 0xC0000142
```

完整捕获 stdout / stderr。若 `$LASTEXITCODE` 仍是 `-1073741502`，根因就现形了。

### 3. ProcMon 抓 DLL 加载链

[Process Monitor](https://learn.microsoft.com/en-us/sysinternals/downloads/procmon) 过滤 `Process Name is opencode.exe`（必要时加 `cmd.exe`），关注：

- `CreateFile` / `Load Image` 结果为 `ACCESS DENIED`——EDR/应用控制拦截。
- `NAME NOT FOUND`——DLL 搜索路径缺失。

能看到失败停在哪个 DLL、它的搜索路径是什么。

### 4. EDR / 杀软日志

查检测、隔离、阻止记录。**不要直接关闭终端防护来验证**——用受控 allowlist 或隔离环境验证更安全，也更有说服力。

## 排查路径

```
看到 exit_code=1
   │
   ├─ 同时有 raw_exit_code_hex=0xC0000142？
   │     ├─ 是 → DLL 初始化失败，按"Windows 原生现场采集"取 faulting module
   │     └─ 否 → 检查 raw_exit_code 实际值，对照 Windows NT 状态码表
   │
   ├─ startup phase 失败？
   │     ├─ spawn → 命令解析失败（看 resolved_executable，检查 PATH / .cmd shim）
   │     ├─ discover_port → 进程起来了但没监听（看 stderr，可能 DLL 失败导致早期退出）
   │     └─ health_check → 进程监听了但 /health 不通（看 stderr 栈追踪）
   │
   └─ resolved_executable 是 .cmd / .bat？
         → wrapper 可能把子进程原生码映射为通用码；在服务账户直接跑 opencode.exe 取 $LASTEXITCODE
```

## 常见根因（需 faulting module / ProcMon 证据后才能确认）

- OpenCode 可执行文件或其依赖的 DLL 初始化失败（VC++ Redistributable 缺失/损坏、架构不匹配）。
- `.cmd` / npm / bun / node 等 wrapper 把子进程原生状态映射为通用退出码。
- EDR / 应用控制（AppLocker / WDAC）阻止 DLL 或可执行文件加载。
- 安装损坏、架构或运行库版本冲突。
- 服务账户与交互账户的 PATH、TEMP、用户配置或文件权限不同。

端口占用、普通配置错误通常**不足以**直接解释 `STATUS_DLL_INIT_FAILED`，除非日志显示这是另一次独立失败。

## 诊断命令

```bash
# 检查 OCS 命令解析、wrapper 识别、--version 探测（v1.37.0+）
hotplex doctor
```

`doctor` 的 `dependencies.opencode_server_resolve` 检查项会报告：

- 解析后的实际可执行文件路径；
- 是 native 二进制（`.exe` / 无扩展名）还是 wrapper（`.cmd` / `.bat` / `.ps1` / `.sh`）；
- `opencode --version` 是否在 5 秒内成功；
- 失败时给出平台相关的现场采集提示。
