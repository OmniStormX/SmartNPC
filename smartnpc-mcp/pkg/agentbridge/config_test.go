package agentbridge

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// withFreshRegistry isolates a test from globally-registered factories.
func withFreshRegistry(t *testing.T, fn func()) {
	t.Helper()
	registryMu.Lock()
	savedA, savedR := adapters, relays
	adapters = map[string]AdapterFactory{}
	relays = map[string]RelayFactory{}
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		adapters, relays = savedA, savedR
		registryMu.Unlock()
	}()
	fn()
}

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bridge.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadConfig_HappyPath(t *testing.T) {
	body := `adapters:
  - kind: stardew
    config:
      ws_url: ws://x/ws
relays:
  - kind: echo
transport:
  kind: http
  addr: ":3000"
`
	cfg, err := LoadConfig(writeYAML(t, body))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Adapters[0].Kind != "stardew" || cfg.Relays[0].Kind != "echo" {
		t.Errorf("kinds wrong: %+v", cfg)
	}
	if cfg.Transport.Kind != "http" || cfg.Transport.Addr != ":3000" {
		t.Errorf("transport wrong: %+v", cfg.Transport)
	}
	// Adapter config must be a yaml.Node we can re-decode.
	var ac struct {
		WSURL string `yaml:"ws_url"`
	}
	if err := cfg.Adapters[0].Config.Decode(&ac); err != nil {
		t.Fatalf("decode adapter cfg: %v", err)
	}
	if ac.WSURL != "ws://x/ws" {
		t.Errorf("ws_url = %q", ac.WSURL)
	}
}

func TestLoadConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"no_adapter", "transport:\n  kind: stdio\n", "at least one adapter"},
		{"adapter_missing_kind", "adapters:\n  - config: {}\ntransport:\n  kind: stdio\n", "kind is required"},
		{"transport_kind_missing", "adapters:\n  - kind: x\n", "transport.kind is required"},
		{"http_no_addr", "adapters:\n  - kind: x\ntransport:\n  kind: http\n", "transport.addr is required"},
		{"unknown_transport", "adapters:\n  - kind: x\ntransport:\n  kind: bogus\n", "want stdio | http"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeYAML(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	withFreshRegistry(t, func() {
		f := AdapterFactory(func(yaml.Node, *Server) error { return nil })
		RegisterAdapter("dup", f)
		defer func() {
			if recover() == nil {
				t.Error("expected panic on duplicate RegisterAdapter")
			}
		}()
		RegisterAdapter("dup", f)
	})
}

func TestAssemble_HappyPath_AdapterAndRelayInvoked(t *testing.T) {
	withFreshRegistry(t, func() {
		var adapterCalls, relayCalls atomic.Int32
		RegisterAdapter("fakeadapter", func(_ yaml.Node, _ *Server) error {
			adapterCalls.Add(1)
			return nil
		})
		RegisterRelay("fakerelay", func(_ yaml.Node, _ *slog.Logger) (Backend, error) {
			relayCalls.Add(1)
			return &noopBackend{}, nil
		})
		cfg := &BridgeConfig{
			Adapters:  []ComponentSpec{{Kind: "fakeadapter"}},
			Relays:    []ComponentSpec{{Kind: "fakerelay"}},
			Transport: TransportSpec{Kind: "stdio"},
		}
		m := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
		srv, err := cfg.Assemble(m, nil)
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if adapterCalls.Load() != 1 || relayCalls.Load() != 1 {
			t.Errorf("adapter=%d relay=%d, want 1/1", adapterCalls.Load(), relayCalls.Load())
		}
		// The relay backend must have been attached.
		if len(srv.backends) != 1 {
			t.Errorf("backends attached = %d, want 1", len(srv.backends))
		}
	})
}

func TestAssemble_UnknownAdapterKind(t *testing.T) {
	withFreshRegistry(t, func() {
		cfg := &BridgeConfig{
			Adapters:  []ComponentSpec{{Kind: "ghost"}},
			Transport: TransportSpec{Kind: "stdio"},
		}
		m := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
		_, err := cfg.Assemble(m, nil)
		if err == nil || !strings.Contains(err.Error(), `unknown kind "ghost"`) {
			t.Errorf("err = %v, want unknown-kind", err)
		}
	})
}

func TestAssemble_AdapterFactoryError(t *testing.T) {
	withFreshRegistry(t, func() {
		want := errors.New("adapter init boom")
		RegisterAdapter("bad", func(_ yaml.Node, _ *Server) error { return want })
		cfg := &BridgeConfig{
			Adapters:  []ComponentSpec{{Kind: "bad"}},
			Transport: TransportSpec{Kind: "stdio"},
		}
		m := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
		_, err := cfg.Assemble(m, nil)
		if err == nil || !errors.Is(err, want) {
			t.Errorf("err = %v, want chain to %v", err, want)
		}
	})
}

// noopBackend satisfies Backend with a no-op Forward; used by tests that
// only care that a backend was constructed and attached.
type noopBackend struct{}

func (*noopBackend) Name() string                                  { return "noop" }
func (*noopBackend) Forward(context.Context, eventbus.Event) error { return nil }
