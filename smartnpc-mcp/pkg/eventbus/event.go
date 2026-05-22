// Package eventbus defines the core, domain-agnostic event type that flows
// through the agent-bridge.
//
// Adapters (e.g. adapters/stardew) translate their wire-format events into
// these envelopes; relay backends (relay.Backend implementations) consume
// them. The core MUST NOT interpret domain-specific fields like "season"
// or "tile" — those belong inside adapter-defined typed payloads that get
// marshaled into Event.Payload.
//
// This package was extracted from internal/events as PR 1 of the
// agent-bridge refactor. The legacy internal/events package retains the
// SDV-specific typed structs (ChatMessage, NpcInteract, ...) and will
// move under adapters/stardew/events in a later PR.
package eventbus

import (
	"encoding/json"
	"time"
)

// Event is the unit transported between event sources and relay backends.
//
// Field semantics:
//
//   - Kind is a dotted, namespaced verb describing what happened, e.g.
//     "chat.message", "actor.interact", "world.day_started". Adapters
//     SHOULD keep Kind values stable across versions; consumers route on
//     Kind. Lowercase ASCII, segments separated by '.'.
//
//   - Source identifies the producing adapter, e.g. "sdv" for Stardew
//     Valley. An event flowing from the stardew adapter to a Hermes
//     profile carries Source="sdv" so relay backends and downstream
//     skills can tell domains apart when a future bridge runs multiple
//     adapters at once. Lowercase, no dots.
//
//   - Subject is the free-form principal that the event is "about" —
//     typically an NPC name, the player, or a synthetic entity. Empty
//     string is allowed for global events (e.g. world tick).
//
//   - Payload is the adapter-defined typed payload, marshaled to JSON.
//     Core MUST NOT introspect Payload; adapters and downstream consumers
//     re-marshal as needed.
//
//   - Timestamp is the event production time at the adapter (NOT when it
//     arrived at the bridge). Adapters that lack a wall-clock SHOULD set
//     time.Now() on translation. Serialized as RFC 3339.
type Event struct {
	Kind      string          `json:"kind"`
	Source    string          `json:"source"`
	Subject   string          `json:"subject,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// New constructs an Event with Timestamp = time.Now() and a JSON-encoded
// payload. Returns an error iff payload marshaling fails.
//
// Pass nil for payload when there is none; the resulting Event.Payload
// will be nil (omitted from JSON via the omitempty tag).
func New(kind, source, subject string, payload any) (Event, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Event{}, err
		}
		raw = b
	}
	return Event{
		Kind:      kind,
		Source:    source,
		Subject:   subject,
		Payload:   raw,
		Timestamp: time.Now(),
	}, nil
}
