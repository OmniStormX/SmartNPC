# SmartNPC 环境配置（Windows 主机 + WSL）


`run.bat` 做的事：build mod + mcp → 杀旧进程 → 装 mod + 同步 hermes profile → 起 `smartnpc-mcp --http :3000` → 起 6 个 Hermes gateway（xiami / abigail / haley / harvey / penny / sebastian）→ 通过 SMAPI 启动游戏。

---

## 1. 必装清单

| 组件 | 版本 | 用途 |
|---|---|---|
| Stardew Valley | 1.6+ | 游戏本体 |
| SMAPI | 4.0+ | mod loader |
| .NET SDK | 6.0 | 构建 SMAPI mod |
| Go | 1.22+（推荐 1.25+） | 构建 `smartnpc-mcp` |
| Task | latest | 唯一构建入口（`task.exe`） |
| WSL | Ubuntu-22.04（发行版名固定） | 跑 Hermes |
| Hermes Agent | 当前可用版本 | NPC 决策运行时 |

---

## 2. 安装步骤

### 2.1 Stardew Valley + SMAPI

1. Steam 安装 Stardew Valley，先跑一次进主菜单确认正常
2. 去 [smapi.io](https://smapi.io) 下 Windows 安装器，**关闭游戏**后跑 `install on Windows.bat`
3. 验证：双击 `<SDV>\StardewModdingAPI.exe` 能进游戏，控制台没有红字

记录 SDV 安装路径，下面 §3 要写到 `.env`。常见值：

- 默认 Steam：`C:\Program Files (x86)\Steam\steamapps\common\Stardew Valley`
- 自定义：`D:\Stardew Valley`（本仓库 `run.bat` 默认值）

### 2.2 Go + .NET 6 + Task

```cmd
winget install GoLang.Go
winget install Microsoft.DotNet.SDK.6
go install github.com/go-task/task/v3/cmd/task@latest
```

**新开一个 cmd**（让 PATH 生效），验证：

```cmd
go version
dotnet --version
"%USERPROFILE%\go\bin\task.exe" --version
```

> 三条任意一条不通，先解决；不要往下走。

### 2.3 WSL + Ubuntu-22.04

```cmd
wsl --install -d Ubuntu-22.04
```

第一次会要求重启 + 设 Linux 用户名密码。装完进 WSL：

```cmd
wsl -d Ubuntu-22.04
```

```bash
sudo apt update
sudo apt install -y python3 python3-pip git curl jq
```

> **发行版名必须是 `Ubuntu-22.04`**。`run.bat` 用 `wsl -d Ubuntu-22.04 ...` 写死。如果你有别的发行版，要么重装 22.04，要么把 `run.bat` 里所有 `Ubuntu-22.04` 替换成你的发行版名。

### 2.4 Hermes Agent + 6 个 profile bootstrap

在 WSL 里按 Hermes 实际渠道安装（pip / 安装脚本 / 源码），装完 `hermes --version` 能正常打印即可。

然后让 Hermes 把 6 个 profile 的 `config.yaml` 骨架建出来——`hermes/install.sh` 检测到 profile 没初始化会报 `needs_bootstrap` 退出：

```bash
# WSL 里
for p in xiami abigail haley harvey penny sebastian; do
  hermes -p "$p" run --help >/dev/null 2>&1 || true
done

ls ~/.hermes/profiles/
# 应该看到 6 个目录，每个里面有 config.yaml
```

> 如果你的 Hermes 版本没有 `-p <name> run` 子命令，按它实际的"创建/初始化 profile"命令来做，目标是 `~/.hermes/profiles/<npc>/config.yaml` 这 6 个文件存在。

### 2.5 配置 LLM provider + API key

LLM 的 provider / base_url / api_key 是 Hermes bootstrap 自己问并写到 `~/.hermes/profiles/<npc>/config.yaml`，**不在仓库里**，也不进 `.env`。每个 profile 的 `config.yaml` 里 `model:` 段大致长这样：

```yaml
model:
  provider: openai          # 或 anthropic / azure-openai / 自建 OpenAI 兼容网关
  base_url: https://api.openai.com/v1
  api_key: sk-...
  default: gpt-5.5          # 默认模型；可被 SMARTNPC_HERMES_MODEL 覆盖
```

#### 推荐方式 A：bootstrap 时让 Hermes 写一次

第一次跑 `hermes -p xiami run --help`（即 §2.4 那条循环）时，如果 Hermes 检测不到默认 LLM 配置，会交互式让你选 provider + 填 key + base_url，自动写到那 6 个 `config.yaml`。**API key 默认所有 profile 共用同一个，跨 profile 隔离的是记忆和人格，不是 LLM 账户**。

#### 推荐方式 B：手动改每个 profile 的 `config.yaml`

如果 bootstrap 没问、或想换 key：

```bash
# WSL 里
for p in xiami abigail haley harvey penny sebastian; do
  $EDITOR ~/.hermes/profiles/$p/config.yaml
  # 找到 model: 段，改 provider / base_url / api_key
done
```

> **不要**把 `api_key` 写到仓库 `.env` 然后再让脚本同步——`apply_hermes_tuning.sh` 只覆盖 `model.default` / `compression.*` / `agent.*`，**不动 `api_key`**，是有意为之（避免 git 里出现敏感信息）。

#### 调一下默认模型（可选）

仓库根 `.env` 里设：

```env
SMARTNPC_HERMES_MODEL=gpt-4o-mini
```

`run.bat` 第 3 步会跑 `scripts/apply_hermes_tuning.sh`，这个脚本只把 `model.default` 写进每个 profile 的 `config.yaml`，**不动 provider / base_url / api_key**。空着就用 Hermes bootstrap 的默认（一般是 `gpt-5.5`）。

### 2.6 Hermes API server 鉴权 key（profile ↔ mcp）

`smartnpc-mcp` 通过 HTTP 反向调 Hermes Gateway 的 `/v1/responses` 注入事件，Hermes 这边要鉴权。机制：

| 在哪 | 干嘛 |
|---|---|
| `hermes/profiles/<npc>/config-overlay.yaml` 里的 `API_SERVER_KEY: smartnpc-test-key` | Hermes Gateway 启动时读，作为合法 token |
| 仓库根 `.env` 里的 `SMARTNPC_HERMES_KEY` | mcp 启动时读，写到 outbound 的 `Authorization: Bearer ...` 头 |

**两边必须相等**，仓库默认值都是 `smartnpc-test-key`，第一次配开箱即用。要改的话两边一起改：

```env
# .env
SMARTNPC_HERMES_KEY=my-real-key
```

```yaml
# hermes/profiles/<每个 npc>/config-overlay.yaml
API_SERVER_KEY: my-real-key
```

改完跑 `wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh` 让新 key 同步到 `~/.hermes/profiles/<npc>/config.yaml`。

### 2.7（可选）Langfuse 追踪

`config-overlay.yaml` 默认开了 `observability/langfuse` 插件：每次 NPC turn 的 LLM/工具调用会上报到 Langfuse Cloud。不用这个功能就算了——没配凭证插件会自己跳过。要用：

```bash
# WSL 里
cat >> ~/.hermes/.env <<'EOF'
HERMES_LANGFUSE_BASE_URL=https://cloud.langfuse.com
HERMES_LANGFUSE_PUBLIC_KEY=pk-lf-...
HERMES_LANGFUSE_SECRET_KEY=sk-lf-...
EOF
```

> 注意 **`~/.hermes/.env` 不是 `~/.hermes/profiles/<npc>/.env`**——是 Hermes 全局共享的环境变量文件。

---

## 3. 仓库配置

### 3.1 拉代码 + GitHub CLI

```cmd
d:
git clone <仓库 URL> SmartNPC
cd D:\SmartNPC
gh auth login
```

> `gh` 用于 CI 反馈循环，不影响 `run.bat`，但项目规则要求装。

### 3.2 写 `.env`

```cmd
copy .env.example .env
notepad .env
```

`run.bat` 启动时会先读 `.env`，**所有本机相关的值都通过 `.env` 覆盖，不再需要改 `run.bat`**。

最常见要设的几项：

| 变量 | 何时需要设 | 默认 |
|---|---|---|
| `SMARTNPC_GAME_PATH` | SDV 不在 `D:\Stardew Valley` | `D:\Stardew Valley` |
| `TASK_EXE` | task 不在 `%USERPROFILE%\go\bin\task.exe` | `%USERPROFILE%\go\bin\task.exe` |
| `WSL_DISTRO` | WSL 发行版不叫 `Ubuntu-22.04` | `Ubuntu-22.04` |
| `SMARTNPC_ACTIVE_PROFILES` | 想精简 NPC 数省内存 | 6 个全开 |
| `WIN_HOST_IP` / `WSL_IP` | 自动探测失败时手动固定 | 自动从 WSL 查 |
| `SMARTNPC_HTTP_PORT` | mcp 端口冲突 | `3000` |
| `SMARTNPC_HERMES_KEY` | 改了 profile overlay 里的 `API_SERVER_KEY` | `smartnpc-test-key` |
| `SMARTNPC_HERMES_MODEL` | 想覆盖每个 profile 的默认模型名 | 留空（用 bootstrap 默认） |

> **LLM 的 `api_key` / `base_url` 不在 `.env` 里**——见 §2.5，那是写在每个 profile 的 `~/.hermes/profiles/<npc>/config.yaml` 里的 Hermes bootstrap 配置。

> **IP 自动探测**：`run.bat` 启动时会调 `scripts/detect_wsl_ips.sh`（在 WSL 里跑 `ip route` + `hostname -I`），把 `WIN_HOST_IP` / `WSL_IP` 输出回 cmd。WSL 重启后 IP 变了也会自动重新查，**通常不需要手动管**。只在自动探测失败（`[cfg]` 段看到 `127.0.0.1` + `[WARN]`）或想固定值时才在 `.env` 里写 `WIN_HOST_IP=` / `WSL_IP=`。

---

## 4. （可选）手动查 IP

`run.bat` 启动时会打印 `[cfg] WIN_HOST_IP = ...` / `[cfg] WSL_IP = ...`。如果显示 `127.0.0.1` + `[WARN]` 或自动值不对，手动查：

```cmd
:: 从 Windows 看 WSL IP（写到 .env 的 WSL_IP=）
wsl -d Ubuntu-22.04 hostname -I

:: 从 WSL 看 Windows host IP（默认网关，写到 .env 的 WIN_HOST_IP=）
wsl -d Ubuntu-22.04 bash -lc "ip route | awk '/default/{print \$3}'"
```

> Hermes profile 里的 `mcp_servers` host IP 不用手动改，`hermes/install.sh` 会用 `ip route` 自动渲染。

---

## 5. 验证

### 5.1 本地 CI 必须绿

```cmd
d: && cd D:\SmartNPC
task ci
```

`task ci` 跑 profile 渲染检查 + `go vet` + 全部测试 + 全量 build。**红了不要硬启 `run.bat`**。

### 5.2 一键启动

> ⚠️ **必须在 cmd（不是 PowerShell）里跑**。
> PowerShell 在某些版本下会把 `.bat` 逐行解释，第一行 `setlocal enabledelayedexpansion` 直接被吃成 `'edelayedexpansion' 不是内部或外部命令`。
>
> 正确做法：
> ```
> Win+R → 输入 cmd → 回车
> d: && cd \SmartNPC
> run.bat
> ```
> 双击 `D:\SmartNPC\run.bat` 也可以（资源管理器会用 cmd 跑）。

启动后会先打印 `[env]` 和 `[cfg]` 段，类似：

```
[env] loaded .env
[cfg] SMARTNPC_REPO       = D:\SmartNPC
[cfg] TASK_EXE            = C:\Users\<you>\go\bin\task.exe
[cfg] WSL_DISTRO          = Ubuntu-22.04
[cfg] WIN_HOST_IP         = 192.168.48.1
[cfg] WSL_IP              = 192.168.59.118
[cfg] SMARTNPC_GAME_PATH  = D:\Stardew Valley
[cfg] SMARTNPC_HTTP_PORT  = 3000
[cfg] SMARTNPC_WS_URL     = ws://127.0.0.1:18745/ws
[cfg] ACTIVE_PROFILES     = xiami,abigail,haley,harvey,penny,sebastian
```

确认每行值都对再让它继续往下走（按 Ctrl+C 可以中断）。

正常进度：

| 阶段 | 预期 | 大概耗时 |
|---|---|---|
| `[1/6] Build complete` | mod + mcp 编译通过 | 30s–2min |
| `[2/6] Old processes cleared` | 杀旧 SDV / mcp 进程 | <2s |
| `[3/6] Mod installed, Hermes profiles synced...` | mod 装到游戏目录 + profile 同步 | 10–30s |
| `[4/6] mcp HTTP up at :3000/mcp` | mcp 起来并被 WSL 访问到 | 5–15s |
| `[5/6] All ... gateways healthy` | 选中的 Hermes gateway 全部健康 | 每个 30–60s |
| `[6/6] Game launching` | SMAPI 拉起游戏 | 立即 |

载入存档后按 `Tab` 看到 SmartNPC 聊天面板就成功了。

---

## 6. 常见卡点

| 卡在哪 | 多半原因 | 解决 |
|---|---|---|
| `'edelayedexpansion' 不是内部或外部命令` 等成串错乱 | 在 PowerShell 里直接 `.\run.bat` | 改在 **cmd** 里跑（§5.2 红框） |
| 每行命令前几个字符被吞（`'tlocal' 不是…`） | `run.bat` 文件被改成 LF 行尾 | 用 PowerShell 转回 CRLF：`powershell -NoProfile -Command "$p='D:\SmartNPC\run.bat'; $t=[IO.File]::ReadAllText($p); $t=$t -replace \"`r`n\",\"`n\" -replace \"`n\",\"`r`n\"; [IO.File]::WriteAllText($p,$t,[Text.UTF8Encoding]::new($false))"` |
| `[env]` 那行中文是 `浼氭墦` 之类乱码 | cmd 字体不支持 UTF-8 中文字形 | 内容是对的，**不影响执行**；想消除可在终端属性里换"NSimSun"或"Cascadia Code"字体 |
| `[1/6]` `task` 找不到 | task 不在默认路径 | `.env` 里设 `TASK_EXE=...绝对路径...` |
| `[cfg]` 阶段 `wsl` 报错 / 一直卡住 | WSL 没装 / 发行版名不对 | `wsl -l -v` 查名字，`.env` 里设 `WSL_DISTRO=...` |
| `[cfg] WIN_HOST_IP = 127.0.0.1` + `[WARN]` | `scripts/detect_wsl_ips.sh` 探测失败 | 按 §4 手动查后写到 `.env` 的 `WIN_HOST_IP=` |
| `[cfg] WSL_IP = 127.0.0.1` + `[WARN]` | 同上 | `.env` 里写 `WSL_IP=` |
| `[3/6]` `install.sh` 报 `needs_bootstrap` | Hermes profile 没初始化 | 回 §2.4 跑 bootstrap |
| `[4/6]` 一直 `... waiting for mcp` | mcp 启动报错 / `WIN_HOST_IP` 不对 | 看新弹的 `smartnpc-mcp` PowerShell 窗口的报错；IP 问题按 §4 在 `.env` 里手动设 `WIN_HOST_IP` |
| `[5/6]` 一直 `... waiting for <npc> on :864x` | hermes gateway 起不来 / `WSL_IP` 不对 | 看 `Hermes Gateways` wsl 窗口；按 §4 设 `WSL_IP` |
| 游戏起来但 NPC 不回话 | gateway 早于 mcp 注册，缓存"0 tools" | `run.bat` 顺序已保证；若仍不通，确认 WSL 内能 `curl http://$WIN_HOST_IP:3000/mcp` |
| NPC 收到事件但 `gateway` 日志报 `401 Unauthorized` | `SMARTNPC_HERMES_KEY` ≠ profile 里的 `API_SERVER_KEY` | 见 §2.6，两边必须一致；改完跑 `hermes/install.sh` |
| `gateway` 日志报 `model.api_key invalid` / `401 from openai` | profile 的 `~/.hermes/profiles/<npc>/config.yaml` 里 LLM key 没配 / 过期 | 见 §2.5，编辑那 6 个 `config.yaml` 的 `model.api_key` |
| `task mod:install` 报 DLL 占用 | 游戏没关 | `run.bat` 第 2 步会自动 kill；手动跑就先关游戏 |
| 同时起多个 mcp，ws 连不上 | mod ws 只接受一个客户端 | `task mcp:stop` 杀掉多余实例 |

---

## 7. 一次跑通后

之后每次开发只用：

```cmd
d: && cd D:\SmartNPC
run.bat
```

机器重启 / WSL 重启后 IP 变了，`run.bat` 会自动重新探测，不用手动改任何东西。

如果只想精简 NPC、降内存（每个 gateway ~300–500MB），在 `.env` 里设：

```env
SMARTNPC_ACTIVE_PROFILES=xiami,abigail
```

`run.bat` 会按列表自动从 profile→port 表里取端口去探活，无需别的改动。
