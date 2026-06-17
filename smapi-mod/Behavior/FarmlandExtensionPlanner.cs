// Plans rectangular farmland extensions that hug existing tilled soil.
//
// Goal: when an NPC tills new ground, produce a regular W×H rectangle
// glued to (or adjacent to) the existing farmland silhouette — instead
// of tilling every loose Diggable tile inside the agent's bbox. The
// player sees the field grow as a sequence of clean rectangles, even
// when the existing layout is irregular.
//
// The existing farmland is treated as an arbitrary set of HoeDirt tiles
// (4-connected components, possibly with holes from machines / paths /
// obstacles). We do NOT require the existing field to be a rectangle.
// Only the NEW patch is forced into a regular rectangular shape.
//
// Algorithm:
//   1. Build candidate set: every tile inside the agent's bbox that is
//      currently tillable (passable, Diggable=T, no Object, no terrain).
//   2. Find all existing HoeDirt tiles and group via 4-connected flood
//      fill into connected components (only used for adjacency tests).
//   3. Cold start (no components): place the patch near the NPC's tile
//      so the field has a seed to grow from.
//   4. Warm start: for every (anchor, orientation) pair where the patch
//      fits the candidate set AND is contained in the bbox, score by
//        score = 10·adjacencyLen + 200·sharedEdgeFraction − 0.5·distToNPC
//      Take the highest-scoring rectangle. Try both (W,H) and (H,W)
//      orientations and pick the better one.
//   5. Return null if no rectangle fits — the handler converts that
//      into a MarkNothingToDo response.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using StardewValley;
using StardewValley.TerrainFeatures;

namespace SmartNPC.Bridge
{
    internal static class FarmlandExtensionPlanner
    {
        public readonly struct Plan
        {
            public Rectangle Rect { get; }   // inclusive of (X+Width-1, Y+Height-1)
            public bool AdjacentToExisting { get; }
            public int AdjacencyLen { get; }

            public Plan(Rectangle rect, bool adjacent, int adjacencyLen)
            {
                Rect = rect;
                AdjacentToExisting = adjacent;
                AdjacencyLen = adjacencyLen;
            }
        }

        /// <summary>
        /// Find a regular W×H tilling patch inside <paramref name="bbox"/>
        /// (inclusive on all sides) that prefers to share an edge with
        /// existing HoeDirt. Tries both (W,H) and (H,W) orientations.
        /// </summary>
        /// <param name="location">map to search</param>
        /// <param name="bbox">agent-supplied tile rectangle (inclusive)</param>
        /// <param name="npcTile">NPC current position, used as a tiebreaker</param>
        /// <param name="patchW">preferred patch width (e.g. 10)</param>
        /// <param name="patchH">preferred patch height (e.g. 6)</param>
        /// <returns>best plan, or null if no W×H fits</returns>
        public static Plan? Plan_(GameLocation location, Rectangle bbox,
            Point npcTile, int patchW, int patchH)
        {
            if (location is null) return null;
            if (patchW <= 0 || patchH <= 0) return null;

            // 1. Candidate set: tillable tiles inside bbox.
            var candidates = new HashSet<Point>();
            for (int tx = bbox.Left; tx <= bbox.Right; tx++)
            {
                for (int ty = bbox.Top; ty <= bbox.Bottom; ty++)
                {
                    if (IsTillable(location, tx, ty))
                        candidates.Add(new Point(tx, ty));
                }
            }
            if (candidates.Count == 0) return null;

            // 2. Existing HoeDirt set (no flood fill needed — we only test
            //    membership, not topology, for adjacency scoring).
            var existing = new HashSet<Point>();
            foreach (var kv in location.terrainFeatures.Pairs)
            {
                if (kv.Value is HoeDirt)
                    existing.Add(new Point((int)kv.Key.X, (int)kv.Key.Y));
            }

            // 3 + 4. Try both orientations. If patchW == patchH only one
            // orientation matters (skip the duplicate to halve work).
            Plan? best = TryBest(bbox, candidates, existing, npcTile, patchW, patchH);
            if (patchW != patchH)
            {
                Plan? alt = TryBest(bbox, candidates, existing, npcTile, patchH, patchW);
                if (alt.HasValue && (!best.HasValue || ScoreOf(alt.Value, npcTile) > ScoreOf(best.Value, npcTile)))
                    best = alt;
            }
            return best;
        }

        // ── internals ────────────────────────────────────────────────────

        private static Plan? TryBest(Rectangle bbox, HashSet<Point> candidates,
            HashSet<Point> existing, Point npcTile, int W, int H)
        {
            if (W > bbox.Width + 1 || H > bbox.Height + 1) return null;

            Plan? best = null;
            double bestScore = double.NegativeInfinity;

            // Slide a W×H window across bbox; the bottom-right corner is
            // (x+W-1, y+H-1) and must stay inside bbox.
            for (int x = bbox.Left; x + W - 1 <= bbox.Right; x++)
            {
                for (int y = bbox.Top; y + H - 1 <= bbox.Bottom; y++)
                {
                    // Every tile in the patch must be tillable. We allow
                    // ZERO existing-HoeDirt overlap by design — we want to
                    // PRODUCE new field, not reissue till on existing soil.
                    if (!AllTillable(candidates, x, y, W, H)) continue;

                    int adj = AdjacencyLength(existing, x, y, W, H);
                    var rect = new Rectangle(x, y, W, H);
                    bool adjacent = adj > 0;
                    var plan = new Plan(rect, adjacent, adj);
                    double s = ScoreOf(plan, npcTile);
                    if (s > bestScore)
                    {
                        bestScore = s;
                        best = plan;
                    }
                }
            }

            // Cold-start fallback: no existing field anywhere on the map.
            // Prefer the candidate window closest to the NPC. The earlier
            // loop already covers it (adj will be 0 everywhere); the score
            // collapses to −0.5·distToNPC and the closest window wins.
            if (best.HasValue && existing.Count == 0)
            {
                // Mark `adjacent=false` regardless of adj==0 random hits.
                var b = best.Value;
                best = new Plan(b.Rect, adjacent: false, adjacencyLen: 0);
            }

            return best;
        }

        private static bool AllTillable(HashSet<Point> candidates, int x, int y, int W, int H)
        {
            for (int dx = 0; dx < W; dx++)
            for (int dy = 0; dy < H; dy++)
            {
                if (!candidates.Contains(new Point(x + dx, y + dy)))
                    return false;
            }
            return true;
        }

        // 4-neighborhood length: count of patch perimeter tiles whose
        // outward neighbor is an existing HoeDirt. Caps at 2*(W+H).
        // Bigger = the new rectangle hugs the old field along a long edge.
        private static int AdjacencyLength(HashSet<Point> existing, int x, int y, int W, int H)
        {
            if (existing.Count == 0) return 0;
            int adj = 0;

            // Top edge: each tile (x+dx, y) checks (x+dx, y-1).
            for (int dx = 0; dx < W; dx++)
            {
                if (existing.Contains(new Point(x + dx, y - 1))) adj++;
                if (existing.Contains(new Point(x + dx, y + H))) adj++;
            }
            // Left/right edges.
            for (int dy = 0; dy < H; dy++)
            {
                if (existing.Contains(new Point(x - 1,     y + dy))) adj++;
                if (existing.Contains(new Point(x + W,     y + dy))) adj++;
            }
            return adj;
        }

        // Distance from patch center to the NPC.
        private static double DistToNpc(Rectangle r, Point npc)
        {
            double cx = r.X + r.Width  * 0.5 - 0.5;
            double cy = r.Y + r.Height * 0.5 - 0.5;
            return Math.Abs(cx - npc.X) + Math.Abs(cy - npc.Y);
        }

        private static double ScoreOf(Plan p, Point npc)
        {
            int perim = 2 * (p.Rect.Width + p.Rect.Height);
            double sharedFrac = perim > 0 ? (double)p.AdjacencyLen / perim : 0;
            // Weights: a fully-shared edge dominates distance; distance is
            // a tiebreaker among similarly-adjacent positions. A cold-start
            // patch (adj=0) collapses to "closest to NPC".
            return 10.0 * p.AdjacencyLen
                 + 200.0 * sharedFrac
                 - 0.5 * DistToNpc(p.Rect, npc);
        }

        /// <summary>
        /// Mirrors TillSoilHandler.Execute's per-tile precondition, with one
        /// relaxation: tiles that hold clearable debris (weeds, twigs, small stones,
        /// tree stumps, saplings) are allowed — the debris will be removed before
        /// tilling. Non-debris objects (chests, machines, fences) and non-clearable
        /// terrain features (mature trees, bushes, flooring) still block.
        /// </summary>
        private static bool IsTillable(GameLocation location, int tx, int ty)
        {
            var v = new Microsoft.Xna.Framework.Vector2(tx, ty);

            // Objects: only block if NOT clearable debris.
            if (location.Objects.TryGetValue(v, out var obj) && obj != null)
            {
                if (!ClearDebrisHandler.IsDebris(obj)) return false;
            }

            // TerrainFeatures: only block if NOT clearable debris.
            if (location.terrainFeatures.TryGetValue(v, out var tf) && tf != null)
            {
                if (!ClearDebrisHandler.IsTerrainDebris(tf)) return false;
            }

            if (!location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)) return false;
            if (location.doesTileHaveProperty(tx, ty, "Diggable", "Back") != "T") return false;
            return true;
        }
    }
}
