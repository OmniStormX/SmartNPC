# Hermes-first Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut the SmartNPC runtime over from the frozen `smartnpc-agent` to per-NPC Hermes Agent profiles, with `smartnpc-mcp` as the single ws-side bridge doing multi-profile fan-out via a YAML config.

**Architecture:** Single `smartnpc-mcp` process runs `--http :3000` (MCP server) + `--hermes-config hermes/runtime-config.yaml` (outbound relay). The relay is refactored from one-target to a `Group` of `*Relay` instances, each gated by `NPCName`. Five new Hermes profiles (abigail, haley, harvey, penny, sebastian) ship file-ready; `run.bat` launches two (xiami + abigail). `smartnpc-agent` is not touched and not launched.

**Tech stack:** Go 1.25 (smartnpc-mcp), markdown (SOUL/skill), YAML (config-overlay + runtime-config), bash (WSL launcher), Windows CMD (`run.bat`).

**Reference spec:** [`docs/superpowers/specs/2026-05-12-hermes-first-migration-design.md`](../specs/2026-05-12-hermes-first-migration-design.md).

---

## File map

| Path | Action | Responsibility |
|------|--------|---------------|
| `smartnpc-mcp/internal/hermesrelay/relay.go` | Modify | Expose `ShouldRoute` (rename `shouldRoute` → `ShouldRoute`) so the new Group can ask "does this relay want this event?" without duplicating logic. |
| `smartnpc-mcp/internal/hermesrelay/group.go` | **Create** | `Group{relays []*Relay}` + `NewGroup([]Config, *slog.Logger) (*Group, error)` + `HandleEvent` fan-out that drops with a log when no relay matches. |
| `smartnpc-mcp/internal/hermesrelay/config.go` | **Create** | YAML loader: `LoadConfigFile(path) ([]Config, error)`. Resolves `api_key_env` → env var lookup at load time. |
| `smartnpc-mcp/internal/hermesrelay/group_test.go` | **Create** | Tests for fan-out routing, drop logging, env-var resolution, malformed YAML errors. |
| `smartnpc-mcp/internal/hermesrelay/config_test.go` | **Create** | Tests for `LoadConfigFile` (valid yaml, missing env var, unknown keys). |
| `smartnpc-mcp/cmd/smartnpc-mcp/main.go` | Modify | New `--hermes-config` flag; precedence over legacy single-target flags; instantiate `Group` instead of single `Relay`. |
| `smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go` | Modify | Add fan-out integration test: 2-profile config, send event with `npc=Abigail`, assert POST goes to abigail target only. |
| `hermes/runtime-config.yaml` | **Create** | The canonical 6-profile fan-out config consumed by mcp. |
| `hermes/profiles/abigail/{SOUL.md,config-overlay.yaml,skills/smartnpc/...}` | **Create** | Full profile, port 8643. |
| `hermes/profiles/haley/{SOUL.md,config-overlay.yaml,skills/smartnpc/...}` | **Create** | Full profile, port 8644. |
| `hermes/profiles/harvey/{SOUL.md,config-overlay.yaml,skills/smartnpc/...}` | **Create** | Full profile, port 8645. |
| `hermes/profiles/penny/{SOUL.md,config-overlay.yaml,skills/smartnpc/...}` | **Create** | Full profile, port 8646. |
| `hermes/profiles/sebastian/{SOUL.md,config-overlay.yaml,skills/smartnpc/...}` | **Create** | Full profile, port 8647. |
| `hermes/profiles/<all 6>/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md` | **Create** | New shared skill replacing `consult_npc` semantics (asker + receiver roles, query / behavioral / reply kinds). |
| `hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md` | Modify | Add cross-reference paragraph pointing to `inter-npc-message`. |
| `scripts/start_hermes_profiles.sh` | **Create** | WSL helper that starts named Hermes gateways in background, polls `/health`, writes PIDs to a tracking file. |
| `run.bat` | **Rewrite** | Drop smartnpc-agent launch; call `install.sh` + `ensure_hermes_aux.sh`; start mcp `--http :3000 --hermes-config ...`; start xiami + abigail gateways via `start_hermes_profiles.sh`; launch game. |
| `docs/architecture.md` | Modify | Replace single-target diagram with multi-profile fan-out diagram; describe `runtime-config.yaml`. |
| `docs/hermes-profiles.md` | Modify | Document `runtime-config.yaml` schema; rewrite "Multi-NPC checklist" section. |
| `docs/migration-smartnpc-agent.md` | Modify | Flip behavior parity rows ✅ for 5 new NPCs; group chat row stays ⚠️. |
| `docs/roadmap.md` | Modify | M5 (Hermes-first) → ✅ end-to-end verified; M6 carries group chat + smartnpc-agent archival. |
| `CLAUDE.md` | Modify | Update "正式链路" + "常用命令" to reflect Hermes-first as the default `run.bat` mode. |

---

## Phase A — Backend (Go) refactor

### Task 1: Expose `Relay.ShouldRoute`

**Files:**
- Modify: `smartnpc-mcp/internal/hermesrelay/relay.go:98-116`
- Modify: `smartnpc-mcp/internal/hermesrelay/relay_test.go` (rename any callers of `shouldRoute`)

- [ ] **Step 1: Inspect existing usage**

```bash
grep -rn "shouldRoute" smartnpc-mcp/
```
Expected: usage only inside `relay.go` (line 91 + 102) and possibly the test file.

- [ ] **Step 2: Rename `shouldRoute` → `ShouldRoute`**

In `smartnpc-mcp/internal/hermesrelay/relay.go`, change:

```go
// shouldRoute returns true when this event matches the relay's NPC filter.
// Events with no recipient field pass (broadcast). Malformed payloads are
// dropped when a filter is configured — the safe default for an NPC filter
// is "do not deliver" rather than "deliver to everyone".
func (r *Relay) shouldRoute(name string, data json.RawMessage) bool {
```

to:

```go
// ShouldRoute reports whether this event matches the relay's NPC filter.
// Events with no recipient field pass (broadcast). Malformed payloads are
// dropped when a filter is configured — the safe default for an NPC filter
// is "do not deliver" rather than "deliver to everyone".
//
// Exported so a Group can route a single event across multiple relays
// without each one re-doing the parse.
func (r *Relay) ShouldRoute(name string, data json.RawMessage) bool {
```

Then update the caller at line 91:

```go
func (r *Relay) HandleEvent(_ context.Context, name string, data json.RawMessage) {
	if !r.ShouldRoute(name, data) {
		return
	}
	input := events.FormatForHermes(name, data)
	go r.post(input, name)
}
```

- [ ] **Step 3: Run tests**

```powershell
& "C:\Users\synchen\go\bin\task.exe" mcp:test
```
Expected: PASS (no behavior change; existing tests must continue to pass).

- [ ] **Step 4: Commit**

```bash
git add smartnpc-mcp/internal/hermesrelay/
git commit -m "refactor(mcp): expose Relay.ShouldRoute for Group use"
```

---

### Task 2: YAML config loader

**Files:**
- Create: `smartnpc-mcp/internal/hermesrelay/config.go`
- Create: `smartnpc-mcp/internal/hermesrelay/config_test.go`
- Modify: `smartnpc-mcp/go.mod` (add yaml.v3 if not present)

- [ ] **Step 1: Check yaml.v3 dependency**

```bash
grep "gopkg.in/yaml.v3" smartnpc-mcp/go.mod
```
If absent, add it:

```bash
cd smartnpc-mcp && go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Write the failing test** at `smartnpc-mcp/internal/hermesrelay/config_test.go`:

```go
package hermesrelay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	yaml := `profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://127.0.0.1:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: abigail
    npc_filter: Abigail
    gateway_url: http://127.0.0.1:8643
    conversation: abigail
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("SMARTNPC_HERMES_KEY", "test-bearer")

	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("want 2 cfgs, got %d", len(cfgs))
	}
	if cfgs[0].URL != "http://127.0.0.1:8642" {
		t.Errorf("xiami URL = %q", cfgs[0].URL)
	}
	if cfgs[0].NPCName != "XiaMi" {
		t.Errorf("xiami NPCName = %q", cfgs[0].NPCName)
	}
	if cfgs[0].APIKey != "test-bearer" {
		t.Errorf("xiami APIKey not resolved from env: %q", cfgs[0].APIKey)
	}
	if cfgs[1].Conversation != "abigail" {
		t.Errorf("abigail Conversation = %q", cfgs[1].Conversation)
	}
}

func TestLoadConfigFile_MissingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	yaml := `profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://127.0.0.1:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: MISSING_VAR_XYZ
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	os.Unsetenv("MISSING_VAR_XYZ")

	cfgs, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfgs[0].APIKey != "" {
		t.Errorf("missing env should leave APIKey empty, got %q", cfgs[0].APIKey)
	}
}

func TestLoadConfigFile_NoProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	if err := os.WriteFile(path, []byte("profiles: []\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadConfigFile(path)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("want empty-profile error, got %v", err)
	}
}

func TestLoadConfigFile_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadConfigFile(path)
	if err == nil {
		t.Errorf("want parse error, got nil")
	}
}
```

- [ ] **Step 3: Run — expect FAIL ("LoadConfigFile undefined")**

```powershell
cd smartnpc-mcp; go test ./internal/hermesrelay/ -run TestLoadConfigFile -v
```

- [ ] **Step 4: Implement** `smartnpc-mcp/internal/hermesrelay/config.go`:

```go
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
```

- [ ] **Step 5: Run tests — expect PASS**

```powershell
cd smartnpc-mcp; go test ./internal/hermesrelay/ -run TestLoadConfigFile -v
```

- [ ] **Step 6: Commit**

```bash
git add smartnpc-mcp/internal/hermesrelay/config.go smartnpc-mcp/internal/hermesrelay/config_test.go smartnpc-mcp/go.mod smartnpc-mcp/go.sum
git commit -m "feat(mcp): hermesrelay YAML config loader"
```

---

### Task 3: `Group` fan-out

**Files:**
- Create: `smartnpc-mcp/internal/hermesrelay/group.go`
- Create: `smartnpc-mcp/internal/hermesrelay/group_test.go`

- [ ] **Step 1: Write the failing test** at `smartnpc-mcp/internal/hermesrelay/group_test.go`:

```go
package hermesrelay

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGateway captures POST bodies so a test can assert which gateway saw
// which event. Each request increments hits[name] keyed by a label set per
// test setup.
type fakeGateway struct {
	*httptest.Server
	label string
	hits  *atomic.Int32
}

func newFakeGateway(t *testing.T, label string, hits *atomic.Int32) *fakeGateway {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &fakeGateway{Server: srv, label: label, hits: hits}
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

func TestGroup_RoutesByNPCFilter(t *testing.T) {
	var xiamiHits, abigailHits atomic.Int32
	xiami := newFakeGateway(t, "xiami", &xiamiHits)
	abigail := newFakeGateway(t, "abigail", &abigailHits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: xiami.URL, Conversation: "xiami", Model: "x", NPCName: "XiaMi"},
		{URL: abigail.URL, Conversation: "abigail", Model: "a", NPCName: "Abigail"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// chat_message addressed to Abigail → only abigail gateway hit.
	chatToAbigail := json.RawMessage(`{"npc":"Abigail","text":"hi","source":"player"}`)
	g.HandleEvent(context.Background(), "chat_message", chatToAbigail)

	waitFor(t, func() bool { return abigailHits.Load() == 1 }, 2*time.Second, "abigail receive")
	if xiamiHits.Load() != 0 {
		t.Errorf("xiami should not have been hit, got %d", xiamiHits.Load())
	}
}

func TestGroup_DropsUnknownNPC(t *testing.T) {
	var hits atomic.Int32
	gw := newFakeGateway(t, "only", &hits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: gw.URL, Conversation: "x", Model: "x", NPCName: "XiaMi"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	g.HandleEvent(context.Background(), "chat_message",
		json.RawMessage(`{"npc":"Penny","text":"hi","source":"player"}`))

	// Give the goroutine a fair window to NOT fire.
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("unknown NPC should be dropped, got %d hits", hits.Load())
	}
}

func TestGroup_BroadcastEventReachesAll(t *testing.T) {
	var xiamiHits, abigailHits atomic.Int32
	xiami := newFakeGateway(t, "xiami", &xiamiHits)
	abigail := newFakeGateway(t, "abigail", &abigailHits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: xiami.URL, Conversation: "xiami", Model: "x", NPCName: "XiaMi"},
		{URL: abigail.URL, Conversation: "abigail", Model: "a", NPCName: "Abigail"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// day_started has no recipient → all relays receive it.
	g.HandleEvent(context.Background(), "day_started", json.RawMessage(`{"day":1}`))

	waitFor(t, func() bool { return xiamiHits.Load() == 1 && abigailHits.Load() == 1 },
		2*time.Second, "broadcast to both")
}

func TestNewGroup_EmptyConfigs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := NewGroup(nil, logger)
	if err == nil {
		t.Error("want error on nil configs, got nil")
	}
}
```

- [ ] **Step 2: Run — expect FAIL ("NewGroup undefined")**

```powershell
cd smartnpc-mcp; go test ./internal/hermesrelay/ -run TestGroup -v
```

- [ ] **Step 3: Implement** `smartnpc-mcp/internal/hermesrelay/group.go`:

```go
package hermesrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/OmniStormX/SmartNPC/internal/events"
)

// Group fans an event out to every Relay whose NPC filter matches. Use this
// when smartnpc-mcp is wired against multiple Hermes profiles (multi-NPC
// runtime). A Group with a single Relay degenerates to the single-target
// behavior of the legacy --hermes-url path.
type Group struct {
	relays []*Relay
	logger *slog.Logger
}

// NewGroup constructs one Relay per Config and wraps them in a Group. Returns
// an error when configs is empty or any constituent Relay fails to construct.
func NewGroup(configs []Config, logger *slog.Logger) (*Group, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("hermesrelay: NewGroup requires at least one config")
	}
	if logger == nil {
		logger = slog.Default()
	}
	relays := make([]*Relay, 0, len(configs))
	for i, cfg := range configs {
		r, err := New(cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("hermesrelay: profile %d: %w", i, err)
		}
		relays = append(relays, r)
	}
	return &Group{relays: relays, logger: logger}, nil
}

// HandleEvent implements bridge.EventHandler. It asks each Relay whether the
// event matches its filter (via Relay.ShouldRoute) and dispatches to all that
// match. When no relay matches, the event is dropped with a debug log so an
// unrouteable NPC name surfaces during diagnosis.
func (g *Group) HandleEvent(ctx context.Context, name string, data json.RawMessage) {
	matched := 0
	for _, r := range g.relays {
		if !r.ShouldRoute(name, data) {
			continue
		}
		r.HandleEvent(ctx, name, data)
		matched++
	}
	if matched == 0 {
		recipient, _, _ := events.RecipientNPC(name, data)
		g.logger.Debug("hermesrelay group: no profile matched event, dropping",
			"event", name, "recipient", recipient)
	}
}

// Relays exposes the underlying slice for diagnostics (e.g. main.go logging).
// Callers MUST NOT mutate the slice.
func (g *Group) Relays() []*Relay {
	return g.relays
}
```

- [ ] **Step 4: Run tests — expect PASS**

```powershell
cd smartnpc-mcp; go test ./internal/hermesrelay/ -v
```

- [ ] **Step 5: Commit**

```bash
git add smartnpc-mcp/internal/hermesrelay/group.go smartnpc-mcp/internal/hermesrelay/group_test.go
git commit -m "feat(mcp): hermesrelay Group fans out to multiple profiles"
```

---

### Task 4: Wire `--hermes-config` flag into mcp main.go

**Files:**
- Modify: `smartnpc-mcp/cmd/smartnpc-mcp/main.go`

The existing `makeRouter` takes a `*hermesrelay.Relay`. We replace this with a `bridge.EventHandler` interface (or just `*hermesrelay.Group` directly), because both single-relay and group satisfy the same `HandleEvent` signature. Easiest: have `Group` continue to exist; pass a `bridge.EventHandler` through.

- [ ] **Step 1: Inspect `makeRouter` signature**

```bash
grep -n "makeRouter\|EventHandler" smartnpc-mcp/cmd/smartnpc-mcp/main.go smartnpc-mcp/internal/bridge/*.go
```

Locate the exact parameter type of the `relay` argument and the `bridge.EventHandler` interface definition. Confirm that `*Relay.HandleEvent(ctx, name, data)` and `*Group.HandleEvent(ctx, name, data)` both satisfy it.

- [ ] **Step 2: Add the flag**

In `smartnpc-mcp/cmd/smartnpc-mcp/main.go`, after the `hermesPersonaFile` flag (around line 73), add:

```go
		hermesConfig = flag.String("hermes-config", "",
			"path to a YAML file with a `profiles:` array (one entry per NPC) "+
				"for multi-profile fan-out. When set, takes precedence over "+
				"--hermes-url / --hermes-npc / --hermes-conversation / --hermes-model.")
```

- [ ] **Step 3: Build the relay handler with precedence**

Replace the current block (around line 104-126):

```go
	var relay *hermesrelay.Relay
	if *hermesURL != "" {
		var err error
		relay, err = hermesrelay.New(hermesrelay.Config{ ... }, logger)
		...
	}
```

with:

```go
	// Build the hermes event handler. Precedence:
	//   1. --hermes-config (multi-profile) — preferred for production
	//   2. --hermes-url + sibling flags     — legacy single-target
	//   3. neither                           — relay disabled
	var hermesHandler bridge.EventHandler
	switch {
	case *hermesConfig != "":
		cfgs, err := hermesrelay.LoadConfigFile(*hermesConfig)
		if err != nil {
			logger.Error("hermes-config load failed", "path", *hermesConfig, "err", err)
			os.Exit(1)
		}
		group, err := hermesrelay.NewGroup(cfgs, logger)
		if err != nil {
			logger.Error("hermesrelay group init failed", "err", err)
			os.Exit(1)
		}
		hermesHandler = group
		logger.Info("hermes relay enabled (multi-profile)",
			"config", *hermesConfig, "profiles", len(group.Relays()))
	case *hermesURL != "":
		single, err := hermesrelay.New(hermesrelay.Config{
			URL:          *hermesURL,
			APIKey:       *hermesAPIKey,
			Conversation: *hermesConversation,
			Model:        *hermesModel,
			NPCName:      *hermesNPC,
			PersonaFile:  *hermesPersonaFile,
		}, logger)
		if err != nil {
			logger.Error("hermesrelay init failed", "err", err)
			os.Exit(1)
		}
		hermesHandler = single
		logger.Info("hermes relay enabled (single-profile, legacy flags)",
			"url", *hermesURL, "conversation", *hermesConversation,
			"npc_filter", *hermesNPC)
	default:
		hermesHandler = nil
	}
```

- [ ] **Step 4: Update `makeRouter` call**

The current call (around line 135) passes `relay`. Change to `hermesHandler`. Update `makeRouter`'s signature (in the same file, search downward) to accept `bridge.EventHandler` instead of `*hermesrelay.Relay`. The internals of `makeRouter` only call `HandleEvent` on this value, so the interface swap is mechanical.

If `makeRouter` checks `relay != nil` — `bridge.EventHandler` is an interface, so the nil check still works.

- [ ] **Step 5: Build**

```powershell
cd smartnpc-mcp; go build ./...
```
Expected: success.

- [ ] **Step 6: Run mcp unit + existing pipeline tests**

```powershell
& "C:\Users\synchen\go\bin\task.exe" mcp:test
```
Expected: PASS. Existing single-target tests still work because the legacy flag path is preserved.

- [ ] **Step 7: Commit**

```bash
git add smartnpc-mcp/cmd/smartnpc-mcp/main.go
git commit -m "feat(mcp): --hermes-config flag for multi-profile fan-out"
```

---

### Task 5: Pipeline integration test for fan-out

**Files:**
- Modify: `smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go`

- [ ] **Step 1: Inspect existing pipeline_test.go**

```bash
head -80 smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go
```
Note the test scaffolding: how it builds the fake Hermes server, how it injects an event. Reuse the patterns.

- [ ] **Step 2: Add new test `TestPipeline_HermesConfigMultiProfile`**

Append to `smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go`:

```go
func TestPipeline_HermesConfigMultiProfile(t *testing.T) {
	// Two fake Hermes gateways (one for XiaMi, one for Abigail). Each
	// counts the requests it receives. We then synthesize a chat_message
	// event for Abigail and assert only her gateway is hit.

	var xiamiHits, abigailHits atomic.Int32

	makeGW := func(hits *atomic.Int32) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
		})
		return httptest.NewServer(mux)
	}
	xiamiGW := makeGW(&xiamiHits)
	defer xiamiGW.Close()
	abigailGW := makeGW(&abigailHits)
	defer abigailGW.Close()

	// Write a runtime-config.yaml fixture pointing at both gateways.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "runtime-config.yaml")
	yamlBody := fmt.Sprintf(`profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: %s
    conversation: xiami
    model: hermes-agent
  - name: abigail
    npc_filter: Abigail
    gateway_url: %s
    conversation: abigail
    model: hermes-agent
`, xiamiGW.URL, abigailGW.URL)
	if err := os.WriteFile(cfgPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgs, err := hermesrelay.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	group, err := hermesrelay.NewGroup(cfgs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// Synthesize a chat_message for Abigail and dispatch via the group.
	group.HandleEvent(context.Background(), "chat_message",
		json.RawMessage(`{"npc":"Abigail","text":"hi","source":"player"}`))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if abigailHits.Load() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := abigailHits.Load(); got != 1 {
		t.Errorf("abigail gateway hits = %d, want 1", got)
	}
	if got := xiamiHits.Load(); got != 0 {
		t.Errorf("xiami gateway hits = %d, want 0 (cross-pollination!)", got)
	}
}
```

Add necessary imports at the top of the file if missing:

```go
import (
	// ... existing
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/OmniStormX/SmartNPC/pkg/relay/hermes"
)
```

- [ ] **Step 3: Run the new test**

```powershell
cd smartnpc-mcp; go test ./cmd/smartnpc-mcp/ -run TestPipeline_HermesConfigMultiProfile -v
```
Expected: PASS.

- [ ] **Step 4: Run full mcp test suite to catch any regression**

```powershell
& "C:\Users\synchen\go\bin\task.exe" mcp:test
```
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go
git commit -m "test(mcp): pipeline integration test for hermes-config fan-out"
```

---

## Phase B — Repo config + scripts

### Task 6: `hermes/runtime-config.yaml`

**Files:**
- Create: `hermes/runtime-config.yaml`

- [ ] **Step 1: Detect WSL→Windows gateway IP**

```powershell
wsl -d Ubuntu-22.04 bash -c "ip route | awk '/default/ { print \$3; exit }'"
```
Record the value. Typical: `192.168.59.118`. Call this `<HOST_IP>` below.

Reverse direction (Windows → WSL gateway listening on `0.0.0.0`) uses the same `<HOST_IP>` because Hermes Gateway binds `0.0.0.0` and mcp resolves via that IP.

- [ ] **Step 2: Write the file**

Create `hermes/runtime-config.yaml` with the literal content:

```yaml
# hermes/runtime-config.yaml
#
# Multi-profile fan-out config consumed by smartnpc-mcp --hermes-config.
# Each entry routes events whose `npc` field matches `npc_filter` to the
# named Hermes Gateway. NPC names are case-sensitive PascalCase (game
# internal names: XiaMi, Abigail, Haley, Harvey, Penny, Sebastian).
#
# api_key_env: name of an environment variable mcp reads at startup. The
# resolved value goes into Authorization: Bearer for outbound POSTs. A
# missing env var leaves the header empty (Hermes Gateway will reject if
# the profile is configured with API_SERVER_KEY).
#
# When changing gateway_url, also update the matching profile's
# API_SERVER_PORT in hermes/profiles/<name>/config-overlay.yaml.

profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://<HOST_IP>:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY

  - name: abigail
    npc_filter: Abigail
    gateway_url: http://<HOST_IP>:8643
    conversation: abigail
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY

  - name: haley
    npc_filter: Haley
    gateway_url: http://<HOST_IP>:8644
    conversation: haley
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY

  - name: harvey
    npc_filter: Harvey
    gateway_url: http://<HOST_IP>:8645
    conversation: harvey
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY

  - name: penny
    npc_filter: Penny
    gateway_url: http://<HOST_IP>:8646
    conversation: penny
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY

  - name: sebastian
    npc_filter: Sebastian
    gateway_url: http://<HOST_IP>:8647
    conversation: sebastian
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
```

Substitute the literal `<HOST_IP>` placeholder with the value from Step 1 (e.g. `192.168.59.118`). **Do not leave the placeholder in the committed file.**

- [ ] **Step 3: Sanity-check it parses by piping through Python yaml**

```powershell
wsl -d Ubuntu-22.04 bash -c "python3 -c 'import yaml,sys; print(yaml.safe_load(open(\"/mnt/d/SmartNPC/hermes/runtime-config.yaml\")))'"
```
Expected: dict containing `profiles` list of 6 entries.

- [ ] **Step 4: Commit**

```bash
git add hermes/runtime-config.yaml
git commit -m "feat(hermes): runtime-config.yaml for multi-profile fan-out"
```

---

### Task 7: `scripts/start_hermes_profiles.sh`

**Files:**
- Create: `scripts/start_hermes_profiles.sh`

- [ ] **Step 1: Write the script**

Create with the literal content:

```bash
#!/usr/bin/env bash
# Start one or more Hermes Gateway processes in the background and wait for
# each /health endpoint to come up.
#
# Usage:   bash scripts/start_hermes_profiles.sh xiami,abigail
# Env:     HERMES_BOOT_TIMEOUT (seconds per profile, default 90)
#          HERMES_PIDFILE (default /tmp/smartnpc-hermes-pids.txt)
#
# Port map (must match hermes/profiles/<name>/config-overlay.yaml):
#   xiami=8642 abigail=8643 haley=8644 harvey=8645 penny=8646 sebastian=8647
#
# Exit codes:
#   0  every requested profile is healthy
#   1  one or more profiles failed to come up before HERMES_BOOT_TIMEOUT

set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: $0 profile1[,profile2,...]" >&2
    exit 2
fi

declare -A PORT_OF=(
    [xiami]=8642
    [abigail]=8643
    [haley]=8644
    [harvey]=8645
    [penny]=8646
    [sebastian]=8647
)

TIMEOUT="${HERMES_BOOT_TIMEOUT:-90}"
PIDFILE="${HERMES_PIDFILE:-/tmp/smartnpc-hermes-pids.txt}"
: > "$PIDFILE"

IFS=',' read -ra PROFILES <<<"$1"
failed=()

for profile in "${PROFILES[@]}"; do
    port="${PORT_OF[$profile]:-}"
    if [[ -z "$port" ]]; then
        echo "[start_hermes_profiles] unknown profile: $profile" >&2
        failed+=("$profile")
        continue
    fi

    # Stop any old process bound to this port (best-effort).
    if pid=$(lsof -ti :"$port" 2>/dev/null); then
        echo "[start_hermes_profiles] killing stale pid $pid on :$port"
        kill -9 "$pid" 2>/dev/null || true
        sleep 0.5
    fi

    echo "[start_hermes_profiles] starting $profile on :$port"
    # Detach into background. Logs go to ~/.hermes/profiles/<name>/logs/
    # (hermes handles that itself); stderr is discarded here.
    nohup hermes -p "$profile" gateway run --accept-hooks \
        > "/tmp/hermes-${profile}.log" 2>&1 &
    pid=$!
    echo "$profile $pid $port" >> "$PIDFILE"

    # Wait for health.
    deadline=$(( $(date +%s) + TIMEOUT ))
    healthy=0
    while (( $(date +%s) < deadline )); do
        if curl -sS "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
            healthy=1
            break
        fi
        sleep 1
    done

    if (( healthy == 0 )); then
        echo "[start_hermes_profiles] $profile failed to become healthy" >&2
        failed+=("$profile")
    else
        echo "[start_hermes_profiles] $profile healthy on :$port (pid $pid)"
    fi
done

if (( ${#failed[@]} > 0 )); then
    echo "[start_hermes_profiles] FAILED profiles: ${failed[*]}" >&2
    exit 1
fi

echo "[start_hermes_profiles] all healthy. PIDs recorded in $PIDFILE"
```

- [ ] **Step 2: Make it executable**

```powershell
wsl -d Ubuntu-22.04 chmod +x /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh
```

- [ ] **Step 3: Smoke-test against a non-existent profile (should fail cleanly)**

```powershell
wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh nonexistent
```
Expected: exit 1, message `unknown profile: nonexistent` on stderr.

- [ ] **Step 4: Commit**

```bash
git add scripts/start_hermes_profiles.sh
git commit -m "feat(scripts): start_hermes_profiles.sh — WSL gateway launcher with health check"
```

---

### Task 8: Rewrite `run.bat`

**Files:**
- Modify: `run.bat`

- [ ] **Step 1: Replace contents**

Overwrite `run.bat` with the literal content:

```bat
@echo off
setlocal
title SmartNPC Launcher (Hermes-first)
cd /d D:\SmartNPC

echo ============================================
echo   SmartNPC - Hermes-first One-Click Launcher
echo ============================================
echo.

rem ---- Step 1: Build ----
echo [1/6] Building mod + mcp ...
call C:\Users\synchen\go\bin\task.exe mod:build
if errorlevel 1 goto build_fail
call C:\Users\synchen\go\bin\task.exe mcp:build
if errorlevel 1 goto build_fail
echo [OK] Build complete.
echo.

rem ---- Step 2: Kill old processes ----
echo [2/6] Killing existing game / mcp / agent processes (if any)...
powershell -NoProfile -Command "Get-Process -Name 'Stardew Valley','StardewModdingAPI' -ErrorAction SilentlyContinue | Stop-Process -Force"
powershell -NoProfile -Command "Get-Process -Name 'smartnpc-mcp','smartnpc-agent' -ErrorAction SilentlyContinue | Stop-Process -Force"
timeout /t 1 /nobreak >nul
echo [OK] Old processes cleared.
echo.

rem ---- Step 3: Install mod + sync hermes profiles + ensure auxiliary model ----
echo [3/6] Installing mod, syncing Hermes profiles, ensuring auxiliary model...
call C:\Users\synchen\go\bin\task.exe mod:install
if errorlevel 1 goto install_fail
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/hermes/install.sh"
if errorlevel 1 goto install_fail
wsl -d Ubuntu-22.04 bash -lc "bash /mnt/d/SmartNPC/scripts/ensure_hermes_aux.sh"
echo [OK] Mod installed, Hermes profiles synced, session_search routed to gpt-4o-mini.
echo.

rem ---- Step 4: Start Hermes Gateways for the active profiles ----
rem Default active set: xiami + abigail. To add others (haley/harvey/penny/sebastian),
rem append to ACTIVE_PROFILES below. Each additional gateway adds ~300-500MB RAM
rem and 30-60s startup; balance accordingly.
echo [4/6] Starting Hermes Gateways (xiami + abigail)...
set ACTIVE_PROFILES=xiami,abigail
start "Hermes Gateways" wsl -d Ubuntu-22.04 bash -ic "bash /mnt/d/SmartNPC/scripts/start_hermes_profiles.sh %ACTIVE_PROFILES%"
echo      Waiting for both gateways to become healthy (up to 90s each)...

:wait_xiami
timeout /t 5 /nobreak >nul
curl -sS http://192.168.59.118:8642/health >nul 2>&1
if errorlevel 1 (
echo      ... waiting for xiami
goto wait_xiami
)
:wait_abigail
curl -sS http://192.168.59.118:8643/health >nul 2>&1
if errorlevel 1 (
timeout /t 5 /nobreak >nul
echo      ... waiting for abigail
goto wait_abigail
)
echo [OK] Both gateways healthy.
echo.

rem ---- Step 5: Start mcp in --http mode with multi-profile fan-out ----
echo [5/6] Starting smartnpc-mcp (--http :3000, --hermes-config multi-profile)...
rem SMARTNPC_HERMES_KEY must match API_SERVER_KEY in each profile's config-overlay.yaml.
rem Default 'smartnpc-test-key' matches the shipped overlays.
if not defined SMARTNPC_HERMES_KEY set SMARTNPC_HERMES_KEY=smartnpc-test-key
start "smartnpc-mcp" cmd /k smartnpc-mcp\bin\smartnpc-mcp.exe ^
    --http :3000 ^
    --ws-url ws://127.0.0.1:18745/ws ^
    --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
    --hermes-api-key %SMARTNPC_HERMES_KEY% ^
    --log-level debug
timeout /t 3 /nobreak >nul
echo [OK] mcp launched.
echo.

rem ---- Step 6: Launch the game ----
echo [6/6] Launching Stardew Valley via SMAPI...
start "" "D:\Stardew Valley\StardewModdingAPI.exe"
echo [OK] Game launching. Load a save file.
echo.
echo ===========================
echo   Active NPCs: %ACTIVE_PROFILES%
echo   To enable haley/harvey/penny/sebastian, edit ACTIVE_PROFILES above.
echo   Group chat: M6 (not orchestrated yet — UI works but no NPC replies).
echo ===========================
goto :eof

:build_fail
echo [ERROR] Build failed.
pause
exit /b 1

:install_fail
echo [ERROR] Mod or Hermes install failed.
pause
exit /b 1
```

**Note:** the `--hermes-api-key` flag is **still passed** as a fallback for any legacy single-target case. The yaml's `api_key_env: SMARTNPC_HERMES_KEY` is the primary source. The duplicate is harmless — mcp uses the yaml value when `--hermes-config` is set.

- [ ] **Step 2: Smoke check the syntax (cmd /c)**

```powershell
cmd /c "run.bat" 2>&1 | Select-String -SimpleMatch "ERROR","======" | Select-Object -First 5
```

Don't actually wait for the game — kill the launcher window once you see the header banner. The point is to confirm no syntax errors on the first 10 lines.

- [ ] **Step 3: Commit**

```bash
git add run.bat
git commit -m "feat(run): rewrite run.bat for Hermes-first runtime"
```

---

## Phase C — Hermes profiles (5 NPCs)

### Task 9: Shared `inter-npc-message` skill

**Files:**
- Create: `hermes/profiles/xiami/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md`

This file is authored once and copied verbatim into each of the 6 profiles' `skills/smartnpc/` directory in later tasks.

- [ ] **Step 1: Write the skill**

Create `hermes/profiles/xiami/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md`:

```markdown
---
name: smartnpc-inter-npc-message
description: When the player asks about or asks you to involve another NPC, send them a message instead of fabricating their response. When you receive a message from another NPC, react in character and reply back.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, Stardew-Valley, inter-npc, delegation]
---

# Inter-NPC messaging policy

You can talk to other NPCs through the `npc_send_message` MCP tool. The
tool puts a message into the recipient's mailbox AND fires an event that
the recipient's profile sees. Use this any time the player's request
involves another NPC — never fabricate another NPC's voice or pretend you
did something on their behalf.

## When YOU are the asker

| Player intent | What to do |
|---|---|
| Player asks **about** another NPC's thoughts, plans, schedule, feelings, opinions | Call `npc_send_message(to=<NPC>, kind="query", text="<玩家的问题>", reply_expected=true)`. Then keep talking in character; their reply will arrive on your next inbound event. |
| Player asks you to **make another NPC do something** (come over, go somewhere, deliver a message, perform a task) | Call `npc_send_message(to=<NPC>, kind="behavioral", text="<玩家想请你的事>", reply_expected=true)`. Their own agent will execute the action. |
| Player asks for both info AND action | One `npc_send_message` with `kind="behavioral"` and the action phrased as the `text`. |

Trigger phrases (Chinese / English): "帮我问 / 去问问 / ask X about Y",
"叫 / 让 / 请 X 过来 / 过去 / 做 ...", "告诉 X ...", "把 X 喊来",
"have X come / tell X to ...".

**Forbidden:**

- Generating another NPC's dialogue yourself. Always go through
  `npc_send_message`.
- Pretending you performed an action that belongs to another NPC.
- Repeating the same `npc_send_message` more than once per player turn
  for the same recipient + intent.

After sending, your **own** reply can paraphrase ("I'll let Penny
know") — but keep it short and non-committal until you actually hear
back.

## When YOU are the receiver

When an `event_npc_message` notification arrives (you can also poll
`npc_inbox_get`), inspect the `kind` field:

| kind | Behavior |
|---|---|
| `query` | A peer is asking you a question. Answer it briefly in character via `chat_say`. Then call `npc_send_message(to=<from>, kind="reply", text=<your answer>)` so the asker gets a structured copy of your answer. |
| `behavioral` | A peer is asking you to do something. Read `text`, decide which game tool fits (`npc_move_to`, `npc_summon`, `npc_face_direction`, `mail_send`, ...), call it, then `chat_say` a short in-character confirmation. Reply via `npc_send_message(kind="reply", text="<confirmation>")` so the asker can paraphrase. |
| `reply` | A peer is answering your earlier `query` or confirming your `behavioral` request. Fold the contents into your **next** reply to the player (e.g. "Penny says she's on her way"). Do NOT send a counter-reply. |

## Concrete examples

### Example A — query

> Player → XiaMi: "潘妮今天打算去哪里？"

XiaMi calls:
```
npc_send_message(to="Penny", kind="query",
                 text="玩家想知道你今天打算去哪里",
                 reply_expected=true)
```
XiaMi's own `chat_say`: "等等，我帮你问问她。"

Penny receives the query, replies via `chat_say`: "图书馆吧，下午有
读书会。" + sends `npc_send_message(to="XiaMi", kind="reply",
text="图书馆下午读书会")`.

XiaMi on next turn: "潘妮说下午要去图书馆。"

### Example B — behavioral

> Player → XiaMi: "让阿比盖尔到我这儿来"

XiaMi calls:
```
npc_send_message(to="Abigail", kind="behavioral",
                 text="玩家想请你过去找他",
                 reply_expected=true)
```
XiaMi's own `chat_say`: "好啊，我喊她。"

Abigail receives, calls `npc_summon(npc="Abigail")`, then
`chat_say`: "嘿，我这就过来。" + replies
`npc_send_message(to="XiaMi", kind="reply", text="OK on my way")`.

## Failure modes

- `npc_send_message` returns an error → say something neutral in
  character ("我喊了，但她大概没听见"). Do NOT mention the error code.
- Recipient never replies (no `reply` event arrives) → mention it in
  passing on a later turn ("她那边好像没回音"). Do NOT chase by re-
  sending — the mailbox is store-and-forward; she'll see it eventually.

## See also

- `smartnpc-game-tool-policy` — overall tool-use rules
- `npc_send_message` tool description in mcp-tools.md
```

- [ ] **Step 2: Verify UTF-8 no BOM**

```powershell
python -c "open(r'hermes/profiles/xiami/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md','rb').read().decode('utf-8')"
```
Expected: no exception. Then check first 3 bytes are not `\xef\xbb\xbf` (BOM):

```powershell
python -c "f=open(r'hermes/profiles/xiami/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md','rb'); h=f.read(3); print('BOM' if h==b'\xef\xbb\xbf' else 'no-BOM')"
```
Expected: `no-BOM`.

- [ ] **Step 3: Commit**

```bash
git add hermes/profiles/xiami/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md
git commit -m "feat(hermes): inter-npc-message skill replaces consult_npc logic"
```

---

### Task 10: Cross-reference in `game-tool-policy`

**Files:**
- Modify: `hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md`

- [ ] **Step 1: Locate the place to insert**

```bash
grep -n "## " hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md
```
Look for a `## See also` section or the end of the file.

- [ ] **Step 2: Append a "Delegation" section**

Append before any `## See also` (or at end if none):

```markdown
## Delegation (inter-NPC requests)

When the player's request involves another NPC — asking about them, asking
you to get them to do something, asking you to relay a message — do **not**
fabricate the other NPC's voice or pretend to act for them. Use the
`npc_send_message` MCP tool instead.

Full rules and examples: see the `smartnpc-inter-npc-message` skill.
```

- [ ] **Step 3: Commit**

```bash
git add hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md
git commit -m "docs(hermes): xiami game-tool-policy points at inter-npc-message"
```

---

### Tasks 11–15: Create each of the 5 new NPC profiles

Each task follows the same pattern. Steps 1–4 are bite-sized; step 2 (SOUL.md authoring) is creative writing — but the editorial brief below makes the structure rigid.

**Editorial brief** (applies to all 5 SOUL.md tasks):

1. **Source material:** `smartnpc-agent/personas/<name>.json`. Fields used:
   - `speaker` → English NPC name (used in PascalCase below)
   - `name` → Chinese display name (use in title)
   - `personality` → translate / adapt into `## 性格` section
   - `speaking_style` → `## 说话风格` section (keep as-is if Chinese; translate if English)
   - `background` → `## 人设背景（内心持有，不主动说）` section
   - `soul_notes` → `## 灵魂层次` section (bullet list, 5-7 items)
   - `friendship_behaviors` → `## 好感度对应语气` markdown table (4 rows: 0-2, 3-5, 6-8, 9-10)

2. **Reference template:** `hermes/profiles/xiami/SOUL.md`. Match section order **exactly**:
   1. `# <Chinese name> (<English speaker>) — 星露谷物语 NPC`
   2. `## 身份` — one paragraph (≤4 sentences)
   3. `## 性格` — 3-5 sentences
   4. `## 说话风格` — bullet list (5-7 bullets)
   5. `## 灵魂层次` — bullet list (4-6 bullets, one per soul_notes item)
   6. `## 好感度对应语气`（`friendship_get` hearts）— 4-row markdown table (cols: Hearts, 语气, 参考招呼)
   7. `## 绝对禁止` — bullet list (7 bullets, **identical** to xiami's, possibly with NPC-specific extras)
   8. `## 工具使用原则` — same 5-item ordered list as xiami's, **identical text**
   9. `## 人设背景（内心持有，不主动说）` — one paragraph from the JSON `background`

3. **Voice / quality rules:**
   - Chinese throughout the body. Technical terms (chat_say, npc_*) stay English in backticks.
   - 1-3 sentence cadence preferred in tone examples — never long prose.
   - No markdown table styling beyond what xiami uses.
   - UTF-8 **no BOM**.
   - Each section under 200 words (xiami's SOUL.md is ~500 words total — aim for similar).

4. **Forbidden:**
   - Do not invent lore that contradicts the source JSON (e.g. don't change Penny's family setup).
   - Do not omit the `工具使用原则` block — it's identical across profiles and gives the LLM its rule-of-thumb anchor.
   - Do not include any `[Delegation rule]`-style imperative — that lives in the `inter-npc-message` skill, not SOUL.md.

---

### Task 11: Abigail profile

**Files:**
- Create: `hermes/profiles/abigail/SOUL.md`
- Create: `hermes/profiles/abigail/config-overlay.yaml`
- Create: `hermes/profiles/abigail/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md` (copy from xiami)
- Create: `hermes/profiles/abigail/skills/smartnpc/smartnpc-proactive-greeting/SKILL.md` (copy from xiami)
- Create: `hermes/profiles/abigail/skills/smartnpc/smartnpc-memory-policy/SKILL.md` (copy from xiami)
- Create: `hermes/profiles/abigail/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md` (copy from xiami)

- [ ] **Step 1: Read source and template**

```bash
cat smartnpc-agent/personas/abigail.json
cat hermes/profiles/xiami/SOUL.md
```

- [ ] **Step 2: Write `hermes/profiles/abigail/SOUL.md`**

Apply the editorial brief above. Map fields from `abigail.json` per the table; preserve all 9 sections in order; use the friendship_behaviors entries to fill the 4-row table.

- [ ] **Step 3: Create `hermes/profiles/abigail/config-overlay.yaml`**

Verbatim content:

```yaml
# Config overlay for hermes/profiles/abigail/config.yaml — see xiami for the
# full template. Port 8643 is reserved for Abigail per
# hermes/runtime-config.yaml.

mcp_servers:
  smartnpc_game:
    url: http://__HOST_IP__:3000/mcp
    timeout: 30
    connect_timeout: 10
    tools:
      exclude: []
      resources: false
      prompts: false

API_SERVER_ENABLED: true
API_SERVER_KEY: smartnpc-test-key
API_SERVER_HOST: 0.0.0.0
API_SERVER_PORT: 8643
API_SERVER_MODEL_NAME: abigail
```

- [ ] **Step 4: Copy the 4 skills from xiami**

```powershell
mkdir hermes\profiles\abigail\skills\smartnpc 2>$null
Copy-Item -Recurse -Force hermes\profiles\xiami\skills\smartnpc\* hermes\profiles\abigail\skills\smartnpc\
```

- [ ] **Step 5: Verify UTF-8 no BOM on SOUL.md**

```powershell
python -c "f=open(r'hermes/profiles/abigail/SOUL.md','rb'); h=f.read(3); print('BOM' if h==b'\xef\xbb\xbf' else 'no-BOM')"
```
Expected: `no-BOM`.

- [ ] **Step 6: Run install.sh — confirm Abigail profile is detected**

```powershell
wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh
```
Expected output: `synced profiles: ... abigail ...`.

- [ ] **Step 7: Commit**

```bash
git add hermes/profiles/abigail/
git commit -m "feat(hermes): Abigail profile (SOUL + overlay + skills, port 8643)"
```

---

### Task 12: Haley profile

Same pattern as Task 11, with source `smartnpc-agent/personas/haley.json`, port `8644`, `API_SERVER_MODEL_NAME: haley`.

- [ ] **Step 1:** `cat smartnpc-agent/personas/haley.json; cat hermes/profiles/xiami/SOUL.md`
- [ ] **Step 2:** Write `hermes/profiles/haley/SOUL.md` per editorial brief.
- [ ] **Step 3:** Create `hermes/profiles/haley/config-overlay.yaml` (copy Abigail's, change port 8643→8644 and `MODEL_NAME` abigail→haley).
- [ ] **Step 4:** Copy skills: `Copy-Item -Recurse -Force hermes\profiles\xiami\skills\smartnpc\* hermes\profiles\haley\skills\smartnpc\`
- [ ] **Step 5:** Verify no BOM.
- [ ] **Step 6:** `wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh` — expect `haley` in synced list.
- [ ] **Step 7:** `git add hermes/profiles/haley/ && git commit -m "feat(hermes): Haley profile (port 8644)"`

---

### Task 13: Harvey profile

Same pattern, source `harvey.json`, port `8645`, name `harvey`.

- [ ] **Step 1:** `cat smartnpc-agent/personas/harvey.json; cat hermes/profiles/xiami/SOUL.md`
- [ ] **Step 2:** Write `hermes/profiles/harvey/SOUL.md`.
- [ ] **Step 3:** Create `hermes/profiles/harvey/config-overlay.yaml` (port 8645, MODEL_NAME harvey).
- [ ] **Step 4:** Copy skills.
- [ ] **Step 5:** Verify no BOM.
- [ ] **Step 6:** install.sh → expect harvey in list.
- [ ] **Step 7:** `git commit -m "feat(hermes): Harvey profile (port 8645)"`

---

### Task 14: Penny profile

Same pattern, source `penny.json`, port `8646`, name `penny`.

- [ ] **Step 1:** `cat smartnpc-agent/personas/penny.json; cat hermes/profiles/xiami/SOUL.md`
- [ ] **Step 2:** Write `hermes/profiles/penny/SOUL.md`.
- [ ] **Step 3:** Create `hermes/profiles/penny/config-overlay.yaml` (port 8646, MODEL_NAME penny).
- [ ] **Step 4:** Copy skills.
- [ ] **Step 5:** Verify no BOM.
- [ ] **Step 6:** install.sh → expect penny in list.
- [ ] **Step 7:** `git commit -m "feat(hermes): Penny profile (port 8646)"`

---

### Task 15: Sebastian profile

Same pattern, source `sebastian.json`, port `8647`, name `sebastian`.

- [ ] **Step 1:** `cat smartnpc-agent/personas/sebastian.json; cat hermes/profiles/xiami/SOUL.md`
- [ ] **Step 2:** Write `hermes/profiles/sebastian/SOUL.md`.
- [ ] **Step 3:** Create `hermes/profiles/sebastian/config-overlay.yaml` (port 8647, MODEL_NAME sebastian).
- [ ] **Step 4:** Copy skills.
- [ ] **Step 5:** Verify no BOM.
- [ ] **Step 6:** install.sh → expect sebastian in list.
- [ ] **Step 7:** `git commit -m "feat(hermes): Sebastian profile (port 8647)"`

---

## Phase D — Docs + acceptance

### Task 16: Update repo docs

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/hermes-profiles.md`
- Modify: `docs/migration-smartnpc-agent.md`
- Modify: `docs/roadmap.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: `docs/architecture.md`**

Find the existing data-flow / architecture diagram (likely under a `## Runtime` or `## Architecture` heading) and replace the single-Hermes-relay arrow with the multi-profile fan-out diagram from §2 of the spec. Specifically replace any `smartnpc-mcp → Hermes profile (xiami)` block with the 6-target diagram showing `runtime-config.yaml` as the routing source. Add a short paragraph (3-5 sentences) explaining the YAML schema and `api_key_env` indirection.

- [ ] **Step 2: `docs/hermes-profiles.md`**

In the "Multi-NPC checklist" section, replace step 6 (the `(future work: a --hermes-config file for multi-profile fan-out)` line) with:

```markdown
6. Add the new NPC to `hermes/runtime-config.yaml` with a unique
   `gateway_url` port matching the new profile's `config-overlay.yaml`.
   Re-run `install.sh`; the next mcp restart picks up the new profile
   automatically.
```

Also add a new section near the top after the "Anatomy" section:

```markdown
## Multi-profile fan-out: `hermes/runtime-config.yaml`

The single `smartnpc-mcp` process learns which Hermes Gateway to POST to
from `hermes/runtime-config.yaml`. The file maps NPC internal name
(`npc_filter`, PascalCase, case-sensitive) to gateway URL + conversation
+ model + bearer-token env var. See the file itself for the schema.

When you add an NPC, update **three** things together:

1. `hermes/profiles/<name>/config-overlay.yaml` — `API_SERVER_PORT` +
   `API_SERVER_MODEL_NAME`.
2. `hermes/runtime-config.yaml` — append a `profiles:` entry with the
   same port and model.
3. `scripts/start_hermes_profiles.sh` — extend `PORT_OF` map if not
   already there.

Mismatches between (1) and (2) cause silent message loss (events route to
an empty port).
```

- [ ] **Step 3: `docs/migration-smartnpc-agent.md`**

In the "Behavior parity checklist" table, flip these rows:

- Add a column / row noting **5 new NPCs** (Abigail/Haley/Harvey/Penny/Sebastian) now share parity with XiaMi for: chat, time/weather greeting, friendship-tier tone, movement, memory, multi-NPC concurrent reply.
- Group chat row stays ⚠️.
- Add a line about the new `inter-npc-message` skill replacing `consult_npc`.

- [ ] **Step 4: `docs/roadmap.md`**

Update M5 (Hermes-first) line:
- Change status from "✅ 代码就绪，待实机端到端验证" to "✅ 端到端跑通，6 NPC 配置就绪（默认起 xiami + abigail）" once Task 17 manual smoke succeeds.
- Add an M6 section listing the deferred items: group chat orchestration, smartnpc-agent archival, `--hermes-url` legacy flag removal.

- [ ] **Step 5: `CLAUDE.md`**

In the "项目概览" section, the "**正式链路（Hermes-first，M5 起）**" diagram already shows the right shape but mentions only `Hermes Agent Profile` (single). Update to:

```
SMAPI Mod (C# .NET 6) ──ws :18745── smartnpc-mcp (Go, --http :3000) ──MCP HTTP── 6× Hermes Agent Profile
                                          │                              (xiami, abigail, haley, harvey, penny, sebastian)
                                          └── hermesrelay ──POST /v1/responses──> route by runtime-config.yaml
```

In the "常用命令" / "启动 MCP HTTP 模式" section, replace the manual single-target invocation with:

```cmd
cd D:\SmartNPC\smartnpc-mcp
bin\smartnpc-mcp.exe --http :3000 --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
  --log-level debug
```

- [ ] **Step 6: Verify all 5 files saved as UTF-8 no BOM**

```powershell
foreach ($f in @('docs/architecture.md','docs/hermes-profiles.md','docs/migration-smartnpc-agent.md','docs/roadmap.md','CLAUDE.md')) {
  python -c "f=open(r'$f','rb'); print('$f:', 'BOM' if f.read(3)==b'\xef\xbb\xbf' else 'ok')"
}
```
Expected: `ok` for all 5.

- [ ] **Step 7: Commit**

```bash
git add docs/architecture.md docs/hermes-profiles.md docs/migration-smartnpc-agent.md docs/roadmap.md CLAUDE.md
git commit -m "docs: Hermes-first multi-profile fan-out + 6-NPC runtime"
```

---

### Task 17: Final CI + manual smoke

- [ ] **Step 1: Full `task ci`**

```powershell
& "C:\Users\synchen\go\bin\task.exe" ci
```
Expected: lint + all Go tests + all 3 builds green.

- [ ] **Step 2: Manual smoke — launch via run.bat**

```cmd
run.bat
```
Watch the launcher window. Expected sequence:
1. `[1/6] Building` → OK
2. `[2/6] Killing existing` → OK
3. `[3/6] Installing mod, syncing Hermes profiles` → see `synced profiles: abigail haley harvey penny sebastian xiami`
4. `[4/6] Starting Hermes Gateways` → "xiami healthy on :8642", "abigail healthy on :8643"
5. `[5/6] Starting smartnpc-mcp` → new cmd window with mcp logs
6. `[6/6] Launching Stardew Valley` → SMAPI window appears
7. Banner: `Active NPCs: xiami,abigail`

- [ ] **Step 3: In-game E2E checklist**

Load a save, then:

- Talk to XiaMi via ChatPanel → expect chat_say reply within 10s, in-character.
- Talk to Abigail via ChatPanel → expect reply, no cross-pollination with XiaMi.
- Say "让阿比盖尔过来" to XiaMi → expect XiaMi to respond + Abigail to chat_say + actually move (Abigail must be launched for this).
- Open group chat (`/group XiaMi Abigail` in Ctrl+T chat) → UI works, no NPC reply (documented M6 deferral).
- Talk to Penny via ChatPanel → mcp log should show "no profile matched event, dropping" (Penny gateway not launched).

- [ ] **Step 4: Commit any final tweaks discovered during smoke**

Common fixups: `runtime-config.yaml` HOST_IP wrong on this machine, skill content needing a tone adjustment, etc. Bundle into:

```bash
git add -p
git commit -m "fix(hermes): smoke-test tweaks"
```

- [ ] **Step 5: Final all-green check**

```powershell
& "C:\Users\synchen\go\bin\task.exe" ci
git status
```
Expected: ci green, working tree clean.

---

## Out of scope (deferred to M6)

- Hermes-side group chat orchestration
- `smartnpc-agent/` archival to `archive/`
- `--hermes-url / --hermes-npc / --hermes-conversation / --hermes-model` legacy flag removal
- Cron / proactive recipes for the 5 new NPCs (only xiami has them today)
- Per-NPC LLM model selection in `runtime-config.yaml`

---

## Risk recap

| Risk | Surfaces during | Mitigation |
|------|-----------------|-----------|
| HOST_IP wrong in `runtime-config.yaml` | Task 17 smoke (mcp log shows POST errors) | Detect step in Task 6 + visible startup log |
| Hermes Gateway × 2 cold-start > 90s | Task 17 smoke | `HERMES_BOOT_TIMEOUT` env in `start_hermes_profiles.sh` |
| MCP-notification-triggered receiver skill doesn't fire | Task 17 step 3 (Abigail doesn't move on "让阿比盖尔过来") | Fallback: have hermesrelay POST an extra "you have new mail" envelope on `npc_message` enqueue — implement as M5.1 follow-up if needed |
| SOUL.md tone regression vs personas/*.json | Task 17 chat with each NPC | A/B compare with frozen smartnpc-agent (still bootable for reference) |
| Group chat shows player input but no reply | Task 17 step 3 group test | Documented as M6 — UI banner / docs update only |
