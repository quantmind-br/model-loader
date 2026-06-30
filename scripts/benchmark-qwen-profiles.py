#!/usr/bin/env python3
"""
Benchmark workflow for Qwen3.6-27B MTP vs MTP+DFlash profiles.
Tests all 4 profile configurations and generates a comparison report.
"""

import json
import subprocess
import time
import requests
import sys
from datetime import datetime
from typing import Dict, List, Any
from pathlib import Path

# Profile configurations to test
PROFILES = [
    {
        "id": "qwen3.6-27b-mtp-pi-tune-q4km-262k",
        "name": "MTP-Only (baseline)",
        "category": "baseline",
    },
    {
        "id": "qwen3.6-27b-mtp-dflash-q4km-262k-quality",
        "name": "MTP+DFlash (Quality)",
        "category": "dflash",
    },
    {
        "id": "qwen3.6-27b-mtp-dflash-q4km-262k-throughput",
        "name": "MTP+DFlash (Throughput)",
        "category": "dflash",
    },
    {
        "id": "qwen3.6-27b-mtp-dflash-q4km-262k-speed",
        "name": "MTP+DFlash (Speed)",
        "category": "dflash",
    },
]

# Test prompts (various complexities)
TEST_PROMPTS = [
    {
        "name": "short",
        "content": "O que é especulative decoding?",
        "max_tokens": 100,
    },
    {
        "name": "medium",
        "content": "Explique em detalhe como o speculative decoding com MTP e DFlash funciona juntos para acelerar a inferência.",
        "max_tokens": 500,
    },
    {
        "name": "long",
        "content": "Descreva arquitetura completa de um sistema de inference de linguagem com suporte a especulative decoding multi-nível, incluindo: (1) como MTP built-in funciona, (2) como DFlash draft heads aumentam acceptance rate, (3) cross-attention em janelas de contexto, (4) gestão de memória KV cache em quantização q4_0, (5) estratégias de batching para multi-stream parallelism.",
        "max_tokens": 1000,
    },
]

class ProfileBenchmark:
    def __init__(self):
        self.results: Dict[str, Dict[str, Any]] = {}
        self.api_url = "http://127.0.0.1:4321/v1/chat/completions"
        self.timeout = 300

    def launch_profile(self, profile_id: str) -> bool:
        """Launch a profile using model-loader CLI."""
        print(f"  🚀 Launching profile: {profile_id}")
        try:
            result = subprocess.run(
                ["model-loader", "instance", "start", profile_id],
                timeout=30,
                capture_output=True,
                text=True,
            )
            if result.returncode != 0:
                print(f"    ❌ Launch failed: {result.stderr}")
                return False

            # Wait for API to be ready
            print(f"  ⏳ Waiting for server to be ready...")
            start = time.time()
            while time.time() - start < 60:
                try:
                    requests.get(f"{self.api_url.rsplit('/', 1)[0]}/health", timeout=2)
                    print(f"  ✅ Server ready after {time.time() - start:.1f}s")
                    time.sleep(2)  # Extra buffer
                    return True
                except:
                    time.sleep(1)

            print(f"  ❌ Server didn't become ready")
            return False
        except subprocess.TimeoutExpired:
            print(f"  ❌ Launch timed out")
            return False
        except Exception as e:
            print(f"  ❌ Error launching: {e}")
            return False

    def test_prompt(self, profile_id: str, prompt_info: Dict[str, Any]) -> Dict[str, Any]:
        """Test a single prompt against the API."""
        start_time = time.time()
        try:
            response = requests.post(
                self.api_url,
                json={
                    "model": profile_id,
                    "messages": [{"role": "user", "content": prompt_info["content"]}],
                    "temperature": 0.7,
                    "max_tokens": prompt_info["max_tokens"],
                },
                timeout=self.timeout,
            )

            elapsed = time.time() - start_time

            if response.status_code == 200:
                data = response.json()
                choice = data["choices"][0]
                content = choice.get("message", {}).get("content", "")
                tokens = data["usage"]["completion_tokens"]

                return {
                    "status": "success",
                    "elapsed_sec": elapsed,
                    "tokens_generated": tokens,
                    "tokens_per_sec": tokens / elapsed if elapsed > 0 else 0,
                    "content_length": len(content),
                    "finish_reason": choice.get("finish_reason", "unknown"),
                }
            else:
                return {
                    "status": "error",
                    "error": f"HTTP {response.status_code}: {response.text[:100]}",
                    "elapsed_sec": elapsed,
                }
        except Exception as e:
            return {
                "status": "error",
                "error": str(e),
                "elapsed_sec": time.time() - start_time,
            }

    def benchmark_profile(self, profile: Dict[str, Any]) -> Dict[str, Any]:
        """Run full benchmark suite for a profile."""
        profile_id = profile["id"]
        print(f"\n{'='*60}")
        print(f"Testing: {profile['name']}")
        print(f"{'='*60}")

        # Launch profile
        if not self.launch_profile(profile_id):
            return {"profile": profile, "status": "failed_to_launch"}

        # Test each prompt
        results = {"profile": profile, "tests": {}, "status": "success"}

        for prompt_info in TEST_PROMPTS:
            prompt_name = prompt_info["name"]
            print(f"\n  📝 Testing {prompt_name} prompt...")

            test_result = self.test_prompt(profile_id, prompt_info)
            results["tests"][prompt_name] = test_result

            if test_result["status"] == "success":
                print(
                    f"    ✅ {test_result['tokens_per_sec']:.1f} tok/s "
                    f"({test_result['tokens_generated']} tokens in {test_result['elapsed_sec']:.2f}s)"
                )
            else:
                print(f"    ❌ {test_result['error']}")

        return results

    def run_benchmark_suite(self) -> List[Dict[str, Any]]:
        """Run benchmarks for all profiles."""
        print("\n" + "="*60)
        print("🧪 QWEN3.6-27B MTP vs MTP+DFlash BENCHMARK SUITE")
        print("="*60)
        print(f"Started: {datetime.now().isoformat()}")
        print(f"Profiles: {len(PROFILES)}")
        print(f"Prompts per profile: {len(TEST_PROMPTS)}")
        print("="*60)

        all_results = []
        for i, profile in enumerate(PROFILES, 1):
            print(f"\n[{i}/{len(PROFILES)}] Testing {profile['name']}")
            result = self.benchmark_profile(profile)
            all_results.append(result)

            if i < len(PROFILES):
                print("\n⏸️  Waiting 10 seconds before next test...")
                time.sleep(10)

        return all_results

    def generate_report(self, results: List[Dict[str, Any]]) -> str:
        """Generate formatted benchmark report."""
        report = []
        report.append("\n" + "="*80)
        report.append("📊 BENCHMARK REPORT - Qwen3.6-27B MTP vs MTP+DFlash")
        report.append("="*80)
        report.append(f"Generated: {datetime.now().isoformat()}\n")

        # Summary table
        report.append("PERFORMANCE SUMMARY (tokens/sec)")
        report.append("-" * 80)
        report.append(f"{'Profile':<40} | {'Short':<10} | {'Medium':<10} | {'Long':<10} | {'Avg':<10}")
        report.append("-" * 80)

        for result in results:
            if result["status"] != "success":
                profile_name = result["profile"]["name"]
                report.append(f"{profile_name:<40} | {'FAILED':<10}")
                continue

            profile_name = result["profile"]["name"]
            tests = result["tests"]

            speeds = []
            row_parts = [f"{profile_name:<40}"]
            for prompt_name in ["short", "medium", "long"]:
                if prompt_name in tests:
                    test = tests[prompt_name]
                    if test["status"] == "success":
                        speed = test["tokens_per_sec"]
                        speeds.append(speed)
                        row_parts.append(f"{speed:>9.1f} tok/s")
                    else:
                        speeds.append(0)
                        row_parts.append(f"{'ERROR':<10}")

            avg = sum(speeds) / len(speeds) if speeds else 0
            row_parts.append(f"{avg:>9.1f}")
            report.append(" | ".join(row_parts))

        report.append("-" * 80)
        report.append("")

        # Detailed results
        report.append("DETAILED RESULTS")
        report.append("="*80)

        for result in results:
            if result["status"] != "success":
                profile_name = result["profile"]["name"]
                report.append(f"\n❌ {profile_name}: FAILED TO LAUNCH")
                continue

            profile = result["profile"]
            tests = result["tests"]

            report.append(f"\n✅ {profile['name']}")
            report.append(f"   Category: {profile['category']}")
            report.append(f"   ID: {profile['id']}")

            for prompt_name, test_result in tests.items():
                if test_result["status"] == "success":
                    report.append(
                        f"\n   📝 {prompt_name.upper()}: "
                        f"{test_result['tokens_per_sec']:.1f} tok/s "
                        f"({test_result['tokens_generated']} tokens, {test_result['elapsed_sec']:.2f}s)"
                    )
                else:
                    report.append(f"\n   ❌ {prompt_name.upper()}: {test_result['error']}")

        # Comparison analysis
        report.append("\n\n" + "="*80)
        report.append("📈 ANALYSIS & SPEEDUP COMPARISON")
        report.append("="*80)

        # Compare MTP+DFlash variants against baseline
        baseline = results[0] if results else None
        if baseline and baseline["status"] == "success":
            baseline_speeds = {}
            for prompt_name in ["short", "medium", "long"]:
                if prompt_name in baseline["tests"]:
                    test = baseline["tests"][prompt_name]
                    if test["status"] == "success":
                        baseline_speeds[prompt_name] = test["tokens_per_sec"]

            report.append("\nSpeedup vs MTP-Only baseline:")
            for result in results[1:]:  # Skip baseline
                if result["status"] != "success":
                    continue

                profile_name = result["profile"]["name"]
                report.append(f"\n{profile_name}:")

                for prompt_name in ["short", "medium", "long"]:
                    if prompt_name in result["tests"] and prompt_name in baseline_speeds:
                        test = result["tests"][prompt_name]
                        if test["status"] == "success":
                            baseline_speed = baseline_speeds[prompt_name]
                            current_speed = test["tokens_per_sec"]
                            speedup = current_speed / baseline_speed if baseline_speed > 0 else 0
                            report.append(
                                f"  {prompt_name:<10}: {speedup:>5.2f}x "
                                f"({baseline_speed:.1f} → {current_speed:.1f} tok/s)"
                            )

        report.append("\n" + "="*80)
        return "\n".join(report)

    def save_results(self, results: List[Dict[str, Any]], report: str):
        """Save benchmark results to files."""
        # Save JSON results
        results_file = Path("/tmp/qwen-benchmark-results.json")
        with open(results_file, "w") as f:
            json.dump(results, f, indent=2)
        print(f"\n💾 Results saved to: {results_file}")

        # Save report
        report_file = Path("/tmp/qwen-benchmark-report.txt")
        with open(report_file, "w") as f:
            f.write(report)
        print(f"📄 Report saved to: {report_file}")

        return results_file, report_file


def main():
    benchmark = ProfileBenchmark()

    try:
        # Run benchmark suite
        results = benchmark.run_benchmark_suite()

        # Generate and display report
        report = benchmark.generate_report(results)
        print(report)

        # Save results
        benchmark.save_results(results, report)

        print("\n✅ Benchmark completed successfully!")
        return 0

    except KeyboardInterrupt:
        print("\n\n⚠️  Benchmark interrupted by user")
        return 130
    except Exception as e:
        print(f"\n❌ Benchmark failed: {e}")
        import traceback

        traceback.print_exc()
        return 1


if __name__ == "__main__":
    sys.exit(main())
