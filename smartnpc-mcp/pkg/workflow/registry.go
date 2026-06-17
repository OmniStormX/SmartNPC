package workflow

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// Registry holds named workflow definitions. Thread-safe for reads; Init
// writes once at startup and should not be called concurrently with reads
// afterward.
type Registry struct {
	mu   sync.RWMutex
	defs map[string]*Definition
}

// NewRegistry returns an empty, un-initialised Registry. Call Init before
// using Get/List.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]*Definition{}}
}

// Init scans embedded builtin/ YAML files, then overlays any YAML files found
// under extraDir (when non-empty). Duplicate IDs cause an immediate error;
// overlay semantics: a file in extraDir with the same ID replaces the
// built-in one entirely.
func (r *Registry) Init(extraDir string) error {
	if err := r.loadEmbedded(); err != nil {
		return fmt.Errorf("workflow registry: embedded: %w", err)
	}
	if extraDir != "" {
		if err := r.loadDir(extraDir); err != nil {
			return fmt.Errorf("workflow registry: extra dir %q: %w", extraDir, err)
		}
	}
	return nil
}

// Get returns the Definition for id, or nil when unknown.
func (r *Registry) Get(id string) *Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defs[id]
}

// List returns every loaded Definition. The slice is a shallow copy — callers
// must not mutate the returned definitions.
func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	return out
}

// ── loading ──────────────────────────────────────────────────────────────

func (r *Registry) loadEmbedded() error {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return fmt.Errorf("read builtin dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		def, err := LoadDef(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := r.store(def); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func (r *Registry) loadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// Track IDs seen within this directory scan so we catch duplicates inside
	// the same dir (overlay from extraDir only replaces built-in IDs).
	seenInDir := map[string]string{} // id → filename
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		def, err := LoadDef(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if prevName, ok := seenInDir[def.ID]; ok {
			return fmt.Errorf("duplicate workflow id %q in %q and %q", def.ID, prevName, e.Name())
		}
		seenInDir[def.ID] = e.Name()
		// Overlay: replace built-in with same ID silently.
		if prev := r.defs[def.ID]; prev != nil {
			r.mu.Lock()
			r.defs[def.ID] = def
			r.mu.Unlock()
			continue
		}
		if err := r.store(def); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func (r *Registry) store(def *Definition) error {
	if _, exists := r.defs[def.ID]; exists {
		return fmt.Errorf("duplicate workflow id %q", def.ID)
	}
	r.mu.Lock()
	r.defs[def.ID] = def
	r.mu.Unlock()
	return nil
}

// ── YAML helpers ─────────────────────────────────────────────────────────

// LoadDef parses YAML bytes into a Definition and runs Validate.
func LoadDef(raw []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if err := Validate(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

// ── Validate ─────────────────────────────────────────────────────────────

// Validate runs static checks on a definition. It catches schema-level issues
// (unknown step kind, missing required fields, empty branch/random/foreach
// bodies, duplicate save_as in same scope) early so they surface at startup
// rather than mid-run.
func Validate(def *Definition) error {
	if def == nil {
		return errors.New("nil definition")
	}
	if def.ID == "" {
		return errors.New("missing id")
	}
	if len(def.Steps) == 0 {
		return errors.New("steps must not be empty")
	}
	return validateSteps(def.Steps, newValidateCtx())
}

type validateCtx struct {
	saveAs map[string]bool // tracks save_as names in the current scope
}

func newValidateCtx() *validateCtx {
	return &validateCtx{saveAs: map[string]bool{}}
}

func (c *validateCtx) child() *validateCtx {
	child := newValidateCtx()
	for k := range c.saveAs {
		child.saveAs[k] = true
	}
	return child
}

func validateSteps(steps []Step, ctx *validateCtx) error {
	for i := range steps {
		if err := validateStep(&steps[i], ctx); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}
	return nil
}

func validateStep(s *Step, ctx *validateCtx) error {
	switch s.Kind {
	case StepKindTool:
		t := s.Tool
		if t == nil || t.Name == "" {
			return errors.New("tool step missing name")
		}
		if t.SaveAs != "" {
			if ctx.saveAs[t.SaveAs] {
				return fmt.Errorf("duplicate save_as %q in scope", t.SaveAs)
			}
			ctx.saveAs[t.SaveAs] = true
		}
		policy := strings.ToLower(strings.TrimSpace(t.OnNothingToDo))
		if policy != "" && policy != "skip" && policy != "stop" && policy != "fail" {
			return fmt.Errorf("unknown on_nothing_to_do %q", t.OnNothingToDo)
		}

	case StepKindBranch:
		b := s.Branch
		if b == nil {
			return errors.New("branch step missing payload")
		}
		if b.When == "" {
			return errors.New("branch step missing when")
		}
		if len(b.Then) == 0 && len(b.Else) == 0 {
			return errors.New("branch step has empty then and else")
		}
		if len(b.Then) > 0 {
			if err := validateSteps(b.Then, ctx.child()); err != nil {
				return fmt.Errorf("branch.then: %w", err)
			}
		}
		if len(b.Else) > 0 {
			if err := validateSteps(b.Else, ctx.child()); err != nil {
				return fmt.Errorf("branch.else: %w", err)
			}
		}

	case StepKindRandom:
		r := s.Random
		if r == nil || len(r.Weighted) == 0 {
			return errors.New("random step has no weighted branches")
		}
		hasPositive := false
		for j, w := range r.Weighted {
			if w.Weight < 0 {
				return fmt.Errorf("random branch %d has negative weight %f", j, w.Weight)
			}
			if w.Weight > 0 {
				hasPositive = true
			}
			// Empty Do is legitimate — it means "pick this branch and do nothing".
			if len(w.Do) > 0 {
				if err := validateSteps(w.Do, ctx.child()); err != nil {
					return fmt.Errorf("random branch %d: %w", j, err)
				}
			}
		}
		if !hasPositive {
			return errors.New("random step has no branch with weight > 0")
		}

	case StepKindForEach:
		f := s.ForEach
		if f == nil || f.Over == "" {
			return errors.New("foreach step missing over")
		}
		if f.As == "" {
			return errors.New("foreach step missing as")
		}
		if len(f.Do) == 0 {
			return errors.New("foreach step body is empty")
		}
		child := ctx.child()
		if err := validateSteps(f.Do, child); err != nil {
			return fmt.Errorf("foreach body: %w", err)
		}

	case StepKindSkillCall:
		sc := s.SkillCall
		if sc == nil || sc.Skill == "" {
			return errors.New("skill_call step missing skill")
		}
		// Detect trivial self-reference: a workflow that calls itself.
		// Not a deep cycle check — just the most common mistake.
		// Full cycle detection would need the registry, deferred to P6 lint.
		if sc.Skill == "workflow_run_inline" || sc.Skill == "workflow_list" || sc.Skill == "workflow_get" {
			return fmt.Errorf("skill_call references MCP tool %q — use workflow_run_inline tool instead", sc.Skill)
		}

	case StepKindLLMChoice:
		l := s.LLMChoice
		if l == nil || l.SaveAs == "" {
			return errors.New("llm_choice step missing save_as")
		}
		if len(l.Options) == 0 {
			return errors.New("llm_choice step has no options")
		}
		if ctx.saveAs[l.SaveAs] {
			return fmt.Errorf("duplicate save_as %q in scope", l.SaveAs)
		}
		ctx.saveAs[l.SaveAs] = true

	case StepKindWait:
		w := s.Wait
		if w == nil {
			return errors.New("wait step missing payload")
		}
		cond := strings.ToLower(strings.TrimSpace(w.Condition))
		if cond != "" && cond != "idle" {
			return fmt.Errorf("wait step unknown condition %q", w.Condition)
		}

	case StepKindStop:
		// Always valid — even with empty reason.

	default:
		return fmt.Errorf("unknown step kind %q", s.Kind)
	}
	return nil
}

// ── YAML unmarshaling for Step (tagged union) ────────────────────────────
//
// Because gopkg.in/yaml.v3 does not support ,inline, we decode Step manually:
// read `kind`, then dispatch the rest to the matching sub-struct.

type stepRaw struct {
	Kind  string `yaml:"kind"`
	Label string `yaml:"label,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler for Step. It reads the `kind`
// discriminator and populates the matching sub-struct.
func (s *Step) UnmarshalYAML(value *yaml.Node) error {
	// Read kind first.
	var raw stepRaw
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.Kind == "" {
		return errors.New("step missing kind")
	}
	s.Kind = StepKind(raw.Kind)
	s.Label = raw.Label

	switch s.Kind {
	case StepKindTool:
		s.Tool = &ToolStep{}
		return value.Decode(s.Tool)
	case StepKindBranch:
		s.Branch = &BranchStep{}
		return value.Decode(s.Branch)
	case StepKindRandom:
		s.Random = &RandomStep{}
		return value.Decode(s.Random)
	case StepKindForEach:
		s.ForEach = &ForEachStep{}
		return value.Decode(s.ForEach)
	case StepKindSkillCall:
		s.SkillCall = &SkillCallStep{}
		return value.Decode(s.SkillCall)
	case StepKindLLMChoice:
		s.LLMChoice = &LLMChoiceStep{}
		return value.Decode(s.LLMChoice)
	case StepKindWait:
		s.Wait = &WaitStep{}
		return value.Decode(s.Wait)
	case StepKindStop:
		s.Stop = &StopStep{}
		return value.Decode(s.Stop)
	default:
		return fmt.Errorf("unknown step kind %q", raw.Kind)
	}
}

