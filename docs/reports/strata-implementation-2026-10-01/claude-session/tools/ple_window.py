#!/usr/bin/env python3
"""S18: fresh, non-repetitive long prompts sent while a probe attempt keeps its instance loaded.

Each prompt is a distinct slice of a diverse corpus (repository source and documents), so neither prefix
reuse nor repeated n-grams hide the PLE reads.  Proxy only; the probe owns the lifecycle and the guards.
"""
import argparse
import json
from pathlib import Path
import time
import urllib.request

REPO = Path(__file__).resolve().parents[5]
RUNS = Path(__file__).resolve().parents[1] / 'runs'


def corpus():
    parts = []
    for pattern in ('internal/**/*.go', 'backends/strata-fork/src/**/*.cpp', 'backends/strata-fork/serve/*.py', 'docs/**/*.md'):
        for path in sorted(REPO.glob(pattern)):
            if '_test' in path.name or path.stat().st_size > 400_000:
                continue
            parts.append(f'\n### {path.relative_to(REPO)}\n' + path.read_text(errors='replace'))
    return ''.join(parts)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('profile')
    ap.add_argument('label')
    ap.add_argument('--chars', type=int, default=200_000, help='characters per prompt (~55-65k tokens)')
    ap.add_argument('--count', type=int, default=3)
    ap.add_argument('--after', default='json-text')
    a = ap.parse_args()
    out = RUNS / a.label
    while True:
        try:
            labels = [r['label'] for r in json.loads((out / 'results.json').read_text())]
        except (OSError, ValueError):
            labels = []
        if a.after in labels:
            break
        if (out / 'run-status.json').exists():
            raise SystemExit('attempt ended before the window')
        time.sleep(2)
    text = corpus()
    results = []
    for i in range(a.count):
        if (out / 'guard.json').exists():
            break
        piece = text[i * a.chars:(i + 1) * a.chars]
        body = {'model': a.profile, 'stream': True, 'max_tokens': 8, 'temperature': 0, 'reasoning_effort': 'none',
                'messages': [{'role': 'user', 'content': f'Documento {i}:\n{piece}\n\nResponda apenas: OK'}]}
        start = time.monotonic()
        first, last = None, {}
        req = urllib.request.Request('http://127.0.0.1:4321/v1/chat/completions', data=json.dumps(body).encode(),
                                     headers={'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=1800) as response:
            for raw in response:
                if not raw.startswith(b'data: ') or raw.strip() == b'data: [DONE]':
                    continue
                chunk = json.loads(raw[6:])
                delta = chunk.get('choices', [{}])[0].get('delta', {})
                if first is None and delta.get('content'):
                    first = time.monotonic()
                if 'usage' in chunk:
                    last = chunk
        results.append({'index': i, 'chars': len(piece), 'ttft_s': None if first is None else first - start,
                        'timings': last.get('timings'), 'usage': last.get('usage')})
        (out / 'ple-results.json').write_text(json.dumps(results, indent=2) + '\n')
        print(i, results[-1]['ttft_s'], (last.get('timings') or {}).get('prompt_n'), flush=True)


if __name__ == '__main__':
    main()
