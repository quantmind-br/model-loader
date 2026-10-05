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
sys.path.insert(0, str(REPO / 'scripts'))
from strata_probe_integrity import validate_identity

RUNNER = REPO / 'scripts/strata_harness_runner.py'


def write_json(path, value):
    tmp = path.with_suffix(path.suffix + '.tmp')
    tmp.write_text(json.dumps(value, indent=2) + '\n')
    tmp.replace(path)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('profile')
    ap.add_argument('label')
    ap.add_argument('--after', default='conversation-a2', help='probe label that ends the controls')
    ap.add_argument('--timeout', type=int, default=3600)
    a = ap.parse_args()
    out = RUNS / a.label
    deadline = time.monotonic() + a.timeout
    rc = 0
    error = None
    try:
        while True:
            try:
                labels = [r['label'] for r in json.loads((out / 'results.json').read_text())]
            except (OSError, ValueError):
                labels = []
            if (a.after in labels and (out / 'lifecycle-expected.json').exists()
                    and (out / 'probe-requests-done.json').exists()):
                break
            if (out / 'run-status.json').exists() or time.monotonic() > deadline:
                raise RuntimeError('the attempt ended or timed out before launch and controls finished; harness not run')
            time.sleep(2)
        for mode, flags in [('integrity', ['--stream', '--continuation']), ('auto', ['--promotion'])]:
            if (out / 'run-status.json').exists() or (out / 'guard.json').exists():
                raise RuntimeError('the attempt ended or hit a guard; harness stopped')
            if not validate_identity(out):
                raise RuntimeError('lifecycle identity or request accounting invalid before harness mode')
            started = time.time()
            try:
                done = subprocess.run([sys.executable, str(RUNNER), '--out', str(out), '--',
                                       '--model', a.profile, '--mode', mode, *flags],
                                      capture_output=True, text=True, timeout=1800)
            finally:
                valid = validate_identity(out)
            write_json(out / f'promotion-harness-{mode}.json',
                       {'returncode': done.returncode, 'started': started, 'finished': time.time(),
                        'lifecycle_valid': valid, 'stdout': done.stdout, 'stderr': done.stderr})
            print(mode, done.returncode, flush=True)
            if done.returncode != 0:
                rc = done.returncode
                error = f'{mode} harness failed with returncode {done.returncode}'
            if not valid:
                raise RuntimeError('lifecycle identity or request accounting invalid after harness mode')
    except Exception as exc:
        rc = rc or 1
        error = str(exc)
        print(error, file=sys.stderr, flush=True)
    finally:
        out.mkdir(parents=True, exist_ok=True)
        write_json(out / 'harness-done.json',
                   {'returncode': rc, 'success': rc == 0, 'error': error, 'finished': time.time()})
    return rc


if __name__ == '__main__':
    sys.exit(main())
