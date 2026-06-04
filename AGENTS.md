# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project overview

SmartNPC is a Hermes-first AI NPC runtime for Stardew Valley.

- `smapi-mod/`: C#/.NET 6 SMAPI mod. Owns in-game UI, NPC sprites, WebSocket server, game-thread-safe queries/actions, and thin SMAPI integration glue.
- `smartnpc-mcp/`: Go MCP server. Owns MCP tool schemas/handlers, WebSocket client bridge to the mod, validation, event formatting, and Hermes relay fan-out. It must not persist business state or call LLMs directly.
- `hermes/profiles/<npc>/`: NPC decision/personality layer. Each profile contains `SOUL.md`, skills, cron recipes, config overlays, and local memory/state.
- `docs/`: Architecture/protocol/tool/event documentation and manual verification reports.
- `scripts/`: Profile rendering, verification, launch, reset, and other helper scripts.

## Required workflow

Use `task` as the canonical local entry point for build/test/lint operations.

Common commands from repo root:

```powershell
task --list
task ci-fast        # lint + tests, skips build
task ci             # lint + tests + build
task tidy           # go mod tidy + go work sync
task clean

task mcp:build
task mcp:stop
task mcp:test
task mcp:run WS_URL=ws://127.0.0.1:18745/ws
task mcp:run-echo   # echo mode, no LLM
task mcp:health

task mod:build
task mod:install    # requires SMARTNPC_GAME_PATH

task profiles:render
task profiles:verify
task net:check
task hooks:enable
task hooks:status
```

Notes:

- `task ci` is the expected final verification after code changes when local prerequisites are available.
- C# mod builds require .NET 6 plus local Stardew Valley/SMAPI assemblies. GitHub-hosted CI does not build the mod because it cannot install Stardew Valley.
- `task mod:install` may stop a running Stardew Valley/SMAPI process to release locked DLLs.
- Keep `.env` local. Use `.env.example` for documented configuration.
- Prefer the explicit local task binary when reproducing Claude workflows:
  `C:\Users\synchen\go\bin\task.exe`.
- Do not use bare `go build` or `dotnet build` as the main verification path when a
  matching `task` target exists.

Local runtime sequence:

1. Close the game before `task mod:install`; SMAPI DLLs may be locked.
2. Start MCP before Hermes Gateway so Hermes discovers MCP tools at startup.
3. Start the game through `D:\Stardew Valley\StardewModdingAPI.exe`, not
   `Stardew Valley.exe`.
4. Use `run.bat` from the repo root for the recommended one-shot local launch flow.

Important environment variables from `.env.example`:

- `SMARTNPC_WS_URL`: SMAPI WebSocket URL, default `ws://127.0.0.1:18745/ws`.
- `SMARTNPC_HTTP_PORT`: MCP HTTP port, default `3000`.
- `SMARTNPC_MCP_URL`: full MCP URL for Hermes profiles, for example
  `http://127.0.0.1:3000/mcp`.
- `SMARTNPC_HERMES_MODE`: `docker` or `local`.
- `SMARTNPC_ACTIVE_PROFILES`: comma-separated NPC ids, for example
  `xiami,abigail`.
- `SMARTNPC_GAME_PATH`: Stardew Valley install directory.

## Response and collaboration style

Rules imported from `CLAUDE.md`:

- Reply in Chinese by default. Keep technical terms, API names, commands, and file
  paths in English with backticks where helpful.
- Lead with executable guidance. If comparing multiple approaches, use a table and
  recommend one path.
- Prefer parallel tool calls for independent reads, searches, lint checks, and file
  inspections.
- Avoid narration comments in code.
- After edits, report a 1-3 line summary with key file paths. Do not paste code
  unless the user asks.

## Claude-derived agent workflows

The `.claude/agents/` directory defines four reusable workflows. Apply the closest
one when the user's request matches the role.

### Planning workflow (`.claude/agents/plan.md`)

Use for architecture or implementation planning.

1. Clarify the goal and ask targeted questions only when requirements are ambiguous.
2. Explore relevant code, docs, ADRs, and `CLAUDE.md`.
3. Produce an actionable implementation plan with concrete file paths, interface or
   struct signatures when needed, text data-flow diagrams, risks, and open questions.
4. Estimate effort as S/M/L and identify dependencies between steps.
5. Deliver a numbered plan ready for handoff to implementation.

Rules:

- Do not write implementation code in planning mode.
- Respect boundaries: `pkg/` is game-agnostic framework code;
  `adapters/stardew/` is Stardew-specific code.
- Include the test strategy in every implementation plan.

### Coding workflow (`.claude/agents/coding.md`)

Use for feature work, refactors, and direct implementation requests.

1. Understand the requirement and identify files to change.
2. Implement following local architecture and coding conventions.
3. Add or update tests.
4. Run `C:\Users\synchen\go\bin\task.exe ci-fast` for normal implementation
   verification.
5. If verification fails, fix and rerun, up to 3 attempts before reporting a blocker.
6. Report changes in 1-3 lines with key paths.

### Bug-fix workflow (`.claude/agents/fix-bug.md`)

Use for defects, failing tests, regressions, or unexpected behavior.

1. Reproduce or trace the symptom through the relevant code path.
2. Identify the exact root cause and why it fails.
3. Apply the narrowest correct fix; avoid unrelated refactors.
4. For Go bugs, run the focused `go test -run TestXxx ./path/...` first when
   practical, then `C:\Users\synchen\go\bin\task.exe ci-fast`.
5. For C# bugs, inspect `smapi-mod/` and verify with `task mod:build` when local
   prerequisites are available.
6. If the issue cannot be reproduced or fixed within 3 attempts, stop and report
   findings.

Never suppress errors or add `// nolint` merely to hide failures.

### Check workflow (`.claude/agents/check.md`)

Use for code review, quality gates, or explicit "check/review" requests.

1. Identify the change scope via user direction or `git diff`.
2. Review correctness, conventions, architecture, test coverage, and security.
3. Run `C:\Users\synchen\go\bin\task.exe ci` when local prerequisites are available.
4. Report a PASS/FAIL verdict, then issues categorized as bug, convention,
   performance, security, or test-gap.

Findings should cite file paths and line numbers. Use severity levels:
CRITICAL, WARNING, INFO.

## Claude-derived command SOPs

### CI doctor (`.claude/commands/ci-doctor.md`)

Trigger on requests such as "看 CI", "check ci", "ci 怎么样了", or "Actions 挂了".

1. Fetch the latest run:

   ```cmd
   python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1
   ```

2. If the run succeeded, report PASS with the run URL.
3. If it failed, was cancelled, or timed out, inspect failed logs:

   ```cmd
   gh run view <runId> --log-failed
   ```

4. Categorize failures as compile, test, lint, dependency, environment, workflow, or
   flake.
5. Report the run URL, failed job, category, root cause, and suggested fix.
6. Fix directly only when the change is small and clear; otherwise ask before larger
   repairs.
7. After fixing: run `task ci`, commit only if explicitly requested, push only if
   explicitly requested, then re-check CI.

Do not guess without logs. Do not hide failures by changing workflow behavior,
adding `[skip ci]`, using `--no-verify`, or force-pushing.

### Project launcher (`.claude/commands/project-launcher.md`)

Trigger on requests such as "启动项目", "跑起来", "启动 mcp", or "run server".

Prechecks:

- Check `go version`; Go should be at least 1.22.
- Check `C:\Users\synchen\go\bin\task.exe --version` when needed.

Launch sequence:

1. Build and verify with `C:\Users\synchen\go\bin\task.exe ci`.
2. Start MCP HTTP mode with `task mcp:run`, or manually:

   ```cmd
   smartnpc-mcp\bin\smartnpc-mcp.exe --http :3000 ^
     --ws-url ws://127.0.0.1:18745/ws ^
     --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
     --log-level debug
   ```

3. Verify `http://127.0.0.1:3000/healthz`.
4. Start Hermes gateways from WSL, for example:

   ```bash
   bash scripts/start_hermes_profiles.sh xiami,abigail
   ```

5. The user launches Stardew Valley through
   `D:\Stardew Valley\StardewModdingAPI.exe`.

Use `run.bat` from the repo root for the one-shot local flow when appropriate.

Key constraints:

- MCP runs on Windows; Hermes may run in WSL.
- Do not start the game through `Stardew Valley.exe`; mods require SMAPI.
- The same WebSocket bridge should only be occupied by one MCP instance.

### Pixel farm chat UI skill (`.claude/commands/pixel-farm-chat-ui.md`)

Trigger on requests for Stardew-like, cozy farming game, pixel-art chat, social
panel, skills/talents page, UI prompt, UI spec, or frontend-ready UI schemas.

Use original "cozy pixel-farm RPG" direction:

- Warm parchment panels, chunky wooden frames, amber shadows, green accents, red
  hearts, and readable pixel-style text.
- Original characters, icons, labels, and layouts. Do not copy official Stardew
  Valley assets, logos, screenshots, sprites, characters, or exact layouts.
- Chat UI should usually include header, friend list, relationship hearts, active
  conversation, message history, gift action, timestamps, input bar, and send action.
- Skills UI should usually include player identity, six skill rows, ten-slot level
  bars, talent cards, total skill level, and navigation icons.
- Link chat and skills mechanically when designing both systems together.

When producing a markdown UI spec, prefer sections for overview, visual direction,
screen structure, components, interaction states, sample content, data model, image
prompt, and quality checklist.

## Claude local settings and rule inventory

Imported from `.claude/settings.local.json`:

- Claude's local permissions allow web lookup for Hermes documentation on
  `hermesagent.org.cn`, general web search, WSL commands, Windows `cmd` commands,
  selected `curl` fetches, and reads under `/tmp`.
- These permissions describe the Claude Code setup only. Other agents must still
  obey their active runtime permissions and approval policy.

Inventory note:

- This repository currently has `.claude/agents/`, `.claude/commands/`, and
  `.claude/settings.local.json`.
- No standalone `.claude/skills/` or `.claude/rules/` directory was present when
  this file was updated.
- The closest imported "skill" is the `pixel-farm-chat-ui` command, documented
  above as a triggerable UI workflow.

## Coding conventions

### Cross-cutting architecture

- Keep SMAPI-specific glue in `smapi-mod/`; put reusable protocol/business logic in Go when possible.
- `smartnpc-mcp` is a protocol/tool boundary: WebSocket <-> MCP plus Hermes outbound relay. Do not add long-lived business persistence there.
- Hermes profiles carry LLM personality, policy, and decision behavior. Do not bypass profiles by adding direct LLM calls in MCP or the mod.
- Maintain the one-MCP-client assumption for the SMAPI WebSocket bridge; avoid changes that encourage multiple `smartnpc-mcp` instances competing for the mod connection.
- `smartnpc-mcp/pkg/` is the game-agnostic agent-bridge framework.
- `smartnpc-mcp/adapters/stardew/` is the Stardew Valley adapter layer.
- LLM provider access is routed through Hermes Gateway; use `.env` values such as
  `OPENAI_BASE_URL` as the local source of truth.

### Go (`smartnpc-mcp/`)

- Module path: `github.com/OmniStormX/SmartNPC`.
- Go version is 1.25+ per `go.mod`.
- Keep Stardew MCP tools under `adapters/stardew/tools/`; register tools through
  that package's `registry.go` / `RegisterAll`.
- Framework-level generic tools, such as `ping`, belong in `pkg/agentbridge/`, not
  the Stardew adapter.
- For stdio MCP mode, logs must go to stderr. Do not use stdout logging that can corrupt the MCP protocol stream.
- Wrap errors with context using `fmt.Errorf("...: %w", err)`.
- Use English package comments, exported symbol comments, and TODO/FIXME milestone notes.
- Run `gofmt`/`go test` through `task` where practical.

MCP tool conventions:

- Tool names use lowercase snake case: `<domain>_<verb>`.
- Keep one domain per file where practical.
- Input and Output structs need both `json` and `jsonschema` tags.
- Output structs should have `OK bool` as the first field.
- Handler first return value should be `nil` when the SDK should fill structured
  output from the Output struct.
- New tools must update `docs/protocol.md`.

### C# (`smapi-mod/`)

- Target framework is `net6.0`; nullable reference types are enabled.
- Prefer thin SMAPI event/API adapters and isolate pure logic where it can be tested or reasoned about without the game runtime.
- Respect game-thread safety for all Stardew/SMAPI interactions.

### Hermes profiles

- Shared profile templates live in `hermes/profiles/_master/`; rendered NPC profiles live under `hermes/profiles/<npc>/`.
- `hermes/npcs.yaml` is the single source of truth for NPC metadata such as id,
  game name, display name, gateway port, and peer relationships.
- `SOUL.md` files are handwritten per NPC. Shared skills, cron recipes, and overlays
  are rendered from `_master/`.
- After editing `_master/` or regenerated profile content, run:

```bash
bash scripts/render_profiles.sh
bash scripts/test_profile_render.sh
```

or from repo root:

```powershell
task profiles:verify
```

- Avoid placeholder leaks and XiaMi-specific leaks in non-XiaMi profiles.

## Testing expectations

- New Go packages should include at least one `*_test.go` with a `Test*` function.
- New MCP tools need end-to-end coverage using the in-memory MCP transport pattern: register tools, list tools, call the tool, and assert structured content.
- Prefer table-driven Go tests for variants.
- Avoid sleeps over 100 ms in unit tests; use channels or eventually-style polling for async behavior.
- Do not rely on real MCP subprocesses or real WebSocket connections in unit tests; use in-memory transport and mocks.
- If a verification command fails, fix and rerun. If the same failure cannot be resolved after repeated attempts, stop and report the blocker clearly.
- Name Go tests as `Test<Func>_<Scenario>` when practical, and prefer
  table-driven tests with `t.Run`.
- For normal implementation changes, `task ci-fast` is acceptable while iterating.
  Before final completion of code changes, run `task ci` when local prerequisites are
  available.

## Documentation and files

- Keep text files UTF-8. This repo contains Chinese documentation; avoid rewriting files with a legacy Windows encoding.
- Chinese text files should be UTF-8 without BOM. After writing Chinese-heavy files,
  a useful sanity check is:

  ```powershell
  python -c "open(r'PATH','rb').read().decode('utf-8')"
  ```

- Do not commit generated build outputs (`bin/`, `obj/`, `logs/`, `.gotmp/`, crop outputs) or local secrets (`.env`).
- Prefer updating existing docs over creating new markdown files unless the user asks or the new file is clearly necessary.
- When modifying files you have not read recently, reread the relevant section first.

## Git conventions

- Do not commit unless the user explicitly asks.
- Commit message format, when requested: `<type>(<scope>): <subject>` with types such as `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`, or `build`.
- Recommended scopes include `mcp`, `mod`, `hermes`, `docs`, `ci`, `tools`, and
  `bridge`.
- Keep commit subjects imperative, lowercase, and no more than about 60 characters
  where practical.
- Do not use `--no-verify`, `[skip ci]`, or force-push main/master unless explicitly instructed.
- Commit only after `task ci` passes when local prerequisites are available.

## Windows/local environment notes

- The usual working directory is `d:\SmartNPC` on Windows.
- Prefer PowerShell-native commands in this environment, and avoid composing destructive filesystem operations across shells.
- Before recursive delete/move operations, verify resolved absolute paths are inside the intended workspace.
- If using WSL/Hermes, remember WSL may need the Windows host IP rather than `127.0.0.1` to reach `:3000/mcp`.
