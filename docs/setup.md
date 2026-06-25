# SmartNPC 环境配置（Windows 主机 + WSL Docker）


## 快速开始

```cmd
git clone <仓库 URL> SmartNPC
cd SmartNPC
copy .env.example .env        &rem 编辑 .env，填 4 个必填变量
setup.bat                     &rem 首次：渲染 profile + 构建 Docker 镜像（~3-5 min）
run.bat                       &rem 日常：编译 + 启动全套 + 启动游戏
```

`setup.bat` 做的事：渲染 Hermes profile → 探测 WSL/Windows IP → 从 `npcs.yaml` 生成 `docker-compose.yml` → 生成 Docker `.env` → build Docker 镜像。

`run.bat` 做的事：build mod + mcp → 杀旧进程 → 装 mod → 起 `smartnpc-mcp --http :3000` → `docker compose up -d` 起 Hermes gateway → 启动游戏。

---

## 1. 必装清单

| 组件 | 版本 | 用途 |
|---|---|---|
| Stardew Valley | 1.6+ | 游戏本体 |
| SMAPI | 4.0+ | mod loader |
| .NET SDK | 8.0（C# Dev Kit 需要） | 构建 SMAPI mod（target `net6.0`） |
| Go | 1.22+（推荐 1.25+） | 构建 `smartnpc-mcp` |
| Task | latest | 唯一构建入口（`task.exe`） |
| WSL2 | Ubuntu-22.04（或自定义） | Docker 宿主 |
| Docker Engine | WSL 内 | 跑 Hermes Agent 容器 |

---

## 2. 安装步骤

### 2.1 Stardew Valley + SMAPI

1. Steam 安装 Stardew Valley，先跑一次进主菜单确认正常
2. 去 [smapi.io](https://smapi.io) 下 Windows 安装器，**关闭游戏**后跑 `install on Windows.bat`
3. 验证：双击 `<SDV>\StardewModdingAPI.exe` 能进游戏，控制台没有红字

记录 SDV 安装路径，下面 §3 要写到 `.env`。常见值：

- 默认 Steam：`C:\Program Files (x86)\Steam\steamapps\common\Stardew Valley`
- 自定义：`D:\Stardew Valley`

### 2.2 Go + .NET SDK + Task

```cmd
winget install GoLang.Go
winget install Microsoft.DotNet.SDK.8
go install github.com/go-task/task/v3/cmd/task@latest
```

**新开一个 cmd**（让 PATH 生效），验证：

```cmd
go version
dotnet --version
"%USERPROFILE%\go\bin\task.exe" --version
```

> 三条任意一条不通，先解决；不要往下走。

### 2.3 WSL2 + Docker

```cmd
wsl --install -d Ubuntu-22.04
```

第一次会要求重启 + 设 Linux 用户名密码。装完进 WSL 安装 Docker Engine：

```bash
# WSL 里
sudo apt update
sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
sudo usermod -aG docker $USER
```

**退出 WSL 再重新进入**（让 docker 组生效），验证：

```bash
docker info
docker compose version
```

> 如果你的 WSL 发行版不是 `Ubuntu-22.04`，在 `.env` 里设 `WSL_DISTRO=你的发行版名`。

---

## 3. 仓库配置

### 3.1 拉代码

```cmd
d:
git clone <仓库 URL> SmartNPC
cd D:\SmartNPC
```

### 3.2 写 `.env`

```cmd
copy .env.example .env
notepad .env
```

**新用户只需填这 4 项：**

| 变量 | 说明 | 示例 |
|---|---|---|
| `SMARTNPC_GAME_PATH` | SDV 安装路径 | `D:\Stardew Valley` |
| `HERMES_AGENT_URL` | LLM API 地址 | `https://api.deepseek.com` |
| `HERMES_AGENT_API_KEY` | LLM API Key | `sk-xxx` |
| `HERMES_AGENT_MODEL` | 模型名 | `deepseek-v4-pro` |

其他变量全有合理默认值，详见 `.env.example` 注释。

### 3.3 首次 setup

```cmd
setup.bat
```

这一步会：
1. 检查前置条件（WSL / Docker / 游戏路径 / LLM 配置）
2. 渲染 NPC profiles（从 `hermes/profiles/_master/` 模板）
3. 自动探测 WSL ↔ Windows IP
4. 从 `hermes/npcs.yaml` 动态生成 `deploy/hermes/docker-compose.yml`
5. 生成 `deploy/hermes/.env`（Docker 容器使用的环境变量）
6. 在 WSL Docker 中构建 Hermes Agent 镜像

首次构建约 3-5 分钟（拉基础镜像 + pip install hermes-agent）。

---

## 4. 日常启动

```cmd
run.bat
```

启动后会打印 `[cfg]` 段：

```
[cfg] SMARTNPC_REPO       = D:\SmartNPC
[cfg] TASK_EXE            = C:\Users\<you>\go\bin\task.exe
[cfg] WIN_HOST_IP         = 192.168.48.1
[cfg] HERMES_GATEWAY_HOST = 192.168.59.118
[cfg] SMARTNPC_GAME_PATH  = D:\Stardew Valley
[cfg] SMARTNPC_HTTP_PORT  = 3000
[cfg] ACTIVE_PROFILES     = xiami,abigail,haley,harvey,penny,sebastian
```

正常进度：

| 阶段 | 预期 | 耗时 |
|---|---|---|
| `[1/5] Build complete` | mod + mcp 编译通过 | 30s–2min |
| `[2/5] Old processes cleared` | 杀旧 SDV / mcp 进程 | <2s |
| `[3/5] mcp HTTP up` | mod 安装 + mcp 启动 | 5–15s |
| `[4/5] All gateways healthy` | Docker 容器启动 | 每个 30–60s |
| `[5/5] Game launching` | SMAPI 拉起游戏 | 立即 |

载入存档后按 `Tab` 看到 SmartNPC 聊天面板就成功了。

---

## 5. 何时重跑 setup.bat

| 场景 | 动作 |
|---|---|
| 改了 `hermes/profiles/_master/` 下的 SKILL / 模板 | 重跑 `setup.bat` |
| 改了 `hermes/npcs.yaml`（增删 NPC） | 重跑 `setup.bat` |
| 改了 `.env` 中的 LLM 凭据 | 重跑 `setup.bat` |
| 只改了 Go / C# 代码 | 只需 `run.bat` |
| WSL 重启后 IP 变了 | `run.bat` 自动重新探测，无需操作 |

---

## 6. 可选配置

### 6.1 精简 NPC（省内存）

每个 Hermes gateway 容器约 300–500MB RAM。想精简：

```env
SMARTNPC_ACTIVE_PROFILES=xiami,abigail
```

端口从 `hermes/npcs.yaml` 自动读取，无需其他改动。

### 6.2 Langfuse 观测

在 `.env` 中设：

```env
HERMES_LANGFUSE_PUBLIC_KEY=pk-lf-xxx
HERMES_LANGFUSE_SECRET_KEY=sk-lf-xxx
HERMES_LANGFUSE_BASE_URL=http://langfuse:3000
```

重跑 `setup.bat` 让凭据写入 Docker `.env`。如需 Langfuse 自建实例，可额外用：

```cmd
wsl -d Ubuntu-22.04 bash -lc "cd /mnt/d/SmartNPC/deploy/hermes && docker compose -f docker-compose.yml -f docker-compose.langfuse.yml up -d"
```

### 6.3 Hermes API Key（本地开发默认无需改）

`smartnpc-mcp` 通过 HTTP Bearer token 调 Hermes Gateway，两边必须一致：

- 仓库默认值：`smartnpc-test-key`
- `.env` 中 `SMARTNPC_HERMES_KEY`
- 每个 profile `config-overlay.yaml` 的 `API_SERVER_KEY`

本地开发开箱即用；远程部署时换一个强密码并两边同步。

---

## 7. 常见卡点

| 卡在哪 | 多半原因 | 解决 |
|---|---|---|
| `setup.bat` 报 `Docker is not available in WSL` | Docker Engine 未安装或未启动 | 按 §2.3 安装；`sudo service docker start` |
| `setup.bat` 报 `HERMES_AGENT_URL is not set` | `.env` 未填 LLM 配置 | 编辑 `.env` 填 4 个必填项 |
| `run.bat` 报 `docker-compose.yml not found` | 没跑过 `setup.bat` | 先跑 `setup.bat` |
| `run.bat` 报 `StardewModdingAPI.exe not found` | 游戏路径不对 | `.env` 里改 `SMARTNPC_GAME_PATH` |
| `[1/5]` task 找不到 | task 不在默认路径 | `.env` 里设 `TASK_EXE=...` |
| `[3/5]` 一直 `waiting for mcp` | mcp 启动报错 / IP 问题 | 看 `smartnpc-mcp` PowerShell 窗口 |
| `[4/5]` 一直 `waiting for <npc>` | Docker 容器未起来 | `wsl docker compose -f .../docker-compose.yml logs` |
| 游戏起来但 NPC 不回话 | gateway 缓存"0 tools"（mcp 晚于 gateway 启动） | 正常流程 `run.bat` 已保证顺序；若仍不通重跑 `setup.bat` + `run.bat` |
| gateway 日志 `401 Unauthorized` | HERMES_KEY 不一致 | 见 §6.3 |
| gateway 日志 `model error / 401` | LLM key 无效或额度用完 | `.env` 里检查 `HERMES_AGENT_API_KEY` |
| `在 PowerShell 里跑 run.bat 乱码` | PS 逐行解释 .bat | 改在 **cmd** 里跑 |

---

## 8. 架构参考

```
                 setup.bat (首次)
                     │
    ┌────────────────┼────────────────┐
    │                │                │
render_profiles   generate_compose  docker build
    │                │                │
    ▼                ▼                ▼
hermes/profiles/  docker-compose.yml  Docker images
                                      (in WSL)

                 run.bat (日常)
                     │
    ┌────────┬───────┼───────┬────────┐
    │        │       │       │        │
 mod:build mcp:build  mcp   docker   game
    │        │     HTTP     compose    │
    ▼        ▼    :3000    up -d      ▼
 DLL+deploy  .exe   │       │      SMAPI
                    │       │
                    └───┬───┘
                        │
               NPC gateway healthy
```

关键路径：`run.bat` 保证 mcp 先于 Hermes gateway 启动（gateway 启动时向 mcp 注册工具列表）。
