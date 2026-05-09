package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/agent/chat"
	"github.com/smartnpc/smartnpc-agent/internal/scheduler"
)

// schedulerRouterAdapter adapts a *chat.Router to the scheduler.AgentRouter
// interface. The scheduler doesn't need the full router surface — only the
// list of registered NPC names and a way to ask one of them whether it
// wants to reach out to the player.
type schedulerRouterAdapter struct {
	router *chat.Router
}

func (a schedulerRouterAdapter) ListAgents() []string {
	return a.router.Speakers()
}

// TriggerProactive routes the scheduler-built decision prompt through the
// existing HandleInternalQuery pipeline. That path already runs the LLM
// on a scratch history (so it does not pollute the persistent
// conversation), reuses persona + tools, and returns the persona-stage
// reply text — exactly what the scheduler needs to parse for yes/no.
func (a schedulerRouterAdapter) TriggerProactive(ctx context.Context, npcName, prompt string) (string, error) {
	target := a.router.LookupAgent(npcName)
	if target == nil {
		return "", fmt.Errorf("scheduler: unknown agent %q", npcName)
	}
	resp, err := target.HandleInternalQuery(ctx, chat.InternalQuery{
		FromAgent: "scheduler",
		Question:  prompt,
	})
	if err != nil {
		return "", fmt.Errorf("scheduler: HandleInternalQuery: %w", err)
	}
	return resp.Answer, nil
}

// schedulerSessionAdapter adapts the MCP SDK's *mcp.ClientSession to the
// scheduler.MCPSession interface. The scheduler speaks a JSON-blob shape
// to keep its tool arguments untyped, so we marshal CallToolResult.
// StructuredContent (or text) into raw JSON before returning.
type schedulerSessionAdapter struct {
	session *mcp.ClientSession
}

func (s schedulerSessionAdapter) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if s.session == nil {
		return nil, fmt.Errorf("scheduler: no MCP session")
	}
	res, err := s.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		// Surface the mod-side error message so the scheduler's debug
		// logs are useful. The scheduler only cares that we returned
		// non-nil; the exact text becomes the wrapped error.
		return nil, fmt.Errorf("tool %s reported error: %v", name, res.Content)
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("scheduler: marshal structured: %w", err)
		}
		return b, nil
	}
	// Fall back to concatenated text content if the tool returned no
	// structured payload. Returns raw bytes the caller can json.Unmarshal.
	for _, c := range res.Content {
		if txt, ok := c.(*mcp.TextContent); ok && txt.Text != "" {
			return json.RawMessage(txt.Text), nil
		}
	}
	return json.RawMessage("{}"), nil
}

// newSchedulerAdapters bundles the two adapters. Returned as a
// constructor-style helper so cmd code can wire scheduler.New in one
// line.
func newSchedulerAdapters(router *chat.Router, session *mcp.ClientSession) (scheduler.AgentRouter, scheduler.MCPSession) {
	return schedulerRouterAdapter{router: router}, schedulerSessionAdapter{session: session}
}
