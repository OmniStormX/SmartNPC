package memory

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolHandler is the signature shared by memory_recall and memory_store.
// It accepts the JSON-decoded arguments map an LLM emits via tool_calls
// and returns a stringified JSON result that can be threaded back as a
// `role: tool` message. Implementations never error on bad input — they
// return a JSON envelope with `ok: false` so the LLM can read the failure.
type ToolHandler func(args map[string]any) string

// ToolSpec is the minimal description an Agent needs to advertise the tool
// to its LLM. Keeping it provider-agnostic (no llm import) avoids a
// circular dependency between this package and internal/llm.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// MemoryToolset bundles the recall + store tools plus their pre-bound
// handlers. The chat agent merges these specs into the LLM tool catalogue
// and dispatches matching tool_calls to the appropriate handler before it
// would forward an unknown name to the MCP session.
type MemoryToolset struct {
	NPCName  string
	RecallSpec ToolSpec
	StoreSpec  ToolSpec
	Recall   ToolHandler
	Store    ToolHandler
}

// NewToolset returns the four-piece bundle bound to a specific NPC and
// Store. The returned handlers are safe for concurrent use.
//
// memory_recall(query?, category?, limit?) → JSON {ok, memories:[...]}
// memory_store(content, category, importance?) → JSON {ok, id, error?}
func NewToolset(store Store, npcName string) (*MemoryToolset, error) {
	if store == nil {
		return nil, fmt.Errorf("memory.NewToolset: store is required")
	}
	if npcName == "" {
		return nil, fmt.Errorf("memory.NewToolset: npcName is required")
	}
	return &MemoryToolset{
		NPCName: npcName,
		RecallSpec: ToolSpec{
			Name: "memory_recall",
			Description: "Search this NPC's long-term memory. " +
				"Use it BEFORE answering questions like \"do you remember...\" or " +
				"when grounding a reply in something the player told you previously.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Substring to search for inside memory content (case-insensitive). Optional.",
					},
					"category": map[string]any{
						"type": "string",
						"enum": []any{
							CategoryFact, CategoryPreference,
							CategoryEvent, CategoryRelationship, CategoryPromise,
						},
						"description": "Restrict to a single memory category. Optional.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of memories to return (default 5).",
					},
				},
			},
		},
		StoreSpec: ToolSpec{
			Name: "memory_store",
			Description: "Persist a new long-term memory for this NPC. Call when the player " +
				"tells you something worth remembering across conversations.",
			InputSchema: map[string]any{
				"type": "object",
				"required": []any{"content", "category"},
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "A short, concrete sentence describing the memory.",
					},
					"category": map[string]any{
						"type": "string",
						"enum": []any{
							CategoryFact, CategoryPreference,
							CategoryEvent, CategoryRelationship, CategoryPromise,
						},
					},
					"importance": map[string]any{
						"type":        "integer",
						"description": "1-10 ranking. Defaults to 5 when omitted.",
					},
				},
			},
		},
		Recall: makeRecallHandler(store, npcName),
		Store:  makeStoreHandler(store, npcName),
	}, nil
}

// makeRecallHandler wires a closure that performs the actual lookup. Kept as
// a constructor so we never ship a Toolset whose handlers were not bound to
// a specific NPC.
func makeRecallHandler(store Store, npcName string) ToolHandler {
	return func(args map[string]any) string {
		opts := MemoryQuery{Limit: 5}
		if v, ok := stringArg(args, "query"); ok {
			opts.Search = v
		}
		if v, ok := stringArg(args, "category"); ok {
			cat := strings.ToLower(strings.TrimSpace(v))
			if isValidCategory(cat) {
				opts.Categories = []string{cat}
			}
		}
		if v, ok := intArg(args, "limit"); ok && v > 0 && v <= 50 {
			opts.Limit = v
		}
		mems, err := store.GetMemories(npcName, opts)
		if err != nil {
			return jsonErr(err)
		}
		// Bump access counts so frequently-recalled rows surface higher
		// next time. Best-effort; failures are silent.
		for _, m := range mems {
			_ = store.TouchMemory(m.ID)
		}
		// Strip noisy fields the LLM doesn't need.
		out := struct {
			OK       bool      `json:"ok"`
			Memories []Memory  `json:"memories"`
			Count    int       `json:"count"`
		}{OK: true, Memories: mems, Count: len(mems)}
		b, _ := json.Marshal(out)
		return string(b)
	}
}

func makeStoreHandler(store Store, npcName string) ToolHandler {
	return func(args map[string]any) string {
		content, _ := stringArg(args, "content")
		category, _ := stringArg(args, "category")
		content = strings.TrimSpace(content)
		category = strings.ToLower(strings.TrimSpace(category))
		if content == "" {
			return jsonErrf("content is required")
		}
		if !isValidCategory(category) {
			return jsonErrf("invalid category %q (allowed: %s)", category, AllCategoriesAsList())
		}
		importance := 5
		if v, ok := intArg(args, "importance"); ok {
			importance = clamp(v, 1, 10)
		}
		m := Memory{
			NPCName:    npcName,
			Category:   category,
			Content:    content,
			Importance: importance,
		}
		if err := store.StoreMemory(m); err != nil {
			return jsonErr(err)
		}
		// We don't get the new id back from the interface today; surface
		// success without it. Callers that need the id can read GetMemories.
		out := struct {
			OK         bool   `json:"ok"`
			Category   string `json:"category"`
			Importance int    `json:"importance"`
		}{OK: true, Category: category, Importance: importance}
		b, _ := json.Marshal(out)
		return string(b)
	}
}

// --- helpers ---

func stringArg(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	v, ok := args[key]
	if !ok {
		return "", false
	}
	switch s := v.(type) {
	case string:
		return s, true
	case fmt.Stringer:
		return s.String(), true
	}
	return "", false
}

func intArg(args map[string]any, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		// JSON numbers decode as float64 by default; round toward zero.
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	case string:
		// Tolerate models that quote numbers.
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

func jsonErr(err error) string {
	out := struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{OK: false, Error: err.Error()}
	b, _ := json.Marshal(out)
	return string(b)
}

func jsonErrf(format string, args ...any) string {
	return jsonErr(fmt.Errorf(format, args...))
}
