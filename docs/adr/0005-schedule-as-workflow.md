# ADR-0005: Schedule entries driven by workflow engine

Date: 2026-06-17
Status: Draft (P1-P5 code landed; P6 cleanup in progress)

## Context

Scheduling was a single `action` field — each entry mapped 1:1 to an MCP
tool, and the LLM was invoked at *fire time* to pick tool parameters:

```
schedule_trigger event → relay → Hermes Agent → LLM picks tool params → calls tool
```

This had several problems:
- **Low expressiveness**: entries were single-tool; multi-step combos required
  LLM improvisation at fire time.
- **Expensive per-step LLM calls**: every step cost a full Hermes API round-trip.
- **Hardcoded SKILLs**: farm_maintenance / farm_harvest could not be
  parameterised.
- **Unstable behavior**: LLM decided parameters freshly each time; no
  determinism.
- **Poor debuggability**: only tool-level logs; no step-level state.

## Decision

Replace `action` with `workflow_id` (references a YAML-defined multi-step
workflow) or `workflow` (inline definition). The workflow engine runs steps
locally in `smartnpc-mcp`, only calling the LLM for `llm_choice` and
`skill_call` steps:

```
schedule fires → mcp workflow engine:
  for each step:
    tool       → ws call to mod (no LLM)
    branch     → local expression eval (no LLM)
    random     → local RNG (no LLM)
    llm_choice → single Hermes round-trip
    skill_call → fire-and-forget through relay
```

**Key invariants preserved:**
1. Mod handlers unchanged — all `npc_*` tools keep existing protocol.
2. `NpcActionQueue` serial model unchanged — workflow steps call through the
   same queue.
3. `bbox` / `TSP` / `nothing_to_do` / existing farm_actions unaffected.

## Phased rollout

| Phase | Scope | Status |
|-------|-------|--------|
| P1 | DSL types + engine + tests (14 tests) | ✅ |
| P2 | YAML registry + `workflow_list/get/run_inline` tools | ✅ |
| P3 | `Entry` three-form compatibility + `normalizeEntry` | ✅ |
| P4 | `MCPRunner` + `npcWorkflowWorker` + `SMARTNPC_WORKFLOW_PUMP` | ✅ |
| P5 | Run history JSONL + `workflow_run_history` tool | ✅ |
| P6 | Lint + docs + deprecation (this ADR) | ✅ |

## Consequences

**Positive:**
- 4-10× fewer LLM calls per schedule trigger (most steps are deterministic).
- Workflows are human-readable YAML; CI-validated via `workflow-lint`.
- LLM only needs to pick *which workflow to schedule*, not how to execute it.
- Step-level logging with branch decisions and variable bindings.

**Negative:**
- New YAML DSL to learn (mitigated by `workflow-lint` and authoring guide).
- Engine lives in mcp (mod stays thin), but mcp must be online for schedules
  to run — already true for Hermes-first architecture.
- `llm_choice` steps still incur LLM latency; workflows should minimise them.

## Alternatives considered

| Alternative | Rejected because |
|-------------|-----------------|
| Keep `action` + LLM at fire time | Too expensive, unstable behavior |
| Full Turing-complete script language | Debugging nightmare; no CI validation |
| Move engine to mod (C#) | Mod stays thin by design; Go ecosystem richer for YAML/validation |
| Cel-go / expr-lang for expressions | Heavy dependency; we only need path lookup + comparison |
