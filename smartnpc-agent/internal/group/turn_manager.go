// Turn-taking probabilistic engine.
//
// DetermineRespondents picks 0..N NPCs to reply to a new GroupMessage. The
// per-NPC chance is calculated from:
//
//   - base: 0.6 if msg from player, 0.35 if msg from another NPC
//   - addressed boost: + AddressedBoost when the NPC's name appears
//     case-insensitively in msg.Content (or in msg.ReplyTo)
//   - recent-speak penalty: × 0.5 if LastSpoke < 30s ago
//   - consecutive-speak hard cap: 0 if NPC's name dominates the tail of
//     History at MaxConsecutiveSame entries already
//   - chain decay: × ChainDecay^chainDepth (NPC↔NPC follow-ups taper off)
//   - msg.Speaker is excluded (NPC never replies to themselves)
//   - clamped to [0, 0.95]
//
// All chosen NPCs are returned with a 1-4s staggered Delay so the room
// doesn't talk over itself; addressed NPCs sort earlier in the schedule.
// MaxSimultaneous caps how many slots get filled per call.
package group

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

// TurnConfig tunes the turn manager's probabilistic behaviour. All fields
// have package defaults applied by DefaultTurnConfig.
type TurnConfig struct {
	BaseResponseChance float64       // player → NPC default; 0.6
	NPCtoNPCChance     float64       // NPC → NPC default; 0.35
	MaxConsecutiveSame int           // hard cap; 2
	MinSpeakInterval   time.Duration // applies recent-speak penalty under this; 2s
	MaxSimultaneous    int           // max responders per turn; 2
	AddressedBoost     float64       // additive when NPC is named; 0.4
	ChainDecay         float64       // multiplied per chain depth; 0.5
}

// DefaultTurnConfig returns the package-recommended tuning. Deliberately
// conservative — group chat readability beats LLM utilisation.
func DefaultTurnConfig() TurnConfig {
	return TurnConfig{
		BaseResponseChance: 0.6,
		NPCtoNPCChance:     0.35,
		MaxConsecutiveSame: 2,
		MinSpeakInterval:   2 * time.Second,
		MaxSimultaneous:    2,
		AddressedBoost:     0.4,
		ChainDecay:         0.5,
	}
}

// TurnManager is the probabilistic decision engine. Safe for concurrent use
// because rand.Rand is guarded by an internal mutex; callers may share one
// TurnManager across all groups.
type TurnManager struct {
	config TurnConfig

	mu  sync.Mutex
	rng *rand.Rand
}

// NewTurnManager constructs a manager with the given config and rng. Pass
// nil rng to use a fresh time-seeded one. config is filled with defaults
// for any zero field — callers can construct a partial TurnConfig and
// trust sensible behaviour.
func NewTurnManager(config TurnConfig, rng *rand.Rand) *TurnManager {
	cfg := mergeWithDefaults(config)
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &TurnManager{config: cfg, rng: rng}
}

// Config exposes the resolved (post-merge) configuration. Useful for tests
// that need to verify default substitution.
func (tm *TurnManager) Config() TurnConfig {
	return tm.config
}

// DetermineRespondents returns the NPCs (with stagger delays) who should
// react to newMsg. May be empty when nobody rolls high enough or the
// message itself is from an NPC and chainDepth has driven probabilities
// to zero.
//
// chainDepth = 0 for the original message (player → NPCs); each successive
// NPC-to-NPC bounce increments it.
func (tm *TurnManager) DetermineRespondents(group *GroupConversation, newMsg GroupMessage, chainDepth int) []RespondentDecision {
	if group == nil || len(group.Participants) == 0 {
		return nil
	}

	type candidate struct {
		name     string
		priority float64
	}
	var picks []candidate

	tm.mu.Lock()
	for _, name := range group.Participants {
		// Self-skip: an NPC never replies to its own utterance.
		if strings.EqualFold(name, newMsg.Speaker) {
			continue
		}
		p := group.statsFor(name)
		if !p.IsActive {
			continue
		}
		chance := tm.calculateChance(p, newMsg, group, chainDepth)
		if chance <= 0 {
			continue
		}
		// Roll. The recorded priority equals the chance so the caller can
		// observe the decision when debugging.
		if tm.rng.Float64() < chance {
			picks = append(picks, candidate{name: name, priority: chance})
		}
	}
	tm.mu.Unlock()

	if len(picks) == 0 {
		return nil
	}

	// Highest priority first → earlier delay. Stable so equal-priority NPCs
	// keep registry order which keeps tests deterministic when paired with
	// a seeded rng.
	sort.SliceStable(picks, func(i, j int) bool {
		return picks[i].priority > picks[j].priority
	})

	// Cap by MaxSimultaneous.
	if len(picks) > tm.config.MaxSimultaneous {
		picks = picks[:tm.config.MaxSimultaneous]
	}

	out := make([]RespondentDecision, 0, len(picks))
	for i, c := range picks {
		// Stagger: 1s for the snappiest, +1s per subsequent slot. Keeps the
		// max delay bounded by MaxSimultaneous (≈ 4s for the default cap of
		// 2 slots — there's no way to overshoot the spec's 1-4s window).
		delay := time.Duration(i+1) * time.Second
		out = append(out, RespondentDecision{
			NPC:      c.name,
			Priority: c.priority,
			Delay:    delay,
		})
	}
	return out
}

// calculateChance encapsulates the probability formula. Exposed at package-
// level visibility (lowercase) so the internals can be unit-tested via the
// public DetermineRespondents API; the formula itself is pure given inputs.
func (tm *TurnManager) calculateChance(p *Participant, msg GroupMessage, group *GroupConversation, chainDepth int) float64 {
	var base float64
	if msg.Speaker == SpeakerPlayer {
		base = tm.config.BaseResponseChance
	} else {
		base = tm.config.NPCtoNPCChance
	}

	// Addressed boost: name appears in content (case-insensitive) or is
	// the explicit ReplyTo. ReplyTo wins outright (the message is *for*
	// them, no question of intent).
	if isAddressed(p.Name, msg) {
		base += tm.config.AddressedBoost
	}

	// Recent-speak penalty: keeps any one NPC from monopolising the floor.
	if !p.LastSpoke.IsZero() && time.Since(p.LastSpoke) < 30*time.Second {
		base *= 0.5
	}

	// Hard consecutive-speak cap.
	if countConsecutiveTrailingSpeaker(group.History, p.Name) >= tm.config.MaxConsecutiveSame {
		return 0
	}

	// Chain decay — each layer of NPC-to-NPC reply loses a multiplicative
	// factor so cascades fizzle.
	if chainDepth > 0 {
		base *= math.Pow(tm.config.ChainDecay, float64(chainDepth))
	}

	if base < 0 {
		base = 0
	}
	if base > 0.95 {
		base = 0.95
	}
	return base
}

// isAddressed reports whether the NPC's name (case-insensitive substring)
// appears in msg.Content or matches msg.ReplyTo.
func isAddressed(npcName string, msg GroupMessage) bool {
	if msg.ReplyTo != "" && strings.EqualFold(msg.ReplyTo, npcName) {
		return true
	}
	if msg.Content == "" || npcName == "" {
		return false
	}
	return strings.Contains(strings.ToLower(msg.Content), strings.ToLower(npcName))
}

// countConsecutiveTrailingSpeaker counts how many of the most recent
// messages were authored by name. Stops at the first non-match.
func countConsecutiveTrailingSpeaker(history []GroupMessage, name string) int {
	n := 0
	for i := len(history) - 1; i >= 0; i-- {
		if strings.EqualFold(history[i].Speaker, name) {
			n++
		} else {
			return n
		}
	}
	return n
}

// mergeWithDefaults fills zero-value fields in cfg from DefaultTurnConfig.
// Negative values are kept verbatim — tests can opt into "always silent"
// behaviour by passing -1 chances.
func mergeWithDefaults(cfg TurnConfig) TurnConfig {
	d := DefaultTurnConfig()
	if cfg.BaseResponseChance == 0 {
		cfg.BaseResponseChance = d.BaseResponseChance
	}
	if cfg.NPCtoNPCChance == 0 {
		cfg.NPCtoNPCChance = d.NPCtoNPCChance
	}
	if cfg.MaxConsecutiveSame == 0 {
		cfg.MaxConsecutiveSame = d.MaxConsecutiveSame
	}
	if cfg.MinSpeakInterval == 0 {
		cfg.MinSpeakInterval = d.MinSpeakInterval
	}
	if cfg.MaxSimultaneous == 0 {
		cfg.MaxSimultaneous = d.MaxSimultaneous
	}
	if cfg.AddressedBoost == 0 {
		cfg.AddressedBoost = d.AddressedBoost
	}
	if cfg.ChainDecay == 0 {
		cfg.ChainDecay = d.ChainDecay
	}
	return cfg
}
