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
//
// Implemented today (emitted by the SMAPI mod):
//
//   - EventChatMessage    — player sent a line in the chat panel (targeted)
//   - EventChatReceived   — legacy / group-chat channel (generic)
//   - EventNpcInteract    — player clicked an Agent-managed NPC
//
// Reserved for future mod work (schema frozen in docs/events.md so
// downstream consumers — hermesrelay, Hermes profile — can code against
// the envelope before the mod side lands):
//
//   - EventDayStarted
//   - EventLocationChanged
//   - EventFriendshipChanged
const (
	EventChatMessage         = "chat_message"
	EventChatReceived        = "chat_received"
	EventNpcInteract         = "npc_interact"
	EventGroupCreate         = "group_create" // legacy group chat signal
	EventDayStarted          = "day_started"
	EventLocationChanged     = "location_changed"
	EventFriendshipChanged   = "friendship_changed"
	EventNpcPerceptionUpdate = "npc_perception_update" // reserved, not emitted yet
	EventGameTimeTick        = "game_time_tick"        // reserved — emitted every game hour for scheduler

	// Synthetic (mcp-originated, not from mod) — inter-NPC messaging fan-out.
	// See internal/tools/npc_message.go.
	EventNpcMessage          = "npc_message"
	EventNpcBroadcast        = "npc_broadcast"

	// Synthetic — game-time scheduler triggers a planned activity.
	EventScheduleTrigger     = "schedule_trigger"

	// Debug — fired by the SMAPI mod's `sn_proactive` console command.
	// Carries `{npc: "<PascalCase>"}`. Format layer renders it as a
	// system-prompt nudge that makes the target NPC run the
	// smartnpc-visit SKILL immediately, skipping the dice
	// roll and the 60-minute cool-down. Intended for operators
	// testing proactive behavior without waiting for the scheduled
	// */15 minute cron.
	EventDebugProactiveTrigger = "debug_proactive_trigger"
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
	ActionNpcEmote          = "npc_emote"
	ActionNpcGiveItem       = "npc_give_item"
	ActionNpcFollowStart    = "npc_follow_start"
	ActionNpcFollowStop     = "npc_follow_stop"
	ActionNpcLeadTo         = "npc_lead_to"
	ActionNpcGetBehavior    = "npc_get_behavior"
	ActionPlayerGetStatus   = "player_get_status"

	// ── Rich NPC behaviors (world interaction) ──────────────────
	ActionNpcWander        = "npc_wander"
	ActionNpcClearDebris   = "npc_clear_debris"
	ActionNpcWaterCrops    = "npc_water_crops"
	ActionNpcHarvestCrops  = "npc_harvest_crops"
	ActionNpcDepositItems  = "npc_deposit_items"
	ActionNpcDeliverItems  = "npc_deliver_items"
	ActionNpcForageCollect = "npc_forage_collect"
	ActionNpcPetAnimal     = "npc_pet_animal"
	ActionNpcPlantSeeds    = "npc_plant_seeds"
	ActionNpcTillSoil      = "npc_till_soil"
	ActionNpcInspectObject = "npc_inspect_object"
	ActionNpcPlaceObject   = "npc_place_object"
	ActionNpcBreakResource = "npc_break_resource"
	ActionNpcFertilize     = "npc_fertilize"
	ActionNpcFillGaps      = "npc_fill_gaps"

	// ── NPC inventory transfer ────────────────────────────────────
	ActionNpcWithdrawItems = "npc_withdraw_from_chest"
	ActionNpcTransferItem  = "npc_transfer_item"

	// ── NPC inventory ────────────────────────────────────────────
	ActionNpcInventoryGet  = "npc_inventory_get"
	ActionNpcInventoryPut  = "npc_inventory_put"
	ActionNpcInventoryTake = "npc_inventory_take"

	// ── Rich NPC behaviors (social / performance) ───────────────
	ActionNpcApproachAndSpeak = "npc_approach_and_speak"
	ActionNpcExpressEmotion   = "npc_express_emotion"
	ActionNpcShyRetreat       = "npc_shy_retreat"
	ActionNpcShowTextBubble   = "npc_show_text_bubble"
	ActionNpcIdleActivity     = "npc_idle_activity"
	ActionNpcDanceHappy       = "npc_dance_happy"
	ActionNpcReactSurprise    = "npc_react_surprise"
	ActionNpcPaceAnxiously    = "npc_pace_anxiously"
)

// Request is a client → server RPC call.
//
// FromNPC carries the originating NPC profile when the request is issued by
// a registered agent (via WSClient.CallAs / agent_register_self). The mod
// uses it to route inbound debug mirrors to the correct chat-panel
// conversation; absent / empty FromNPC means the caller is operating
// outside any agent context (e.g. operator console, tests).
type Request struct {
	Type    string      `json:"type"`               // "request"
	ID      string      `json:"id"`                 // uuid
	Action  string      `json:"action"`             // see ActionName constants
	Params  any         `json:"params,omitempty"`   // action-specific
	FromNPC string      `json:"from_npc,omitempty"` // originating NPC profile, if any
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
