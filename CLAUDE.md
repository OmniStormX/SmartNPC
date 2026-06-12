# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 回复风格

- 中文回复；技术术语、API 名、文件路径保留英文 + 反引号
- 优先给可执行方案，不堆砌选项；多方案对比用表格
- 不在结尾加客套；不在代码里加 narration 注释
- 工具调用并行优先：能并发的 read/search/lint 一次性发出
- 改动完成后给 1-3 行小结 + 关键路径，不贴代码

## 项目概览

SmartNPC — 星露谷物语 AI NPC 系统，基于 MCP 构建：

> 本文件面向 Claude Code。其他 AI 工具入口：[AGENTS.md](AGENTS.md)（agent 工作流 SOP）、[CODEBUDDY.md](CODEBUDDY.md)（CodeBuddy 视角）。

**正式链路（Hermes-first）：**
```
SMAPI Mod (C# .NET 6) ──ws :18745── smartnpc-mcp (Go, --http :3000) ──MCP HTTP── 6× Hermes Agent Profile
                                          │                              (xiami, abigail, haley, harvey, penny, sebastian)
                                          └── hermesrelay ──POST /v1/responses──> route by runtime-config.yaml
```

| 模块 | 职责 |
|------|------|
| `smapi-mod/` | SMAPI Mod，ws server 暴露游戏 API |
| `smartnpc-mcp/` | Go MCP Server + agent-bridge 框架；SDV 为首个 adapter |
| `hermes/profiles/` | 6 个 NPC profile（SOUL + skills + cron） |

> `smartnpc-mcp` 经 ADR-0004 重构后分为 `pkg/`（game-agnostic 框架）+ `adapters/stardew/`（SDV 适配层）。日常 SDV 开发继续用 `cmd/smartnpc-mcp`；通用部署用 `cmd/agent-bridge`（bridge.yaml 驱动）。

## Windows 环境

- Shell 是 PowerShell；跨盘：`cd D:\path; <cmd>`
- Go：`GOROOT=C:\Program Files\Go`，`GOPATH=C:\Users\synchen\go`
- Task 路径：`C:\Users\synchen\go\bin\task.exe`
- WDAC：不要预设拦截，实测 `bin\xxx.exe --version`；截至 2026-04-30 不再拦截
- 编码：UTF-8 无 BOM；写中文文件后自检 `python -c "open(r'PATH','rb').read().decode('utf-8')"`
- .NET：`global.json` 钉 SDK 8.0.0（C# Dev Kit 需要 ≥8），mod 实际 target `net6.0`

## 常用命令

```powershell
C:\Users\synchen\go\bin\task.exe ci              # profiles:verify + lint + test + build（完整 CI）
C:\Users\synchen\go\bin\task.exe ci-fast         # profiles:verify + lint + test
C:\Users\synchen\go\bin\task.exe mcp:build       # 构建 mcp
C:\Users\synchen\go\bin\task.exe mcp:test-race   # 带 -race 的测试
C:\Users\synchen\go\bin\task.exe mcp:stop        # 杀掉本机运行中的 mcp 进程
C:\Users\synchen\go\bin\task.exe mcp:health      # 探测 /healthz（确认 mcp 在线）
C:\Users\synchen\go\bin\task.exe mod:install     # 编译+部署 mod 到游戏目录
C:\Users\synchen\go\bin\task.exe tidy            # go mod tidy 全模块
C:\Users\synchen\go\bin\task.exe profiles:render # 渲染 _master → 6 NPC profile
C:\Users\synchen\go\bin\task.exe profiles:verify # 校验渲染产物（CI 会跑）
C:\Users\synchen\go\bin\task.exe net:check       # 探测 LLM + Langfuse 连通性（WSL 侧，不需游戏/mcp）
C:\Users\synchen\go\bin\task.exe hooks:enable    # 启用 git hooks（commit smapi-mod/ 时自动 mod:install）
C:\Users\synchen\go\bin\task.exe hooks:status    # 查看 hooks 是否启用
C:\Users\synchen\go\bin\task.exe --list          # 列出所有可用 task
```

**运行单个测试：**
```powershell
cd D:\SmartNPC\smartnpc-mcp; go test -run TestPing ./pkg/agentbridge/...
cd D:\SmartNPC\smartnpc-mcp; go test -run TestChatSay ./adapters/stardew/tools/...
```

**Echo 模式（不接 LLM，验证游戏往返）：** 请求直接回声返回，用于验证 ws 连接 + MCP 工具注册是否正常，无需 Hermes/LLM 在线。
```powershell
C:\Users\synchen\go\bin\task.exe mcp:run-echo
```

**启动 LLM 后端（WSL 终端）：**
```bash
hermes gateway run --accept-hooks
# WSL 内验证: curl http://localhost:8642/health
# Windows 侧通过 WSL IP（`wsl hostname -I`）访问，通常是 192.168.59.118:8642
# 实际地址以 .env 中 OPENAI_BASE_URL 为准
```

**启动游戏：** 必须通过 `D:\Stardew Valley\StardewModdingAPI.exe`（不能直接用 `Stardew Valley.exe`）

**一键启动（推荐）：** 仓库根 `run.bat` 自动 build mod + 起 hermes + 启动 mcp HTTP。日常调试优先走它，避免漏步骤。

> ⚠️ `run.bat` 必须 CRLF 行尾。改完后自检：
> ```powershell
> $b = [IO.File]::ReadAllBytes('D:\SmartNPC\run.bat'); $b[9..10] -join ','
> ```
> 第 9 字节应为 `13`（CR），不是 `10`（LF）。若是 LF 立即转 CRLF：
> ```powershell
> $p = 'D:\SmartNPC\run.bat'; $t = [IO.File]::ReadAllText($p); $t = $t -replace "`r`n", "`n" -replace "`n", "`r`n"; [IO.File]::WriteAllText($p, $t, [Text.UTF8Encoding]::new($false))
> ```

**启动 MCP HTTP 模式（Hermes-first 链路）：**
```powershell
cd D:\SmartNPC\smartnpc-mcp
bin\smartnpc-mcp.exe --http :3000 --ws-url ws://127.0.0.1:18745/ws `
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml `
  --log-level debug
```
此模式下 mcp 同时暴露 Streamable HTTP 给 Hermes 做 MCP 客户端，并通过 hermesrelay 将游戏事件转发给 Hermes Gateway。

**Taskfile 约定：** 所有构建/测试/lint 统一 `task <name>`；子项目用 `task <ns>:<task>`；禁止裸 `go build`/`dotnet build`

**环境变量（`.env.example`）：** 复制 `.env.example` 为 `.env` 后编辑；Taskfile 的 `dotenv:` 自动加载。变量按优先级分 4 档：`[必看]`（必须确认）、`[按机器]`（非默认时设）、`[自动]`（有合理 fallback）、`[排障]`（出问题才开）。关键变量：
- `SMARTNPC_WS_URL` — ws 地址（默认 `ws://127.0.0.1:18745/ws`）
- `SMARTNPC_HTTP_PORT` — mcp HTTP 端口（默认 `3000`）
- `SMARTNPC_MCP_URL` — Hermes profile 连 mcp 的完整 URL（必须显式设，如 `http://127.0.0.1:3000/mcp`）
- `SMARTNPC_HERMES_MODE` — `docker`（默认）或 `local`
- `SMARTNPC_ACTIVE_PROFILES` — 启动哪些 NPC（默认全部 6 个，精简如 `xiami,abigail`）
- `SMARTNPC_GAME_PATH` — SDV 安装目录（空则自动探测）

**启动顺序约束：**
1. 关闭游戏后再 `task mod:install`（DLL 可能被锁）
2. MCP 先于 Hermes Gateway 启动（Hermes 启动时发现工具，晚启动会缓存到 0 tools）
3. 同一 WebSocket 只能一个 MCP 实例占用

**Git hooks（可选）：** `task hooks:enable` — commit 涉及 `smapi-mod/` 时自动 `task mod:install`；`task hooks:disable` 关闭

**游戏内按键：**

| 按键 | 功能 |
|------|------|
| `Tab` | 打开/关闭 SmartNPC 聊天面板 |
| `F2` | 打开面板并聚焦联系人列表 |
| `Ctrl+T` | 原版聊天输入（按距离路由给附近 NPC） |
| `Esc` | 关闭聊天面板 |
| `F3` | 调试面板 |
| 点击 Agent NPC | 拦截原版对话，打开对应 NPC 聊天窗口 |

## 架构与模块边界

**Go Workspace** — 根 `go.work` 联动单模块 `./smartnpc-mcp`（module path `github.com/OmniStormX/SmartNPC`）

**边界原则：**
- C# 只放 SMAPI 胶水（事件、Harmony patch、ws 编解码）；业务逻辑在 Go
- `smartnpc-mcp` 不持久化状态，只做协议桥
- LLM Provider 固定 OpenAI 兼容（Hermes Gateway 暴露 `/v1/responses`）；地址以 `.env` `OPENAI_BASE_URL` 为准
- **Hermes-first 架构**：smartnpc-mcp 直连 Hermes Agent，NPC 人格/记忆/技能/决策全部由 Hermes profile 承载

**agent-bridge 框架分层（ADR-0004）：**

```
smartnpc-mcp/
├── pkg/                          ← 公共框架 API（game-agnostic）
│   ├── agentbridge/              ← Server, EventSource, Backend, ToolGroup, registry
│   ├── eventbus/                 ← 通用 Event{Kind, Source, Subject, Payload, Timestamp}
│   ├── transport/                ← MCP transport（HTTP today）
│   └── relay/
│       ├── hermes/               ← Hermes Gateway Backend（转发 + 路由）
│       └── echo/                 ← dev/smoke 回声 Backend
├── adapters/stardew/             ← SDV 适配层（game-specific）
│   ├── adapter.go                ← factory 注册 + EventSource 实现
│   ├── bridge/                   ← ws 客户端 + mock + protocol DTO
│   ├── events/                   ← SDV 事件 typed structs + Hermes prompt format
│   └── tools/                    ← MCP 工具按 domain 一文件（chat / game_query / mail / npc_* / player_query）
├── cmd/
│   ├── smartnpc-mcp/             ← 日常 SDV 启动入口（run.bat 调用）
│   └── agent-bridge/             ← 通用 CLI，bridge.yaml 驱动
└── internal/log/                 ← 框架私有 logger
```

**关键目录（详细）：**
- `smartnpc-mcp/pkg/agentbridge/` — 组合根 `Server`、`EventSource`/`Backend` 接口、`ToolGroup` 注册、`meta.go`（ping tool）
- `smartnpc-mcp/pkg/relay/hermes/` — Hermes Gateway Backend：runtime-config 加载、事件路由、group 逻辑
- `smartnpc-mcp/adapters/stardew/tools/` — MCP 工具实现（chat / game_query / mail / npc_behavior / npc_movement / npc_perception / npc_message / player_query）；`registry.go` → `RegisterAll`
- `smartnpc-mcp/adapters/stardew/bridge/` — ws 客户端 + testserver mock
- `smartnpc-mcp/adapters/stardew/events/` — 游戏事件 typed structs（`ChatMessage` / `NpcInteract` 等）+ format 工具
- `hermes/npcs.yaml` — **NPC 元数据唯一真相源**（id / game_name / display_name / gateway_port / peer 关系）。增删 NPC 必须从这个文件开始，`render_profiles.sh` 和 `runtime-config.yaml` 都从它生成。
- `hermes/profiles/_master/` — **共享 SKILL 模板母本**（`config-overlay.yaml` + `cron-recipes.md` + `critical-policy.md` + `skills/`，不含 `SOUL.md`）。通过 `scripts/render_profiles.sh` 用 `{{NPC_NAME}}` 等 8 个占位符渲染到 6 个 NPC 目录。**不要直接编辑非 `_master/` 下的渲染产物——会被 render 覆盖。** 详见 [ADR-0003](docs/adr/0003-npc-name-placeholder-cloning.md)。Skills：`smartnpc-core` / `smartnpc-gift` / `smartnpc-greeting` / `smartnpc-group-chat` / `smartnpc-inter-npc` / `smartnpc-memory` / `smartnpc-schedule` / `smartnpc-visit`。
- `hermes/profiles/<npc>/` — 单个 NPC profile。`SOUL.md` 手写保留，`critical-policy.md` 手写保留，其余由 `_master/` 渲染生成。6 个 NPC：`xiami` / `abigail` / `haley` / `harvey` / `penny` / `sebastian`。
- `smapi-mod/Bridge/` — C# 侧 ws server + 协议 DTO
- `smapi-mod/NPC/` — 多 NPC 路由（`AudibleNPCResolver.cs` + `TurnQueue.cs`）
- `smapi-mod/{Query,Perception,Movement,Mail,Chat,UI}/` — 按 domain 拆分的游戏侧 handler
- `smapi-mod/Behavior/` — NPC 世界行为 handler（DepositItems / ClearDebris 等）；跨模块数据流：C# FollowSystem 触发 → ws action → MCP tool → Go handler → ws response → C# Tick 执行。**行为可实现性规划**详见 [`report-behavior.md`](report-behavior.md)（20 个 NPC 行为 + 三层优先级）。已实现：`npc_wander` / `npc_clear_debris` / `npc_deposit_items` / `npc_deliver_items` / `npc_till_soil` / `npc_approach_and_speak` / `npc_forage_collect`
- `smapi-mod/Debug/` — 游戏内调试命令入口（`smartnpc_deposit_items` 等）
- `smapi-mod/assets/xiami/` — NPC sprite 资产 + 构建脚本
- `scripts/` — Hermes 管理脚本：`render_profiles.sh`（渲染模板）、`start_hermes_profiles.sh`（启动指定 NPC gateway）、`detect_wsl_ips.sh`（自动探测 WSL/Windows IP）、`apply_hermes_tuning.sh`（调参）、`lib/npc_registry.sh`（从 `hermes/npcs.yaml` 读 NPC 列表的公共库）
- `deploy/hermes/` — Docker Compose 部署方案（Dockerfile + docker-compose.yml + Langfuse 可选）；`deploy/hermes/profiles/` 是旧版 profile 备份，日常开发以 `hermes/profiles/` 为准

## Sprite 资产管线

原始素材经 `build_spritesheet.py` 加工为 SDV 兼容的 spritesheet：

```
xiami.png (1448×1086 原始大图，棋盘格背景)
    ↓  xiami.json (tight bbox 坐标)
    ↓  build_spritesheet.py (裁切 + 缩放 + 去背 + 拼合)
XiaMi_spritesheet.png (64×416, 4列×13行, 每帧 16×32, RGBA 透明)
```

**关键约束：**
- `AnimatedSprite` 构造参数 `(textureName, startFrame, spriteWidth, spriteHeight)` 必须与 spritesheet 的帧尺寸一致（当前 16×32）
- C# 加载的是 `assets/xiami/XiaMi_spritesheet.png`，不是原始 `xiami.png`
- 修改原始素材后必须重跑 `python smapi-mod/assets/xiami/build_spritesheet.py` 再部署
- `xiami.json` 中 `frames.*.bbox` 是可见像素的紧包围盒，不是固定网格
- `crop_test.py` 可验证 bbox 裁切是否正确（输出到 `test_crops/`）

**WebSocket 协议（`docs/protocol.md`）：**
- `ws://127.0.0.1:18745/ws`，JSON 文本帧
- 消息类型：`request`（有 `id`）/ `response`（关联 `id`）/ `event`（push）
- 已实现 actions：`chat_say` / `mail_send` / `game_*` / `friendship_get` / `npc_*` / `player_get_status` / `npc_inventory_*` / `npc_deposit_items` / `npc_clear_debris`
- 已实现 events：`chat_message` / `chat_received` / `npc_interact` / `group_create`（详见 `docs/events.md`）

## Hermes 部署模式

`.env` 中 `SMARTNPC_HERMES_MODE` 控制 Hermes 启动方式：

| 模式 | 说明 |
|------|------|
| `docker`（默认） | `deploy/hermes/docker-compose.yml` 启动容器化 Hermes |
| `local` | 直接调用 WSL 本地的 `hermes` CLI（需要在 PATH 或设 `HERMES_EXE`） |

**NPC Gateway 端口**（源自 `hermes/npcs.yaml`）：xiami=8642, abigail=8643, haley=8644, harvey=8645, penny=8646, sebastian=8647

**`deploy/` 目录**包含 Docker Compose 编排、Dockerfile、profile 同步脚本、Langfuse 可选集成。日常本地开发用 `local` 模式更快；CI/远程部署走 `docker` 模式。

## 跨平台 / Linux 接入注意

Go 部分在 Windows/Linux 均可运行；C# mod 仅 Windows（依赖 SMAPI + SDV）。若 Linux 接入需注意：

| 位置 | 问题 |
|------|------|
| `smapi-mod/StardewMCPBridge.csproj` `<GamePath>` | 硬编码 Windows 本地路径，Linux 需 `<GamePath Condition="...">$(SMARTNPC_GAME_PATH)</GamePath>` |
| `smapi-mod/Taskfile.yml` `GAME_PATH`、`DOTNET` | 硬编码 `D:\...` 和 `C:\Program Files\dotnet\dotnet.exe` |
| `Taskfile.yml` hooks 子命令 | 只写了 `powershell`，Linux 需 `platforms:` 分支 |
| `smartnpc-mcp/Taskfile.yml` `mcp:run` | `cmd /c start ...` 仅 Windows |
| `smapi-mod/Taskfile.yml` `mod:install` | `powershell Copy-Item` 仅 Windows |

**集中配置：** 仓库根 `.env.example`（git 追踪）+ `.env`（`.gitignore`），根 `Taskfile.yml` `dotenv: ['.env']` 全局加载。新机器 `cp .env.example .env` 后按本机改值。

## 代码规范

### Go

- Go 1.25+（`go.mod` 声明 1.25.0）；可用 `log/slog`、泛型、`for range int`
- **stdio MCP server 日志全走 stderr**，禁止 `fmt.Println`/`log` 默认输出
- 错误：`fmt.Errorf("...: %w", err)`
- 包注释和导出符号用英文；TODO 注明 milestone

### MCP 工具（`adapters/stardew/tools/`）

- 命名：`<domain>_<verb>` 全小写下划线
- 一个 domain 一个文件；`registry.go` → `RegisterAll` 统一注册
- Input/Output struct 必须有 `json` + `jsonschema` tag；Output 首字段 `OK bool`
- Handler 第一返回值传 `nil`，让 SDK 用 Output 填充
- 新增工具必须同步 `docs/protocol.md`
- 框架层通用工具（如 `ping`）放 `pkg/agentbridge/meta.go`，不放 adapter

### 测试纪律

- 新增 Go package 必须有 `*_test.go`
- 新增 MCP 工具必须配 `InMemoryTransport` 端到端测试（参考 `adapters/stardew/tools/*_test.go`）
- 框架层测试在 `pkg/agentbridge/*_test.go`、`pkg/relay/hermes/*_test.go`
- 测试命名：`Test<Func>_<Scenario>`；表驱动 + `t.Run`
- 禁止 sleep > 100ms；禁止真实 MCP 子进程或真实 ws 连接
- **改完代码必须跑 `task ci`；失败不能说"完成了"；3 次修不好停下来问用户**

## Git 提交规范

**格式：** `<type>(<scope>): <subject>`
- type: `feat`/`fix`/`refactor`/`test`/`docs`/`chore`/`ci`/`build`
- scope: `mcp`/`mod`/`hermes`/`docs`/`ci`/`tools`/`bridge`
- subject: 祈使句、小写、≤60 字

**流程：**
- commit 前必须 `task ci` 通过
- 用户本人 git 身份 + trailer：`Co-Authored-By: Claude <noreply@anthropic.com>`
- 禁止 `--force` 到主分支、`--no-verify`、`[skip ci]`
- 不主动 commit（除非用户明确要求）

**发版：** push semver tag → `release.yml` 自动构建 windows/linux Go 二进制 + GitHub Release
```powershell
git tag v0.x.0; git push origin v0.x.0
```

## CI 反馈循环

用户说"看 CI"/"check ci" 时：

1. `python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1`
2. SUCCESS → 一句话汇报
3. FAILURE → `gh run view <runId> --log-failed` → 归类（compile/test/lint/dependency/environment/workflow/flake）→ 汇报 + 修复
4. 修复后 `task ci` → commit → push → 循环
5. 3 次失败停止，汇总给用户

禁止：盲猜（必须看日志）、改 workflow 掩盖、`[skip ci]`/`continue-on-error`

> ⚠️ 注意：CI `ci.yml` 中 `env.GO_VERSION: '1.22'` 滞后于 `go.mod` 声明的 `go 1.25.0`。若需修复，只需改 `.github/workflows/ci.yml` 顶部 `GO_VERSION` 值。

## 当前里程碑

| Milestone | 状态 |
|-----------|------|
| M1 Go workspace + MCP ping | ✅ |
| M1.5 Taskfile + GitHub Actions CI/Release | ✅ |
| M2 SMAPI Mod + WebSocket 桥接 | ✅ |
| M3 NPC 行为工具集（query/perception/movement/mail/chat/behavior） | ✅ |
| M4 OpenAI provider + 旧 Go agent loop（已删除） | ✅ → 移除 |
| M5 (Hermes-first) smartnpc-mcp 强化 + Hermes profile per NPC | ✅ 代码 + 6 NPC 配置就绪 — 待实机端到端验证（默认起 xiami + abigail） |

每个 milestone 完成后等用户验证再进入下一个。

## M5 (Hermes-first) 落地清单

全部代码已就绪；5.6 / 5.7（实机端到端）待用户验证。关键路径见上方「agent-bridge 框架分层」。
详见 [`REFACTOR.md`](REFACTOR.md)、[`docs/architecture.md`](docs/architecture.md)、[ADR-0001](docs/adr/0001-synthetic-events-go-through-hermesrelay.md)。
