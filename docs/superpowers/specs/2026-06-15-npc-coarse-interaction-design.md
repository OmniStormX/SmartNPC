# NPC 粗粒度交互模型设计

## 1. 问题陈述

### 当前模型（细粒度）

```
Agent                          MCP Tools                         Mod (C#)
  │                               │                                 │
  ├─ npc_inspect_object(what=all,r=5) ──→                    扫描5×5,返回每个tile坐标
  │  ← {mature_crops:[{x:12,y:5,crop:"防风草"},{x:13,y:6,...}]}  │
  │                               │                                 │
  ├─ npc_harvest_crops(r=5,max=5) ──→                       扫描+执行
  │  ← {harvested:3}               │                                 │
  │                               │                                 │
  ├─ npc_water_crops(r=5,max=8)  ──→                         扫描+执行
  │  ← {watered:5}                 │                                 │
  │                               │                                 │
  ├─ npc_deposit_items()          ──→                         找箱子+存物
  │                               │                                 │
```

**核心问题：**

1. **Agent 负担重** — 观测返回瓦片级坐标（每株作物的 x,y），Agent 要处理大量空间数据，但实际决策不需要这个精度
2. **往返次数多** — 每个行为一次 MCP 调用，一次农场日常需要 4-6 次往返
3. **Agent 做微决策** — radius 该设多少？max_count 该设多少？这些是 Mod 更适合决定的事情
4. **观测与行为脱节** — 观测到的坐标不能直接喂给行为工具，Agent 需要重新翻译

### 根本原因

Agent 充当了"中间管理层"，承担了本应在 Mod 侧完成的坐标筛选、范围决策、执行调度工作。

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| Agent 只需决策"做什么" | 行为类型 + 优先级，不指定坐标 |
| 观测输出直接可喂给行为 | bbox 从观测原样传入行为工具 |
| 减少往返次数 | 一次观测 + 最多一次行为调用完成农场日常 |
| 保留现有工具 | 渐进式演进，不推翻重来 |
| Mod 处理可达性/路径 | Agent 不操心 NPC 能否走到目标 |

---

## 3. 架构对比

### Before

```
Agent ──每个tile的坐标──→ 决策 ──radius+max_count──→ Mod扫描执行
         (逐瓦片)              (微参数)
```

### After

```
Agent ──行为分类+bbox──→ 决策 ──行为类型+bbox──→ Mod扫描执行
         (聚合摘要)          (粗粒度)
```

---

## 4. 观测层：`npc_inspect_object` 改造

### 4.1 新增 `what: "farm_actions"` 模式

调用示例：
```json
{
  "npc": "Abigail",
  "what": "farm_actions",
  "radius": 40
}
```

### 4.2 返回格式

```json
{
  "ok": true,
  "npc": "Abigail",
  "map": "Farm",
  "radius": 40,
  "tiles_scanned": 2028,
  "summary": "5 株成熟作物, 8 块未浇水, 3 垃圾, 2 采集物, 4 块空地",
  "actions_available": {
    "harvest": {
      "count": 5,
      "bbox": {"x1": 2, "y1": 3, "x2": 8, "y2": 7},
      "crops": [
        {"id": "(O)24", "name": "防风草", "count": 3},
        {"id": "(O)20", "name": "土豆", "count": 2}
      ]
    },
    "water": {
      "count": 8,
      "bbox": {"x1": 1, "y1": 1, "x2": 9, "y2": 9}
    },
    "clear": {
      "count": 3,
      "bbox": {"x1": 10, "y1": 2, "x2": 12, "y2": 5}
    },
    "till": {
      "count": 0
    },
    "forage": {
      "count": 2,
      "bbox": {"x1": 15, "y1": 6, "x2": 16, "y2": 7}
    },
    "plant": {
      "count": 4,
      "bbox": {"x1": 2, "y1": 3, "x2": 8, "y2": 7}
    }
  }
}
```

### 4.3 行为分类定义

| 分类 key | 扫描目标 | bbox 含义 |
|----------|---------|-----------|
| `harvest` | 成熟作物 (fullyGrown=true) | 所有成熟作物包围盒 |
| `water` | 未浇水作物 (needsWatering) | 所有未浇水作物包围盒 |
| `clear` | 垃圾 (杂草/树枝/小石头) | 所有垃圾包围盒 |
| `till` | 可翻地空 tile (Diggable=T, 无占用) | 所有可翻地包围盒 |
| `forage` | 采集物 (IsSpawnedObject) | 所有采集物包围盒 |
| `plant` | 空地 (HoeDirt 无 crop) | 所有空地包围盒 |

### 4.4 Radius 上限调整

| 参数 | 旧 | 新 |
|------|----|----|
| `radius` max | 10 | **40** |

### 4.5 兼容性

- `what: "crops"` / `"objects"` / `"terrain"` / `"all"` — 保持原有逐瓦片输出，不改行为
- `what: "farm_actions"` — 新聚合模式
- 无 `what` 参数时默认 `"all"`，行为不变

---

## 5. 行为层：现有工具加 bbox 参数

### 5.1 改动的工具

| 工具 | 新增参数 | radius 上限 |
|------|---------|-------------|
| `npc_harvest_crops` | `x1, y1, x2, y2` | 10 → **40** |
| `npc_water_crops` | `x1, y1, x2, y2` | 10 → **40** |
| `npc_clear_debris` | `x1, y1, x2, y2` | 10 → **40** |
| `npc_till_soil` | `x1, y1, x2, y2` | 8 → **40** |
| `npc_forage_collect` | `x1, y1, x2, y2` | 15 → **40** |

### 5.2 行为工具 Input 示例（改后）

```json
// npc_harvest_crops — bbox 模式
{
  "npc": "Abigail",
  "x1": 2, "y1": 3, "x2": 8, "y2": 7,
  "max_count": 5
}

// npc_harvest_crops — radius 模式（向后兼容）
{
  "npc": "Abigail",
  "radius": 5,
  "max_count": 5
}
```

### 5.3 优先级规则

1. 若 `x1,y1,x2,y2` 全部非零 → 在 bbox 矩形内扫描目标
2. 否则 → 以 NPC 为中心按 `radius` 扫描
3. 两者都不设 → 使用默认 radius

### 5.4 不改动的工具

| 工具 | 原因 |
|------|------|
| `npc_deposit_items` | 已有 `auto_find` 模式，不需坐标 |
| `npc_deliver_items` | 目标固定是玩家，不需 bbox |
| `npc_plant_seeds` | 需要 `seed_id` 参数，bbox 可选后续加 |
| `npc_fertilize` | 需要 `fertilizer_id`，同 plant |
| `npc_break_resource` | 需要 `what` 过滤，bbox 可选后续加 |
| `npc_wander` | 已有 `x1/y1/x2/y2` zone 约束 |

---

## 6. 典型交互流程

### 6.1 农场日常（改后）

```
Agent                          MCP Tools                         Mod (C#)
  │                               │                                 │
  ├─ npc_inspect_object(what=farm_actions,r=40) ──→          扫描25×25,分类聚合,算bbox
  │  ← {actions_available:{harvest:{bbox:(2,3)-(8,7),...},    │
  │       water:{bbox:(1,1)-(9,9)}, clear:{...}, ...}}          │
  │                               │                                 │
  │ Agent 决策:                                                  │
  │   1. 有成熟作物 → harvest 优先级最高                          │
  │   2. 有未浇水 → water 次之                                    │
  │   3. 有垃圾 → clear 再次                                      │
  │                               │                                 │
  ├─ npc_harvest_crops(x1=2,y1=3,x2=8,y2=7,max=5) ──→       在bbox内扫描+行走+收获
  │  ← {harvested:5}               │                                 │
  │                               │                                 │
  ├─ npc_water_crops(x1=1,y1=1,x2=9,y2=9,max=20) ──→         在bbox内扫描+行走+浇水
  │  ← {watered:8}                 │                                 │
  │                               │                                 │
  ├─ npc_deposit_items(auto_find=true) ──→                    找最近箱子+存物
  │                               │                                 │
```

### 6.2 对比：往返次数

| 场景 | 旧模型 | 新模型 |
|------|--------|--------|
| 农场日常（观察+收获+浇水+存储） | 4 次 | 4 次（观测变为一次有用调用） |
| 发现无活可干 | 2 次（观察→放弃） | 1 次（观察即知） |
| 后续批量（可选） | N/A | 1 次批量调用替代 N 次单独调用 |

---

## 7. 可选扩展：`npc_execute_batch`

### 7.1 动机

即使加了 bbox，一次完整的 farm 日常仍需 3-4 次 MCP 往返。可加一个批量工具让 Agent 一次性提交行为队列。

### 7.2 接口

```json
// 请求
{
  "npc": "Abigail",
  "actions": [
    {"type": "harvest", "x1": 2, "y1": 3, "x2": 8, "y2": 7, "max_count": 5, "priority": 1},
    {"type": "water",   "x1": 1, "y1": 1, "x2": 9, "y2": 9, "max_count": 20, "priority": 2},
    {"type": "deposit", "auto_find": true, "priority": 3}
  ]
}

// 响应 — 每个 action 的执行结果
{
  "ok": true,
  "results": [
    {"type": "harvest", "ok": true, "done": 5},
    {"type": "water",   "ok": true, "done": 8},
    {"type": "deposit", "ok": true, "deposited": 13}
  ],
  "summary": "harvest=5 water=8 deposit=13"
}
```

### 7.3 是否纳入当前改动

**暂不纳入**，原因：
- 先验证"观测→bbox→单行为"闭环是否流畅
- 单行为调用仍是当前主流，改动面小
- 批量工具可后续独立加，不阻塞当前设计

---

## 8. 实现计划

### Phase 1：Go 端 Struct + 描述更新

**文件：** `smartnpc-mcp/adapters/stardew/tools/npc_world_action.go`

- 新增类型：`BBox`、`CropSummary`、`ActionGroup`
- `NpcInspectObjectInput`：radius 上限 10→40，what 增加 `farm_actions`
- `NpcInspectObjectOutput`：新增 `ActionsAvailable map[string]ActionGroup`
- 5 个行为 Input：各加 `X1/Y1/X2/Y2`，radius 上限放宽到 40
- 更新所有相关 tool Description

### Phase 2：C# 端观测改造

**文件：** `smapi-mod/Behavior/WorldActionHandlers.cs`

- `InspectObjectHandler.GetResult`：
  - radius 上限 10→40
  - 新增 `farm_actions` 扫描逻辑（6 类目标 + bbox 计算）
  - 新增 `AddActionGroup` 辅助方法
  - `farm_actions` 模式直接 return，不走 legacy 路径
- `ParseWhat`：增加 `farm_actions` 识别

### Phase 3：C# 端行为 handler bbox 支持

**文件：** `smapi-mod/Behavior/WorldActionHandlers.cs`

修改 5 个 handler 的 `Execute` 方法：
- 解析 `x1/y1/x2/y2` 参数
- 若 4 个参数全 > 0，在该矩形内扫描（代替 radius 圆形扫描）
- Radius 上限放宽到 40

### Phase 4：验证

- Go 端 `go test ./adapters/stardew/tools/...`（更新存量 test）
- C# 端 `dotnet build`
- 全量 `task ci`

---

## 9. 影响分析

### 破坏性变更

| 项 | 程度 | 缓解 |
|----|------|------|
| `NpcInspectObjectOutput` 新增字段 | 加字段，非破坏 | Go struct tag `omitempty` |
| 行为 Input 新增 bbox 字段 | 加字段，非破坏 | `omitempty`，不传时走旧逻辑 |
| radius 上限放宽 | 非破坏 | 旧调用不受影响 |
| `farm_actions` 新 what 值 | 纯新增 | 旧 `all/crops/objects/terrain` 行为不变 |

**无破坏性变更** — 所有旧调用路径保持不变。

### 后续工作（不在本设计范围）

- Hermes Skill 文件（`smartnpc-farm` 等）需更新指导语，教 Agent 如何使用新 `farm_actions` 模式
- `npc_execute_batch` 批量工具可独立设计
- `npc_plant_seeds` / `npc_fertilize` / `npc_break_resource` 后续也可加 bbox
