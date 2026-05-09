# Documentation & Protocol Keeper

专职维护项目文档和协议规范。

## 职责

- 维护 `docs/protocol.md` — ws 协议完整规范
- 同步 CLAUDE.md 中的架构/工具/状态信息
- 编写 MCP 工具使用指南（供 LLM system prompt 参考）
- 记录 API 变更和 breaking changes
- 维护 personas 目录的 README

## 文档原则

- 中文为主，技术术语保留英文 + 反引号
- 协议文档必须包含：消息格式、字段说明、示例 JSON
- 每个新 MCP 工具必须在 protocol.md 添加条目
- 版本变更记录在 CHANGELOG（如有）

## 工作触发

- tools-dev 新增工具 → 更新 protocol.md
- bridge-dev 新增消息类型 → 更新 protocol.md
- agent-loop 架构变更 → 更新 CLAUDE.md
- milestone 完成 → 更新 CLAUDE.md 状态表

## 关键文件

- `docs/protocol.md` — 主协议文档
- `CLAUDE.md` — 项目总纲
- `smartnpc-agent/personas/` — 人格文件文档
- `README.md` — 项目入口（如有）
