// SmartNPC SMAPI mod per-install configuration.
//
// Loaded by SMAPI via `helper.ReadConfig<ModConfig>()` — it auto-generates
// <Mods>/StardewMCPBridge/config.json on first launch with the defaults
// below. Players can then edit the file to bind the ws server to a
// non-default host / port (useful when 18745 clashes with something else,
// or when running multiple SDV instances side-by-side).
//
// Keep field names stable: renaming breaks existing config.json files.

namespace SmartNPC.Bridge
{
    public sealed class ModConfig
    {
        /// <summary>
        /// Interface the ws server listens on. Default <c>127.0.0.1</c>
        /// (loopback only). Use <c>0.0.0.0</c> to expose to the LAN — do NOT
        /// do that on an untrusted network; the protocol has no auth yet.
        /// </summary>
        public string Host { get; set; } = "127.0.0.1";

        /// <summary>
        /// TCP port the ws server binds to. Default <c>18745</c>. Must match
        /// the <c>--ws-url</c> passed to <c>smartnpc-mcp</c>.
        /// </summary>
        public int Port { get; set; } = 18745;

        /// <summary>
        /// Build the <c>http://host:port/</c> prefix consumed by
        /// <see cref="System.Net.HttpListener"/>.
        /// </summary>
        public string ListenPrefix() => $"http://{this.Host}:{this.Port}/";

        /// <summary>
        /// When <c>true</c>, every inbound <c>request</c> frame received
        /// from <c>smartnpc-mcp</c> over the ws bridge is mirrored into the
        /// player's in-game chat panel as
        /// <c>[mcp→mod] action=&lt;name&gt; id=&lt;id&gt; params=&lt;json&gt;</c>.
        /// Useful for verifying which tools the LLM is firing and with
        /// what arguments without tailing log files. Truncated to 240
        /// chars per line. Default <c>false</c>.
        /// </summary>
        public bool DebugLogIncomingRequests { get; set; } = false;

        /// <summary>
        /// When <c>true</c>, NPC behavior actions (world + social) show a
        /// text bubble above the NPC's head indicating what action was
        /// triggered (e.g. "[wander] 去镇上逛逛"). Useful for debugging
        /// schedule triggers and NPC AI decisions visually in-game.
        /// When <c>false</c>, the handler runs but no bubble is displayed.
        /// Default <c>true</c>.
        /// </summary>
        public bool DebugShowBubble { get; set; } = true;

        /// <summary>
        /// When <c>true</c>, draw axis-aligned debug rectangles in the world
        /// to visualize coarse-grained tool decisions:
        ///   - red box   : the area an <c>npc_inspect_object</c> call
        ///                 (farm_actions mode) just scanned. Auto-fades a
        ///                 few seconds after the observation finishes.
        ///   - yellow box: the bbox a behavior tool (harvest/water/clear/
        ///                 till/forage) is currently acting in. Cleared
        ///                 when the NPC's FollowSystem returns to Idle.
        /// Only the local player's current map is rendered; off-map NPCs
        /// do not draw. Default <c>false</c> to avoid surprising players
        /// who didn't opt in.
        /// </summary>
        public bool DebugShowBBoxOverlay { get; set; } = false;
    }
}
