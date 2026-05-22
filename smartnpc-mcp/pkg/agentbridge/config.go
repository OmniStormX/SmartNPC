package agentbridge

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BridgeConfig is the on-disk shape of bridge.yaml.
//
// Example:
//
//	adapters:
//	  - kind: stardew
//	    config:
//	      ws_url: ws://127.0.0.1:18745/ws
//	relays:
//	  - kind: hermes
//	    config:
//	      runtime_config: ./hermes/runtime-config.yaml
//	  - kind: echo
//	transport:
//	  kind: http
//	  addr: ":3000"
//
// Schema decisions:
//
//   - `kind` is the registry key (matches RegisterAdapter / RegisterRelay).
//   - `config:` is captured as yaml.Node so each factory owns its schema.
//     The framework never enumerates inner fields.
//   - Lists, not maps, so adapters/relays can repeat the same kind in the
//     future (e.g. two stardew bridges to two SDV instances).
//
// Transport.Kind today must be "http" or "stdio". When "http", Addr is
// required; transport.config{} is reserved for future fields.
type BridgeConfig struct {
	Adapters  []ComponentSpec `yaml:"adapters"`
	Relays    []ComponentSpec `yaml:"relays,omitempty"`
	Transport TransportSpec   `yaml:"transport"`
}

// ComponentSpec is one entry under `adapters:` or `relays:`.
type ComponentSpec struct {
	Kind   string    `yaml:"kind"`
	Config yaml.Node `yaml:"config,omitempty"`
}

// TransportSpec configures the MCP transport that exposes the assembled
// Server to MCP clients.
type TransportSpec struct {
	Kind string `yaml:"kind"`           // "stdio" | "http"
	Addr string `yaml:"addr,omitempty"` // required when Kind == "http"
}

// LoadConfig reads and decodes bridge.yaml from disk. Returns a
// validation error when required fields are missing.
//
// Validation here is intentionally lightweight: kinds are not yet
// dereferenced against the registry (LoadAndAssemble does that), so a
// caller can LoadConfig in one process and assemble in another (e.g.
// dry-run vs run).
func LoadConfig(path string) (*BridgeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agentbridge: read %q: %w", path, err)
	}
	var cfg BridgeConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("agentbridge: parse %q: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("agentbridge: %q: %w", path, err)
	}
	return &cfg, nil
}

func (c *BridgeConfig) validate() error {
	if len(c.Adapters) == 0 {
		return fmt.Errorf("at least one adapter is required")
	}
	for i, a := range c.Adapters {
		if a.Kind == "" {
			return fmt.Errorf("adapters[%d]: kind is required", i)
		}
	}
	for i, r := range c.Relays {
		if r.Kind == "" {
			return fmt.Errorf("relays[%d]: kind is required", i)
		}
	}
	switch c.Transport.Kind {
	case "stdio":
		// no addr required
	case "http":
		if c.Transport.Addr == "" {
			return fmt.Errorf("transport.addr is required when kind=http")
		}
	case "":
		return fmt.Errorf("transport.kind is required (stdio | http)")
	default:
		return fmt.Errorf("transport.kind = %q, want stdio | http", c.Transport.Kind)
	}
	return nil
}

// Assemble materializes a Server from the given BridgeConfig: builds
// each adapter via the registry, builds and attaches each relay, and
// registers the framework-level meta tools (ping). Returns the Server
// ready for caller-driven Run; does NOT itself start the MCP transport.
//
// mcpServer is owned by the caller (so callers can pass through any
// mcp.ServerOptions); Assemble borrows it for tool registration and
// notification fan-out.
//
// Errors are wrapped with the offending kind / index so a typo in
// bridge.yaml points at the offending line.
func (c *BridgeConfig) Assemble(mcpServer *mcp.Server, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	srv := New(mcpServer, Options{Logger: logger})
	RegisterMeta(mcpServer)

	// Adapters first so attached EventSources are ready when Run launches.
	for i, spec := range c.Adapters {
		factory := LookupAdapter(spec.Kind)
		if factory == nil {
			return nil, fmt.Errorf("adapters[%d]: unknown kind %q (registered: %v)",
				i, spec.Kind, RegisteredAdapterKinds())
		}
		if err := factory(spec.Config, srv); err != nil {
			return nil, fmt.Errorf("adapters[%d] (%s): %w", i, spec.Kind, err)
		}
	}

	for i, spec := range c.Relays {
		factory := LookupRelay(spec.Kind)
		if factory == nil {
			return nil, fmt.Errorf("relays[%d]: unknown kind %q (registered: %v)",
				i, spec.Kind, RegisteredRelayKinds())
		}
		backend, err := factory(spec.Config, logger)
		if err != nil {
			return nil, fmt.Errorf("relays[%d] (%s): %w", i, spec.Kind, err)
		}
		srv.AttachBackend(backend)
	}

	return srv, nil
}
