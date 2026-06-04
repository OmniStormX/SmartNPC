#!/usr/bin/env python3
"""
LLM API 并发测试 — 验证 deepseek API 是否支持 6 路并发请求。

用法:
  python scripts/test_concurrency.py [--concurrency 6] [--model deepseek-v4-flash-external]

输出: 每个请求的 start/end 时间线 + 总耗时，判断串行/并行。
"""

import argparse
import json
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from urllib.request import Request, urlopen
from urllib.error import URLError, HTTPError

API_URL = "http://api.anyone.woa.com/v1/chat/completions"
API_KEY = "sk-OdSYFCG40h4b6qQbgqLo0eutP9S4xO11PlWlSwVnoL54qAbw"


def send_request(idx: int, model: str) -> dict:
    """Send a minimal chat completion and return timing info."""
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": f"Say 'hello {idx}' in one word."}],
        "max_tokens": 10,
        "temperature": 0,
    }).encode()

    req = Request(API_URL, data=payload, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", f"Bearer {API_KEY}")

    t_start = time.perf_counter()
    try:
        with urlopen(req, timeout=60) as resp:
            body = json.loads(resp.read())
            t_end = time.perf_counter()
            return {
                "idx": idx,
                "status": resp.status,
                "start": t_start,
                "end": t_end,
                "elapsed_ms": int((t_end - t_start) * 1000),
                "content": body.get("choices", [{}])[0].get("message", {}).get("content", ""),
                "error": None,
            }
    except (URLError, HTTPError) as e:
        t_end = time.perf_counter()
        return {
            "idx": idx,
            "status": getattr(e, "code", 0),
            "start": t_start,
            "end": t_end,
            "elapsed_ms": int((t_end - t_start) * 1000),
            "content": "",
            "error": str(e),
        }


def main():
    parser = argparse.ArgumentParser(description="LLM API concurrency test")
    parser.add_argument("--concurrency", "-n", type=int, default=6,
                        help="number of concurrent requests (default: 6)")
    parser.add_argument("--model", "-m", type=str, default="deepseek-v4-flash-external",
                        help="model name")
    args = parser.parse_args()

    n = args.concurrency
    model = args.model
    print(f"[test] sending {n} concurrent requests to {API_URL}")
    print(f"[test] model: {model}")
    print()

    # Warm up: single request to establish TCP connection
    print("[warmup] sending 1 request...", end=" ", flush=True)
    warmup = send_request(0, model)
    if warmup["error"]:
        print(f"FAILED: {warmup['error']}")
        sys.exit(1)
    print(f"OK ({warmup['elapsed_ms']}ms)")
    print()

    # Concurrent burst
    t_global_start = time.perf_counter()
    results = []
    with ThreadPoolExecutor(max_workers=n) as pool:
        futures = {pool.submit(send_request, i + 1, model): i for i in range(n)}
        for future in as_completed(futures):
            results.append(future.result())

    t_global_end = time.perf_counter()
    total_ms = int((t_global_end - t_global_start) * 1000)

    # Sort by start time for timeline display
    results.sort(key=lambda r: r["start"])
    ref = results[0]["start"]

    print(f"{'#':>3} {'start':>8} {'end':>8} {'elapsed':>8}  status  response")
    print("-" * 72)
    for r in results:
        s_ms = int((r["start"] - ref) * 1000)
        e_ms = int((r["end"] - ref) * 1000)
        err = r["error"] or r["content"][:30]
        print(f"{r['idx']:>3} {s_ms:>6}ms {e_ms:>6}ms {r['elapsed_ms']:>6}ms  {r['status']:>5}  {err}")

    print("-" * 72)
    print(f"Total wall-clock: {total_ms}ms")

    # Analysis
    avg_elapsed = sum(r["elapsed_ms"] for r in results) / len(results)
    max_elapsed = max(r["elapsed_ms"] for r in results)
    sum_elapsed = sum(r["elapsed_ms"] for r in results)

    # If parallel: total ≈ max(individual). If serial: total ≈ sum(individual).
    parallelism = sum_elapsed / total_ms if total_ms > 0 else 0

    print(f"\nAvg per-request: {avg_elapsed:.0f}ms")
    print(f"Max per-request: {max_elapsed}ms")
    print(f"Sum all requests: {sum_elapsed}ms")
    print(f"Effective parallelism: {parallelism:.1f}x")
    print()

    if parallelism >= n * 0.7:
        print(f"[RESULT] API supports ~{n} concurrent requests (parallel)")
        print("  => bottleneck is NOT the LLM API. Check Hermes Gateway internals.")
    elif parallelism >= 2:
        concurrency_est = round(parallelism)
        print(f"[RESULT] API allows ~{concurrency_est} concurrent (partially parallel)")
        print(f"  => 有限并发; 6 NPC 中 ~{n - concurrency_est} 个会排队")
    else:
        print(f"[RESULT] API appears to serialize requests (effectively serial)")
        print("  => 瓶颈确认: LLM API 每次只处理 1 个请求, 其余排队")
        print("  => 方案 A (预规划直执行) 是最优解")


if __name__ == "__main__":
    main()
