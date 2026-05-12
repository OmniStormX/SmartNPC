// Serializes outgoing chat_say's so multiple concurrently-active NPC
// profiles can't talk over each other. Each entry represents one NPC's
// pending speech act; the queue releases them on a minimum-gap timer so
// adjacent lines from different NPCs read like a real conversation
// instead of a stream-of-consciousness blast.
//
// Status: SCAFFOLDING, not yet wired into any consumer. Reserved for
// when multiple Hermes profiles are concurrently active (e.g. XiaMi +
// Abigail both reacting to a `day_started` broadcast). A future
// milestone will drain it from the game-thread tick loop.
//
// Design notes:
//   - Pure in-memory FIFO with a per-queue "last released at" timestamp.
//   - `Enqueue` returns immediately; the (future) consumer drains by
//     calling `TryDequeueReady` on each tick.
//   - Min gap defaults to 800ms — short enough to feel responsive,
//     long enough to read like turn-taking.
//   - Thread-safe: every public method takes the same internal lock so
//     the ws receive loop (producer) and the future game-thread consumer
//     can call into it without external synchronization.

using System;
using System.Collections.Generic;

namespace SmartNPC.Bridge
{
    internal sealed class PendingTurn
    {
        public string Speaker = string.Empty;       // NPC display name
        public string Text = string.Empty;
        public string Color = "yellow";
        public DateTime EnqueuedAt = DateTime.UtcNow;
    }

    internal sealed class TurnQueue
    {
        private readonly Queue<PendingTurn> _queue = new();
        private DateTime _lastReleased = DateTime.MinValue;
        private readonly object _gate = new();

        /// <summary>Minimum gap between successive releases.</summary>
        public TimeSpan MinGap { get; set; } = TimeSpan.FromMilliseconds(800);

        /// <summary>Maximum queue depth before old entries are dropped.</summary>
        public int Capacity { get; set; } = 32;

        public int Count
        {
            get { lock (_gate) return _queue.Count; }
        }

        /// <summary>Push a pending turn. Drops the oldest if at capacity.</summary>
        public void Enqueue(PendingTurn turn)
        {
            if (turn is null) return;
            lock (_gate)
            {
                while (_queue.Count >= Capacity)
                    _queue.Dequeue();
                _queue.Enqueue(turn);
            }
        }

        /// <summary>
        /// Pop the next turn IF the min-gap timer has elapsed since the last
        /// release. Returns <c>null</c> when the queue is empty or it's too
        /// soon to speak again.
        /// </summary>
        public PendingTurn? TryDequeueReady()
        {
            lock (_gate)
            {
                if (_queue.Count == 0) return null;
                if (DateTime.UtcNow - _lastReleased < MinGap) return null;

                var t = _queue.Dequeue();
                _lastReleased = DateTime.UtcNow;
                return t;
            }
        }

        /// <summary>Drop all pending turns (e.g. on save reload).</summary>
        public void Clear()
        {
            lock (_gate)
            {
                _queue.Clear();
                _lastReleased = DateTime.MinValue;
            }
        }
    }
}
