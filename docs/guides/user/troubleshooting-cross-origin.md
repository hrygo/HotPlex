---
title: 常见问题排查手册
weight: 5
description: 用服务器 IP 访问时页面卡住、创建会话报错、工作区创建失败？本手册手把手教你解决。
---

# 常见问题排查手册

> 本手册面向普通用户，用一步一步的操作指引帮你解决部署和使用中遇到的问题。
> 不需要编程基础，只需要会编辑配置文件和重启服务。

---

## 你将学到什么

读完本手册，你能解决以下 4 类问题：

| 问题 | 你会遇到的情况 |
|------|--------------|
| **问题一** | 用服务器 IP 访问 WebChat，登录后页面一直转圈，无法创建会话 |
| **问题二** | 创建会话时选择 opencode_server 报错，但选 claude_code 正常 |
| **问题三** | 把 localhost 改成服务器 IP 后，新建工作区报错 |
| **问题四** | 不清楚 API Key 还有没有用，不知道该怎么认证 |

---

## 开始之前：确认你的环境

在开始排查之前，请先回答以下问题，帮助你判断该看哪个章节：

**问题 1：你是怎么访问 HotPlex 的？**

- A. 在安装了 HotPlex 的那台电脑上，打开浏览器访问 `http://localhost:8888` → **你不太会遇到本手册的问题**，但如果遇到了，直接看问题一
- B. 在另一台电脑/手机/平板上，通过服务器 IP 访问，例如 `http://192.168.1.100:8888` → **看问题一和问题三**
- C. 通过域名访问，例如 `https://hotplex.example.com` → **看问题一**

**问题 2：你创建会话时选择的是什么 Worker 类型？**

- A. claude_code（默认） → 如果报错，看问题一
- B. opencode_server → 如果报错，看问题二
- C. 不确定 → 看问题四了解认证方式

---

## 问题一：用服务器 IP 访问时，页面一直转圈

### 你会看到什么

1. 你在浏览器地址栏输入 `http://192.168.1.100:8888`（你的服务器 IP）
2. 页面正常加载，你看到了登录界面
3. 输入用户名和密码，点击登录
4. 登录成功，但页面卡在"创建中"或一直转圈
5. 打开浏览器开发者工具（按 F12），在 Console（控制台）里看到红色错误：

```
POST /api/workspaces → 403
{"error":{"code":"FORBIDDEN","message":"cross-origin write blocked"}}
```

或者在 Admin 管理后台看到：

```
Unexpected token '<', "<!DOCTYPE "... is not valid JSON
```

### 为什么会这样

HotPlex 默认配置允许所有来源的访问（`allowed_origins: ["*"]`），这在 localhost 下工作正常。但当你用服务器 IP 访问时，浏览器出于安全考虑，会检查"这个请求是不是来自被允许的地址"。默认的通配符配置在写操作（比如创建会话、创建工作区）时会被拦截。

**简单来说**：你需要告诉 HotPlex"我允许从哪个地址访问"。

### 解决方法（3 选 1）

#### 方法 A：修改配置文件（推荐，最简单）

**第 1 步：找到配置文件**

打开终端（Terminal），输入以下命令查看配置文件位置：

```bash
echo ~/.hotplex/config.yaml
```

你会看到类似 `/Users/你的用户名/.hotplex/config.yaml` 的路径。记住这个路径。

**第 2 步：打开配置文件**

用任何文本编辑器打开这个文件：

```bash
# macOS
open -e ~/.hotplex/config.yaml

# Linux（使用 nano）
nano ~/.hotplex/config.yaml

# Windows（使用记事本）
notepad %USERPROFILE%\.hotplex\config.yaml
```

**第 3 步：找到 allowed_origins 配置**

在文件中搜索 `allowed_origins`，你会看到类似这样的内容：

```yaml
security:
  # ... 其他配置 ...
  allowed_origins:
    - "*"
```

**第 4 步：修改配置**

把 `- "*"` 改成你的实际访问地址。格式是：`协议://IP地址:端口号`。

**举例**：

如果你通过 `http://192.168.1.100:8888` 访问，改成：

```yaml
security:
  allowed_origins:
    - "http://192.168.1.100:8888"
    - "http://localhost:8888"
```

**注意**：
- 必须包含 `http://` 或 `https://`
- 必须包含端口号（默认是 8888）
- 不要有多余的斜杠（`http://192.168.1.100:8888/` 是错的）
- 建议同时保留 `localhost` 那一行，方便本机访问

**如果你同时从多个地址访问**（比如电脑和手机），把所有地址都列出来：

```yaml
security:
  allowed_origins:
    - "http://192.168.1.100:8888"    # 电脑访问
    - "http://192.168.1.101:8888"    # 手机访问
    - "http://localhost:8888"         # 本机访问
```

**第 5 步：保存文件**

- macOS/记事本：按 `Cmd + S` 或 `Ctrl + S`
- nano：按 `Ctrl + O`，然后按 `Enter` 确认，再按 `Ctrl + X` 退出

**第 6 步：重启 HotPlex**

```bash
hotplex gateway restart
```

你应该看到：

```
Gateway restarted successfully
```

如果提示 `hotplex: command not found`，试试：

```bash
# macOS/Linux
~/.hotplex/bin/hotplex gateway restart

# 或者重新安装后重试
export PATH="$HOME/.hotplex/bin:$PATH"
hotplex gateway restart
```

**第 7 步：验证修复**

1. **清除浏览器缓存**：按 `Ctrl + Shift + Delete`（Windows）或 `Cmd + Shift + Delete`（Mac），选择"缓存的图片和文件"，点击清除
2. **关闭浏览器**，重新打开
3. 再次访问 `http://192.168.1.100:8888`
4. 登录，观察页面是否正常加载，不再转圈

**如果还是转圈**，按 F12 打开开发者工具，切换到 Network（网络）面板，刷新页面，找到 `workspaces` 这个请求，看它的状态码：

- **200** → 成功了！问题已解决
- **403** → 配置没生效，检查第 4 步的地址是否写对了
- **401** → 登录失效了，重新登录试试
- **其他** → 看问题四

---

#### 方法 B：使用反向代理（适合有域名的场景）

如果你有域名（比如 `hotplex.example.com`），可以通过 Nginx 反向代理让浏览器认为"WebChat 和 API 是同一个地址"，从而避免跨域问题。

**第 1 步：安装 Nginx**

```bash
# Ubuntu/Debian
sudo apt update && sudo apt install nginx

# CentOS/RHEL
sudo yum install nginx

# macOS
brew install nginx
```

**第 2 步：配置 Nginx**

创建配置文件：

```bash
sudo nano /etc/nginx/sites-available/hotplex
```

粘贴以下内容（把 `hotplex.example.com` 改成你的域名）：

```nginx
server {
    listen 80;
    server_name hotplex.example.com;

    location / {
        proxy_pass http://127.0.0.1:8888;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

保存并启用：

```bash
sudo ln -s /etc/nginx/sites-available/hotplex /etc/nginx/sites-enabled/
sudo nginx -t          # 测试配置是否正确
sudo systemctl reload nginx   # 重载 Nginx
```

**第 3 步：修改 HotPlex 配置**

编辑 `~/.hotplex/config.yaml`：

```yaml
security:
  allowed_origins:
    - "http://hotplex.example.com"   # 你的域名
```

**第 4 步：重启 HotPlex**

```bash
hotplex gateway restart
```

**第 5 步：验证**

访问 `http://hotplex.example.com`，登录，检查是否正常。

---

#### 方法 C：只在本机使用（最简单）

如果你只需要在安装 HotPlex 的那台电脑上使用，不需要从其他设备访问，那么：

1. **不要修改任何配置**
2. 始终通过 `http://localhost:8888` 访问
3. 不要改成 `127.0.0.1` 或服务器 IP

localhost 访问时，浏览器会自动认为是"同源"，不会触发跨域检查。

---

### 问题一的完整检查清单

完成上述步骤后，逐项检查：

- [ ] 配置文件中的 `allowed_origins` 包含你的访问地址（含协议和端口）
- [ ] 地址格式正确：`http://192.168.1.100:8888`（不是 `192.168.1.100:8888`，不是 `http://192.168.1.100:8888/`）
- [ ] 已执行 `hotplex gateway restart` 重启服务
- [ ] 已清除浏览器缓存
- [ ] 浏览器地址栏显示的地址与配置中的地址完全一致

如果全部打勾还是不行，看下面的"还是不行？"部分。

---

## 问题二：opencode_server 创建会话失败

### 你会看到什么

1. 在 WebChat 中创建新会话
2. 选择 Worker 类型为 `opencode_server`
3. 点击创建，页面提示"failed"或"创建失败"
4. 如果选择 `claude_code`，则创建成功

### 为什么会这样

`opencode_server` 和 `claude_code` 是两种不同的 AI Worker：

- **claude_code**：每次创建会话时启动一个独立的 Claude 进程，简单直接
- **opencode_server**：使用一个共享的 OpenCode 服务进程，所有会话共用

`opencode_server` 的启动过程更复杂，需要：
1. `opencode` 程序已安装
2. 启动一个后台服务进程
3. 等待服务就绪
4. 通过 HTTP 创建会话

任何一个环节出问题，都会导致创建失败。

### 解决方法

#### 第 1 步：确认 opencode 已安装

打开终端，输入：

```bash
which opencode
```

**你应该看到**：一个路径，比如 `/usr/local/bin/opencode` 或 `/home/你的用户名/.local/bin/opencode`

**如果看到**：`opencode not found` 或没有输出

→ **opencode 没有安装**，请先安装：

```bash
# 方法 1：使用官方安装脚本
curl -fsSL https://opencode.ai/install | bash

# 方法 2：如果你用 npm
npm install -g opencode

# 方法 3：从源码编译（需要 Go 1.26+）
git clone https://github.com/opencode-ai/opencode.git
cd opencode
go build -o opencode ./cmd/opencode
sudo mv opencode /usr/local/bin/
```

安装完成后，验证：

```bash
opencode --version
```

应该输出版本号，比如 `opencode version 1.2.3`。

#### 第 2 步：检查 HotPlex 配置

编辑 `~/.hotplex/config.yaml`，找到 `worker` 部分：

```yaml
worker:
  opencode_server:
    command: "opencode"       # 确保这里与 which opencode 的输出一致
    ready_timeout: 15s        # 如果网络慢，可以改成 30s
```

**如果 `which opencode` 输出的是完整路径**（比如 `/usr/local/bin/opencode`），建议把 `command` 也改成完整路径：

```yaml
worker:
  opencode_server:
    command: "/usr/local/bin/opencode"
```

保存后重启：

```bash
hotplex gateway restart
```

#### 第 3 步：查看日志定位问题

```bash
hotplex gateway logs -f
```

这个命令会实时显示日志。**保持这个窗口打开**，然后在另一个终端或浏览器中尝试创建 opencode_server 会话。

**观察日志中出现的错误信息**，对照下表：

| 你看到的日志 | 问题是什么 | 怎么解决 |
|-------------|-----------|---------|
| `executable file not found in path` | opencode 没安装或不在 PATH 中 | 回到第 1 步安装，或在配置中写完整路径 |
| `timeout discovering port` | opencode 启动了但没输出端口信息 | 可能是 opencode 版本不兼容，尝试更新到最新版 |
| `timeout waiting for server health` | opencode 服务启动了但不响应 | 检查 opencode 是否正常运行：`ps aux \| grep opencode` |
| `create session failed: status 500` | opencode 内部错误 | 查看 opencode 自身的日志 |
| `singleton not initialized` | HotPlex 配置问题 | 检查 config.yaml 中 `worker.opencode_server` 配置是否正确 |
| `connection refused` | opencode 服务已停止 | 重启 HotPlex：`hotplex gateway restart` |

#### 第 4 步：手动测试 opencode 服务

如果日志没有明确错误，可以手动测试 opencode 是否能正常启动：

```bash
# 启动 opencode 服务（按 Ctrl+C 停止）
opencode serve --port 0
```

**你应该看到**类似这样的输出：

```
Starting OpenCode server...
listening on http://127.0.0.1:12345
```

**如果没有看到 `listening on` 这一行**，或者输出格式不同，说明 opencode 版本与 HotPlex 不兼容。请：

1. 更新 opencode 到最新版
2. 或暂时使用 `claude_code` Worker

#### 第 5 步：暂时使用 claude_code

如果 opencode_server 持续无法工作，你可以先用 claude_code 代替：

**方法 1：在 WebChat 中创建会话时选择 claude_code**

创建会话的界面中，Worker 类型选择 `claude_code`（通常是默认选项）。

**方法 2：修改默认 Worker 类型**

编辑 `~/.hotplex/config.yaml`：

```yaml
messaging:
  worker_type: claude_code   # 改成 claude_code
```

保存后重启：

```bash
hotplex gateway restart
```

#### 第 6 步：验证修复

完成上述步骤后，在 WebChat 中创建新会话：

1. 点击"新建会话"
2. 选择 Worker 类型（opencode_server 或 claude_code）
3. 输入会话标题
4. 点击创建

**你应该看到**：会话创建成功，进入对话界面，可以开始输入消息。

---

### 问题二的完整检查清单

- [ ] `which opencode` 有输出，且 `opencode --version` 显示版本号
- [ ] `~/.hotplex/config.yaml` 中 `worker.opencode_server.command` 与 `which opencode` 输出一致
- [ ] 已执行 `hotplex gateway restart`
- [ ] 日志中没有 `executable file not found` 错误
- [ ] 手动运行 `opencode serve --port 0` 能看到 `listening on` 输出

---

## 问题三：把 localhost 改成服务器 IP 后，新建工作区报错

### 你会看到什么

1. 你修改了 `~/.hotplex/config.yaml` 中的 `gateway.addr`，从 `localhost:8888` 改成 `192.168.1.100:8888`
2. 重启 HotPlex 后，访问 WebChat
3. 登录后，点击"新建工作区"或系统自动创建工作区时报错
4. 把 `gateway.addr` 改回 `localhost:8888`，错误消失

### 为什么会这样

这个问题和**问题一**是同一个原因。`gateway.addr` 控制的是 HotPlex 监听哪个地址，但真正影响跨域访问的是 `security.allowed_origins` 配置。

当你把 `gateway.addr` 改成服务器 IP 后，浏览器从服务器 IP 访问，但 `allowed_origins` 还是默认的 `["*"]`，导致写操作被拦截。

### 解决方法

**你不需要改 `gateway.addr`**。正确的做法是：

1. **保持 `gateway.addr` 为 `0.0.0.0:8888`**（监听所有网卡，允许外部访问）或 `localhost:8888`（仅本机访问）
2. **修改 `security.allowed_origins`**，添加你的访问地址

编辑 `~/.hotplex/config.yaml`：

```yaml
gateway:
  addr: "0.0.0.0:8888"   # 监听所有网卡，允许从任何 IP 访问

security:
  allowed_origins:
    - "http://192.168.1.100:8888"   # 你的服务器 IP
    - "http://localhost:8888"        # 本机访问
```

保存后重启：

```bash
hotplex gateway restart
```

然后按照**问题一的方法 A**中的"第 7 步：验证修复"操作。

### 常见误区

| 误区 | 正确做法 |
|------|---------|
| "把 `gateway.addr` 改成服务器 IP 就能从外部访问" | `gateway.addr` 应该改成 `0.0.0.0:8888`（监听所有网卡），而不是具体 IP |
| "改了 `gateway.addr` 就不需要改 `allowed_origins`" | 两者是独立的配置，都需要正确设置 |
| "用 `127.0.0.1:8888` 和 `localhost:8888` 是一样的" | 对浏览器来说不一样，`127.0.0.1` 可能触发跨域问题，建议统一用 `localhost` |

---

## 问题四：API Key 还有用吗？该怎么认证？

### 你的疑问

你可能注意到了：

- 以前的教程说"在 Admin 后台创建 API Key，然后用 API Key 访问"
- 现在的 WebChat 是"先登录账号，然后自动创建会话"
- 你不确定 API Key 还有没有用，或者该怎么用

### 答案：API Key 仍然完全有效

HotPlex 支持两种认证方式，**它们同时工作，互不影响**：

| 认证方式 | 适合谁用 | 怎么用 |
|---------|---------|--------|
| **API Key** | 开发者、脚本、自动化工具、SDK | 在请求中带上 API Key |
| **账号登录（Cookie）** | 普通用户、WebChat 界面 | 在网页上输入用户名密码登录 |

**简单来说**：
- 如果你用 WebChat 网页界面 → 用账号登录（你已经在用了）
- 如果你用代码/脚本/SDK 连接 HotPlex → 用 API Key

### API Key 怎么用

#### 第 1 步：在 Admin 后台创建 API Key

1. 访问 `http://localhost:8888/admin`（或你的服务器地址）
2. 登录 Admin 后台
3. 找到"API Keys"或"API 密钥"菜单
4. 点击"创建新的 API Key"
5. 给 Key 起个名字（比如"我的脚本"），选择对应的用户
6. 点击创建，**复制生成的 API Key**（只显示一次，务必保存好）

#### 第 2 步：使用 API Key 访问

**方式 1：通过 HTTP Header（推荐）**

```bash
curl -H "X-API-Key: 你的API密钥" \
  http://localhost:8888/api/sessions
```

**方式 2：通过 URL 参数**

```bash
curl "http://localhost:8888/api/sessions?api_key=你的API密钥"
```

**方式 3：在代码中使用**

```python
# Python 示例
import requests

headers = {"X-API-Key": "你的API密钥"}
response = requests.get("http://localhost:8888/api/sessions", headers=headers)
print(response.json())
```

```javascript
// JavaScript 示例
const response = await fetch("http://localhost:8888/api/sessions", {
  headers: { "X-API-Key": "你的API密钥" }
});
const data = await response.json();
console.log(data);
```

#### 第 3 步：验证 API Key 有效

```bash
# 测试 API Key 是否能正常获取会话列表
curl -H "X-API-Key: 你的API密钥" http://localhost:8888/api/sessions
```

**你应该看到**：一个 JSON 数组，包含你的会话列表，类似：

```json
{
  "sessions": [
    {
      "id": "abc123...",
      "title": "测试会话",
      "status": "active",
      ...
    }
  ],
  "total": 1
}
```

**如果看到**：`{"error":{"code":"UNAUTHORIZED","message":"..."}}`

→ API Key 无效或已过期，回到 Admin 后台重新创建一个。

### API Key 的常见用途

| 用途 | 示例 |
|------|------|
| **SDK 连接** | Go/Python/TypeScript/Java SDK 使用 API Key 认证 |
| **自动化脚本** | 定时任务、CI/CD 流水线中触发 AI 会话 |
| **消息平台** | Slack/飞书 Bot 的后端认证 |
| **多用户管理** | 为不同用户分配不同 Key，隔离会话和数据 |

### 两种认证方式的对比

| | API Key | 账号登录（Cookie） |
|---|---|---|
| **谁用** | 开发者、脚本、自动化工具 | 普通用户 |
| **怎么用** | 在请求头或 URL 中带上 Key | 在网页上输入用户名密码 |
| **需要 Admin 后台吗** | 需要（创建 Key） | 不需要 |
| **适合什么场景** | 代码集成、自动化、SDK | 日常使用 WebChat |
| **安全性** | Key 要保密，不要泄露 | 密码要保密，登录后浏览器自动管理 |
| **是否仍然有效** | ✅ 完全支持 | ✅ 完全支持 |

---

## 还是不行？完整的排查流程

如果按照上述步骤操作后问题仍然存在，按以下顺序逐项检查：

### 检查 1：HotPlex 是否在运行

```bash
hotplex gateway status
```

**你应该看到**：`Gateway is running (PID: 12345)`

**如果看到**：`Gateway is not running`

→ 启动 HotPlex：

```bash
hotplex gateway start
```

### 检查 2：端口是否在监听

```bash
# macOS/Linux
lsof -i :8888

# Windows
netstat -ano | findstr :8888
```

**你应该看到**：一行输出，显示 `hotplex` 进程在监听 8888 端口

**如果没有输出**：HotPlex 没有正常启动，查看日志：

```bash
hotplex gateway logs -n 50   # 查看最近 50 行日志
```

### 检查 3：配置文件是否正确

```bash
# 查看当前生效的 allowed_origins
grep -A 5 "allowed_origins" ~/.hotplex/config.yaml
```

**你应该看到**：

```yaml
allowed_origins:
  - "http://192.168.1.100:8888"
  - "http://localhost:8888"
```

**如果看到**：

```yaml
allowed_origins:
  - "*"
```

→ 回到问题一的方法 A，修改配置。

### 检查 4：浏览器缓存是否清除

1. 按 `Ctrl + Shift + Delete`（Windows）或 `Cmd + Shift + Delete`（Mac）
2. 选择"缓存的图片和文件"
3. 时间范围选"全部时间"
4. 点击"清除数据"
5. 关闭浏览器，重新打开

### 检查 5：用无痕模式测试

无痕模式不使用缓存和 Cookie，可以排除缓存问题：

- **Chrome**：`Ctrl + Shift + N`（Windows）或 `Cmd + Shift + N`（Mac）
- **Firefox**：`Ctrl + Shift + P`（Windows）或 `Cmd + Shift + P`（Mac）
- **Safari**：`Cmd + Shift + N`

在无痕窗口中访问 HotPlex，登录，检查是否正常。

### 检查 6：查看浏览器网络请求

1. 按 F12 打开开发者工具
2. 切换到 **Network**（网络）面板
3. 刷新页面
4. 找到失败的请求（红色），点击查看

**你应该关注**：

| 字段 | 期望值 | 如果不对 |
|------|--------|---------|
| **Status Code** | 200 | 403 → 跨域问题，看问题一；401 → 认证问题，看问题四 |
| **Request URL** | 你的服务器地址 | 如果地址不对，检查配置 |
| **Response Headers** 中的 `Access-Control-Allow-Origin` | 你的服务器地址 | 如果是 `*` 或缺失，说明配置没生效 |

### 检查 7：查看 HotPlex 日志

```bash
hotplex gateway logs -f
```

在另一个窗口中操作 WebChat，观察日志输出。

**你应该看到**：正常的请求日志，没有 `ERROR` 或 `WARN`。

**如果看到**：

- `WARN: wildcard "*" in allowed_origins` → 配置还是通配符，需要改成具体地址
- `ERROR: cross-origin write blocked` → CSRF 拦截，看问题一
- `ERROR: executable file not found` → opencode 没安装，看问题二

---

## 快速参考卡片

### 配置文件位置

| 系统 | 路径 |
|------|------|
| macOS/Linux | `~/.hotplex/config.yaml` |
| Windows | `%USERPROFILE%\.hotplex\config.yaml` |

### 常用命令

```bash
# 查看状态
hotplex gateway status

# 启动/停止/重启
hotplex gateway start
hotplex gateway stop
hotplex gateway restart

# 查看日志
hotplex gateway logs -f        # 实时跟踪
hotplex gateway logs -n 100    # 最近 100 行

# 诊断检查
hotplex doctor                 # 运行 25 项诊断检查
```

### 配置修改后的操作

**每次修改 `~/.hotplex/config.yaml` 后，都要重启 HotPlex**：

```bash
hotplex gateway restart
```

然后**清除浏览器缓存**或**用无痕模式测试**。

### 关键配置项

```yaml
# 监听地址（允许外部访问用 0.0.0.0，仅本机用 localhost）
gateway:
  addr: "0.0.0.0:8888"

# 允许的来源（必须包含你的访问地址）
security:
  allowed_origins:
    - "http://192.168.1.100:8888"   # 你的服务器 IP
    - "http://localhost:8888"        # 本机

# Worker 类型（默认 claude_code）
messaging:
  worker_type: claude_code

# opencode_server 配置（如果用 opencode_server）
worker:
  opencode_server:
    command: "opencode"       # opencode 二进制路径
    ready_timeout: 15s        # 启动超时
```

---

## 获取帮助

如果本手册没有解决你的问题：

1. **运行诊断**：`hotplex doctor`，它会检查 25 项常见问题并给出建议
2. **查看日志**：`hotplex gateway logs -n 200`，把日志内容发给支持者
3. **提交 Issue**：在 [GitHub](https://github.com/hrygo/hotplex/issues) 提交问题，附上：
   - 你的 HotPlex 版本：`hotplex version`
   - 你的操作系统：`uname -a`（Mac/Linux）或 `ver`（Windows）
   - 你的配置文件：`cat ~/.hotplex/config.yaml`（注意删除敏感信息）
   - 错误日志：`hotplex gateway logs -n 100`
   - 浏览器控制台的错误信息（F12 → Console）

---

## 相关文档

- [配置完整参考](../../reference/configuration.md) — 所有配置项的详细说明
- [CLI 命令参考](../../reference/cli.md) — 所有 hotplex 命令的用法
- [认证与授权](../../security/Security-Authentication.md) — API Key 和 Cookie 认证的技术细节
- [安全策略](../../reference/security-policies.md) — CSRF、CORS 等安全机制说明
