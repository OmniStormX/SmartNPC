package agentbridge

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// AdapterFactory builds and registers an adapter onto the given Server.
//
// cfg is the raw `config:` subtree from bridge.yaml; the factory is
// responsible for decoding it into its own concrete Config struct via
// cfg.Decode(&typed). Use yaml.Node so each adapter owns its schema —
// the framework never enumerates known adapter fields.
//
// Implementations typically call srv.AttachEventSource and srv.Mount;
// they may also stash adapter state (ws clients, etc.) in closures.
// Returning an error aborts CLI startup.
type AdapterFactory func(cfg yaml.Node, srv *Server) error

// RelayFactory builds a Backend from a yaml config subtree.
//
// Same Decode contract as AdapterFactory. The returned Backend is
// attached to the Server by the caller (LoadAndAssemble), not by
// the factory itself — this keeps the factory pure.
type RelayFactory func(cfg yaml.Node, logger *slog.Logger) (Backend, error)

// Global registries. Populated by adapter / relay packages via init()
// (or by tests via Register*ForTest below). Reads are guarded so init()
// races don't trip the race detector even though Go guarantees init()
// ordering within a single binary.
var (
	registryMu sync.RWMutex
	adapters   = map[string]AdapterFactory{}
	relays     = map[string]RelayFactory{}
)

// RegisterAdapter associates a kind string with a factory. Re-registering
// the same kind panics — duplicate registration is a programming error
// (typically two init() functions racing for the same name).
//
// Convention: lowercase ASCII, single word ("stardew", "minecraft").
func RegisterAdapter(kind string, f AdapterFactory) {
	if kind == "" || f == nil {
		panic("agentbridge.RegisterAdapter: empty kind or nil factory")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := adapters[kind]; dup {
		panic(fmt.Sprintf("agentbridge.RegisterAdapter: duplicate kind %q", kind))
	}
	adapters[kind] = f
}

// RegisterRelay is the relay-flavored counterpart to RegisterAdapter.
func RegisterRelay(kind string, f RelayFactory) {
	if kind == "" || f == nil {
		panic("agentbridge.RegisterRelay: empty kind or nil factory")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := relays[kind]; dup {
		panic(fmt.Sprintf("agentbridge.RegisterRelay: duplicate kind %q", kind))
	}
	relays[kind] = f
}

// LookupAdapter returns the factory registered under kind, or nil.
// Exposed for tests and diagnostics; CLI startup uses LoadAndAssemble.
func LookupAdapter(kind string) AdapterFactory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return adapters[kind]
}

// LookupRelay is the relay-flavored counterpart to LookupAdapter.
func LookupRelay(kind string) RelayFactory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return relays[kind]
}

// RegisteredAdapterKinds returns the sorted list of registered adapter
// kinds — used in CLI error messages ("unknown adapter %q; known: %v").
func RegisteredAdapterKinds() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(adapters))
	for k := range adapters {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RegisteredRelayKinds is the relay-flavored counterpart.
func RegisteredRelayKinds() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(relays))
	for k := range relays {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// resetRegistriesForTest clears both registries. Used by tests that
// install ad-hoc factories and need to leave the global state clean.
// NOT exported — tests in the same package call this directly.
func resetRegistriesForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	adapters = map[string]AdapterFactory{}
	relays = map[string]RelayFactory{}
}
