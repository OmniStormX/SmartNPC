package chat

import (
	"testing"
)

// ── HasMoveIntent ────────────────────────────────────────────

func TestHasMoveIntent(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Chinese positives
		{"走到农场左边", true},
		{"去湖边", true},
		{"陪我去房子前面吧", true},
		{"你去大门看看", true},
		{"过来", true},
		{"前往温室", true},
		{"到那边去", true},
		// English positives (case-insensitive)
		{"go to the lake", true},
		{"GO TO the gate", true},
		{"let's go to the barn", true},
		{"lets go check the coop", true},
		{"move to the greenhouse", true},
		{"walk to the lake", true},
		{"head to the gate", true},
		// Negatives
		{"", false},
		{"hello there", false},
		{"你今天过得怎么样", false},
		{"我刚吃了饭", false},
		{"the weather is nice", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			got := HasMoveIntent(c.in)
			if got != c.want {
				t.Errorf("HasMoveIntent(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// ── LookupAlias ──────────────────────────────────────────────

func TestLookupAlias_LongestMatchWins(t *testing.T) {
	// The default table has "房" inside aliases like "房子前面"/"房子"/"家门口"/"门口".
	// "房子前面" is 4 runes → should beat "房子" (2 runes) when both appear in the input.
	tbl := NewLocationTable(DefaultLocations())

	cases := []struct {
		in       string
		wantName string // empty means no match
	}{
		{"我想去房子前面", "房子前面"},
		{"陪我到湖边坐一会", "湖边"},
		{"走到大门", "大门"},
		{"去温室看看", "温室"},
		{"let's head to the barn", "畜棚"},
		{"go to the coop", "鸡舍"},
		{"check out the greenhouse please", "温室"},
		{"来左边", "农场左边"},
		{"nothing relevant here", ""},
		{"", ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			loc := tbl.LookupAlias(c.in)
			if c.wantName == "" {
				if loc != nil {
					t.Errorf("LookupAlias(%q) = %+v, want nil", c.in, loc)
				}
				return
			}
			if loc == nil {
				t.Fatalf("LookupAlias(%q) = nil, want %q", c.in, c.wantName)
			}
			if loc.Name != c.wantName {
				t.Errorf("LookupAlias(%q) = %q, want %q", c.in, loc.Name, c.wantName)
			}
		})
	}
}

func TestLookupAlias_CaseInsensitive(t *testing.T) {
	tbl := NewLocationTable(DefaultLocations())
	loc := tbl.LookupAlias("GO TO THE LAKE")
	if loc == nil || loc.Name != "湖边" {
		t.Fatalf("case-insensitive match failed: %+v", loc)
	}
}

func TestLookupAlias_NilTable(t *testing.T) {
	var tbl *LocationTable
	if tbl.LookupAlias("go to the lake") != nil {
		t.Error("nil table should return nil")
	}
}

// ── DetectMoveIntent ─────────────────────────────────────────

func TestDetectMoveIntent(t *testing.T) {
	tbl := NewLocationTable(DefaultLocations())

	t.Run("no intent, no location", func(t *testing.T) {
		mi := tbl.DetectMoveIntent("hello there")
		if mi.HasIntent || mi.Location != nil {
			t.Errorf("unexpected: %+v", mi)
		}
	})

	t.Run("intent without location", func(t *testing.T) {
		mi := tbl.DetectMoveIntent("过来啊")
		if !mi.HasIntent {
			t.Error("expected HasIntent=true")
		}
		if mi.Location != nil {
			t.Errorf("expected no location, got %+v", mi.Location)
		}
	})

	t.Run("intent with location", func(t *testing.T) {
		mi := tbl.DetectMoveIntent("陪我去湖边")
		if !mi.HasIntent {
			t.Fatal("expected HasIntent=true")
		}
		if mi.Location == nil || mi.Location.Name != "湖边" {
			t.Errorf("expected 湖边, got %+v", mi.Location)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		mi := tbl.DetectMoveIntent("")
		if mi.HasIntent || mi.Location != nil {
			t.Errorf("empty input should yield zero value, got %+v", mi)
		}
	})
}

// ── custom location override ─────────────────────────────────

func TestLookupAlias_CustomLocations(t *testing.T) {
	tbl := NewLocationTable([]NamedLocation{
		{Name: "秘密基地", Aliases: []string{"秘密基地", "basement"}, Map: "Cellar", X: 5, Y: 5},
	})
	loc := tbl.LookupAlias("带我去秘密基地")
	if loc == nil || loc.Name != "秘密基地" || loc.Map != "Cellar" {
		t.Fatalf("custom location not found: %+v", loc)
	}
	// An alias that's not in the custom table should miss.
	if tbl.LookupAlias("go to the lake") != nil {
		t.Error("built-in aliases must not leak when custom table is provided")
	}
}

// ── top-level DetectMoveIntent (FarmLocations) ───────────────

// TestDetectMoveIntent_TableDriven exercises the package-level
// DetectMoveIntent(text) → (bool, *NamedLocation) helper that scans against
// FarmLocations. It covers Chinese + English move phrasings against the
// canonical Farm landmark set.
func TestDetectMoveIntent_TableDriven(t *testing.T) {
	cases := []struct {
		in       string
		wantHit  bool
		wantName string // "" when hit is true but no location match expected
	}{
		// Chinese hits
		{"走到农场左边", true, "农场左边"},
		{"去湖边", true, "湖边"},
		{"你能去温室吗", true, "温室"},
		{"能不能到大门", true, "大门"},
		{"帮我去房子前面", true, "房子前面"},
		{"移动到畜棚", true, "畜棚"},
		{"陪我去鸡舍", true, "鸡舍"},
		{"过去看看", true, ""}, // intent without a known landmark
		// English hits
		{"go to the lake", true, "湖边"},
		{"MOVE TO the gate", true, "大门"},
		{"let's go to the barn", true, "畜棚"},
		{"walk to the greenhouse", true, "温室"},
		{"head to the coop", true, "鸡舍"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			hit, loc := DetectMoveIntent(c.in)
			if hit != c.wantHit {
				t.Fatalf("hit = %v, want %v", hit, c.wantHit)
			}
			if c.wantName == "" {
				if loc != nil {
					t.Errorf("loc = %+v, want nil", loc)
				}
				return
			}
			if loc == nil {
				t.Fatalf("loc is nil, want %q", c.wantName)
			}
			if loc.Name != c.wantName {
				t.Errorf("loc.Name = %q, want %q", loc.Name, c.wantName)
			}
		})
	}
}

// TestDetectMoveIntent_NoMatch verifies normal conversation does not
// accidentally trigger the move-intent path.
func TestDetectMoveIntent_NoMatch(t *testing.T) {
	cases := []string{
		"",
		"hello there",
		"你今天过得怎么样",
		"我刚吃了饭",
		"the weather is nice today",
		"别担心我",
		"你吃饭了吗",
	}
	for _, in := range cases {
		in := in
		t.Run(in, func(t *testing.T) {
			hit, loc := DetectMoveIntent(in)
			if hit {
				t.Errorf("DetectMoveIntent(%q) hit=true, want false", in)
			}
			if loc != nil {
				t.Errorf("loc should be nil, got %+v", loc)
			}
		})
	}
}

// TestFarmLocations_Coverage asserts every spec'd landmark resolves via
// DetectMoveIntent so future alias edits can't silently drop a location.
func TestFarmLocations_Coverage(t *testing.T) {
	want := []string{"农场左边", "房子前面", "湖边", "大门", "温室", "畜棚", "鸡舍"}
	have := make(map[string]bool, len(FarmLocations))
	for _, l := range FarmLocations {
		have[l.Name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("FarmLocations missing %q", name)
		}
	}
}

// ── DetectBehaviorIntent ─────────────────────────────────────

// TestDetectBehaviorIntent_Classification exercises the package-level helper
// across every intent class plus several overlap edge-cases. Stop must win
// over follow/lead/summon when keywords overlap.
func TestDetectBehaviorIntent_Classification(t *testing.T) {
	cases := []struct {
		in        string
		wantKind  string
		wantLoc   string // canonical name; "" when no location expected
	}{
		// summon
		{"过来", "summon", ""},
		{"来这儿", "summon", ""}, // contains "来这"
		{"到我这来", "summon", ""},
		{"come here", "summon", ""},
		{"COME HERE now", "summon", ""},
		// follow
		{"跟着我", "follow", ""},
		{"跟我走", "follow", ""},
		{"follow me", "follow", ""},
		// lead with destination
		{"带我去湖边", "lead", "湖边"},
		{"你带我去大门", "lead", "大门"},
		{"lead me to the greenhouse", "lead", "温室"},
		// lead without destination
		{"带路", "lead", ""},
		{"lead the way", "lead", ""},
		// stop
		{"别跟了", "stop", ""},
		{"停下", "stop", ""},
		{"stop following me", "stop", ""},
		// stop takes precedence over follow when keywords overlap
		{"别跟着我走了", "stop", ""},
		// nothing
		{"", "", ""},
		{"你今天过得怎么样", "", ""},
		{"the weather is nice", "", ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			kind, loc := DetectBehaviorIntent(c.in)
			if kind != c.wantKind {
				t.Fatalf("intent = %q, want %q", kind, c.wantKind)
			}
			if c.wantLoc == "" {
				if loc != nil {
					t.Errorf("loc = %+v, want nil", loc)
				}
				return
			}
			if loc == nil {
				t.Fatalf("loc = nil, want %q", c.wantLoc)
			}
			if loc.Name != c.wantLoc {
				t.Errorf("loc.Name = %q, want %q", loc.Name, c.wantLoc)
			}
		})
	}
}

// TestDetectBehaviorIntent_CustomTable ensures persona-provided locations are
// honored when callers use the table-scoped variant.
func TestDetectBehaviorIntent_CustomTable(t *testing.T) {
	tbl := NewLocationTable([]NamedLocation{
		{Name: "秘密基地", Aliases: []string{"秘密基地"}, Map: "Cellar", X: 5, Y: 5},
	})

	kind, loc := tbl.DetectBehaviorIntent("带我去秘密基地")
	if kind != "lead" {
		t.Fatalf("intent = %q, want lead", kind)
	}
	if loc == nil || loc.Name != "秘密基地" || loc.Map != "Cellar" {
		t.Fatalf("loc = %+v, want 秘密基地/Cellar", loc)
	}

	// Built-in Farm aliases must not leak when the table is custom.
	kind2, loc2 := tbl.DetectBehaviorIntent("带我去湖边")
	if kind2 != "lead" {
		t.Errorf("lead keyword should still fire, got %q", kind2)
	}
	if loc2 != nil {
		t.Errorf("custom table must not resolve built-in alias 湖边, got %+v", loc2)
	}
}
