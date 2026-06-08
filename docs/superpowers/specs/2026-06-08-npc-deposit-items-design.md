# npc_deposit_items 实现设计

## Goal

NPC 将自身背包（`NpcInventory`）中的物品放入游戏世界中的箱子（`Chest`）。
支持：指定箱子坐标 或 自动寻找最近箱子；指定物品 ID 过滤 或 放入全部物品。

## Architecture

复用 `ClearDebris` 的「Execute 扫描 → FollowSystem tick 驱动」模式：

```
DepositItemsHandler.Execute（游戏线程，由 NpcActionHandlerBase 调度）
  → 解析参数（箱子坐标、item_ids、map）
  → FollowSystem.StartDepositItems(npcName, chestTile, chestMap, itemIds, inventory)

FollowSystem.TickDepositItems（每帧，PumpOnGameTick）
  → 阶段 1：寻路到箱子旁（≤ 1.5 tile）
  → 阶段 2：到达 → 逐 slot 放入 → NpcInventory.Take → doEmote → Idle
```

## 参数规范

### Go MCP 输入（修改 `NpcDepositItemsInput`）

```go
type NpcDepositItemsInput struct {
    NPC      string   `json:"npc"`
    ChestX   int      `json:"chest_x,omitempty"`   // 与 ChestY 配合使用；AutoFind=true 时忽略
    ChestY   int      `json:"chest_y,omitempty"`
    AutoFind bool     `json:"auto_find,omitempty"` // true = 忽略坐标，自动找最近箱子
    Map      string   `json:"map,omitempty"`        // 默认 NPC 当前地图
    ItemIds  []string `json:"item_ids,omitempty"`   // 空 = 放全部背包物品
}
```

### Go MCP 输出（修改 `NpcDepositItemsOutput`）

```go
type NpcDepositItemsOutput struct {
    OK        bool   `json:"ok"`
    NPC       string `json:"npc"`
    Deposited int    `json:"deposited"`            // 实际放入的物品总数量
    ChestX    int    `json:"chest_x"`              // 实际使用箱子坐标（自动找时有用）
    ChestY    int    `json:"chest_y"`
    Message   string `json:"message,omitempty"`
}
```

### C# ws params（JSON）

```json
{
  "npc": "Abigail",
  "chest_x": 10,
  "chest_y": 5,
  "map": "Farm",
  "item_ids": ["(O)390", "(O)388"]
}
```

- `auto_find: true`（或 `chest_x/chest_y` 均为 0）→ 自动扫描最近 Chest
- `item_ids` 为空数组或缺失 → 放全部背包物品

## FollowSystem 状态扩展

### `NpcBehaviorMode` 新增

```csharp
DepositItems,
```

### `NpcBehaviorState` 新增字段

```csharp
public Point         DepositChestTile { get; set; }
public string?       DepositChestMap  { get; set; }
public HashSet<string>? DepositItemIds { get; set; }  // null = 全部
public NpcInventory? DepositInventory { get; set; }
public bool          DepositPathed    { get; set; }
public int           DepositedCount   { get; set; }   // 累计已放入数量（用于返回值）
```

## `StartDepositItems` 逻辑

```
1. 如果 autoFind == true 或坐标均为 0：
   → 扫描 location.Objects，筛选 StardewValley.Objects.Chest 类型
   → 取距 NPC 最近的一个
   → 找不到 → 立即 log warn，不进入 DepositItems 模式，返回 no_chest_found
2. 设置 state 字段，Mode = DepositItems
```

## `TickDepositItems` 逻辑

```
每帧：
  1. 获取目标地图的 location，检查 chestTile 上的 Object 是否仍是 Chest
     → 不是（被移走）→ Mode = Idle，保留已放入数量
  2. dist(npc.Tile, chestTile) > 1.5 且路径未完成：
     → 尝试 PathFindController 到 chestTile + (0,1)（下方）
     → 失败则依次尝试 (0,-1) (1,0) (-1,0)
  3. dist ≤ 1.5 且路径完成：
     a. items = NpcInventory.GetItems(npcName)
        若 DepositItemIds != null → 过滤
        若 items 为空 → Mode = Idle
     b. 对每个 slot：
        item = ItemRegistry.Create(slot.ItemId, slot.Count, slot.Quality)
        leftover = chest.addItem(item)
        placed = slot.Count - (leftover?.Stack ?? 0)
        NpcInventory.Take(npcName, slot.ItemId, placed)
        DepositedCount += placed
     c. npc.doEmote(32)  // happy
     d. Mode = Idle
```

## 错误情况

| 情况 | 处理 |
|------|------|
| 背包为空（或过滤后无物品） | 不走路，直接返回 `deposited=0, ok=true` |
| 找不到箱子（自动模式） | 返回 `ok=false, message="no_chest_found"` |
| 指定坐标没有 Chest | 返回 `ok=false, message="not_a_chest"` |
| 箱子满（addItem 返回剩余） | 放入能放的，剩余留在 NpcInventory，`deposited` 反映实际放入量 |
| 箱子在放入过程中消失 | 停止，以已放入数量返回 |

## 修改文件清单

### 修改
- `smapi-mod/Behavior/WorldActionHandlers.cs` — `DepositItemsHandler` 实现
- `smapi-mod/Movement/FollowSystem.cs` — 新增 `DepositItems` mode + state 字段 + tick handler
- `smartnpc-mcp/adapters/stardew/tools/npc_world_action.go` — 修改 `NpcDepositItemsInput/Output`

## 与现有系统的衔接

- `NpcInventory` 已有 `GetItems` / `Take` 接口，直接使用
- `FollowSystem` 已有 `ClearDebris` 模式为范本，结构完全一致
- debug 命令 `smartnpc_deposit_items <NpcName> [chestX] [chestY]` 可复用 `HandleClearDebris` 的模式添加
