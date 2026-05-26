#!/usr/bin/env python3
"""Quick diagnostic - is Codex API responding at all?"""
import json
import time
import requests

print("Loading credentials...")
with open('/home/zhafron/sub2api-account-20260526091014.json') as f:
    data = json.load(f)

creds = data['accounts'][0]['credentials']
print(f"Using account: {creds['email']}")

# Simple non-streaming test first
print("\n=== Test 1: Non-streaming request (quick check) ===")
headers = {
    "Authorization": f"Bearer {creds['access_token']}",
    "Content-Type": "application/json"
}

payload = {
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Say hello"}],
    "max_tokens": 50
}

start = time.time()
try:
    print("Sending request...")
    resp = requests.post(
        "https://api.openai.com/v1/chat/completions",
        headers=headers,
        json=payload,
        timeout=30
    )
    elapsed = time.time() - start
    print(f"Status: {resp.status_code} ({elapsed:.2f}s)")
    if resp.status_code == 200:
        result = resp.json()
        print(f"Response: {result['choices'][0]['message']['content']}")
    else:
        print(f"Error: {resp.text[:200]}")
except Exception as e:
    print(f"Exception: {e}")

# Now test Codex endpoint
print("\n=== Test 2: Codex endpoint (streaming) ===")
codex_headers = {
    "Authorization": f"Bearer {creds['access_token']}",
    "Content-Type": "application/json",
    "Accept": "text/event-stream"
}

codex_payload = {
    "model": "gpt-4o-mini",
    "instructions": "Be helpful",
    "input": [{"type": "message", "role": "user", "content": "Say hello"}],
    "stream": True,
    "max_output_tokens": 100
}

start = time.time()
try:
    print("Sending Codex streaming request...")
    resp = requests.post(
        "https://chatgpt.com/backend-api/codex/responses",
        headers=codex_headers,
        json=codex_payload,
        stream=True,
        timeout=30
    )
    print(f"Status: {resp.status_code} ({time.time()-start:.2f}s)")
    
    if resp.status_code != 200:
        print(f"Error: {resp.text[:200]}")
    else:
        print("Reading stream...")
        chunk_count = 0
        for line in resp.iter_lines():
            chunk_count += 1
            if chunk_count > 10:  # Just read first few chunks
                break
            print(f"Chunk {chunk_count}: {line.decode('utf-8')[:100]}")
        
        print(f"Read {chunk_count} chunks successfully")
        
except Exception as e:
    elapsed = time.time() - start
    print(f"Exception after {elapsed:.2f}s: {e}")

print("\nDone!")
