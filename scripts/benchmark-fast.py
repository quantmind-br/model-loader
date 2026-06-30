#!/usr/bin/env python3
"""Fast benchmark of all 4 Qwen profiles."""

import subprocess
import time
import requests
import json
from datetime import datetime

PROFILES = [
    ("qwen3.6-27b-mtp-pi-tune-q4km-262k", "MTP-Only"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-quality", "Quality"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-throughput", "Throughput"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-speed", "Speed"),
]

PROMPTS = [
    ("short", "O que é IA?", 50),
    ("medium", "Explique especulative decoding com MTP e DFlash.", 200),
    ("long", "Descreva como MTP e DFlash juntos aceleram inference.", 500),
]

API = "http://127.0.0.1:4321/v1/chat/completions"

def test_profile(profile_id, name):
    """Test single profile."""
    print(f"\n{'='*70}")
    print(f"Profile: {name} ({profile_id})")
    print('='*70)

    # Start
    print("Starting...", end=" ", flush=True)
    try:
        result = subprocess.run(
            ["model-loader", "instance", "start", profile_id],
            capture_output=True,
            text=True,
            timeout=30
        )
        if result.returncode != 0:
            print(f"❌ Start failed")
            return None
        print("✅")
    except Exception as e:
        print(f"❌ {e}")
        return None

    time.sleep(3)

    results = {}

    for prompt_name, prompt_text, max_tokens in PROMPTS:
        print(f"  {prompt_name:<10}: ", end="", flush=True)

        try:
            start = time.time()
            response = requests.post(
                API,
                json={
                    "model": profile_id,
                    "messages": [{"role": "user", "content": prompt_text}],
                    "temperature": 0.7,
                    "max_tokens": max_tokens,
                },
                timeout=120
            )
            elapsed = time.time() - start

            if response.status_code == 200:
                data = response.json()
                tokens = data["usage"]["completion_tokens"]
                tok_per_sec = tokens / elapsed

                results[prompt_name] = {
                    "tokens": tokens,
                    "elapsed": f"{elapsed:.2f}s",
                    "tok_per_sec": f"{tok_per_sec:.1f}",
                }
                print(f"✅ {tok_per_sec:.1f} tok/s")
            else:
                print(f"❌ HTTP {response.status_code}")
                results[prompt_name] = {"error": f"HTTP {response.status_code}"}

        except Exception as e:
            print(f"❌ {str(e)[:40]}")
            results[prompt_name] = {"error": str(e)[:60]}

    return results

print("\n" + "="*70)
print("🧪 QWEN3.6-27B BENCHMARK - ALL PROFILES")
print("="*70)
print(f"Time: {datetime.now().isoformat()}")
print(f"Profiles: {len(PROFILES)}, Prompts: {len(PROMPTS)}")
print("="*70)

all_results = {}

for profile_id, name in PROFILES:
    results = test_profile(profile_id, name)
    if results:
        all_results[name] = results

# Summary
print("\n" + "="*70)
print("📊 SUMMARY")
print("="*70)
print(f"\n{'Profile':<20} | {'Short':<12} | {'Medium':<12} | {'Long':<12} | {'Avg':<10}")
print("-"*70)

baseline = {}

for name, results in all_results.items():
    row = f"{name:<20} |"
    speeds = []

    for prompt_name in ["short", "medium", "long"]:
        if prompt_name in results and "tok_per_sec" in results[prompt_name]:
            speed_str = results[prompt_name]["tok_per_sec"]
            speed = float(speed_str.split()[0])
            speeds.append(speed)
            row += f" {speed_str:>11} |"
        else:
            row += f" {'ERROR':<11} |"

    if speeds:
        avg = sum(speeds) / len(speeds)
        row += f" {avg:>8.1f}"

        if name == "MTP-Only":
            baseline = {p: float(results[p]["tok_per_sec"].split()[0])
                       for p in ["short", "medium", "long"]
                       if p in results and "tok_per_sec" in results[p]}

    print(row)

# Speedups
if baseline:
    print("\n" + "="*70)
    print("📈 SPEEDUP vs MTP-Only")
    print("="*70)

    for name, results in list(all_results.items())[1:]:
        print(f"\n{name}:")
        for prompt_name in ["short", "medium", "long"]:
            if prompt_name in results and "tok_per_sec" in results[prompt_name]:
                speed = float(results[prompt_name]["tok_per_sec"].split()[0])
                base = baseline.get(prompt_name, 0)
                speedup = speed / base if base > 0 else 0
                print(f"  {prompt_name:<10}: {speedup:>5.2f}x ({base:.1f} → {speed:.1f} tok/s)")

print("\n" + "="*70)
print("✅ Benchmark complete!")
print("="*70)

# Save
with open("/tmp/benchmark-results.json", "w") as f:
    json.dump(all_results, f, indent=2)
print(f"\n📁 Results: /tmp/benchmark-results.json")
