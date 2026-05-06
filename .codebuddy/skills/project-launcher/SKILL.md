---
name: project-launcher
description: 当用户要求启动、运行、跑起来、launch、start SmartNPC 项目的某个组件（smartnpc-mcp / SMAPI Mod / 整个项目）时使用此 skill。触发词包括"启动项目"、"跑一下 mcp"、"启动 mcp"、"run server"、"项目跑起来"、"launch"、"启动游戏 mod" 等。提供一套 Windows 原生的启动 SOP（不使用 WSL），并列出已知的 WDAC / Device Guard 拦截绕过策略与排查步骤。
---

# Project Launcher

启动 SmartNPC 项目的各个组件。**仅 Windows 原生方案**（Hermes 除外，它跑在 WSL）。

## 何时使用

应当使用：
- 用户说"启动项目"/"跑起来"/"启动 mcp"/"run server"/"启动 agent"
- 用户改完代码想本地起来 smoke test

不应使用：
- 用户只是想跑测试（用 `task test`）
- 用户问"项目架构是什么"

## 组件清单与启动顺序

```
1. Hermes API Server (WSL)     → 提供 LLM 能力
2. SMAPI Mod (Stardew Valley)  → 提供游戏 API（ws :18745）
3. smartnpc-agent (Windows)    → spawn mcp 子进程 + 连 Hermes + 连 Mod
```

⚠️ **关键约束**：mod 的 ws 端口 `:18745` 只接受**一个**客户端。不要同时跑 `task mcp:run`（独立 mcp）和 `smartnpc-agent`（spawn mcp），否则两个 mcp 进程会互踢。

## 启动 SOP

### 步骤 1 — 前置检查

```cmd
go version
smartnpc-agent\bin\smartnpc-agent.exe -version
smartnpc-mcp\bin\smartnpc-mcp.exe --version
```

缺二进制就先 build：
```cmd
"%USERPROFILE%\go\bin\task.exe" build
```

### 步骤 2 — 启动 Hermes API Server（WSL Ubuntu-22.04）

Hermes 作为 OpenAI 兼容 LLM 后端运行。配置已在 `~/.hermes/.env`：

```
API_SERVER_ENABLED=true
API_SERVER_KEY=smartnpc-test-key
API_SERVER_HOST=0.0.0.0
```

启动命令（在 WSL 或另一个终端）：

```bash
hermes gateway run --accept-hooks
```

验证（Windows 侧）：
```cmd
curl -sS http://localhost:8642/health
```
期望：`{"status": "ok", "platform": "hermes-agent"}`

### 步骤 3 — 启动 Stardew Valley（SMAPI）

必须通过 SMAPI 启动：
```cmd
"D:\Stardew Valley\StardewModdingAPI.exe"
```

Mod 安装（如需更新）：
```cmd
"%USERPROFILE%\go\bin\task.exe" mod:install
```

> 注意：游戏运行时 DLL 被锁，需**关游戏后**再 install。

### 步骤 4 — 启动 smartnpc-agent（核心命令）

```cmd
cd /d d:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe ^
  -mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe ^
  -mcp-args="--ws-url=ws://127.0.0.1:18745/ws" ^
  -log-level debug ^
  run ^
  -llm-url http://localhost:8642/v1 ^
  -api-key smartnpc-test-key ^
  -model hermes-agent ^
  -speaker XiaMi
```

正常输出：
```
{"level":"INFO","msg":"smartnpc-agent starting"}
{"level":"INFO","msg":"mcp server spawned"}
{"level":"INFO","msg":"ws connected","url":"ws://127.0.0.1:18745/ws"}
{"level":"INFO","msg":"waiting for chat events..."}
```

此时在游戏内按 `Ctrl+T` 打开聊天框输入文字，agent 会通过 Hermes LLM 生成回复并让 NPC 说话。

### 步骤 5 — 停止

- Agent：在它所在的 cmd 窗口按 `Ctrl+C`
- Hermes：在 WSL 终端按 `Ctrl+C`
- 游戏：正常退出

## 独立 MCP 模式（不走 Agent）

如果只需要验证 MCP server 本身（不需要 LLM），可以单独跑 mcp：

```cmd
"%USERPROFILE%\go\bin\task.exe" mcp:run
```

此模式暴露 HTTP `:3000` + 连 ws bridge。用于：
- `curl` 直接测试 MCP 工具
- Hermes 通过 `hermes mcp add smartnpc --url http://...:3000/mcp` 直连
- Echo mode 验证（加 `ECHO=1`）

⚠️ **不要和 agent 同时运行**（会抢 ws 端口）。

## 常见问题

| 现象 | 原因 | 解决 |
|------|------|------|
| ws 反复 connect/disconnect | 两个 mcp 进程抢 `:18745` | 停掉 `task mcp:run`，只保留 agent |
| `unknown subcommand: ws://...` | `-mcp-args` 引号被 cmd 拆分 | 用 `=` 号：`-mcp-args="--ws-url=ws://..."` |
| Hermes health 不通 | gateway 没启动 / 防火墙 | 确认 WSL 里 `hermes gateway run` 在跑 |
| DLL 被锁无法 install | 游戏正在运行 | 先关游戏再 `task mod:install` |
| WDAC 拦截 exe | 公司策略 | 参考 `references/wdac-bypass.md` |

## 内置资源

| 路径 | 用途 |
|------|------|
| `references/wdac-bypass.md` | WDAC 绕过方案 |
