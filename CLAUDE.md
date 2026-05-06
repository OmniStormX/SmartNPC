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
# 验证: curl http://localhost:8642/health
```

**启动游戏：** 必须通过 `D:\Stardew Valley\StardewModdingAPI.exe`（不能直接用 `Stardew Valley.exe`）

**Taskfile 约定：** 所有构建/测试/lint 统一 `task <name>`；子项目用 `task <ns>:<task>`；禁止裸 `go build`/`dotnet build`

## 架构与模块边界

**Go Workspace** — 根 `go.work` 联动：
- `github.com/smartnpc/smartnpc-mcp`
- `github.com/smartnpc/smartnpc-agent`

**边界原则：**
- C# 只放 SMAPI 胶水（事件、Harmony patch、ws 编解码）；业务逻辑在 Go
- `smartnpc-mcp` 不持久化状态，只做协议桥
- `smartnpc-agent` 通过 stdio spawn mcp；不引入 HTTP 直连
- LLM Provider 固定 OpenAI 兼容优先（当前用 Hermes gateway `192.168.59.118:8642`）

**关键目录：**
- `smartnpc-mcp/internal/tools/` — MCP 工具实现，按 domain 一文件
- `smartnpc-mcp/internal/bridge/` — ws 客户端 + mock
- `smartnpc-agent/internal/agent/` — NPC agent loop 核心
- `smartnpc-agent/internal/llm/` — LLM provider 抽象 + OpenAI 实现
- `smartnpc-agent/personas/` — NPC 人格模板（如 `xiami_soul.md`）
- `smapi-mod/Bridge/` — C# 侧 ws server + 协议 DTO
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
- 已实现：`chat_say`、`mail_send`、`chat_received` event

## 代码规范

### Go

- Go 1.22+；可用 `log/slog`、泛型、`for range int`
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

## 当前里程碑

| Milestone | 状态 |
|-----------|------|
| M1 Go workspace + MCP ping + agent CLI | ✅ |
| M1.5 Taskfile + GitHub Actions CI/Release | ✅ |
| M2 SMAPI Mod + WebSocket 桥接 | ✅ |
| M3 NPC 行为工具集（20+ 工具） | ⬜ |
| M4 OpenAI provider + 单 NPC agent loop | 🔧 进行中 |
| M5 SQLite 记忆 + 调度 + 多 NPC 编排 | ⬜ |

每个 milestone 完成后等用户验证再进入下一个。
