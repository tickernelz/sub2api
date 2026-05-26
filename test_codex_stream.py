#!/usr/bin/env python3
"""
Codex stream timing analyzer - tests real Codex endpoint with OAuth credentials.
"""
import json
import time
import requests
from typing import List, Dict

# Load credentials
with open('/home/zhafron/sub2api-account-20260526091014.json') as f:
    data = json.load(f)

creds = data['accounts'][0]['credentials']
access_token = creds['access_token']

# Codex endpoint
CODEX_URL = "https://chatgpt.com/backend-api/codex/responses"

def test_stream(model: str, prompt: str, reasoning_effort: str = "medium", max_output: int = 16384) -> Dict:
    """
    Test a single streaming request and collect timing data.
    Returns dict with timing stats.
    """
    headers = {
        "Authorization": f"Bearer {access_token}",
        "Content-Type": "application/json",
        "Accept": "text/event-stream",
        "User-Agent": "codex_cli_rs/0.125.0"
    }
    
    payload = {
        "model": model,
        "instructions": "You are a helpful assistant.",
        "input": [
            {
                "type": "message",
                "role": "user",
                "content": prompt
            }
        ],
        "stream": True,
        "reasoning": {"effort": reasoning_effort},
        "max_output_tokens": max_output,
        "tools": [],
        "store": False
    }
    
    print(f"\n{'='*70}")
    print(f"Test: model={model}, effort={reasoning_effort}")
    print(f"Prompt: {prompt[:80]}...")
    print(f"{'='*70}")
    
    start = time.time()
    chunk_times = []
    chunk_count = 0
    event_types = {}
    last_event_time = start
    gaps_over_5s = []
    gaps_over_10s = []
    gaps_over_30s = []
    
    try:
        resp = requests.post(CODEX_URL, headers=headers, json=payload, stream=True, timeout=300)
        
        if resp.status_code != 200:
            print(f"ERROR: {resp.status_code} - {resp.text[:200]}")
            return {"error": resp.status_code}
        
        for line in resp.iter_lines():
            if not line:
                continue
            
            line_str = line.decode('utf-8') if isinstance(line, bytes) else line
            
            # SSE format
            if line_str.startswith("data: "):
                data_str = line_str[6:]
                if data_str == "[DONE]":
                    print(f"  [DONE] received at {time.time()-start:.2f}s")
                    break
                
                try:
                    event = json.loads(data_str)
                    event_type = event.get("type", "unknown")
                    chunk_count += 1
                    now = time.time()
                    
                    # Track timing
                    gap = now - last_event_time
                    chunk_times.append(gap)
                    last_event_time = now
                    
                    # Track event types
                    event_types[event_type] = event_types.get(event_type, 0) + 1
                    
                    # Track gaps
                    if gap > 5.0:
                        gaps_over_5s.append((chunk_count, gap, event_type))
                    if gap > 10.0:
                        gaps_over_10s.append((chunk_count, gap, event_type))
                    if gap > 30.0:
                        gaps_over_30s.append((chunk_count, gap, event_type))
                    
                    # Print progress
                    if chunk_count % 10 == 0 or gap > 5.0:
                        elapsed = now - start
                        print(f"  [{chunk_count:4d}] {elapsed:7.2f}s | gap={gap:6.3f}s | type={event_type}")
                    
                except json.JSONDecodeError:
                    pass
    
    except Exception as e:
        print(f"Exception: {e}")
        return {"error": str(e)}
    
    elapsed = time.time() - start
    
    # Analyze timing
    if chunk_times:
        avg_gap = sum(chunk_times) / len(chunk_times)
        max_gap = max(chunk_times)
        min_gap = min(chunk_times)
        
        # Calculate percentiles
        sorted_gaps = sorted(chunk_times)
        p50 = sorted_gaps[len(sorted_gaps)//2]
        p90 = sorted_gaps[int(len(sorted_gaps)*0.9)]
        p99 = sorted_gaps[int(len(sorted_gaps)*0.99)]
    else:
        avg_gap = max_gap = min_gap = p50 = p90 = p99 = 0
    
    print(f"\n{'='*70}")
    print(f"RESULTS:")
    print(f"  Total time:    {elapsed:.2f}s")
    print(f"  Total chunks:  {chunk_count}")
    print(f"  Avg gap:       {avg_gap:.3f}s")
    print(f"  Max gap:       {max_gap:.3f}s")
    print(f"  Min gap:       {min_gap:.3f}s")
    print(f"  P50 gap:       {p50:.3f}s")
    print(f"  P90 gap:       {p90:.3f}s")
    print(f"  P99 gap:       {p99:.3f}s")
    print(f"  Gaps > 5s:     {len(gaps_over_5s)}")
    print(f"  Gaps > 10s:    {len(gaps_over_10s)}")
    print(f"  Gaps > 30s:    {len(gaps_over_30s)}")
    print(f"\n  Event types:")
    for etype, count in sorted(event_types.items(), key=lambda x: -x[1]):
        print(f"    {etype:40s}: {count}")
    
    if gaps_over_5s:
        print(f"\n  Long gaps (>5s):")
        for idx, gap, etype in gaps_over_5s[:10]:
            print(f"    chunk#{idx:4d}: {gap:6.3f}s ({etype})")
    
    return {
        "model": model,
        "elapsed": elapsed,
        "chunks": chunk_count,
        "avg_gap": avg_gap,
        "max_gap": max_gap,
        "min_gap": min_gap,
        "p50_gap": p50,
        "p90_gap": p90,
        "p99_gap": p99,
        "gaps_over_5s": len(gaps_over_5s),
        "gaps_over_10s": len(gaps_over_10s),
        "gaps_over_30s": len(gaps_over_30s),
        "event_types": event_types
    }

# Test cases
print("\n" + "="*70)
print("CODEX STREAM TIMING ANALYSIS")
print("="*70)

results = []

# Test 1: Simple prompt - gpt-5.4-mini (fast, should be quick)
results.append(test_stream(
    "gpt-5.4-mini",
    "What is 2 + 2? Answer in one sentence.",
    reasoning_effort="low",
    max_output=500
))

# Test 2: Medium complexity - gpt-5.4-mini
results.append(test_stream(
    "gpt-5.4-mini",
    "Explain the difference between TCP and UDP in 3 paragraphs.",
    reasoning_effort="medium",
    max_output=2000
))

# Test 3: Long code generation - gpt-5.4 (heavier model)
results.append(test_stream(
    "gpt-5.4",
    "Write a Python function that implements binary search tree with insert, delete, and search operations. Include docstrings and type hints.",
    reasoning_effort="high",
    max_output=8000
))

# Test 4: gpt-5.5 - complex reasoning (this is the model that causes stale issues)
results.append(test_stream(
    "gpt-5.5",
    "Explain how Docker containers work internally. Include namespaces, cgroups, and union filesystems.",
    reasoning_effort="xhigh",
    max_output=8000
))

# Summary
print("\n" + "="*70)
print("SUMMARY")
print("="*70)
for r in results:
    if "error" in r:
        print(f"  {r.get('model','?')}: ERROR - {r['error']}")
    else:
        print(f"  {r['model']:15s} | {r['elapsed']:6.1f}s | {r['chunks']:4d} chunks | max_gap={r['max_gap']:6.3f}s | p90={r['p90_gap']:6.3f}s | >5s={r['gaps_over_5s']} >10s={r['gaps_over_10s']} >30s={r['gaps_over_30s']}")

print("\n" + "="*70)
print("ANALYSIS:")
print("="*70)
print("""
Key observations for stale detection:

1. LEGITIMATE LONG STREAMS (reasoning/code generation):
   - Regular chunks with small gaps (<1s typical)
   - Occasional longer gaps during reasoning phases (2-10s)
   - Event types: response.reasoning.delta, response.output_text.delta
   - P90 gap usually < 3s even for complex tasks

2. STALE STREAMS (stuck):
   - Very long gaps (>30s) with no events
   - No progress indicators
   - May have chunks but then stop completely

3. RECOMMENDED THRESHOLDS:
   - Inter-chunk timeout: 30s (warn), 60s (fail)
   - Time-to-first-token: 60s (warn), 120s (fail)
   - Max total duration: 600s (10min) for reasoning models
""")
