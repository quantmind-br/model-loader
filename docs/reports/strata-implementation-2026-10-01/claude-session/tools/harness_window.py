#!/usr/bin/env python3
"""Run the bundled strict tool harness while a probe attempt keeps its instance loaded.

Waits for the attempt's controls to finish (its last control label), then runs
integrity (with stream + continuation) and auto --promotion against the proxy,
saving raw stdout/stderr next to the attempt. It never starts or stops models:
the probe (model-loader instance start, guards, sampler) owns the lifecycle.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import time

REPO = Path(__file__).resolve().parents[5]
RUNS = Path(__file__).resolve().parents[1] / 'runs'
HARNESS = REPO / '.agents/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py'


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('profile')
    ap.add_argument('label')
    ap.add_argument('--after', default='conversation-a2', help='probe label that ends the controls')
    ap.add_argument('--timeout', type=int, default=3600)
    a = ap.parse_args()
    out = RUNS / a.label
    deadline = time.monotonic() + a.timeout
    while True:
        try:
            labels = [r['label'] for r in json.loads((out / 'results.json').read_text())]
        except (OSError, ValueError):
            labels = []
        if a.after in labels:
            break
        if (out / 'run-status.json').exists() or time.monotonic() > deadline:
            raise SystemExit('the attempt ended before its controls finished; harness not run')
        time.sleep(2)
    for mode, flags in [('integrity', ['--stream', '--continuation']), ('auto', ['--promotion'])]:
        if (out / 'run-status.json').exists() or (out / 'guard.json').exists():
            raise SystemExit('the attempt ended or hit a guard; harness stopped')
        started = time.time()
        done = subprocess.run([sys.executable, str(HARNESS), '--model', a.profile, '--mode', mode, *flags],
                              capture_output=True, text=True, timeout=1800)
        (out / f'promotion-harness-{mode}.json').write_text(json.dumps(
            {'returncode': done.returncode, 'started': started, 'finished': time.time(),
             'stdout': done.stdout, 'stderr': done.stderr}, indent=2) + '\n')
        print(mode, done.returncode, flush=True)


if __name__ == '__main__':
    main()
