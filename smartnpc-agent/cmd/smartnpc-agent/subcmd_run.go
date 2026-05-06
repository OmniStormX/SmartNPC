package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

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
	persona := fs.String("persona", "", "path to persona JSON file (overrides --system)")
	timeout := fs.Duration("llm-timeout", 90*time.Second, "timeout per LLM request")
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

	// Resolve system prompt: persona file > --system flag > default
	systemPrompt := *system
	if *persona != "" {
		p, err := chat.LoadPersona(*persona)
		if err != nil {
			return fmt.Errorf("load persona: %w", err)
		}
		systemPrompt = p.SystemPrompt
		if *speaker == "Abigail" && p.Speaker != "" {
			*speaker = p.Speaker
		}
	}

	agent := chat.New(chat.Config{
		Provider:     provider,
		Speaker:      *speaker,
		SystemPrompt: systemPrompt,
		Timeout:      *timeout,
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

	// Wire session and load available tools for the LLM.
	agent.SetSession(cli.Session())
	if err := agent.LoadTools(ctx); err != nil {
		// Non-fatal: agent works without tools, just can't call them.
		fmt.Fprintf(os.Stderr, "warning: failed to load tools: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "SmartNPC chat agent running (speaker=%s, model=%s, timeout=%s)\n", *speaker, *model, *timeout)
	fmt.Fprintf(os.Stderr, "Waiting for chat events... (Ctrl+C to stop)\n")

	<-ctx.Done()
	return nil
}
