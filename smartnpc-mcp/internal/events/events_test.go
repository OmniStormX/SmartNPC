package events

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

func TestDecodeChatMessage_Roundtrip(t *testing.T) {
	in := ChatMessage{NPC: "XiaMi", Target: "XiaMi", Text: "你好", Source: "player"}
	buf, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := DecodeChatMessage(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != in {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, in)
	}
}

func TestDecodeChatMessage_IgnoresUnknownFields(t *testing.T) {
	raw := []byte(`{"npc":"XiaMi","text":"hi","source":"player","future_field":42}`)
	got, err := DecodeChatMessage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NPC != "XiaMi" || got.Text != "hi" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestFormatForHermes_CoversKnownEvents(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		payload any
		want    string // substring the formatted string must contain
	}{
		{
			name:    "chat_message",
			event:   bridge.EventChatMessage,
			payload: ChatMessage{NPC: "XiaMi", Text: "你好", Source: "player"},
			want:    "Farmer says to you: 你好",
		},
		{
			name:    "chat_received",
			event:   bridge.EventChatReceived,
			payload: ChatReceived{Text: "anyone there?", Source: "player"},
			want:    "anyone there?",
		},
		{
			name:    "npc_interact",
			event:   bridge.EventNpcInteract,
			payload: NpcInteract{NPC: "XiaMi", Source: "player"},
			want:    "walked up to you",
		},
		{
			name:    "day_started",
			event:   bridge.EventDayStarted,
			payload: DayStarted{Day: 5, Season: "spring", Year: 1, DayOfWeek: "Mon"},
			want:    "Spring 5",
		},
		{
			name:    "friendship_changed_positive",
			event:   bridge.EventFriendshipChanged,
			payload: FriendshipChanged{NPC: "XiaMi", PointDelta: 50, Hearts: 3, Trigger: "gift"},
			want:    "improved",
		},
		{
			name:    "friendship_changed_negative",
			event:   bridge.EventFriendshipChanged,
			payload: FriendshipChanged{NPC: "XiaMi", PointDelta: -20, Hearts: 2, Trigger: "decay"},
			want:    "worsened",
		},
		{
			name:    "npc_message",
			event:   bridge.EventNpcMessage,
			payload: NpcMessage{From: "Abigail", To: "XiaMi", Text: "农场出事了"},
			want:    "Abigail says to you",
		},
		{
			name:    "npc_broadcast",
			event:   bridge.EventNpcBroadcast,
			payload: NpcBroadcast{From: "Abigail", Kind: "alarm"},
			want:    "broadcast a alarm event",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := FormatForHermes(tc.event, raw)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("FormatForHermes(%s) = %q; want contains %q",
					tc.event, got, tc.want)
			}
		})
	}
}

func TestFormatForHermes_UnknownEvent(t *testing.T) {
	got := FormatForHermes("something_new", json.RawMessage(`{}`))
	if !strings.Contains(got, "something_new") {
		t.Fatalf("expected fallback to echo event name; got %q", got)
	}
}

func TestFormatForHermes_MalformedPayloadFallsBack(t *testing.T) {
	got := FormatForHermes(bridge.EventChatMessage, json.RawMessage(`not valid json`))
	if !strings.Contains(got, bridge.EventChatMessage) {
		t.Fatalf("expected fallback string for bad payload; got %q", got)
	}
}

func TestIsModAndIsSynthetic(t *testing.T) {
	if !IsMod(bridge.EventChatMessage) {
		t.Errorf("chat_message should be mod-sourced")
	}
	if IsMod(bridge.EventNpcMessage) {
		t.Errorf("npc_message should NOT be mod-sourced")
	}
	if IsMod(bridge.EventDayStarted) {
		t.Errorf("day_started is reserved, should NOT be reported as currently emitted")
	}
	if !IsSynthetic(bridge.EventNpcBroadcast) {
		t.Errorf("npc_broadcast should be synthetic")
	}
	if IsSynthetic(bridge.EventChatReceived) {
		t.Errorf("chat_received should NOT be synthetic")
	}
}

func TestIsReserved(t *testing.T) {
	if !IsReserved(bridge.EventDayStarted) {
		t.Errorf("day_started should be reserved")
	}
	if !IsReserved(bridge.EventFriendshipChanged) {
		t.Errorf("friendship_changed should be reserved")
	}
	if IsReserved(bridge.EventChatMessage) {
		t.Errorf("chat_message is emitted today, not reserved")
	}
	if IsReserved(bridge.EventNpcMessage) {
		t.Errorf("npc_message is synthetic, not reserved-mod")
	}
}

func TestRecipientNPC(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantName string
		wantOK   bool
		wantErr  bool
	}{
		{"npc_field", `{"npc":"XiaMi","text":"hi"}`, "XiaMi", true, false},
		{"to_field", `{"from":"Abigail","to":"XiaMi","text":"hi"}`, "XiaMi", true, false},
		{"target_field", `{"target":"XiaMi","text":"hi"}`, "XiaMi", true, false},
		{"npc_wins_over_target", `{"npc":"XiaMi","target":"Abigail"}`, "XiaMi", true, false},
		{"empty_object", `{}`, "", false, false},
		{"no_recipient_fields", `{"day":5,"season":"spring"}`, "", false, false},
		{"malformed", `not json`, "", false, true},
		{"empty_payload", ``, "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := RecipientNPC("any", json.RawMessage(tc.payload))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.wantName || ok != tc.wantOK {
				t.Fatalf("got (%q, %v); want (%q, %v)", got, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}
