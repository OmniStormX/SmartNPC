package memory

import "fmt"

// retriever defaults — kept as constants so tests and call sites don't
// duplicate magic numbers.
const (
	// defaultRecentSummaryCount is how many of the most recent conversation
	// summaries are folded into a ContextBundle.
	defaultRecentSummaryCount = 3
	// defaultKeyMemoryLimit caps the non-relationship memory list returned
	// in the bundle.
	defaultKeyMemoryLimit = 15
	// defaultMinImportance is the lower importance bound for memories that
	// qualify as "key".
	defaultMinImportance = 3
)

// assembleContextBundle is the canonical retriever implementation. It is the
// only function the Store hands the live *sqliteStore to, so test harnesses
// can swap in a fake store by providing their own Store implementation that
// returns a pre-baked ContextBundle.
//
// Composition rules:
//
//   - RecentSummary: top-N summaries by created_at DESC.
//   - RelationshipFacts: every non-expired memory in the relationship
//     category (always included regardless of importance).
//   - KeyMemories: top-K non-relationship memories with importance >=
//     defaultMinImportance, ordered by importance DESC, access_count DESC,
//     created_at DESC.
//   - TotalConversations: lifetime conversation count for the NPC.
//
// Memories surfaced into the bundle have their access_count incremented
// (TouchMemory) so frequently-recalled rows drift to the top over time.
//
// currentMessage is reserved for future relevance scoring and is currently
// unused; the parameter stays in the signature so adopting a real ranker
// later won't break callers.
func assembleContextBundle(s *sqliteStore, npcName, _ string) (*ContextBundle, error) {
	if npcName == "" {
		return nil, fmt.Errorf("assembleContextBundle: npcName is required")
	}

	// --- summaries ---
	summaries, err := s.GetRecentSummaries(npcName, defaultRecentSummaryCount)
	if err != nil {
		return nil, err
	}
	// Surface oldest-first so the LLM reads chronologically.
	reverseSummaries(summaries)

	// --- relationship facts (always include) ---
	relFacts, err := s.GetMemories(npcName, MemoryQuery{
		Categories: []string{CategoryRelationship},
	})
	if err != nil {
		return nil, err
	}

	// --- key memories ---
	keyMemories, err := s.GetMemories(npcName, MemoryQuery{
		Categories: []string{
			CategoryFact,
			CategoryPreference,
			CategoryEvent,
			CategoryPromise,
		},
		MinImportance: defaultMinImportance,
		Limit:         defaultKeyMemoryLimit,
	})
	if err != nil {
		return nil, err
	}

	// --- conversation count ---
	total, err := countConversations(s, npcName)
	if err != nil {
		return nil, err
	}

	// --- touch surfaced memories ---
	for _, m := range relFacts {
		_ = s.TouchMemory(m.ID)
	}
	for _, m := range keyMemories {
		_ = s.TouchMemory(m.ID)
	}

	return &ContextBundle{
		RecentSummary:      summaries,
		KeyMemories:        keyMemories,
		RelationshipFacts:  relFacts,
		TotalConversations: total,
	}, nil
}

// reverseSummaries flips the slice in-place so callers get chronological
// (oldest-first) order from a DESC-ordered query.
func reverseSummaries(s []Summary) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
