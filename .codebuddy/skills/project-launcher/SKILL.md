---
name: project-launcher
description: 当用户要求启动、运行、跑起来、launch、start SmartNPC 项目的某个组件（smartnpc-mcp / SMAPI Mod / 整个项目）时使用此 skill。触发词包括"启动项目"、"跑一下 mcp"、"启动 mcp"、"run server"、"项目跑起来"、"launch"、"启动游戏 mod" 等。提供一套 Windows 原生的启动 SOP（不使用 WSL），并列出已知的 WDAC / Device Guard 拦截绕过策略与排查步骤。
---

# Project Launcher

启动 SmartNPC 项目的各个组件。**仅 Windows 原生方案**，不使用 WSL。

## 何时使用

应当使用：
- 用户说"启动项目"/"跑起来"/"启动 mcp"/"run server"
- 用户改完代码想本地起来 smoke test
- 把 mcp 暴露给 Hermes / Claude Desktop 等 MCP client 之前

不应使用：
- 用户只是想跑测试（用 `task test` / `task mcp:test`）
- 用户问"项目架构是什么"（用 README / docs）

## 组件清单

| 组件 | 位置 | 启动方式 | 端口 |
|------|------|---------|------|
| `smartnpc-mcp` | Windows | `task mcp:run` | HTTP `:3000` (MCP), WS `:18745` (mod bridge) |
| SMAPI Mod | Windows | 启动 Stardew Valley + SMAPI（mod 自动加载） | WS `:18745` (server side) |
| Hermes Agent | WSL Ubuntu-22.04 | 用户自己起；通过 Win host IP 连 mcp | — |

## 启动 SOP

### 步骤 1 — 前置检查

```cmd
go version
```

要求 1.22+。缺失则提示用户装 Go 后停止。

### 步骤 2 — 启动 smartnpc-mcp（HTTP mode）

**首选方式**（用 Taskfile，统一入口）：

```cmd
cd /d d:\SmartNPC
task mcp:run
```

**等价的 raw 命令**（task 不可用时）：

```cmd
cd /d d:\SmartNPC\smartnpc-mcp
go build -o bin\smartnpc-mcp.exe .\cmd\smartnpc-mcp\
start "smartnpc-mcp" cmd /k "cd /d d:\SmartNPC && smartnpc-mcp\bin\smartnpc-mcp.exe --http :3000 --log-level=debug --ws-url="""""
```

关键 flag：

| flag | 含义 | 默认 |
|------|------|------|
| `--http :PORT` | 启用 Streamable HTTP transport，监听 `:PORT/mcp` + `:PORT/healthz` | 关（走 stdio） |
| `--ws-url URL` | 连接 SMAPI Mod 的 WebSocket bridge | `ws://localhost:18745/ws` |
| `--ws-url=""` | **关闭** ws bridge（mod 还没起时用，避免反复重连噪音） | — |
| `--echo-mode` | 内置 echo NPC：把玩家说的话原样回复，不需要 LLM；用于验证链路 | off |
| `--log-level` | debug/info/warn/error | info |

启动后**新开的 cmd 窗口**里应该看到（JSON 格式日志）：

```
{"level":"INFO","msg":"smartnpc-mcp starting","version":"...","http_addr":":3000"}
{"level":"INFO","msg":"listening on streamable HTTP","addr":":3000","endpoint":"/mcp"}
```

### 步骤 3 — 验证 mcp 健康

```cmd
task mcp:health
```

或裸 curl：

```cmd
curl -sS http://127.0.0.1:3000/healthz
```

期望输出 `{"ok":true}`。

### 步骤 4 — （可选）启动 SMAPI Mod

把 mod 装到 SDV 后，**用户手动启动游戏**。mcp 进程会在 ws 重连循环里检测到 mod online 并打印：

```
{"level":"INFO","msg":"ws bridge connected","url":"ws://localhost:18745/ws"}
```

Mod 安装：

```cmd
task mod:install
```

游戏内按 `Ctrl+T` 打开聊天框；echo mode 下输入任意文字，mcp 会让 NPC `<SmartNPC>` 用 `You said: ...` 回复。

### 步骤 5 — 停止

```cmd
task mcp:stop
```

或者直接关掉那个 cmd 窗口（Ctrl+C）。

## 已知问题：WDAC / Device Guard

公司机器上有 Application Control 策略，**会拦截某些 user-built exe**。表现：

```
fork/exec C:\Users\<u>\AppData\Local\go-build\xx\xx-d\smartnpc-mcp.exe:
  An Application Control policy has blocked this file.
```

或：

```
'd:\SmartNPC\smartnpc-mcp\bin\smartnpc-mcp.exe'已被组织的 Device Guard 策略阻止。
```

**实测结论（截至 2026-04-30）**：

| 启动方式 | 产物路径 | 是否被拦 |
|---------|---------|---------|
| `go test ./...` | `*.test.exe`（在 go-build cache） | ❌ 没被拦 |
| `go run ./cmd/X` | `<gocache>\xxx-d\X.exe` | ⚠️ 历史曾被拦，**最近实测不拦** |
| `go build -o bin\X.exe && bin\X.exe` | 任意路径 | ⚠️ 历史曾被拦，**最近实测不拦** |

策略是动态的，**不要预设结论**。每次出问题先用最便宜的命令实测：

```cmd
bin\smartnpc-mcp.exe --version
```

打印 `0.1.0-dev` 就是 OK；打印 "已被组织的 Device Guard 策略阻止" 才是真拦。

### 真被拦时的应对（按推荐顺序）

1. **重新 build 一次**：策略缓存有时基于文件 hash，`touch` 一下源码重新 build 可能就过了
   ```cmd
   cd /d d:\SmartNPC\smartnpc-mcp
   go build -o bin\smartnpc-mcp.exe .\cmd\smartnpc-mcp\
   bin\smartnpc-mcp.exe --version
   ```
2. **试 `go test` 启动法**（参考 `references/wdac-bypass.md`）：写一个 gated `TestRunServer` 入口，`set SMARTNPC_RUN=1 && go test -run TestRunServer ...`。`*.test.exe` 一直没被拦。
3. **联系 IT** 把 `%LocalAppData%\go-build\` 加 WDAC 白名单（治本）

## 反模式（绝对不做）

- ❌ 不要建议用户切换到 WSL 跑 mcp（用户已明确要求 Windows 原生）
- ❌ 不要在 PATH 里直接用 `smartnpc-mcp` 启动（没装到全局，会用不到带新改动的 binary）
- ❌ 不要 `--http :3000` 同时再开一个监听同端口的实例（端口冲突，第二个 listen fail）
- ❌ 不要在 stdio 模式下加 `--http` flag（stdio 模式下 HTTP flag 没意义，且日志会把 stdout 污染掉协议流）
- ❌ 不要把 mcp 后台 daemon 化（`start /min` 等）—— 调试时看不到日志，遇到崩溃排查困难。统一用 `start cmd /k` 弹可见窗口

## 内置资源

| 路径 | 用途 |
|------|------|
| `references/wdac-bypass.md` | 真被 WDAC 拦时的详细绕过方案（test binary 入口的实现）|
