package hermesrelay

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// runtimeConfigFile mirrors hermes/runtime-config.yaml.
type runtimeConfigFile struct {
	Profiles []profileEntry `yaml:"profiles"`
}

type profileEntry struct {
	Name         string `yaml:"name"`
	NPCFilter    string `yaml:"npc_filter"`
	GatewayURL   string `yaml:"gateway_url"`
	Conversation string `yaml:"conversation"`
	Model        string `yaml:"model"`
	APIKeyEnv    string `yaml:"api_key_env"`
	PersonaFile        string `yaml:"persona_file,omitempty"`
	CriticalPolicyFile string `yaml:"critical_policy_file,omitempty"`
	TimeoutMS          int    `yaml:"timeout_ms,omitempty"`
}

// LoadConfigFile reads a multi-profile runtime config and returns a slice of
// Config values ready to feed into NewGroup. API keys are resolved from env
// vars at load time; a missing env var produces an empty APIKey (which the
// underlying Relay treats as "no Authorization header").
//
// Env vars consumed at load time:
//
//	SMARTNPC_RELAY_DEBUG_PAYLOAD=1            — emit outbound + inbound full
//	                                            body Debug records per turn.
//	SMARTNPC_RELAY_PAYLOAD_LOG=<path>         — when set, the Debug records
//	                                            above go to this file (JSON
//	                                            lines) instead of the main
//	                                            slog stream. Implicitly turns
//	                                            DEBUG_PAYLOAD on.
//	SMARTNPC_RELAY_STORE=0|false              — disable Hermes server-side
//	                                            conversation store. Default
//	                                            on; only flip when debugging
//	                                            duplicate-memory issues.
//	SMARTNPC_RELAY_TIMEOUT_MS=<int>           — POST timeout. Default 120000.
func expandRuntimeValue(value string) string {
	return os.Expand(value, func(key string) string {
		if key == "SMARTNPC_HERMES_GATEWAY_HOST" {
			if v := strings.TrimSpace(os.Getenv(key)); v != "" {
				return v
			}
			if v := strings.TrimSpace(os.Getenv("WSL_IP")); v != "" {
				return v
			}
			return "127.0.0.1"
		}
		return os.Getenv(key)
	})
}

func LoadConfigFile(path string) ([]Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hermesrelay: read %q: %w", path, err)
	}
	var file runtimeConfigFile
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("hermesrelay: parse %q: %w", path, err)
	}
	if len(file.Profiles) == 0 {
		return nil, fmt.Errorf("hermesrelay: %q has empty profiles list", path)
	}
	payloadLogger, payloadEnabled, err := payloadLoggerFromEnv()
	if err != nil {
		return nil, err
	}
	debugPayload := DebugPayloadEnabled() || payloadEnabled
	store := StoreFromEnv()
	timeout := TimeoutFromEnv()
	out := make([]Config, 0, len(file.Profiles))
	for i, p := range file.Profiles {
		p.GatewayURL = expandRuntimeValue(p.GatewayURL)
		if p.GatewayURL == "" {
			return nil, fmt.Errorf("hermesrelay: profile %d (%s): gateway_url required", i, p.Name)
		}
		if p.Conversation == "" {
			return nil, fmt.Errorf("hermesrelay: profile %d (%s): conversation required", i, p.Name)
		}
		if p.Model == "" {
			return nil, fmt.Errorf("hermesrelay: profile %d (%s): model required", i, p.Name)
		}
		cfg := Config{
			URL:           p.GatewayURL,
			Conversation:  p.Conversation,
			Model:         p.Model,
			NPCName:       p.NPCFilter,
			PersonaFile:        p.PersonaFile,
				CriticalPolicyFile: p.CriticalPolicyFile,
			DebugPayload:  debugPayload,
			PayloadLogger: payloadLogger,
			Store:         store,
			Timeout:       timeout,
		}
		if p.APIKeyEnv != "" {
			cfg.APIKey = os.Getenv(p.APIKeyEnv)
		}
		out = append(out, cfg)
	}
	return out, nil
}

// StoreFromEnv resolves SMARTNPC_RELAY_STORE, defaulting to true so Hermes
// owns the conversation history (with its compression layer). mcp does NOT
// maintain its own short-window history; this knob exists only as an escape
// hatch for debugging.
func StoreFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTNPC_RELAY_STORE")))
	switch v {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// TimeoutFromEnv resolves SMARTNPC_RELAY_TIMEOUT_MS, defaulting to 120s.
// Values <1000ms or non-numeric clamp to the default — the LLM round trip
// dominates this number and should never be sub-second.
func TimeoutFromEnv() time.Duration {
	const defaultTimeout = 120 * time.Second
	v := strings.TrimSpace(os.Getenv("SMARTNPC_RELAY_TIMEOUT_MS"))
	if v == "" {
		return defaultTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1000 {
		return defaultTimeout
	}
	return time.Duration(n) * time.Millisecond
}

// DebugPayloadEnabled reports whether the env var that turns on full
// request/response body logging is set to a truthy value (1, true, yes, on,
// case-insensitive). Exported so callers building Configs without going
// through LoadConfigFile (e.g. the legacy --hermes-url flag path) can apply
// the same toggle.
func DebugPayloadEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTNPC_RELAY_DEBUG_PAYLOAD")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

var (
	payloadLoggerOnce sync.Once
	payloadLoggerVal  *slog.Logger
	payloadLoggerErr  error
)

// PayloadLoggerFromEnv returns a *slog.Logger bound to the path in
// SMARTNPC_RELAY_PAYLOAD_LOG, or (nil, false, nil) when the var is unset.
// Implicitly enables DebugPayload on the caller side. Cached: opening the
// file once across all Relays guarantees a single fd / one shared mutex.
//
// Exported so the legacy --hermes-url flag path in main.go can use the same
// resolution as LoadConfigFile.
func PayloadLoggerFromEnv() (*slog.Logger, bool, error) {
	return payloadLoggerFromEnv()
}

func payloadLoggerFromEnv() (*slog.Logger, bool, error) {
	payloadLoggerOnce.Do(func() {
		path := strings.TrimSpace(os.Getenv("SMARTNPC_RELAY_PAYLOAD_LOG"))
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			payloadLoggerErr = fmt.Errorf("hermesrelay: open SMARTNPC_RELAY_PAYLOAD_LOG=%q: %w", path, err)
			return
		}
		payloadLoggerVal = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	return payloadLoggerVal, payloadLoggerVal != nil, payloadLoggerErr
}
