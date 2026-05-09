package scheduler

import (
	"fmt"
	"strings"
)

// GameState holds context about the current game world, used to compose the
// decision prompt sent to an NPC.
type GameState struct {
	TimeString     string
	DateString     string
	Weather        string
	PlayerLocation string
}

// ProactiveDecision is the parsed output of an NPC's response to the
// proactive trigger prompt.
type ProactiveDecision struct {
	WantsToApproach bool
	Reason          string
	OpeningLine     string
}

// buildDecisionPrompt constructs the internal prompt asking an NPC whether
// it wants to approach the player right now.
func buildDecisionPrompt(_ string, gs GameState) string {
	return fmt.Sprintf(`It is %s on %s. The weather is %s.
The player is at %s.

Consider: do you want to approach the player right now?
Think about:
- What time of day it is and what you'd normally be doing
- Whether you have something to say (news, a question, a gift, gossip)
- Your personality and relationship with the player

Reply in this EXACT format:
DECISION: YES or NO
REASON: (one sentence why)
OPENING: (what you will say to the player)

If you don't want to approach, just write:
DECISION: NO
REASON: (brief reason)`,
		gs.TimeString, gs.DateString, gs.Weather, gs.PlayerLocation)
}

// parseDecision extracts a ProactiveDecision from the NPC's raw reply text.
func parseDecision(reply string) *ProactiveDecision {
	d := &ProactiveDecision{}

	lines := strings.Split(reply, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)

		if strings.HasPrefix(upper, "DECISION:") {
			val := strings.TrimSpace(line[len("DECISION:"):])
			d.WantsToApproach = strings.EqualFold(strings.TrimSpace(val), "YES")
		} else if strings.HasPrefix(upper, "REASON:") {
			d.Reason = strings.TrimSpace(line[len("REASON:"):])
		} else if strings.HasPrefix(upper, "OPENING:") {
			d.OpeningLine = strings.TrimSpace(line[len("OPENING:"):])
		}
	}

	// Fallback heuristic: if no structured format but reply contains "yes"
	if !d.WantsToApproach && d.Reason == "" {
		lower := strings.ToLower(reply)
		if strings.Contains(lower, "yes") && !strings.Contains(lower, "no") {
			d.WantsToApproach = true
			d.Reason = reply
			d.OpeningLine = reply
		}
	}

	return d
}
