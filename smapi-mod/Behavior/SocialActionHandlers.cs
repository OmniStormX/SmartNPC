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
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_approach_and_speak";
        protected override bool RefuseWhileBusy => true;

        public ApproachAndSpeakHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            string? reason = ParseReason(@params);

            if (!string.IsNullOrWhiteSpace(reason))
                return $"[搭话] {reason}";
            return "[搭话]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            string? reason = ParseReason(@params);

            _follow.StartApproachAndSpeak(npcName, reason);

            Log.Log(
                $"[npc_approach_and_speak] {npcName}: started{(reason is not null ? $" reason=\"{reason}\"" : "")}",
                LogLevel.Info);
        }

        /// <summary>
        /// Read the bubble text from params. Go MCP tool sends "message",
        /// legacy callers may send "reason". Try both.
        /// </summary>
        private static string? ParseReason(JsonElement @params)
        {
            if (@params.ValueKind != JsonValueKind.Object) return null;

            // Primary: "message" field (from MCP tool NpcApproachAndSpeakInput).
            if (@params.TryGetProperty("message", out JsonElement msgEl) &&
                msgEl.ValueKind == JsonValueKind.String)
                return msgEl.GetString();

            // Fallback: "reason" field (legacy / debug).
            if (@params.TryGetProperty("reason", out JsonElement rEl) &&
                rEl.ValueKind == JsonValueKind.String)
                return rEl.GetString();

            return null;
        }
    }

    internal sealed class ExpressEmotionHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_express_emotion";
        public ExpressEmotionHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }

        private static readonly System.Collections.Generic.Dictionary<string, int> s_emotes = new(StringComparer.OrdinalIgnoreCase)
        {
            ["heart"] = 20,
            ["exclamation"] = 16,
            ["question"] = 24,
            ["anger"] = 12,
            ["sweat"] = 28,
            ["cry"] = 4,
            ["note"] = 8,
            ["sleep"] = 32,
            ["star"] = 36,
            ["neutral"] = 0,
            ["happy"] = 20,
            ["sad"] = 28,
            ["surprised"] = 24,
            ["angry"] = 12,
        };

        protected override string ResolveBubble(JsonElement @params)
        {
            string? emo = ParseEmotion(@params);
            return emo is not null ? $"[{emo}]" : "[emote]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            string? emo = ParseEmotion(@params);
            int emoteId = 20; // default: heart
            if (emo is not null && s_emotes.TryGetValue(emo, out int id))
                emoteId = id;

            npc.doEmote(emoteId);
            Log.Log(
                $"[npc_express_emotion] {npcName}: emote={emo} id={emoteId}",
                LogLevel.Debug);
        }

        private static string? ParseEmotion(JsonElement @params)
        {
            if (@params.ValueKind != JsonValueKind.Object) return null;
            if (@params.TryGetProperty("emotion", out JsonElement e) &&
                e.ValueKind == JsonValueKind.String)
                return e.GetString();
            if (@params.TryGetProperty("emote", out JsonElement e2) &&
                e2.ValueKind == JsonValueKind.String)
                return e2.GetString();
            return null;
        }
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
