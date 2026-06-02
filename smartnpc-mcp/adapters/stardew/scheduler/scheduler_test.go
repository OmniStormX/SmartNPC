package scheduler

import (
	"testing"
)

func TestScheduler_SetAndGet(t *testing.T) {
	s := New()

	sched := DaySchedule{
		NPC:    "XiaMi",
		Day:    15,
		Season: "spring",
		Year:   1,
		Entries: []Entry{
			{GameHour: 7, Action: "npc_water_crops", Reason: "早起浇水"},
			{GameHour: 9, Action: "npc_wander", Reason: "逛逛镇子"},
			{GameHour: 18, Action: "npc_approach_and_speak", Reason: "找玩家聊天"},
		},
	}
	s.SetSchedule(sched)

	got := s.GetSchedule("XiaMi")
	if got == nil {
		t.Fatal("expected non-nil schedule")
	}
	if got.NPC != "XiaMi" || got.Season != "spring" || len(got.Entries) != 3 {
		t.Errorf("schedule mismatch: %+v", got)
	}
}

func TestScheduler_GetReturnsNilForUnknown(t *testing.T) {
	s := New()
	if got := s.GetSchedule("Nobody"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestScheduler_Tick_FiresMatchingEntries(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{
		NPC: "XiaMi",
		Entries: []Entry{
			{GameHour: 7, Action: "npc_water_crops"},
			{GameHour: 9, Action: "npc_wander"},
			{GameHour: 7, Action: "npc_clear_debris"},
		},
	})

	fired := s.Tick(7)
	if len(fired) != 2 {
		t.Fatalf("expected 2 fired entries at hour 7, got %d", len(fired))
	}
	for _, f := range fired {
		if f.NPC != "XiaMi" || f.GameHour != 7 {
			t.Errorf("unexpected fired entry: %+v", f)
		}
	}

	// Tick again at same hour — should not re-fire
	fired2 := s.Tick(7)
	if len(fired2) != 0 {
		t.Errorf("expected 0 re-fires, got %d", len(fired2))
	}

	// Tick at hour 9
	fired9 := s.Tick(9)
	if len(fired9) != 1 || fired9[0].Action != "npc_wander" {
		t.Errorf("hour 9 fires: %+v", fired9)
	}
}

func TestScheduler_Tick_MultipleNPCs(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{
		NPC:     "XiaMi",
		Entries: []Entry{{GameHour: 8, Action: "npc_idle_activity"}},
	})
	s.SetSchedule(DaySchedule{
		NPC:     "Abigail",
		Entries: []Entry{{GameHour: 8, Action: "npc_wander"}},
	})

	fired := s.Tick(8)
	if len(fired) != 2 {
		t.Fatalf("expected 2 fired (one per NPC), got %d", len(fired))
	}
	names := map[string]bool{}
	for _, f := range fired {
		names[f.NPC] = true
	}
	if !names["XiaMi"] || !names["Abigail"] {
		t.Errorf("expected both NPCs fired, got %v", names)
	}
}

func TestScheduler_ClearAll(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{NPC: "XiaMi", Entries: []Entry{{GameHour: 7, Action: "a"}}})
	s.SetSchedule(DaySchedule{NPC: "Abigail", Entries: []Entry{{GameHour: 7, Action: "b"}}})
	s.ClearAll()

	if got := s.GetSchedule("XiaMi"); got != nil {
		t.Error("expected nil after ClearAll")
	}
	if got := s.GetSchedule("Abigail"); got != nil {
		t.Error("expected nil after ClearAll")
	}
}

func TestScheduler_ClearNPC(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{NPC: "XiaMi", Entries: []Entry{{GameHour: 7, Action: "a"}}})
	s.SetSchedule(DaySchedule{NPC: "Abigail", Entries: []Entry{{GameHour: 7, Action: "b"}}})
	s.ClearNPC("XiaMi")

	if got := s.GetSchedule("XiaMi"); got != nil {
		t.Error("expected nil for XiaMi after ClearNPC")
	}
	if got := s.GetSchedule("Abigail"); got == nil {
		t.Error("expected non-nil for Abigail")
	}
}

func TestScheduler_PendingEntries(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{
		NPC: "XiaMi",
		Entries: []Entry{
			{GameHour: 7, Action: "a"},
			{GameHour: 9, Action: "b"},
			{GameHour: 12, Action: "c"},
		},
	})

	// Fire hour 7
	s.Tick(7)

	pending := s.PendingEntries("XiaMi")
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0].Action != "b" || pending[1].Action != "c" {
		t.Errorf("unexpected pending: %+v", pending)
	}
}

func TestScheduler_AllNPCs(t *testing.T) {
	s := New()
	s.SetSchedule(DaySchedule{NPC: "XiaMi"})
	s.SetSchedule(DaySchedule{NPC: "Abigail"})

	npcs := s.AllNPCs()
	if len(npcs) != 2 {
		t.Fatalf("expected 2, got %d", len(npcs))
	}
}
