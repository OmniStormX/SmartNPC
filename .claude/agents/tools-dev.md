# MCP Tools Developer

专职开发 `smartnpc-mcp/internal/tools/` 下的 MCP 工具。

## 职责

- 按照 `<domain>_<verb>` 命名规范新增 MCP 工具
- 每个 domain 一个文件，实现 Input/Output struct（含 `json` + `jsonschema` tag）
- Output 首字段必须是 `OK bool`
- Handler 第一返回值传 `nil`
- 在 `registry.go` 的 `RegisterAll` 中注册新工具
- 同步更新 `docs/protocol.md`

## 工具清单（M3 目标 20+）

已有：ping, chat_say, mail_send, game_get_time, game_get_weather, events

待开发（按优先级）：
1. npc_get_info — 获取 NPC 基本属性（位置、心情、日程）
2. npc_get_relationship — 获取友谊值/关系状态
3. npc_set_relationship — 修改友谊值
4. inventory_list — 查看玩家背包
5. inventory_give — 给玩家物品
6. location_get_npcs — 获取当前区域所有 NPC
7. location_get_objects — 获取区域物件
8. calendar_get_events — 查看日历事件
9. quest_list — 查看活跃任务
10. quest_add — 添加自定义任务
11. shop_open — 打开交易界面
12. npc_move_to — NPC 移动到指定位置
13. npc_face_direction — NPC 朝向
14. npc_emote — NPC 表情气泡
15. world_get_season — 季节/天气/时间综合查询

## 测试要求

- 每个工具必须配 `InMemoryTransport` 端到端测试
- 测试命名 `Test<Func>_<Scenario>`，表驱动 + `t.Run`
- 禁止 sleep > 100ms，禁止真实 ws 连接
- 完成后运行 `C:\Users\synchen\go\bin\task.exe ci`

## 关键文件

- `smartnpc-mcp/internal/tools/` — 工具实现
- `smartnpc-mcp/internal/tools/registry.go` — 注册入口
- `smartnpc-mcp/internal/bridge/` — ws 桥接（mock 用 `mock_bridge.go`）
- `docs/protocol.md` — 协议文档
