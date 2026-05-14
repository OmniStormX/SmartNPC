---
name: project-launcher
description: 当用户要求启动、运行、跑起来、launch、start SmartNPC 项目的某个组件（smartnpc-mcp / SMAPI Mod / Hermes / 整个项目）时使用此 skill。触发词包括"启动项目"、"跑一下 mcp"、"启动 mcp"、"run server"、"项目跑起来"、"launch"、"启动游戏 mod" 等。提供一套 Windows 原生的启动 SOP（Hermes 跑在 WSL 内）。
---

# Project Launcher

启动 SmartNPC 项目的各个组件。架构：**smapi-mod (Win) ↔ smartnpc-mcp (Win) ↔ Hermes Gateways (WSL)**。

## 何时使用

应当使用：
- 用户说"启动项目"/"跑起来"/"启动 mcp"/"run server"
- 用户改完代码想本地起来 smoke test

不应使用：
- 用户只是想跑测试（用 `task test`）
- 用户问"项目架构是什么"

## 组件清单与启动顺序

```
1. SMAPI Mod (Stardew Valley) → 游戏 API 端口 ws :18745
2. smartnpc-mcp (Windows)     → --http :3000 + 连 ws :18745 + hermesrelay 转发
3. Hermes Gateways (WSL)      → 每个 NPC 一个端口（:8642-:8647）
4. 启动游戏                    → SMAPI mod 自动加载
```

⚠️ **关键约束**：mod 的 ws 端口 `:18745` 只接受**一个**客户端。不要起多个 mcp。

> **首选一键脚本**：`run.bat` 串联所有步骤（默认起 xiami + abigail）。下面只在 debug 单组件时用。

## 启动 SOP

### 步骤 1 — 前置检查

```cmd
go version
smartnpc-mcp\bin\smartnpc-mcp.exe --version
```

缺二进制就先 build：
```cmd
"%USERPROFILE%\go\bin\task.exe" build
```

### 步骤 2 — 启动 smartnpc-mcp（Windows）

```cmd
smartnpc-mcp\bin\smartnpc-mcp.exe ^
  --http :3000 ^
  --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
  --hermes-api-key smartnpc-test-key ^
  --log-level debug
```

验证：`curl http://127.0.0.1:3000/healthz` → `{"ok":true}`

### 步骤 3 — 启动 Hermes Gateways（WSL Ubuntu-22.04）

```bash
bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh xiami,abigail
```

验证（Windows 侧）：
```cmd
curl -sS http://192.168.59.118:8642/health
curl -sS http://192.168.59.118:8643/health
```

### 步骤 4 — 启动 Stardew Valley（SMAPI）

```cmd
"D:\Stardew Valley\StardewModdingAPI.exe"
```

Mod 安装（如需更新）：
```cmd
"%USERPROFILE%\go\bin\task.exe" mod:install
```

> 注意：游戏运行时 DLL 被锁，需**关游戏后**再 install。

### 步骤 5 — 停止

- mcp：在 cmd 窗口按 `Ctrl+C`
- Hermes：WSL 终端按 `Ctrl+C`
- 游戏：正常退出

## 常见问题

| 现象 | 原因 | 解决 |
|------|------|------|
| ws 反复 connect/disconnect | 多个 mcp 抢 `:18745` | kill 多余的 `smartnpc-mcp.exe` |
| Hermes health 不通 | gateway 没启动 / 防火墙 | 确认 WSL 里 `hermes gateway run` 在跑 |
| Hermes 调不到 mcp 工具 | mcp 启动晚于 Hermes | 先起 mcp 再起 Hermes |
| DLL 被锁无法 install | 游戏正在运行 | 先关游戏再 `task mod:install` |
| WDAC 拦截 exe | 公司策略 | 参考 `references/wdac-bypass.md` |

## 内置资源

| 路径 | 用途 |
|------|------|
| `references/wdac-bypass.md` | WDAC 绕过方案 |
