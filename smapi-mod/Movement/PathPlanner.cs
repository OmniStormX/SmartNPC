// Tile-grid TSP-ish path planner for NPC behavior actions.
//
// Each long-running tool (harvest / water / clear / till / forage / plant /
// break / fertilize) collects a set of target tiles inside a bbox or
// radius. Visiting them in raw "nearest from current position" order is
// short-sighted — the very first hop is greedy, but every subsequent hop
// inherits whatever direction that first pick happened to face. The NPC
// ends up zig-zagging across the area.
//
// This planner produces a near-optimal visit order for the open-path
// variant of TSP (start at NPC, visit every target once, no return):
//
//   1. Nearest-neighbor greedy → initial tour. O(n²) — cheap.
//   2. 2-opt local optimisation → reverse any segment whose endpoints
//      can be re-stitched with shorter total edge length. Converges in
//      a handful of passes for small n. O(passes · n²).
//
// Distance metric is Manhattan (axis-aligned tile grid). It's a strict
// underestimate of real PathFindController cost when obstacles are
// present, but the relative ordering is stable enough for the planner.
//
// Complexity at runtime: bbox mode tops out around 100 tiles; this runs
// in well under a millisecond and never blocks the game thread.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;

namespace SmartNPC.Bridge
{
    internal static class PathPlanner
    {
        /// <summary>Manhattan distance between two integer tiles.</summary>
        public static int Manhattan(Point a, Point b)
            => Math.Abs(a.X - b.X) + Math.Abs(a.Y - b.Y);

        /// <summary>
        /// Order `points` so the NPC starting at <paramref name="start"/>
        /// visits every tile once with (approximately) minimum total
        /// Manhattan travel. Returns a new list; the input is not mutated.
        /// </summary>
        public static List<Point> Plan(Point start, IList<Point> points)
            => PlanBy(start, points, p => p);

        /// <summary>
        /// Generic variant: order arbitrary work items by the position
        /// each one represents. Useful when the caller carries extra
        /// per-item metadata (item id, drop preview, etc.) alongside the
        /// tile coordinate.
        /// </summary>
        public static List<T> PlanBy<T>(Point start, IList<T> items, Func<T, Point> getPoint)
        {
            int n = items.Count;
            if (n <= 1) return new List<T>(items);

            // ── 1. Nearest-neighbor initial tour (open path) ──────────
            var pool = new List<T>(items);
            var tour = new List<T>(n);
            Point cur = start;
            while (pool.Count > 0)
            {
                int best = 0;
                int bestD = Manhattan(cur, getPoint(pool[0]));
                for (int i = 1; i < pool.Count; i++)
                {
                    int d = Manhattan(cur, getPoint(pool[i]));
                    if (d < bestD) { bestD = d; best = i; }
                }
                tour.Add(pool[best]);
                cur = getPoint(pool[best]);
                pool.RemoveAt(best);
            }

            // ── 2. 2-opt: reverse any segment whose endpoints stitch
            //    back with strictly shorter cost.
            //
            //    Open-path cost is sum of edges:
            //      start -> tour[0] -> tour[1] -> ... -> tour[n-1]
            //    A 2-opt swap on indices (i, j) with i <= j reverses the
            //    segment tour[i..j]. The only edges that change are the
            //    two boundary edges touching i and j+1. Compute the delta
            //    locally; full sum is unnecessary.
            //
            //    Cap passes to keep the planner trivially bounded at any n.
            const int maxPasses = 8;
            for (int pass = 0; pass < maxPasses; pass++)
            {
                bool improved = false;
                for (int i = 0; i < tour.Count - 1; i++)
                {
                    for (int j = i + 1; j < tour.Count; j++)
                    {
                        Point a  = i == 0 ? start : getPoint(tour[i - 1]);
                        Point b  = getPoint(tour[i]);
                        Point c  = getPoint(tour[j]);
                        bool hasD = j + 1 < tour.Count;
                        Point d  = hasD ? getPoint(tour[j + 1]) : default;

                        int before = Manhattan(a, b);
                        int after  = Manhattan(a, c);
                        if (hasD)
                        {
                            before += Manhattan(c, d);
                            after  += Manhattan(b, d);
                        }

                        if (after < before)
                        {
                            tour.Reverse(i, j - i + 1);
                            improved = true;
                        }
                    }
                }
                if (!improved) break;
            }

            return tour;
        }
    }
}
