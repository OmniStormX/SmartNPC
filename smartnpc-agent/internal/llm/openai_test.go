package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChat_SimpleTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var req oaiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "hermes-agent" {
			t.Errorf("expected model hermes-agent, got %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}

		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message:      oaiMessage{Role: "assistant", Content: "Hello there!"},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	p, err := NewOpenAI(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "hermes-agent",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a helpful NPC."},
			{Role: RoleUser, Content: "Hi!"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello there!" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
}

func TestChat_ToolCallResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message: oaiMessage{
					Role: "assistant",
					ToolCalls: []oaiToolCall{{
						ID:   "call_123",
						Type: "function",
						Function: oaiToolCallFunc{
							Name:      "chat_say",
							Arguments: `{"speaker":"NPC","text":"hello"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		})
	}))
	defer srv.Close()

	p, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "test"})
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "say hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" || tc.Name != "chat_say" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit"}}`))
	}))
	defer srv.Close()

	p, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "test"})
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestChat_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(oaiResponse{Choices: []oaiChoice{}})
	}))
	defer srv.Close()

	p, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "test"})
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestChat_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header when APIKey is empty")
		}
		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{
				Message:      oaiMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	p, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewOpenAI should allow empty APIKey: %v", err)
	}
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil || resp.Content != "ok" {
		t.Errorf("unexpected: resp=%v, err=%v", resp, err)
	}
}

func TestChat_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	p, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Chat(ctx, ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
