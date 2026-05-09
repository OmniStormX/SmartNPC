// Handles the player_get_status query action used by the proactive scheduler.
// Read-only and synchronous: every flag is derived from Game1 state without
// pumping the game tick.

using System.Text.Json;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class PlayerQueryHandler
    {
        private readonly IMonitor _log;

        public PlayerQueryHandler(IMonitor log) { _log = log; }

        public Task<Response> HandleGetStatus(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            // `busy` is the composite flag the agent uses to decide whether
            // to interrupt the player. Anything that pops a modal-style
            // overlay (menus / events / festivals) counts.
            bool inMenu = Game1.activeClickableMenu != null;
            bool inEvent = Game1.eventUp || Game1.isFestival();
            bool isMoving = false;
            string location = "";

            try
            {
                if (Game1.player != null)
                {
                    isMoving = Game1.player.isMoving();
                }
                if (Game1.currentLocation != null)
                {
                    location = Game1.currentLocation.Name ?? "";
                }
            }
            catch
            {
                // Defensive: any null deref / state edge case during a save
                // transition falls through to an "unknown" response rather
                // than a 500. The scheduler treats missing data as
                // "available" by default, which is the safer fallback.
            }

            bool busy = inMenu || inEvent;

            var result = new
            {
                ok = true,
                busy,
                in_menu = inMenu,
                in_event = inEvent,
                is_moving = isMoving,
                location,
            };

            return Task.FromResult(Response.Success(id, result));
        }
    }
}
