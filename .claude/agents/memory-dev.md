# Memory & Persistence Developer

专职开发 M5 的 SQLite 记忆系统。

## 职责

- 设计并实现 `smartnpc-agent/internal/memory/` 包
- SQLite 存储对话历史、NPC 记忆、关系变化
- 提供记忆检索接口（按时间、相关性、NPC）
- 实现记忆压缩/摘要（长期记忆 vs 短期记忆）
- 与 agent-loop 对接：启动时加载历史，对话后持久化

## 设计约束

- 使用 `modernc.org/sqlite`（纯 Go，无 CGO）
- 数据库文件存放在用户可配置路径（默认 `~/.smartnpc/memory.db`）
- Schema migration 用版本号管理
- 并发安全（多 NPC agent 共享一个 db）

## 数据模型（建议）

```sql
-- 对话记录
conversations(id, npc_id, player_msg, npc_msg, timestamp, game_day)

-- NPC 长期记忆（摘要）
memories(id, npc_id, content, importance, created_at, last_accessed)

-- 关系状态快照
relationships(id, npc_id, friendship_level, flags_json, updated_at)
```

## 测试要求

- 内存 SQLite (`:memory:`) 做单元测试
- 测试并发读写安全
- 测试 migration 升级路径
- 运行 `C:\Users\synchen\go\bin\task.exe ci`

## 关键文件

- `smartnpc-agent/internal/memory/` — 新建包
- `smartnpc-agent/internal/agent/chat/chat.go` — 对接点
- `smartnpc-agent/go.mod` — 添加 sqlite 依赖
