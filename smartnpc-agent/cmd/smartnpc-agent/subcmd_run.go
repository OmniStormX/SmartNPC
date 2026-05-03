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
	baseURL := fs.String("llm-url", "",
		"OpenAI-compatible API base URL (defaults to $OPENAI_BASE_URL, "+
			"then https://api.openai.com/v1)")
	model := fs.String("model", "", "LLM model name (defaults to $OPENAI_MODEL, then gpt-4o-mini)")
	apiKey := fs.String("api-key", "", "LLM API key (defaults to $OPENAI_API_KEY)")
	system := fs.String("system", "", "custom system prompt (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve config with flag > env > provider default precedence.
	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	url := *baseURL
	if url == "" {
		url = os.Getenv("OPENAI_BASE_URL")
	}
	mdl := *model
	if mdl == "" {
		mdl = os.Getenv("OPENAI_MODEL")
	}

	provider, err := llm.NewOpenAI(llm.OpenAIConfig{
		APIKey:  key,
		BaseURL: url,
		Model:   mdl,
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

	effectiveModel := mdl
	if effectiveModel == "" {
		effectiveModel = "gpt-4o-mini"
	}
	fmt.Fprintf(os.Stderr, "SmartNPC chat agent running (speaker=%s, model=%s)\n", *speaker, effectiveModel)
	fmt.Fprintf(os.Stderr, "Waiting for chat events... (Ctrl+C to stop)\n")

	<-ctx.Done()
	return nil
}
