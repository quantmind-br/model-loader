#!/usr/bin/env python3
"""Simple direct test of profiles."""

import subprocess
import time
import requests
import json

def test_single_profile(profile_id):
    """Test a single profile."""
    print(f"\n{'='*60}")
    print(f"Testing: {profile_id}")
    print('='*60)

    # Start profile
    print(f"Starting profile...")
    result = subprocess.run(
        ["model-loader", "instance", "start", profile_id],
        capture_output=True,
        text=True,
        timeout=30
    )

    if result.returncode != 0:
        print(f"❌ Start failed: {result.stderr[:200]}")
        return None

    print(f"✅ Profile started")
    time.sleep(5)  # Wait for model to load

    # Test inference
    print(f"\nTesting inference...")
    try:
        start = time.time()
        response = requests.post(
            "http://127.0.0.1:4321/v1/chat/completions",
            json={
                "model": profile_id,
                "messages": [{"role": "user", "content": "O que é IA? Responda em uma frase."}],
                "temperature": 0.7,
                "max_tokens": 50,
            },
            timeout=120
        )
        elapsed = time.time() - start

        if response.status_code == 200:
            data = response.json()
            tokens = data["usage"]["completion_tokens"]
            content = data["choices"][0]["message"]["content"]
            tok_per_sec = tokens / elapsed

            print(f"✅ Success!")
            print(f"   Tokens: {tokens}")
            print(f"   Time: {elapsed:.2f}s")
            print(f"   Speed: {tok_per_sec:.1f} tok/s")
            print(f"   Response: {content[:80]}...")

            return {
                "tokens": tokens,
                "elapsed": elapsed,
                "tok_per_sec": tok_per_sec
            }
        else:
            print(f"❌ HTTP {response.status_code}")
            return None

    except Exception as e:
        print(f"❌ Error: {e}")
        return None

# Test MTP-Only first
result = test_single_profile("qwen3.6-27b-mtp-pi-tune-q4km-262k")

if result:
    print(f"\n{'='*60}")
    print(f"✅ TEST SUCCESSFUL")
    print(f"{'='*60}")
    print(json.dumps(result, indent=2))
else:
    print(f"\n{'='*60}")
    print(f"❌ TEST FAILED")
    print(f"{'='*60}")
