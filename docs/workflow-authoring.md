# Workflow Authoring Guide

Workflows are YAML files in `smartnpc-mcp/pkg/workflow/builtin/` that define
multi-step NPC routines. Each workflow bundles several tools with branching
logic, randomisation, and variable bindings — the engine runs them locally
without per-step LLM calls.

## Quick Start

```yaml
# pkg/workflow/builtin/my_workflow.yaml
id: my_workflow
description: 简要说明这个工作流做什么
version: "1"
inputs:
  - name: radius
    description: 巡视半径
    default: 10
steps:
  - kind: tool
    name: npc_inspect_object
    args:
      what: farm_actions
      radius: "$radius"
    save_as: obs

  - kind: branch
    when: "$obs.actions_available.water.count > 0"
    then:
      - kind: tool
        name: npc_water_crops
        args:
          x1: "$obs.actions_available.water.bbox.x1"
          y1: "$obs.actions_available.water.bbox.y1"
          x2: "$obs.actions_available.water.bbox.x2"
          y2: "$obs.actions_available.water.bbox.y2"
        on_nothing_to_do: skip
```

## Step Types

### tool — Call an MCP tool

```yaml
kind: tool
name: npc_water_crops          # MCP tool name
args:                           # arguments ($refs resolved at runtime)
  radius: 5
  x1: "$obs.water.bbox.x1"
save_as: obs                    # bind output as $obs
on_nothing_to_do: skip          # skip | stop | fail (default: skip)
```

### branch — If/else

```yaml
kind: branch
when: "$obs.water.count > 0"    # expression; truthy → then, falsy → else
then: [ ... steps ... ]
else: [ ... steps ... ]         # optional
```

### random — Weighted pick

```yaml
kind: random
weighted:
  - weight: 3
    do: [ ... steps ... ]       # picked 3/6 of the time
  - weight: 2
    do: [ ... steps ... ]       # picked 2/6 of the time
  - weight: 1
    do: []                       # picked 1/6 of the time (no-op)
```

### foreach — Iterate a list

```yaml
kind: foreach
over: "$obs.harvest.crops"      # list-valued variable
as: crop                         # each item bound as $crop
do: [ ... steps ... ]            # runs once per item
max_iter: 50                     # safety cap (default 50)
```

### skill_call — Trigger a Hermes SKILL

```yaml
kind: skill_call
skill: smartnpc-greeting         # SKILL name from hermes/profiles/_master/skills/
```

### llm_choice — Ask the LLM to pick

```yaml
kind: llm_choice
prompt: "Is it raining or sunny?"
options: ["rain", "sun"]
save_as: weather_choice
```

### wait — Pause for NPC idle

```yaml
kind: wait
condition: idle                  # only "idle" supported
timeout_seconds: 30              # default 30
```

### stop — End early

```yaml
kind: stop
reason: "no tillable area available"
```

## Expressions

Limited to path lookup, comparison, and boolean logic:

| Expression | Meaning |
|-----------|---------|
| `$obs.water.count` | Variable path lookup |
| `$obs.water.count > 0` | Numeric comparison |
| `$season == "fall"` | String comparison |
| `$a > 0 && $b > 0` | Boolean AND |
| `$a > 0 \|\| $b > 0` | Boolean OR |
| `!$done` | Boolean NOT |
| `true`, `false`, `nil` | Literals |

**Truthiness:** nil=false, non-zero numbers=true, non-empty strings=true,
non-empty lists/maps=true.

## Inputs

Declare optional named arguments:

```yaml
inputs:
  - name: target_seed
    description: 种子 ID
    default: "(O)472"           # used when caller doesn't supply
```

Callers supply via `args` in `npc_plan_day`:
```json
{"workflow_id": "farm_morning_round", "args": {"target_seed": "(O)490"}}
```

## Validation

Run the lint tool before committing:

```bash
go run ./cmd/workflow-lint/
```

Checks:
- All steps have valid `kind` and required fields
- Tool names match known MCP tools
- Skill names match known Hermes SKILLs
- No duplicate `save_as` in same scope
- Variable references point to declared inputs or prior `save_as` bindings

## Runtime Override

Set `SMARTNPC_WORKFLOW_DIR` to load custom workflows that replace built-in
ones without rebuilding:

```powershell
$env:SMARTNPC_WORKFLOW_DIR = "D:\SmartNPC\custom-workflows"
```
