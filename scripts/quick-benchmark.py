#!/usr/bin/env python3
"""Quick benchmark using existing model-loader instance."""

import requests
import time
import json
from datetime import datetime

PROFILES = [
    ("qwen3.6-27b-mtp-pi-tune-q4km-262k", "MTP-Only"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-quality", "Quality"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-throughput", "Throughput"),
    ("qwen3.6-27b-mtp-dflash-q4km-262k-speed", "Speed"),
]

PROMPTS = [
    ("short", "O que é especulative decoding?", 100),
    ("medium", "Explique como MTP e DFlash funcionam juntos para acelerar inferência.", 500),
    ("long", "Descreva arquitetura de inference com especulative decoding: (1) MTP, (2) DFlash, (3) cross-attention, (4) KV cache q4_0, (5) parallelism.", 1000),
]

API_URL = "http://127.0.0.1:4321/v1/chat/completions"

def test_profile(profile_id, profile_name):
    """Test a single profile with all prompts."""
    print(f"\n{'='*70}")
    print(f"Testing: {profile_name} ({profile_id})")
    print(f"{'='*70}")

    results = {}

    for prompt_name, prompt_text, max_tokens in PROMPTS:
        try:
            print(f"\n  📝 {prompt_name.upper()}: ", end="", flush=True)

            start = time.time()
            response = requests.post(
                API_URL,
                json={
                    "model": profile_id,
                    "messages": [{"role": "user", "content": prompt_text}],
                    "temperature": 0.7,
                    "max_tokens": max_tokens,
                },
                timeout=300,
            )
            elapsed = time.time() - start

            if response.status_code == 200:
                data = response.json()
                tokens = data["usage"]["completion_tokens"]
                tok_per_sec = tokens / elapsed if elapsed > 0 else 0

                results[prompt_name] = {
                    "status": "success",
                    "elapsed": elapsed,
                    "tokens": tokens,
                    "tok_per_sec": tok_per_sec,
                }

                print(f"✅ {tok_per_sec:.1f} tok/s ({tokens} tokens, {elapsed:.2f}s)")
            else:
                results[prompt_name] = {
                    "status": "error",
                    "error": f"HTTP {response.status_code}",
                }
                print(f"❌ HTTP {response.status_code}")

        except Exception as e:
            results[prompt_name] = {
                "status": "error",
                "error": str(e),
            }
            print(f"❌ {str(e)[:50]}")

    return results

def main():
    print("\n" + "="*70)
    print("🧪 QWEN3.6-27B BENCHMARK - QUICK VERSION")
    print("="*70)
    print(f"API: {API_URL}")
    print(f"Time: {datetime.now().isoformat()}")
    print(f"Profiles: {len(PROFILES)}, Prompts: {len(PROMPTS)}")
    print("="*70)

    all_results = {}
    baseline_speeds = None

    for profile_id, profile_name in PROFILES:
        results = test_profile(profile_id, profile_name)
        all_results[profile_name] = results

        # Store baseline speeds for comparison
        if profile_name == "MTP-Only":
            baseline_speeds = {
                k: v.get("tok_per_sec", 0) for k, v in results.items() if v.get("status") == "success"
            }

    # Print summary
    print("\n" + "="*70)
    print("📊 SUMMARY")
    print("="*70)
    print(f"\n{'Profile':<30} | {'Short (tok/s)':<15} | {'Medium':<15} | {'Long':<15} | {'Avg':<10}")
    print("-"*90)

    for profile_name, results in all_results.items():
        speeds = []
        row = f"{profile_name:<30} |"

        for prompt_name, _ in [("short", "Short"), ("medium", "Medium"), ("long", "Long")]:
            if prompt_name in results:
                result = results[prompt_name]
                if result.get("status") == "success":
                    speed = result["tok_per_sec"]
                    speeds.append(speed)
                    row += f" {speed:>13.1f} |"
                else:
                    row += f" {'ERROR':<13} |"
            else:
                row += f" {'N/A':<13} |"

        avg = sum(speeds) / len(speeds) if speeds else 0
        row += f" {avg:>8.1f}"
        print(row)

    # Print speedups
    if baseline_speeds:
        print("\n" + "="*70)
        print("📈 SPEEDUP vs MTP-Only")
        print("="*70)

        for profile_name, results in list(all_results.items())[1:]:
            print(f"\n{profile_name}:")

            for prompt_name in ["short", "medium", "long"]:
                if prompt_name in results and prompt_name in baseline_speeds:
                    result = results[prompt_name]
                    if result.get("status") == "success":
                        baseline = baseline_speeds[prompt_name]
                        current = result["tok_per_sec"]
                        speedup = current / baseline if baseline > 0 else 0
                        print(f"  {prompt_name:<10}: {speedup:>5.2f}x ({baseline:.1f} → {current:.1f} tok/s)")

    # Save results
    results_file = "/tmp/qwen-quick-benchmark.json"
    with open(results_file, "w") as f:
        json.dump(all_results, f, indent=2)

    print(f"\n✅ Results saved to: {results_file}")
    print("="*70)

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n⚠️  Interrupted by user")
    except Exception as e:
        print(f"\n❌ Error: {e}")
        import traceback
        traceback.print_exc()
