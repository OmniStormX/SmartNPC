package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
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
		Description: "Read the current in-game time (hour/minute), date (day/season/year) " +
			"and short day-of-week. Use this before greeting the player so you can say " +
			"\"good morning\" vs \"good evening\" appropriately. Takes no parameters.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GameGetTimeInput) (*mcp.CallToolResult, GameGetTimeOutput, error) {
		raw, err := br.Call(ctx, bridge.ActionGameGetTime, in)
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
		Description: "Read today's in-game weather (sunny/rainy/snowy/stormy) plus the " +
			"current season. Use this to make weather-aware small talk (\"brought an " +
			"umbrella?\") or to gate outdoor activity suggestions. Takes no parameters.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GameGetWeatherInput) (*mcp.CallToolResult, GameGetWeatherOutput, error) {
		raw, err := br.Call(ctx, bridge.ActionGameGetWeather, in)
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
		Description: "Read the player's friendship level with a specific NPC (by internal " +
			"name, e.g. \"Abigail\", \"XiaMi\"). Returns raw points, hearts (points/250), " +
			"and relationship status (friendly/dating/engaged/married/none). Use this to " +
			"calibrate warmth of dialogue — more hearts means more intimacy.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FriendshipGetInput) (*mcp.CallToolResult, FriendshipGetOutput, error) {
		if in.NPC == "" {
			return nil, FriendshipGetOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionFriendshipGet, in)
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
