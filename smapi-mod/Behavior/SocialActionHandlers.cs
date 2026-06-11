// Per-action handler files for social actions.
// Each is a minimal subclass of NpcActionHandlerBase. Override Execute
// when ready to implement real game logic.

using System;
using System.Text.Json;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    // ── Social actions ───────────────────────────────────────────────

    internal sealed class ApproachAndSpeakHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_approach_and_speak";
        public ApproachAndSpeakHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: pathfind to player and initiate dialogue
    }

    internal sealed class ExpressEmotionHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_express_emotion";
        public ExpressEmotionHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: play emote animation matching the emotion param
    }

    internal sealed class ShyRetreatHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_shy_retreat";
        public ShyRetreatHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: turn away and walk a few tiles back
    }

    internal sealed class ShowTextBubbleHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_show_text_bubble";
        public ShowTextBubbleHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }

        /// <summary>Use params.text directly as the bubble content.</summary>
        protected override string ResolveBubble(JsonElement @params)
        {
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("text", out JsonElement textEl) &&
                textEl.ValueKind == JsonValueKind.String &&
                !string.IsNullOrEmpty(textEl.GetString()))
            {
                return textEl.GetString()!;
            }
            return "[text_bubble]";
        }

        // Execute is intentionally a no-op: the bubble IS the action.
    }

    internal sealed class IdleActivityHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_idle_activity";
        public IdleActivityHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: play idle animation (farm, rest, look_around)
    }

    internal sealed class DanceHappyHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_dance_happy";
        public DanceHappyHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: play happy dance sprite animation
    }

    internal sealed class ReactSurpriseHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_react_surprise";
        public ReactSurpriseHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: play surprise jump + emote
    }

    internal sealed class PaceAnxiouslyHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_pace_anxiously";
        public PaceAnxiouslyHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: pace back and forth on 2-3 tiles
    }
}
