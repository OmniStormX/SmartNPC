// Package chatadapter bridges the chat package's per-NPC Router to the
// group package's AgentRouter contract. It lives outside both packages to
// avoid any circular import: chat doesn't need to know about group, and
// group doesn't need to import chat.
//
// Usage:
//
//	r := chat.NewRouter()
//	... r.Register(...)
//	orch := group.NewOrchestrator(chatadapter.New(r), group.DefaultConfig(), logger)
package chatadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/smartnpc/smartnpc-agent/internal/agent/chat"
	"github.com/smartnpc/smartnpc-agent/internal/group"
)

// Adapter implements group.AgentRouter on top of a chat.Router.
type Adapter struct {
	router *chat.Router
}

// New constructs a chatadapter.Adapter. router must not be nil.
func New(router *chat.Router) *Adapter {
	return &Adapter{router: router}
}

// ListAgents satisfies group.AgentRouter. Returns the speaker names in
// router registration order so prompts and tests stay deterministic.
func (a *Adapter) ListAgents() []string {
	if a.router == nil {
		return nil
	}
	return a.router.Speakers()
}

// PromptInGroup satisfies group.AgentRouter. It looks up the named NPC's
// chat.Agent and delegates to HandleInternalQuery (the F3 internal-query
// pipeline) — which is exactly the right shape: it runs the full Dual-LLM
// path on a scratch history that doesn't pollute the NPC's persistent
// 1-on-1 conversation.
//
// The synthetic FromAgent is "group:<lastSpeaker>" so the persona model
// can choose to acknowledge the addresser; lastMsg.Content is folded in
// as Context (separately from the orchestrator-rendered groupPrompt) so
// the persona layer sees both the rich room view AND the literal new line.
func (a *Adapter) PromptInGroup(ctx context.Context, npcName string, groupPrompt string, lastMsg group.GroupMessage) (string, error) {
	if a.router == nil {
		return "", errors.New("chatadapter: nil router")
	}
	agent := a.router.LookupAgent(npcName)
	if agent == nil {
		return "", fmt.Errorf("chatadapter: unknown agent %q", npcName)
	}

	from := lastMsg.Speaker
	if from == group.SpeakerPlayer {
		from = "Player"
	}
	resp, err := agent.HandleInternalQuery(ctx, chat.InternalQuery{
		FromAgent: from,
		Question:  groupPrompt,
		Context:   lastMsg.Content,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Answer, nil
}
