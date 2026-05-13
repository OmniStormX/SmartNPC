package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/smartnpc/smartnpc-agent/internal/agent/chat"
	"github.com/smartnpc/smartnpc-agent/internal/group"
	"github.com/smartnpc/smartnpc-agent/internal/group/chatadapter"
)

// groupHandlerAdapter bridges the chat.GroupHandler interface to the real
// group.Orchestrator. It lives in cmd because it ties together packages
// that don't know about each other.
type groupHandlerAdapter struct {
	orch *group.Orchestrator
}

func (g *groupHandlerAdapter) CreateGroup(participants []string) (string, error) {
	return g.orch.CreateGroup(participants)
}

func (g *groupHandlerAdapter) OnPlayerMessage(_ context.Context, groupID, text string) {
	// The caller's ctx is bound to the MCP LoggingMessageHandler invocation
	// and gets cancelled the moment that handler returns. The orchestrator
	// dispatches replies on goroutines with staggered delays (1-4s), so we
	// must detach to a fresh context here — otherwise every dispatched
	// goroutine hits <-ctx.Done() inside dispatchAsync and bails before
	// running PromptInGroup.
	g.orch.OnMessage(context.Background(), groupID, group.GroupMessage{
		Speaker: group.SpeakerPlayer,
		Content: text,
	})
}

// setupGroupChat wires the group orchestrator into the router and returns
// the orchestrator (for potential future use like /group-leave commands).
// session is used for chat_say callbacks when NPCs reply in group.
func setupGroupChat(router *chat.Router, session *mcp.ClientSession, logger *slog.Logger) *group.Orchestrator {
	adapter := chatadapter.New(router)

	cfg := group.DefaultConfig()
	cfg.OnNPCReply = func(ctx context.Context, groupID, npcName, reply string) {
		if session == nil {
			return
		}
		// Send the NPC's group reply to the game via chat_say with
		// channel=group so the mod routes it exclusively to the group panel,
		// without polluting the NPC's private 1-on-1 history.
		_, _ = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "chat_say",
			Arguments: map[string]any{
				"speaker":  npcName,
				"text":     reply,
				"channel":  "group",
				"group_id": groupID,
			},
		})
	}

	orch := group.NewOrchestrator(adapter, cfg, logger)

	// Wire the orchestrator into the router's event dispatch.
	router.SetGroupHandler(&groupHandlerAdapter{orch: orch})

	return orch
}

// marshalJSON is a tiny helper for building event payloads.
func marshalJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
