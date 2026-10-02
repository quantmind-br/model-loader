#!/usr/bin/env python3
"""S18 summary: per arm, the diverse long prompts' TTFT and the stage-0 host wait for PLE rows."""
import json
from pathlib import Path
import re
import statistics

RUNS = Path(__file__).resolve().parents[1] / 'runs'
GPU = re.compile(r'prefill timing: (\d+) tokens, GPU timeline (\d+) ms, wall (\d+) ms')
HOST = re.compile(r'chunk setup \(PLE rows, the expert stream plan\) (\d+) ms, waiting for each chunk (\d+) ms, '
                  r'after each chunk \(the draft layer, progress\) (\d+) ms, PLE (\d+) ms')


def long_prompts(run):
    """(tokens, gpu ms, wall ms, setup ms, ple ms) of the stage-0 reports for prompts over 20k tokens."""
    lines = (run / 'engine.log').read_text(errors='replace').splitlines()
    out = []
    for i, line in enumerate(lines):
        g = GPU.search(line)
        if not g or int(g.group(1)) < 20000:
            continue
        h = HOST.search(lines[i + 1]) if i + 1 < len(lines) else None
        if h:
            out.append((int(g.group(1)), int(g.group(2)), int(g.group(3)), int(h.group(1)), int(h.group(4))))
    return out


def main():
    arms = {}
    for run in sorted(RUNS.glob('s18-io*-*')):
        n = run.name.split('-')[1][2:]
        res = json.loads((run / 'ple-results.json').read_text()) if (run / 'ple-results.json').exists() else []
        status = json.loads((run / 'run-status.json').read_text()) if (run / 'run-status.json').exists() else {}
        arms.setdefault(n, []).append({'run': run.name, 'guard': (run / 'guard.json').exists(), 'complete': status.get('requests_complete'),
                                       'ttft': [r['ttft_s'] for r in res], 'prompt_ms': [(r['timings'] or {}).get('prompt_ms') for r in res],
                                       'engine': long_prompts(run)})
    summary = {}
    for n, runs in sorted(arms.items(), key=lambda kv: int(kv[0])):
        ttft = [t for r in runs for t in r['ttft'] if t]
        eng = [e for r in runs for e in r['engine']]
        summary[n] = {'runs': [(r['run'], r['complete'], r['guard'], len(r['ttft'])) for r in runs],
                      'ttft_median_s': round(statistics.median(ttft), 2) if ttft else None,
                      'ttft_range_s': (round(min(ttft), 2), round(max(ttft), 2)) if ttft else None,
                      'ple_wait_ms_median': statistics.median(e[4] for e in eng) if eng else None,
                      'setup_ms_median': statistics.median(e[3] for e in eng) if eng else None,
                      'wall_minus_gpu_ms_median': statistics.median(e[2] - e[1] for e in eng) if eng else None,
                      'samples': len(ttft)}
        print(n, json.dumps(summary[n]))
    (RUNS.parent / 's18-summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
