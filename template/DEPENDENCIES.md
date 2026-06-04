---
name: mcporter-internal
description: 基于开源 mcporter 改造而来，用于适配内部 KM、iSearch、iWiki、工蜂、TAPD等 公司内部OA 平台的 MCP 服务。支持直接列举、配置、鉴权并调用 MCP 服务器/工具（支持 HTTP 或 stdio），包括临时服务器、配置编辑及 CLI/类型生成，如果其他skill提到了依赖mcporter-internal，那么用terminal调用 mcporter-internal。

---

# mcporter-internal 安装与使用指南

本 skill 依赖 `mcporter-internal` 作为 MCP 客户端。如果当前环境没有加载 `mcporter-internal` skill，请参考本文档完成安装和配置。

---

## 安装

```bash
npm install -g @tencent/mcporter-internal --registry=https://mirrors.tencent.com/npm
```

## 验证安装

```bash
mcporter-internal list
# 列出所有已配置的 MCP 服务及其状态
```

---

## 基本用法

### 列出服务和工具

```bash
# 列出所有服务
mcporter-internal list

# 查看某个服务的工具列表及参数 schema
mcporter-internal list <服务名> --schema
```

### 调用工具

```bash
# ✅ Mac/Linux 优先：使用 --args JSON 格式传参，确保参数类型正确
mcporter-internal call <服务名>.<工具名> --args '{"key": "value"}'

# ✅ Windows 可用 kv 方式（避免 JSON 在终端的解析问题）
# 当参数值为数字形式的 ID/编码时加 --raw-strings 防止被转为 number（如 docid=12345 保持 "12345"）
mcporter-internal call <服务名>.<工具名> key=value --raw-strings
```

> ⚠️ **提示**：`--raw-strings` 禁止数字强制转换（numeric coercion），当 kv 方式传参的值为数字形式的 ID/编码且需要保持为字符串时，加上此选项（如 `docid=12345` 保持 `"12345"`）。若参数本身就是数字类型（如 `limit=5`）则无需添加。Mac/Linux 下也可直接使用 `--args JSON` 格式来避免此问题。

### 守护进程管理

```bash
mcporter-internal daemon start    # 启动守护进程
mcporter-internal daemon status   # 查看状态
mcporter-internal daemon stop     # 停止
mcporter-internal daemon restart  # 重启
```

### ⚠️ JSON 参数跨平台写法

- **macOS / Linux**：使用双引号包裹，内部双引号需转义：`--args "{\"key\": \"value\"}"`
- **Windows PowerShell**：使用单引号包裹，但内部双引号**仍需 `\` 转义**：`--args '{\"key\": \"value\"}'`。这是因为 PowerShell 传参给外部程序时，底层 Windows `CommandLineToArgvW` 会将未转义的双引号当作参数分隔符吃掉，导致 JSON 解析失败（报错 `Unable to parse --args: Expected property name or '}' in JSON at position 1`）。
- **Windows PowerShell 禁止**用双引号包裹 JSON（如 `--args "{"key":"value"}"`），否则内部双引号会被 PowerShell 自身吞掉。

---
