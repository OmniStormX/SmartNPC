package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// Sentinel errors for parameter validation.
var (
	errNpcRequired    = fmt.Errorf("npc is required")
	errTextRequired   = fmt.Errorf("text is required")
	errSeedIDRequired = fmt.Errorf("seed_id is required")
	errItemIDRequired = fmt.Errorf("item_id is required")
)

// ── npc_approach_and_speak ────────────────────────────────────────

type NpcApproachAndSpeakInput struct {
	NPC     string `json:"npc"              jsonschema:"NPC internal name"`
	Message string `json:"message,omitempty" jsonschema:"optional message to show after arriving (uses chat_say internally)"`
}

type NpcApproachAndSpeakOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_express_emotion ───────────────────────────────────────────

type NpcExpressEmotionInput struct {
	NPC     string `json:"npc"     jsonschema:"NPC internal name"`
	Emotion string `json:"emotion" jsonschema:"one of: happy, shy, angry, thinking, sad"`
}

type NpcExpressEmotionOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Emotion string `json:"emotion"           jsonschema:"emotion applied"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_shy_retreat ───────────────────────────────────────────────

type NpcShyRetreatInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type NpcShyRetreatOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_show_text_bubble ──────────────────────────────────────────

type NpcShowTextBubbleInput struct {
	NPC  string `json:"npc"  jsonschema:"NPC internal name"`
	Text string `json:"text" jsonschema:"text to display above NPC head (max ~30 chars recommended)"`
}

type NpcShowTextBubbleOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_idle_activity ─────────────────────────────────────────────

type NpcIdleActivityInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Activity string `json:"activity"          jsonschema:"one of: farm, rest, look_around"`
	Mode     string `json:"mode,omitempty"    jsonschema:"once or loop (default: once)"`
}

type NpcIdleActivityOutput struct {
	OK       bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC      string `json:"npc"               jsonschema:"echo"`
	Activity string `json:"activity"          jsonschema:"activity started"`
	Mode     string `json:"mode"              jsonschema:"mode applied"`
	Message  string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_dance_happy ───────────────────────────────────────────────

type NpcDanceHappyInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type NpcDanceHappyOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_react_surprise ────────────────────────────────────────────

type NpcReactSurpriseInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type NpcReactSurpriseOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_pace_anxiously ────────────────────────────────────────────

type NpcPaceAnxiouslyInput struct {
	NPC           string `json:"npc"                      jsonschema:"NPC internal name"`
	DurationTicks int    `json:"duration_ticks,omitempty" jsonschema:"how many game ticks to pace (default: 200)"`
}

type NpcPaceAnxiouslyOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── registration ──────────────────────────────────────────────────

//nolint:unused
var _ = bridge.ActionNpcApproachAndSpeak // ensure import is used

// callBridgeSocial mirrors callBridge in npc_world_action.go but is kept
// separate so each file is self-contained. Forwards a stub-tool call to
// the Mod via WebSocket and unmarshals the response into out.
//
// req is required so the underlying ws Request frame can be stamped with
// the originating NPC profile via callCtx (see npc_world_action.go's
// callBridge for the rationale).
func callBridgeSocial[Out any](ctx context.Context, req *mcp.CallToolRequest, br *bridge.WSClient, action string, in any, label string) (Out, error) {
	var out Out
	raw, err := br.Call(callCtx(ctx, req), action, in)
	if err != nil {
		return out, fmt.Errorf("%s: %w", label, err)
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

func registerNpcSocialAction(s *mcp.Server, br *bridge.WSClient) {

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_approach_and_speak",
		Description: "NPC pathfinds to the player, faces them, shows an emote bubble, " +
			"then optionally says a line via chat_say.\n\n" +
			"When to call: NPC wants to initiate conversation proactively - greeting, " +
			"delivering news, or reacting to something they noticed.\n\n" +
			"Side-effect: WRITE (moves NPC, shows emote, optionally sends chat).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcApproachAndSpeakInput) (*mcp.CallToolResult, NpcApproachAndSpeakOutput, error) {
		if in.NPC == "" {
			return nil, NpcApproachAndSpeakOutput{}, errNpcRequired
		}
		logToolCall("npc_approach_and_speak", in)
		out, err := callBridgeSocial[NpcApproachAndSpeakOutput](ctx, req, br, bridge.ActionNpcApproachAndSpeak, in, "npc_approach_and_speak")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_express_emotion",
		Description: "NPC performs a compound emotion expression combining animation frames, " +
			"emote bubbles, jump, shake, and facing changes.\n\n" +
			"When to call: NPC reacts emotionally to dialogue or events - happy after " +
			"receiving a gift, shy when complimented, angry when insulted.\n\n" +
			"Valid emotions: happy, shy, angry, thinking, sad.\n\n" +
			"Side-effect: WRITE (visual only - no game-state mutation).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcExpressEmotionInput) (*mcp.CallToolResult, NpcExpressEmotionOutput, error) {
		if in.NPC == "" {
			return nil, NpcExpressEmotionOutput{}, errNpcRequired
		}
		logToolCall("npc_express_emotion", in)
		out, err := callBridgeSocial[NpcExpressEmotionOutput](ctx, req, br, bridge.ActionNpcExpressEmotion, in, "npc_express_emotion")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_shy_retreat",
		Description: "NPC steps back one tile away from the player, turns away, and shows " +
			"a shy/embarrassed animation (shake + heart bubble).\n\n" +
			"When to call: NPC is flustered by a compliment, confession, or teasing.\n\n" +
			"Side-effect: WRITE (moves NPC one tile, visual animation).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcShyRetreatInput) (*mcp.CallToolResult, NpcShyRetreatOutput, error) {
		if in.NPC == "" {
			return nil, NpcShyRetreatOutput{}, errNpcRequired
		}
		logToolCall("npc_shy_retreat", in)
		out, err := callBridgeSocial[NpcShyRetreatOutput](ctx, req, br, bridge.ActionNpcShyRetreat, in, "npc_shy_retreat")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_show_text_bubble",
		Description: "Show a short text bubble above the NPC's head (showTextAboveHead). " +
			"Lightweight expression - muttering, thinking out loud, short reactions.\n\n" +
			"When to call: NPC wants to express something brief without using the chat " +
			"panel - internal monologue visible to nearby player.\n\n" +
			"Side-effect: WRITE (visual only, disappears after ~2s).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcShowTextBubbleInput) (*mcp.CallToolResult, NpcShowTextBubbleOutput, error) {
		if in.NPC == "" {
			return nil, NpcShowTextBubbleOutput{}, errNpcRequired
		}
		if in.Text == "" {
			return nil, NpcShowTextBubbleOutput{}, errTextRequired
		}
		logToolCall("npc_show_text_bubble", in)
		out, err := callBridgeSocial[NpcShowTextBubbleOutput](ctx, req, br, bridge.ActionNpcShowTextBubble, in, "npc_show_text_bubble")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_idle_activity",
		Description: "NPC performs an idle animation: farming motion, resting pose, or " +
			"looking around. Does not change world state.\n\n" +
			"When to call: NPC is idle and you want them to look busy/natural rather " +
			"than standing still.\n\n" +
			"Activities: farm (hoe/water gesture), rest (sit/lean), look_around (glance sides).\n" +
			"Modes: once (play one cycle), loop (repeat until interrupted).\n\n" +
			"Side-effect: WRITE (visual only).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcIdleActivityInput) (*mcp.CallToolResult, NpcIdleActivityOutput, error) {
		if in.NPC == "" {
			return nil, NpcIdleActivityOutput{}, errNpcRequired
		}
		if in.Mode == "" {
			in.Mode = "once"
		}
		logToolCall("npc_idle_activity", in)
		out, err := callBridgeSocial[NpcIdleActivityOutput](ctx, req, br, bridge.ActionNpcIdleActivity, in, "npc_idle_activity")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_dance_happy",
		Description: "NPC performs a happy celebration: jumps, cheers, spins, shows heart bubble.\n\n" +
			"When to call: good news, friendship milestone, quest completion, harvest success.\n\n" +
			"Side-effect: WRITE (visual only - jump + animation + emote).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcDanceHappyInput) (*mcp.CallToolResult, NpcDanceHappyOutput, error) {
		if in.NPC == "" {
			return nil, NpcDanceHappyOutput{}, errNpcRequired
		}
		logToolCall("npc_dance_happy", in)
		out, err := callBridgeSocial[NpcDanceHappyOutput](ctx, req, br, bridge.ActionNpcDanceHappy, in, "npc_dance_happy")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_react_surprise",
		Description: "NPC jumps and shows a surprised reaction (! bubble + startled frame).\n\n" +
			"When to call: player suddenly appears, unexpected gift, surprising news.\n\n" +
			"Side-effect: WRITE (visual only - jump + emote).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcReactSurpriseInput) (*mcp.CallToolResult, NpcReactSurpriseOutput, error) {
		if in.NPC == "" {
			return nil, NpcReactSurpriseOutput{}, errNpcRequired
		}
		logToolCall("npc_react_surprise", in)
		out, err := callBridgeSocial[NpcReactSurpriseOutput](ctx, req, br, bridge.ActionNpcReactSurprise, in, "npc_react_surprise")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_pace_anxiously",
		Description: "NPC paces left and right nervously, occasionally showing a ? bubble.\n\n" +
			"When to call: NPC is waiting, worried, conflicted, or indecisive.\n\n" +
			"Side-effect: WRITE (visual movement - stays within 2 tiles of origin).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcPaceAnxiouslyInput) (*mcp.CallToolResult, NpcPaceAnxiouslyOutput, error) {
		if in.NPC == "" {
			return nil, NpcPaceAnxiouslyOutput{}, errNpcRequired
		}
		logToolCall("npc_pace_anxiously", in)
		out, err := callBridgeSocial[NpcPaceAnxiouslyOutput](ctx, req, br, bridge.ActionNpcPaceAnxiously, in, "npc_pace_anxiously")
		return nil, out, err
	})
}
