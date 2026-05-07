// Named-location + move-intent parsing.
//
// The motivation: some OpenAI-compatible backends (notably the Hermes gateway
// we target today) don't reliably emit `tool_calls` when the user issues a
// move command. So instead of depending on the LLM to call `npc_move_to`, we
// parse the user's utterance ourselves, map it to a known location, fire the
// movement tool directly, and inject a short context line into the user
// message so the LLM can produce a natural in-character reply.
//
// Scope:
//   - Keyword-based intent detection (Chinese + English)
//   - Lookup against a named-location table (defaults to Farm landmarks,
//     extensible via Persona.NamedLocations)
//   - Returns both the matched location and an intent flag; callers decide
//     how to bridge to the MCP tool call.
//
// This is intentionally simple pattern matching. Do not grow this into a
// general-purpose NLU — if LLM tool_calls start working reliably for a given
// backend, prefer those. The auto-executor is a pragmatic fallback.

package chat

import (
	"sort"
	"strings"
)

// NamedLocation is a human-addressable point of interest on a map. The same
// location can be referenced by multiple aliases (Chinese, English,
// colloquial) so the intent parser has a fighting chance at matching
// whatever the player typed.
type NamedLocation struct {
	Name    string   `json:"name"`            // canonical display name, used in the "[你正在走向X]" hint
	Aliases []string `json:"aliases"`         // other strings that should resolve here (lowercased for matching)
	Map     string   `json:"map,omitempty"`   // SDV map name; default is "Farm"
	X       int      `json:"x"`               // tile X
	Y       int      `json:"y"`               // tile Y
}

// LocationTable is an ordered collection of named locations. Lookup scans in
// longest-alias-first order so that "房子前面" wins over plain "房".
type LocationTable struct {
	Locations []NamedLocation
}

// NewLocationTable builds a LocationTable from the given slice. Callers must
// not mutate the slice afterward; ownership passes to the table.
func NewLocationTable(locs []NamedLocation) *LocationTable {
	out := make([]NamedLocation, len(locs))
	copy(out, locs)
	return &LocationTable{Locations: out}
}

// DefaultLocations returns the Farm-map landmarks that ship with the agent.
// Tile coordinates are chosen to be broadly walkable; fine-tune per-farm via
// persona overrides when the specific layout matters.
func DefaultLocations() []NamedLocation {
	return append([]NamedLocation(nil), FarmLocations...)
}

// FarmLocations is the top-level, package-exported list of Farm landmarks.
// Kept as a var so external code (tests, tools) can append to it or diff
// against it. DefaultLocations() returns a copy so mutating the result is
// safe; mutate FarmLocations only at init time.
var FarmLocations = []NamedLocation{
	{
		Name:    "农场左边",
		Aliases: []string{"农场左边", "农场西边", "西边", "左边", "left", "west"},
		Map:     "Farm",
		X:       10,
		Y:       15,
	},
	{
		Name:    "房子前面",
		Aliases: []string{"房子前面", "房前", "房子", "家门口", "门口", "门前", "house", "door", "farmhouse"},
		Map:     "Farm",
		X:       64,
		Y:       16,
	},
	{
		Name:    "湖边",
		Aliases: []string{"湖边", "水边", "池塘", "小湖", "lake", "pond"},
		Map:     "Farm",
		X:       45,
		Y:       30,
	},
	{
		Name:    "大门",
		Aliases: []string{"大门", "入口", "农场入口", "门", "gate", "farm gate", "entrance"},
		Map:     "Farm",
		X:       79,
		Y:       15,
	},
	{
		Name:    "温室",
		Aliases: []string{"温室", "暖房", "greenhouse"},
		Map:     "Farm",
		X:       28,
		Y:       12,
	},
	{
		Name:    "畜棚",
		Aliases: []string{"畜棚", "牛棚", "barn"},
		Map:     "Farm",
		X:       77,
		Y:       10,
	},
	{
		Name:    "鸡舍",
		Aliases: []string{"鸡舍", "鸡窝", "coop"},
		Map:     "Farm",
		X:       72,
		Y:       10,
	},
	{
		Name:    "农场中心",
		Aliases: []string{"农场中心", "中心", "中间", "center", "middle"},
		Map:     "Farm",
		X:       64,
		Y:       20,
	},
}

// Move-intent keywords. The parser considers the utterance to contain a move
// intent if any of these substrings appear. Case-insensitive; Chinese
// entries match as-is.
var moveIntentKeywords = []string{
	// Chinese
	"走到", "走去", "走过去", "去到", "到", "去", "过去", "前往", "移动到",
	"跟我去", "跟我走", "带我去", "陪我去", "帮我去", "能去", "能不能到",
	"回到", "回去", "靠近", "过来",
	// English (match as lowercase substrings)
	"go to", "move to", "walk to", "head to", "come to", "come over",
	"let's go", "lets go",
}

// HasMoveIntent reports whether the user text contains any move-intent keyword.
// Matching is case-insensitive; Chinese keywords compare against the raw
// (already Unicode-safe) string.
func HasMoveIntent(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, kw := range moveIntentKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// LookupAlias returns the location whose alias is a substring of text (case
// insensitive). If multiple aliases match, the longest one wins so that
// "房子前面" beats "房" when the user said "去房子前面".
// Returns nil if no alias matches.
func (t *LocationTable) LookupAlias(text string) *NamedLocation {
	if t == nil || len(t.Locations) == 0 || text == "" {
		return nil
	}
	lower := strings.ToLower(text)

	// Build (aliasLower, *loc) pairs sorted by length desc for longest match.
	type candidate struct {
		alias string
		loc   *NamedLocation
	}
	var cands []candidate
	for i := range t.Locations {
		loc := &t.Locations[i]
		for _, a := range loc.Aliases {
			if a == "" {
				continue
			}
			cands = append(cands, candidate{strings.ToLower(a), loc})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return len([]rune(cands[i].alias)) > len([]rune(cands[j].alias))
	})
	for _, c := range cands {
		if strings.Contains(lower, c.alias) {
			return c.loc
		}
	}
	return nil
}

// MoveIntent is the parsed result of inspecting a user message.
type MoveIntent struct {
	HasIntent bool           // true if a move keyword was detected
	Location  *NamedLocation // non-nil when a named destination was resolved
	Raw       string         // the original user text (trimmed)
}

// DetectMoveIntent is the package-level convenience that scans against
// FarmLocations. Returns (hasIntent, location); location is nil when the
// move keyword fires but no known landmark matched. Callers that want to
// use a custom table (e.g. persona overrides) should go through
// (*LocationTable).DetectMoveIntent.
func DetectMoveIntent(text string) (bool, *NamedLocation) {
	if !HasMoveIntent(text) {
		return false, nil
	}
	tbl := NewLocationTable(FarmLocations)
	return true, tbl.LookupAlias(text)
}

// ── behavior intent ────────────────────────────────────────────
//
// Behavior intents map a free-form user utterance to one of four high-level
// NPC behavior verbs handled by the mod's FollowSystem (summon / follow /
// lead / stop). They're complementary to move-intent: move-intent targets a
// specific tile, behavior-intent changes the NPC's *mode* of motion.
//
// Resolution order in the agent:
//   1. If a "stop" keyword fires, emit intent="stop" — takes precedence over
//      everything else so "别跟了" does not accidentally re-trigger a follow.
//   2. Else if a "lead" keyword fires AND a landmark resolves, emit "lead"
//      (with the matched location). A lead without a destination falls back
//      to plain follow so the player isn't left with a no-op.
//   3. Else if a "follow" keyword fires, emit "follow".
//   4. Else if a "summon" keyword fires, emit "summon".
//   5. Else empty intent.
//
// Matching is case-insensitive; Chinese keywords compare as-is.

var (
	summonKeywords = []string{
		// Chinese
		"过来", "来这", "到这来", "到我这", "到这里来", "来我这",
		// English
		"come here", "come over here", "come to me",
	}
	followKeywords = []string{
		// Chinese
		"跟着我", "跟我走", "跟上我", "跟上来", "一起走",
		// English
		"follow me", "come with me", "come along",
	}
	leadKeywords = []string{
		// Chinese
		"带路", "带我去", "带我到", "领我去", "领路", "你带我",
		// English
		"lead the way", "lead me to", "show me the way",
	}
	stopKeywords = []string{
		// Chinese
		"别跟了", "别跟着我", "不要跟了", "不用跟了", "停下", "停下来",
		"别跟着", "不跟了",
		// English
		"stop following", "don't follow", "do not follow", "stop",
	}
)

// containsAny reports whether lowered text contains any of the keywords.
// Keywords are expected to already be lowercase / Chinese literal.
func containsAny(lowered string, kws []string) bool {
	for _, kw := range kws {
		if kw != "" && strings.Contains(lowered, kw) {
			return true
		}
	}
	return false
}

// DetectBehaviorIntent classifies an utterance into one of four behavior
// verbs: "summon", "follow", "lead", "stop", or "" when nothing matches.
//
// For "lead" the returned *NamedLocation is the resolved destination (may be
// nil if the player said "lead the way" without naming anywhere); for other
// intents the location is always nil — behavior tools operate on the NPC's
// mode, not a specific tile.
//
// The lookup table for lead destinations is the package-level FarmLocations
// set; callers that need persona overrides should call
// (*LocationTable).DetectBehaviorIntent instead.
func DetectBehaviorIntent(text string) (string, *NamedLocation) {
	tbl := NewLocationTable(FarmLocations)
	return tbl.DetectBehaviorIntent(text)
}

// DetectBehaviorIntent is the table-scoped variant used by the agent when a
// persona supplies its own named-location list. See DetectBehaviorIntent for
// the classification rules.
func (t *LocationTable) DetectBehaviorIntent(text string) (string, *NamedLocation) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	lower := strings.ToLower(trimmed)

	// 1. Stop wins outright.
	if containsAny(lower, stopKeywords) {
		return "stop", nil
	}
	// 2. Lead (with optional destination).
	if containsAny(lower, leadKeywords) {
		return "lead", t.LookupAlias(trimmed)
	}
	// 3. Follow.
	if containsAny(lower, followKeywords) {
		return "follow", nil
	}
	// 4. Summon.
	if containsAny(lower, summonKeywords) {
		return "summon", nil
	}
	return "", nil
}

// DetectMoveIntent inspects user text for a move keyword plus an optional
// named destination. It returns:
//
//   - HasIntent=false when no move keyword was found (the caller should
//     treat the message as a normal chat turn).
//   - HasIntent=true and Location=nil when the keyword fired but no known
//     landmark alias appeared — caller should ask the LLM to clarify.
//   - HasIntent=true and Location!=nil when both fired — caller should execute
//     `npc_move_to` and inject the movement context into the LLM prompt.
func (t *LocationTable) DetectMoveIntent(text string) MoveIntent {
	out := MoveIntent{Raw: strings.TrimSpace(text)}
	if out.Raw == "" {
		return out
	}
	if !HasMoveIntent(out.Raw) {
		return out
	}
	out.HasIntent = true
	out.Location = t.LookupAlias(out.Raw)
	return out
}
