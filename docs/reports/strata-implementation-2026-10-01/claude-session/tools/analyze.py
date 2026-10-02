#!/usr/bin/env python3
"""Summarize claude-session attempts: per-request decode, engine windows, guards.

Engine `strata decode timing` lines are matched to decoded requests in order.
ms/window and tokens/window separate engine cost from draft acceptance, which
differs between arms when sampling trajectories diverge.
"""
import argparse
import json
from pathlib import Path
import re
import statistics

TIMING = re.compile(r'strata decode timing: (\d+) windows, avg T ([\d.]+), ([\d.]+) tokens/window, ([\d.]+) ms/window'
                    r' = verify ([\d.]+) \(GPU-reach wait ([\d.]+) \+ per-layer host ([\d.]+).*?\+ draft ([\d.]+);'
                    r' per layer-window: CPU experts ([\d.]+) \(([\d.]+) entries\), VRAM hits ([\d.]+), PCIe ([\d.]+)')
HIT = re.compile(r'decode expert cache hit rate: ([\d.]+)%')


def engine_windows(path):
    rows = []
    if not path.exists():
        return rows
    text = path.read_text(errors='replace').splitlines()
    for i, line in enumerate(text):
        m = TIMING.search(line)
        if not m:
            continue
        v = [float(x) for x in m.groups()]
        row = dict(zip(['windows', 'avg_t', 'tokens_per_window', 'ms_per_window', 'verify_ms', 'gpu_wait_ms',
                        'host_ms', 'draft_ms', 'cpu_experts', 'cpu_entries', 'vram_hits', 'pcie'], v))
        for follow in text[i + 1:i + 4]:
            h = HIT.search(follow)
            if h:
                row['hit_rate'] = float(h.group(1))
        rows.append(row)
    return rows


def summarize(run):
    results = json.loads((run / 'results.json').read_text()) if (run / 'results.json').exists() else []
    samples = [json.loads(l) for l in (run / 'resources.jsonl').read_text().splitlines()] if (run / 'resources.jsonl').exists() else []
    windows = engine_windows(run / 'engine.log')
    decoded = [r for r in results if (r.get('timings') or {}).get('predicted_n', 0) > 1]
    rows = []
    for r in results:
        t = r.get('timings') or {}
        row = {'label': r['label'], 'prompt_n': t.get('prompt_n'), 'cache_n': t.get('cache_n'),
               'predicted_n': t.get('predicted_n'), 'tps': t.get('predicted_per_second'), 'ttft_s': r.get('ttft_s'),
               'prompt_ms': t.get('prompt_ms'), 'draft_n': t.get('draft_n'), 'accepted': t.get('draft_n_accepted'),
               'finish': r.get('finish')}
        if t.get('draft_n'):
            row['accept'] = round(t['draft_n_accepted'] / t['draft_n'], 3)
        rows.append(row)
    if len(windows) == len(decoded):
        for r, w in zip(decoded, windows):
            next(x for x in rows if x['label'] == r['label']).update({'engine': w})
    code = [x for x in rows if x['label'].startswith('code-')]
    out = {'run': run.name, 'attempt': json.loads((run / 'attempt.json').read_text()) if (run / 'attempt.json').exists() else None,
           'run_status': json.loads((run / 'run-status.json').read_text()) if (run / 'run-status.json').exists() else None,
           'guard': json.loads((run / 'guard.json').read_text()) if (run / 'guard.json').exists() else None,
           'windows_matched': len(windows) == len(decoded), 'requests': rows}
    if code:
        out['code_tps'] = statistics.median(x['tps'] for x in code)
        if all('engine' in x for x in code):
            out['code_ms_per_window'] = statistics.median(x['engine']['ms_per_window'] for x in code)
            out['code_tokens_per_window'] = statistics.median(x['engine']['tokens_per_window'] for x in code)
        out['code_accept'] = statistics.median(x.get('accept', 0) for x in code)
    if samples:
        out['peak_gpu_mib'] = {}
        for s in samples:
            for g in s['gpus']:
                out['peak_gpu_mib'][g['index']] = max(out['peak_gpu_mib'].get(g['index'], 0), float(g['memory_mib']))
        out['max_swap_growth_gib'] = round(max(0, max(s['swap_growth_kib'] for s in samples)) / 1024 ** 2, 3)
        out['min_available_gib'] = round(min(s['memory_kib']['MemAvailable'] for s in samples) / 1024 ** 2, 2)
        out['peak_temperature_c'] = {i: max(float(g['temperature_c']) for s in samples for g in s['gpus'] if g['index'] == i)
                                     for i in out['peak_gpu_mib']}
        out['max_guard_strikes'] = max(s.get('guard_strikes', 0) for s in samples)
    info = next(((r.get('timings') or {}).get('engine_info') for r in results if (r.get('timings') or {}).get('engine_info')), None)
    out['engine_info'] = info
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('runs', nargs='+', type=Path)
    a = ap.parse_args()
    for run in a.runs:
        s = summarize(run)
        (run / 'claude-summary.json').write_text(json.dumps(s, indent=2, ensure_ascii=False) + '\n')
        print(f"{s['run']}: rc={(s['attempt'] or {}).get('returncode')} guard={(s['guard'] or {}).get('reasons')} "
              f"code_tps={s.get('code_tps')} ms/win={s.get('code_ms_per_window')} tok/win={s.get('code_tokens_per_window')} "
              f"accept={s.get('code_accept')} peak={s.get('peak_gpu_mib')} swap+={s.get('max_swap_growth_gib')} "
              f"T={s.get('peak_temperature_c')} matched={s['windows_matched']}")
        for x in s['requests']:
            e = x.get('engine') or {}
            print(f"   {x['label']:<26} p={x['prompt_n']} c={x['cache_n']} n={x['predicted_n']} tps={x['tps']} "
                  f"ttft={x['ttft_s'] and round(x['ttft_s'], 3)} acc={x.get('accept')} ms/w={e.get('ms_per_window')} "
                  f"tok/w={e.get('tokens_per_window')} hit={e.get('hit_rate')} fin={x['finish']}")


if __name__ == '__main__':
    main()
