package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// ── game_get_time ──────────────────────────────────────────────

type GameGetTimeInput struct{}

type GameGetTimeOutput struct {
	OK        bool   `json:"ok"          jsonschema:"true on success"`
	Hour      int    `json:"hour"        jsonschema:"current hour (0-23)"`
	Minute    int    `json:"minute"      jsonschema:"current minute (0 or 30)"`
	TimeOfDay int    `json:"timeOfDay"   jsonschema:"raw time value, e.g. 1430"`
	Day       int    `json:"day"         jsonschema:"day of month (1-28)"`
	DayOfWeek string `json:"day_of_week" jsonschema:"short day name, e.g. Mon"`
	Season    string `json:"season"      jsonschema:"spring/summer/fall/winter"`
	Year      int    `json:"year"        jsonschema:"in-game year"`
}

// ── game_get_weather ───────────────────────────────────────────

type GameGetWeatherInput struct{}

type GameGetWeatherOutput struct {
	OK          bool   `json:"ok"           jsonschema:"true on success"`
	Weather     string `json:"weather"      jsonschema:"sunny/rainy/snowy/stormy"`
	IsRaining   bool   `json:"is_raining"   jsonschema:"true if raining"`
	IsSnowing   bool   `json:"is_snowing"   jsonschema:"true if snowing"`
	IsLightning bool   `json:"is_lightning" jsonschema:"true if thunderstorm"`
	Season      string `json:"season"       jsonschema:"current season"`
}

// ── friendship_get ─────────────────────────────────────────────

type FriendshipGetInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name, e.g. \"XiaMi\" or \"Abigail\""`
}

type FriendshipGetOutput struct {
	OK        bool   `json:"ok"         jsonschema:"true on success"`
	NPC       string `json:"npc"        jsonschema:"queried NPC name"`
	Points    int    `json:"points"     jsonschema:"raw friendship points"`
	Hearts    int    `json:"hearts"     jsonschema:"friendship hearts (points/250)"`
	MaxHearts int    `json:"max_hearts" jsonschema:"maximum hearts (usually 10)"`
	Status    string `json:"status"     jsonschema:"relationship status: friendly/dating/engaged/married/none"`
}

// ── registration ───────────────────────────────────────────────

func registerGameQuery(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "game_get_time",
		Description: "Read the current in-game clock and calendar: hour (0-23), minute, " +
			"raw time-of-day (e.g. 1430), day-of-month (1-28), short day-of-week, " +
			"season (spring/summer/fall/winter), and year.\n\n" +
			"When to call: BEFORE any greeting or reply that references time (\"good morning\" " +
			"vs \"good evening\"), before suggesting bedtime / late-night activity, or when " +
			"the player asks \"what time is it\" / \"几点了\".\n\n" +
			"Side-effect: READ. Takes no parameters. Requires a loaded save.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in GameGetTimeInput) (*mcp.CallToolResult, GameGetTimeOutput, error) {
		logToolCall("game_get_time", in)
		raw, err := br.Call(callCtx(ctx, req), bridge.ActionGameGetTime, in)
		if err != nil {
			return nil, GameGetTimeOutput{}, fmt.Errorf("game_get_time: %w", err)
		}
		var out GameGetTimeOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "game_get_weather",
		Description: "Read today's weather (sunny/rainy/snowy/stormy) and the current season. " +
			"Separate boolean flags distinguish rain/snow/lightning for fine-grained checks.\n\n" +
			"When to call: for weather-aware small talk (\"brought an umbrella?\"), before " +
			"gating outdoor activity suggestions, or when the player asks about the weather.\n\n" +
			"Side-effect: READ. Takes no parameters. Requires a loaded save.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in GameGetWeatherInput) (*mcp.CallToolResult, GameGetWeatherOutput, error) {
		logToolCall("game_get_weather", in)
		raw, err := br.Call(callCtx(ctx, req), bridge.ActionGameGetWeather, in)
		if err != nil {
			return nil, GameGetWeatherOutput{}, fmt.Errorf("game_get_weather: %w", err)
		}
		var out GameGetWeatherOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "friendship_get",
		Description: "Read the player's relationship with a specific NPC: raw points, " +
			"hearts (points/250, capped at `max_hearts` which is usually 10), and a " +
			"status label (friendly/dating/engaged/married/none).\n\n" +
			"When to call: BEFORE any relationship-sensitive reply — gifts, apologies, " +
			"romance, emotionally intense topics, or when the player asks \"do you like " +
			"me?\" / \"我们关系怎么样\". Calibrate warmth of dialogue to heart count " +
			"(0-2: polite distance, 3-6: friendly, 7+: intimate).\n\n" +
			"Side-effect: READ. Requires a loaded save. Returns `invalid_params` if `npc` " +
			"is empty, `npc_not_found` if the name does not resolve.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in FriendshipGetInput) (*mcp.CallToolResult, FriendshipGetOutput, error) {
		if in.NPC == "" {
			return nil, FriendshipGetOutput{}, fmt.Errorf("npc is required")
		}
		logToolCall("friendship_get", in)
		raw, err := br.Call(callCtx(ctx, req), bridge.ActionFriendshipGet, in)
		if err != nil {
			return nil, FriendshipGetOutput{}, fmt.Errorf("friendship_get: %w", err)
		}
		var out FriendshipGetOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}
