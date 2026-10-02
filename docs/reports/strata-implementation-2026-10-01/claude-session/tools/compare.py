#!/usr/bin/env python3
"""Compare arms (groups of attempts) from claude-summary.json: medians, ranges, fresh-fill TTFT, guards."""
import argparse
import json
from pathlib import Path
import statistics

RUNS = Path(__file__).resolve().parents[1] / 'runs'


def load(label):
    s = json.loads((RUNS / label / 'claude-summary.json').read_text())
    s['req'] = {x['label']: x for x in s['requests']}
    return s


def med(values):
    values = [v for v in values if v is not None]
    return (round(statistics.median(values), 3), round(min(values), 3), round(max(values), 3)) if values else None


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('arms', nargs='+', help='NAME=label1,label2,...')
    a = ap.parse_args()
    out = {}
    for arm in a.arms:
        name, labels = arm.split('=', 1)
        runs = [load(l) for l in labels.split(',')]
        complete = [r for r in runs if (r['run_status'] or {}).get('requests_complete')]
        fills = sorted({k for r in complete for k in r['req'] if k.startswith('fresh-')}, key=lambda k: float(k[6:]))
        out[name] = {
            'starts': len(runs), 'complete': len(complete),
            'guards': [(r['run'], (r['guard'] or {}).get('reasons')) for r in runs if r['guard']],
            'code_tps': med([r.get('code_tps') for r in complete]),
            'code_ms_per_window': med([r.get('code_ms_per_window') for r in complete]),
            'code_accept': med([r.get('code_accept') for r in complete]),
            'portuguese_tps': med([r['req']['portuguese']['tps'] for r in complete if 'portuguese' in r['req']]),
            'fresh_ttft_s': {f: med([r['req'][f]['ttft_s'] for r in complete if f in r['req']]) for f in fills},
            'fresh_prompt_n': {f: med([r['req'][f]['prompt_n'] for r in complete if f in r['req']]) for f in fills},
            'retrieval_exact': all(all(json.loads((RUNS / r['run'] / 'results.json').read_text())[i]['content'].strip()
                                       == 'LARANJA-7391' for i, x in enumerate(r['requests']) if x['label'].startswith(('fresh-', 'cached-')))
                                   for r in complete),
            'max_swap_growth_gib': med([r.get('max_swap_growth_gib') for r in runs]),
            'peak_gpu0_mib': med([r.get('peak_gpu_mib', {}).get('0') for r in runs]),
            'peak_gpu1_mib': med([r.get('peak_gpu_mib', {}).get('1') for r in runs]),
            'peak_temp_gpu0': med([r.get('peak_temperature_c', {}).get('0') for r in runs]),
            'min_available_gib': med([r.get('min_available_gib') for r in runs]),
            'expert_slots': med([(r.get('engine_info') or {}).get('expert_slots') for r in complete]),
        }
    print(json.dumps(out, indent=1))


if __name__ == '__main__':
    main()
