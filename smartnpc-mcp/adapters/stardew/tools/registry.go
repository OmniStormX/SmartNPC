// Package tools registers Stardew Valley-specific MCP tools on the server.
//
// Framework-level tools (e.g. `ping`) live in pkg/agentbridge.RegisterMeta
// and are registered separately by the composition root (main.go) — keeping
// adapter-agnostic introspection in core means it stays available even when
// the SDV bridge is detached.
package tools

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// RegisterAll wires every Stardew-specific tool group onto the given server.
//
// br may be nil; mod-backed tools (chat_say, mail_send, game_*, npc_*) are
// then omitted so the inter-NPC messaging tools (which run entirely
// in-process) remain usable on a bridge-less deployment.
//
// hermes, when non-nil, is the same bridge.EventHandler used for forwarding
// mod events to the Hermes Gateway. Inter-NPC messaging tools fan their
// synthetic events through it so the recipient's Hermes profile is woken
// up immediately. Pass nil when running without a Hermes backend.
//
// chatGuard, when non-nil, enforces the per-wake-up chat_say budget — one
// per speaker in private (refreshed on each inbound event addressed to that
// NPC), one per (group, speaker) in group chat (refreshed on player input
// into the group). Pass nil to disable throttling (tests, or pure inter-NPC
// servers without the bridge). The same guard instance must be passed to
// the router so MaybeResetGuard fires the matching reset on each event.
//
// logger is used for non-fatal notification delivery failures in the
// inter-NPC message fan-out. When nil, slog.Default() is assumed.
//
// The returned *scheduler.Scheduler is the shared instance managing NPC
// daily schedules. The caller should wire it into the event router so
// game_time_tick events trigger sched.Tick(hour). Nil is never returned.
func RegisterAll(s *mcp.Server, br *bridge.WSClient, hermes bridge.EventHandler, chatGuard *ChatSayGuard, logger *slog.Logger, workflowReg *workflow.Registry, schedDebug bool) *scheduler.Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	sched := scheduler.New()

	// Workflow discovery / debug tools — independent of mod bridge.
	// workflow_run_inline is only registered when schedDebug is true.
	RegisterWorkflow(s, workflowReg, schedDebug)

	registerNpcMessage(s, logger, hermes)
	// Schedule tools — NPC daily planning. Independent of mod bridge.
	registerNpcSchedule(s, sched, br, workflowReg, schedDebug)

	if br != nil {
		// agent_register_self installs first so profiles see it at the top
		// of ListTools and the SOUL/policy can reference it as the very
		// first call to make on startup.
		registerAgent(s, br)
		registerMail(s, br)
		registerChat(s, br, chatGuard)
		registerGameQuery(s, br)
		registerNpcPerception(s, br)
		registerNpcMovement(s, br)
		registerNpcBehavior(s, br)
		registerPlayerQuery(s, br)
		// Rich NPC behavior tools forward to the Mod via WebSocket.
		// Mod side currently returns a stub bubble + ack; the wiring is
		// real, only the in-game effect is a placeholder.
		registerNpcWorldAction(s, br)
		registerNpcInventory(s, br)
		registerNpcSocialAction(s, br)
	}

	return sched
}
