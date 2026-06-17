// Base class for NPC behavior action handlers.
//
// Provides the common scaffolding: parse params.npc, find the NPC on the
// game thread, show a head bubble, call the virtual Execute method, and
// return a structured response. Subclasses only override Execute to add
// real game logic when ready.
//
// Concurrency model
// -----------------
// Each NPC has a single FollowSystem mode at any time. Handlers fall into
// three execution classes:
//
//   1. Trivial (default, RefuseWhileBusy=false):
//      Executes immediately on the next game tick. No queue, no FollowSystem
//      mode change. Examples: chat_say, emote, inspect_object, idle_activity.
//
//   2. Preemptable mode (RefuseWhileBusy=true, IsPreemptable=true):
//      Currently only `npc_wander`. Treated like a serial action when
//      starting (so two wanders don't fight), BUT a higher-priority queued
//      task may cancel it via FollowSystem.Stop and run immediately.
//
//   3. Serial / exclusive (RefuseWhileBusy=true, IsPreemptable=false):
//      Long-running work (harvest/water/clear/till/forage/plant/fertilize/
//      break/deposit/deliver/withdraw/approach_and_speak). Must run to
//      completion before the next exclusive action on the same NPC starts.
//      If the NPC is currently busy with another exclusive action, the new
//      task is APPENDED to a per-NPC queue and the agent receives a
//      structured `queued=true` ack. The queue auto-drains in
//      PumpOnGameTick when FollowSystem.GetMode() returns Idle (or, for
//      preemptable modes, after we forcibly stop them).

using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Text.Json;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal abstract class NpcActionHandlerBase
    {
        protected readonly IMonitor Log;
        private readonly Func<bool> _showBubble;
        private readonly ConcurrentQueue<Action> _pending = new();

        // 配置 showbubble 配置
        protected NpcActionHandlerBase(IMonitor log, Func<bool>? showBubble = null)
        {
            Log = log;
            _showBubble = showBubble ?? (() => true);
        }

        /// <summary>The ws action name (e.g. "npc_wander").</summary>
        protected abstract string ActionName { get; }

        /// <summary>Public accessor for registration.</summary>
        public string ActionNamePublic => ActionName;

        /// <summary>
        /// True for any handler that needs the "single long-running mode at a time"
        /// guarantee. Default false: trivial actions (chat_say, emote, bubble,
        /// inspect_object, ...) ignore queueing entirely.
        /// </summary>
        protected virtual bool RefuseWhileBusy => false;

        /// <summary>
        /// True if this handler's mode can be safely cancelled by a newer
        /// queued exclusive action. Currently only npc_wander overrides
        /// this — wandering is filler behavior, real work supersedes it.
        /// </summary>
        protected virtual bool IsPreemptable => false;

        private FollowSystem? _follow;

        /// <summary>
        /// Wire the FollowSystem so the queue dispatcher can read modes
        /// and force-stop preemptable ones. Called by long-running
        /// subclass constructors (was previously called SetBusyGate).
        /// </summary>
        protected void SetBusyGate(FollowSystem follow) => _follow = follow;

        /// <summary>
        /// Override to implement real game logic. Called on the game thread
        /// AFTER the bubble has been shown. The NPC is guaranteed non-null
        /// and the save is loaded.
        /// </summary>
        protected virtual void Execute(NPC npc, string npcName, JsonElement @params)
        {
            // Default: no-op (bubble-only). Override in subclass.
        }

        /// <summary>
        /// Override to return custom response data instead of the default ack.
        /// Called immediately after Execute on the game thread.
        /// </summary>
        protected virtual object? GetResult(NPC npc, string npcName, JsonElement @params)
            => null;

        // Set by Execute via MarkNothingToDo when it finds no targets in
        // the requested area. The base ack-builder reads this on the same
        // game-thread tick and converts the default success ack into a
        // structured "nothing_to_do" payload so the agent can re-plan
        // instead of treating the call as completed work.
        //
        // Per-thunk lifetime: cleared at the start of every Handle()
        // invocation. The pending-task pump runs serially on the game
        // thread (one thunk at a time), so a plain instance field is fine
        // — but we still reset before AND after Execute to be safe against
        // re-entry from custom subclass code paths.
        private string? _nothingReason;

        /// <summary>
        /// Subclasses call this from Execute when their preflight scan finds
        /// no actionable targets in the agent-supplied region. The base
        /// class then returns a structured ack with `nothing_to_do=true`
        /// and a human-readable hint, instead of the generic success
        /// message — that way the agent re-evaluates on the same turn
        /// rather than ticking off "task done".
        /// </summary>
        protected void MarkNothingToDo(string reason)
        {
            _nothingReason = reason;
        }

        /// <summary>
        /// Resolve the bubble text shown above the NPC's head.
        /// Default: "[short_name] reason/text" or just "[short_name]".
        /// Override for actions like npc_show_text_bubble that use params.text directly.
        /// </summary>
        protected virtual string ResolveBubble(JsonElement @params)
        {
            string shortName = ActionName.StartsWith("npc_", StringComparison.Ordinal)
                ? ActionName.Substring(4)
                : ActionName;

            string? extra = null;
            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("reason", out JsonElement reasonEl) &&
                    reasonEl.ValueKind == JsonValueKind.String)
                    extra = reasonEl.GetString();
                else if (@params.TryGetProperty("text", out JsonElement txtEl) &&
                         txtEl.ValueKind == JsonValueKind.String)
                    extra = txtEl.GetString();
            }

            if (!string.IsNullOrEmpty(extra))
            {
                if (extra!.Length > 40)
                    extra = extra.Substring(0, 37) + "...";
                return $"[{shortName}] {extra}";
            }
            return $"[{shortName}]";
        }

        /// <summary>
        /// The RequestHandler delegate to register with the router.
        /// Usage: _router.Register("npc_wander", handler.Handle);
        /// </summary>
        public Task<Response> Handle(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            string? npcName = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("npc", out JsonElement npcEl) &&
                npcEl.ValueKind == JsonValueKind.String)
            {
                npcName = npcEl.GetString();
            }

            if (string.IsNullOrWhiteSpace(npcName))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            string bubble = ResolveBubble(@params);
            var tcs = new TaskCompletionSource<Response>();
            string captured = npcName;
            JsonElement capturedParams = @params.Clone();

            // Build the deferred game-thread thunk. Used both for immediate
            // execution AND queued execution — same code path either way so
            // bubble timing, error handling, and result shape stay aligned.
            Action runOnGameThread = () =>
            {
                // Reset the no-op marker before invoking subclass code.
                // The pump runs thunks serially on the game thread, so a
                // plain field is enough; reset is defense-in-depth in case
                // a previous thunk threw before we cleared it.
                _nothingReason = null;
                try
                {
                    NPC? npc = Game1.getCharacterFromName(captured);
                    if (npc is null)
                    {
                        Log.Log($"[{ActionName}] NPC '{captured}' not found", LogLevel.Warn);
                        tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{captured}'"));
                        return;
                    }

                    if (_showBubble())
                    {
                        npc.showTextAboveHead(bubble);

                        // Register persistent bubble for long-running actions.
                        // FollowSystem.PumpOnGameTick will refresh it every ~1s
                        // with an elapsed-seconds counter, and clear it on Idle.
                        if (RefuseWhileBusy && _follow != null)
                            _follow.SetActionBubble(captured, bubble);

                        string npcMap = npc.currentLocation?.Name ?? "<null>";
                        string playerMap = Game1.player?.currentLocation?.Name ?? "<null>";
                        bool sameMap = string.Equals(npcMap, playerMap, StringComparison.Ordinal);
                        Log.Log($"[{ActionName}] npc={captured} bubble=\"{bubble}\" sameMap={sameMap}", LogLevel.Debug);

                        if (!sameMap)
                            Log.Log($"[{ActionName}] {captured} on '{npcMap}' but player on '{playerMap}' — not visible", LogLevel.Warn);
                    }

                    Execute(npc, captured, capturedParams);
                    object? custom = GetResult(npc, captured, capturedParams);

                    // If Execute marked the call as a no-op via
                    // MarkNothingToDo, surface that to the agent so it
                    // can pick a different action instead of charging on
                    // as if work was done.
                    string? noop = _nothingReason;
                    _nothingReason = null;

                    if (custom != null)
                    {
                        tcs.TrySetResult(Response.Success(id, custom));
                    }
                    else if (noop != null)
                    {
                        tcs.TrySetResult(Response.Success(id, new
                        {
                            ok             = true,
                            npc            = captured,
                            action         = ActionName,
                            nothing_to_do  = true,
                            reason         = noop,
                            message        = $"{ActionName}: {noop}. The action did " +
                                             "nothing — re-evaluate on this same turn " +
                                             "and pick a different tool (e.g. inspect " +
                                             "a wider area, switch to harvest/forage/" +
                                             "till/plant, or run a workflow).",
                        }));
                    }
                    else
                    {
                        tcs.TrySetResult(Response.Success(id, new
                        {
                            ok = true,
                            npc = captured,
                            action = ActionName,
                            message = $"{ActionName} acknowledged",
                        }));
                    }
                }
                catch (Exception ex)
                {
                    Log.Log($"[{ActionName}] error: {ex.Message}", LogLevel.Error);
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            };

            // Routing decision: trivial actions, queue-eligible actions, or
            // queued-now-run-later. Trivial path bypasses the queue entirely
            // so chat_say / emote / inspect / etc. never wait on harvest.
            if (!RefuseWhileBusy || _follow == null)
            {
                _pending.Enqueue(runOnGameThread);
                return tcs.Task;
            }

            NpcBehaviorMode currentMode = _follow.GetMode(captured);

            if (currentMode == NpcBehaviorMode.Idle)
            {
                // NPC is free — run immediately.
                _pending.Enqueue(runOnGameThread);
                return tcs.Task;
            }

            // NPC is busy. If the in-flight mode is preemptable (wander),
            // cancel it and run the new task right now. Otherwise, append
            // to that NPC's serial queue and ack with queued=true so the
            // agent knows it's pending.
            if (NpcActionQueue.IsPreemptableMode(currentMode))
            {
                Log.Log(
                    $"[{ActionName}] {captured} preempting {currentMode} for higher-priority task",
                    LogLevel.Info);
                _follow.Stop(captured);
                _pending.Enqueue(runOnGameThread);
                return tcs.Task;
            }

            // Serial enqueue: defer until the current exclusive action
            // finishes (FollowSystem returns to Idle), then this thunk
            // runs via PumpOnGameTick → DrainReadyTasks.
            int position = NpcActionQueue.Enqueue(captured, runOnGameThread, ActionName);
            Log.Log(
                $"[{ActionName}] {captured} busy with {currentMode}; queued at position {position}",
                LogLevel.Info);
            tcs.TrySetResult(Response.Success(id, new
            {
                ok       = true,
                npc      = captured,
                action   = ActionName,
                queued   = true,
                position,
                message  = $"{captured} is currently {currentMode}; {ActionName} queued at position {position} and will run when current task finishes.",
            }));
            return tcs.Task;
        }

        /// <summary>
        /// Drain queued game-thread work for THIS handler. Called from
        /// OnUpdateTicked. Per-NPC serial queue is drained separately by
        /// NpcActionQueue.DrainReadyTasks (called once per tick from
        /// ModEntry).
        /// </summary>
        public void PumpOnGameTick()
        {
            while (_pending.TryDequeue(out Action? action))
                action();
        }
    }

    /// <summary>
    /// Per-NPC FIFO of serial (exclusive) actions waiting for the current
    /// FollowSystem mode to release. Shared across every long-running
    /// handler instance because exclusivity is a property of the NPC,
    /// not of the tool — harvest must wait on water from a different
    /// handler instance.
    /// </summary>
    internal static class NpcActionQueue
    {
        // Per-NPC FIFO (npcName → pending tasks). Locked on the dictionary
        // for both reads and writes; queues are short and contention is
        // negligible compared to game-tick cost.
        private static readonly object _lock = new();
        private static readonly Dictionary<string, Queue<PendingTask>> _queues = new();

        // Used by ModEntry once at startup to share the FollowSystem with
        // the dispatcher. Without it, DrainReadyTasks has no way to test
        // whether a queue head is allowed to start yet.
        private static FollowSystem? _follow;

        public static void Configure(FollowSystem follow) => _follow = follow;

        public static bool IsPreemptableMode(NpcBehaviorMode mode)
            // Only Wander is preemptable today. Following / Leading are
            // intentionally NOT preemptable: the player initiated those.
            => mode == NpcBehaviorMode.Wander;

        /// <summary>
        /// Append a deferred task to the NPC's serial queue. Returns the
        /// 1-based position (1 = will run next when current task ends).
        /// </summary>
        public static int Enqueue(string npcName, Action runOnGameThread, string actionName)
        {
            lock (_lock)
            {
                if (!_queues.TryGetValue(npcName, out var q))
                {
                    q = new Queue<PendingTask>();
                    _queues[npcName] = q;
                }
                q.Enqueue(new PendingTask(actionName, runOnGameThread));
                return q.Count;
            }
        }

        /// <summary>
        /// Per-tick dispatcher: for every NPC with a non-empty queue,
        /// check whether FollowSystem is Idle. If so, dequeue and run one
        /// task. Only runs ONE per NPC per tick — back-to-back tasks
        /// usually need a tick of game state mutation to settle anyway.
        /// </summary>
        public static void DrainReadyTasks(IMonitor? log = null)
        {
            if (_follow == null) return;

            // Snapshot under lock so handler threads can keep enqueuing
            // safely; the actual run happens outside the lock.
            List<(string npc, PendingTask task)> ready = new();
            lock (_lock)
            {
                foreach (var kv in _queues)
                {
                    if (kv.Value.Count == 0) continue;
                    if (_follow.GetMode(kv.Key) != NpcBehaviorMode.Idle) continue;
                    ready.Add((kv.Key, kv.Value.Dequeue()));
                }
            }

            foreach (var (npc, task) in ready)
            {
                log?.Log(
                    $"[NpcActionQueue] {npc}: dispatching queued {task.ActionName}",
                    LogLevel.Debug);
                try { task.Run(); }
                catch (Exception ex)
                {
                    log?.Log(
                        $"[NpcActionQueue] {npc}: queued {task.ActionName} threw: {ex.Message}",
                        LogLevel.Warn);
                }
            }
        }

        /// <summary>Discard all queued tasks for a single NPC. Called by
        /// FollowSystem.ForceIdle when an action is cancelled by timeout.</summary>
        public static void Clear(string npcName)
        {
            lock (_lock) { _queues.Remove(npcName); }
        }

        /// <summary>Diagnostic snapshot for debug commands.</summary>
        public static IReadOnlyDictionary<string, IReadOnlyList<string>> Snapshot()
        {
            lock (_lock)
            {
                var copy = new Dictionary<string, IReadOnlyList<string>>(_queues.Count);
                foreach (var kv in _queues)
                {
                    var actions = new List<string>(kv.Value.Count);
                    foreach (var t in kv.Value) actions.Add(t.ActionName);
                    copy[kv.Key] = actions;
                }
                return copy;
            }
        }

        private readonly struct PendingTask
        {
            public string ActionName { get; }
            private readonly Action _run;
            public PendingTask(string actionName, Action run)
            {
                ActionName = actionName;
                _run       = run;
            }
            public void Run() => _run();
        }
    }
}
