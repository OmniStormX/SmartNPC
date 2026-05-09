// npc_delegate local tool.
//
// npc_delegate synchronously forwards a request to another NPC agent and
// waits for its reply, returning the text as a structured tool result. This
// lets NPC A consult NPC B's knowledge / opinion inside a single decision
// turn and weave the response into its own reply.
//
// Safety rails:
//   - max chain length bounded by maxDelegateDepth to prevent unbounded
//     A→B→C→D recursion.
//   - self-delegation rejected up front.
//   - delegateTimeout caps a single hop so a stuck callee cannot stall the
//     caller's LLM round-trip indefinitely.
//
// Depth accounting: a fresh agent turn starts at delegateDepth == 0 (reset
// by respondAndSay). Each synchronous forward bumps the callee's counter
// by one while the remote respond() runs, then restores the previous value
// on return. A callee whose depth equals maxDelegateDepth may no longer
// delegate further.

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// maxDelegateDepth bounds the number of chained npc_delegate hops.
	maxDelegateDepth = 2
	// delegateTimeout caps a single delegation round so one slow NPC can't
	// tie up the caller's decision stage indefinitely.
	delegateTimeout = 30 * time.Second
)

// delegateResponse is the JSON shape returned to the decision LLM as the
// npc_delegate tool result. Kept flat (no nested objects) so models with
// weaker JSON parsers can still extract the reply text.
type delegateResponse struct {
	OK    bool   `json:"ok"`
	From  string `json:"from,omitempty"`
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// marshalDelegateResponse formats a delegateResponse as compact JSON. On
// marshal failure we fall back to a minimal error payload so the caller's
// tool-result message is always valid JSON.
func marshalDelegateResponse(r delegateResponse) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"ok":false,"error":"marshal failed"}`
	}
	return string(b)
}

// handleNpcDelegate implements the npc_delegate local tool. It synchronously
// forwards the request to the target agent and blocks until the target's
// respond() returns (bounded by delegateTimeout). The result is a JSON
// payload that the caller's decision LLM can incorporate into its own reply.
//
// Errors are returned as ok=false tool results rather than Go errors so the
// decision LLM can observe and recover from them (e.g. fall back to chat_say
// directly).
func (a *Agent) handleNpcDelegate(args map[string]any) string {
	to, _ := args["to"].(string)
	request, _ := args["request"].(string)
	to = strings.TrimSpace(to)
	if to == "" || request == "" {
		return marshalDelegateResponse(delegateResponse{Error: "missing required fields: to, request"})
	}

	a.mu.Lock()
	self := a.cfg.Speaker
	r := a.router
	depth := a.delegateDepth
	logger := a.cfg.Logger
	a.mu.Unlock()

	if strings.EqualFold(to, self) {
		return marshalDelegateResponse(delegateResponse{Error: "cannot delegate to self"})
	}

	if depth >= maxDelegateDepth {
		return marshalDelegateResponse(delegateResponse{
			Error: fmt.Sprintf("delegate depth %d reached max %d; cannot chain further", depth, maxDelegateDepth),
		})
	}

	if r == nil {
		return marshalDelegateResponse(delegateResponse{Error: "router not configured"})
	}

	target := r.GetAgent(to)
	if target == nil {
		return marshalDelegateResponse(delegateResponse{Error: fmt.Sprintf("unknown NPC %q", to)})
	}

	// Push the target's depth forward so its own npc_delegate sees the
	// correct chain length. Restore on exit so a later independent turn on
	// the target starts at depth 0 again.
	newDepth := depth + 1
	target.mu.Lock()
	prevDepth := target.delegateDepth
	target.delegateDepth = newDepth
	targetSpeaker := target.cfg.Speaker
	target.mu.Unlock()
	defer func() {
		target.mu.Lock()
		target.delegateDepth = prevDepth
		target.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), delegateTimeout)
	defer cancel()

	prompt := fmt.Sprintf("[来自 %s 的委托请求] %s\n请基于你的人设和知识，简短地给出回应（1-3 句）。", self, request)
	logger.Info("npc_delegate begin",
		"from", self, "to", targetSpeaker,
		"depth", newDepth, "request", truncateStr(request, 200))

	reply, err := target.respond(ctx, prompt)
	if err != nil {
		logger.Warn("npc_delegate target failed", "to", targetSpeaker, "err", err)
		return marshalDelegateResponse(delegateResponse{
			From:  targetSpeaker,
			Error: fmt.Sprintf("target responded with error: %v", err),
		})
	}

	logger.Info("npc_delegate reply",
		"from", self, "to", targetSpeaker,
		"reply", truncateStr(reply, 200))
	return marshalDelegateResponse(delegateResponse{
		OK:    true,
		From:  targetSpeaker,
		Reply: reply,
	})
}
