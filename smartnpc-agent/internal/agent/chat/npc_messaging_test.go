package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

func TestNpcSendMessage_DeliverySuccess(t *testing.T) {
	providerA := &mockProvider{replies: []llm.ChatResponse{
		{Content: "好的，我告诉 Sebastian 了！"},
	}}
	providerB := &mockProvider{replies: []llm.ChatResponse{
		{Content: "哦？Abigail 找我有事？"},
	}}

	agentA := New(Config{
		Provider: providerA,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})
	agentB := New(Config{
		Provider: providerB,
		Speaker:  "Sebastian",
		Timeout:  5 * time.Second,
	})

	doneB := make(chan struct{}, 4)
	agentB.mu.Lock()
	agentB.replyDone = doneB
	agentB.mu.Unlock()

	router := NewRouter()
	router.Register("Abigail", agentA)
	router.Register("Sebastian", agentB)
	router.WireAgentRouters()

	// Agent A calls npc_send_message targeting Agent B.
	tc := llm.ToolCall{
		ID:   "tc_1",
		Name: "npc_send_message",
		Arguments: map[string]any{
			"to":      "Sebastian",
			"message": "今晚去矿洞探险吗？",
		},
	}
	result, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("npc_send_message should be handled as local tool")
	}
	if !strings.Contains(result, `"ok":true`) {
		t.Fatalf("expected ok:true in result, got: %s", result)
	}

	// Wait for Agent B to process the incoming NPC message.
	select {
	case <-doneB:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Agent B to respond")
	}

	// Verify B's history contains the injected message from Abigail.
	// The message goes through the normal respond() pipeline and ends up
	// as a User role entry (the formatted "[NPC 消息] ..." string).
	agentB.mu.Lock()
	histB := agentB.history
	agentB.mu.Unlock()
	if len(histB) < 2 {
		t.Fatalf("expected at least 2 history entries in B, got %d", len(histB))
	}
	found := false
	for _, msg := range histB {
		if strings.Contains(msg.Content, "Abigail") && strings.Contains(msg.Content, "今晚去矿洞探险吗") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Agent B's history does not contain the NPC message from Abigail")
	}
}

func TestNpcSendMessage_UnknownRecipient(t *testing.T) {
	provider := &mockProvider{}
	agent := New(Config{
		Provider: provider,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})
	router := NewRouter()
	router.Register("Abigail", agent)
	router.WireAgentRouters()

	tc := llm.ToolCall{
		ID:   "tc_2",
		Name: "npc_send_message",
		Arguments: map[string]any{
			"to":      "UnknownNPC",
			"message": "hello",
		},
	}
	result, handled := agent.executeLocalTool(tc)
	if !handled {
		t.Fatal("should be handled")
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' in result, got: %s", result)
	}
}

func TestNpcSendMessage_MissingArgs(t *testing.T) {
	provider := &mockProvider{}
	agent := New(Config{
		Provider: provider,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})
	router := NewRouter()
	router.Register("Abigail", agent)
	router.WireAgentRouters()

	tc := llm.ToolCall{
		ID:        "tc_3",
		Name:      "npc_send_message",
		Arguments: map[string]any{"to": "Sebastian"},
	}
	result, _ := agent.executeLocalTool(tc)
	if !strings.Contains(result, "missing required") {
		t.Errorf("expected 'missing required' in result, got: %s", result)
	}
}

func TestLocalToolSpecs_ContainsNpcSendMessage(t *testing.T) {
	provider := &mockProvider{}
	agent := New(Config{
		Provider: provider,
		Speaker:  "Abigail",
		Timeout:  5 * time.Second,
	})
	specs := agent.localToolSpecs()
	if len(specs) == 0 {
		t.Fatal("expected at least one local tool spec")
	}
	found := false
	for _, s := range specs {
		if s.Name == "npc_send_message" {
			found = true
			if s.Description == "" {
				t.Error("npc_send_message should have a description")
			}
		}
	}
	if !found {
		t.Error("npc_send_message not found in local tool specs")
	}
}

func TestRouter_WireAgentRouters(t *testing.T) {
	provider := &mockProvider{}
	agentA := New(Config{Provider: provider, Speaker: "Abigail"})
	agentB := New(Config{Provider: provider, Speaker: "Sebastian"})

	router := NewRouter()
	router.Register("Abigail", agentA)
	router.Register("Sebastian", agentB)
	router.WireAgentRouters()

	// Verify router is wired.
	agentA.mu.Lock()
	hasRouter := agentA.router != nil
	agentA.mu.Unlock()
	if !hasRouter {
		t.Error("agentA.router should be non-nil after WireAgentRouters")
	}
}
