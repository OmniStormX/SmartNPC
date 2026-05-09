// Package group implements multi-NPC group conversations with probabilistic
// turn-taking. A GroupConversation hosts an ordered transcript whose entries
// can come from the player or any participating NPC; the orchestrator decides
// who responds next via TurnManager.
//
// The package is intentionally provider-agnostic: it interacts with NPC
// agents only through the AgentRouter interface (defined in orchestrator.go).
// chat.Router implements that interface so a single Router can power both
// 1-on-1 chat (existing) and group chat (this package).
package group

import (
	"time"
)

// SpeakerPlayer is the canonical "speaker" string used in GroupMessage.Speaker
// when the player is the source. Anything else is treated as an NPC name.
const SpeakerPlayer = "player"

// GroupConversation is the in-memory transcript + roster for one group chat.
// Concurrent access goes through Orchestrator's lock; clients that obtain a
// pointer via GetGroup must not mutate it directly.
type GroupConversation struct {
	// ID is a stable identifier (UUID) used by the UI to address the group.
	ID string
	// Participants holds the NPC names currently in the room. The player is
	// always implicitly present and is NOT listed here.
	Participants []string
	// History is an append-only ordered transcript. Trimmed to MaxHistory
	// entries on append (oldest dropped first).
	History []GroupMessage
	// CreatedAt / LastActivity are wall-clock timestamps used for grooming
	// idle groups out of the orchestrator and for UI display.
	CreatedAt    time.Time
	LastActivity time.Time
	// MaxHistory caps History size. Defaults to 60 in NewGroup.
	MaxHistory int
	// stats keys NPC name → live participation stats. Maintained by the
	// orchestrator on every appendMessage.
	stats map[string]*Participant
}

// GroupMessage is one transcript entry. Speaker == SpeakerPlayer for the
// human, otherwise an NPC display name (case is preserved as written).
type GroupMessage struct {
	Speaker   string
	Content   string
	Timestamp time.Time
	// ReplyTo is an optional explicit "@addressee" hint. Empty when the
	// message is broadcast to the room.
	ReplyTo string
}

// Participant tracks per-NPC live state used by the turn manager. Updated
// when an NPC actually speaks (not when they're merely selected — a [PASS]
// reply must not increment SpeakCount).
type Participant struct {
	Name       string
	LastSpoke  time.Time
	SpeakCount int
	// IsActive is true while the NPC is part of the group's roster. Cleared
	// by RemoveParticipant.
	IsActive bool
}

// RespondentDecision is one slot in the turn manager's output: the NPC who
// should respond, the priority that won them the slot (purely diagnostic),
// and the staggered Delay before they speak. Higher Priority → earlier
// Delay so addressed NPCs feel snappier.
type RespondentDecision struct {
	NPC      string
	Priority float64
	Delay    time.Duration
}

// statsFor returns the live participant stats for name, lazily creating an
// inactive zero-value entry when absent. Caller must hold the orchestrator's
// lock; turn_manager holds its own rng lock independently. Returning a
// pointer keeps mutations cheap.
func (g *GroupConversation) statsFor(name string) *Participant {
	if g.stats == nil {
		g.stats = make(map[string]*Participant)
	}
	p, ok := g.stats[name]
	if !ok {
		p = &Participant{Name: name, IsActive: true}
		g.stats[name] = p
	}
	return p
}
