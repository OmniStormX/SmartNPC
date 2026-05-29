package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// newSocialActionClientServer creates an in-memory MCP client/server with the
// social-action tools wired through a real bridge.WSClient backed by a fake
// mod ws server. The fake echoes {ok:true, npc:<npc>}.
func newSocialActionClientServer(t *testing.T) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	srv := bridge.NewTestServer(func(_ context.Context, _ string, params json.RawMessage) (any, error) {
		var p struct {
			NPC     string `json:"npc"`
			Emotion string `json:"emotion"`
		}
		_ = json.Unmarshal(params, &p)
		out := map[string]any{"ok": true, "npc": p.NPC, "message": "stub: mod ack"}
		if p.Emotion != "" {
			out["emotion"] = p.Emotion
		}
		return out, nil
	})
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	if err := br.Connect(ctx); err != nil {
		cancel()
		srv.Close()
		t.Fatalf("ws connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcSocialAction(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("client connect: %v", err)
	}
	cleanup := func() {
		cs.Close()
		br.Close()
		srv.Close()
		cancel()
	}
	return cs, ctx, cleanup
}

// ── ListTools ─────────────────────────────────────────────────────

func TestNpcSocialAction_ListTools(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_approach_and_speak": false,
		"npc_express_emotion":    false,
		"npc_shy_retreat":        false,
		"npc_show_text_bubble":   false,
		"npc_idle_activity":      false,
		"npc_dance_happy":        false,
		"npc_react_surprise":     false,
		"npc_pace_anxiously":     false,
	}
	for _, tool := range listed.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
			if tool.Description == "" {
				t.Errorf("tool %s has empty description", tool.Name)
			}
			if tool.InputSchema == nil {
				t.Errorf("tool %s has nil InputSchema", tool.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q missing from ListTools", name)
		}
	}
}

// ── npc_approach_and_speak ────────────────────────────────────────

func TestNpcApproachAndSpeak_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_approach_and_speak",
		Arguments: map[string]any{"npc": "XiaMi", "message": "早上好！"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcApproachAndSpeakOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcApproachAndSpeak_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_approach_and_speak",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── npc_show_text_bubble ──────────────────────────────────────────

func TestNpcShowTextBubble_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_show_text_bubble",
		Arguments: map[string]any{"npc": "XiaMi", "text": "嗯...天气不错"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcShowTextBubbleOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcShowTextBubble_RejectsEmptyText(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_show_text_bubble",
		Arguments: map[string]any{"npc": "XiaMi", "text": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty text")
	}
}

// ── npc_express_emotion ───────────────────────────────────────────

func TestNpcExpressEmotion_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_express_emotion",
		Arguments: map[string]any{"npc": "XiaMi", "emotion": "happy"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcExpressEmotionOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Emotion != "happy" {
		t.Errorf("output: %+v", out)
	}
}

// ── npc_dance_happy ───────────────────────────────────────────────

func TestNpcDanceHappy_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_dance_happy",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcDanceHappyOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcDanceHappy_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newSocialActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_dance_happy",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}
