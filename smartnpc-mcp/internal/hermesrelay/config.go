package hermesrelay

import (
	"fmt"
	"os"

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
		}
		if p.APIKeyEnv != "" {
			cfg.APIKey = os.Getenv(p.APIKeyEnv)
		}
		out = append(out, cfg)
	}
	return out, nil
}
