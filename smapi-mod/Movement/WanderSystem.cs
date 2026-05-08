// WanderSystem — NPC autonomous movement state machine.
//
// Agent-managed NPCs automatically wander within their current map. The system
// picks random walkable tiles near the NPC and uses PathFindController to move
// there, with idle pauses between walks. This is purely C#-side logic — no LLM
// involvement or MCP tool calls needed.
//
// When two agent-managed NPCs come within EncounterRadius of each other, an
// "npc_encounter" logging/notification event is broadcast so the Agent can
// decide whether to initiate a conversation (memory sharing).
//
// Interaction with FollowSystem: if an NPC is in Following/Leading/Summoning
// mode, WanderSystem yields and does not override their pathfinding.
//
// Idle behavior: when an NPC reaches its target it enters the Idle phase and
// rolls a die:
//   - 30% play a SDV built-in emote bubble (doEmote)
//   - 20% switch to a custom static pose frame (XiaMi only) for 2-3 seconds
//   -  5% play a one-shot action animation (hoe / watering) — Animating phase
//   - 45% just stand and face a random direction
// NPCs without custom frames (non-XiaMi) only get emote + random turn.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Pathfinding;

namespace SmartNPC.Bridge
{
    internal sealed class WanderSystem
    {
        // How often (in ticks) to evaluate wander decisions.
        private const int EvalIntervalTicks = 60; // ~1 second at 60fps

        // NPC will walk to a random tile within this radius. Reduced from 12
        // to 8 so wanders stay local and feel less teleport-y.
        private const int WanderRadius = 8;

        // Probability (0..1) that PickRandomNearbyTile biases the candidate
        // toward the NPC's current facing direction.
        private const double ForwardBiasProb = 0.6;

        // Idle pause: after arriving, wait this many ticks before next walk.
        private const int MinIdleTicks = 180;  // 3 seconds
        private const int MaxIdleTicks = 600;  // 10 seconds

        // Static pose hold time (when NPC freezes on a non-walk frame).
        private const int MinPoseTicks = 120; // 2 seconds
        private const int MaxPoseTicks = 180; // 3 seconds

        // Two agent NPCs within this tile distance trigger an encounter event.
        private const float EncounterRadius = 3.5f;

        // Cooldown between encounter events for the same NPC pair (ticks).
        private const int EncounterCooldownTicks = 3600; // ~60 seconds

        // SDV built-in emote indices (see Game1 / NPC.doEmote).
        private static readonly int[] BuiltInEmotes =
        {
            20, // happy
            28, // angry / question
            32, // surprised
            40, // heart
            56, // sleep (Zz)
        };

        // Custom static pose frames available on XiaMi's spritesheet.
        private static readonly int[] XiaMiPoseFrames =
        {
            XiaMiData.FrameIdleStand,
            XiaMiData.FrameHoldPumpkin,
            XiaMiData.FrameHoldCrop,
            XiaMiData.FrameHoldFlower,
            XiaMiData.FrameHoldChicken,
        };

        private readonly IMonitor _log;
        private readonly FollowSystem _followSystem;
        private readonly Dictionary<string, WanderState> _states = new(StringComparer.OrdinalIgnoreCase);
        private readonly Dictionary<string, uint> _encounterCooldowns = new(StringComparer.OrdinalIgnoreCase);
        private readonly Random _rng = new();

        private uint _tickCounter;

        /// <summary>Fired when two agent NPCs meet. Handler should broadcast to the Agent.</summary>
        public event Action<string, string, string>? OnNpcEncounter; // (npcA, npcB, mapName)

        public WanderSystem(IMonitor log, FollowSystem followSystem)
        {
            _log = log;
            _followSystem = followSystem;
        }

        /// <summary>Call from ModEntry.OnUpdateTicked every tick.</summary>
        public void Tick()
        {
            _tickCounter++;
            if (_tickCounter % EvalIntervalTicks != 0)
                return;

            foreach (string npcName in AgentNpcRegistry.GetAll())
            {
                var npc = Game1.getCharacterFromName(npcName);
                if (npc == null) continue;

                // Yield to FollowSystem if NPC is in an active behavior mode.
                if (_followSystem.GetMode(npcName) != NpcBehaviorMode.Idle)
                    continue;

                if (!_states.TryGetValue(npcName, out var state))
                {
                    state = new WanderState();
                    _states[npcName] = state;
                }

                UpdateWander(npc, state);
            }

            // Check for NPC-NPC encounters.
            CheckEncounters();
        }

        private void UpdateWander(NPC npc, WanderState state)
        {
            switch (state.Phase)
            {
                case WanderPhase.Idle:
                    state.IdleTicksRemaining -= EvalIntervalTicks;
                    if (state.IdleTicksRemaining <= 0)
                        StartNextWander(npc, state);
                    break;

                case WanderPhase.Posing:
                    // Holding a static pose frame — keep re-asserting it in case
                    // SDV's sprite updater tries to override (paranoia).
                    if (state.PoseFrame >= 0)
                        npc.Sprite.CurrentFrame = state.PoseFrame;

                    state.IdleTicksRemaining -= EvalIntervalTicks;
                    if (state.IdleTicksRemaining <= 0)
                    {
                        // Restore a walk-facing frame before returning to Idle.
                        ResetToFacingFrame(npc);
                        state.Phase = WanderPhase.Idle;
                        state.PoseFrame = -1;
                        state.IdleTicksRemaining = _rng.Next(MinIdleTicks, MaxIdleTicks);
                    }
                    break;

                case WanderPhase.Animating:
                    // Custom animation is driven by AnimatedSprite itself; we
                    // just wait out the budgeted time, then force-exit to Idle.
                    state.IdleTicksRemaining -= EvalIntervalTicks;
                    if (state.IdleTicksRemaining <= 0)
                    {
                        npc.Sprite.CurrentAnimation = null;
                        ResetToFacingFrame(npc);
                        state.Phase = WanderPhase.Idle;
                        state.IdleTicksRemaining = _rng.Next(MinIdleTicks, MaxIdleTicks);
                    }
                    break;

                case WanderPhase.Walking:
                    // Check if NPC has arrived (controller cleared or at target).
                    if (npc.controller == null)
                        EnterIdleAfterArrival(npc, state);
                    break;
            }
        }

        // ── Phase transitions ─────────────────────────────────────────────

        private void EnterIdleAfterArrival(NPC npc, WanderState state)
        {
            // Random turn on arrival so the NPC doesn't always face the walk
            // direction — adds life at path ends.
            npc.faceDirection(_rng.Next(4));

            double roll = _rng.NextDouble();
            bool isXiaMi = string.Equals(npc.Name, "XiaMi", StringComparison.Ordinal);

            if (roll < 0.30)
            {
                // Emote bubble (available to all NPCs).
                TryDoEmote(npc);
                state.Phase = WanderPhase.Idle;
                state.IdleTicksRemaining = _rng.Next(MinIdleTicks, MaxIdleTicks);
            }
            else if (isXiaMi && roll < 0.50)
            {
                // Hold a static pose frame for 2-3 seconds.
                int frame = XiaMiPoseFrames[_rng.Next(XiaMiPoseFrames.Length)];
                npc.Sprite.CurrentAnimation = null;
                npc.Sprite.CurrentFrame = frame;
                state.PoseFrame = frame;
                state.Phase = WanderPhase.Posing;
                state.IdleTicksRemaining = _rng.Next(MinPoseTicks, MaxPoseTicks);
            }
            else if (isXiaMi && roll < 0.55)
            {
                // 5% — play a one-shot action animation (hoe or watering).
                if (TryStartActionAnimation(npc, state))
                    return;
                // Fallback to normal idle on failure.
                state.Phase = WanderPhase.Idle;
                state.IdleTicksRemaining = _rng.Next(MinIdleTicks, MaxIdleTicks);
            }
            else
            {
                // Plain idle — just stand and face a random direction.
                state.Phase = WanderPhase.Idle;
                state.IdleTicksRemaining = _rng.Next(MinIdleTicks, MaxIdleTicks);
            }
        }

        private void StartNextWander(NPC npc, WanderState state)
        {
            var target = PickRandomNearbyTile(npc);
            if (!target.HasValue)
            {
                state.IdleTicksRemaining = MinIdleTicks;
                return;
            }

            try
            {
                var path = new PathFindController(
                    c: npc,
                    location: npc.currentLocation,
                    endPoint: target.Value,
                    finalFacingDirection: -1,
                    endBehaviorFunction: null);
                if (path.pathToEndPoint != null && path.pathToEndPoint.Count > 0)
                {
                    npc.controller = path;
                    state.Phase = WanderPhase.Walking;
                    state.TargetTile = target.Value;
                }
                else
                {
                    state.IdleTicksRemaining = MinIdleTicks;
                }
            }
            catch
            {
                state.IdleTicksRemaining = MinIdleTicks;
            }
        }

        // ── Animation helpers ─────────────────────────────────────────────

        private void TryDoEmote(NPC npc)
        {
            try
            {
                int emote = BuiltInEmotes[_rng.Next(BuiltInEmotes.Length)];
                npc.doEmote(emote);
            }
            catch (Exception ex)
            {
                _log.Log($"[Wander] doEmote failed for {npc.Name}: {ex.Message}", LogLevel.Trace);
            }
        }

        /// <summary>
        /// Play a one-shot hoe or watering animation using AnimatedSprite.
        /// Returns true on success; state is updated to Animating.
        /// </summary>
        private bool TryStartActionAnimation(NPC npc, WanderState state)
        {
            try
            {
                // Face down so the action frames render correctly.
                npc.faceDirection(2);

                bool hoe = _rng.Next(2) == 0;
                int start = hoe ? XiaMiData.FrameHoeStart : XiaMiData.FrameWaterStart;
                int end = hoe ? XiaMiData.FrameHoeEnd : XiaMiData.FrameWaterEnd;

                var frames = new List<FarmerSprite.AnimationFrame>();
                for (int f = start; f <= end; f++)
                    frames.Add(new FarmerSprite.AnimationFrame(f, 180, false, false));
                // Hold the last frame briefly.
                frames.Add(new FarmerSprite.AnimationFrame(end, 300, false, false));

                npc.Sprite.setCurrentAnimation(frames);
                npc.Sprite.loop = false;

                state.Phase = WanderPhase.Animating;
                // Give the animation up to 5 seconds to complete before we force-exit.
                state.IdleTicksRemaining = 300;
                return true;
            }
            catch (Exception ex)
            {
                _log.Log($"[Wander] action animation failed for {npc.Name}: {ex.Message}", LogLevel.Trace);
                return false;
            }
        }

        /// <summary>
        /// Restore the sprite's CurrentFrame to a plausible standing frame for
        /// the NPC's current facing direction, so the NPC doesn't get stuck on
        /// an action/pose frame after coming back from Posing/Animating.
        /// </summary>
        private static void ResetToFacingFrame(NPC npc)
        {
            try
            {
                int facing = npc.FacingDirection;
                int frame = facing switch
                {
                    0 => XiaMiData.FrameBackStart,   // up
                    1 => XiaMiData.FrameRightStart,  // right
                    2 => XiaMiData.FrameFrontStart,  // down
                    3 => XiaMiData.FrameLeftStart,   // left
                    _ => XiaMiData.FrameFrontStart,
                };
                // This frame layout is only valid for XiaMi; other NPCs use the
                // standard 0/4/8/12 layout which happens to match our indices,
                // so this is still broadly safe.
                npc.Sprite.CurrentFrame = frame;
            }
            catch
            {
                // Ignore — SDV will reassign a walk frame on the next move.
            }
        }

        // ── Tile selection ────────────────────────────────────────────────

        private Point? PickRandomNearbyTile(NPC npc)
        {
            var loc = npc.currentLocation;
            var center = npc.TilePoint;
            bool preferForward = _rng.NextDouble() < ForwardBiasProb;
            (int fx, int fy) = FacingVector(npc.FacingDirection);

            // Try up to 8 random tiles within WanderRadius.
            for (int i = 0; i < 8; i++)
            {
                int dx, dy;
                if (preferForward && i < 5)
                {
                    // Bias the first few candidates toward the facing direction:
                    // forward component in [1..radius], side component in [-radius/2..radius/2].
                    int forward = _rng.Next(1, WanderRadius + 1);
                    int side = _rng.Next(-WanderRadius / 2, WanderRadius / 2 + 1);
                    dx = fx * forward + Math.Abs(fy) * side;
                    dy = fy * forward + Math.Abs(fx) * side;
                }
                else
                {
                    dx = _rng.Next(-WanderRadius, WanderRadius + 1);
                    dy = _rng.Next(-WanderRadius, WanderRadius + 1);
                }

                var candidate = new Point(center.X + dx, center.Y + dy);

                // Skip if out of bounds.
                if (candidate.X < 0 || candidate.Y < 0 ||
                    candidate.X >= loc.Map.Layers[0].LayerWidth ||
                    candidate.Y >= loc.Map.Layers[0].LayerHeight)
                    continue;

                // Skip zero-delta (same tile).
                if (dx == 0 && dy == 0)
                    continue;

                // Check tile passability.
                if (loc.isTilePassable(new xTile.Dimensions.Location(candidate.X * 64, candidate.Y * 64), Game1.viewport) &&
                    !loc.isObjectAtTile(candidate.X, candidate.Y) &&
                    !loc.isTerrainFeatureAt(candidate.X, candidate.Y))
                {
                    return candidate;
                }
            }
            return null;
        }

        private static (int dx, int dy) FacingVector(int facing) => facing switch
        {
            0 => (0, -1), // up
            1 => (1, 0),  // right
            2 => (0, 1),  // down
            3 => (-1, 0), // left
            _ => (0, 1),
        };

        // ── Encounters ────────────────────────────────────────────────────

        private void CheckEncounters()
        {
            var allManaged = AgentNpcRegistry.GetAll();
            for (int i = 0; i < allManaged.Count; i++)
            {
                var npcA = Game1.getCharacterFromName(allManaged[i]);
                if (npcA == null) continue;

                for (int j = i + 1; j < allManaged.Count; j++)
                {
                    var npcB = Game1.getCharacterFromName(allManaged[j]);
                    if (npcB == null) continue;

                    // Must be on same map.
                    if (npcA.currentLocation != npcB.currentLocation)
                        continue;

                    float dist = Vector2.Distance(npcA.Position / 64f, npcB.Position / 64f);
                    if (dist > EncounterRadius)
                        continue;

                    // Check cooldown.
                    string pairKey = string.Compare(allManaged[i], allManaged[j], StringComparison.Ordinal) < 0
                        ? $"{allManaged[i]}:{allManaged[j]}"
                        : $"{allManaged[j]}:{allManaged[i]}";

                    if (_encounterCooldowns.TryGetValue(pairKey, out uint lastTick) &&
                        _tickCounter - lastTick < EncounterCooldownTicks)
                        continue;

                    _encounterCooldowns[pairKey] = _tickCounter;

                    _log.Log($"[Wander] NPC encounter: {allManaged[i]} <-> {allManaged[j]} " +
                             $"(dist={dist:F1}, map={npcA.currentLocation.Name})", LogLevel.Info);

                    OnNpcEncounter?.Invoke(allManaged[i], allManaged[j], npcA.currentLocation.Name);
                }
            }
        }
    }

    internal enum WanderPhase
    {
        Idle,
        Walking,
        Posing,
        Animating,
    }

    internal sealed class WanderState
    {
        public WanderPhase Phase { get; set; } = WanderPhase.Idle;
        public int IdleTicksRemaining { get; set; } = 60; // Start with short initial idle.
        public Point TargetTile { get; set; }
        /// <summary>Frame index currently held during Posing phase (-1 = none).</summary>
        public int PoseFrame { get; set; } = -1;
    }
}
