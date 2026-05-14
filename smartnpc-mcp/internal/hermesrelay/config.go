package hermesrelay

import (
	"fmt"
	"os"
	"strings"

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
	PersonaFile  string `yaml:"persona_file,omitempty"`
	TimeoutMS    int    `yaml:"timeout_ms,omitempty"`
}

// LoadConfigFile reads a multi-profile runtime config and returns a slice of
// Config values ready to feed into NewGroup. API keys are resolved from env
// vars at load time; a missing env var produces an empty APIKey (which the
// underlying Relay treats as "no Authorization header").
//
// Env var SMARTNPC_RELAY_DEBUG_PAYLOAD=1 turns on per-turn dump of the
// outbound request body + inbound response body via slog.Debug. Off by
// default — the payload contains the full persona and can be 10-50KB.
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
	debugPayload := DebugPayloadEnabled()
	out := make([]Config, 0, len(file.Profiles))
	for i, p := range file.Profiles {
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
			URL:          p.GatewayURL,
			Conversation: p.Conversation,
			Model:        p.Model,
			NPCName:      p.NPCFilter,
			PersonaFile:  p.PersonaFile,
			DebugPayload: debugPayload,
		}
		if p.APIKeyEnv != "" {
			cfg.APIKey = os.Getenv(p.APIKeyEnv)
		}
		out = append(out, cfg)
	}
	return out, nil
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
