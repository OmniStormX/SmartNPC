#!/usr/bin/env python3
"""
Hermes Gateway 并发测试 — 验证 6 个 gateway 是否能同时处理请求。

用法 (在 WSL 内运行):
  python3 scripts/test_gateway_concurrency.py [--host 127.0.0.1]

前提: 6 个 Hermes Gateway 已启动 (ports 8642-8647)。
      smartnpc-mcp 已启动 (--http :3000)。

测试方式:
  同时向 6 个 gateway POST 一条简单事件，观察 tool calls 是否并行返回。
"""

import argparse
import json
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from urllib.request import Request, urlopen
from urllib.error import URLError, HTTPError

PROFILES = [
    ("xiami",     8642),
    ("abigail",   8643),
    ("haley",     8644),
    ("harvey",    8645),
    ("penny",     8646),
    ("sebastian", 8647),
]


def check_health(host: str, port: int) -> bool:
    """Check if a gateway is healthy."""
    try:
        req = Request(f"http://{host}:{port}/health")
        with urlopen(req, timeout=3) as resp:
            return resp.status == 200
    except Exception:
        return False


def send_event(host: str, port: int, profile: str, api_key: str) -> dict:
    """Send a simple event to a Hermes gateway and time the response."""
    payload = json.dumps({
        "model": "hermes-agent",
        "input": f"[test] Concurrency check for {profile}. Reply with exactly: 'OK {profile}'",
        "conversation": profile,
        "store": False,
    }).encode()

    url = f"http://{host}:{port}/v1/responses"
    req = Request(url, data=payload, method="POST")
    req.add_header("Content-Type", "application/json")
    if api_key:
        req.add_header("Authorization", f"Bearer {api_key}")

    t_start = time.perf_counter()
    try:
        with urlopen(req, timeout=120) as resp:
            body = resp.read().decode()
            t_end = time.perf_counter()
            return {
                "profile": profile,
                "port": port,
                "status": resp.status,
                "start": t_start,
                "end": t_end,
                "elapsed_ms": int((t_end - t_start) * 1000),
                "body_preview": body[:200],
                "error": None,
            }
    except (URLError, HTTPError) as e:
        t_end = time.perf_counter()
        body = ""
        if hasattr(e, "read"):
            try:
                body = e.read().decode()[:200]
            except Exception:
                pass
        return {
            "profile": profile,
            "port": port,
            "status": getattr(e, "code", 0),
            "start": t_start,
            "end": t_end,
            "elapsed_ms": int((t_end - t_start) * 1000),
            "body_preview": body,
            "error": str(e),
        }


def main():
    parser = argparse.ArgumentParser(description="Hermes Gateway concurrency test")
    parser.add_argument("--host", type=str, default="127.0.0.1",
                        help="Hermes Gateway host (default: 127.0.0.1)")
    parser.add_argument("--api-key", type=str, default="smartnpc-test-key",
                        help="Bearer token for gateway auth")
    parser.add_argument("--profiles", type=str, default="",
                        help="Comma-separated profile names (default: all 6)")
    args = parser.parse_args()

    profiles = PROFILES
    if args.profiles:
        names = [n.strip() for n in args.profiles.split(",")]
        profiles = [(n, p) for (n, p) in PROFILES if n in names]

    print(f"[test] Hermes Gateway concurrency test")
    print(f"[test] host: {args.host}, profiles: {len(profiles)}")
    print()

    # Phase 1: Health check
    print("[health] checking gateway availability...")
    alive = []
    for name, port in profiles:
        ok = check_health(args.host, port)
        status = "OK" if ok else "DOWN"
        print(f"  {name:12} :{port}  {status}")
        if ok:
            alive.append((name, port))
    print()

    if not alive:
        print("[ERROR] No gateways are healthy. Start them first:")
        print("  bash scripts/start_hermes_profiles.sh xiami,abigail,haley,harvey,penny,sebastian")
        sys.exit(1)

    n = len(alive)
    print(f"[test] sending {n} concurrent events...")
    print()

    # Phase 2: Concurrent burst
    t_global_start = time.perf_counter()
    results = []
    with ThreadPoolExecutor(max_workers=n) as pool:
        futures = {
            pool.submit(send_event, args.host, port, name, args.api_key): name
            for name, port in alive
        }
        for future in as_completed(futures):
            results.append(future.result())

    t_global_end = time.perf_counter()
    total_ms = int((t_global_end - t_global_start) * 1000)

    # Sort by start time
    results.sort(key=lambda r: r["start"])
    ref = results[0]["start"]

    print(f"{'profile':>12} {'start':>8} {'end':>8} {'elapsed':>8}  status  error/preview")
    print("-" * 90)
    for r in results:
        s_ms = int((r["start"] - ref) * 1000)
        e_ms = int((r["end"] - ref) * 1000)
        info = r["error"] or r["body_preview"][:40]
        print(f"{r['profile']:>12} {s_ms:>6}ms {e_ms:>6}ms {r['elapsed_ms']:>6}ms  {r['status']:>5}  {info}")

    print("-" * 90)
    print(f"Total wall-clock: {total_ms}ms")

    # Analysis
    successful = [r for r in results if not r["error"]]
    if successful:
        avg_elapsed = sum(r["elapsed_ms"] for r in successful) / len(successful)
        sum_elapsed = sum(r["elapsed_ms"] for r in successful)
        parallelism = sum_elapsed / total_ms if total_ms > 0 else 0

        print(f"\nAvg per-request: {avg_elapsed:.0f}ms")
        print(f"Sum all requests: {sum_elapsed}ms")
        print(f"Effective parallelism: {parallelism:.1f}x")
        print()

        if parallelism >= n * 0.7:
            print(f"[RESULT] Hermes Gateways process events in PARALLEL")
            print("  => 瓶颈不在 Gateway 进程层。可能在 Gateway→MCP tool call 路径。")
        elif parallelism >= 1.5:
            print(f"[RESULT] Partial parallelism ({parallelism:.1f}x)")
            print("  => 有限并行; 可能 Hermes 内部有并发约束")
        else:
            print(f"[RESULT] Hermes Gateways appear SERIAL")
            print("  => 瓶颈确认: Hermes Gateway 串行处理请求")
            print("  => 检查 hermes 配置: concurrency / queue / worker 设置")


if __name__ == "__main__":
    main()
