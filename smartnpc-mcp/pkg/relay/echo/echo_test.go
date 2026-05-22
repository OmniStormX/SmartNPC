package echo

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

func TestBackend_Forward_LogsEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	b := &Backend{Logger: logger}

	ev, err := eventbus.New("chat.message", "sdv", "XiaMi", map[string]string{"text": "hi"})
	if err != nil {
		t.Fatalf("eventbus.New: %v", err)
	}
	if err := b.Forward(context.Background(), ev); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	// Single JSON line in the buffer; decode and assert key fields.
	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("decode log line: %v; raw=%q", err, line)
	}
	if rec["kind"] != "chat.message" {
		t.Errorf("kind = %v, want chat.message", rec["kind"])
	}
	if rec["source"] != "sdv" {
		t.Errorf("source = %v, want sdv", rec["source"])
	}
	if rec["subject"] != "XiaMi" {
		t.Errorf("subject = %v, want XiaMi", rec["subject"])
	}
}

func TestBackend_Name(t *testing.T) {
	b := &Backend{}
	if b.Name() != "echo" {
		t.Errorf("Name() = %q, want echo", b.Name())
	}
}
