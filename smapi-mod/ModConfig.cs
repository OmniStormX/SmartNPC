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
        /// When <c>true</c>, every stub-action invocation (any tool whose
        /// Mod-side implementation isn't written yet) also pushes a debug
        /// message into the player's chat panel — same surface as
        /// <c>chat_say</c>, so you see <c>[stub:npc_water_crops] params=...</c>
        /// in the conversation history alongside the head-bubble. Default
        /// <c>false</c> — only the bubble is shown.
        /// </summary>
        public bool DebugShowMessage { get; set; } = false;
    }
}
