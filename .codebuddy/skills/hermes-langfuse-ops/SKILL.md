---
name: hermes-langfuse-ops
description: Hermes Agent 的 Langfuse 可观测性配置与排障。当用户提到 Langfuse trace 看不到、Hermes 观测配置、plugin 加载失败、环境变量注入等问题时使用。
---

# Hermes Langfuse Observability

Hermes Agent 通过内置 plugin `observability/langfuse` 将 LLM 调用、工具调用链路上报到 Langfuse。

## 凭证存放约定

明文凭证一律不进仓库。本机 Langfuse key 写到下面任意一处：

- `~/.hermes/.env`
- 启动 shell 的 `export`
- 仓库根 `.env`（已 .gitignore）

仓库内只允许出现占位符：

```
HERMES_LANGFUSE_PUBLIC_KEY=pk-lf-...
HERMES_LANGFUSE_SECRET_KEY=sk-lf-...
HERMES_LANGFUSE_BASE_URL=https://cloud.langfuse.com
```

面板地址：`https://cloud.langfuse.com`

## 环境变量注入——关键陷阱

### 问题
Plugin 通过 `os.environ.get("HERMES_LANGFUSE_PUBLIC_KEY")` 读取凭证。Hermes 的 `.env` 文件加载机制 **不会** 在所有模式下自动将变量 export 到 `os.environ`（gateway 模式下 `reload_env()` 可能未被调用）。

### 正确做法
**必须在启动 hermes 的 shell 中显式 export 环境变量**，确保它们存在于进程的 `os.environ` 中：

```bash
export HERMES_LANGFUSE_PUBLIC_KEY='pk-lf-...'
export HERMES_LANGFUSE_SECRET_KEY='sk-lf-...'
export HERMES_LANGFUSE_BASE_URL='https://cloud.langfuse.com'
hermes -p <profile> gateway run --accept-hooks
```

仅写入 `~/.hermes/.env` 或 `~/.hermes/profiles/<npc>/.env` 是 **不够的**。

### WSL 启动脚本中的注入
参见 `scripts/launch_hermes_wsl.sh`——在脚本头部 export 变量，然后 `setsid hermes ...` 启动 gateway。子进程自动继承父进程环境。

## Plugin 发现机制

### WSL 原生安装（正常工作）
- Plugin 路径：`~/.hermes/hermes-agent/plugins/observability/langfuse/`
- 必须存在 `plugin.yaml` 清单文件
- `config.yaml` 中声明 `plugins.enabled: [observability/langfuse]`
- `hermes -p <npc> plugins list` 应显示 `observability/langfuse | enabled`

### Docker 容器（已知问题）
hermes-agent 0.14.0 的 pip 包不含 `plugin.yaml`，且 bundled plugins 目录扫描有深度限制。Workaround 见 `deploy/hermes/scripts/entrypoint.sh`——在 `$PROFILE_DIR/plugins/langfuse/` 创建 `plugin.yaml` + symlink `__init__.py`。

### Docker + 自建 Langfuse Server v2 不兼容
Langfuse SDK v4.x 使用 OTLP 协议导出 spans，`langfuse/langfuse:2` Docker 镜像不支持 OTLP（返回 404）。需要：
- 升级到 `langfuse/langfuse:3`（支持 OTLP），或
- 直接使用 Langfuse Cloud（`https://cloud.langfuse.com`，已原生支持）

## WSL 进程管理注意事项

1. **`nohup ... &` 不够**：WSL 中 `bash -c` 退出时会杀掉子进程组。必须用 `setsid` 使 gateway 脱离终端会话。
2. **`bash -lc` 很慢**：login shell 加载 `.bashrc`/`.profile` 需要额外 3-5s。用 `bash -c` + 显式 export PATH 代替。
3. **`$HOME` 在 `bash -c` 中未设置**：需要手动 `export HOME=/home/synchen`。

## 排查步骤

当 Langfuse 面板看不到 trace 时：

1. **确认 plugin 已加载**：
   ```bash
   hermes -p <npc> plugins list | grep langfuse
   # 应显示 "enabled"
   ```

2. **确认进程环境变量**：
   ```bash
   pid=$(pgrep -f 'hermes.*-p <npc>.*gateway')
   cat /proc/$pid/environ | tr '\0' '\n' | grep LANGFUSE
   # 必须看到 PUBLIC_KEY / SECRET_KEY / BASE_URL
   ```

3. **独立验证 SDK 连通性**：
   ```bash
   ~/.hermes/hermes-agent/venv/bin/python /mnt/d/SmartNPC/scripts/test_langfuse.py
   ```

4. **开启 debug 日志**：
   ```bash
   export HERMES_LANGFUSE_DEBUG=true
   hermes -p <npc> gateway run --accept-hooks 2>&1 | grep -i langfuse
   ```

5. **检查 Langfuse 项目匹配**：确认面板上选对了 project（API key 对应的 project）。

## 相关文件

| 文件 | 作用 |
|------|------|
| `scripts/launch_hermes_wsl.sh` | WSL 原生启动脚本（export 凭证 + setsid） |
| `scripts/test_langfuse.py` | 独立 Langfuse SDK 连通性测试（不依赖 Hermes） |
| `scripts/test_hermes_langfuse_plugin.py` | Hermes plugin hook 模拟测试 |
| `hermes/profiles/_master/.env` | Profile 模板中的 Langfuse 配置（渲染到各 NPC） |
| `deploy/hermes/scripts/entrypoint.sh` | Docker 部署的 plugin 发现 workaround |
| `deploy/hermes/docker-compose.langfuse.yml` | 自建 Langfuse v2 stack（已知不兼容 SDK v4） |
| `run-wsl.bat` | 一键启动脚本（WSL 原生 Hermes 模式） |
