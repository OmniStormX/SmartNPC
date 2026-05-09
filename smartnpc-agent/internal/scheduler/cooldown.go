package scheduler

import (
	"strings"
	"sync"
	"time"
)

// CooldownTracker manages per-NPC and global cooldown state.
type CooldownTracker struct {
	mu         sync.RWMutex
	lastAction map[string]time.Time // npcName (lowercase) → last proactive action time
	lastGlobal time.Time            // last time ANY proactive event fired
	dailyCount map[string]int       // npcName (lowercase) → actions today
	currentDay int                  // game day (totalDays)
}

// NewCooldownTracker returns a zero-state tracker.
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		lastAction: make(map[string]time.Time),
		dailyCount: make(map[string]int),
	}
}

func normalizeNPC(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GlobalReady returns true if the global cooldown has elapsed since the last
// proactive event.
func (ct *CooldownTracker) GlobalReady(now time.Time, cooldown time.Duration) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return now.Sub(ct.lastGlobal) >= cooldown
}

// IsEligible checks whether npcName can be triggered right now.
func (ct *CooldownTracker) IsEligible(npcName string, now time.Time, cfg Config) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	key := normalizeNPC(npcName)

	// Per-NPC cooldown
	if last, ok := ct.lastAction[key]; ok {
		if now.Sub(last) < cfg.CooldownPerNPC {
			return false
		}
	}

	// Daily limit
	if ct.dailyCount[key] >= cfg.MaxDailyPerNPC {
		return false
	}

	return true
}

// RecordAction marks an NPC as having performed a proactive action.
func (ct *CooldownTracker) RecordAction(npcName string, now time.Time) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	key := normalizeNPC(npcName)
	ct.lastAction[key] = now
	ct.lastGlobal = now
	ct.dailyCount[key]++
}

// ResetIfNewDay resets daily counts when the game day changes.
func (ct *CooldownTracker) ResetIfNewDay(gameDay int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if gameDay != ct.currentDay {
		ct.currentDay = gameDay
		ct.dailyCount = make(map[string]int)
	}
}
