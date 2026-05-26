#!/usr/bin/env python3
"""
Stream timing analyzer for Codex/OpenAI streaming responses.
Tests real credentials to observe chunk timing patterns.
"""

import json
import time
import requests
from datetime import datetime
from typing import List, Dict, Any

# Load credentials
with open('/home/zhafron/sub2api-account-20260526091014.json', 'r') as f:
    data = json.load(f)

# Use first available account
account = data['accounts'][0]
credentials = account['credentials']

print(f"Using account: {account['name']} ({credentials['email']})")
print(f"Plan: {credentials['plan_type']}")
print("=" * 80)

# Test scenarios
test_cases = [
    {
        'name': 'Simple quick response',
        'prompt': 'What is 2+2? Answer in one sentence.',
        'expected_duration': 'short'
    },
    {
        'name': 'Medium complexity',
        'prompt': 'Explain how Docker containers work in 3 paragraphs.',
        'expected_duration': 'medium'
    },
    {
        'name': 'Long code generation',
        'prompt': 'Write a complete Python Flask application with user authentication, database models, and API endpoints. Include all necessary imports and setup code.',
        'expected_duration': 'long'
    },
    {
        'name': 'Complex reasoning',
        'prompt': 'Analyze the pros and cons of microservices vs monolithic architecture. Provide detailed examples and a recommendation.',
        'expected_duration': 'long'
    }
]

def test_stream(test_case: Dict[str, Any]):
    print(f"\nTest: {test_case['name']}")
    print(f"Prompt: {test_case['prompt'][:100]}...")
    print(f"Expected: {test_case['expected_duration']}")
    print("-" * 80)
    
    # Prepare request
    headers = {
        'Authorization': f'Bearer {credentials["access_token"]}',
        'Content-Type': 'application/json'
    }
    
    payload = {
        'model': 'gpt-4o-mini',  # Use faster model for testing
        'messages': [
            {'role': 'user', 'content': test_case['prompt']}
        ],
        'stream': True,
        'max_tokens': 1000
    }
    
    # Track timing
    start_time = time.time()
    first_token_time = None
    chunk_times = []
    inter_chunk_intervals = []
    total_chunks = 0
    total_tokens = 0
    
    try:
        # Make request
        response = requests.post(
            'https://api.openai.com/v1/chat/completions',
            headers=headers,
            json=payload,
            stream=True,
            timeout=120
        )
        
        print(f"Status: {response.status_code}")
        
        if response.status_code != 200:
            print(f"Error: {response.text}")
            return None
        
        # Process stream
        for line in response.iter_lines():
            if not line:
                continue
            
            line_str = line.decode('utf-8')
            
            if line_str.startswith('data: '):
                data_str = line_str[6:]
                
                if data_str == '[DONE]':
                    break
                
                try:
                    chunk_data = json.loads(data_str)
                    chunk_time = time.time()
                    
                    # Track first token
                    if first_token_time is None:
                        first_token_time = chunk_time
                        print(f"First token: {chunk_time - start_time:.3f}s")
                    
                    # Track chunk timing
                    chunk_times.append(chunk_time)
                    if len(chunk_times) > 1:
                        interval = chunk_time - chunk_times[-2]
                        inter_chunk_intervals.append(interval)
                    
                    total_chunks += 1
                    
                    # Count tokens (rough estimate)
                    if 'choices' in chunk_data and len(chunk_data['choices']) > 0:
                        delta = chunk_data['choices'][0].get('delta', {})
                        content = delta.get('content', '')
                        if content:
                            total_tokens += len(content.split())
                    
                    # Print progress every 10 chunks
                    if total_chunks % 10 == 0:
                        elapsed = chunk_time - start_time
                        print(f"  Chunk {total_chunks}: {elapsed:.2f}s elapsed, ~{total_tokens} words")
                
                except json.JSONDecodeError:
                    pass
    
    except Exception as e:
        print(f"Error during stream: {e}")
        return None
    
    end_time = time.time()
    total_duration = end_time - start_time
    
    # Analyze results
    print("\nResults:")
    print(f"  Total duration: {total_duration:.2f}s")
    print(f"  Total chunks: {total_chunks}")
    print(f"  Approx tokens: ~{total_tokens} words")
    
    if first_token_time:
        time_to_first_token = first_token_time - start_time
        print(f"  Time to first token: {time_to_first_token:.3f}s")
    
    if inter_chunk_intervals:
        avg_interval = sum(inter_chunk_intervals) / len(inter_chunk_intervals)
        max_interval = max(inter_chunk_intervals)
        min_interval = min(inter_chunk_intervals)
        
        # Count long gaps (> 2s)
        long_gaps = [i for i in inter_chunk_intervals if i > 2.0]
        
        print(f"  Avg inter-chunk interval: {avg_interval:.3f}s")
        print(f"  Min inter-chunk interval: {min_interval:.3f}s")
        print(f"  Max inter-chunk interval: {max_interval:.3f}s")
        print(f"  Gaps > 2s: {len(long_gaps)}")
        
        if long_gaps:
            print(f"  Longest gaps: {sorted(long_gaps, reverse=True)[:5]}")
        
        # Calculate chunks per second
        chunks_per_sec = total_chunks / total_duration if total_duration > 0 else 0
        print(f"  Chunks per second: {chunks_per_sec:.2f}")
    
    return {
        'test_name': test_case['name'],
        'expected': test_case['expected_duration'],
        'duration': total_duration,
        'chunks': total_chunks,
        'time_to_first_token': time_to_first_token if first_token_time else None,
        'avg_interval': avg_interval if inter_chunk_intervals else None,
        'max_interval': max_interval if inter_chunk_intervals else None,
        'long_gaps': len(long_gaps) if inter_chunk_intervals else 0
    }

# Run tests
print("\nStarting stream timing tests...")
print(f"Time: {datetime.now().isoformat()}")

results = []
for test_case in test_cases:
    result = test_stream(test_case)
    if result:
        results.append(result)
    
    # Wait between tests
    print("\nWaiting 3 seconds...")
    time.sleep(3)

# Summary
print("\n" + "=" * 80)
print("SUMMARY")
print("=" * 80)
print(f"{'Test':<25} {'Expected':<10} {'Duration':<10} {'Chunks':<8} {'First Token':<12} {'Max Gap':<10} {'Long Gaps':<10}")
print("-" * 80)

for r in results:
    first_token = f"{r['time_to_first_token']:.3f}s" if r['time_to_first_token'] else "N/A"
    max_gap = f"{r['max_interval']:.3f}s" if r['max_interval'] else "N/A"
    print(f"{r['test_name']:<25} {r['expected']:<10} {r['duration']:<10.2f} {r['chunks']:<8} {first_token:<12} {max_gap:<10} {r['long_gaps']:<10}")

print("\nAnalysis:")
print("- Legitimate streams show regular chunk flow with occasional gaps")
print("- Stale streams would show: very long gaps (>30s) or no first token")
print("- Use this data to set intelligent thresholds")
