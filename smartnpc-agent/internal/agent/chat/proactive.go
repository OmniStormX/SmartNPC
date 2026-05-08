// Proactive behavior ticker — periodically injects a system prompt into each
// NPC agent, giving them the opportunity to perform autonomous actions (walk,
// chat with other NPCs, idle, etc.) without player initiation.

package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProactiveConfig configures the autonomous behavior ticker.
type ProactiveConfig struct {
	// Interval between proactive ticks. Default 4 minutes.
	Interval time.Duration
	// Jitter adds randomness to avoid all NPCs firing at once. Default ±60s.
	Jitter time.Duration
	// Enabled controls whether the ticker is active. Default true.
	Enabled bool
}

// DefaultProactiveConfig returns sensible defaults.
func DefaultProactiveConfig() ProactiveConfig {
	return ProactiveConfig{
		Interval: 4 * time.Minute,
		Jitter:   60 * time.Second,
		Enabled:  true,
	}
}

// StartProactive launches a background goroutine for each registered agent
// that periodically triggers autonomous behavior. Cancelled via ctx.
func (r *Router) StartProactive(ctx context.Context, cfg ProactiveConfig) {
	if !cfg.Enabled {
		return
	}
	for _, a := range r.Agents() {
		go a.proactiveLoop(ctx, cfg)
	}
	if logger := r.anyLogger(); logger != nil {
		logger.Info("proactive ticker started",
			"agents", len(r.Agents()),
			"interval", cfg.Interval,
			"jitter", cfg.Jitter)
	}
}

// proactiveLoop runs the ticker for a single agent.
func (a *Agent) proactiveLoop(ctx context.Context, cfg ProactiveConfig) {
	// Initial delay: random offset so NPCs don't all fire together at startup.
	initialDelay := time.Duration(rand.Int63n(int64(cfg.Interval)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	for {
		// Calculate next tick with jitter.
		jitter := time.Duration(rand.Int63n(int64(cfg.Jitter)*2)) - cfg.Jitter
		wait := cfg.Interval + jitter
		if wait < 30*time.Second {
			wait = 30 * time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		// Skip if agent was recently in conversation (within last 30 seconds).
		if a.recentlyActive(30 * time.Second) {
			a.cfg.Logger.Debug("proactive tick skipped: recently active", "speaker", a.cfg.Speaker)
			continue
		}

		a.cfg.Logger.Info("proactive tick", "speaker", a.cfg.Speaker)
		a.doProactiveTick(ctx)
	}
}

// recentlyActive returns true if the agent had a user message within the
// given duration, indicating the player is actively chatting.
func (a *Agent) recentlyActive(within time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastUserMsgTime.IsZero() {
		return false
	}
	return time.Since(a.lastUserMsgTime) < within
}

// doProactiveTick builds a proactive prompt and runs the agent's respond loop.
// If the LLM replies "idle", no chat_say is emitted.
func (a *Agent) doProactiveTick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	// Gather game state for context.
	gameState := a.getGameStateContext(tickCtx)

	prompt := buildProactivePrompt(a.cfg.Speaker, gameState)

	reply, err := a.respond(tickCtx, prompt)
	if err != nil {
		a.cfg.Logger.Warn("proactive tick LLM failed", "speaker", a.cfg.Speaker, "err", err)
		return
	}

	// If LLM chose to idle, suppress chat_say.
	if isIdleReply(reply) {
		a.cfg.Logger.Debug("proactive tick: NPC chose idle", "speaker", a.cfg.Speaker)
		return
	}

	// NPC decided to do something — speak it (tool calls already executed
	// during respond()). Only emit chat_say if there's actual dialogue.
	a.cfg.Logger.Info("proactive action", "speaker", a.cfg.Speaker, "reply", truncateStr(reply, 100))

	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return
	}
	_, err = s.CallTool(tickCtx, &mcp.CallToolParams{
		Name: "chat_say",
		Arguments: map[string]any{
			"speaker": a.cfg.Speaker,
			"text":    reply,
		},
	})
	if err != nil {
		a.cfg.Logger.Warn("proactive chat_say failed", "err", err)
	}
}

// buildProactivePrompt constructs the system injection for autonomous behavior.
func buildProactivePrompt(speaker, gameState string) string {
	var sb strings.Builder
	sb.WriteString("[系统提示 — 自主行为时间]\n")
	if gameState != "" {
		sb.WriteString(gameState)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("你是 %s，你已经空闲了一段时间。根据你的性格和当前状态，你可以自由决定做什么：\n", speaker))
	sb.WriteString("- 使用 npc_move_to 去某个地方散步\n")
	sb.WriteString("- 使用 npc_send_message 给另一个 NPC 传话或闲聊\n")
	sb.WriteString("- 使用 npc_get_nearby 观察周围环境\n")
	sb.WriteString("- 使用 npc_get_environment 感受当前环境\n")
	sb.WriteString("- 或者什么都不做（回复 idle）\n")
	sb.WriteString("\n规则：\n")
	sb.WriteString("- 不要主动对玩家说话（除非你在玩家附近且真的有话想说）\n")
	sb.WriteString("- 如果你选择做某事，执行对应的 tool 调用\n")
	sb.WriteString("- 如果你什么都不想做，只回复一个词: idle\n")
	sb.WriteString("- 保持符合你性格的行为模式")
	return sb.String()
}

// isIdleReply returns true if the LLM chose not to act.
func isIdleReply(reply string) bool {
	r := strings.TrimSpace(strings.ToLower(reply))
	return r == "idle" || r == "(idle)" || r == "" || r == "(no response)" || r == "idle."
}
