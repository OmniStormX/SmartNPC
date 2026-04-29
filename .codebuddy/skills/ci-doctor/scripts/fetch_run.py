"""Fetch the latest GitHub Actions run for the current repository as structured JSON.

Usage:
    python fetch_run.py [--limit N] [--workflow ci.yml] [--branch main]

Output (single JSON object printed to stdout):
    {
      "runId": "12345",
      "status": "completed|in_progress|queued",
      "conclusion": "success|failure|cancelled|timed_out|null",
      "event": "push|pull_request|...",
      "headBranch": "main",
      "headSha": "abc1234",
      "displayTitle": "feat(mcp): ...",
      "url": "https://github.com/.../actions/runs/12345",
      "createdAt": "2026-04-29T12:00:00Z",
      "updatedAt": "2026-04-29T12:03:21Z"
    }

Requires: `gh` CLI in PATH and authenticated (`gh auth status`).
Exit codes:
    0  ok
    2  gh CLI missing or unauthenticated
    3  no runs found
    4  unexpected gh failure
"""
from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys


FIELDS = [
    "databaseId",
    "status",
    "conclusion",
    "event",
    "headBranch",
    "headSha",
    "displayTitle",
    "url",
    "createdAt",
    "updatedAt",
    "workflowName",
]


def die(code: int, msg: str) -> None:
    print(json.dumps({"error": msg}), file=sys.stderr)
    sys.exit(code)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--limit", type=int, default=1)
    ap.add_argument("--workflow", default=None, help="workflow file name, e.g. ci.yml")
    ap.add_argument("--branch", default=None, help="filter by branch")
    args = ap.parse_args()

    if not shutil.which("gh"):
        die(2, "gh CLI not found. Install with: winget install GitHub.cli")

    cmd = ["gh", "run", "list", "--limit", str(args.limit), "--json", ",".join(FIELDS)]
    if args.workflow:
        cmd += ["--workflow", args.workflow]
    if args.branch:
        cmd += ["--branch", args.branch]

    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    except FileNotFoundError:
        die(2, "gh CLI not executable")

    if proc.returncode != 0:
        die(4, f"gh run list failed: {proc.stderr.strip()}")

    try:
        runs = json.loads(proc.stdout or "[]")
    except json.JSONDecodeError as e:
        die(4, f"invalid JSON from gh: {e}")

    if not runs:
        die(3, "no runs found")

    if args.limit == 1:
        r = runs[0]
        out = {
            "runId": str(r.get("databaseId", "")),
            "status": r.get("status", ""),
            "conclusion": r.get("conclusion") or None,
            "event": r.get("event", ""),
            "headBranch": r.get("headBranch", ""),
            "headSha": (r.get("headSha", "") or "")[:7],
            "displayTitle": r.get("displayTitle", ""),
            "url": r.get("url", ""),
            "workflowName": r.get("workflowName", ""),
            "createdAt": r.get("createdAt", ""),
            "updatedAt": r.get("updatedAt", ""),
        }
        print(json.dumps(out, indent=2))
    else:
        print(json.dumps(runs, indent=2))


if __name__ == "__main__":
    main()
