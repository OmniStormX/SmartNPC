# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **注意：** 本文件内容从 `.codebuddy/rules/` 和 `.codebuddy/skills/` 导入合并而来。
> 执行 `/init` 时应自动从 `.codebuddy/` 文件夹导入最新配置到本文件。

## 回复风格

- 中文回复；技术术语、API 名、文件路径保留英文 + 反引号
- 优先给可执行方案，不堆砌选项分析；多方案对比用表格收敛
- 设计类长回复用表格、短列表，不要长 bullet 嵌套
- 不在结尾问"还需要其他帮助吗"之类的客套
- 工具调用并行优先：能并发的 read/search/lint 一次性发出
- 不要在代码里加"narration 注释"解释操作；该注释解释 why，不解释 what
- 改动完成后给一段 1-3 行的简短小结 + 关键产物路径，不要把代码贴回回复里

## 项目概览

SmartNPC 是星露谷物语（Stardew Valley）的 AI NPC 系统，基于 Model Context Protocol（MCP）构建。三层架构：

```
SMAPI Mod (C# .NET 6)  ──WebSocket :18745──  smartnpc-mcp (Go MCP Server)  ──stdio MCP──  smartnpc-agent (Go Orchestrator)
```

- **smapi-mod**：C# SMAPI Mod，嵌入 WebSocket 服务器，监听 `:18745/ws`，将游戏 API 暴露为 JSON 消息
- **smartnpc-mcp**：Go MCP Server，桥接 WebSocket ↔ MCP 工具，通过 stdio 或 HTTP 对外服务
- **smartnpc-agent**：Go NPC 编排器，spawn `smartnpc-mcp` 子进程，通过 OpenAI + MCP 驱动 NPC 人格

## Windows 环境

### Shell

- 当前 shell 是 `cmd.exe`，不是 PowerShell
- 跨盘切目录用 `d: && cd d:\path && <cmd>` 形式
- 不要用 PowerShell 专属语法（`Set-Location`、`;` 链式、`$env:` 等）
- 环境变量改动后当前 cmd 窗口 inherit 旧值；用 `cmd /d /c "..."` 起干净子进程验证

### Go 工具链

- `GOROOT=C:\Program Files\Go`，`GOPATH=C:\Users\synchen\go`
- `go.exe` 本体（在 `C:\Program Files\Go\`，签名）始终被信任
- 遇到 GOPATH/GOROOT 冲突时先查 4 个位置确认设置源头（注册表 HKCU/HKLM、`%APPDATA%\go\env`、环境变量）

### Device Guard / WDAC

- **不要预设拦截**，每次用最便宜的命令实测：`bin\smartnpc-mcp.exe --version`
- 截至 2026-04-30 实测不再拦截 user-built exe
- 若真被拦：重新 build → 试 `go test` 启动法 → 联系 IT 加白名单
- `go test` / `go vet` / `go build` 产生的 test binary 从未被拦

### 文件编码

- 所有文本文件统一 UTF-8（无 BOM）
- 写完含中文的文件后自检：`python -c "open(r'PATH','rb').read().decode('utf-8'); print('utf-8 ok')"`
- 行尾不强制 LF；Windows CRLF 也接受

## 常用命令

需先安装 [Task](https://taskfile.dev)：`go install github.com/go-task/task/v3/cmd/task@latest`

```cmd
task ci              # 完整本地 CI：lint + test + build（等价于 GitHub Actions）
task ci-fast         # 快速检查：lint + test，跳过 build
task mcp:build       # 仅构建 smartnpc-mcp
task agent:build     # 仅构建 smartnpc-agent
task mcp:test        # 仅跑 smartnpc-mcp 测试
task agent:test      # 仅跑 smartnpc-agent 测试
task mcp:test-race   # 带竞态检测跑 smartnpc-mcp 测试
task tidy            # 全部 Go 模块 go mod tidy + go work sync
task lint            # go vet 全部 Go 模块
task clean           # 删除所有构建产物
```

若 `task` 被 Device Guard 拦截，可直接用 `go test ./...` 在对应子目录下运行。

**跑单个测试：**
```cmd
cd smartnpc-mcp
go test ./internal/tools/ -run TestPing -v
```

**手工冒烟测试（需先 build mcp）：**
```cmd
cd smartnpc-agent
go run ./cmd/smartnpc-agent --mcp-bin ..\smartnpc-mcp\smartnpc-mcp.exe tools
go run ./cmd/smartnpc-agent --mcp-bin ..\smartnpc-mcp\smartnpc-mcp.exe ping --message "hi"
```

**SMAPI Mod 构建**（需本机安装 Stardew Valley + .NET 6 SDK）：
```cmd
cd smapi-mod && dotnet build -c Debug
```
CI 不构建 Mod，本地 `task ci` 才是 Mod 变更的检验源头。

**mod 自动部署 Git Hook（可选）：**
```cmd
task hooks:enable   # commit smapi-mod/ 后自动 install 到本机 Mods 目录
task hooks:disable
```

**发版：**
```cmd
git tag v0.2.0 && git push origin v0.2.0
```
GitHub Actions 自动构建 Windows + Linux 二进制并发布 Release。

### Taskfile 使用约定

- **唯一入口**：所有构建 / 测试 / lint / 发布动作必须通过 `task <name>` 触发，不直接调底层命令
- 子项目命名空间：`task <ns>:<task>`（`mcp:build`、`agent:test`、`mod:build`）
- 新增子项目时必须同步：子项目 `Taskfile.yml` + 根 `includes:` + CI path filter
- **禁止**在文档里教用户用裸 `go build` / `dotnet build`，统一指向 `task build`
- **禁止**在 CI workflow 里写裸 `go test`，统一 `task test`

## 架构与模块边界

### Go Workspace

根目录 `go.work` 联动两个独立 Go module：
- `github.com/smartnpc/smartnpc-mcp`（`smartnpc-mcp/`）
- `github.com/smartnpc/smartnpc-agent`（`smartnpc-agent/`）

M3 完成后如出现 DTO 复制，考虑抽出 `pkg/` 共享 module。

### smartnpc-mcp 内部结构

- `cmd/smartnpc-mcp/main.go`：入口，支持 `--http :PORT`（Streamable HTTP）和默认 stdio 两种传输
- `internal/bridge/`：WebSocket 客户端（`WSClient`），连接 SMAPI mod；含自动重连和挂起请求关联
- `internal/tools/`：MCP 工具注册，**一个 domain 一个文件**；`registry.go` 的 `RegisterAll` 统一注册
- `internal/log/`：slog 封装，**所有日志走 stderr**，禁止污染 stdio MCP 流

### smartnpc-agent 内部结构

- `cmd/smartnpc-agent/`：CLI 入口（`tools` / `ping` 子命令）
- `internal/mcpclient/`：对 MCP Go SDK client 的薄封装，spawn 子进程
- `internal/llm/`：LLM provider 接口 + OpenAI 实现（M4 实装）

### smapi-mod C# 结构

- `ModEntry.cs`：SMAPI 事件订阅，胶水代码
- `Bridge/WebSocketServer.cs`：单连接 WebSocket 服务器（`System.Net.HttpListener`，单客户端策略）
- `Bridge/MessageRouter.cs`：分发 ws request 到各 Handler
- `Bridge/Protocol.cs`：Request / Response / Event DTO
- `Chat/`, `Mail/`：各业务 handler

### 模块边界原则

- C# 只放 SMAPI 胶水代码（事件订阅、Harmony patch、ws 编解码）；业务逻辑全在 Go 侧
- `smartnpc-mcp` 不持久化任何业务状态，只做协议桥
- `smartnpc-agent` 通过 stdio spawn `smartnpc-mcp`；不引入 HTTP 直连方案（除非用户明确要求）
- LLM Provider 固定 OpenAI 优先；Anthropic / 其他 provider 后续再加

## WebSocket 协议（`docs/protocol.md`）

- 传输：`ws://127.0.0.1:18745/ws`，JSON 文本帧
- 三种消息类型：`request`（client→server，有 `id`）/ `response`（关联 `id`，含 `ok`/`error`）/ `event`（server→client push，含 `timestamp`）
- 已实现 action：`chat_say`（显示聊天框）、`mail_send`（HUD 消息）
- 已实现 event：`chat_received`（玩家输入）
- 服务端每 30s 发 ws ping；mod 为单连接，新客户端连入会踢掉旧连接

## 代码规范

### Go

- 目标 Go 1.22+，可用 `log/slog`、`for range int`、泛型
- **stdio MCP server 的所有日志必须走 stderr**，禁止 `fmt.Println`、禁止 `log` 包默认输出
- 错误用 `fmt.Errorf("...: %w", err)` 包装，不丢失原 err
- 包注释和导出符号注释用英文；TODO/FIXME 注明 milestone（M2/M3/...）
- 包路径前缀统一 `github.com/smartnpc/<module>`

### MCP 工具设计

- 命名格式：`<domain>_<verb>`，全小写下划线（查询类用 `get`/`list`/`find`，写入类用动词原形）
- 文件按 domain 拆分：`tools/npc_dialogue.go`、`tools/friendship.go`，不堆一个文件
- 每个工具定义独立的 `XxxInput` / `XxxOutput` struct，字段必须有 `json` 和 `jsonschema` tag
- Output 第一个字段固定为 `OK bool`
- Handler 第一个返回值（`*mcp.CallToolResult`）一律传 `nil`，让 SDK 用 Output 自动填充
- 新增工具在 `registry.go` 的 `RegisterAll` 里注册；通过传入的 `*bridge.WSClient` 调 SMAPI，不持有全局 client
- 事件通知用 MCP `notifications/message` 推送，不做成 tool
- 长耗时工具用 `req.Session.NotifyProgress` 推进度
- 新增工具时必须同步更新 `docs/protocol.md`

### 测试纪律

- **新增任何 Go package 必须同时新增 `*_test.go`**，至少一个 `Test*` 函数
- **新增 MCP 工具必须配 `InMemoryTransport` 端到端测试**（参考 `smartnpc-mcp/internal/tools/meta_test.go`）
- 涉及 SMAPI 调用的工具用 `bridge/mock.go` mock，不起真 ws 连接
- 测试命名：`Test<Function>_<Scenario>`，端到端测试带 `EndToEnd` 后缀
- 表驱动测试用 `tests := []struct{ name string; ... }{...}` + `t.Run`
- 单测里禁止 sleep > 100ms；要等待异步用 channel 或 `eventually` 风格
- 单测里禁止启动真实 MCP 子进程或真实 ws 连接
- **改完代码必须跑 `task ci`**；失败禁止说"完成了"；失败 3 次修不好要停下来问用户

## Git 提交规范

### Commit Message

- 格式：`<type>(<scope>): <subject>`
  - type: `feat` / `fix` / `refactor` / `test` / `docs` / `chore` / `ci` / `build`
  - scope: `mcp` / `agent` / `mod` / `docs` / `ci` / `tools` / `bridge`
  - subject: 祈使句、小写开头、不加句号、≤ 60 字
- 示例：`feat(mcp): add npc_speak tool with InMemoryTransport test`
- 涉及 milestone 收尾的 commit 在 body 里写一句 `Closes M2`

### 自动提交身份

当用户明确要求"自动提交"或"帮我 commit + push"时：
- author / committer 用用户本人的 git 默认身份（不 `-c user.name/email` 覆盖）
- AI 协作通过 trailer 体现，末尾追加：
  ```
  Co-Authored-By: Claude <noreply@anthropic.com>
  Generated-By: CodeBuddy + Claude
  ```

### 流程

- 每次自动 commit 前必须本地 `task ci` 通过，禁止 push 红的代码
- 多文件改动尽量做成一个原子 commit
- 不允许 `git push --force` 到主分支（除非用户明确要求）
- 禁止 `--no-verify`、`[skip ci]`、`--no-gpg-sign`

### Git Hooks

- **不用** pre-commit / pre-push hook 做校验（CI 已覆盖）
- **允许** post-commit hook 做开发体验副作用（opt-in `task hooks:enable`，非阻塞）

### 禁止

- 禁止改用户全局 `~/.gitconfig`
- 禁止在用户没说"自动 commit"时主动 commit
- 禁止伪造他人 GitHub 账号 email

## CI 反馈循环

用户 push 之后说"看 CI" / "check ci"时，执行以下流程：

1. **拉取最近一次运行**：`python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1`
2. **SUCCESS**：一句话汇报 PASS + 关键指标，结束
3. **FAILURE / CANCELLED**：
   - `gh run view <runId> --log-failed` 拉日志
   - 归类（compile / test / lint / dependency / environment / workflow / flake）
   - 汇报格式：`CI 失败 / 失败 job / 分类 / 根因 / 建议修复`
   - 修复 < 5 行直接动手；否则等用户确认
4. **修复后**：`task ci` 本地验证 → commit → push → 回到步骤 1
5. **3 次仍未 PASS**：停止自动修复，汇总所有失败信息给用户

### 禁止

- 禁止盲目猜测失败原因，必须看到日志
- 禁止改 `.github/workflows/` 来掩盖失败
- 禁止 `[skip ci]`、`continue-on-error: true` 等绕过

## Skills（来自 `.codebuddy/skills/`）

### ci-doctor

诊断并修复 GitHub Actions CI 失败。触发词："看 CI"、"check ci"、"ci 怎么样了"、"Actions 挂了"。

**SOP：**
1. 前置检查 `gh --version` + `gh auth status`
2. 运行 `python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1` 获取结构化 JSON
3. 按 `references/failure-patterns.md` 分类失败
4. 修复 → `task ci` → commit → push → 循环至 SUCCESS 或第 3 次失败

内置资源：
- `.codebuddy/skills/ci-doctor/scripts/fetch_run.py` — 结构化 CI 状态 JSON
- `.codebuddy/skills/ci-doctor/references/failure-patterns.md` — 失败分类目录

### project-launcher

启动 SmartNPC 项目组件（Windows 原生，不用 WSL）。触发词："启动项目"、"跑起来"、"启动 mcp"、"run server"。

**SOP：**
1. 检查 `go version` ≥ 1.22
2. `task mcp:run` 或手动 build + 启动 HTTP mode（`--http :3000 --ws-url="" --log-level=debug`）
3. `curl http://127.0.0.1:3000/healthz` 验证
4. 可选：`task mod:install` + 启动游戏

关键 flag：`--http :PORT`、`--ws-url URL`（`=""` 关闭 bridge）、`--echo-mode`、`--log-level`

内置资源：
- `.codebuddy/skills/project-launcher/references/wdac-bypass.md` — WDAC 绕过方案

## 当前里程碑

| Milestone | 状态 |
|-----------|------|
| M1 Go workspace + MCP ping + agent CLI | ✅ |
| M1.5 Taskfile + GitHub Actions CI/Release | ✅ |
| M2 SMAPI Mod 骨架 + WebSocket 桥接 | ✅（smapi-mod 已有 ws server + chat/mail 工具） |
| M3 NPC 行为工具集（20+ 工具） | ⬜ |
| M4 OpenAI provider + 单 NPC agent loop | ⬜ |
| M5 SQLite 记忆 + 调度 + 多 NPC 编排 | ⬜ |

每个 milestone 完成后**等用户验证**再进入下一个。

---

## 维护说明

本文件的规则和 skills 来源于 `.codebuddy/` 目录：
- `.codebuddy/rules/*.mdc` — 项目规则（回复风格、Git 规范、测试纪律、Windows 环境等）
- `.codebuddy/skills/*/SKILL.md` — 可执行技能（ci-doctor、project-launcher）

**`/init` 时应自动扫描 `.codebuddy/` 文件夹并将最新内容合并到本文件。**
