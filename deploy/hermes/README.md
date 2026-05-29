# Hermes Agent — Remote Deployment

一键部署 6 个 NPC Hermes Gateway 到远程 Linux 服务器。

## 部署步骤（只需 3 步）

```bash
# 1. 克隆仓库 & 准备 profile 数据（首次）
git clone https://github.com/OmniStormX/SmartNPC.git
cd SmartNPC/deploy/hermes
bash scripts/sync-profiles.sh

# 2. 配置 .env（唯一需要手动编辑的文件）
cp .env.example .env
vim .env

# 3. 启动
docker compose up -d --build
```

之后所有配置变更只需改 `.env` 然后 `docker compose up -d`（无需重新 build）。

## .env 配置项

```env
# 玩家 PC 的 mcp 公网地址（Hermes 回调 MCP 工具用）
SMARTNPC_MCP_URL=http://your-public-ip:3000/mcp

# Gateway 鉴权 key（与玩家侧 --hermes-api-key 一致）
SMARTNPC_HERMES_KEY=smartnpc-test-key

# LLM 供应商
HERMES_AGENT_URL=https://api.deepseek.com/v1
HERMES_AGENT_API_KEY=sk-xxx
HERMES_AGENT_MODEL=deepseek-v4-pro-external

# Langfuse（可选，留空关闭）
HERMES_LANGFUSE_PUBLIC_KEY=
HERMES_LANGFUSE_SECRET_KEY=
HERMES_LANGFUSE_BASE_URL=https://cloud.langfuse.com
```

## 玩家侧设置

Windows PC 的 `.env` 加一行：

```env
SMARTNPC_HERMES_GATEWAY_HOST=<远程服务器公网IP>
```

启动 mcp 时带上远程服务器地址：

```cmd
smartnpc-mcp\bin\smartnpc-mcp.exe --http :3000 ^
  --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config hermes\runtime-config.yaml ^
  --hermes-api-key smartnpc-test-key ^
  --mcp-api-key %SMARTNPC_MCP_API_KEY% ^
  --log-level debug
```

确保玩家 PC 的 `:3000` 端口从公网可达（路由器端口转发 / 防火墙规则）。

## 运维

```bash
docker compose ps                          # 查看状态
docker compose logs -f hermes-xiami        # 看日志
docker compose restart hermes-xiami        # 重启单个
docker compose down                        # 停止全部
bash scripts/healthcheck.sh                # 批量健康检查
```

修改 SOUL.md 或 skills 后需要重新 build：

```bash
bash scripts/sync-profiles.sh
docker compose up -d --build
```

修改 .env（LLM key、MCP 地址等）只需重启：

```bash
docker compose up -d
```

## 端口

| NPC | 端口 |
|-----|------|
| xiami | 8642 |
| abigail | 8643 |
| haley | 8644 |
| harvey | 8645 |
| penny | 8646 |
| sebastian | 8647 |

## 排障

| 问题 | 原因 | 解决 |
|------|------|------|
| Gateway unhealthy | 容器崩溃或启动中 | `docker compose logs hermes-<npc>` |
| Tool calls timeout | mcp 不可达 | `curl http://<mcp-host>:3000/healthz` |
| 401 on tool calls | API key 不匹配 | 检查 `.env` 中的 key 与 mcp 侧一致 |
| NPC 不回复 | hermesrelay 连不上 gateway | 确认 8642-8647 端口开放 |
