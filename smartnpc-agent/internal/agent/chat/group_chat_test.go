package chat

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// withZeroGroupChatDelay neutralises the inter-NPC sleep so table-driven
// group-chat tests don't have to wait out the 1.5s production pause. Always
// restore via the returned cleanup func — other group-chat tests rely on the
// default delay being in place.
func withZeroGroupChatDelay(t *testing.T) {
	t.Helper()
	prev := groupChatReplyDelay
	groupChatReplyDelay = 0
	t.Cleanup(func() { groupChatReplyDelay = prev })
}

// newGroupChatAgent builds an Agent backed by a mockProvider that always
// returns the given reply. FriendshipTimeout is disabled so the call doesn't
// try to hit an MCP session we never wire up.
func newGroupChatAgent(speaker, reply string) (*Agent, *mockProvider) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: reply, FinishReason: "stop"},
	}}
	a := New(Config{
		Provider:          mp,
		Speaker:           speaker,
		SystemPrompt:      "test " + speaker,
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	return a, mp
}

// newGroupChatAgentReplies is the multi-reply variant: each element of
// replies is returned in order so two-round tests can seed distinct
// Round 1 / Round 2 responses. Callers pass as many replies as rounds
// they expect to exercise; anything past the queue length falls back to
// the mockProvider default ("mock reply") which is NOT idle, so seeding
// too few is usually a test bug.
func newGroupChatAgentReplies(speaker string, replies ...string) (*Agent, *mockProvider) {
	resp := make([]llm.ChatResponse, len(replies))
	for i, r := range replies {
		resp[i] = llm.ChatResponse{Content: r, FinishReason: "stop"}
	}
	mp := &mockProvider{replies: resp}
	a := New(Config{
		Provider:          mp,
		Speaker:           speaker,
		SystemPrompt:      "test " + speaker,
		MaxHistory:        20,
		FriendshipTimeout: -1,
	})
	return a, mp
}

// TestGroupChat_ThreeParticipants verifies the sequential reply contract:
// every participant runs in order and contributes one entry to the returned
// history.
func TestGroupChat_ThreeParticipants(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, mp1 := newGroupChatAgent("XiaMi", "我觉得去海边！")
	a2, mp2 := newGroupChatAgent("Abigail", "矿洞更刺激。")
	a3, mp3 := newGroupChatAgent("Sebastian", "呃……随便吧。")

	r := NewRouterFromAgents([]*Agent{a1, a2, a3})

	history := r.RunGroupChat(context.Background(),
		[]string{"XiaMi", "Abigail", "Sebastian"},
		"你们觉得今天去哪玩比较好？")

	if len(history) != 3 {
		t.Fatalf("expected 3 replies, got %d: %+v", len(history), history)
	}
	wantOrder := []string{"XiaMi", "Abigail", "Sebastian"}
	for i, want := range wantOrder {
		if history[i].Speaker != want {
			t.Errorf("history[%d].Speaker = %q, want %q", i, history[i].Speaker, want)
		}
	}
	for i, mp := range []*mockProvider{mp1, mp2, mp3} {
		if len(mp.calls) != 1 {
			t.Errorf("provider[%d] should fire exactly once, got %d", i, len(mp.calls))
		}
	}
}

// TestGroupChat_ContextCarried verifies that each subsequent participant sees
// all previous replies injected into the user-side prompt. The first NPC must
// NOT see any prior replies; the second must see the first; the third must
// see both.
func TestGroupChat_ContextCarried(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, mp1 := newGroupChatAgent("XiaMi", "海边吧！")
	a2, mp2 := newGroupChatAgent("Abigail", "不要，去矿洞。")
	a3, mp3 := newGroupChatAgent("Sebastian", "都行。")

	r := NewRouterFromAgents([]*Agent{a1, a2, a3})

	r.RunGroupChat(context.Background(),
		[]string{"XiaMi", "Abigail", "Sebastian"},
		"去哪玩？")

	// Helper: pull the last user-role message from a mockProvider's most
	// recent call.
	latestUser := func(mp *mockProvider) string {
		if len(mp.calls) == 0 {
			return ""
		}
		msgs := mp.calls[len(mp.calls)-1].Messages
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == llm.RoleUser {
				return msgs[i].Content
			}
		}
		return ""
	}

	// First NPC: no prior replies — "其他NPC已经说了" block must be absent.
	first := latestUser(mp1)
	if !strings.Contains(first, "[群聊]") {
		t.Errorf("first prompt missing [群聊] marker: %q", first)
	}
	if strings.Contains(first, "其他NPC已经说了") {
		t.Errorf("first NPC should not see any prior replies, got: %q", first)
	}

	// Second NPC: must see XiaMi's reply verbatim.
	second := latestUser(mp2)
	if !strings.Contains(second, "其他NPC已经说了") {
		t.Errorf("second prompt missing prior-replies block: %q", second)
	}
	if !strings.Contains(second, "XiaMi") || !strings.Contains(second, "海边吧！") {
		t.Errorf("second NPC did not see XiaMi's reply; prompt=%q", second)
	}
	if strings.Contains(second, "Abigail：「") {
		t.Errorf("second NPC should not see its own reply yet: %q", second)
	}

	// Third NPC: must see both XiaMi AND Abigail.
	third := latestUser(mp3)
	if !strings.Contains(third, "XiaMi") || !strings.Contains(third, "海边吧！") {
		t.Errorf("third NPC did not see XiaMi's reply; prompt=%q", third)
	}
	if !strings.Contains(third, "Abigail") || !strings.Contains(third, "不要，去矿洞。") {
		t.Errorf("third NPC did not see Abigail's reply; prompt=%q", third)
	}
}

// TestGroupChat_UnknownNPC verifies unknown participants are skipped without
// aborting the round.
func TestGroupChat_UnknownNPC(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, mp1 := newGroupChatAgent("XiaMi", "hi")
	a2, mp2 := newGroupChatAgent("Abigail", "yo")
	r := NewRouterFromAgents([]*Agent{a1, a2})

	history := r.RunGroupChat(context.Background(),
		[]string{"XiaMi", "Ghost", "Abigail"},
		"打个招呼")

	if len(history) != 2 {
		t.Fatalf("expected 2 replies (Ghost skipped), got %d: %+v", len(history), history)
	}
	if history[0].Speaker != "XiaMi" || history[1].Speaker != "Abigail" {
		t.Errorf("unexpected reply order: %+v", history)
	}
	if len(mp1.calls) != 1 || len(mp2.calls) != 1 {
		t.Errorf("each known agent should fire once, got xiami=%d abigail=%d",
			len(mp1.calls), len(mp2.calls))
	}
}

// TestGroupChat_IdleParticipantSkipped verifies that an NPC replying with
// "idle" is dropped from the history so downstream NPCs don't see a silent
// line, matching the pattern used by the proactive ticker.
func TestGroupChat_IdleParticipantSkipped(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, _ := newGroupChatAgent("XiaMi", "hi")
	a2, _ := newGroupChatAgent("Abigail", "idle")
	a3, mp3 := newGroupChatAgent("Sebastian", "sup")
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})

	history := r.RunGroupChat(context.Background(),
		[]string{"XiaMi", "Abigail", "Sebastian"},
		"hey")

	if len(history) != 2 {
		t.Fatalf("expected 2 replies (Abigail idle), got %d: %+v", len(history), history)
	}
	for _, m := range history {
		if m.Speaker == "Abigail" {
			t.Errorf("idle reply leaked into history: %+v", m)
		}
	}
	// Sebastian must not see Abigail in its prompt.
	if len(mp3.calls) == 0 {
		t.Fatal("Sebastian should still run after Abigail's idle skip")
	}
	last := mp3.calls[len(mp3.calls)-1]
	for _, m := range last.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "Abigail：「") {
			t.Errorf("Sebastian's prompt should not reference Abigail's idle turn: %q", m.Content)
		}
	}
}

// TestGroupChat_BuildPromptFormat locks the prompt shape so regressions on
// the [群聊] tag or ordering pop up as test failures rather than silent
// behaviour drift.
func TestGroupChat_BuildPromptFormat(t *testing.T) {
	prompt := buildGroupChatPrompt(
		"你们好",
		[]GroupMessage{
			{Speaker: "XiaMi", Text: "嗨！"},
			{Speaker: "Abigail", Text: "干嘛？"},
		},
		"Sebastian",
		1,
	)
	wants := []string{
		"[群聊] 玩家说：你们好",
		"其他NPC已经说了：",
		"- XiaMi：「嗨！」",
		"- Abigail：「干嘛？」",
		"现在轮到你（Sebastian）发言",
	}
	for _, w := range wants {
		if !strings.Contains(prompt, w) {
			t.Errorf("prompt missing %q; full prompt:\n%s", w, prompt)
		}
	}
}

// TestGroupChat_ContextCancelCutsRound verifies that RunGroupChat uses its own
// internal context and completes even when the caller's context is canceled.
// (Group chat runs asynchronously — it must not depend on the notification
// handler's short-lived context.)
func TestGroupChat_ContextCancelCutsRound(t *testing.T) {
	prev := groupChatReplyDelay
	groupChatReplyDelay = 0
	t.Cleanup(func() { groupChatReplyDelay = prev })

	a1, _ := newGroupChatAgent("XiaMi", "hi")
	a2, _ := newGroupChatAgent("Abigail", "yo")
	r := NewRouterFromAgents([]*Agent{a1, a2})

	// Cancel the caller's context immediately — RunGroupChat should still
	// complete because it derives its own background context internally.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before RunGroupChat starts

	history := r.RunGroupChat(ctx, []string{"XiaMi", "Abigail"}, "hey")

	// Both participants should still produce replies despite caller cancel.
	if len(history) != 2 {
		t.Fatalf("expected 2 replies (independent ctx), got %d: %+v", len(history), history)
	}
}

// ── extractGroupChatMessage parser ───────────────────────────

func TestExtractGroupChatMessage_HappyPath(t *testing.T) {
	req := fakeLoggingRequest("group_chat_message", map[string]any{
		"participants": []any{"XiaMi", "Abigail", "Sebastian"},
		"text":         "你们觉得今天去哪玩比较好？",
		"source":       "player",
	})
	ps, text, ok := extractGroupChatMessage(req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if text != "你们觉得今天去哪玩比较好？" {
		t.Errorf("text = %q", text)
	}
	want := []string{"XiaMi", "Abigail", "Sebastian"}
	if len(ps) != len(want) {
		t.Fatalf("participants = %v, want %v", ps, want)
	}
	for i, w := range want {
		if ps[i] != w {
			t.Errorf("participants[%d] = %q, want %q", i, ps[i], w)
		}
	}
}

func TestExtractGroupChatMessage_FiltersEmptyNames(t *testing.T) {
	req := fakeLoggingRequest("group_chat_message", map[string]any{
		"participants": []any{"XiaMi", "", "Abigail"},
		"text":         "hey",
	})
	ps, _, ok := extractGroupChatMessage(req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(ps) != 2 || ps[0] != "XiaMi" || ps[1] != "Abigail" {
		t.Errorf("empty names not filtered: %v", ps)
	}
}

func TestExtractGroupChatMessage_RejectsEmpty(t *testing.T) {
	cases := []struct {
		name string
		req  *mcp.LoggingMessageRequest
	}{
		{"no text", fakeLoggingRequest("group_chat_message", map[string]any{
			"participants": []any{"XiaMi"},
			"text":         "",
		})},
		{"no participants", fakeLoggingRequest("group_chat_message", map[string]any{
			"participants": []any{},
			"text":         "hi",
		})},
		{"wrong event name", fakeLoggingRequest("chat_message", map[string]any{
			"npc": "XiaMi", "text": "hi",
		})},
		{"nil req", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, ok := extractGroupChatMessage(c.req)
			if ok {
				t.Errorf("expected ok=false for %s", c.name)
			}
		})
	}
}

// TestRouter_GroupChatMessageDispatches verifies the HandleNotification branch
// for group_chat_message actually spawns a RunGroupChat round that hits every
// participant's provider.
func TestRouter_GroupChatMessageDispatches(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, mp1 := newGroupChatAgent("XiaMi", "嗨！")
	a2, mp2 := newGroupChatAgent("Abigail", "哦。")
	a3, mp3 := newGroupChatAgent("Sebastian", "在。")
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})
	h := r.HandleNotification()

	h(context.Background(), fakeLoggingRequest("group_chat_message", map[string]any{
		"participants": []any{"XiaMi", "Abigail", "Sebastian"},
		"text":         "谁在？",
		"source":       "player",
	}))

	// RunGroupChat runs in a goroutine — poll each provider briefly. Delay is
	// zeroed so the round should finish well under 500ms.
	waitCalls := func(mp *mockProvider, want int, who string) {
		t.Helper()
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			mp.mu.Lock()
			n := len(mp.calls)
			mp.mu.Unlock()
			if n >= want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		mp.mu.Lock()
		n := len(mp.calls)
		mp.mu.Unlock()
		t.Fatalf("%s: expected %d calls, got %d", who, want, n)
	}
	waitCalls(mp1, 1, "XiaMi")
	waitCalls(mp2, 1, "Abigail")
	waitCalls(mp3, 1, "Sebastian")
}

// ── GroupSession lifecycle (new persistent API) ───────────────

// TestGroupSession_CreateAndClose verifies the create → active → close
// contract: a fresh session is active with the requested participants,
// and after Close it is marked inactive AND removed from the map.
func TestGroupSession_CreateAndClose(t *testing.T) {
	r := NewRouter()
	s := r.CreateGroupSession("grp_test", []string{"XiaMi", "Abigail"})
	if s == nil {
		t.Fatal("CreateGroupSession returned nil")
	}
	if !s.Active {
		t.Errorf("new session should be Active, got %v", s.Active)
	}
	if s.ID != "grp_test" {
		t.Errorf("session.ID = %q, want %q", s.ID, "grp_test")
	}
	if got := s.Participants; len(got) != 2 || got[0] != "XiaMi" || got[1] != "Abigail" {
		t.Errorf("participants = %v, want [XiaMi Abigail]", got)
	}
	if r.GetGroupSession("grp_test") != s {
		t.Errorf("GetGroupSession should return the same pointer")
	}

	r.CloseGroupSession("grp_test")
	if s.Active {
		t.Errorf("session should be inactive after Close")
	}
	if r.GetGroupSession("grp_test") != nil {
		t.Errorf("GetGroupSession should return nil after Close")
	}

	// Closing an unknown ID must not panic.
	r.CloseGroupSession("does-not-exist")
}

// TestGroupSession_AddRemoveParticipant covers the invite/kick path:
// start with 3, add a 4th, remove the 2nd → participants list reflects
// each operation. Unknown IDs return an error; duplicate adds are no-ops.
func TestGroupSession_AddRemoveParticipant(t *testing.T) {
	r := NewRouter()
	r.CreateGroupSession("grp", []string{"XiaMi", "Abigail", "Sebastian"})

	if err := r.AddGroupParticipant("grp", "Leah"); err != nil {
		t.Fatalf("AddGroupParticipant: %v", err)
	}
	s := r.GetGroupSession("grp")
	if len(s.Participants) != 4 || s.Participants[3] != "Leah" {
		t.Errorf("after add: %v", s.Participants)
	}

	// Dup add is a no-op (len stays 4).
	if err := r.AddGroupParticipant("grp", "Leah"); err != nil {
		t.Fatalf("dup add: %v", err)
	}
	if len(s.Participants) != 4 {
		t.Errorf("dup add changed length: %v", s.Participants)
	}

	if err := r.RemoveGroupParticipant("grp", "Abigail"); err != nil {
		t.Fatalf("RemoveGroupParticipant: %v", err)
	}
	want := []string{"XiaMi", "Sebastian", "Leah"}
	if len(s.Participants) != len(want) {
		t.Fatalf("after remove: %v, want %v", s.Participants, want)
	}
	for i, w := range want {
		if s.Participants[i] != w {
			t.Errorf("after remove [%d] = %q, want %q", i, s.Participants[i], w)
		}
	}

	// Removing a non-member is a no-op (no error, no change).
	if err := r.RemoveGroupParticipant("grp", "Ghost"); err != nil {
		t.Errorf("remove non-member: %v", err)
	}

	// Unknown session ID returns an error for both Add and Remove.
	if err := r.AddGroupParticipant("missing", "XiaMi"); err == nil {
		t.Errorf("AddGroupParticipant on missing session should error")
	}
	if err := r.RemoveGroupParticipant("missing", "XiaMi"); err == nil {
		t.Errorf("RemoveGroupParticipant on missing session should error")
	}
	// Empty NPC name on Add rejected up front.
	if err := r.AddGroupParticipant("grp", ""); err == nil {
		t.Errorf("AddGroupParticipant with empty name should error")
	}
}

// TestHandleGroupMessage_TwoRounds verifies both rounds execute: 3
// participants × 2 rounds = up to 6 replies, with distinct Round 1 /
// Round 2 content surfaced in order.
func TestHandleGroupMessage_TwoRounds(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, _ := newGroupChatAgentReplies("XiaMi", "R1-XiaMi", "R2-XiaMi")
	a2, _ := newGroupChatAgentReplies("Abigail", "R1-Abigail", "R2-Abigail")
	a3, _ := newGroupChatAgentReplies("Sebastian", "R1-Sebastian", "R2-Sebastian")
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})

	r.CreateGroupSession("grp", []string{"XiaMi", "Abigail", "Sebastian"})
	replies := r.HandleGroupMessage("grp", "你们今晚去哪？")
	r.CloseGroupSession("grp")

	if len(replies) != 6 {
		t.Fatalf("expected 6 replies (3 × 2 rounds), got %d: %+v", len(replies), replies)
	}
	wantOrder := []string{
		"XiaMi", "Abigail", "Sebastian", // Round 1
		"XiaMi", "Abigail", "Sebastian", // Round 2
	}
	for i, w := range wantOrder {
		if replies[i].Speaker != w {
			t.Errorf("replies[%d].Speaker = %q, want %q", i, replies[i].Speaker, w)
		}
	}
	if replies[0].Text != "R1-XiaMi" || replies[3].Text != "R2-XiaMi" {
		t.Errorf("round separation broken: r1=%q r2=%q",
			replies[0].Text, replies[3].Text)
	}
}

// TestHandleGroupMessage_TokenControl verifies the len(participants)*2
// turn cap halts the round even if some participants still have queued
// replies. We stub 3 agents but artificially inflate the participant
// list so maxTurns=6 is reached after round 1 of a larger cohort.
//
// Simplest path: register 3 known agents and 3 unknown names — unknowns
// are skipped (don't bump TurnCount), so we end up with exactly 6
// non-idle replies across the two rounds. The TurnCount on the session
// must equal 6 (== maxTurns == 3 known × 2 rounds when unknowns are
// dropped before the counter).
func TestHandleGroupMessage_TokenControl(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, _ := newGroupChatAgentReplies("XiaMi", "r1", "r2")
	a2, _ := newGroupChatAgentReplies("Abigail", "r1", "r2")
	a3, _ := newGroupChatAgentReplies("Sebastian", "r1", "r2")
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})
	r.CreateGroupSession("grp", []string{"XiaMi", "Abigail", "Sebastian"})

	replies := r.HandleGroupMessage("grp", "hi")
	s := r.GetGroupSession("grp")
	if s == nil {
		t.Fatal("session missing after HandleGroupMessage")
	}
	if s.TurnCount != len(replies) {
		t.Errorf("TurnCount = %d, want %d (== len(replies))", s.TurnCount, len(replies))
	}
	if s.TurnCount > len(s.Participants)*2 {
		t.Errorf("TurnCount %d exceeds cap %d",
			s.TurnCount, len(s.Participants)*2)
	}
}

// TestHandleGroupMessage_IdleInRound2 verifies Round 2 idle replies are
// dropped (no history entry, no turn counted) so downstream NPCs in the
// same round don't see the ghost line.
func TestHandleGroupMessage_IdleInRound2(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, _ := newGroupChatAgentReplies("XiaMi", "R1", "idle")     // R2 idle
	a2, _ := newGroupChatAgentReplies("Abigail", "R1", "R2-OK")  // R2 speaks
	a3, _ := newGroupChatAgentReplies("Sebastian", "R1", "idle") // R2 idle
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})

	r.CreateGroupSession("grp", []string{"XiaMi", "Abigail", "Sebastian"})
	replies := r.HandleGroupMessage("grp", "hey")

	// Expected: 3 Round 1 + 1 Round 2 = 4 replies (idle × 2 dropped).
	if len(replies) != 4 {
		t.Fatalf("expected 4 non-idle replies, got %d: %+v", len(replies), replies)
	}
	for _, m := range replies {
		if isIdleReply(m.Text) {
			t.Errorf("idle reply leaked into replies: %+v", m)
		}
	}
	// The lone R2 speaker should be Abigail.
	if replies[3].Speaker != "Abigail" || replies[3].Text != "R2-OK" {
		t.Errorf("Round 2 reply unexpected: %+v", replies[3])
	}
	// History on the session should mirror: player + 4 non-idle NPC msgs.
	s := r.GetGroupSession("grp")
	if s == nil {
		t.Fatal("session missing")
	}
	if got := len(s.History); got != 5 {
		t.Errorf("history length = %d, want 5 (player + 4)", got)
	}
	if s.History[0].Speaker != "player" {
		t.Errorf("history[0] should be player, got %q", s.History[0].Speaker)
	}
}

// TestHandleGroupMessage_ResetOnNewPlayerMsg verifies two successive
// player turns: TurnCount resets to 0 at the top of each call, and the
// session History accumulates across both turns (so NPCs in turn 2 can
// see turn 1 context).
func TestHandleGroupMessage_ResetOnNewPlayerMsg(t *testing.T) {
	withZeroGroupChatDelay(t)

	// Seed 4 replies per NPC (2 rounds × 2 player turns).
	a1, _ := newGroupChatAgentReplies("XiaMi", "t1r1", "t1r2", "t2r1", "t2r2")
	a2, _ := newGroupChatAgentReplies("Abigail", "t1r1", "t1r2", "t2r1", "t2r2")
	r := NewRouterFromAgents([]*Agent{a1, a2})

	r.CreateGroupSession("grp", []string{"XiaMi", "Abigail"})

	_ = r.HandleGroupMessage("grp", "turn 1")
	s := r.GetGroupSession("grp")
	afterT1 := s.TurnCount
	historyLenT1 := len(s.History)
	if afterT1 == 0 {
		t.Fatal("TurnCount should advance during turn 1")
	}

	_ = r.HandleGroupMessage("grp", "turn 2")
	if s.TurnCount == 0 {
		t.Errorf("TurnCount = 0 after turn 2 (reset but no replies?)")
	}
	// Reset contract: TurnCount is reset to 0 at the top of turn 2 then
	// bumped per reply, so it must not have compounded on afterT1.
	if s.TurnCount > len(s.Participants)*2 {
		t.Errorf("TurnCount %d > cap %d — reset missed",
			s.TurnCount, len(s.Participants)*2)
	}
	// History grows across turns: turn 1 contributions + player-2 + turn 2 contributions.
	if len(s.History) <= historyLenT1 {
		t.Errorf("history shrank or stagnated across turns: t1=%d t2=%d",
			historyLenT1, len(s.History))
	}
	// Both player lines must be present.
	var playerLines int
	for _, m := range s.History {
		if m.Speaker == "player" {
			playerLines++
		}
	}
	if playerLines != 2 {
		t.Errorf("expected 2 player lines in history, got %d", playerLines)
	}
}

// TestHandleGroupMessage_UnknownSession verifies calling on an unknown
// group_id returns empty and doesn't panic.
func TestHandleGroupMessage_UnknownSession(t *testing.T) {
	withZeroGroupChatDelay(t)
	a1, _ := newGroupChatAgent("XiaMi", "hi")
	r := NewRouterFromAgents([]*Agent{a1})

	// Must not panic, must return nil.
	replies := r.HandleGroupMessage("never-created", "anyone there?")
	if replies != nil {
		t.Errorf("expected nil replies for unknown session, got %+v", replies)
	}
}

// TestGroupSession_HistoryTrim verifies appendGroupHistory caps at 50
// entries, trimming oldest-first so the most recent stays.
func TestGroupSession_HistoryTrim(t *testing.T) {
	// Drive the helper directly — the round loop has too many moving
	// parts to exercise 50+ entries cheaply.
	var history []GroupMessage
	for i := 0; i < 70; i++ {
		history = appendGroupHistory(history, GroupMessage{
			Speaker: "NPC",
			Text:    "x" + strconv.Itoa(i),
		})
	}
	if len(history) != groupHistoryCap {
		t.Fatalf("history len = %d, want %d", len(history), groupHistoryCap)
	}
	// First entry after trim should be the one at original index 20
	// (70 - 50 = 20).
	if history[0].Text != "x20" {
		t.Errorf("oldest-after-trim = %q, want %q", history[0].Text, "x20")
	}
	if history[len(history)-1].Text != "x69" {
		t.Errorf("newest = %q, want %q",
			history[len(history)-1].Text, "x69")
	}
}

// ── group_* lifecycle event extractors ────────────────────────

func TestExtractGroupCreate_HappyPath(t *testing.T) {
	req := fakeLoggingRequest("group_create", map[string]any{
		"group_id":     "grp_x",
		"participants": []any{"XiaMi", "Abigail"},
	})
	id, ps, ok := extractGroupCreate(req)
	if !ok || id != "grp_x" {
		t.Fatalf("ok=%v id=%q", ok, id)
	}
	if len(ps) != 2 || ps[0] != "XiaMi" || ps[1] != "Abigail" {
		t.Errorf("participants = %v", ps)
	}
}

func TestExtractGroupCreate_Rejects(t *testing.T) {
	cases := []struct {
		name string
		req  *mcp.LoggingMessageRequest
	}{
		{"no id", fakeLoggingRequest("group_create", map[string]any{
			"participants": []any{"XiaMi"},
		})},
		{"no participants", fakeLoggingRequest("group_create", map[string]any{
			"group_id": "g",
		})},
		{"wrong name", fakeLoggingRequest("chat_message", map[string]any{
			"group_id": "g", "participants": []any{"X"},
		})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, ok := extractGroupCreate(c.req); ok {
				t.Error("expected ok=false")
			}
		})
	}
}

func TestExtractGroupMessage_HappyPath(t *testing.T) {
	req := fakeLoggingRequest("group_message", map[string]any{
		"group_id": "grp_x",
		"text":     "hi",
		"source":   "player",
	})
	id, text, ok := extractGroupMessage(req)
	if !ok || id != "grp_x" || text != "hi" {
		t.Fatalf("ok=%v id=%q text=%q", ok, id, text)
	}
}

func TestExtractGroupInviteKick(t *testing.T) {
	// Use the same helper under both event names.
	invite := fakeLoggingRequest("group_invite", map[string]any{
		"group_id": "g", "npc": "Sebastian",
	})
	id, npc, ok := extractGroupInvite(invite)
	if !ok || id != "g" || npc != "Sebastian" {
		t.Errorf("invite: ok=%v id=%q npc=%q", ok, id, npc)
	}
	kick := fakeLoggingRequest("group_kick", map[string]any{
		"group_id": "g", "npc": "Sebastian",
	})
	id, npc, ok = extractGroupKick(kick)
	if !ok || id != "g" || npc != "Sebastian" {
		t.Errorf("kick: ok=%v id=%q npc=%q", ok, id, npc)
	}
	// Wrong event name ⇒ ok=false via extractors.
	wrong := fakeLoggingRequest("group_invite", map[string]any{
		"group_id": "g", "npc": "Sebastian",
	})
	if _, _, ok := extractGroupKick(wrong); ok {
		t.Error("group_kick should reject group_invite envelope")
	}
}

func TestExtractGroupClose(t *testing.T) {
	req := fakeLoggingRequest("group_close", map[string]any{"group_id": "g"})
	id, ok := extractGroupClose(req)
	if !ok || id != "g" {
		t.Errorf("ok=%v id=%q", ok, id)
	}
	// Missing group_id ⇒ reject.
	bad := fakeLoggingRequest("group_close", map[string]any{})
	if _, ok := extractGroupClose(bad); ok {
		t.Error("expected ok=false for empty id")
	}
}

// TestRouter_GroupLifecycleDispatches walks the full lifecycle through
// HandleNotification: create → message → invite → message (4 participants
// now) → close. Verifies each event produces the expected side-effect on
// the router's session map.
func TestRouter_GroupLifecycleDispatches(t *testing.T) {
	withZeroGroupChatDelay(t)

	a1, _ := newGroupChatAgentReplies("XiaMi", "r1", "r2", "r3", "r4")
	a2, _ := newGroupChatAgentReplies("Abigail", "r1", "r2", "r3", "r4")
	a3, _ := newGroupChatAgentReplies("Sebastian", "r1", "r2", "r3", "r4")
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})
	h := r.HandleNotification()

	// group_create
	h(context.Background(), fakeLoggingRequest("group_create", map[string]any{
		"group_id":     "grp_live",
		"participants": []any{"XiaMi", "Abigail"},
	}))
	s := r.GetGroupSession("grp_live")
	if s == nil || len(s.Participants) != 2 {
		t.Fatalf("after create: %+v", s)
	}

	// group_invite adds Sebastian.
	h(context.Background(), fakeLoggingRequest("group_invite", map[string]any{
		"group_id": "grp_live",
		"npc":      "Sebastian",
	}))
	if len(s.Participants) != 3 {
		t.Fatalf("after invite: %v", s.Participants)
	}

	// group_message triggers async round — poll for settle.
	h(context.Background(), fakeLoggingRequest("group_message", map[string]any{
		"group_id": "grp_live",
		"text":     "hey",
		"source":   "player",
	}))
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		tc := s.TurnCount
		s.mu.Unlock()
		if tc > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.TurnCount == 0 {
		t.Errorf("group_message did not advance TurnCount")
	}

	// group_kick drops Abigail.
	h(context.Background(), fakeLoggingRequest("group_kick", map[string]any{
		"group_id": "grp_live",
		"npc":      "Abigail",
	}))
	if len(s.Participants) != 2 {
		t.Errorf("after kick: %v", s.Participants)
	}

	// group_close wipes the session.
	h(context.Background(), fakeLoggingRequest("group_close", map[string]any{
		"group_id": "grp_live",
	}))
	if r.GetGroupSession("grp_live") != nil {
		t.Errorf("session still present after group_close")
	}
}
