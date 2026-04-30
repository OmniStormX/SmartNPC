// Package llm defines the LLM provider abstraction used by NPC agents.
//
// M1 only provides the interface and a stub OpenAI implementation. The
// concrete OpenAI integration (function calling + MCP-tool bridging) lands in
// M4.
package llm

import "context"

// Role identifies the speaker of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single turn in a chat conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Name optionally identifies the tool / function that produced this
	// message (only meaningful when Role == RoleTool).
	Name string `json:"name,omitempty"`
	// ToolCallID correlates a tool result with the assistant's tool_call.
	ToolCallID string `json:"toolCallId,omitempty"`
	// ToolCalls is populated when an assistant message requests tool invocations.
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
}

// ToolSpec describes a callable tool exposed to the model. The actual JSON
// schema is opaque here so that we can forward MCP tool schemas verbatim.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ChatRequest is a single round trip to the LLM provider.
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolSpec
	Temperature float64
	MaxTokens   int
}

// ChatResponse is the model's reply.
type ChatResponse struct {
	// Content is non-empty when the model produced a final text answer.
	Content string
	// ToolCalls is non-empty when the model wants the agent to invoke tools
	// before continuing.
	ToolCalls []ToolCall
	// FinishReason is provider-specific (e.g. "stop", "tool_calls", "length").
	FinishReason string
}

// Provider is implemented by concrete LLM backends.
type Provider interface {
	// Chat performs a single completion. Implementations must respect ctx
	// cancellation.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// Name identifies the provider for logging.
	Name() string
}
