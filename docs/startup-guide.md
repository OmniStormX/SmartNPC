# SmartNPC 项目启动指南

> 从零到 NPC 在游戏里说话，完整的分步手册。

---

## 目录

1. [整体架构与网络拓扑](#1-整体架构与网络拓扑)
2. [环境要求](#2-环境要求)
3. [首次安装](#3-首次安装)
4. [两种运行模式](#4-两种运行模式)
5. [手动分步启动](#5-手动分步启动)
6. [一键启动 (run.bat)](#6-一键启动-runbat)
7. [逐层验证](#7-逐层验证)
8. [常见问题排查](#8-常见问题排查)
9. [开发环境差异](#9-开发环境差异)

---

## 1. 整体架构与网络拓扑

### 1.1 三层架构

```
┌─────────────────────────────────────────────────────────────────┐
│  Windows 主机                                                     │
│                                                                   │
│  ┌─────────────────────┐     ┌──────────────────────────────┐   │
│  │ Stardew Valley +     │     │ smartnpc-mcp (Go)             │   │
│  │ SMAPI + Mod          │ ws  │                              │   │
│  │ WebSocket Server     │◄───►│ HTTP :3000/mcp               │   │
│  │ :18745/ws            │     │ hermesrelay → 转发事件         │   │
│  └─────────────────────┘     └──────────┬───────────────────┘   │
│                                         │                        │
│                                         │ HTTP (WSL IP)           │
│                                         │ + MCP (Win IP)           │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─  │
│  WSL2 (Ubuntu-22.04)                   │                        │
│  ┌─────────────────────────────────────┼──────────────────────┐ │
│  │  Docker 或 本地进程                  │                      │ │
│  │                                     ▼                      │ │
│  │  hermes-xiami    :8642  ◄── MCP client + /v1/responses    │ │
│  │  hermes-abigail  :8643                                     │ │
│  │  hermes-haley    :8644                                     │ │
│  │  hermes-harvey   :8645                                     │ │
│  │  hermes-penny    :8646                                     │ │
│  │  hermes-sebastian:8647                                     │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 关键地址

| 组件 | 地址 | 说明 |
|------|------|------|
| Mod WebSocket | `ws://127.0.0.1:18745/ws` | 游戏内 ws server，只连本机 |
| MCP HTTP | `http://127.0.0.1:3000/mcp` | Streamable HTTP MCP 端点 |
| MCP Health | `http://127.0.0.1:3000/healthz` | MCP 存活探针 |
| Hermes Gateway | `http://<WSL_IP>:8642-8647` | 每个 NPC 一个端口 |
| WSL ← Windows 互访 | `WIN_HOST_IP` ≈ `192.168.48.1`，`WSL_IP` ≈ `192.168.59.118` | 自动探测 |

**网络要点：**
- WSL 内的容器/进程访问 Windows 上的 MCP 用 `WIN_HOST_IP`（不是 `127.0.0.1`）
- Windows 上的 `run.bat` 访问 WSL 内的 Hermes 用 `WSL_IP`
- `hermes/runtime-config.yaml` 中 `gateway_url` 指向 WSL IP
- `deploy/hermes/.env` 中 `SMARTNPC_MCP_URL` 指向 Windows 主机 IP

---

## 2. 环境要求

### 2.1 必装软件

| 软件 | 最低版本 | 用途 | 安装方式 |
|------|---------|------|---------|
| Windows 10/11 | — | 宿主系统 | — |
| Stardew Valley | 1.6+ | 游戏本体 | Steam / GOG |
| SMAPI | 4.0+ | Mod loader | [smapi.io](https://smapi.io) |
| .NET SDK | 6.0（推荐 8.0） | 构建 C# mod | `winget install Microsoft.DotNet.SDK.8` |
| Go | 1.25+ | 构建 Go MCP | `winget install GoLang.Go` |
| Task | latest | 统一构建入口 | `go install github.com/go-task/task/v3/cmd/task@latest` |
| WSL2 | Ubuntu-22.04 | Hermes 运行环境 | `wsl --install -d Ubuntu-22.04` |
| Docker Engine | WSL 内 | Hermes 容器（docker 模式） | 见 §3.3 |
| Git | latest | 版本控制 | `winget install Git.Git` |

### 2.2 验证安装

在 **新的 cmd 窗口**中逐条验证（每条都应当输出正常）：

```cmd
:: .NET SDK
dotnet --version
:: 应输出 6.0.x 或 8.0.x

:: Go
go version
:: 应输出 go1.25.x 或更高

:: Task
"%USERPROFILE%\go\bin\task.exe" --version
:: 应输出 Task version: 3.x.x

:: WSL
wsl --status
:: 应显示 Default Distribution: Ubuntu-22.04

:: Docker（在 WSL 内）
wsl -d Ubuntu-22.04 docker info
:: 应输出 Docker 系统信息，不报错

:: 游戏
dir "D:\Stardew Valley\StardewModdingAPI.exe"
:: 应找到文件（路径按实际改）
```

> 任意一条不通，**不要往下走**。先解决该条依赖。

---

## 3. 首次安装

### 3.1 克隆仓库

```cmd
d:
git clone <仓库地址> SmartNPC
cd D:\SmartNPC
```

### 3.2 配置 .env

```cmd
copy .env.example .env
notepad .env
```

**最小配置——必须填这 4 项：**

```env
# 1. 游戏安装目录（必须含 StardewModdingAPI.exe）
SMARTNPC_GAME_PATH=D:\Stardew Valley

# 2. LLM API 地址
HERMES_AGENT_URL=https://api.deepseek.com

# 3. LLM API Key
HERMES_AGENT_API_KEY=sk-xxxxxxxxxxxxxxxx

# 4. 模型名
HERMES_AGENT_MODEL=deepseek-v4-pro
```

其他变量全有合理默认值。完整说明见 `.env.example` 注释。

### 3.3 安装 WSL Docker（仅 Docker 模式需要）

在 WSL 终端中：

```bash
# 进入 WSL
wsl -d Ubuntu-22.04

# 安装 Docker Engine
sudo apt update
sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
sudo usermod -aG docker $USER

# 退出并重新进入 WSL 让 docker 组权限生效
exit
wsl -d Ubuntu-22.04
docker info          # 验证
docker compose version
```

### 3.4 一键 Setup（Docker 模式）

```cmd
cd D:\SmartNPC
setup.bat
```

`setup.bat` 执行 7 个步骤：

| 步骤 | 操作 | 耗时 |
|------|------|------|
| [1/7] | 加载 `.env` | <1s |
| [2/7] | 检查前置条件（游戏路径 / LLM 配置 / WSL / Docker） | <5s |
| [3/7] | 渲染 Hermes profiles（`_master/` → 6 NPC） | <10s |
| [4/7] | 探测 WSL/Windows IP | <5s |
| [5/7] | 从 `npcs.yaml` 生成 `docker-compose.yml` | <5s |
| [6/7] | 生成 `deploy/hermes/.env`（Docker 容器用） | <1s |
| [7/7] | 同步 profiles → WSL + `docker compose build` | 3–5min |

> 如果 WSL 发行版名不是 `Ubuntu-22.04`，在 `.env` 中设 `WSL_DISTRO=你的发行版名`。

### 3.5 Local 模式的首次准备

如果不用 Docker、直接跑 Hermes CLI（`SMARTNPC_HERMES_MODE=local`）：

```bash
# WSL 内安装 Hermes
pip install hermes-agent

# 渲染 profiles
bash /mnt/d/SmartNPC/scripts/render_profiles.sh

# 安装 profiles 到 Hermes 目录
bash /mnt/d/SmartNPC/hermes/install.sh
```

无需跑 `setup.bat`（它全程为 Docker 模式设计）。

---

## 4. 两种运行模式

### 4.1 对比

| | Docker 模式（默认） | Local 模式 |
|---|---|---|
| Hermes 运行方式 | WSL 内 Docker 容器 | WSL 内直接 `hermes` 进程 |
| `.env` 配置 | `SMARTNPC_HERMES_MODE=docker` | `SMARTNPC_HERMES_MODE=local` |
| 首次准备 | `setup.bat` | 手动 `pip install hermes-agent` + `render_profiles.sh` + `install.sh` |
| 启动命令 | `run.bat`（自动 `docker compose up`） | `run.bat` + 手动在 WSL 起 gateway |
| 内存占用 | 每个 NPC ~300-500MB（容器） | 每个 NPC ~200-400MB（进程） |
| 适合场景 | 日常开发、稳定可复现 | 快速迭代、调 Hermes 源码 |
| IP 管理 | `deploy/hermes/.env` 自动生成 | 手动设 `runtime-config.yaml` 中 `gateway_url` |

### 4.2 切换模式

在 `.env` 中改一行：

```env
# Docker 模式（默认）
SMARTNPC_HERMES_MODE=docker

# Local 模式
SMARTNPC_HERMES_MODE=local
```

重跑 `setup.bat`（Docker）或手动执行 Local 准备步骤。

---

## 5. 手动分步启动

> 推荐第一次启动走手动流程，逐层确认后再用 `run.bat`。

### Step 1: 构建

```cmd
cd D:\SmartNPC

:: 构建 mod（C# SMAPI 插件）
task mod:build

:: 构建 MCP（Go 二进制）
task mcp:build
```

产物：
- `smartnpc-mcp/bin/smartnpc-mcp.exe`
- `smapi-mod/bin/Debug/net6.0/StardewMCPBridge.dll`

### Step 2: 安装 Mod + 启动游戏

```powershell
# 安装 mod 到游戏 Mods 目录
task mod:install

# 启动游戏（通过 SMAPI）
& "D:\Stardew Valley\StardewModdingAPI.exe"
```

游戏启动后，看 SMAPI 控制台应出现 `[SmartNPC]` 开头的日志行。进入任意存档。

此时 Mod 已在 `ws://127.0.0.1:18745/ws` 监听。

### Step 3: 启动 smartnpc-mcp

```powershell
cd D:\SmartNPC\smartnpc-mcp

# Docker 模式（默认）
.\bin\smartnpc-mcp.exe `
  --http :3000 `
  --ws-url ws://127.0.0.1:18745/ws `
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml `
  --hermes-api-key smartnpc-test-key `
  --log-level debug

# Echo 模式（不接 LLM，纯验证 ws + MCP 工具注册）
.\bin\smartnpc-mcp.exe `
  --http :3000 `
  --ws-url ws://127.0.0.1:18745/ws `
  --echo-mode `
  --log-level debug
```

**验证 MCP 已上线：**

```powershell
# 新开一个终端
curl http://127.0.0.1:3000/healthz
# 应输出 OK

curl http://127.0.0.1:3000/status
# 应输出 MCP server status JSON
```

### Step 4: 启动 Hermes Gateway

#### Docker 模式

```powershell
# Windows 终端
wsl -d Ubuntu-22.04 bash -lc "cd /mnt/d/SmartNPC/deploy/hermes && docker compose up -d"

# 验证每个 gateway 健康
curl http://<WSL_IP>:8642/health   # xiami
curl http://<WSL_IP>:8643/health   # abigail
```

> WSL IP 可通过 `wsl -d Ubuntu-22.04 hostname -I` 获取。

#### Local 模式

```bash
# WSL 终端
bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh xiami,abigail
```

按需调整 NPC 列表。每个 gateway 日志在 `~/.hermes/profiles/<npc>/logs/gateway.log`。

### Step 5: 端到端验证

1. 游戏里加载存档
2. 按 `Tab` → 应出现 SmartNPC 聊天面板
3. 在联系人列表（`F2`）看到 NPC
4. 点击 NPC 发消息 → NPC 应回复

> 首次 LLM 调用可能有几秒延迟（冷启动），后续更快。

---

## 6. 一键启动 (run.bat)

```cmd
cd D:\SmartNPC
run.bat
```

### 6.1 执行流程

| 阶段 | 操作 | 预期耗时 |
|------|------|---------|
| `[env]` | 加载 `.env` + 设默认值 | <1s |
| `[cfg]` | 打印关键变量（游戏路径、IP、端口、活跃 NPC） | <1s |
| `[1/5]` | `task mod:build` + `task mcp:build` | 30s–2min |
| `[2/5]` | 杀旧游戏/mcp 进程 | <2s |
| `[3/5]` | 装 mod + 起 mcp HTTP（新窗口） + 等待 `/mcp` 端口 | 5–15s |
| `[4/5]` | `docker compose up -d` + 逐个等 gateway `/health` | 每个 30–60s |
| `[5/5]` | 起 `StardewModdingAPI.exe` | 立即 |

### 6.2 自定义启用的 NPC

在 `.env` 中设：

```env
# 只起 xiami 和 abigail（省资源）
SMARTNPC_ACTIVE_PROFILES=xiami,abigail

# 起全部 6 个
SMARTNPC_ACTIVE_PROFILES=xiami,abigail,haley,harvey,penny,sebastian
```

端口从 `hermes/npcs.yaml` 自动读取，无需手动改任何配置。

### 6.3 run.bat 日志

每次运行生成带时间戳的日志：

```
logs/
├── mcp_20260622_143000.log      # MCP 进程输出
└── payload_20260622_143000.log  # Hermes relay 请求/响应 body（若开调试）
```

MCP 进程还有交互式 PowerShell 窗口，实时看日志。

---

## 7. 逐层验证

推荐按自下而上的顺序排查：

### L1: 游戏层

```powershell
# 确认 SMAPI 正常运行
# → 看 SMAPI 控制台，应出现 "[SmartNPC]" 开头日志

# 确认 ws 端口在监听
netstat -ano | findstr 18745
# → 应看到 LISTENING
```

### L2: MCP 层

```powershell
# MCP 进程存活
task mcp:health

# 或手动
curl http://127.0.0.1:3000/healthz
# → OK

# 检查 MCP tools 列表
curl -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' http://127.0.0.1:3000/mcp
# → 返回含 chat_say, npc_move_to 等工具的大型 JSON
```

### L3: Hermes 层

```powershell
# 获取 WSL IP
wsl -d Ubuntu-22.04 hostname -I
# → 如 192.168.59.118

# 检查每个 gateway 健康
curl http://192.168.59.118:8642/health   # xiami
curl http://192.168.59.118:8643/health   # abigail
# → 均应返回 {"status":"ok"} 或类似正常 JSON
```

```bash
# WSL 内看 gateway 日志
wsl -d Ubuntu-22.04
docker compose -f /mnt/d/SmartNPC/deploy/hermes/docker-compose.yml logs --tail=50 hermes-xiami

# Local 模式
cat ~/.hermes/profiles/xiami/logs/gateway.log
```

### L4: 端到端

1. 游戏内按 `Tab` → 聊天面板出现
2. 选择 NPC → 发消息
3. NPC 回复 → 全链路通畅

**Echo 模式端到端：** 启动 MCP 时加 `--echo-mode`，不发 LLM，NPC 直接回声玩家消息。用于快速确认 ws + MCP 工具注册正常。

---

## 8. 常见问题排查

### 8.1 启动类

| 症状 | 原因 | 解决 |
|------|------|------|
| `setup.bat` 报 `HERMES_AGENT_URL is not set` | `.env` 未填 LLM 配置 | 编辑 `.env` 填 4 个必填项 |
| `setup.bat` 报 `Docker is not available in WSL` | Docker 未装或未启动 | `wsl -d Ubuntu-22.04 sudo service docker start`；若未装回 §3.3 |
| `setup.bat` 报 `StardewModdingAPI.exe not found` | 游戏路径不对 | `.env` 中改 `SMARTNPC_GAME_PATH` |
| `run.bat` 报 `docker-compose.yml not found` | 没跑过 `setup.bat` | 先跑 `setup.bat` |
| `run.bat` [1/5] `task` 找不到 | task 不在默认路径 | `.env` 设 `TASK_EXE=C:\Users\...\go\bin\task.exe` |
| `run.bat` [3/5] 一直 `waiting for mcp` | MCP 启动失败 | 看 `smartnpc-mcp` PowerShell 窗口报错；常见：端口占用、`ws-url` 不对 |
| `run.bat` [4/5] 一直 `waiting for xiami` | Docker 容器未正常启动 | `wsl docker compose -f /mnt/d/SmartNPC/deploy/hermes/docker-compose.yml logs --tail=80 hermes-xiami` |

### 8.2 网络类

| 症状 | 原因 | 解决 |
|------|------|------|
| gateway 日志 `connection refused` 连 MCP | `WIN_HOST_IP` 不对 | 重跑 `setup.bat` 重新探测 IP；或手动改 `deploy/hermes/.env` |
| gateway 日志 `401 Unauthorized` | API Key 不一致 | 检查 `.env` `SMARTNPC_HERMES_KEY` 和 profile `config-overlay.yaml` 中 `API_SERVER_KEY` 一致 |
| MCP 日志 ws 连不上 | 游戏未启动 / ws 端口未开 | 确认游戏已进存档；`netstat -ano | findstr 18745` |
| gateway 缓存 "0 tools" | MCP 晚于 Hermes 启动 | `run.bat` 已保证 mcp 先起；若手动启动需注意顺序 |

### 8.3 LLM 类

| 症状 | 原因 | 解决 |
|------|------|------|
| gateway 日志 `401` / `403` | LLM API Key 无效 | 检查 `.env` `HERMES_AGENT_API_KEY` |
| gateway 日志 `model not found` | 模型名不对 | 改 `.env` `HERMES_AGENT_MODEL` |
| NPC 不回话，gateway 日志无 LLM 调用 | 事件未路由到该 NPC | 看 MCP 日志中 relay 转发记录 |
| NPC 回复很慢（>30s） | LLM 冷启动 / API 限速 | 首次慢正常；若持续检查 API 配额 |

### 8.4 游戏内

| 症状 | 原因 | 解决 |
|------|------|------|
| 按 `Tab` 无反应 | Mod 未加载 | SMAPI 控制台看有无 `[SmartNPC]` 日志；`task mod:install` |
| 面板出现但列表为空 | 无 Agent NPC 注册 | 进存档后等几秒，Mod 自动注册 |
| WSL IP 变了自己不知道 | WSL 重启后 DHCP 换 IP | `run.bat` 自动重新探测；手动 `wsl -d Ubuntu-22.04 hostname -I` |
| WDAC 拦截 `smartnpc-mcp.exe` | Windows 安全策略 | 实测 2026-04-30 起不再拦截；若遇到 `bin\xxx.exe --version` 确认 |

### 8.5 排查决策树

```
NPC 不回话？
├── 检查 MCP 是否在线
│   └── curl http://127.0.0.1:3000/healthz
│       ├── 不通 → MCP 没起来，看 PowerShell 窗口
│       └── 通 ↓
├── 检查 Hermes Gateway 是否健康
│   └── curl http://<WSL_IP>:8642/health
│       ├── 不通 → Docker/Local gateway 没起来，看日志
│       └── 通 ↓
├── 检查游戏是否正常运行
│   └── 游戏内按 Tab
│       ├── 无面板 → Mod 未加载
│       └── 有面板 ↓
├── 检查 MCP tools 在 Hermes 是否可见
│   └── gateway 日志搜索 "tools/list" 或 "0 tools"
│       ├── 0 tools → 重启 gateway（mcp 先于 gateway）
│       └── N tools ↓
└── 可能原因：
    ├── LLM API Key 过期 / 额度用完
    ├── 事件未路由到该 NPC（看 relay 日志）
    └── LLM 返回了空响应（看 gateway 日志）
```

---

## 9. 开发环境差异

### 9.1 开发时的快捷方式

| 场景 | 命令 |
|------|------|
| 只改 Go 代码 | `task mcp:build`，重启 MCP 进程 |
| 只改 C# Mod | `task mod:install`，重启游戏 |
| 只改 Hermes SKILL | 重跑 `task profiles:render`，重启 gateway |
| 改 `npcs.yaml`（增删 NPC） | 重跑 `setup.bat` |
| 改 `.env` LLM 凭据 | 重跑 `setup.bat`（让新凭据写入 Docker `.env`） |

### 9.2 Echo 模式（离线开发）

不需要 LLM 在线即可验证游戏 ↔ MCP 往返：

```powershell
task mcp:run-echo
```

NPC 会把你发的消息原样回声。用于验证：
- ws 连接正常
- MCP 工具注册正常
- 聊天面板 UI 正常

### 9.3 只跑部分 NPC

```env
# .env
SMARTNPC_ACTIVE_PROFILES=xiami
```

只起 xiami 一个 gateway。其他 5 个不消耗内存/LLM 额度。

### 9.4 跨平台开发

Go 部分（`smartnpc-mcp`）在 Windows/Linux/macOS 均可编译运行。C# mod 仅 Windows（依赖 SMAPI + SDV）。

在 Linux 上开发 Go 部分：
```bash
cd smartnpc-mcp
go test ./...
go build -o bin/smartnpc-mcp ./cmd/smartnpc-mcp
```

详见 `CLAUDE.md` §「跨平台 / Linux 接入注意」。

### 9.5 VS Code / Cursor 推荐配置

```json
{
  "go.goroot": "C:\\Program Files\\Go",
  "go.gopath": "C:\\Users\\<user>\\go",
  "dotnet.rootPath": "C:\\Program Files\\dotnet",
  "files.exclude": {
    "**/obj": true,
    "**/bin": true,
    "**/.gotmp": true
  }
}
```

---

## 附录 A: 端口一览

| 端口 | 组件 | 协议 |
|------|------|------|
| 18745 | SMAPI Mod WebSocket Server | WebSocket |
| 3000 | smartnpc-mcp HTTP | HTTP (MCP Streamable) |
| 8642 | Hermes → xiami | HTTP |
| 8643 | Hermes → abigail | HTTP |
| 8644 | Hermes → haley | HTTP |
| 8645 | Hermes → harvey | HTTP |
| 8646 | Hermes → penny | HTTP |
| 8647 | Hermes → sebastian | HTTP |

## 附录 B: 关键配置文件

| 文件 | 谁读 | 内容 |
|------|------|------|
| `.env` | Taskfile, run.bat, setup.bat | 本机环境变量 |
| `.env.example` | 人 | 变量文档 |
| `hermes/npcs.yaml` | setup.bat, render_profiles.sh, start_hermes_profiles.sh | NPC 注册表（真相源） |
| `hermes/runtime-config.yaml` | smartnpc-mcp (hermesrelay) | NPC→gateway 路由表 |
| `hermes/profiles/<npc>/SOUL.md` | Hermes Agent | NPC 人格定义 |
| `hermes/profiles/<npc>/config-overlay.yaml` | Hermes Agent | Gateway/MCP 连接配置 |
| `hermes/profiles/_master/` | render_profiles.sh | 共享 SKILL 模板 |
| `deploy/hermes/docker-compose.yml` | Docker | 容器编排（由 setup.bat 生成） |
| `deploy/hermes/.env` | Docker 容器 | MCP_URL + LLM 凭据（由 setup.bat 生成） |

