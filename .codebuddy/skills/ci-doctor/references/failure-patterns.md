# GitHub Actions Failure Patterns — SmartNPC Catalog

Reference catalog for the `ci-doctor` skill. Each entry includes:
- **Marker**: a string or regex to grep in the failed log
- **Category**: one of compile / test / lint / dependency / environment / workflow / flake
- **Triage**: minimum information to gather
- **Fix vector**: typical resolution

---

## Go compile error

- **Marker**: `: undefined:`, `cannot find package`, `syntax error`, `imported and not used`
- **Category**: compile
- **Triage**: which file + line + which Go module (`smartnpc-mcp` vs `smartnpc-agent`)
- **Fix vector**:
  - Missing import → add it
  - Unused import → remove or `_` alias
  - Cross-module dep → check `go.work` is in sync; run `go work sync`
  - SDK API drift after `go get -u` → pin previous version or adapt to new API

## Go test failure

- **Marker**: `--- FAIL:`, `panic:` in test output, `FAIL\tgithub.com/...`
- **Category**: test
- **Triage**:
  - Identify failing test name from `--- FAIL: TestXxx`
  - Read the assertion message above the FAIL line
  - Reproduce locally: `go test -run TestXxx -v ./path/to/pkg`
- **Fix vector**:
  - Real regression → fix source under test
  - Test brittle (timing, ordering) → harden test (channels, deterministic clock)
  - Schema/jsonschema drift in MCP tool → realign Input/Output struct + test fixture

## Go vet / lint failure

- **Marker**: `go vet` non-empty output, `printf format`, `composite literal uses unkeyed fields`
- **Category**: lint
- **Fix vector**: apply the diagnostic verbatim — vet warnings are almost always correct.

## Dependency drift

- **Marker**: `missing go.sum entry`, `verifying ...: checksum mismatch`, `inconsistent vendoring`
- **Category**: dependency
- **Fix vector**:
  - `go mod tidy` in each affected module
  - `go work sync` at repo root
  - Commit updated `go.sum` / `go.work.sum`

## C# compile / test failure

- **Marker**: `error CS\d+`, `Build FAILED`, `[xUnit.net]`, `Test Run Failed`
- **Category**: compile or test
- **Triage**: which `.cs` file + diagnostic code (CSxxxx)
- **Fix vector**:
  - CS0246 (type not found) → check `using` + project reference
  - SMAPI API mismatch → check `manifest.json` `MinimumApiVersion` against SMAPI version

## Environment / runner

- **Marker**: `Unable to locate executable`, `Resource not accessible by integration`, `Error: Process completed with exit code 137` (OOM)
- **Category**: environment
- **Fix vector**:
  - Missing tool → add `setup-go` / `setup-dotnet` step
  - Wrong action version → pin to a known-good `@vN` tag
  - OOM → split test job, raise runner spec, or trim test data

## Workflow YAML

- **Marker**: `Invalid workflow file`, `you have an error in your yaml syntax`, `Unrecognized named-value`
- **Category**: workflow
- **Fix vector**:
  - Run `python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"` locally to lint
  - Beware tab vs space indentation; YAML requires spaces

## Flake (transient)

- **Marker**: passes on retry without code change; mentions network timeout, `i/o timeout`, `connection reset`, `429 Too Many Requests`
- **Category**: flake
- **Fix vector**:
  - First flake → re-run once via `gh run rerun --failed <runId>`
  - Recurring flake → harden test (retries, mock external) or quarantine with `t.Skip` + tracking issue
  - Do **not** mark `continue-on-error: true` to silence flakes

---

## Quick-grep patterns for the diagnosis script

```
FAIL\t                  → Go test failure
--- FAIL:               → Go test failure (specific test)
^panic:                 → Go runtime panic
error CS                → C# compile error
go: missing go.sum      → Dependency drift
Error: Process completed → Workflow / runner failure
Invalid workflow file   → Workflow YAML
```
