Diagnose and fix GitHub Actions CI failures.

## Trigger
User says "看 CI" / "check ci" / "ci 怎么样了" / "Actions 挂了"

## SOP

### Step 1 — Fetch latest run
```cmd
python .codebuddy\skills\ci-doctor\scripts\fetch_run.py --limit 1
```
Parse the JSON output:
- `conclusion == "success"` → report PASS + URL, done
- `conclusion in ["failure", "cancelled", "timed_out"]` → Step 2
- `status == "in_progress"` → tell user to wait

### Step 2 — Extract failed logs
```cmd
gh run view <runId> --log-failed
```

### Step 3 — Categorize failure
Match against known patterns:
- **compile**: `undefined`, `cannot find package`, `error CS`
- **test**: `FAIL\t`, `--- FAIL:`, `^panic:`
- **lint**: vet output, printf format
- **dependency**: `missing go.sum`, checksum mismatch
- **environment**: missing tool, OOM (exit 137)
- **workflow**: `Invalid workflow file`, YAML syntax
- **flake**: passes on re-run, network timeout

### Step 4 — Report
Format:
```
CI 失败汇报
- run: <url>
- 失败 job: <jobName>
- 分类: <category>
- 根因: <one-liner>
- 建议修复: ...
```
Fix ≤5 lines → fix directly; else wait for user OK.

### Step 5 — Fix loop
1. Edit code
2. `task ci` locally (must pass!)
3. Commit + push
4. Back to Step 1

**Stop at 3 failures** → escalate to user.

## Forbidden
- Don't guess without reading logs
- Don't modify `.github/workflows/` to hide failures
- No `[skip ci]` / `--no-verify` / `git push --force`
