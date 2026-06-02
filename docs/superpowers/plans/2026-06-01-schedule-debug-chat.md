# Schedule Debug Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `--log-level debug` is active, push `npc_plan_day` result summaries to the corresponding NPC's in-game chat panel via the existing `DebugLogIncomingRequests` mirror.

**Architecture:** `registerNpcSchedule` gains access to the ws bridge client and a debug flag. After a successful `npc_plan_day`, it calls `br.CallAs(ctx, npc, "chat_say", ...)` for each target NPC. The mod already mirrors all inbound requests (including `chat_say`) to the debug chat panel when `DebugLogIncomingRequests` is enabled — and `chat_say` has a registered handler so the message also appears as a real chat bubble. No C# changes needed.

**Tech Stack:** Go 1.25, `github.com/modelcontextprotocol/go-sdk/mcp`, existing `bridge.WSClient`

---

### Task 1: Extend `registerNpcSchedule` signature

**Files:**
- Modify: `smartnpc-mcp/adapters/stardew/tools/npc_schedule.go:73`
- Modify: `smartnpc-mcp/adapters/stardew/tools/registry.go:51`

- [ ] **Step 1: Update `registerNpcSchedule` signature to accept bridge + debug flag**

In `npc_schedule.go`, change the function signature and add imports:

```go
import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
)

// registerNpcSchedule mounts schedule tools on the MCP server.
// sched is the shared Scheduler instance; if nil, a new one is created
// (useful for tests). The same scheduler must be wired into the event
// router so game_time_tick can call sched.Tick().
//
// br + debug: when both are non-nil/true, npc_plan_day pushes a concise
// summary to the NPC's in-game chat panel via chat_say (same pattern as
// schedDebug in main.go's schedule_trigger handler).
func registerNpcSchedule(s *mcp.Server, sched *scheduler.Scheduler, br *bridge.WSClient, debug bool) {
```

- [ ] **Step 2: Update call site in `registry.go`**

In `registry.go` line 51, pass `br` and a new `debug` parameter:

```go
func RegisterAll(s *mcp.Server, br *bridge.WSClient, hermes bridge.EventHandler, chatGuard *ChatSayGuard, logger *slog.Logger, schedDebug bool) *scheduler.Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	sched := scheduler.New()

	registerNpcMessage(s, logger, hermes)
	// Schedule tools — NPC daily planning.
	registerNpcSchedule(s, sched, br, schedDebug)
```

- [ ] **Step 3: Update `main.go` call site to pass `schedDebug`**

In `cmd/smartnpc-mcp/main.go` line 184, the current call is:

```go
dayScheduler := tools.RegisterAll(server, br, hermesHandler, chatGuard, logger)
```

Change to:

```go
dayScheduler := tools.RegisterAll(server, br, hermesHandler, chatGuard, logger, *logLevel == "debug")
```

- [ ] **Step 4: Update test helper `newScheduleClientServer` to pass nil/false**

In `npc_schedule_test.go` line 21:

```go
registerNpcSchedule(server, sched, nil, false)
```

- [ ] **Step 5: Run tests to verify signature changes compile**

Run: `cd D:\SmartNPC\smartnpc-mcp && go build ./...`
Expected: clean build, no errors.

---

### Task 2: Push debug summary after `npc_plan_day` success

**Files:**
- Modify: `smartnpc-mcp/adapters/stardew/tools/npc_schedule.go:106-174` (inside the handler closure)

- [ ] **Step 1: Add debug push logic after the success return in `npc_plan_day` handler**

Inside the `npc_plan_day` handler closure, after the `slog.Info("npc_plan_day result", ...)` line (line 170) and before `return nil, out, nil`, add:

```go
		// Debug mode: push concise summary to each target NPC's chat panel.
		if debug && br != nil {
			for _, npc := range targets {
				msg := fmt.Sprintf("[plan] stored %d entries, day %d %s", len(entries), in.Day, in.Season)
				go br.CallAs(context.Background(), npc, bridge.ActionChatSay, map[string]any{
					"npc":  npc,
					"text": msg,
				})
			}
		}
```

Note: `go` goroutine because `CallAs` is fire-and-forget — we don't block the tool response on the debug push. `context.Background()` because the tool's context may be cancelled after return.

- [ ] **Step 2: Run existing schedule tests**

Run: `cd D:\SmartNPC\smartnpc-mcp && go test -run TestNpc ./adapters/stardew/tools/...`
Expected: all PASS (debug=false in tests, no bridge calls attempted).

- [ ] **Step 3: Run full test suite**

Run: `cd D:\SmartNPC\smartnpc-mcp && go test ./...`
Expected: all PASS.

- [ ] **Step 4: Run CI**

Run: `C:\Users\synchen\go\bin\task.exe ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add smartnpc-mcp/adapters/stardew/tools/npc_schedule.go smartnpc-mcp/adapters/stardew/tools/registry.go smartnpc-mcp/adapters/stardew/tools/npc_schedule_test.go smartnpc-mcp/cmd/smartnpc-mcp/main.go
git commit -m "feat(mcp): push npc_plan_day summary to chat panel in debug mode

When --log-level debug, npc_plan_day success pushes a concise
[plan] stored N entries, day D season line to the NPC's game chat
panel via CallAs chat_say. Reuses the existing DebugLogIncomingRequests
mirror on the mod side — no C# changes needed."
```

---

## Notes

- **C# zero-change**: The mod's `DebugLogIncomingRequests` already mirrors all inbound requests to the NPC chat panel (gated by config.json). The `chat_say` action has a registered handler so the message is also rendered as a real chat bubble. Both surfaces show the debug info — no new mod code needed.
- **Goroutine safety**: `br.CallAs` is thread-safe (WSClient uses internal mutex). The `go` dispatch avoids blocking the MCP tool response on the ws round-trip.
- **Existing `schedule_trigger` debug**: unchanged — still handled in `main.go`'s `makeRouter` with the same `schedDebug` condition. The two debug push sites are independent: one fires at plan-time, the other at trigger-time.
