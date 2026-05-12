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

**正式链路（Hermes-first，M5 起）：**
```
SMAPI Mod (C# .NET 6) ──ws :18745── smartnpc-mcp (Go, --http :3000) ──MCP HTTP── 6× Hermes Agent Profile
                                          │                              (xiami, abigail, haley, harvey, penny, sebastian)
                                          └── hermesrelay ──POST /v1/responses──> route by runtime-config.yaml
```

**调试链路（旧，smartnpc-agent 已冻结）：**
```
SMAPI Mod (C# .NET 6) ──ws :18745── smartnpc-mcp (Go) ──stdio MCP── smartnpc-agent (Go)
```

| 模块 | 职责 |
|------|------|
| `smapi-mod/` | SMAPI Mod，ws server 暴露游戏 API |
| `smartnpc-mcp/` | Go MCP Server，ws↔MCP 桥接（stdio / HTTP） |
| `smartnpc-agent/` | Go NPC 编排器，spawn mcp 子进程，LLM 驱动对话 |

## Windows 环境

- Shell 是 `cmd.exe`；跨盘：`d: && cd d:\path && <cmd>`；禁止 PowerShell 语法
- Go：`GOROOT=C:\Program Files\Go`，`GOPATH=C:\Users\synchen\go`
- Task 路径：`C:\Users\synchen\go\bin\task.exe`
- WDAC：不要预设拦截，实测 `bin\xxx.exe --version`；截至 2026-04-30 不再拦截
- 编码：UTF-8 无 BOM；写中文文件后自检 `python -c "open(r'PATH','rb').read().decode('utf-8')"`

## 常用命令

```cmd
C:\Users\synchen\go\bin\task.exe ci          # lint + test + build（完整 CI）
C:\Users\synchen\go\bin\task.exe ci-fast     # lint + test
C:\Users\synchen\go\bin\task.exe mcp:build   # 构建 mcp
C:\Users\synchen\go\bin\task.exe agent:build # 构建 agent
C:\Users\synchen\go\bin\task.exe mod:install # 编译+部署 mod 到游戏目录
C:\Users\synchen\go\bin\task.exe tidy        # go mod tidy 全模块
C:\Users\synchen\go\bin\task.exe --list      # 列出所有可用 task
```

**运行单个测试：**
```cmd
cd D:\SmartNPC\smartnpc-mcp && go test -run TestPing ./internal/tools/...
cd D:\SmartNPC\smartnpc-agent && go test -run TestAgent_RunLoop ./internal/agent/...
```

**启动 agent 对话模式：**
```cmd
cd D:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe --mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe ^
  --mcp-args "--ws-url ws://127.0.0.1:18745/ws" --log-level debug run --api-key smartnpc-test-key
```
注意：agent 会 spawn mcp 子进程连 ws，不要同时跑独立的 `task mcp:run`，mod 只接受一个 ws 客户端。

**启动 LLM 后端（WSL 终端）：**
```bash
hermes gateway run --accept-hooks
# WSL 内验证: curl http://localhost:8642/health
# Windows 侧通过 WSL IP（`wsl hostname -I`）访问，通常是 192.168.59.118:8642
# 实际地址以 .env 中 OPENAI_BASE_URL 为准
```

**启动游戏：** 必须通过 `D:\Stardew Valley\StardewModdingAPI.exe`（不能直接用 `Stardew Valley.exe`）

**一键启动（推荐）：** 仓库根 `run.bat` 自动 build mod + 起 hermes + 拉起多 NPC agent。日常调试优先走它，避免漏步骤。

**启动 MCP HTTP 模式（Hermes-first 链路）：**
```cmd
cd D:\SmartNPC\smartnpc-mcp
bin\smartnpc-mcp.exe --http :3000 --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
  --log-level debug
```
此模式下 mcp 同时暴露 Streamable HTTP 给 Hermes 做 MCP 客户端，并通过 hermesrelay 将游戏事件转发给 Hermes Gateway。

**Taskfile 约定：** 所有构建/测试/lint 统一 `task <name>`；子项目用 `task <ns>:<task>`；禁止裸 `go build`/`dotnet build`

**环境变量（`.env.example`）：** 复制 `.env.example` 为 `.env` 后编辑；Taskfile 的 `dotenv:` 自动加载。关键变量：
- `SMARTNPC_WS_URL` — ws 地址（默认 `ws://127.0.0.1:18745/ws`）
- `SMARTNPC_HTTP_PORT` — mcp HTTP 端口（默认 `3000`）
- `OPENAI_BASE_URL` / `OPENAI_API_KEY` / `OPENAI_MODEL` — LLM 配置
- `SMARTNPC_GAME_PATH` — SDV 安装目录（空则自动探测）

## 架构与模块边界

**Go Workspace** — 根 `go.work` 联动：
- `github.com/smartnpc/smartnpc-mcp`
- `github.com/smartnpc/smartnpc-agent`

**边界原则：**
- C# 只放 SMAPI 胶水（事件、Harmony patch、ws 编解码）；业务逻辑在 Go
- `smartnpc-mcp` 不持久化状态，只做协议桥
- `smartnpc-agent` 通过 stdio spawn mcp；不引入 HTTP 直连
- LLM Provider 固定 OpenAI 兼容优先（当前用 Hermes gateway，地址以 `.env` `OPENAI_BASE_URL` 为准；所有环境变量见 `.env.example`）
- **Dual-LLM 架构**（旧链路）：dialogue model（对话）与 router model（工具/路由）分离，详见 `smartnpc-agent/internal/agent/`
- **Hermes-first 架构**（M5 起）：smartnpc-mcp 直连 Hermes Agent，NPC 人格/记忆/技能/决策全部由 Hermes profile 承载
- **多 NPC 编排**：单 agent 进程承载多个 NPC persona，通过 router 决定哪个 NPC 响应；persona 在 `personas/*.md`

**关键目录：**
- `smartnpc-mcp/internal/tools/` — MCP 工具实现，按 domain 一文件（chat / game_query / mail / npc_behavior / npc_movement / npc_perception / npc_message / player_query / meta）
- `smartnpc-mcp/internal/bridge/` — ws 客户端 + mock
- `smartnpc-mcp/internal/events/` — 游戏事件 typed structs（`ChatMessage` / `NpcInteract` 等）+ format 工具
- `smartnpc-mcp/internal/hermesrelay/` — 游戏事件 → Hermes Gateway HTTP POST 转发层
- `smartnpc-agent/internal/agent/` — NPC agent loop 核心（含 dual-LLM、多 NPC router）⚠️ **已冻结**
- `smartnpc-agent/internal/llm/` — LLM provider 抽象 + OpenAI 实现 ⚠️ **已冻结**
- `smartnpc-agent/personas/` — NPC 人格模板（多 NPC，如 `xiami_soul.md` 等）
- `hermes/profiles/xiami/` — Hermes NPC profile（`SOUL.md` + `config-overlay.yaml` + `skills/`）
- `smapi-mod/Bridge/` — C# 侧 ws server + 协议 DTO
- `smapi-mod/NPC/` — 多 NPC 路由（`AudibleNPCResolver.cs` + `TurnQueue.cs`）
- `smapi-mod/{Query,Perception,Movement,Mail,Chat,UI}/` — 按 domain 拆分的游戏侧 handler
- `smapi-mod/assets/xiami/` — NPC sprite 资产 + 构建脚本

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
- 已实现 actions：`chat_say` / `mail_send` / `game_*` / `friendship_get` / `npc_*` / `player_get_status`
- 已实现 events：`chat_message` / `chat_received` / `npc_interact` / `group_create`（详见 `docs/events.md`）

## 代码规范

### Go

- Go 1.25+（`go.mod` 声明 1.25.0）；可用 `log/slog`、泛型、`for range int`
- **stdio MCP server 日志全走 stderr**，禁止 `fmt.Println`/`log` 默认输出
- 错误：`fmt.Errorf("...: %w", err)`
- 包注释和导出符号用英文；TODO 注明 milestone

### MCP 工具

- 命名：`<domain>_<verb>` 全小写下划线
- 一个 domain 一个文件；`registry.go` → `RegisterAll` 统一注册
- Input/Output struct 必须有 `json` + `jsonschema` tag；Output 首字段 `OK bool`
- Handler 第一返回值传 `nil`，让 SDK 用 Output 填充
- 新增工具必须同步 `docs/protocol.md`

### 测试纪律

- 新增 Go package 必须有 `*_test.go`
- 新增 MCP 工具必须配 `InMemoryTransport` 端到端测试
- 测试命名：`Test<Func>_<Scenario>`；表驱动 + `t.Run`
- 禁止 sleep > 100ms；禁止真实 MCP 子进程或真实 ws 连接
- **改完代码必须跑 `task ci`；失败不能说"完成了"；3 次修不好停下来问用户**

## Git 提交规范

**格式：** `<type>(<scope>): <subject>`
- type: `feat`/`fix`/`refactor`/`test`/`docs`/`chore`/`ci`/`build`
- scope: `mcp`/`agent`/`mod`/`docs`/`ci`/`tools`/`bridge`
- subject: 祈使句、小写、≤60 字

**流程：**
- commit 前必须 `task ci` 通过
- 用户本人 git 身份 + trailer：`Co-Authored-By: Claude <noreply@anthropic.com>`
- 禁止 `--force` 到主分支、`--no-verify`、`[skip ci]`
- 不主动 commit（除非用户明确要求）

**发版：** push semver tag → `release.yml` 自动构建 windows/linux Go 二进制 + GitHub Release
```cmd
git tag v0.x.0 && git push origin v0.x.0
```

## CI 反馈循环

用户说"看 CI"/"check ci" 时：

1. `python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1`
2. SUCCESS → 一句话汇报
3. FAILURE → `gh run view <runId> --log-failed` → 归类（compile/test/lint/dependency/environment/workflow/flake）→ 汇报 + 修复
4. 修复后 `task ci` → commit → push → 循环
5. 3 次失败停止，汇总给用户

禁止：盲猜（必须看日志）、改 workflow 掩盖、`[skip ci]`/`continue-on-error`

> ⚠️ 注意：CI `ci.yml` 中 `GO_VERSION: '1.22'` 滞后于 `go.mod` 声明的 `go 1.25.0`，可能需要更新。

## 当前里程碑

| Milestone | 状态 |
|-----------|------|
| M1 Go workspace + MCP ping + agent CLI | ✅ |
| M1.5 Taskfile + GitHub Actions CI/Release | ✅ |
| M2 SMAPI Mod + WebSocket 桥接 | ✅ |
| M3 NPC 行为工具集（query/perception/movement/mail/chat/behavior） | ✅ |
| M4 OpenAI provider + 单 NPC agent loop + dual-LLM + 多 NPC router | ✅ |
| M5 (旧) SQLite 记忆 + 委派 + proactive + group chat + QQ-style UI（commit `b56d439`）| ⛔ **冻结**，重定向 |
| M5 (Hermes-first) smartnpc-mcp 强化 + Hermes profile per NPC，smartnpc-agent 退出主链路 | ✅ 代码 + 6 NPC 配置就绪 — 待实机端到端验证（默认起 xiami + abigail） |

每个 milestone 完成后等用户验证再进入下一个。

## M5 (Hermes-first) 落地清单

| # | 内容 | 关键产物 |
|---|------|---------|
| 5.0 / 5.0b | 冻结声明 + Hermes 触发方案锁定 B | `REFACTOR.md` / `docs/hermes-event-trigger.md` |
| 5.1 | smartnpc-mcp `--http :3000` Streamable HTTP | `cmd/smartnpc-mcp/main.go::runHTTP` |
| 5.2 | tool description 操作手册化（when to call / side-effect） | `smartnpc-mcp/internal/tools/*.go` |
| 5.3 | inter-NPC 工具 `npc_send_message` / `_broadcast_event` / `_inbox_*` | `internal/tools/npc_message.go` |
| 5.4 | 事件 payload 规范化（typed structs + reserved schemas） | `internal/events/` + `docs/events.md` |
| 5.5 | `hermes/profiles/xiami/`（SOUL.md + skill + overlay） | `hermes/` + `hermes/install.sh` |
| 5.8 | hermesrelay outbound HTTP → Hermes Gateway | `internal/hermesrelay/` |
| 5.9 / 5.10 / 5.11 | proactive-greeting + memory-policy + cron recipes | `hermes/profiles/xiami/skills/...` + `cron-recipes.md` |
| 5.12 | SMAPI mod 多 NPC 路由（AudibleNPCResolver + TurnQueue） | `smapi-mod/NPC/AudibleNPCResolver.cs` + `TurnQueue.cs` |
| 5.13 | smartnpc-agent 降级 dev harness | `smartnpc-agent/README.md` |
| 5.14 | 文档拆分（architecture / hermes-profiles / mcp-tools / migration） | `docs/` |

**待验证（5.6 / 5.7）**：实机跑通"玩家聊天 → Hermes → chat_say"和"问时间 → Hermes 自动调 game_get_time"。代码全部就绪，pipeline 集成测试已有；需要游戏 + Hermes gateway + mcp HTTP 模式同时跑一次完整 happy path。

## smartnpc-agent 冻结声明（2026-05-11 起）

详见 [REFACTOR.md](REFACTOR.md) + [docs/roadmap.md](docs/roadmap.md) M5(Hermes-first) 章节。

- `smartnpc-agent/` **不再接受新功能**：不加 memory / scheduler / multi-NPC pool / event router
- `internal/agent/`、`internal/llm/`、`personas/` 仅作为回归对照保留
- M5 旧实现（commit `b56d439`）作为"旧路线对照"封存，不再扩展
- 仅允许：bug fix、依赖升级、为对照测试服务的最小改动
- 新功能一律走 **smartnpc-mcp 强化 + Hermes profile** 路径

如果接到"往 smartnpc-agent 加 X"的请求，先把请求映射到 Hermes profile / skill / MCP tool，再回报用户确认；不要直接动 Go agent 代码。
