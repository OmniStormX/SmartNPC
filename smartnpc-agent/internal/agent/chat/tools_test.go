package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// fakeToolInput mirrors an MCP tool's typed input so AddTool can derive a
// JSON Schema automatically.
type fakeToolInput struct {
	Speaker string `json:"speaker" jsonschema:"NPC display name"`
	Text    string `json:"text"    jsonschema:"message to display"`
}

type fakeToolOutput struct {
	OK bool `json:"ok"`
}

// newInMemoryAgent spins up an in-memory MCP server + client pair, registers
// a few fake tools, and returns an Agent wired to that client session along
// with the tool name → last-call-args captured by the server.
func newInMemoryAgent(t *testing.T, toolNames ...string) (*Agent, map[string]map[string]any) {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "t"}, nil)

	calls := make(map[string]map[string]any)
	for _, name := range toolNames {
		name := name
		mcp.AddTool(server, &mcp.Tool{
			Name:        name,
			Description: "fake tool " + name,
		}, func(_ context.Context, _ *mcp.CallToolRequest, in fakeToolInput) (*mcp.CallToolResult, fakeToolOutput, error) {
			calls[name] = map[string]any{"speaker": in.Speaker, "text": in.Text}
			return nil, fakeToolOutput{OK: true}, nil
		})
	}

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "agent-test", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	agent := New(Config{
		Provider:     &mockProvider{},
		Speaker:      "NPC",
		SystemPrompt: "test",
		MaxHistory:   10,
	})
	agent.SetSession(cs)
	return agent, calls
}

// TestLoadTools_ConvertsMCPSchema is an end-to-end check: the agent connects
// to a real (in-memory) MCP server, calls ListTools, and we verify that every
// tool surfaces with the expected name / description / JSON Schema shape.
func TestLoadTools_ConvertsMCPSchema(t *testing.T) {
	agent, _ := newInMemoryAgent(t, "chat_say", "mail_send")

	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	tools := agent.Tools()
	if len(tools) < 2 {
		t.Fatalf("expected >= 2 tools, got %d", len(tools))
	}

	byName := make(map[string]llm.ToolSpec, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	for _, want := range []string{"chat_say", "mail_send"} {
		tool, ok := byName[want]
		if !ok {
			t.Fatalf("tool %s missing from LoadTools output", want)
		}
		if tool.Description == "" {
			t.Errorf("tool %s: description is empty", want)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %s: InputSchema is nil", want)
		}
		if typ, _ := tool.InputSchema["type"].(string); typ != "object" {
			t.Errorf("tool %s: schema type = %v, want object", want, tool.InputSchema["type"])
		}
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s: properties missing; schema=%v", want, tool.InputSchema)
		}
		if _, ok := props["speaker"]; !ok {
			t.Errorf("tool %s: missing 'speaker' property; props=%v", want, props)
		}
		if _, ok := props["text"]; !ok {
			t.Errorf("tool %s: missing 'text' property; props=%v", want, props)
		}
	}
}

// TestLoadTools_PropagatesToProvider closes the loop: after LoadTools, the
// next respond() call must forward the freshly-discovered tools into the LLM
// request.
func TestLoadTools_PropagatesToProvider(t *testing.T) {
	agent, _ := newInMemoryAgent(t, "chat_say")
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "ok", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	if _, err := agent.respond(context.Background(), "hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
	}
	forwarded := mp.calls[0].Tools
	if len(forwarded) < 1 {
		t.Fatalf("expected tools passed to provider, got %d", len(forwarded))
	}
	var found bool
	for _, tool := range forwarded {
		if tool.Name == "chat_say" {
			found = true
			if tool.InputSchema == nil {
				t.Error("chat_say: InputSchema should not be nil when forwarded to provider")
			}
		}
	}
	if !found {
		t.Errorf("chat_say not forwarded to provider; tools=%+v", forwarded)
	}
}

// TestExecuteTool_InvokesMCPCall verifies the tool execution path round-trips
// through the MCP session: args get delivered to the server and the structured
// result comes back as a JSON string that the LLM can consume.
func TestExecuteTool_InvokesMCPCall(t *testing.T) {
	agent, calls := newInMemoryAgent(t, "chat_say")

	out, err := agent.executeTool(context.Background(), llm.ToolCall{
		ID:   "call_1",
		Name: "chat_say",
		Arguments: map[string]any{
			"speaker": "NPC",
			"text":    "hi there",
		},
	})
	if err != nil {
		t.Fatalf("executeTool: %v", err)
	}

	if calls["chat_say"] == nil {
		t.Fatal("MCP server did not receive chat_say call")
	}
	if calls["chat_say"]["speaker"] != "NPC" || calls["chat_say"]["text"] != "hi there" {
		t.Errorf("unexpected args: %v", calls["chat_say"])
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("tool output not JSON: %v (raw=%s)", err, out)
	}
	if parsed["ok"] != true {
		t.Errorf("expected ok=true, got %v", parsed)
	}
}

func TestExecuteTool_NoSession(t *testing.T) {
	agent := New(Config{Provider: &mockProvider{}, Speaker: "NPC", MaxHistory: 10})
	_, err := agent.executeTool(context.Background(), llm.ToolCall{Name: "chat_say"})
	if err == nil {
		t.Fatal("expected error when session is nil")
	}
}
