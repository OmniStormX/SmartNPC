package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/smartnpc/smartnpc-agent/internal/agent/chat"
	"github.com/smartnpc/smartnpc-agent/internal/llm"
	"github.com/smartnpc/smartnpc-agent/internal/mcpclient"
)

func runAgent(ctx context.Context, mcpBin string, mcpExtraArgs []string, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	speaker := fs.String("speaker", "Abigail", "NPC display name (must match a game NPC for dialogue box)")
	baseURL := fs.String("llm-url", "http://192.168.59.118:8642/v1", "OpenAI-compatible API base URL")
	model := fs.String("model", "hermes-agent", "LLM model name")
	apiKey := fs.String("api-key", "", "LLM API key (defaults to OPENAI_API_KEY env var)")
	system := fs.String("system", "", "custom system prompt (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Fall back to env var if --api-key not provided
	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}

	provider, err := llm.NewOpenAI(llm.OpenAIConfig{
		APIKey:  key,
		BaseURL: *baseURL,
		Model:   *model,
	})
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}

	agent := chat.New(chat.Config{
		Provider:     provider,
		Speaker:      *speaker,
		SystemPrompt: *system,
	})

	cli, err := mcpclient.Spawn(ctx, mcpclient.Options{
		Binary:         mcpBin,
		Args:           mcpExtraArgs,
		LoggingHandler: agent.HandleNotification(),
	})
	if err != nil {
		return fmt.Errorf("spawn smartnpc-mcp: %w", err)
	}
	defer cli.Close()

	// Wire the live session into the agent so it can call tools.
	agent.SetSession(cli.Session())

	fmt.Fprintf(os.Stderr, "SmartNPC chat agent running (speaker=%s, model=%s)\n", *speaker, *model)
	fmt.Fprintf(os.Stderr, "Waiting for chat events... (Ctrl+C to stop)\n")

	<-ctx.Done()
	return nil
}
