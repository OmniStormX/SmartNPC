// Package scheduler implements proactive NPC behavior — periodically picking
// an NPC and asking whether it wants to approach the player unprompted.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// AgentRouter abstracts the chat.Router for scheduler use.
type AgentRouter interface {
	// ListAgents returns the speaker names of all registered NPC agents.
	ListAgents() []string
	// TriggerProactive injects an internal prompt into the named NPC and
	// returns its raw text reply (used to decide if it wants to approach).
	TriggerProactive(ctx context.Context, npcName, prompt string) (string, error)
}

// MCPSession abstracts tool invocation on the shared MCP session.
type MCPSession interface {
	CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error)
}

// Config holds all tunable knobs for the proactive scheduler.
type Config struct {
	CheckInterval    time.Duration // how often to attempt (default 15 min)
	CooldownPerNPC   time.Duration // min wait before same NPC acts again (60 min)
	GlobalCooldown   time.Duration // min wait between ANY proactive event (5 min)
	ActiveHoursStart int           // earliest game hour to trigger (inclusive, 6)
	ActiveHoursEnd   int           // latest game hour to trigger (exclusive, 22)
	MaxDailyPerNPC   int           // max proactive actions per game-day per NPC (2)
	ApproachChance   float64       // base probability NPC wants to act (0.3)
}

// DefaultConfig returns production-tuned defaults.
func DefaultConfig() Config {
	return Config{
		CheckInterval:    15 * time.Minute,
		CooldownPerNPC:   60 * time.Minute,
		GlobalCooldown:   5 * time.Minute,
		ActiveHoursStart: 6,
		ActiveHoursEnd:   22,
		MaxDailyPerNPC:   2,
		ApproachChance:   0.3,
	}
}

// Scheduler runs a periodic loop that may trigger NPC proactive behaviors.
type Scheduler struct {
	router    AgentRouter
	session   MCPSession
	cooldowns *CooldownTracker
	config    Config
	rng       *rand.Rand
	logger    *slog.Logger

	stopCh chan struct{}
	wg     sync.WaitGroup

	// nowFn is injectable for testing (defaults to time.Now).
	nowFn func() time.Time
}

// New creates a Scheduler. Call Start to begin the periodic loop.
func New(router AgentRouter, session MCPSession, cfg Config, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		router:    router,
		session:   session,
		cooldowns: NewCooldownTracker(),
		config:    cfg,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:    logger,
		stopCh:    make(chan struct{}),
		nowFn:     time.Now,
	}
}

// Start begins the scheduler loop in a background goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.loop(ctx)
}

// Stop signals the loop to exit and waits for it to finish.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// Tick is exported for testing — runs a single proactive check cycle.
func (s *Scheduler) Tick(ctx context.Context) {
	s.tick(ctx)
}

func (s *Scheduler) tick(ctx context.Context) {
	now := s.nowFn()

	// 1. Global cooldown
	if !s.cooldowns.GlobalReady(now, s.config.GlobalCooldown) {
		s.logger.Debug("scheduler: global cooldown active")
		return
	}

	// 2. Get game time and check active hours
	gameTime, err := s.getGameTime(ctx)
	if err != nil {
		s.logger.Debug("scheduler: failed to get game time", "err", err)
		return
	}
	if gameTime.Hour < s.config.ActiveHoursStart || gameTime.Hour >= s.config.ActiveHoursEnd {
		s.logger.Debug("scheduler: outside active hours", "hour", gameTime.Hour)
		return
	}

	// 3. Check player status
	playerBusy, err := s.isPlayerBusy(ctx)
	if err != nil {
		s.logger.Debug("scheduler: failed to get player status", "err", err)
		return
	}
	if playerBusy {
		s.logger.Debug("scheduler: player is busy")
		return
	}

	// 4. Reset daily counts if game day changed
	s.cooldowns.ResetIfNewDay(gameTime.TotalDays)

	// 5. Filter eligible NPCs
	agents := s.router.ListAgents()
	var eligible []string
	for _, name := range agents {
		if s.cooldowns.IsEligible(name, now, s.config) {
			eligible = append(eligible, name)
		}
	}
	if len(eligible) == 0 {
		s.logger.Debug("scheduler: no eligible NPCs")
		return
	}

	// 6. Random selection (could be weighted by friendship later)
	selected := eligible[s.rng.Intn(len(eligible))]

	// 7. Ask NPC if it wants to approach
	gs := GameState{
		TimeString:     fmt.Sprintf("%d:00", gameTime.Hour),
		DateString:     fmt.Sprintf("Day %d", gameTime.TotalDays),
		Weather:        gameTime.Weather,
		PlayerLocation: gameTime.PlayerLocation,
	}
	prompt := buildDecisionPrompt(selected, gs)

	reply, err := s.router.TriggerProactive(ctx, selected, prompt)
	if err != nil {
		s.logger.Debug("scheduler: NPC decision failed", "npc", selected, "err", err)
		return
	}

	decision := parseDecision(reply)
	if !decision.WantsToApproach {
		s.logger.Info("scheduler: NPC declined", "npc", selected)
		return
	}

	// 8. Execute approach: summon + speak
	s.logger.Info("scheduler: NPC approaching player", "npc", selected, "reason", decision.Reason)

	_, err = s.session.CallTool(ctx, "npc_summon", map[string]any{"npc": selected})
	if err != nil {
		s.logger.Debug("scheduler: summon failed", "npc", selected, "err", err)
		return
	}

	// Brief pause for the NPC to walk over
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	openingLine := decision.OpeningLine
	if openingLine == "" {
		openingLine = decision.Reason
	}
	_, err = s.session.CallTool(ctx, "chat_say", map[string]any{
		"speaker": selected,
		"text":    openingLine,
	})
	if err != nil {
		s.logger.Debug("scheduler: chat_say failed", "npc", selected, "err", err)
	}

	// 9. Record cooldown
	s.cooldowns.RecordAction(selected, now)
}

// GameTimeInfo holds parsed game time state from the game_get_time tool.
type GameTimeInfo struct {
	Hour           int
	TotalDays      int
	Weather        string
	PlayerLocation string
}

func (s *Scheduler) getGameTime(ctx context.Context) (*GameTimeInfo, error) {
	raw, err := s.session.CallTool(ctx, "game_get_time", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Hour           int    `json:"hour"`
		TotalDays      int    `json:"total_days"`
		Weather        string `json:"weather"`
		PlayerLocation string `json:"player_location"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse game_get_time: %w", err)
	}
	return &GameTimeInfo{
		Hour:           result.Hour,
		TotalDays:      result.TotalDays,
		Weather:        result.Weather,
		PlayerLocation: result.PlayerLocation,
	}, nil
}

func (s *Scheduler) isPlayerBusy(ctx context.Context) (bool, error) {
	raw, err := s.session.CallTool(ctx, "player_get_status", nil)
	if err != nil {
		// If the tool doesn't exist yet, assume player is available
		return false, nil
	}
	var result struct {
		Busy    bool `json:"busy"`
		InMenu  bool `json:"in_menu"`
		InEvent bool `json:"in_event"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, nil
	}
	return result.Busy || result.InMenu || result.InEvent, nil
}
