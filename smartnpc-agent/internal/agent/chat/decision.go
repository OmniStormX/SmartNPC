// Dual-LLM decision/persona pipeline.
//
// Motivation: local role-play backends (Hermes, etc.) excel at in-character
// Chinese dialogue but refuse to emit OpenAI-style tool_calls reliably.
// Meanwhile frontier tool-calling models (GPT-5.5) are strong at reasoning
// about which game action to take but lack the fine-grained persona voice we
// want for a Stardew NPC.
//
// The pipeline splits responsibilities:
//
//   1. Decision stage — DecisionProvider sees a compact instruction prompt
//      that lists the available MCP tools, current world state, and recent
//      dialogue, and is asked to DECIDE which actions to take. It runs a
//      bounded tool-call loop; the text it eventually produces is discarded.
//      Only the tool calls and their results are kept.
//
//   2. Persona stage — PersonaProvider sees the full persona system prompt,
//      the per-turn friendship/game-state context, the original player
//      message, AND a [System observations] block summarizing what actions
//      the decision layer already executed (semantically paraphrased so the
//      role-play model doesn't echo tool names back at the player). It is
//      called WITHOUT tools and produces the final reply.
//
// When DecisionProvider is nil, respond() runs the legacy single-model loop
// in chat.go; dual mode is strictly opt-in via Config.
//
// Error handling: decision-stage failures fall back to the single-LLM
// respond() path (using Config.Provider) whenever possible, so the player
// still gets a tool-aware reply. If the fallback Provider is also the
// PersonaProvider (common when callers opted into dual mode by only wiring
// DecisionProvider and letting PersonaProvider default to Provider), the
// persona stage just runs without observations.

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

const (
	// maxDecisionRounds caps the tool-calling loop in the decision stage to
	// keep a single chat turn bounded. 3 is enough for: one read (e.g.
	// npc_get_nearby) → one action (e.g. npc_move_to) → one verification.
	maxDecisionRounds = 3
	// decisionMaxTokens is intentionally low — the decision layer only needs
	// to reason + emit tool_calls. Its final text is discarded.
	decisionMaxTokens = 400
	// personaMaxTokens keeps persona replies within typical dialogue-box size.
	personaMaxTokens = 300
)

// decisionSystemPromptTemplate is the base prompt fed to the decision layer.
// The variable {speaker} is substituted at runtime so the model knows which
// NPC it is steering. Additional state blocks (time/weather/friendship/
// position) are appended after substitution.
const decisionSystemPromptTemplate = `You are a Stardew Valley NPC behavior controller for "{speaker}".
Analyze the player's message and decide what actions to take.
You MUST use tool calls for physical actions (moving, looking around, checking surroundings).
If the message is purely conversational (greetings, small talk, questions about mood), call NO tools — a separate role-play model will answer.

Rules:
- Prefer read tools (npc_get_nearby, npc_get_environment, npc_get_position, friendship_get, game_get_time, game_get_weather) before committing to an action.
- Do NOT write dialogue. Do NOT role-play. Any text you return will be discarded; only tool_calls matter.
- When you are done, reply with the single word "done".`

// actionResult is one decision-stage tool invocation plus its response,
// serialized into the persona-stage context.
type actionResult struct {
	ToolName string         // MCP tool that was called
	Args     map[string]any // arguments echoed back for observability
	Output   string         // stringified tool result (JSON or text)
	Err      string         // non-empty when the call failed
}

// respondDual runs the decision → persona pipeline. See the file-level
// doc-comment for the architectural rationale.
func (a *Agent) respondDual(ctx context.Context, userText string) (string, error) {
	a.mu.Lock()
	decision := a.cfg.DecisionProvider
	persona := a.cfg.PersonaProvider
	fallback := a.cfg.Provider
	a.mu.Unlock()

	if decision == nil {
		return "", fmt.Errorf("respondDual called without DecisionProvider")
	}
	if persona == nil {
		// PersonaProvider is normally filled in New() from Provider; this
		// belt-and-suspenders check keeps the pipeline safe to call from
		// tests that wire the struct by hand.
		return "", fmt.Errorf("respondDual called without PersonaProvider")
	}

	// Pre-fetch the same contextual addenda the single-model path uses, so
	// the persona stage has identical ambient awareness. Also consumed by
	// the decision system prompt so the reasoner knows world state before
	// picking tools.
	friendshipCtx := a.getFriendshipContext(ctx)
	gameStateCtx := a.getGameStateContext(ctx)
	memoryCtx := a.memoryContextAddendum()

	// Open / reuse the persistent conversation up front so every stage's
	// mirrored messages share the same id.
	convID, err := a.memoryStartTurn()
	if err != nil {
		a.cfg.Logger.Warn("memory start turn failed", "err", err)
	}

	// All tool invocations are delegated to the decision LLM's judgment.
	// No keyword-based pre-processing — the LLM decides what actions to take.
	effectiveUserText := userText

	// Mirror the (annotated) user turn before any LLM call.
	a.memoryAppend(convID, "user", effectiveUserText, nil)

	// ── Stage 1: decision ────────────────────────────────────────
	results, decisionErr := a.runDecisionStage(ctx, decision, effectiveUserText, friendshipCtx, gameStateCtx)
	if decisionErr != nil {
		a.cfg.Logger.Warn("decision stage failed", "err", decisionErr)
		// Graceful degradation: if a distinct single-LLM Provider is
		// configured (i.e. not the same object as PersonaProvider),
		// replay the turn on it so the player still gets a tool-aware
		// response. Otherwise fall through to persona-only mode so we
		// at least produce some in-character reply.
		if fallback != nil && fallback != persona {
			a.cfg.Logger.Info("falling back to single-LLM respond()", "err", decisionErr)
			// Temporarily drop the dual config so respond() stays on the
			// legacy path for this one call, then restore it.
			a.mu.Lock()
			saved := a.cfg.DecisionProvider
			a.cfg.DecisionProvider = nil
			a.mu.Unlock()
			defer func() {
				a.mu.Lock()
				a.cfg.DecisionProvider = saved
				a.mu.Unlock()
			}()
			return a.respond(ctx, userText)
		}
	}

	a.cfg.Logger.Info("decision", "tool_calls", len(results))

	// ── Stage 2: persona ─────────────────────────────────────────
	reply, err := a.runPersonaStage(ctx, persona, effectiveUserText, friendshipCtx, gameStateCtx, memoryCtx, results)
	if err != nil {
		return reply, err
	}
	a.memoryAppend(convID, "assistant", reply, nil)
	a.memoryNoteTurn(convID)
	return reply, nil
}

// runDecisionStage feeds the user message + available tool specs to the
// decision LLM, iteratively executing any tool_calls it emits. Returns the
// accumulated action results (possibly empty) and the terminal error from
// the decision LLM, if any. Tool-execution errors are captured inside each
// actionResult instead of bubbling up; only transport / API errors surface.
func (a *Agent) runDecisionStage(
	ctx context.Context,
	decider llm.Provider,
	userText, friendshipCtx, gameStateCtx string,
) ([]actionResult, error) {
	a.mu.Lock()
	tools := a.toolSpecsLocked()
	model := a.cfg.DecisionModel
	speaker := a.cfg.Speaker
	history := a.snapshotHistory()
	a.mu.Unlock()

	sysPrompt := buildDecisionSystemPrompt(speaker, friendshipCtx, gameStateCtx)

	// Build a minimal message stack: runtime decision system prompt + recent
	// dialogue (so the model can resolve pronouns like "回到那里") + the new
	// player utterance.
	msgs := make([]llm.Message, 0, 2+len(history))
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: sysPrompt})
	msgs = append(msgs, history...)
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: userText})

	var results []actionResult
	for round := range maxDecisionRounds {
		resp, err := decider.Chat(ctx, llm.ChatRequest{
			Model:       model,
			Messages:    msgs,
			Tools:       tools,
			Temperature: 0, // use model default (GPT-5.5 only supports default=1)
			MaxTokens:   decisionMaxTokens,
		})
		if err != nil {
			return results, fmt.Errorf("decision round %d: %w", round, err)
		}
		if len(resp.ToolCalls) == 0 {
			return results, nil
		}

		// Log the structured tool_calls from the decision LLM for debugging.
		for i, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			a.cfg.Logger.Info("decision tool_call",
				"round", round,
				"index", i,
				"id", tc.ID,
				"name", tc.Name,
				"arguments", string(argsJSON),
			)
		}

		// Append the assistant's tool-call message so subsequent rounds can
		// reason about the prior calls; OpenAI-style dialogs require this.
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls})

		for _, tc := range resp.ToolCalls {
			out, err := a.executeTool(ctx, tc)
			ar := actionResult{ToolName: tc.Name, Args: tc.Arguments, Output: out}
			if err != nil {
				ar.Err = err.Error()
				a.cfg.Logger.Warn("decision tool failed", "tool", tc.Name, "err", err)
			}
			results = append(results, ar)
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				Content:    out,
				Name:       tc.Name,
				ToolCallID: tc.ID,
			})
			argsJSON, _ := json.Marshal(tc.Arguments)
			a.cfg.Logger.Info("decision tool result",
				"name", tc.Name,
				"call_id", tc.ID,
				"arguments", string(argsJSON),
				"success", err == nil,
				"result", truncateStr(out, 500),
			)
		}
	}
	a.cfg.Logger.Debug("decision stage exhausted rounds", "rounds", maxDecisionRounds, "calls", len(results))
	return results, nil
}

// buildDecisionSystemPrompt substitutes {speaker} and appends a short state
// block so the decision layer can condition on the world. Empty context
// strings are elided to keep the prompt compact.
func buildDecisionSystemPrompt(speaker, friendshipCtx, gameStateCtx string) string {
	p := strings.ReplaceAll(decisionSystemPromptTemplate, "{speaker}", speaker)
	var state []string
	if gameStateCtx != "" {
		state = append(state, gameStateCtx)
	}
	if friendshipCtx != "" {
		state = append(state, friendshipCtx)
	}
	if len(state) == 0 {
		return p
	}
	return p + "\n\nCurrent state:\n" + strings.Join(state, "\n")
}

// runPersonaStage asks PersonaProvider to produce the final reply given the
// full persona prompt, dynamic context, and decision-stage observations.
// Runs WITHOUT tools — the persona layer speaks, it doesn't act.
func (a *Agent) runPersonaStage(
	ctx context.Context,
	personaLLM llm.Provider,
	userText, friendshipCtx, gameStateCtx, memoryCtx string,
	results []actionResult,
) (string, error) {
	a.mu.Lock()
	model := a.cfg.PersonaModel
	// Append the player's (possibly annotated) message to history once,
	// here, so the persona stage's reply is correctly threaded even when the
	// decision stage fires no tools.
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: userText})
	a.trimHistory()
	// Build the extra system addendum: friendship + game state + memory + action log.
	extra := joinContextBlocks(friendshipCtx, gameStateCtx, memoryCtx, formatActionResults(results))
	msgs := a.buildMessages(extra)
	a.mu.Unlock()

	resp, err := personaLLM.Chat(ctx, llm.ChatRequest{
		Model:    model,
		Messages: msgs,
		// Intentionally omit Tools — persona stage must not invoke tools.
		Temperature: 0.8,
		MaxTokens:   personaMaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("persona stage: %w", err)
	}
	reply := resp.Content
	if reply == "" {
		reply = "(no response)"
	}

	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: reply})
	a.trimHistory()
	a.mu.Unlock()
	return reply, nil
}

// snapshotHistory returns a shallow copy of the current conversation
// history so the decision stage can reason about prior turns without
// competing with concurrent mutations to a.history.
// Caller must hold a.mu.
func (a *Agent) snapshotHistory() []llm.Message {
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}

// formatActionResult semantically paraphrases a single tool invocation for
// the persona stage. The goal is to hide tool names, JSON, and coordinates
// from the role-play model so it doesn't recite them at the player.
// Returns "" if the result contributes no meaningful signal.
func formatActionResult(r actionResult) string {
	if r.Err != "" {
		// Surface failure mode in plain prose so the persona layer can
		// apologize or re-ask. Never include tool / arg details.
		return "刚才的动作没能完成（" + r.Err + "）。"
	}
	switch r.ToolName {
	case "npc_move_to":
		// Prefer a human-readable destination when the decision layer used
		// named coordinates; otherwise stay vague to avoid reciting (x,y).
		return "你已经开始朝玩家指定的位置走去了。"
	case "npc_face_direction":
		return "你已经转身面对了玩家所指的方向。"
	case "npc_get_position":
		return summarizePositionOutput(r.Output)
	case "npc_get_nearby":
		return summarizeNearbyOutput(r.Output)
	case "npc_get_environment":
		return summarizeEnvOutput(r.Output)
	case "friendship_get":
		return "你已经在心里掂量过跟玩家的关系深浅。"
	case "game_get_time":
		return "你已经留意过现在的时间。"
	case "game_get_weather":
		return "你已经看过今天的天气。"
	}
	// Unknown tool — fall back to raw output so at least the information
	// reaches the persona layer, truncated to keep context cheap.
	return truncateForPrompt(r.Output, 200)
}

// summarizePositionOutput extracts the "at (x,y) on map" bits from an
// npc_get_position JSON blob. Safely returns a generic phrase when the
// output doesn't parse.
func summarizePositionOutput(raw string) string {
	if raw == "" {
		return "你已经确认过自己现在的位置。"
	}
	// We intentionally do not import encoding/json for a one-shot summary;
	// the persona layer only needs a human phrase, not exact coords.
	if strings.Contains(raw, `"is_moving":true`) {
		return "你正在移动中，知道自己还没走到目的地。"
	}
	return "你已经确认过自己现在的位置，脚下的路很熟悉。"
}

// summarizeNearbyOutput glances at an npc_get_nearby JSON blob to decide
// between "you're alone" and "there are others nearby" without leaking the
// raw list to the persona stage.
func summarizeNearbyOutput(raw string) string {
	if strings.Contains(raw, `"count":0`) || strings.Contains(raw, `"nearby":[]`) {
		return "你扫了一眼四周，附近没有别人。"
	}
	return "你留意到附近还有其他人的身影。"
}

// summarizeEnvOutput collapses an npc_get_environment blob into a short
// cue. Same spirit as summarizeNearbyOutput: no raw data exposure.
func summarizeEnvOutput(raw string) string {
	if raw == "" {
		return "你对周围的环境心里有数。"
	}
	return "你对脚下的地图、时辰和天气都心里有数。"
}

// formatActionResults renders the concatenation of per-result semantic
// summaries. Empty → "" (dropped by joinContextBlocks).
func formatActionResults(results []actionResult) string {
	if len(results) == 0 {
		return ""
	}
	var lines []string
	for _, r := range results {
		line := formatActionResult(r)
		if line != "" {
			lines = append(lines, "- "+line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "[系统观察 — 以下动作刚刚由决策层代你执行完毕，请自然融入回答，不要向玩家复述工具名、坐标或 JSON。]\n" + strings.Join(lines, "\n")
}

// compactArgs renders a map as a stable key=value list. Not used by the
// default persona-stage prompt any more (per formatActionResult we want to
// HIDE raw args) but retained for debug logging and tests that want a
// deterministic string form of a tool call.
func compactArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	// Small N; insertion sort keeps it deterministic without importing sort.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		fmt.Fprintf(&sb, "%v", args[k])
	}
	sb.WriteByte('}')
	return sb.String()
}

// truncateForPrompt clamps tool output that might otherwise blow the
// persona-stage context window. Byte-based to stay simple; Unicode runes
// survive because we walk back to a UTF-8 start byte before cutting.
func truncateForPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Prefer a line break as the cut-off so we don't slice mid-token.
	cut := strings.LastIndex(s[:max], "\n")
	if cut < max/2 {
		cut = max
	}
	for cut > 0 && cut < len(s) && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…(truncated)"
}

// joinContextBlocks concatenates non-empty context blocks with blank-line
// separators. Kept out-of-band so respondDual mirrors the single-model
// path's system-prompt assembly.
func joinContextBlocks(blocks ...string) string {
	var out []string
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	return strings.Join(out, "\n\n")
}

// _ is here to keep the mcp import used when future extensions need it —
// currently respondDual routes all MCP traffic through executeTool which
// already depends on mcp. If you remove this, run `go vet` to double-check.
var _ = mcp.CallToolParams{}
