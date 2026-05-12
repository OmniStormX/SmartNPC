// NPC-to-NPC delegation chain.
//
// "Delegate" / "consult" lets one agent ask another agent a question and
// fold the answer into its own tool-calling loop. The intent: when a player
// asks NPC A a question that lives in NPC B's domain (Harvey is the doctor,
// Penny knows the schoolchildren, etc.), A's decision layer can call
// `consult_npc(B, question)` and treat the reply as a tool result.
//
// Design constraints:
//
//   1. Recursion safety. The delegate chain is carried in the request
//      context so every nested call sees the full ancestry. We bail out
//      with a soft fallback when:
//        - len(chain) >= MaxDepth (no further hops), or
//        - the requested target is already on the chain (cycle).
//
//   2. Bounded latency. A consultation is wrapped in a 10-second timeout
//      separate from the parent call's deadline, so a slow B can't stall A
//      indefinitely.
//
//   3. No history pollution. The consulted agent runs the question on a
//      *temporary* history snapshot that does NOT mutate the persistent
//      conversation. Internal queries are private to the asker.
//
//   4. Same Dual-LLM pipeline. Internal queries flow through the standard
//      respond() / respondDual() path (with a fresh, scoped Agent clone)
//      so they benefit from tools, friendship calibration, etc. The reply
//      is the persona-stage text — that's what A actually wants to hear.

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
	"github.com/smartnpc/smartnpc-agent/internal/memory"
)

const (
	// MaxDelegateDepth caps how many hops a single chain can take. Depth 0
	// is the player's original turn (no delegation yet); depth >= MaxDelegateDepth
	// blocks any further consult_npc call. With MaxDelegateDepth = 2 we allow
	// player → A → B but not player → A → B → C.
	MaxDelegateDepth = 2

	// DefaultDelegateTimeout caps a single ConsultAgent call. The decision
	// layer's overall budget (cfg.Timeout, typically 90s) is much larger;
	// this lets one slow peer fail fast without dragging the parent turn
	// down. Sized for dual-LLM consultees: decision stage ≈ 5-10s + persona
	// stage ≈ 5-15s, so a flat 10s budget would starve persona every time
	// (observed: persona stage failing with "context deadline exceeded"
	// right after decision returned). 45s gives ~2× the typical end-to-end
	// pipeline so slow tool chains still fit.
	DefaultDelegateTimeout = 45 * time.Second

	// ConsultToolName is the synthetic tool name surfaced to the decision
	// layer. It is intercepted in executeTool — never routed to the real MCP
	// server.
	ConsultToolName = "consult_npc"
)

// DelegateRequest is the structured form of a consult_npc invocation.
type DelegateRequest struct {
	From      string // the agent issuing the consultation
	To        string // the agent being consulted
	Question  string // the question text passed verbatim to the consulted agent
	Context   string // optional reason / extra context for the consulted agent
	MaxTokens int    // forwarded as a soft cap; 0 → use the consulted agent's default
}

// DelegateResponse is the structured form of consult_npc's return value.
type DelegateResponse struct {
	Answer    string   // the consulted agent's persona-stage reply
	Consulted string   // the speaker name actually answered
	ToolsUsed []string // tool names invoked by the consulted agent (best-effort, may be empty)
}

// InternalQuery is the request shape passed to Agent.HandleInternalQuery.
// It mirrors DelegateRequest but is scoped to the receiving agent.
type InternalQuery struct {
	// FromAgent is the speaker that originated the consultation. Surfaces
	// inside the consulted agent's user message so its persona can choose to
	// address the asker directly.
	FromAgent string
	// Question is the literal question to answer.
	Question string
	// Context optionally explains why the question is being asked.
	Context string
	// MaxTokens optionally clamps the persona-stage output.
	MaxTokens int
}

// InternalResponse is the result of HandleInternalQuery.
type InternalResponse struct {
	Answer    string
	ToolsUsed []string
}

// ── recursion safety ───────────────────────────────────────────────────────

// delegateChainKey is a private context key that carries the chain of
// speaker names visited so far. Stored under a typed key so unrelated
// context.WithValue calls can't accidentally collide.
type delegateChainKey struct{}

// chainFromContext returns the delegate chain attached to ctx, or nil when
// the call is at the top of the dispatch (no consult yet).
func chainFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(delegateChainKey{}).([]string)
	return v
}

// withChain returns a child context whose delegate chain is parent + appended.
// The returned slice shares no storage with the parent so concurrent siblings
// can't accidentally observe each other's appends.
func withChain(parent context.Context, append1 string) context.Context {
	prev := chainFromContext(parent)
	next := make([]string, 0, len(prev)+1)
	next = append(next, prev...)
	next = append(next, append1)
	return context.WithValue(parent, delegateChainKey{}, next)
}

// containsCI reports whether s is in chain (case-insensitive comparison —
// speaker names may differ in casing across the player and the registry).
func containsCI(chain []string, s string) bool {
	target := normalizeSpeaker(s)
	for _, v := range chain {
		if normalizeSpeaker(v) == target {
			return true
		}
	}
	return false
}

// ── delegate fallback / errors ─────────────────────────────────────────────

// ErrDelegateMaxDepth is returned (and surfaced to the caller as a tool
// "result") when a consult would push the chain past MaxDelegateDepth.
var ErrDelegateMaxDepth = errors.New("delegate chain too deep")

// ErrDelegateCycle is returned when the requested target is already on the
// chain — preventing A → B → A or longer loops.
var ErrDelegateCycle = errors.New("delegate cycle detected")

// ErrDelegateUnknownTarget is returned when no agent matches the requested
// target speaker.
var ErrDelegateUnknownTarget = errors.New("unknown target agent")

// ErrDelegateNoRouter is returned when an agent tries to consult but is not
// attached to a router.
var ErrDelegateNoRouter = errors.New("agent not attached to a router")

// fallbackAnswer renders a short polite reply that the asking agent can
// re-paraphrase. We avoid leaking error machinery to the persona layer.
func fallbackAnswer(target string, cause error) string {
	return fmt.Sprintf("（无法咨询 %s：%v）", target, cause)
}

// attachRouter wires the back-reference from the agent to the router it
// was registered with. Stored on the Agent so consult_npc tool calls can
// reach peers via Router.ConsultAgent. Idempotent — re-registering an
// agent updates the pointer atomically under Agent.mu.
func (a *Agent) attachRouter(r *Router) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.router = r
	a.mu.Unlock()
}

// HandleInternalQuery answers a consult request from a peer agent. It runs
// the standard respond() / respondDual() pipeline on a *temporary* history
// snapshot — the persistent conversation is not mutated, so internal queries
// stay private to the asker.
//
// However, the consulted agent retains its recent history as read-only context
// so it can give informed answers (e.g. "I just told the player about X").
// The user message format embeds the asking agent's name + optional context
// so this agent's persona can respond appropriately.
func (a *Agent) HandleInternalQuery(ctx context.Context, q InternalQuery) (*InternalResponse, error) {
	if a == nil {
		return nil, ErrDelegateUnknownTarget
	}
	if a.cfg.Provider == nil && a.cfg.DecisionProvider == nil {
		return &InternalResponse{
			Answer: fallbackAnswer(a.cfg.Speaker, errors.New("no provider configured")),
		}, nil
	}

	prompt := q.Question
	if q.FromAgent != "" {
		prompt = "[来自 " + q.FromAgent + " 的咨询] " + prompt
	}
	if q.Context != "" {
		prompt = prompt + "\n[上下文] " + q.Context
	}

	// Save & swap in a scratch history that includes recent context so the
	// consulted NPC can give informed answers. We keep the last N messages
	// as read-only background, then append the consult question.
	a.mu.Lock()
	saved := a.history
	// Copy up to 10 recent messages as context (read-only snapshot).
	contextLen := len(saved)
	if contextLen > 10 {
		contextLen = 10
	}
	scratch := make([]llm.Message, contextLen, contextLen+2)
	copy(scratch, saved[len(saved)-contextLen:])
	a.history = scratch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.history = saved
		a.mu.Unlock()
	}()

	reply, err := a.respond(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &InternalResponse{Answer: reply}, nil
}

// executeConsult intercepts the synthetic consult_npc tool call and routes
// it to the back-referenced Router. Returns a JSON-encoded result string in
// the same shape the MCP path produces, so the LLM sees a uniform tool
// response.
//
// Skeleton landed with F2 so chat.go compiles while F3's consult-npc tool
// is fully implemented. Reads npc_name + question + context from the tool
// arguments map and calls Router.ConsultAgent.
func (a *Agent) executeConsult(ctx context.Context, tc llm.ToolCall) (string, error) {
	a.mu.Lock()
	router := a.router
	speaker := a.cfg.Speaker
	session := a.session
	a.mu.Unlock()
	if router == nil {
		return jsonConsultErr(fallbackAnswer("(unknown)", ErrDelegateNoRouter)), nil
	}

	target, _ := tc.Arguments["npc_name"].(string)
	question, _ := tc.Arguments["question"].(string)
	contextHint, _ := tc.Arguments["context"].(string)

	resp, err := router.ConsultAgent(ctx, speaker, target, question, contextHint)
	if err != nil {
		a.cfg.Logger.Info("consult_npc failed", "from", speaker, "to", target, "err", err)
		return jsonConsultErr(fallbackAnswer(target, err)), nil
	}
	a.cfg.Logger.Info("consult_npc success", "from", speaker, "to", target, "answer_len", len(resp.Answer))

	// Surface B's reply in-game so the player hears the consulted NPC
	// acknowledge the request (default behaviour for behavior-style delegation
	// where B executes a tool — chat_move, mail_send, etc. — and would
	// otherwise stay silent). A's persona layer still paraphrases via the
	// JSON tool result returned below.
	if session != nil && resp.Consulted != "" && strings.TrimSpace(resp.Answer) != "" {
		if _, sayErr := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "chat_say",
			Arguments: map[string]any{
				"speaker": resp.Consulted,
				"text":    resp.Answer,
			},
		}); sayErr != nil {
			a.cfg.Logger.Warn("consult_npc chat_say failed", "speaker", resp.Consulted, "err", sayErr)
		}
	}

	// Record the consultation in the asker's long-term memory so future
	// conversations can reference it (e.g. "I asked Harvey about this before").
	if a.memoryEnabled() && a.memory != nil && a.memory.store != nil {
		note := fmt.Sprintf("我咨询了 %s：「%s」。%s 回答：「%s」",
			resp.Consulted, question, resp.Consulted, resp.Answer)
		_ = a.memory.store.StoreMemory(memory.Memory{
			NPCName:    speaker,
			Category:   memory.CategoryEvent,
			Content:    note,
			Importance: 4,
		})
	}

	b, _ := json.Marshal(map[string]any{
		"ok":         true,
		"consulted":  resp.Consulted,
		"answer":     resp.Answer,
		"tools_used": resp.ToolsUsed,
	})
	return string(b), nil
}

// jsonConsultErr renders a tool-result envelope for failed consult calls so
// the persona layer reads a structured fallback rather than raw error text.
func jsonConsultErr(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "answer": msg})
	return string(b)
}

// ── consult_npc tool spec ──────────────────────────────────────────────────

// consultToolSpec is the JSON-schema spec advertised to the decision LLM.
// Importing it via Agent.Tools() keeps it in the same llm.ToolSpec list as
// the MCP-derived tools, so providers don't need a special case.
func consultToolSpec() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"npc_name": map[string]any{
				"type":        "string",
				"description": "Display name (or internal name) of the NPC to consult, e.g. \"Harvey\".",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The question to ask, phrased in the player's language.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional: why you're asking (e.g. who the player is, prior turn).",
			},
		},
		"required": []string{"npc_name", "question"},
	}
}
