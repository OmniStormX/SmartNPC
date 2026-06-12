# CODEBUDDY.md This file provides guidance to CodeBuddy when working with code in this repository.

## 项目定位

SmartNPC — 星露谷物语 AI NPC 系统。每个 NPC 对应一个独立的 Hermes Agent profile（隔离 SOUL / 记忆 / API 端口）。

```
Stardew Valley (SMAPI) ── ws :18745 ── smartnpc-mcp (Go, --http :3000) ── MCP HTTP ── 6× Hermes Profile
                                              └── hermesrelay ── POST /v1/responses ──┘  (xiami, abigail, haley,
                                                                                          harvey, penny, sebastian)
```

| 模块 | 语言 | 角色 |
|------|------|------|
| `smapi-mod/` (`StardewMCPBridge`) | C# .NET 6 | SMAPI Mod，ws server + NPC spawn/移动/交互 + Harmony patch 聊天框 |
| `smartnpc-mcp/` | Go 1.25+ | MCP Server（stdio 或 `--http :PORT`），ws↔MCP 工具桥，hermesrelay 事件转发 |
| `hermes/profiles/` | YAML + MD | 6 个 NPC profile：SOUL.md + skills + cron + overlay |

> 旧 `smartnpc-agent/` Go 编排器已删除（M5 Hermes-first 迁移完成）。

Go 模块通过根 `go.work` 联动。

## 命令（全部走 Taskfile）

```bash
task --list                # 列出所有任务
task ci                    # profiles:verify + lint + test + build（与 GitHub Actions 等价）
task ci-fast               # profiles:verify + lint + test（跳过 build）
task lint                  # go work sync + go vet
task test                  # 所有 Go + mod 测试
task build                 # 构建三端全部产物
task profiles:verify       # sanity-check hermes/npcs.yaml + runtime-config + 渲染 profiles 树
task tidy                  # 每个 Go 模块 go mod tidy + go work sync
task clean                 # 清理产物
```

> 根 Taskfile 已 `dotenv: ['.env']`，所有 task 自动加载仓库根 `.env`（git ignored，按 `.env.example` 复制）。

子项目命名空间：

```bash
task mcp:build             # 构建 smartnpc-mcp 到 smartnpc-mcp/bin/
task mcp:test              # go test ./...
task mcp:test-race         # go test -race ./...
task mcp:run PORT=3000 WS_URL=ws://localhost:18745/ws   # Windows：新窗口起 HTTP 模式
task mcp:run-echo          # 不带 LLM 的 echo NPC（PORT=3000, ECHO=1）
task mcp:stop              # 杀掉本机 smartnpc-mcp 进程
task mcp:health PORT=3000  # curl /healthz

task mod:build             # dotnet build -c Debug
task mod:install           # 构建并拷贝 DLL 到 <GAME_PATH>\Mods\StardewMCPBridge\
```

单测运行：
```bash
# 单个包
cd smartnpc-mcp && go test ./adapters/stardew/tools/... -run TestPing_EchoesMessage
# 单个 package -race
cd smartnpc-mcp && go test -race -count=1 ./adapters/stardew/bridge/...
```

CI 本地复现：`task ci`（**任何提交前必须通过**；失败禁止说"完成了"；3 次修不好停下问用户）。

## 架构关键点

### MCP 工具注册

所有工具放 `smartnpc-mcp/adapters/stardew/tools/`，一个 domain 一个文件，在 `registry.go` 的 `RegisterAll` 里统一挂载。新增工具的硬约束（缺一不可）：

- 命名 `<domain>_<verb>` 全小写下划线
- Input/Output struct 必须带 `json` + `jsonschema` tag
- Output 首字段是 `OK bool`
- Handler 第一个返回值传 `nil`，让 SDK 用 Output 自动填充 content
- **(a) 在 `RegisterAll` 注册** + **(b) 配 `InMemoryTransport` 端到端测试**（参考 `meta_test.go`）+ **(c) 同步更新 `docs/protocol.md`**

> inter-NPC 工具 `npc_send_message` / `npc_broadcast_event` 复用 hermesrelay outbound 路径触发 recipient profile —— 详见 [ADR-0001](docs/adr/0001-synthetic-events-go-through-hermesrelay.md)。

### stdio vs HTTP 模式

`smartnpc-mcp` 入口在 `smartnpc-mcp/cmd/smartnpc-mcp/main.go`，运行模式由 `--http` flag 切换：
- stdio 模式（默认）：`server.Run(ctx, &mcp.StdioTransport{})` —— 本地 MCP 客户端用
- HTTP 模式（`--http :3000`）：Streamable HTTP 在 `/mcp`，健康检查 `/healthz` —— 给 Hermes / Claude Desktop 等跨主机 MCP 客户端用

**stdio 模式禁止向 stdout 写任何日志**，否则污染 MCP 协议流。日志统一用 `internal/log` 走 stderr；禁止 `fmt.Println` / `log` 包默认输出。

### ws 桥

`smartnpc-mcp/adapters/stardew/bridge/ws_client.go` 是到 SMAPI mod 的 ws 客户端。默认地址 `ws://127.0.0.1:18745/ws`（`DefaultWSURL` 常量），带自动重连。事件通过 `EventHandler` 回调，`main.go` 里的 `makeRouter` 把事件转发给 MCP logging notification + hermesrelay outbound POST，同时支持 `--echo-mode` 原地回应 `chat_say`。

### Hermes Profile 运行时

每个 NPC profile 自带 Hermes Gateway，监听独立端口：
- `--hermes-config D:\SmartNPC\hermes\runtime-config.yaml` 让 mcp 知道每个 NPC → 哪个 gateway URL
- `hermesrelay` 把 ws 事件 POST 到 Hermes `/v1/responses`
- Hermes LLM 决策后通过 MCP HTTP 回调 mcp 的工具（chat_say / npc_* 等）

### SMAPI Mod 侧

- 入口 `smapi-mod/ModEntry.cs:18`，ws 监听 `http://127.0.0.1:18745/`（`ListenPrefix` 常量）
- `Bridge/WebSocketServer.cs` + `Bridge/MessageRouter.cs` 做协议分发
- `Chat/ChatInputCapture.cs` 用 Harmony patch `ChatBox.receiveChatMessage`，Ctrl+T 捕获玩家输入并推 `chat_received` 事件
- `NPC/XiaMiData.cs` 负责自定义 NPC 的完整生命周期：精灵图注册、NPC Dispositions、对话、日程表、SaveLoaded 时 spawn 实例到农场
- **`.csproj` 里 `<GamePath>` 通过环境变量 `SMARTNPC_GAME_PATH` 设置**，缺省让 `ModBuildConfig` 自动探测

### NPC 精灵系统

自定义 NPC 精灵图放在 `smapi-mod/assets/<npc_name>/`：
- `XiaMi.png` — RGBA spritesheet（已去背景），通过 `AssetRequested` hook 注入 `Characters/<NpcName>`
- `XiaMi_Portrait.png` — 肖像图，注入 `Portraits/<NpcName>`
- `sprite_actions_positions_*.json` — 动画帧坐标元数据（开发参考，游戏不读）
- `process_sprite.py` — 辅助脚本：去背景 + 透明化

**SDV 精灵帧尺寸**：`AnimatedSprite` 构造参数 `spriteWidth` / `spriteHeight` 必须和 spritesheet 网格对齐。SDV 原版是 16×32；自定义高清图要按实际帧大小设。

### Hermes Agent Profile 隔离

每个 NPC 对应一个独立 Hermes profile，完全隔离：

| 配置项 | 路径 | 说明 |
|--------|------|------|
| SOUL.md | `~/.hermes/profiles/<npc>/SOUL.md` | NPC 人格（替代 Hermes 默认人格） |
| .env | `~/.hermes/profiles/<npc>/.env` | 独立 API key、端口、host |
| 记忆 | `~/.hermes/profiles/<npc>/memories/` | 自动隔离，不跨 profile |
| 端口 | `API_SERVER_PORT=864x` | 每个 NPC 不同端口（xiami=8642, abigail=8643, …, sebastian=8647） |

**仓库内的 profile 源是渲染出来的**：

- `hermes/profiles/_master/` — **共享 SKILL 模板母本**（`config-overlay.yaml` + `cron-recipes.md` + `skills/`，**不含 `SOUL.md`**）
- `hermes/profiles/<npc>/` — 渲染产物，仅 `SOUL.md` 是手写保留
- `scripts/render_profiles.sh` 通过 8 个 `{{NPC_NAME}}` 类占位符把 `_master/` 渲染到每个 NPC 目录
- ⚠️ **不要直接编辑非 `_master/` 下的渲染产物 ——`task profiles:verify` 会失败，下次 render 也会被覆盖**。详见 [ADR-0003](docs/adr/0003-npc-name-placeholder-cloning.md)

创建新 NPC profile（仅本机运行时）：
```bash
hermes profile create <npc_name> --clone
# 编辑 SOUL.md + .env（设置 API_SERVER_PORT / API_SERVER_KEY / OPENAI_API_KEY）
hermes -p <npc_name> gateway run --accept-hooks
```

### 协议

`docs/protocol.md` 是 ws 消息契约的唯一来源。三类帧：
- `request`（带 `id`）
- `response`（关联 `id`）
- `event`（push，无 `id`）

**已实现 actions**：`chat_say` / `mail_send` / `game_*`（`game_get_time` / `game_get_weather` 等）/ `friendship_get` / `npc_*`（`npc_move_to` / `npc_summon` / `npc_emote` / `npc_give_item` / `npc_follow_*` / `npc_lead_to` / `npc_send_message` / `npc_broadcast_event` 等）/ `player_get_status`

**已实现 events**：`chat_message` / `chat_received` / `npc_interact` / `group_create`（详见 [`docs/events.md`](docs/events.md)）

## 代码规范（硬约束）

- **Go 1.25+**（`go.mod` 声明 `go 1.25.0`）；可用 `log/slog`、泛型、`for range int`
- ⚠️ CI workflow 中 `GO_VERSION: '1.22'` 滞后于 `go.mod`，未来若用到 1.25 独有特性需先升 CI
- 错误包装：`fmt.Errorf("...: %w", err)`，不要丢原 err
- 包注释和导出符号注释写英文；TODO/FIXME 标注 milestone
- 新增 Go package **必须**有 `*_test.go`（至少一个 smoke test）
- 测试命名 `Test<Func>_<Scenario>`；表驱动 + `t.Run`；禁止 `sleep > 100ms`；禁止启真实 mcp 子进程或真实 ws 连接，统一用 `InMemoryTransport` / mock
- C# 只放 SMAPI 胶水（事件订阅、Harmony patch、ws 编解码），业务逻辑全部放 Go 侧
- 禁止在 `smartnpc-mcp` 持久化业务状态；hermesrelay 是无状态 outbound 转发

## Git 提交

格式：`<type>(<scope>): <subject>`（祈使句、小写、≤60 字）
- type：`feat`/`fix`/`refactor`/`test`/`docs`/`chore`/`ci`/`build`
- scope：`mcp`/`mod`/`hermes`/`docs`/`ci`/`tools`/`bridge`

流程硬规则：
- commit 前 `task ci` 必须绿
- 用用户本人 git 身份，末尾追加固定 trailer：
  ```
  Co-Authored-By: Claude <noreply@anthropic.com>
  Generated-By: CodeBuddy + Claude
  ```
- 禁止 `--force` 到主分支、`--no-verify`、`[skip ci]`、`continue-on-error` 绕 CI
- **用户没明说"自动 commit"时不要主动 commit**

## CI 反馈循环

用户说"看 CI"/"check ci"：
1. `python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1`（或 `gh run list --limit 1`）拿 runId
2. SUCCESS → 一句话汇报
3. FAILURE → `gh run view <runId> --log-failed` → 归类（compile / test / lint / dependency / environment / workflow / flake）→ 报根因 + 修复方案
4. 修复后 `task ci` → commit → push → 循环
5. 3 次不过停下找用户

禁止：盲猜原因、改 workflow 掩盖、用 `continue-on-error` 装作过了。

> 完整 SOP 见 `.codebuddy/skills/ci-doctor/SKILL.md` + `references/failure-patterns.md`。

## 跨平台 / Linux 接入注意

当前仓库在 Windows 和 Linux 上都能跑 Go 部分，但有几处 **硬编码会刺痛 Linux 用户**，修之前先读一下：

| 位置 | 问题 | 处理建议 |
|------|------|---------|
| `smapi-mod/StardewMCPBridge.csproj` `<GamePath>` | 硬编码机器本地 SDV 安装路径 | 改用 env var：`<GamePath Condition="'$(GamePath)' == ''">$(SMARTNPC_GAME_PATH)</GamePath>`；缺省留空让 `ModBuildConfig` 自动探测 |
| `smapi-mod/Taskfile.yml` `GAME_PATH`、`DOTNET` | Windows 盘符 `D:\Stardew Valley`、`C:\Program Files\dotnet\dotnet.exe` | Taskfile 已支持 `{{default "..." .VAR}}`，优先读环境变量 `SMARTNPC_GAME_PATH` / `DOTNET`；Linux 下 `dotnet` 在 PATH 上即可 |
| `smartnpc-mcp/adapters/stardew/bridge/ws_client.go` `DefaultWSURL`、`ModEntry.cs` `ListenPrefix` | `127.0.0.1:18745` 硬编码 | 保留常量作 default，但都支持命令行 flag / mod 配置文件覆盖；mod 侧加 `config.json` 用 SMAPI 标准 `helper.ReadConfig<T>()` 读 `Host` / `Port` |
| `Taskfile.yml` hooks 子命令 | 只写了 `powershell -NoProfile`，Linux 直接报错 | 按 `platforms: [windows]` 拆；Linux 下用 `echo` 等价输出 |
| `smartnpc-mcp/Taskfile.yml` `mcp:run` | `cmd /c start ...` 新窗口，只 Windows 能跑 | 加 `platforms: [linux, darwin]` 分支：`nohup ./bin/smartnpc-mcp --http :{{.PORT}} ... &` 或直接前台跑 |
| `smapi-mod/Taskfile.yml` `mod:install` | `powershell Copy-Item`、`New-Item` | Linux / macOS 加 `cp -f ... $HOME/.local/share/Steam/steamapps/common/Stardew Valley/Mods/StardewMCPBridge/` 的 `platforms: [linux, darwin]` 分支 |

**集中配置方式**：仓库根已落地 `.env.example`（git 追踪）+ `.env`（`.gitignore`），根 `Taskfile.yml` 用 `dotenv: ['.env']` 全局加载。新机器接入只需 `cp .env.example .env` 后按本机改值。关键变量：

```env
# .env.example —— 复制成 .env，按本机实际填写
SMARTNPC_GAME_PATH=/home/<you>/.local/share/Steam/steamapps/common/Stardew Valley
DOTNET=/usr/bin/dotnet
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-...
SMARTNPC_WS_URL=ws://127.0.0.1:18745/ws
SMARTNPC_HTTP_PORT=3000
```

这样 Linux 接入流程就是：
1. 装 Go 1.25+、.NET 6 SDK、`task`（`go install github.com/go-task/task/v3/cmd/task@latest` 或 apt/brew 包）
2. 装 Stardew Valley（Steam）+ SMAPI，确认能启动
3. `cp .env.example .env` 按机器实际改
4. `task ci` 验证 Go 部分全绿
5. `task mod:build` → 首次需要 `<GamePath>` 能解析到 SDV 安装目录（ModBuildConfig 会自动找大多数 Steam 默认位置）
6. `task mod:install` 把 DLL 拷进 `$SMARTNPC_GAME_PATH/Mods/StardewMCPBridge/`
7. 启游戏 + SMAPI → ws 起在 `18745`
8. 启 mcp（HTTP 模式）：`./smartnpc-mcp/bin/smartnpc-mcp --http :3000 --ws-url $SMARTNPC_WS_URL --hermes-config hermes/runtime-config.yaml`
9. 启对应 NPC 的 Hermes Gateway（WSL）：`hermes -p xiami gateway run --accept-hooks`

**动手改之前先问用户**要不要做这层重构 —— 涉及 Taskfile / csproj / Go flag defaults 多处联动，按上面表格一次性收敛成 `.env` 是一个独立 PR 的量。

## 里程碑

| Milestone | 状态 |
|-----------|------|
| M1 Go workspace + MCP ping | ✅ |
| M1.5 Taskfile + GitHub Actions CI/Release | ✅ |
| M2 SMAPI Mod + HTTP bridge | ✅ |
| M3 WebSocket bridge + 游戏内聊天框 + echo agent | ✅ |
| M4 OpenAI provider + persona + Hermes 隔离 + NPC spawn | ✅ |
| M5 Hermes-first：mcp HTTP + hermesrelay + 6 NPC profile | ✅ 代码就绪；**5.6 / 5.7 实机 happy path 待人工验证** |

每个 milestone 做完**等用户验证**再进下一个，不要自动连推。

## 网络拓扑注意

- **WSL 内的 Hermes Gateway 访问 Windows 上的 mcp `:3000/mcp` 时，可能需要 Windows host IP，而不是 `127.0.0.1`**
  - WSL2 默认 NAT，`127.0.0.1` 只指向 WSL 自身
  - 用 `wsl hostname -I` 拿 WSL IP；用 `ipconfig`（Windows 侧）拿 Windows host IP（通常 `192.168.x.x` 或 vEthernet 接口的 IP）
  - 实际值以仓库 `.env` 中 `OPENAI_BASE_URL` 和 `hermes/runtime-config.yaml` 中 `gateway_url` 为准
- **mcp 必须先于 Hermes Gateway 启动**，否则 Hermes 启动时发现 `:3000/mcp` 不在线会缓存到 "0 tools"
- **不要起多个 mcp** —— SMAPI Mod ws 只接受一个客户端

## 启动全栈（Windows）

> ⚠️ **`run.bat` 必须 CRLF 行尾**。`write_to_file` 工具在 Windows 上偶发把它写成 LF，导致 cmd 解析时每行掉前 1–3 字符（症状：`'tlocal' 不是内部或外部命令`）。改完 `run.bat` 后立即自检：
> ```cmd
> powershell -NoProfile -Command "$b=[IO.File]::ReadAllBytes('D:\SmartNPC\run.bat'); $b[9..10] -join ','"
> ```
> 第 9 字节应该是 `13`（CR），不是 `10`（LF）。若是 LF 立即转 CRLF：
> ```cmd
> powershell -NoProfile -Command "$p='D:\SmartNPC\run.bat'; $t=[IO.File]::ReadAllText($p); $t=$t -replace \"`r`n\",\"`n\" -replace \"`n\",\"`r`n\"; [IO.File]::WriteAllText($p,$t,[Text.UTF8Encoding]::new($false))"
> ```
> `.gitattributes` 已声明 `*.bat text eol=crlf`，但写文件工具不一定经过 git。


```cmd
:: 推荐一键: run.bat（build mod + 起 mcp + 起 hermes + 起游戏）
run.bat

:: 手动版:
:: 1. 构建全部
task ci

:: 2. 启动 mcp（HTTP 模式 + 多 profile fan-out）
smartnpc-mcp\bin\smartnpc-mcp.exe ^
  --http :3000 ^
  --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
  --hermes-api-key smartnpc-test-key ^
  --log-level debug

:: 3. 启动 Hermes gateways（WSL）
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh xiami,abigail"

:: 4. 安装 mod + 启动游戏（游戏关闭状态下）
task mod:install
"D:\Stardew Valley\StardewModdingAPI.exe"
```
