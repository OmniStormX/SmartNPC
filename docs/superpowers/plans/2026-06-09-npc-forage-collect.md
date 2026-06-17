# npc_forage_collect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `npc_forage_collect` so an NPC walks to each spawned forage object within radius, removes it from the map, stores it in NPC backpack, and pushes a `forage_collected` ws event per item so Hermes can follow the collection in real time.

**Architecture:** The Go MCP tool (`npc_world_action.go`) and `bridge.ActionNpcForageCollect` constant already exist and are wired. The C# `ForageCollectHandler` stub exists in `WorldActionHandlers.cs` but has no real logic. We need to: (1) flesh out `ForageCollectHandler.Execute()`, (2) add `ForageCollect` mode + state fields + `StartForageCollect()` + `TickForageCollect()` to `FollowSystem.cs`, (3) update `ModEntry.cs` to pass `_npcInventory` and `_follow` to the handler, (4) add Go tests, and (5) document the new event in `docs/events.md`.

**Tech Stack:** Go 1.25, `modelcontextprotocol/go-sdk`, C# net6.0 / SMAPI 4, StardewValley `location.Objects`, `PathFindController`, `NpcInventory`.

---

## File Map

| File | Change |
|---|---|
| `smapi-mod/Behavior/WorldActionHandlers.cs` | Flesh out `ForageCollectHandler` — add `_inventory`, `_follow` fields, `Execute()`, `ResolveBubble()`, helpers |
| `smapi-mod/Movement/FollowSystem.cs` | Add `ForageCollect` to `NpcBehaviorMode`, add 4 state fields to `NpcBehaviorState`, add `StartForageCollect()`, `TickForageCollect()`, wire into `PumpOnGameTick` switch |
| `smapi-mod/ModEntry.cs` | Update `ForageCollectHandler` construction to pass `_npcInventory, _follow` |
| `smartnpc-mcp/adapters/stardew/tools/npc_world_action_test.go` | Add `TestNpcForageCollect_EndToEnd` and `TestNpcForageCollect_RejectsEmptyNPC` |
| `docs/events.md` | Add `forage_collected` to status table + full schema section |

---

## Task 1 — Flesh out ForageCollectHandler in WorldActionHandlers.cs

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs:282-287`

The existing stub:
```csharp
internal sealed class ForageCollectHandler : NpcActionHandlerBase
{
    protected override string ActionName => "npc_forage_collect";
    public ForageCollectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
    // TODO: find forage items in range and pick them up
}
```

Replace the entire stub with this full implementation:

- [ ] **Replace the ForageCollectHandler stub**

In `smapi-mod/Behavior/WorldActionHandlers.cs`, replace lines 282–287 (the entire `ForageCollectHandler` class) with:

```csharp
internal sealed class ForageCollectHandler : NpcActionHandlerBase
{
    private readonly NpcInventory _inventory;
    private readonly FollowSystem _follow;

    protected override string ActionName => "npc_forage_collect";

    public ForageCollectHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
        : base(log, showBubble)
    {
        _inventory = inventory;
        _follow    = follow;
    }

    protected override string ResolveBubble(JsonElement @params)
    {
        int radius   = ParseInt(@params, "radius",    8, 1, 15);
        int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
        return $"[采集] r={radius} max={maxCount}";
    }

    protected override void Execute(NPC npc, string npcName, JsonElement @params)
    {
        int radius   = ParseInt(@params, "radius",    8, 1, 15);
        int maxCount = ParseInt(@params, "max_count", 3, 1, 10);

        var location = npc.currentLocation;
        if (location is null) return;

        var npcTile = npc.Tile;
        var targets = new List<(Microsoft.Xna.Framework.Vector2 tile, string itemId, string itemName)>();

        foreach (var kv in location.Objects.Pairs)
        {
            var tile = kv.Key;
            var obj  = kv.Value;
            if (!obj.IsSpawnedObject) continue;
            float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
            if (dist > radius) continue;
            targets.Add((tile, obj.ItemId, obj.DisplayName));
        }

        targets.Sort((a, b) =>
            Microsoft.Xna.Framework.Vector2.Distance(npcTile, a.tile)
                .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b.tile)));

        if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

        if (targets.Count == 0)
        {
            Log.Log($"[npc_forage_collect] {npcName}: no forage in radius={radius}", LogLevel.Info);
            return;
        }

        var forageTargets = targets.Select(t =>
            (new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y), t.itemId, t.itemName))
            .ToList();

        _follow.StartForageCollect(npcName, forageTargets, _inventory);
        Log.Log($"[npc_forage_collect] {npcName}: queued {targets.Count} forage targets", LogLevel.Info);
    }

    private static int ParseInt(JsonElement p, string key, int def, int min, int max)
    {
        if (p.ValueKind == JsonValueKind.Object &&
            p.TryGetProperty(key, out JsonElement el) &&
            el.TryGetInt32(out int v))
            return System.Math.Clamp(v, min, max);
        return def;
    }
}
```

---

## Task 2 — Update ModEntry.cs to pass dependencies to ForageCollectHandler

**Files:**
- Modify: `smapi-mod/ModEntry.cs:135`

- [ ] **Update ForageCollectHandler construction**

In `smapi-mod/ModEntry.cs`, replace line 135:
```csharp
// Before:
new ForageCollectHandler(this.Monitor, showBubble),

// After:
new ForageCollectHandler(this.Monitor, showBubble, _npcInventory, _follow),
```

---

## Task 3 — Add ForageCollect mode + state to FollowSystem.cs

**Files:**
- Modify: `smapi-mod/Movement/FollowSystem.cs`

We need three changes in this file.

- [ ] **Step 3a: Add ForageCollect to the NpcBehaviorMode enum**

In `FollowSystem.cs`, find the enum block (lines 32–44):
```csharp
internal enum NpcBehaviorMode
{
    Idle,
    Summoning,
    Following,
    Leading,
    Wander,
    ClearDebris,
    DepositItems,
    DeliverItems,
    TillSoil,
    ApproachAndSpeak,
}
```

Add `ForageCollect,` after `ClearDebris,`:
```csharp
internal enum NpcBehaviorMode
{
    Idle,
    Summoning,
    Following,
    Leading,
    Wander,
    ClearDebris,
    ForageCollect,
    DepositItems,
    DeliverItems,
    TillSoil,
    ApproachAndSpeak,
}
```

- [ ] **Step 3b: Add ForageCollect state fields to NpcBehaviorState**

In `FollowSystem.cs`, find the comment `// ApproachAndSpeak: walk to player and emote.` (after the TillSoil fields, around line 84). Insert the ForageCollect fields before the ApproachAndSpeak block:

```csharp
        // ForageCollect: walk to spawned objects and pick them up.
        public Queue<(Point Tile, string ItemId, string ItemName)>? ForageQueue    { get; set; }
        public (Point Tile, string ItemId, string ItemName)          ForageTarget  { get; set; }
        public NpcInventory?                                          ForageInventory { get; set; }
        public bool                                                   ForagePathed  { get; set; }
```

- [ ] **Step 3c: Add the StartForageCollect() method**

Find the `StartClearDebris()` method. After the closing brace of `StartClearDebris()`, add:

```csharp
/// <summary>
/// Queue up a list of forage targets for the NPC to walk to, pick up, and
/// store in <paramref name="inventory"/>, emitting a ws event per item.
/// </summary>
public void StartForageCollect(
    string npcName,
    IEnumerable<(Point, string, string)> targets,
    NpcInventory inventory)
{
    var st = this.GetOrCreate(npcName);
    st.ForageQueue    = new Queue<(Point, string, string)>(targets);
    st.ForageInventory = inventory;
    st.ForagePathed   = false;

    if (st.ForageQueue.Count == 0)
    {
        _log.Log($"[FollowSystem/ForageCollect] {npcName}: no targets, nothing to do", LogLevel.Debug);
        return;
    }

    st.ForageTarget = st.ForageQueue.Dequeue();
    st.Mode         = NpcBehaviorMode.ForageCollect;
    st.LastPathTick = 0;
    _log.Log($"[FollowSystem/ForageCollect] {npcName}: started, {st.ForageQueue.Count + 1} targets", LogLevel.Debug);
}
```

- [ ] **Step 3d: Add a BroadcastEvent delegate field to FollowSystem**

`FollowSystem` only receives `IMonitor` today (constructor on line 119). Events are pushed via `_ws.BroadcastEvent(name, data)` elsewhere in the mod (e.g., `ModEntry.cs`). We inject a delegate instead of `WebSocketServer` directly to avoid the ordering issue (`_follow` is constructed before `_ws`).

In `FollowSystem.cs`, add the field next to `_log`:
```csharp
private readonly IMonitor _log;
private Func<string, object?, Task>? _broadcastEvent;  // ← add this line
```

Add a setter so `ModEntry` can wire it up after `_ws` is created:
```csharp
/// <summary>
/// Wire in the ws broadcast function after WebSocketServer is created.
/// Called from ModEntry once _ws is ready.
/// </summary>
public void SetBroadcast(Func<string, object?, Task> broadcast)
{
    _broadcastEvent = broadcast;
}
```

- [ ] **Step 3e: Add the TickForageCollect() method**

Find `TickClearDebris()`. After its closing brace, add:

```csharp
private void TickForageCollect(NPC npc, string npcName, NpcBehaviorState st)
{
    var location = npc.currentLocation;
    if (location is null || st.ForageQueue is null || st.ForageInventory is null)
    {
        st.Mode = NpcBehaviorMode.Idle;
        return;
    }

    (Point target, string itemId, string itemName) = st.ForageTarget;
    var targetV2 = new Vector2(target.X, target.Y);

    float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
    bool pathDone = npc.controller == null
                    || npc.controller.pathToEndPoint == null
                    || npc.controller.pathToEndPoint.Count == 0;

    if (dist <= 1.5f && pathDone)
    {
        if (location.Objects.ContainsKey(targetV2))
        {
            location.Objects.Remove(targetV2);
            st.ForageInventory.Add(npcName, itemId, 1);
            npc.doEmote(20); // heart emote — picking something up
            _log.Log(
                $"[FollowSystem/ForageCollect] {npcName}: collected {itemName} ({itemId}) " +
                $"at ({target.X},{target.Y})",
                LogLevel.Debug);

            // Push ws event so Hermes can react in real time.
            _broadcastEvent?.Invoke("forage_collected", new
            {
                npc       = npcName,
                item_id   = itemId,
                item_name = itemName,
                quantity  = 1,
                tile_x    = target.X,
                tile_y    = target.Y,
                location  = location.Name ?? "",
            });
        }
        else
        {
            _log.Log(
                $"[FollowSystem/ForageCollect] {npcName}: object at ({target.X},{target.Y}) " +
                $"already gone, skipping",
                LogLevel.Debug);
        }

        if (st.ForageQueue.Count == 0)
        {
            _log.Log($"[FollowSystem/ForageCollect] {npcName}: all targets done → Idle", LogLevel.Debug);
            st.Mode = NpcBehaviorMode.Idle;
            return;
        }

        st.ForageTarget = st.ForageQueue.Dequeue();
        st.ForagePathed = false;
        st.LastPathTick = 0;
        return;
    }

    if (!st.ForagePathed || npc.controller == null)
    {
        Point adjacent = new Point(target.X, target.Y + 1);
        bool ok = this.TryStartPath(npc, location, adjacent);
        if (!ok)
        {
            adjacent = new Point(target.X, target.Y - 1);
            ok = this.TryStartPath(npc, location, adjacent);
        }
        st.ForagePathed = ok;
        st.LastPathTick = _tickCounter > 0 ? _tickCounter : 1;

        _log.Log(
            $"[FollowSystem/ForageCollect] {npcName}: pathing to ({target.X},{target.Y}) ok={ok}",
            LogLevel.Debug);
    }
}
```

- [ ] **Step 3f: Wire ForageCollect into PumpOnGameTick dispatch**

In `FollowSystem.cs`, find the `switch (st.Mode)` block in `PumpOnGameTick()`. Add the `ForageCollect` case right after `ClearDebris`:

```csharp
case NpcBehaviorMode.ClearDebris:
    this.TickClearDebris(npc, name, st);
    break;
case NpcBehaviorMode.ForageCollect:
    this.TickForageCollect(npc, name, st);
    break;
```

---

## Task 4 — Wire SetBroadcast in ModEntry.cs

**Files:**
- Modify: `smapi-mod/ModEntry.cs`

`_follow` (line 94) is created before `_ws` (line 156). We need to call `_follow.SetBroadcast(...)` after `_ws` is ready.

- [ ] **Call SetBroadcast after WebSocketServer is created**

In `smapi-mod/ModEntry.cs`, find where `_ws` is assigned (around line 156):
```csharp
_ws = new WebSocketServer(prefix, _router, this.Monitor);
```

Add the following line immediately after:
```csharp
_follow.SetBroadcast((name, data) => _ws.BroadcastEvent(name, data));
```

---

## Task 5 — Add Go tests for npc_forage_collect

**Files:**
- Modify: `smartnpc-mcp/adapters/stardew/tools/npc_world_action_test.go`

- [ ] **Add two tests after the last existing test in the file**

Append to `npc_world_action_test.go`:

```go
// ── npc_forage_collect ────────────────────────────────────────────

func TestNpcForageCollect_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_forage_collect",
		Arguments: map[string]any{"npc": "XiaMi", "radius": 8, "max_count": 5},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcForageCollectOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcForageCollect_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_forage_collect",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}
```

- [ ] **Run the Go tests**

```cmd
cd D:\SmartNPC\smartnpc-mcp && go test -run TestNpcForageCollect ./adapters/stardew/tools/...
```

Expected output: both tests PASS.

- [ ] **Run the full test suite**

```cmd
C:\Users\synchen\go\bin\task.exe ci-fast
```

Expected: all checks pass.

---

## Task 6 — Document forage_collected event in docs/events.md

**Files:**
- Modify: `docs/events.md`

- [ ] **Add to the status summary table**

In `docs/events.md`, find the status summary table (around line 40–52). Add a new row:

```markdown
| `forage_collected` | mod | ✅ implemented |
```

- [ ] **Add the full event schema**

In `docs/events.md`, find the `## Mod events` section. After the `group_create` section (and before `## Reserved mod events`), add:

```markdown
### `forage_collected`

Emitted by `smapi-mod/Movement/FollowSystem.cs::TickForageCollect` each time
an NPC successfully removes a forage object from the map and stores it in their
NPC backpack. One event per item collected.

Emitted after `location.Objects.Remove(tile)` succeeds — the object is already
gone from the world when the event fires.

| field | type | notes |
|---|---|---|
| `npc` | string | NPC internal name, e.g. `"XiaMi"` |
| `item_id` | string | SDV qualified item id, e.g. `"(O)281"` (Morel) |
| `item_name` | string | Display name, e.g. `"Morel"` |
| `quantity` | int | always `1` per event |
| `tile_x` | int | tile X where the forage was collected |
| `tile_y` | int | tile Y where the forage was collected |
| `location` | string | SDV map name, e.g. `"Forest"` |

**Hermes rendering** (`events.FormatForHermes`):  
`NPC XiaMi collected Morel at (42,17) in Forest.`  
(Add a case to `adapters/stardew/events/format.go` when wiring hermesrelay formatting.)
```

---

## Task 7 — Full CI verification

- [ ] **Run full CI**

```cmd
C:\Users\synchen\go\bin\task.exe ci
```

Expected: profiles:verify ✅, lint ✅, test ✅, build ✅.

If `task ci` fails on the mod build due to C# compile errors, fix them and re-run. Common issues:
- Missing `using System.Text.Json;` import in `FollowSystem.cs` (add at the top)
- Tuple syntax mismatch — ensure `(Point Tile, string ItemId, string ItemName)` is consistent across `NpcBehaviorState`, `StartForageCollect` signature, and `TickForageCollect` destructure
- `_eventPush` not defined — see Task 4

---

## Spec coverage self-check

| Spec section | Covered by task |
|---|---|
| Go tool interface (structs + registration) | Already exists; Task 5 adds tests |
| ws event format `forage_collected` | Task 3d (emit), Task 6 (docs) |
| C# state machine: ForageCollect mode | Task 3a–3e |
| C# Handler Execute() + scan | Task 1 |
| ModEntry.cs wiring | Task 2 |
| Test: basic end-to-end | Task 5 |
| Test: empty NPC rejected | Task 5 |
| docs/events.md update | Task 6 |
