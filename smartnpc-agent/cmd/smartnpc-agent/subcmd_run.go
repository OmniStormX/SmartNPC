package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/agent/chat"
	"github.com/smartnpc/smartnpc-agent/internal/llm"
	"github.com/smartnpc/smartnpc-agent/internal/mcpclient"
)

func runAgent(ctx context.Context, mcpBin string, mcpExtraArgs []string, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	speaker := fs.String("speaker", "Abigail", "NPC display name (must match a game NPC for dialogue box)")

	// ── Persona LLM (the in-character role-play model) ─────────────
	// --llm-url remains the primary knob; --persona-url is a preferred alias
	// introduced alongside the dual-LLM architecture. If both are set,
	// --persona-url wins.
	baseURL := fs.String("llm-url", "",
		"OpenAI-compatible API base URL for the persona LLM "+
			"(defaults to $OPENAI_BASE_URL, then https://api.openai.com/v1)")
	personaURL := fs.String("persona-url", "",
		"alias for --llm-url; when set, overrides --llm-url for the persona LLM")
	model := fs.String("model", "", "persona LLM model name (defaults to $OPENAI_MODEL, then gpt-4o-mini)")
	apiKey := fs.String("api-key", "", "persona LLM API key (defaults to $OPENAI_API_KEY)")

	// ── Decision LLM (optional; enables dual-LLM mode when --decision-url set) ──
	decisionURL := fs.String("decision-url", "",
		"base URL for the decision LLM (dual-LLM mode). "+
			"When empty, runs the single-LLM loop for backward compatibility.")
	decisionModel := fs.String("decision-model", "gpt-5.5",
		"model name for the decision LLM (only used when --decision-url is set)")
	decisionKey := fs.String("decision-key", "",
		"API key for the decision LLM (defaults to $OPENAI_API_KEY)")

	system := fs.String("system", "", "custom system prompt (optional)")
	persona := fs.String("persona", "", "path to persona JSON file (overrides --system)")
	personasDir := fs.String("personas-dir", "",
		"path to a directory of persona JSON files. When set, one Agent is "+
			"instantiated per file and events are routed by their `npc` field. "+
			"Mutually exclusive with --persona.")
	timeout := fs.Duration("llm-timeout", 90*time.Second, "timeout per LLM request")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *persona != "" && *personasDir != "" {
		return fmt.Errorf("--persona and --personas-dir are mutually exclusive")
	}

	// Resolve persona provider config with flag > env > provider default precedence.
	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	// --persona-url wins over --llm-url; both fall back to $OPENAI_BASE_URL.
	url := *personaURL
	if url == "" {
		url = *baseURL
	}
	if url == "" {
		url = os.Getenv("OPENAI_BASE_URL")
	}
	mdl := *model
	if mdl == "" {
		mdl = os.Getenv("OPENAI_MODEL")
	}

	personaProvider, err := llm.NewOpenAI(llm.OpenAIConfig{
		APIKey:  key,
		BaseURL: url,
		Model:   mdl,
	})
	if err != nil {
		return fmt.Errorf("create persona LLM provider: %w", err)
	}

	// Resolve decision provider (dual-LLM mode) when --decision-url is set.
	// Flag > $SMARTNPC_DECISION_URL env fallback > empty (single-LLM mode).
	decURL := *decisionURL
	if decURL == "" {
		decURL = os.Getenv("SMARTNPC_DECISION_URL")
	}
	var decisionProvider llm.Provider
	if decURL != "" {
		dkey := *decisionKey
		if dkey == "" {
			dkey = os.Getenv("OPENAI_API_KEY")
		}
		decisionProvider, err = llm.NewOpenAI(llm.OpenAIConfig{
			APIKey:  dkey,
			BaseURL: decURL,
			Model:   *decisionModel,
		})
		if err != nil {
			return fmt.Errorf("create decision LLM provider: %w", err)
		}
	}

	// buildAgent turns a loaded persona (possibly nil) + system prompt into a
	// fully-wired chat.Agent. Shared by both the single-persona and
	// personas-dir paths so their Agent configuration stays in lockstep.
	buildAgent := func(speakerName, sysPrompt string, p *chat.Persona) *chat.Agent {
		cfg := chat.Config{
			Provider:     personaProvider,
			Speaker:      speakerName,
			SystemPrompt: sysPrompt,
			Persona:      p,
			Timeout:      *timeout,
		}
		if decisionProvider != nil {
			cfg.DecisionProvider = decisionProvider
			cfg.PersonaProvider = personaProvider
			cfg.DecisionModel = *decisionModel
			cfg.PersonaModel = mdl
		}
		return chat.New(cfg)
	}

	// Resolve agents: --personas-dir → multi-agent router; --persona or
	// --system → single agent.
	var (
		agents          []*chat.Agent
		modeDescription string
	)
	switch {
	case *personasDir != "":
		agents, err = loadPersonasDir(*personasDir, buildAgent)
		if err != nil {
			return fmt.Errorf("load personas dir: %w", err)
		}
		if len(agents) == 0 {
			return fmt.Errorf("no persona JSON files found in %s", *personasDir)
		}
		names := make([]string, len(agents))
		for i, a := range agents {
			names[i] = a.Speaker()
		}
		modeDescription = fmt.Sprintf("MULTI-NPC mode (%d agents: %s)", len(agents), strings.Join(names, ", "))
	default:
		// Single-agent path (persona file or --system/--speaker defaults).
		systemPrompt := *system
		var loadedPersona *chat.Persona
		if *persona != "" {
			p, err := chat.LoadPersona(*persona)
			if err != nil {
				return fmt.Errorf("load persona: %w", err)
			}
			systemPrompt = p.SystemPrompt
			loadedPersona = p
			if *speaker == "Abigail" && p.Speaker != "" {
				*speaker = p.Speaker
			}
		}
		agents = []*chat.Agent{buildAgent(*speaker, systemPrompt, loadedPersona)}
		modeDescription = "single-agent mode (speaker=" + *speaker + ")"
	}

	// Build the router and register each agent by its speaker name. The
	// Register API (instead of a variadic constructor) makes it easier for
	// future code paths to add agents on the fly.
	router := chat.NewRouter()
	for _, a := range agents {
		router.Register(a.Speaker(), a)
	}

	cli, err := mcpclient.Spawn(ctx, mcpclient.Options{
		Binary:         mcpBin,
		Args:           mcpExtraArgs,
		LoggingHandler: router.HandleNotification(),
	})
	if err != nil {
		return fmt.Errorf("spawn smartnpc-mcp: %w", err)
	}
	defer cli.Close()

	// Wire session and load available tools for every agent. All agents
	// share the same MCP session and tool catalogue by design.
	router.SetSession(cli.Session())
	if err := router.LoadTools(ctx); err != nil {
		// Non-fatal: agents work without tools, they just can't call them.
		fmt.Fprintf(os.Stderr, "warning: failed to load tools: %v\n", err)
	}

	if decisionProvider != nil {
		fmt.Fprintf(os.Stderr,
			"SmartNPC chat agent running in DUAL-LLM mode (%s, decision=%s @ %s, persona=%s @ %s, timeout=%s)\n",
			modeDescription, *decisionModel, decURL, mdl, url, *timeout)
	} else {
		fmt.Fprintf(os.Stderr,
			"SmartNPC chat agent running (%s, model=%s, timeout=%s)\n",
			modeDescription, mdl, *timeout)
	}
	fmt.Fprintf(os.Stderr, "Waiting for NPC events (chat_message / npc_interact / chat_received)... (Ctrl+C to stop)\n")

	// Block until the user cancels (Ctrl+C) or the signal context fires.
	// Events are handled in goroutines dispatched by the MCP client's
	// LoggingMessageHandler — this main goroutine just keeps the process
	// alive so the session stays open.
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "shutting down...")
	return nil
}

// loadPersonasDir scans dir for *.json files, parses each one as a persona,
// and builds one Agent per file using the supplied factory. Sorted by
// filename so a `ls personas/` matches the insertion order in the router,
// which keeps diagnostics predictable.
func loadPersonasDir(dir string, build func(string, string, *chat.Persona) *chat.Agent) ([]*chat.Agent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)

	var agents []*chat.Agent
	for _, p := range paths {
		persona, err := chat.LoadPersona(p)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", p, err)
		}
		if persona.Speaker == "" {
			return nil, fmt.Errorf("persona %s missing \"speaker\" field", p)
		}
		agents = append(agents, build(persona.Speaker, persona.SystemPrompt, persona))
	}
	return agents, nil
}
