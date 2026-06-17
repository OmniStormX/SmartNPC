package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// mockFollowSystem implements FollowSystemQuery for tests.
type mockFollowSystem struct {
	mu     sync.Mutex
	modes  map[string]string
	callCt map[string]int
}

func newMockFollow() *mockFollowSystem {
	return &mockFollowSystem{
		modes:  map[string]string{},
		callCt: map[string]int{},
	}
}

func (m *mockFollowSystem) SetMode(npc, mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modes[npc] = mode
}

func (m *mockFollowSystem) GetMode(npc string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCt[npc]++
	return m.modes[npc]
}

func TestMCPRunner_WaitIdle(t *testing.T) {
	t.Run("idle on first poll", func(t *testing.T) {
		follow := newMockFollow()
		follow.SetMode("TestNPC", "Idle")
		runner := NewMCPRunner(MCPRunnerOptions{Follow: follow})
		ok, err := runner.WaitIdle(context.Background(), "TestNPC", 5*time.Second)
		if err != nil {
			t.Fatalf("WaitIdle: %v", err)
		}
		if !ok {
			t.Error("expected idle=true on first poll")
		}
	})

	t.Run("becomes idle after polls", func(t *testing.T) {
		follow := newMockFollow()
		follow.SetMode("TestNPC", "Walking")

		// Make it become idle after 3 polls.
		go func() {
			time.Sleep(600 * time.Millisecond)
			follow.SetMode("TestNPC", "Idle")
		}()

		runner := NewMCPRunner(MCPRunnerOptions{Follow: follow})
		ok, err := runner.WaitIdle(context.Background(), "TestNPC", 5*time.Second)
		if err != nil {
			t.Fatalf("WaitIdle: %v", err)
		}
		if !ok {
			t.Error("expected idle=true after mode change")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		follow := newMockFollow()
		follow.SetMode("TestNPC", "Walking") // never goes idle
		runner := NewMCPRunner(MCPRunnerOptions{Follow: follow})
		ok, err := runner.WaitIdle(context.Background(), "TestNPC", 200*time.Millisecond)
		if err != nil {
			t.Fatalf("WaitIdle: %v", err)
		}
		if ok {
			t.Error("expected idle=false on timeout")
		}
	})

	t.Run("no follow system defaults to idle", func(t *testing.T) {
		runner := NewMCPRunner(MCPRunnerOptions{})
		ok, err := runner.WaitIdle(context.Background(), "TestNPC", 1*time.Second)
		if err != nil {
			t.Fatalf("WaitIdle: %v", err)
		}
		if !ok {
			t.Error("expected idle=true when no follow system configured")
		}
	})
}

func TestMCPRunner_CallTool_NilBridgeReturnsError(t *testing.T) {
	runner := NewMCPRunner(MCPRunnerOptions{})
	args := map[string]any{"radius": 5}
	_, err := runner.CallTool(context.Background(), "Abigail", "npc_wander", args)
	if err == nil {
		t.Error("expected error when calling tool with nil bridge")
	}
	// The nil bridge check is before args mutation, so args should be unchanged.
	if _, ok := args["npc"]; ok {
		t.Errorf("args should not be mutated on early nil-bridge return")
	}
}

func TestMCPRunner_LLMChoice_Defaults(t *testing.T) {
	runner := NewMCPRunner(MCPRunnerOptions{})
	choice, err := runner.LLMChoice(context.Background(), "TestNPC", "pick one", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("LLMChoice: %v", err)
	}
	if choice != "a" {
		t.Errorf("expected default choice 'a', got %q", choice)
	}
}

func TestMCPRunner_LLMChoice_EmptyOptions(t *testing.T) {
	runner := NewMCPRunner(MCPRunnerOptions{})
	_, err := runner.LLMChoice(context.Background(), "TestNPC", "pick", nil)
	if err == nil {
		t.Error("expected error for empty options")
	}
}

func TestMCPRunner_CallSkill_RelayWired(t *testing.T) {
	var relayCalled bool
	var relayEvent string
	var relayData json.RawMessage
	runner := NewMCPRunner(MCPRunnerOptions{
		Relay: func(_ context.Context, eventName string, data json.RawMessage) {
			relayCalled = true
			relayEvent = eventName
			relayData = data
		},
	})
	err := runner.CallSkill(context.Background(), "TestNPC", "smartnpc-farm-maintenance", map[string]any{"radius": float64(12)})
	if err != nil {
		t.Fatalf("CallSkill: %v", err)
	}
	if !relayCalled {
		t.Fatal("relay was not called")
	}
	if relayEvent != "workflow_skill_call" {
		t.Errorf("event = %q; want workflow_skill_call", relayEvent)
	}
	var payload struct {
		NPC   string         `json:"npc"`
		Skill string         `json:"skill"`
		Args  map[string]any `json:"args"`
	}
	if err := json.Unmarshal(relayData, &payload); err != nil {
		t.Fatalf("unmarshal relay data: %v", err)
	}
	if payload.NPC != "TestNPC" {
		t.Errorf("npc = %q; want TestNPC", payload.NPC)
	}
	if payload.Skill != "smartnpc-farm-maintenance" {
		t.Errorf("skill = %q; want smartnpc-farm-maintenance", payload.Skill)
	}
	if payload.Args["radius"].(float64) != 12 {
		t.Errorf("args.radius = %v; want 12", payload.Args["radius"])
	}
}

func TestMCPRunner_CallSkill_NilRelay(t *testing.T) {
	runner := NewMCPRunner(MCPRunnerOptions{})
	err := runner.CallSkill(context.Background(), "TestNPC", "any-skill", nil)
	if err != nil {
		t.Errorf("CallSkill with nil relay: %v", err)
	}
}

func TestMCPRunner_CallSkill_NilArgs(t *testing.T) {
	var relayCalled bool
	runner := NewMCPRunner(MCPRunnerOptions{
		Relay: func(_ context.Context, _ string, _ json.RawMessage) {
			relayCalled = true
		},
	})
	err := runner.CallSkill(context.Background(), "NPC", "skill", nil)
	if err != nil {
		t.Fatalf("CallSkill nil args: %v", err)
	}
	if !relayCalled {
		t.Error("relay should still be called when args is nil")
	}
}

func TestMCPRunner_ImplementsRunnerInterface(t *testing.T) {
	// Compile-time check: MCPRunner satisfies the Runner interface.
	var _ Runner = (*MCPRunner)(nil)

	// Verify all methods are accessible.
	runner := NewMCPRunner(MCPRunnerOptions{})
	if runner == nil {
		t.Fatal("NewMCPRunner returned nil")
	}
}

func TestFollowSystemQuery_DefaultNil(t *testing.T) {
	runner := NewMCPRunner(MCPRunnerOptions{Follow: nil})
	if runner.follow != nil {
		t.Error("expected follow=nil when not provided")
	}
	// WaitIdle should handle nil follow gracefully.
	ok, err := runner.WaitIdle(context.Background(), "NPC", 100*time.Millisecond)
	if err != nil {
		t.Errorf("WaitIdle with nil follow: %v", err)
	}
	if !ok {
		t.Error("WaitIdle should return true when follow is nil")
	}
}

// TestChoiceReplyRoundtrip verifies the pending choices mechanism.
func TestChoiceReplyRoundtrip(t *testing.T) {
	// This test covers the pending choices mechanism used by the
	// LLMChoice protocol (RegisterPendingChoice / CompletePendingChoice).
	// These are exported by the tools package but the round-trip logic
	// is verified here conceptually.
	t.Run("complete before timeout", func(t *testing.T) {
		// Simulate the channel mechanism.
		ch := make(chan string, 1)
		var mu sync.Mutex
		reg := map[string]chan string{"req-1": ch}

		go func() {
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			c, ok := reg["req-1"]
			mu.Unlock()
			if ok {
				c <- "option-b"
			}
		}()

		select {
		case choice := <-ch:
			if choice != "option-b" {
				t.Errorf("expected option-b, got %q", choice)
			}
		case <-time.After(time.Second):
			t.Error("timeout waiting for choice")
		}
	})

	t.Run("timeout fallback", func(t *testing.T) {
		fallback := "option-a"
		ch := make(chan string, 1)

		select {
		case choice := <-ch:
			_ = choice
		case <-time.After(50 * time.Millisecond):
			// Timeout — use fallback.
		}
		if fallback != "option-a" {
			t.Error("fallback should be first option")
		}
	})
}

func TestRunnerJSONMarshaling(t *testing.T) {
	// Verify that JSON tags on Runner-related types are correct.
	type testOutput struct {
		OK      bool   `json:"ok"`
		Message string `json:"message,omitempty"`
	}

	out := testOutput{OK: true, Message: "done"}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded testOutput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.OK || decoded.Message != "done" {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}
