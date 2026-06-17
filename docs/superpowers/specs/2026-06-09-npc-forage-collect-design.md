# npc_forage_collect — Design Spec

**Date:** 2026-06-09  
**Status:** Approved  
**Scope:** 新增 MCP 工具 `npc_forage_collect`，让 NPC 自主采集地图上的野生物品（蘑菇、贝壳、野果等），采集结果暂存 NPC 背包，每捡一件推送 `forage_collected` ws event 给 Hermes。

---

## 1. 数据流 & 组件边界

```
LLM → npc_forage_collect(npc, radius, max_count)
  ↓
Go MCP Tool (npc_behavior.go)
  br.Call("npc_forage_collect", params) → WS request
  ← {ok:true, found:N, message:"acknowledged"}

C# ForageCollectHandler.Execute()
  扫描 location.Objects.Pairs (IsSpawnedObject)
  过滤半径 + max_count → sorted tile list
  FollowSystem.StartForageCollect(npcName, targets, inventory)
  ← 立即返回 {ok:true, found:N}

FollowSystem.TickForageCollect() [每帧]
  Phase 1: TryStartPath → 走向目标
  Phase 2: 到达 → Remove(tile) + inventory.Add(itemId,1)
               + doEmote(20) + 推 forage_collected ws event
  Phase 3: 下一个目标 or Mode=Idle

ws event → EventPusher.Push("forage_collected", {...})
         → Hermes 收到实时采集通知
```

三层边界与现有行为一致：
- **smapi-mod**：游戏线程操作（对象移除、路径寻找、动画）
- **smartnpc-mcp**：MCP 工具 schema、ws 参数校验、半径/数量 clamp
- **Hermes profile**：决策何时调用本工具（skill + cron）

---

## 2. Go MCP 工具接口

**文件：** `smartnpc-mcp/adapters/stardew/tools/npc_behavior.go`（追加）

```go
type NpcForageCollectInput struct {
    NpcName  string `json:"npc_name"  jsonschema:"required,description=NPC game name (e.g. XiaMi)"`
    Radius   int    `json:"radius"    jsonschema:"default=6,description=Search radius in tiles (1-15)"`
    MaxCount int    `json:"max_count" jsonschema:"default=5,description=Max forageable items to collect (1-20)"`
}

type NpcForageCollectOutput struct {
    OK      bool   `json:"ok"`
    Found   int    `json:"found"`    // 扫描到的可采集物总数（不受 max_count 限制）
    Message string `json:"message"`
}
```

- `radius` 服务端 clamp 到 [1, 15]；`max_count` clamp 到 [1, 20]
- `found` 返回扫描到的总数，让 LLM 判断现场是否丰收
- 响应为 "acknowledged"；实际采集通过 event 推送

---

## 3. ws event 格式（`forage_collected`）

每捡完一个物品推一次，格式：

```json
{
  "type": "event",
  "event": "forage_collected",
  "data": {
    "npc":       "XiaMi",
    "item_id":   "(O)281",
    "item_name": "Morel",
    "quantity":  1,
    "tile_x":    42,
    "tile_y":    17,
    "location":  "Forest"
  }
}
```

- `item_id`：SDV 格式（`(O)<numericId>` 或 `(O)<qualifiedId>`），从 `obj.ItemId` 读取
- `item_name`：`obj.DisplayName`，便于 Hermes 直接用于对话
- 推送时机：`Objects.Remove(tile)` 成功后立即推（对象已消失再通知，避免推后被别人拿走）

---

## 4. C# 状态机扩展

**文件：** `smapi-mod/Movement/FollowSystem.cs`

### 4a. NpcBehaviorState 新增字段

```csharp
// ForageCollect mode
public Queue<(Point Tile, string ItemId, string ItemName)> ForageQueue = new();
public (Point Tile, string ItemId, string ItemName) ForageTarget;
public bool ForagePathed;
```

### 4b. FollowMode 新增枚举值

```csharp
ForageCollect,
```

### 4c. StartForageCollect()

```csharp
public void StartForageCollect(
    string npcName,
    List<(Point, string, string)> targets,
    NpcInventory inventory)
{
    var state = GetOrCreate(npcName);
    state.Inventory = inventory;
    state.ForageQueue = new Queue<(Point, string, string)>(targets);
    state.ForageTarget = state.ForageQueue.Dequeue();
    state.ForagePathed = false;
    state.Mode = FollowMode.ForageCollect;
}
```

### 4d. TickForageCollect() 三阶段

```
Phase 1 (ForagePathed == false):
  TryStartPath(npc, location, adjacent_to(ForageTarget.Tile))
  ForagePathed = true

Phase 2 (距离 > 1.5 || path not complete):
  继续走（pathfinding 自然推进）

Phase 3 (到达):
  if (location.Objects.ContainsKey(ForageTarget.Tile))
    location.Objects.Remove(ForageTarget.Tile)
    state.Inventory.Add(npcName, ForageTarget.ItemId, 1)
    npc.doEmote(20)  // 拾取动画
    EventPusher.Push("forage_collected", { npc, item_id, item_name, qty:1, tile_x, tile_y, location })
  // else: 对象已被移除，静默跳过

  if (ForageQueue.Count > 0)
    ForageTarget = ForageQueue.Dequeue()
    ForagePathed = false
  else
    Mode = Idle
```

### 4e. 新增 C# Handler

**文件：** `smapi-mod/Behavior/ForageCollectHandler.cs`

继承 `NpcActionHandlerBase`，`Execute()` 逻辑：
1. 解析 `radius`（clamp 1-15）、`max_count`（clamp 1-20）
2. 查找 NPC 当前 location
3. 遍历 `location.Objects.Pairs`，过滤 `obj.IsSpawnedObject == true`
4. 按欧式距离排序，取前 `max_count` 个
5. 调用 `FollowSystem.StartForageCollect(...)`
6. 返回 `{ok:true, found:<total_scanned>}`

---

## 5. docs/events.md 更新

追加 `forage_collected` event 说明。

---

## 6. 测试覆盖

| 测试 | 文件 | 验证点 |
|---|---|---|
| `TestNpcForageCollect_Basic` | `adapters/stardew/tools/npc_behavior_test.go` | 工具注册 + mock ws 返回 ok + found=N |
| `TestNpcForageCollect_NotFound` | 同上 | found=0 时 ok=true 不崩 |
| `TestNpcForageCollect_RadiusClamp` | 同上 | radius=99 被截断为 15 |
| `TestNpcForageCollect_MaxCountClamp` | 同上 | max_count=99 被截断为 20 |

---

## 7. 不在本次 scope 内

- `npc_wander` + `npc_forage_collect` 的 Hermes skill 组合（Hermes 侧决策，不需 C# 改动）
- 采集物品质量（普通/金星/银星）——当前 `NpcInventory.Add` 只存 itemId，质量扩展留 future
- 跨地图采集（NPC 传送后继续）——复杂度高，留 future
