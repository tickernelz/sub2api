#!/usr/bin/env python3
"""
gpt-5.5 reasoning xhigh stream timing analyzer.
Tests multiple iterations to catch intermittent stale streams.
Tracks reasoning phase vs output phase separately.
"""
import json
import requests
import time
import sys
import statistics

with open('/home/zhafron/sub2api-account-20260526091014.json') as f:
    data = json.load(f)

# Rotate across accounts to avoid hitting rate limit
accounts = data['accounts']

CODEX_URL = "https://chatgpt.com/backend-api/codex/responses"

def run_stream_test(account, prompt, label, iteration):
    """Run a single streaming test with detailed event tracking."""
    creds = account['credentials']
    headers = {
        'Authorization': f'Bearer {creds["access_token"]}',
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream'
    }

    payload = {
        'model': 'gpt-5.5',
        'instructions': 'You are a helpful assistant. Think carefully before answering.',
        'input': [{'type': 'message', 'role': 'user', 'content': prompt}],
        'stream': True,
        'store': False,
        'reasoning': {'effort': 'xhigh'}
    }

    print(f"\n{'='*70}")
    print(f"[{account['name']}] iter={iteration} | {label}")
    print(f"Prompt: {prompt[:80]}...")
    print(f"{'='*70}")

    start = time.time()
    events = []
    last_event_time = start
    first_event_time = None
    reasoning_start = None
    output_start = None
    reasoning_chunks = 0
    output_chunks = 0
    reasoning_gaps = []
    output_gaps = []
    phase_transitions = []
    total_bytes = 0
    got_done = False
    error_msg = None

    try:
        resp = requests.post(
            CODEX_URL,
            headers=headers,
            json=payload,
            stream=True,
            timeout=300  # 5 min timeout
        )

        connect_time = time.time() - start
        print(f"  Connected: {connect_time:.3f}s | Status: {resp.status_code}")

        if resp.status_code != 200:
            body = resp.text[:300]
            print(f"  ERROR: {body}")
            return None

        for line in resp.iter_lines():
            now = time.time()
            if not first_event_time:
                first_event_time = now

            line_str = line.decode('utf-8') if isinstance(line, bytes) else str(line)
            total_bytes += len(line_str)

            # Skip empty lines and pure event: lines (we parse data: lines)
            if not line_str.startswith('data: '):
                continue

            data_str = line_str[6:]
            if data_str == '[DONE]':
                got_done = True
                print(f"  [DONE] at {now - start:.3f}s")
                break

            try:
                event = json.loads(data_str)
            except json.JSONDecodeError:
                continue

            etype = event.get('type', 'unknown')
            gap = now - last_event_time
            elapsed = now - start

            # Track phases
            phase = 'unknown'
            if 'reasoning' in etype:
                phase = 'reasoning'
                if reasoning_start is None:
                    reasoning_start = now
                    phase_transitions.append(('reasoning_start', elapsed))
                reasoning_chunks += 1
                if len(reasoning_gaps) > 0 or reasoning_chunks > 1:
                    reasoning_gaps.append(gap)
            elif 'output' in etype or 'text' in etype:
                phase = 'output'
                if output_start is None:
                    output_start = now
                    phase_transitions.append(('output_start', elapsed))
                output_chunks += 1
                output_gaps.append(gap)
            else:
                # Lifecycle events (created, in_progress, completed, etc.)
                phase = 'lifecycle'

            events.append({
                'time': elapsed,
                'gap': gap,
                'type': etype,
                'phase': phase
            })

            # Print notable events
            if gap > 2.0:
                print(f"  ⚠️  [{len(events):5d}] +{elapsed:7.3f}s | GAP={gap:6.3f}s | {etype}")
            elif etype in ('response.created', 'response.in_progress',
                           'response.completed', 'response.failed',
                           'response.reasoning_summary_text.delta',
                           'response.output_item.added'):
                print(f"  📌 [{len(events):5d}] +{elapsed:7.3f}s | gap={gap:6.3f}s | {etype}")

            last_event_time = now

    except requests.exceptions.Timeout:
        elapsed = time.time() - start
        print(f"  🚨 TIMEOUT after {elapsed:.1f}s")
        error_msg = 'timeout'
    except requests.exceptions.ConnectionError as e:
        elapsed = time.time() - start
        print(f"  🚨 CONNECTION ERROR after {elapsed:.1f}s: {e}")
        error_msg = 'connection_error'
    except Exception as e:
        elapsed = time.time() - start
        print(f"  🚨 EXCEPTION after {elapsed:.1f}s: {e}")
        error_msg = str(e)

    total_time = time.time() - start

    # Compute stats
    all_gaps = [e['gap'] for e in events[1:]]

    stats = {
        'account': account['name'],
        'label': label,
        'iteration': iteration,
        'total_time': total_time,
        'ttft': (first_event_time - start) if first_event_time else None,
        'total_events': len(events),
        'total_bytes': total_bytes,
        'got_done': got_done,
        'error': error_msg,
        'reasoning_chunks': reasoning_chunks,
        'output_chunks': output_chunks,
        'phase_transitions': phase_transitions,
    }

    if all_gaps:
        stats['gap_avg'] = statistics.mean(all_gaps)
        stats['gap_max'] = max(all_gaps)
        stats['gap_p95'] = sorted(all_gaps)[int(len(all_gaps) * 0.95)]
        stats['gap_p99'] = sorted(all_gaps)[min(int(len(all_gaps) * 0.99), len(all_gaps) - 1)]
        stats['gaps_over_2s'] = len([g for g in all_gaps if g > 2.0])
        stats['gaps_over_5s'] = len([g for g in all_gaps if g > 5.0])
        stats['gaps_over_10s'] = len([g for g in all_gaps if g > 10.0])
        stats['gaps_over_30s'] = len([g for g in all_gaps if g > 30.0])

    if reasoning_gaps:
        stats['reasoning_gap_avg'] = statistics.mean(reasoning_gaps)
        stats['reasoning_gap_max'] = max(reasoning_gaps)

    if output_gaps:
        stats['output_gap_avg'] = statistics.mean(output_gaps)
        stats['output_gap_max'] = max(output_gaps)

    # Summary
    print(f"\n  Result: {total_time:.1f}s | events={len(events)} | reasoning={reasoning_chunks} output={output_chunks}")
    if all_gaps:
        print(f"  Gaps: avg={stats['gap_avg']:.3f}s max={stats['gap_max']:.3f}s p95={stats['gap_p95']:.3f}s p99={stats['gap_p99']:.3f}s")
        print(f"  Gaps >2s: {stats['gaps_over_2s']} | >5s: {stats['gaps_over_5s']} | >10s: {stats['gaps_over_10s']} | >30s: {stats['gaps_over_30s']}")
    if reasoning_gaps:
        print(f"  Reasoning phase: avg_gap={stats['reasoning_gap_avg']:.3f}s max_gap={stats['reasoning_gap_max']:.3f}s")
    if output_gaps:
        print(f"  Output phase: avg_gap={stats['output_gap_avg']:.3f}s max_gap={stats['output_gap_max']:.3f}s")
    if phase_transitions:
        for pt in phase_transitions:
            print(f"  Phase: {pt[0]} at {pt[1]:.3f}s")
    if not got_done and not error_msg:
        print(f"  ⚠️  Stream ended WITHOUT [DONE]")
    if error_msg:
        print(f"  ❌ Error: {error_msg}")

    return stats


# ============================================================================
# Main test suite
# ============================================================================

PROMPTS = [
    # Short - should be fast, stale here is obvious
    ("What is 2+2? Answer in one word.", "simple_math"),
    # Medium reasoning - expect some thinking delay
    ("A farmer has 17 sheep. All but 9 run away. How many sheep does the farmer have left? Explain your reasoning.", "riddle"),
    # Code generation - longer output
    ("Write a Python function that implements merge sort with detailed comments.", "code_gen"),
    # Complex analysis - heavy reasoning
    ("Compare the time complexity of quicksort vs mergesort vs heapsort. When would you choose each one?", "algo_comparison"),
    # Long reasoning task
    ("Design a database schema for an e-commerce platform with users, products, orders, reviews, and inventory. Explain the relationships.", "schema_design"),
]

ITERATIONS = 3  # Run each prompt multiple times to catch intermittent stale

print("="*70)
print(f"gpt-5.5 reasoning=xhigh STREAM TIMING ANALYSIS")
print(f"Prompts: {len(PROMPTS)} | Iterations: {ITERATIONS}")
print(f"Accounts: {', '.join(a['name'] for a in accounts)}")
print(f"Time: {time.strftime('%Y-%m-%d %H:%M:%S')}")
print("="*70)

all_results = []
test_num = 0

for prompt, label in PROMPTS:
    for iteration in range(1, ITERATIONS + 1):
        test_num += 1
        # Rotate accounts
        acct = accounts[(test_num - 1) % len(accounts)]
        
        stats = run_stream_test(acct, prompt, label, iteration)
        if stats:
            all_results.append(stats)
        
        # Brief pause between tests
        time.sleep(2)

# ============================================================================
# Final summary
# ============================================================================
print("\n" + "="*70)
print("FINAL SUMMARY")
print("="*70)

print(f"\n{'Label':<20} {'Iter':>4} {'Account':<15} {'Time':>7} {'TTFT':>7} {'Events':>7} {'MaxGap':>8} {'>2s':>4} {'>5s':>4} {'>10s':>5} {'>30s':>5} {'Done':>5} {'Error':>10}")
print("-" * 120)

for r in all_results:
    ttft = f"{r['ttft']:.3f}" if r.get('ttft') else "N/A"
    max_gap = f"{r['gap_max']:.3f}" if 'gap_max' in r else "N/A"
    done = "✓" if r['got_done'] else "✗"
    err = r.get('error', '') or ''
    print(f"{r['label']:<20} {r['iteration']:>4} {r['account']:<15} {r['total_time']:>7.1f} {ttft:>7} {r['total_events']:>7} {max_gap:>8} {r.get('gaps_over_2s',0):>4} {r.get('gaps_over_5s',0):>4} {r.get('gaps_over_10s',0):>5} {r.get('gaps_over_30s',0):>5} {done:>5} {err:>10}")

# Aggregate analysis
successful = [r for r in all_results if r.get('got_done') and not r.get('error')]
failed = [r for r in all_results if not r.get('got_done') or r.get('error')]

print(f"\nSuccessful: {len(successful)}/{len(all_results)}")
print(f"Failed/Stale: {len(failed)}/{len(all_results)}")

if successful:
    all_max_gaps = [r['gap_max'] for r in successful if 'gap_max' in r]
    all_ttfts = [r['ttft'] for r in successful if r.get('ttft')]
    all_times = [r['total_time'] for r in successful]

    print(f"\nSuccessful streams:")
    print(f"  TTFT: min={min(all_ttfts):.3f}s max={max(all_ttfts):.3f}s avg={statistics.mean(all_ttfts):.3f}s")
    print(f"  Total time: min={min(all_times):.1f}s max={max(all_times):.1f}s avg={statistics.mean(all_times):.1f}s")
    print(f"  Max inter-chunk gap: min={min(all_max_gaps):.3f}s max={max(all_max_gaps):.3f}s avg={statistics.mean(all_max_gaps):.3f}s")

# Save raw data
with open('/tmp/codex_gpt55_xhigh_timing.json', 'w') as f:
    json.dump(all_results, f, indent=2, default=str)
print(f"\nRaw data saved to /tmp/codex_gpt55_xhigh_timing.json")
