package eventbus

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestNew_HappyPath(t *testing.T) {
	type chatPayload struct {
		Text string `json:"text"`
	}
	before := time.Now()
	ev, err := New("chat.message", "sdv", "XiaMi", chatPayload{Text: "hello"})
	after := time.Now()
	if err != nil {
		t.Fatalf("New returned err: %v", err)
	}
	if ev.Kind != "chat.message" {
		t.Errorf("Kind = %q, want %q", ev.Kind, "chat.message")
	}
	if ev.Source != "sdv" {
		t.Errorf("Source = %q, want %q", ev.Source, "sdv")
	}
	if ev.Subject != "XiaMi" {
		t.Errorf("Subject = %q, want %q", ev.Subject, "XiaMi")
	}
	if ev.Timestamp.Before(before) || ev.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in [%v, %v]", ev.Timestamp, before, after)
	}
	// Payload round-trip.
	var got chatPayload
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if got.Text != "hello" {
		t.Errorf("payload Text = %q, want %q", got.Text, "hello")
	}
}

func TestNew_NilPayload(t *testing.T) {
	ev, err := New("world.day_started", "sdv", "", nil)
	if err != nil {
		t.Fatalf("New returned err: %v", err)
	}
	if ev.Payload != nil {
		t.Errorf("Payload = %v, want nil for nil input", ev.Payload)
	}
	if ev.Subject != "" {
		t.Errorf("Subject = %q, want empty", ev.Subject)
	}
}

func TestNew_UnmarshalablePayload(t *testing.T) {
	// math.Inf is not representable in JSON; encoding/json returns error.
	_, err := New("debug.bad", "sdv", "", math.Inf(1))
	if err == nil {
		t.Fatal("New should have errored on Inf payload, got nil")
	}
}

func TestEvent_JSONShape(t *testing.T) {
	// Lock down the wire shape: omitempty on Subject and Payload, RFC3339
	// timestamp. Downstream consumers (Hermes profile, relay backends)
	// depend on these field names.
	ev, err := New("actor.interact", "sdv", "Abigail", map[string]string{"src": "player"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"kind":"actor.interact"`, `"source":"sdv"`, `"subject":"Abigail"`, `"payload":{"src":"player"}`, `"timestamp":`} {
		if !strings.Contains(s, want) {
			t.Errorf("marshaled JSON missing %q; got %s", want, s)
		}
	}

	// Empty subject + nil payload are omitted.
	bare, err := New("world.tick", "sdv", "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b2, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s2 := string(b2)
	if strings.Contains(s2, `"subject"`) {
		t.Errorf("empty Subject should be omitted; got %s", s2)
	}
	if strings.Contains(s2, `"payload"`) {
		t.Errorf("nil Payload should be omitted; got %s", s2)
	}
}
