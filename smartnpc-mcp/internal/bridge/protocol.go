// Wire protocol DTOs mirroring smapi-mod/Bridge/Protocol.cs.
// See docs/protocol.md for the spec.
package bridge

import "encoding/json"

// FrameType discriminates the JSON envelope.
const (
	TypeRequest  = "request"
	TypeResponse = "response"
	TypeEvent    = "event"
)

// EventName constants — the canonical set of server-pushed events.
const (
	EventChatReceived       = "chat_received"
	EventNpcPerceptionUpdate = "npc_perception_update" // reserved, not emitted yet
)

// ActionName constants — the canonical set of client-issued requests.
const (
	ActionMailSend          = "mail_send"
	ActionChatSay           = "chat_say"
	ActionGameGetTime       = "game_get_time"
	ActionGameGetWeather    = "game_get_weather"
	ActionFriendshipGet     = "friendship_get"
	ActionNpcGetNearby      = "npc_get_nearby"
	ActionNpcGetEnvironment = "npc_get_environment"
	ActionNpcMoveTo         = "npc_move_to"
	ActionNpcFaceDirection  = "npc_face_direction"
	ActionNpcGetPosition    = "npc_get_position"
	ActionNpcSummon         = "npc_summon"
	ActionNpcFollowStart    = "npc_follow_start"
	ActionNpcFollowStop     = "npc_follow_stop"
	ActionNpcLeadTo         = "npc_lead_to"
	ActionNpcGetBehavior    = "npc_get_behavior"
	ActionPlayerGetStatus   = "player_get_status"
)

// Request is a client → server RPC call.
type Request struct {
	Type   string      `json:"type"`             // "request"
	ID     string      `json:"id"`               // uuid
	Action string      `json:"action"`           // see ActionName constants
	Params interface{} `json:"params,omitempty"` // action-specific
}

// Response is a server → client reply correlated with a Request by ID.
type Response struct {
	Type  string          `json:"type"`            // "response"
	ID    string          `json:"id"`              // mirrors Request.ID
	OK    bool            `json:"ok"`              // success flag
	Data  json.RawMessage `json:"data,omitempty"`  // present when OK
	Error *ResponseError  `json:"error,omitempty"` // present when !OK
}

// ResponseError carries a stable code and a human message.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Event is a server → client push (no reply expected).
type Event struct {
	Type      string          `json:"type"`      // "event"
	Name      string          `json:"name"`      // see EventName constants
	Data      json.RawMessage `json:"data"`      // event-specific
	Timestamp int64           `json:"timestamp"` // unix millis
}

// frameType is used internally to peek at the envelope before full decode.
type frameType struct {
	Type string `json:"type"`
}
