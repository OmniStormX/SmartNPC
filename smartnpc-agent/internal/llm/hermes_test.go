package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHermesProvider_Chat(t *testing.T) {
	// Mock Hermes server that returns a structured response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}

		var req hermesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Input != "hello" {
			t.Errorf("expected input 'hello', got %q", req.Input)
		}
		if !strings.HasPrefix(req.Conversation, "smartnpc-abigail-") {
			t.Errorf("expected conversation prefix 'smartnpc-abigail-', got %q", req.Conversation)
		}
		if !req.Store {
			t.Error("expected store=true")
		}
		if req.Instructions == "" {
			t.Error("expected non-empty instructions")
		}

		resp := hermesResponse{
			ID:     "resp_test123",
			Object: "response",
			Status: "completed",
			Output: []hermesOutputItem{
				{
					Type: "message",
					Role: "assistant",
					Content: []hermesContentPart{
						{Type: "output_text", Text: "Hi there! Nice to see you today."},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider, err := NewHermes(HermesConfig{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		Model:        "hermes-agent",
		Conversation: "smartnpc-abigail",
	})
	if err != nil {
		t.Fatalf("NewHermes: %v", err)
	}
	if provider.Name() != "hermes" {
		t.Errorf("expected name 'hermes', got %q", provider.Name())
	}

	resp, err := provider.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are Abigail."},
			{Role: RoleUser, Content: "hello"},
		},
		MaxTokens: 300,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hi there! Nice to see you today." {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestHermesProvider_IgnoresTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req hermesRequest
		json.NewDecoder(r.Body).Decode(&req)
		// Verify no tools-related fields are sent.
		body, _ := json.Marshal(req)
		if containsStr(string(body), "tools") {
			t.Error("request should not contain tools")
		}
		resp := hermesResponse{
			ID:     "resp_2",
			Status: "completed",
			Output: []hermesOutputItem{{
				Type:    "message",
				Role:    "assistant",
				Content: []hermesContentPart{{Type: "output_text", Text: "ok"}},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider, _ := NewHermes(HermesConfig{
		BaseURL:      srv.URL,
		Conversation: "test",
	})
	_, err := provider.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolSpec{
			{Name: "npc_move_to", Description: "move npc"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestHermesProvider_ExtractsLastUserMessage(t *testing.T) {
	var capturedInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req hermesRequest
		json.NewDecoder(r.Body).Decode(&req)
		capturedInput = req.Input
		resp := hermesResponse{
			ID: "resp_3", Status: "completed",
			Output: []hermesOutputItem{{
				Type: "message", Role: "assistant",
				Content: []hermesContentPart{{Type: "output_text", Text: "reply"}},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider, _ := NewHermes(HermesConfig{BaseURL: srv.URL, Conversation: "t"})
	provider.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "sys"},
			{Role: RoleUser, Content: "first message"},
			{Role: RoleAssistant, Content: "reply 1"},
			{Role: RoleUser, Content: "second message"},
		},
	})
	if capturedInput != "second message" {
		t.Errorf("expected last user message, got %q", capturedInput)
	}
}

func TestHermesProvider_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad input","type":"invalid_request"}}`))
	}))
	defer srv.Close()

	provider, _ := NewHermes(HermesConfig{BaseURL: srv.URL, Conversation: "t"})
	_, err := provider.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !containsStr(err.Error(), "400") {
		t.Errorf("expected HTTP 400 in error, got: %v", err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && strContains(s, sub))
}

func strContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
