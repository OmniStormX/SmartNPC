# ADR-0003: NPC profile templates use placeholder substitution at install time

> **Status**: Proposed — design phase, awaiting implementation
> **Date**: 2026-05-12
> **Context**: M5 follow-up F1/F2 (NPC name placeholder-ization in
> shared SKILL templates)
> **Supersedes**: —
> **Related**: [ADR-0001](./0001-synthetic-events-go-through-hermesrelay.md),
> [ADR-0002](./0002-group-chat-channel-end-to-end.md),
> [`docs/adr/drafts/synthetic-events-doc-patches.md`](./drafts/synthetic-events-doc-patches.md)
> F1 / F2 sections

---

## Context

The delegate-fix PR (ADR-0001) wired synthetic events through
hermesrelay so inter-NPC delegation reaches the recipient's Hermes
profile. While auditing the M5 fan-out we surfaced a **separate,
pre-existing** template-hygiene bug that the wiring fix does not touch.

5 of the 6 NPC profiles (`abigail`, `haley`, `harvey`, `penny`,
`sebastian`) were created by `cp -r` from `hermes/profiles/xiami/` and
retain hard-coded NPC literals across multiple shared SKILL files
([`docs/adr/drafts/synthetic-events-doc-patches.md`](./drafts/synthetic-events-doc-patches.md)
F1 section has the full audit). Concretely:

| File | Hard-coded literals per profile |
|---|---|
| `skills/smartnpc/memory-policy/SKILL.md` | 9 hits — `(XiaMi)` headings, `memories/xiami/state.db` path, `conversation 'xiami'`, XiaMi-specific 口癖 examples, frontmatter `description:` reads "How XiaMi uses…" |
| `skills/smartnpc/inter-npc-message/SKILL.md` | 9 → ~17 hits after delegate-fix rewrite — `Player → XiaMi` example pairs, `to="Penny"` literal arguments, CN aliases `潘妮` / `阿比盖尔` |
| `skills/smartnpc/game-tool-policy/SKILL.md` | 1 hit — `speaker = "XiaMi"` example |
| `config-overlay.yaml` | 1 hit — comment-only reference to "see xiami for the full template" |
| `skills/smartnpc/proactive-greeting/SKILL.md` | 0 hits — already generic (heart-tier abstractions), no work needed |

Net effect: the silent-ack memory write path on a non-xiami profile
writes to **xiami**'s memory namespace and self-narrates in XiaMi's
voice. The frontmatter on Abigail's `memory-policy/SKILL.md`
literally reads "How XiaMi uses…". This is the most legible
data-corruption path; the inter-npc-message example pairs are a
softer "delegate routing-accuracy degraded" symptom.

Adopt **render-time placeholder substitution** via GNU `sed` in a
dedicated [`scripts/render_profiles.sh`](../../scripts/render_profiles.sh)
script. Placeholders are Mustache-style `{{NAME}}` tokens; per-profile
values come from a hardcoded TABLE inside the render script. The
rendered tree lives in-repo at `hermes/profiles/<npc>/` so diffs are
reviewable; [`hermes/install.sh`](../../hermes/install.sh) continues to
own the WSL-side copy step unchanged.

**Source-of-truth layout:**

- **Master template**: [`hermes/profiles/_master/`](../../hermes/profiles/_master/)
  — contains shared artifacts only: `config-overlay.yaml`,
  `cron-recipes.md`, `skills/smartnpc/{game-tool-policy,inter-npc-message,memory-policy,proactive-greeting}/SKILL.md`.
- **Per-NPC hand-written**: `hermes/profiles/<npc>/SOUL.md` — 6 hand-
  written persona files that **do not** live in `_master/` and are
  **never** touched by the render script. SOUL.md is identity/voice
  and is intentionally not templatable.
- **Rendered output**: `hermes/profiles/<npc>/{skills,config-overlay.yaml,cron-recipes.md}`
  — regenerated every render. The xiami tree is rendered the same way
  all 5 other NPCs are (no master-is-also-runnable asymmetry); the
  canonical sanity check after render is `git diff hermes/profiles/xiami/`
  returns empty.

**Eight placeholders** (one more than the initial 7-placeholder spec —
`{{NPC_PORT}}` was added during implementation because each Hermes
Gateway binds a different port and config-overlay.yaml must
parameterize it):

| Placeholder | Example (abigail) | Use site |
|---|---|---|
| `{{NPC_NAME}}` | `Abigail` | PascalCase internal name; valid `speaker:` arg |
| `{{NPC_DISPLAY}}` | `阿比盖尔` | CN display name for prose |
| `{{NPC_DIR}}` | `abigail` | profile dir; `memories/<dir>/`, `conversation:` |
| `{{NPC_PORT}}` | `8643` | Hermes Gateway `API_SERVER_PORT` (per `runtime-config.yaml`) |
| `{{PEER_A_NAME}}` | `Penny` | First example peer, PascalCase |
| `{{PEER_A_DISPLAY}}` | `潘妮` | First peer CN display |
| `{{PEER_B_NAME}}` | `Sebastian` | Second example peer, PascalCase |
| `{{PEER_B_DISPLAY}}` | `塞巴斯蒂安` | Second peer CN display |

**Per-profile data source**: hardcoded TABLE in `scripts/render_profiles.sh`
(NPC table: `DIR NAME DISPLAY PORT PEER_A_NAME PEER_A_DISPLAY PEER_B_NAME PEER_B_DISPLAY`).
A per-profile YAML was considered but rejected — the table is 6 rows
today and changes only when a new NPC is added, at which point the
table edit is colocated with the runtime-config.yaml edit the author
must already make.

## Alternatives considered

### A. Pure runtime resolution (Hermes resolves placeholders on profile load)

Keep literal `{{NPC_NAME}}` (or similar) tokens in the profile files
on disk and have Hermes substitute them when loading the profile.

Rejected because:

- Adds runtime complexity to every profile load — Hermes Gateway
  startup, hot reload, debug profile-load tracing all need to handle
  pre-substituted vs. post-substituted state.
- **Debuggability suffers**: `cat hermes/profiles/abigail/skills/.../SKILL.md`
  shows literal `{{NPC_NAME}}` rather than `Abigail`, so anyone reading
  the file (humans, lint tools, future AI assistants) sees template
  source instead of effective content.
- No upside over install-time substitution — the placeholders are
  static per profile; nothing about runtime warrants late binding.
- `hermes/install.sh` already does install-time templating
  (`__HOST_IP__`); reusing that path is strictly simpler than
  introducing a runtime layer.

### B. Per-profile manual edit (status quo, no automation)

Keep the 5 cloned profiles, manually edit each SKILL file to fix the
NPC literals, no template/render mechanism.

Rejected because:

- **High drift risk**: any edit to xiami's master copy must be
  manually replayed across 5 profiles, with manual placeholder
  re-fixing each time. Violates DRY hard.
- **Audit pain**: there is no single source of truth — diff-after-
  edit becomes "5 hand-edited copies vs. 1 master, all subtly
  divergent". This is exactly how the current bug got introduced
  (cp without find-and-replace).
- F2 regression test (diff-after-render across 6 profiles) cannot
  exist without a render step; manual edit makes drift undetectable.

### C. xiami-as-master (earlier draft — superseded by `_master/` tree)

Earlier drafts of this ADR proposed keeping xiami as both the runnable
profile AND the source-of-truth template, with the other 5 profiles
rendered from xiami. The implementation adopted a **dedicated
`_master/` tree** instead: shared artifacts live in
`hermes/profiles/_master/`, xiami is rendered from it the same way all
5 other NPCs are. SOUL.md stays hand-written per-NPC and never enters
`_master/`.

Reasons the `_master/` tree won:

- Symmetry: xiami is no longer special. `git diff hermes/profiles/xiami/`
  after a render is a clean sanity check (empty diff); with xiami-as-
  master the render was a no-op for xiami and a substitution for the
  other 5, which muddies the regression-guard semantics.
- `install.sh` ignores `_master/` by iterating only over profile dirs
  with a `SOUL.md`; no special-case carve-out required.
- SOUL.md is identity/voice and was always going to stay per-NPC; the
  `_master/` tree makes that separation explicit (SOUL.md has no
  `_master/` sibling) rather than implicit ("don't sed xiami's
  SOUL.md").

## Consequences

### Positive

- Single source of truth (`hermes/profiles/_master/`) for shared SKILL
  + overlay + cron content; edits propagate to all 6 profiles via a
  single `bash scripts/render_profiles.sh` run.
- Render script is small (~85 lines, GNU sed + bash) and decoupled
  from `install.sh` (which keeps doing the WSL copy step unchanged).
- Regression test (diff-after-render) is straightforward: run
  `scripts/render_profiles.sh`, `git diff hermes/profiles/` must be
  empty. Pinned at render-time so drift is caught before it ships.
- All 6 profiles render symmetrically from the same template; xiami
  has no special-case status beyond being the historical reference
  point.
- SOUL.md stays per-NPC and hand-authored — identity/voice work is
  never collapsed into a template.

### Negative / risks

- Introduces a render step before commit — anyone editing a per-NPC
  rendered SKILL/overlay file directly will have their changes
  overwritten on next render. The rendered files are committed to the
  repo (not gitignored) so the diff is reviewable, but editors must
  remember to edit `_master/` instead.
- Placeholder regression: a SKILL author who hand-types a literal NPC
  name into `_master/` reintroduces the bug silently across all 6
  profiles. Mitigated by post-render lint:
  `grep -rE 'XiaMi|xiami|夏弥' hermes/profiles/{abigail,haley,harvey,penny,sebastian}/`
  must return zero hits.
- Port drift: `{{NPC_PORT}}` values in the render script's TABLE must
  stay in sync with [`hermes/runtime-config.yaml`](../../hermes/runtime-config.yaml)
  `profiles[*].gateway_url` ports. A mismatch produces a rendered
  `config-overlay.yaml` that binds a different port than the relay
  posts to — silent message loss. Mitigated by keeping both edits
  colocated in the "adding a new NPC" workflow (see
  [`docs/hermes-profiles.md`](../hermes-profiles.md) Multi-profile
  fan-out section).
- `{{NPC_DISPLAY}}` and peer assignments are hardcoded in the render
  script's TABLE. Considered acceptable for 6 NPCs; revisit if the
  roster grows past ~12.

## Pinned tests

Implemented manual checks (to be promoted to CI/linter at some point):

- **Pre-render lint**: `_master/` tree contains only placeholder tokens
  in templated regions — `grep -rE 'XiaMi|xiami|夏弥' hermes/profiles/_master/`
  must return zero hits.
- **Post-render lint**: 5 non-xiami profiles contain zero NPC-specific
  literals from the xiami voice — `grep -rE 'XiaMi|xiami|夏弥' hermes/profiles/{abigail,haley,harvey,penny,sebastian}/skills/`
  must return zero hits (peer references that intentionally name other
  NPCs are rendered via `{{PEER_*}}` placeholders, so this is a clean
  grep).
- **Render idempotency / xiami byte-equality**: running
  `bash scripts/render_profiles.sh` twice produces identical output,
  and `git diff hermes/profiles/xiami/` after a render is empty —
  xiami is rendered from `_master/` with its own row of the TABLE,
  so any byte drift means a placeholder substitution regressed.
- **Frontmatter check**: `description:` on each rendered SKILL.md
  names the correct NPC (e.g. Abigail's `memory-policy/SKILL.md`
  description reads "How Abigail uses…", Penny's reads "How Penny
  uses…"). Automated by the post-render grep above — a frontmatter
  still saying "How XiaMi uses…" would be caught.

**Still TODO:** promote the render+grep sequence into `task ci-fast` so
a PR that edits a rendered SKILL without re-rendering fails CI.
Tracked separately from this ADR's acceptance criteria.

## Implementation notes

Landed on `rebuild`:

- [`hermes/profiles/_master/`](../../hermes/profiles/_master/) — master
  template tree. Contains `config-overlay.yaml`, `cron-recipes.md`, and
  `skills/smartnpc/{game-tool-policy,inter-npc-message,memory-policy,proactive-greeting}/SKILL.md`.
  Does **not** contain SOUL.md.
- [`scripts/render_profiles.sh`](../../scripts/render_profiles.sh) —
  85-line bash script with an inline TABLE (6 rows) driving GNU sed
  substitution of 8 placeholders across every `.md` and `.yaml` file
  in each target profile's `skills/` + `config-overlay.yaml` +
  `cron-recipes.md`. Idempotent; re-renders xiami from the same
  template as the other 5.
- `hermes/install.sh` — **unchanged**. Still owns WSL-side copy; the
  render step runs separately (and earlier) in the author workflow.
- 5 non-xiami profiles — overlays upgraded from short form to full
  form; all 6 gained `cron-recipes.md`; all 6 skills now derive from
  `_master/` and contain zero xiami-specific literals.
- SOUL.md — 6 hand-written files retained as-is, one per NPC.
- Doc surface synced via doc-coord patches:
  [`docs/architecture.md`](../architecture.md) "Profile cloning
  mechanism" section (P-F1-1),
  [`docs/hermes-profiles.md`](../hermes-profiles.md) render flow
  (P-F1-2),
  [`scripts/render_profiles.sh`](../../scripts/render_profiles.sh)
  top-of-file comment block (P-F1-3),
  [`CLAUDE.md`](../../CLAUDE.md) 关键目录 row + master warning
  (P-F1-4).

After render (`bash scripts/render_profiles.sh`):

- `grep -rE 'XiaMi|xiami|夏弥' hermes/profiles/{abigail,haley,harvey,penny,sebastian}/skills/`
  returns zero hits.
- `git diff hermes/profiles/xiami/` is empty (xiami round-trips byte-
  identically through render).
- Each profile's `memory-policy/SKILL.md` frontmatter `description:`
  correctly names the owning NPC (caught by the grep above).

Manual smoke (E2E, not yet performed — gates flipping Status to
Accepted): load a save, open private chat with each non-xiami NPC,
trigger the silent-ack memory path, confirm writes land in
`memories/<npc>/` not `memories/xiami/` and the silent-ack self-
narration uses the correct NPC's voice.

When this E2E pass succeeds for all 5 non-xiami NPCs, flip this ADR's
status from **Proposed** to **Accepted** and update
[`docs/roadmap.md`](../roadmap.md) M5 follow-up F-1 / F-2 rows.
