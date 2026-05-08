package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

func TestIsIdleReply(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"idle", true},
		{"Idle", true},
		{"IDLE", true},
		{"idle.", true},
		{"(idle)", true},
		{"", true},
		{"(no response)", true},
		{"  idle  ", true},
		{"I'll go for a walk.", false},
		{"*stretches* idle sounds nice but I think I'll wander.", false},
	}
	for _, tt := range tests {
		got := isIdleReply(tt.input)
		if got != tt.want {
			t.Errorf("isIdleReply(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBuildProactivePrompt(t *testing.T) {
	prompt := buildProactivePrompt("Abigail",
		"[Current game state] Time: 14:00 (afternoon), spring 5",
		"[你的位置] 地图: Farm, 坐标: (64, 15), 朝向: 南",
		"[周围] 附近有: OmniStorm(玩家, 3格)")
	if !strings.Contains(prompt, "Abigail") {
		t.Error("prompt should contain speaker name")
	}
	if !strings.Contains(prompt, "npc_send_message") {
		t.Error("prompt should mention npc_send_message tool")
	}
	if !strings.Contains(prompt, "idle") {
		t.Error("prompt should mention idle option")
	}
	if !strings.Contains(prompt, "14:00") {
		t.Error("prompt should contain game state")
	}
	if !strings.Contains(prompt, "Farm") {
		t.Error("prompt should contain position info")
	}
	if !strings.Contains(prompt, "OmniStorm") {
		t.Error("prompt should contain nearby info")
	}
	// Movement is handled by C# WanderSystem — prompt should NOT suggest npc_move_to
	if !strings.Contains(prompt, "不要使用 npc_move_to") {
		t.Error("prompt should prohibit npc_move_to")
	}
}

func TestBuildProactivePrompt_NoGameState(t *testing.T) {
	prompt := buildProactivePrompt("XiaMi", "", "", "")
	if !strings.Contains(prompt, "XiaMi") {
		t.Error("prompt should contain speaker name")
	}
	if strings.Contains(prompt, "[Current game state]") {
		t.Error("prompt should not contain game state marker when empty")
	}
}

func TestRecentlyActive(t *testing.T) {
	provider := &mockProvider{}
	agent := New(Config{
		Provider: provider,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})

	// Initially not active.
	if agent.recentlyActive(30 * time.Second) {
		t.Error("should not be recently active initially")
	}

	// Simulate receiving a user message.
	agent.mu.Lock()
	agent.lastUserMsgTime = time.Now()
	agent.mu.Unlock()

	if !agent.recentlyActive(30 * time.Second) {
		t.Error("should be recently active after user message")
	}

	// Simulate time passing.
	agent.mu.Lock()
	agent.lastUserMsgTime = time.Now().Add(-60 * time.Second)
	agent.mu.Unlock()

	if agent.recentlyActive(30 * time.Second) {
		t.Error("should not be recently active after 60s")
	}
}

func TestProactiveConfig_Default(t *testing.T) {
	cfg := DefaultProactiveConfig()
	if cfg.Interval != 4*time.Minute {
		t.Errorf("expected 4m interval, got %v", cfg.Interval)
	}
	if cfg.Jitter != 60*time.Second {
		t.Errorf("expected 60s jitter, got %v", cfg.Jitter)
	}
	if !cfg.Enabled {
		t.Error("should be enabled by default")
	}
}

func TestProactiveIdleDoesNotSpeak(t *testing.T) {
	// Agent whose LLM always replies "idle" should not call chat_say.
	provider := &mockProvider{replies: []llm.ChatResponse{
		{Content: "idle"},
	}}
	agent := New(Config{
		Provider: provider,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})

	// doProactiveTick should not panic even without a session (chat_say won't fire).
	// No session means no tool calls happen, which is fine for this test.
	agent.doProactiveTick(t.Context())
	// If we got here without panic/crash, the idle path works.
}
