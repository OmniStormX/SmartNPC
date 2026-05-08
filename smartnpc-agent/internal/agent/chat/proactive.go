// Proactive behavior ticker — periodically injects a system prompt into each
// NPC agent, giving them the opportunity to perform autonomous social actions
// (chat with nearby NPCs, react to events) without player initiation.
//
// Movement is handled entirely by the C# WanderSystem state machine — the
// proactive ticker no longer suggests npc_move_to.

package chat

import (
	"context"
	"encoding/json"
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
//
// IMPORTANT: To avoid polluting the Hermes conversation with hundreds of idle
// proactive ticks, we use respondProactive() which only invokes the persona
// stage when the decision layer actually took action. Idle ticks never touch
// the Hermes session, keeping input_tokens from ballooning.
func (a *Agent) doProactiveTick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	// Gather context: game state + NPC position + nearby entities.
	gameState := a.getGameStateContext(tickCtx)
	position := a.getPositionContext(tickCtx)
	nearby := a.getNearbyContext(tickCtx)

	prompt := buildProactivePrompt(a.cfg.Speaker, gameState, position, nearby)

	reply, err := a.respondProactive(tickCtx, prompt)
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
// Movement is handled by C# WanderSystem — this prompt only covers social
// behaviors and environmental awareness.
func buildProactivePrompt(speaker, gameState, position, nearby string) string {
	var sb strings.Builder
	sb.WriteString("[系统提示 — 自主行为时间]\n")
	if gameState != "" {
		sb.WriteString(gameState)
		sb.WriteString("\n")
	}
	if position != "" {
		sb.WriteString(position)
		sb.WriteString("\n")
	}
	if nearby != "" {
		sb.WriteString(nearby)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\n你是 %s，已经空闲了一段时间。你正在闲逛。根据你的性格和周围环境，你可以：\n", speaker))
	sb.WriteString("- 如果附近有其他 NPC，可以用 npc_send_message 跟他们闲聊（简短1-2句）\n")
	sb.WriteString("- 如果附近有玩家且距离<5，可以主动打招呼\n")
	sb.WriteString("- 什么都不做（回复 idle）\n")
	sb.WriteString("\n规则：\n")
	sb.WriteString("- 不要使用 npc_move_to（移动由系统自动处理）\n")
	sb.WriteString("- 跟 NPC 闲聊保持简短（1-2句），不要频繁\n")
	sb.WriteString("- 大部分时候选择 idle，保持自然节奏\n")
	sb.WriteString("- 如果什么都不想做，只回复一个词: idle")
	return sb.String()
}

// getPositionContext calls npc_get_position via MCP and returns a formatted
// string like "[你的位置] 地图: Farm, 坐标: (64, 15), 朝向: 南".
func (a *Agent) getPositionContext(ctx context.Context) string {
	a.mu.Lock()
	s := a.session
	speaker := a.cfg.Speaker
	a.mu.Unlock()
	if s == nil {
		return ""
	}

	res, err := s.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_position",
		Arguments: map[string]any{"npc": speaker},
	})
	if err != nil || res == nil || res.IsError {
		return ""
	}

	raw := extractToolText(res)
	if raw == "" {
		return ""
	}

	var pos struct {
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Map       string  `json:"map"`
		Direction string  `json:"direction"`
		IsMoving  bool    `json:"is_moving"`
	}
	if err := json.Unmarshal([]byte(raw), &pos); err != nil {
		return ""
	}

	dirCN := map[string]string{
		"up": "北", "down": "南", "left": "西", "right": "东",
	}
	dir := dirCN[pos.Direction]
	if dir == "" {
		dir = pos.Direction
	}

	result := fmt.Sprintf("[你的位置] 地图: %s, 坐标: (%.0f, %.0f), 朝向: %s",
		pos.Map, pos.X, pos.Y, dir)
	if pos.IsMoving {
		result += " (移动中)"
	}
	return result
}

// getNearbyContext calls npc_get_nearby via MCP and returns a formatted string
// listing nearby NPCs and players. Returns "" on failure or when alone.
func (a *Agent) getNearbyContext(ctx context.Context) string {
	a.mu.Lock()
	s := a.session
	speaker := a.cfg.Speaker
	a.mu.Unlock()
	if s == nil {
		return ""
	}

	res, err := s.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_get_nearby",
		Arguments: map[string]any{
			"npc":    speaker,
			"radius": 15.0,
		},
	})
	if err != nil || res == nil || res.IsError {
		return ""
	}

	raw := extractToolText(res)
	if raw == "" {
		return ""
	}

	var data struct {
		Count  int `json:"count"`
		Nearby []struct {
			Name     string  `json:"name"`
			Type     string  `json:"type"`
			Distance float64 `json:"distance"`
		} `json:"nearby"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data.Count == 0 {
		return "[周围] 附近没有其他人"
	}

	var parts []string
	for _, n := range data.Nearby {
		typeLabel := "NPC"
		if n.Type == "player" {
			typeLabel = "玩家"
		}
		parts = append(parts, fmt.Sprintf("%s(%s, %.0f格)", n.Name, typeLabel, n.Distance))
	}
	return "[周围] 附近有: " + strings.Join(parts, ", ")
}

// isIdleReply returns true if the LLM chose not to act.
func isIdleReply(reply string) bool {
	r := strings.TrimSpace(strings.ToLower(reply))
	return r == "idle" || r == "(idle)" || r == "" || r == "(no response)" || r == "idle."
}
