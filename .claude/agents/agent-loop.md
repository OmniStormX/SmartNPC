# Agent Loop Engineer

专职开发 `smartnpc-agent/internal/agent/` 的 NPC agent 主循环。

## 职责

- 修复 agent loop 事件驱动机制（当前 `<-ctx.Done()` 导致挂起）
- 实现事件分发：bridge ws event → agent notification → LLM 决策
- 完善工具调用循环（当前 max 5 rounds）的错误恢复
- 添加 graceful shutdown + 重连逻辑
- 与 memory-dev 配合实现对话历史持久化接口

## 当前问题

1. `main.go:93` 的 `<-ctx.Done()` 永远阻塞 — 需要 event listener 驱动
2. 事件处理器已注册（chat_received, npc_interact）但 bridge 未连通时无法触发
3. 缺少集成测试验证完整 event → respond 流程

## 架构要求

- Agent 通过 stdio spawn `smartnpc-mcp` 子进程
- 不引入 HTTP 直连，所有游戏交互通过 MCP tools
- LLM Provider 用 OpenAI 兼容接口（Hermes gateway `192.168.59.118:8642`）

## 测试要求

- 补充 E2E agent loop 测试（mock LLM + mock MCP）
- 验证多轮工具调用、历史裁剪、错误恢复
- 运行 `C:\Users\synchen\go\bin\task.exe ci`

## 关键文件

- `smartnpc-agent/internal/agent/chat/chat.go` — 核心循环
- `smartnpc-agent/internal/agent/chat/persona.go` — 人格加载
- `smartnpc-agent/cmd/smartnpc-agent/main.go` — CLI 入口
- `smartnpc-agent/internal/llm/` — LLM provider
