#!/usr/bin/env python3
"""Collect streaming timing patterns from Codex API"""
import json
import requests
import time
import statistics

with open('/home/zhafron/sub2api-account-20260526091014.json') as f:
    data = json.load(f)

creds = data['accounts'][0]['credentials']
headers = {
    'Authorization': f'Bearer {creds["access_token"]}',
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream'
}

def collect_stream_stats(prompt, label):
    """Collect timing statistics for a single streaming request"""
    payload = {
        'model': 'gpt-5.4-mini',
        'instructions': 'Be helpful and concise.',
        'input': [{'type': 'message', 'role': 'user', 'content': prompt}],
        'stream': True,
        'store': False
    }
    
    start = time.time()
    resp = requests.post(
        'https://chatgpt.com/backend-api/codex/responses',
        headers=headers,
        json=payload,
        stream=True,
        timeout=120
    )
    
    if resp.status_code != 200:
        return None
    
    chunks = []
    last_time = start
    first_time = None
    
    for line in resp.iter_lines():
        now = time.time()
        if not first_time:
            first_time = now
        
        line_str = line.decode('utf-8') if isinstance(line, bytes) else str(line)
        if line_str and not line_str.startswith('event:') and not line_str.startswith('data:'):
            continue
            
        gap = now - last_time
        chunks.append({
            'time': now - start,
            'gap': gap,
            'event': line_str[:80]
        })
        last_time = now
    
    total_time = time.time() - start
    
    return {
        'label': label,
        'total_chunks': len(chunks),
        'total_time': total_time,
        'ttft': first_time - start if first_time else 0,
        'chunk_gaps': [c['gap'] for c in chunks[1:]],
        'chunks': chunks
    }

# Test scenarios
scenarios = [
    ("Say hello", "simple_greeting"),
    ("What is 2+2?", "simple_math"),
    ("List 5 colors", "simple_list"),
    ("Explain what an API is in one sentence", "simple_explanation"),
    ("Write a haiku about coding", "creative_short"),
]

print("Collecting streaming timing data...\n")
results = []

for prompt, label in scenarios:
    print(f"Testing: {label}")
    stats = collect_stream_stats(prompt, label)
    
    if stats:
        gaps = stats['chunk_gaps']
        print(f"  Chunks: {stats['total_chunks']}")
        print(f"  Total time: {stats['total_time']:.3f}s")
        print(f"  TTFT: {stats['ttft']:.3f}s")
        print(f"  Chunk gaps - avg: {statistics.mean(gaps):.3f}s, max: {max(gaps):.3f}s, p95: {sorted(gaps)[int(len(gaps)*0.95)]:.3f}s")
        results.append(stats)
    else:
        print(f"  FAILED")
    
    time.sleep(1)  # Rate limit courtesy

print("\n" + "="*70)
print("AGGREGATE STATISTICS")
print("="*70)

all_gaps = []
all_ttfts = []
all_totals = []

for r in results:
    all_gaps.extend(r['chunk_gaps'])
    all_ttfts.append(r['ttft'])
    all_totals.append(r['total_time'])

print(f"\nTime to First Token (TTFT):")
print(f"  Min: {min(all_ttfts):.3f}s")
print(f"  Max: {max(all_ttfts):.3f}s")
print(f"  Avg: {statistics.mean(all_ttfts):.3f}s")
print(f"  P95: {sorted(all_ttfts)[int(len(all_ttfts)*0.95)]:.3f}s")

print(f"\nChunk Gap (inter-chunk interval):")
print(f"  Min: {min(all_gaps):.3f}s")
print(f"  Max: {max(all_gaps):.3f}s")
print(f"  Avg: {statistics.mean(all_gaps):.3f}s")
print(f"  P95: {sorted(all_gaps)[int(len(all_gaps)*0.95)]:.3f}s")
print(f"  P99: {sorted(all_gaps)[int(len(all_gaps)*0.99)]:.3f}s")

print(f"\nTotal Request Time:")
print(f"  Min: {min(all_totals):.3f}s")
print(f"  Max: {max(all_totals):.3f}s")
print(f"  Avg: {statistics.mean(all_totals):.3f}s")

# Find anomalous gaps
large_gaps = [(i, g) for i, g in enumerate(all_gaps) if g > 1.0]
if large_gaps:
    print(f"\nLarge gaps (>1s): {len(large_gaps)} occurrences")
    for idx, gap in large_gaps[:5]:
        print(f"  Gap #{idx}: {gap:.3f}s")

# Save raw data
with open('/tmp/codex_timing_data.json', 'w') as f:
    json.dump({
        'results': [{
            'label': r['label'],
            'total_chunks': r['total_chunks'],
            'total_time': r['total_time'],
            'ttft': r['ttft'],
            'chunk_gaps': r['chunk_gaps']
        } for r in results]
    }, f, indent=2)

print("\nData saved to /tmp/codex_timing_data.json")
