// Handles game state query actions: game_get_time, game_get_weather, friendship_get.
// All are read-only and return immediately (no PumpOnGameTick needed).

using System;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class GameQueryHandler
    {
        private readonly IMonitor _log;

        public GameQueryHandler(IMonitor log) { _log = log; }

        public Task<Response> HandleGetTime(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            var result = new
            {
                ok = true,
                hour = Game1.timeOfDay / 100,
                minute = Game1.timeOfDay % 100,
                timeOfDay = Game1.timeOfDay,
                day = Game1.dayOfMonth,
                dayOfWeek = Game1.shortDayNameFromDayOfSeason(Game1.dayOfMonth),
                season = Game1.currentSeason,
                year = Game1.year,
            };

            return Task.FromResult(Response.Success(id, result));
        }

        public Task<Response> HandleGetWeather(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            string weather = "sunny";
            bool isRaining = Game1.isRaining;
            bool isSnowing = Game1.isSnowing;
            bool isLightning = Game1.isLightning;

            if (isLightning) weather = "stormy";
            else if (isRaining) weather = "rainy";
            else if (isSnowing) weather = "snowy";

            var result = new
            {
                ok = true,
                weather,
                is_raining = isRaining,
                is_snowing = isSnowing,
                is_lightning = isLightning,
                season = Game1.currentSeason,
            };

            return Task.FromResult(Response.Success(id, result));
        }

        public Task<Response> HandleGetFriendship(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            FriendshipParams? p;
            try { p = JsonSerializer.Deserialize<FriendshipParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            int points = 0;
            int hearts = 0;
            string status = "none";

            if (Game1.player.friendshipData.TryGetValue(p.Npc, out var friendship))
            {
                points = friendship.Points;
                hearts = points / 250;
                status = friendship.Status.ToString().ToLower();
            }

            var result = new
            {
                ok = true,
                npc = p.Npc,
                points,
                hearts,
                max_hearts = 10,
                status,
            };

            return Task.FromResult(Response.Success(id, result));
        }

        private sealed class FriendshipParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
        }
    }
}
