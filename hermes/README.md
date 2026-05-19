# Hermes Profiles (SmartNPC)

仓库签入版的 Hermes profile 源，作为 M5 (Hermes-first) 路线的第一批 NPC 配置。

```
hermes/
├── README.md                 ← 本文件
├── install.sh                ← 同步到 ~/.hermes/profiles/ 的脚本（WSL bash）
└── profiles/
    └── xiami/
        ├── SOUL.md           ← 人格 + 说话风格 + 好感度档位 + 工具使用原则
        ├── config-overlay.yaml ← mcp_servers + API_SERVER_* 配置（合并到 config.yaml）
        └── skills/
            └── smartnpc/
                └── game-tool-policy/
                    └── SKILL.md   ← 工具使用细则（Hermes 原生 skill 格式）
```

**重要：**`config.yaml` 不进仓库。Hermes 在 `hermes -p <name> run` 时自动生成带默认值的完整 config.yaml（包含 LLM provider、个人密钥等机器相关内容）。我们只签入与 SmartNPC 相关的覆盖层。

---

## 架构回顾

M5 目标链路（详见 [`REFACTOR.md`](../REFACTOR.md)）：

```
SMAPI Mod ──ws :18745── smartnpc-mcp --http :3000 ──MCP─── Hermes profile
```

Hermes profile 承担：人格、记忆、技能、tool planning / calling、reflection。  
`smartnpc-mcp` 只做游戏能力边界（MCP tool + event notification + 硬校验）。

---

## 安装步骤（首次）

**前置条件：**
- WSL 里已装 Hermes 且 `hermes --version` 能跑
- **Hermes 的 `mcp` Python 包已安装**（Hermes 默认不带，需要显式装到它的 venv）：
  ```bash
  ~/.hermes/hermes-agent/venv/bin/pip install mcp
  ```
  未装时 `hermes mcp test` 会报 "streamable_http is not available" / "Upgrade the mcp package"。
- Stardew Valley + SMAPI 可用（macOS/Linux 原生也行，关键是 WSL 能联通到 `:3000` 和 `:18745`）
- smartnpc-mcp 二进制已编译：`task mcp:build`
- smartnpc-mcp HTTP 服务已绑定到 `0.0.0.0:3000`（`task mcp:run` 默认如此）；bind 到 `127.0.0.1` 时 WSL 访问不了。

### 1. Bootstrap Hermes 为 xiami 建 profile 目录

Hermes 首次执行某 profile 时会自动创建 `~/.hermes/profiles/<name>/`。在 WSL 里跑一次：

```bash
hermes -p xiami help   # 或任何 hermes 命令，触发 profile 初始化
```

### 2. 同步仓库内容到 profile 目录

**从 WSL：**

```bash
bash /mnt/d/SmartNPC/hermes/install.sh
```

**从 Windows cmd：**

```cmd
wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh
```

脚本会：

1. 把 `hermes/profiles/xiami/SOUL.md` 覆盖到 `~/.hermes/profiles/xiami/SOUL.md`
2. 把 `hermes/profiles/xiami/skills/smartnpc/` 合并到 `~/.hermes/profiles/xiami/skills/smartnpc/`（不覆盖 Hermes 内置 skill）
3. 检测 WSL 默认网关（= Windows 主机 IP），把 `config-overlay.yaml` 里的 `__HOST_IP__` 占位符替换掉，追加到 `~/.hermes/profiles/xiami/config.yaml`。包含 `mcp_servers` 块（连 smartnpc-mcp）和 `API_SERVER_*` 块（开 REST gateway，让 hermesrelay 能 POST 事件进来）。

如果 `config.yaml` 里已经有 `mcp_servers:` 或 `API_SERVER_ENABLED:`，脚本**不会**覆盖；会提示手动合并（避免误删其他 MCP server / 端口冲突）。

### 3. 启动运行时

启动顺序：

```cmd
:: 1. SMAPI + 游戏
"D:\Stardew Valley\StardewModdingAPI.exe"

:: 2. smartnpc-mcp HTTP 模式 + Hermes 事件转发（新 cmd 窗口）
::    --hermes-* 参数告诉 mcp 把游戏事件 POST 到 xiami profile 的 gateway
bin\smartnpc-mcp\smartnpc-mcp.exe ^
  --http :3000 --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-url http://192.168.48.1:8642 ^
  --hermes-api-key smartnpc-test-key ^
  --hermes-conversation xiami ^
  --hermes-model xiami ^
  --hermes-npc XiaMi ^
  --hermes-persona-file hermes\profiles\xiami\SOUL.md

:: 3. Hermes xiami profile gateway（WSL 窗口）
wsl -d Ubuntu-22.04
hermes -p xiami gateway run --accept-hooks
```

> `--hermes-url` 用 `http://127.0.0.1:8642` 或 WSL 网关 IP，取决于 mcp 是从 Windows 还是 WSL 内访问 Hermes。WSL gateway 默认绑定 `0.0.0.0:8642`，从 Windows 通过 `localhost:8642` 经 WSL 端口转发可达。

验证联通：

```bash
# WSL 里
hermes -p xiami mcp test smartnpc_game   # 应 ✓ Connected + Tools discovered
curl -sS http://127.0.0.1:8642/health    # Hermes gateway 健康
```

---

## 更新 profile

改了 `hermes/profiles/xiami/SOUL.md` 或 skill 后：

```bash
bash /mnt/d/SmartNPC/hermes/install.sh
```

`config.yaml` 级别的改动（比如想 exclude 某工具）改 `mcp-servers.yaml` 然后手动同步到 `~/.hermes/.../config.yaml`。

---

## 调试

- **工具列表**：`hermes -p xiami` 启动后发一条消息，它会先调用 `native-mcp` 列出工具；日志在 `~/.hermes/profiles/xiami/logs/`
- **MCP 连不上**：从 WSL 里 curl `http://127.0.0.1:3000/mcp`，预期返回 `405 Method Not Allowed`（GET 不被允许但说明服务活着）
- **Hermes profile 路径**：`~/.hermes/profiles/xiami/` — 可以 `cd` 进去直接看状态

---

## 新增 NPC

1. 在 `hermes/profiles/<new-npc>/` 下放 `SOUL.md` + `mcp-servers.yaml`（可复用 xiami 的 mcp-servers.yaml，每个 NPC 一般都连同一个 smartnpc_game）
2. 可选：拷 `skills/smartnpc/smartnpc-game-tool-policy/` 过来或单独写
3. 重跑 `install.sh`
4. 启 `hermes -p <new-npc> gateway run --accept-hooks`（多 profile 并存时注意改 `API_SERVER_PORT` 避免端口冲突）

多 NPC 路由由 SMAPI mod 的 `AudibleNPCResolver`（Phase 4, M5.12）决定。
