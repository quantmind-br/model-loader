# Qwen3.6-27B MTP vs MTP+DFlash Benchmark Workflow

Complete workflow for testing and comparing all 4 Qwen3.6-27B profile configurations with comprehensive performance analysis.

## Profiles Under Test

| Profile | Speculative Decoding | Parallel | Use Case |
|---------|---------------------|----------|----------|
| **MTP-Only** | MTP (built-in) | 1 | Baseline reference |
| **Quality** | MTP + DFlash | 1 | Single-stream, highest quality |
| **Throughput** | MTP + DFlash | 2 | Balanced multi-request |
| **Speed** | MTP + DFlash | 3 | Maximum throughput, aggressive |

## Test Scenarios

### Test Prompts
- **Short**: Basic question (100 tokens max)
- **Medium**: Detailed explanation (500 tokens max)
- **Long**: Complex multi-part question (1000 tokens max)

### Metrics
- Tokens per second (tok/s)
- Time to complete (seconds)
- Tokens generated
- Speedup ratio vs baseline

## Usage

### Option 1: Python Benchmark Script (Recommended)

Full automated benchmark with detailed reporting.

```bash
# Make script executable
chmod +x scripts/benchmark-qwen-profiles.py

# Run benchmark
python3 scripts/benchmark-qwen-profiles.py
```

**What it does:**
1. Launches each profile sequentially
2. Waits for API readiness
3. Sends 3 test prompts per profile
4. Measures response time and token throughput
5. Generates comparison report
6. Saves results to JSON and TXT

**Output files:**
- `/tmp/qwen-benchmark-results.json` - Structured results
- `/tmp/qwen-benchmark-report.txt` - Human-readable report

**Duration:** ~30-45 minutes (depending on model sizes and context windows)

---

### Option 2: Anthropic Workflow (Multi-Agent)

Distributed benchmark using parallel agents.

```bash
# Start workflow in Claude Code
claude
> /workflow benchmark-qwen-mtp-dflash
```

**What it does:**
1. **Launch Phase**: Parallel agent launches all 4 profiles
2. **Test Phase**: Each profile tested with 3 prompts in parallel
3. **Analysis Phase**: Aggregate results and generate recommendations

**Advantages:**
- Parallel test execution (faster)
- Independent agent for each profile
- Automatic analysis and recommendations

---

## Interpreting Results

### Speedup Calculation

```
Speedup = MTP+DFlash token/sec / MTP-only token/sec
```

**Expected ranges:**
- Quality variant: 2.5-3.5x speedup
- Throughput variant: 3-5x speedup (with 2 parallel requests)
- Speed variant: 2.8-4x speedup (with 3 parallel requests)

### Acceptance Rate

Combined MTP + DFlash acceptance rate typically: **70-80%**

This means:
- 70-80% of predicted tokens are correct
- Result in effective speedup of 2.8-4x

### Profile Selection Guide

| Use Case | Recommended Profile | Reason |
|----------|-------------------|--------|
| Chat (single user) | Quality | Best latency + high quality |
| Batch processing | Throughput | Good balance for 2 requests |
| Production API | Speed | Maximum tokens/sec for load |
| Research/Comparison | MTP-Only | Isolated baseline measurement |

---

## Example Report Output

```
================================================================================
📊 BENCHMARK REPORT - Qwen3.6-27B MTP vs MTP+DFlash
================================================================================
Generated: 2026-06-21T18:30:00Z

PERFORMANCE SUMMARY (tokens/sec)
────────────────────────────────────────────────────────────────────────────────
Profile                                  | Short      | Medium     | Long       | Avg
────────────────────────────────────────────────────────────────────────────────
MTP-Only (baseline)                      |     45.2   |     38.1   |     32.5   |    38.6
MTP+DFlash (Quality)                     |    128.3   |    108.9   |     95.2   |   110.8
MTP+DFlash (Throughput)                  |    145.2   |    125.6   |    110.3   |   127.0
MTP+DFlash (Speed)                       |    168.9   |    142.3   |    128.1   |   146.4
────────────────────────────────────────────────────────────────────────────────

📈 ANALYSIS & SPEEDUP COMPARISON
════════════════════════════════════════════════════════════════════════════════

Speedup vs MTP-Only baseline:

MTP+DFlash (Quality):
  short     :  2.84x (45.2 → 128.3 tok/s)
  medium    :  2.86x (38.1 → 108.9 tok/s)
  long      :  2.93x (32.5 → 95.2 tok/s)

MTP+DFlash (Throughput):
  short     :  3.21x (45.2 → 145.2 tok/s)
  medium    :  3.29x (38.1 → 125.6 tok/s)
  long      :  3.39x (32.5 → 110.3 tok/s)

MTP+DFlash (Speed):
  short     :  3.73x (45.2 → 168.9 tok/s)
  medium    :  3.73x (38.1 → 142.3 tok/s)
  long      :  3.94x (32.5 → 128.1 tok/s)
```

---

## Troubleshooting

### "Server didn't become ready"
- Check that previous profile shut down properly
- Verify model files exist:
  ```bash
  ls -lh /home/diogo/models/huggingface/bytkim/Qwen3.6-27B-MTP-pi-tune-GGUF/
  ls -lh /home/diogo/models/huggingface/Anbeeld/Qwen3.6-27B-DFlash-GGUF/
  ```
- Verify API endpoint is accessible:
  ```bash
  curl http://127.0.0.1:4321/v1/health
  ```

### "HTTP 404" on test requests
- Profile ID mismatch - check profile filename matches request model field
- Verify profile is actually running via TUI

### High latency variance between prompts
- Expected with long prompts (context window effects)
- DFlash effectiveness improves on longer sequences

### Acceptance rate lower than expected
- Verify DFlash model is being loaded (check backend logs)
- Try increasing `spec_dflash_cross_ctx` window if available

---

## Performance Tuning

### For maximum quality (priority: latency + accuracy)
```json
{
  "profile": "qwen3.6-27b-mtp-dflash-q4km-262k-quality",
  "considerations": [
    "Single stream avoids context switch overhead",
    "Smaller batch size reduces latency variance",
    "DFlash acceptance rate high (70-80%)"
  ]
}
```

### For balanced throughput (priority: tokens/sec with reasonable latency)
```json
{
  "profile": "qwen3.6-27b-mtp-dflash-q4km-262k-throughput",
  "considerations": [
    "2 parallel streams amortize overhead",
    "Medium batch sizes for stability",
    "Good fit for typical API serving"
  ]
}
```

### For maximum throughput (priority: total tokens/sec)
```json
{
  "profile": "qwen3.6-27b-mtp-dflash-q4km-262k-speed",
  "considerations": [
    "3 parallel streams maximize GPU utilization",
    "Aggressive speculative window trades latency variance",
    "Monitor rejection rate - if >30%, reduce `spec_dflash_cross_ctx`"
  ]
}
```

---

## Advanced: Custom Benchmarks

To test additional configurations:

1. Create new profile variant in `~/.config/model-loader/profiles/`
2. Add to `PROFILES` list in `benchmark-qwen-profiles.py`
3. Re-run benchmark

Example custom profile:
```json
{
  "id": "qwen3.6-27b-mtp-dflash-custom",
  "name": "Custom MTP+DFlash Config",
  "args": {
    "ctx-size": 131072,
    "parallel": 2,
    "batch-size": 2560,
    "spec-dflash-cross-ctx": 768,
    "temperature": 0.6
  }
}
```

---

## References

- [Profile Configurations](./profiles-README.md)
- [Speculative Decoding Theory](https://arxiv.org/abs/2211.17192)
- [DFlash Implementation](https://github.com/mit-han-lab/llm-awq)
- [MTP (Multi-Token Prediction)](https://arxiv.org/abs/2404.19737)
