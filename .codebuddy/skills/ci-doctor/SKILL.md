---
name: ci-doctor
description: This skill should be used when the user asks to check, diagnose, or fix a GitHub Actions CI run for the SmartNPC project. Triggers include phrases like "check ci", "看 CI", "Actions 挂了", "ci 失败", "修复 ci", "ci 怎么样了". Provides a deterministic SOP to fetch the latest run status with the gh CLI, classify failures, propose fixes, and re-verify until green.
---

# CI Doctor

Diagnose and remediate GitHub Actions CI failures for SmartNPC.

## When to use this skill

Invoke when:
- User asks to "check CI" / "看 CI" / "ci 怎么样了" after a push.
- User reports a red Actions badge or pastes a failure log.
- An automatic commit + push completes and CI status must be verified.

Skip when:
- No commits have been pushed since the last green run (ask user instead of guessing).
- The repository has no `.github/workflows/` (CI is not set up; redirect to setting it up first).

## Prerequisites

Verify the environment before any diagnosis:

1. Confirm `gh` CLI is installed: `gh --version`. If missing, instruct user to run `winget install GitHub.cli` followed by `gh auth login` and stop.
2. Confirm authentication: `gh auth status`. If not authenticated, instruct `gh auth login` and stop.
3. Confirm working directory is the repo root (contains `.git/`).

## Diagnosis SOP

Follow these steps in strict order. Do not skip ahead.

### Step 1 — Fetch the latest run

Run the helper script to get a structured snapshot:

```cmd
python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1
```

The script returns JSON with `runId`, `status`, `conclusion`, `event`, `headBranch`, `headSha`, `displayTitle`, `url`.

If `conclusion == "success"`: report a one-liner with run URL + duration. Stop.

If `conclusion in ["failure", "cancelled", "timed_out"]`: continue to Step 2.

If `status == "in_progress"`: tell the user the run is still in progress, suggest re-checking in N minutes (estimate from typical run time), do not block.

### Step 2 — Pull failed-job logs

```cmd
gh run view <runId> --log-failed > .codebuddy\skills\ci-doctor\.tmp_failed.log
```

Then read the log via `read_file` and locate failure markers. Reference `references/failure-patterns.md` for known patterns and quick triage.

### Step 3 — Classify the failure

Match the log against the categories in `references/failure-patterns.md`:

- **Compile error** — Go build / dotnet build failed
- **Test failure** — `--- FAIL:` or `[xUnit]` red
- **Lint failure** — `go vet` complaint or schema violation
- **Dependency drift** — `go.sum mismatch` / `dotnet restore` failure
- **Environment** — runner OS issue, action version pinning, secret missing
- **Workflow config** — YAML syntax error, path filter mismatch
- **Flake** — passes locally + transient (network, race) — only conclude after re-running

State the classification explicitly in the response.

### Step 4 — Propose a fix

For each classification the references file provides a recommended fix vector. Present the diagnosis to the user as:

```
CI 失败汇报
- run: <url>
- 失败 job: <jobName>
- 分类: <category>
- 根因: <one sentence>
- 建议修复: <bullet list>
```

If the fix is small (≤ 5 lines, single file, mechanical), proceed without asking. Otherwise wait for user approval.

### Step 5 — Apply, verify, re-push

1. Edit code with the standard editing tools.
2. Run `task ci` locally. Block on failure — do not push red code.
3. Commit using the rules from `git-workflow` (Claude identity if user marked task as auto-commit).
4. `git push`.
5. Loop back to Step 1 until success or 3 failed attempts. After 3 failures, stop and hand back to the user with a full summary.

## Anti-patterns (do not do)

- Do not modify `.github/workflows/` to mask failures (e.g. `continue-on-error: true`, removing failing jobs).
- Do not retry without changing anything to "see if the flake clears" more than once.
- Do not `git push --force` to fix CI history.
- Do not claim CI is green without showing the actual `conclusion: success` from `gh run view`.

## Bundled resources

| Path | Purpose |
|------|---------|
| `scripts/fetch_run.py` | Fetch latest run as structured JSON (wraps `gh run list/view`). |
| `references/failure-patterns.md` | Failure category catalog with triage hints. |
