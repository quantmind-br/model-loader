#!/usr/bin/env python3
"""Run the bundled tool-call skill unchanged, accounting each urllib generation."""
import argparse
from pathlib import Path
import runpy
import sys
import urllib.request
from strata_probe_integrity import accounted_urlopen, validate_identity


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('skill_args', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    skill_args = args.skill_args
    if skill_args[:1] == ['--']:
        skill_args = skill_args[1:]
    if not validate_identity(args.out):
        return 2
    harness = Path(__file__).resolve().parents[1] / '.agents/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py'
    original = urllib.request.urlopen
    previous_argv = sys.argv
    urllib.request.urlopen = lambda request, *pos, **kw: accounted_urlopen(
        args.out, request, *pos, source='harness', **kw)
    sys.argv = [str(harness), *skill_args]
    code = 0
    try:
        try:
            runpy.run_path(str(harness), run_name='__main__')
        except SystemExit as exc:
            code = exc.code if isinstance(exc.code, int) else (0 if exc.code is None else 1)
    finally:
        sys.argv = previous_argv
        urllib.request.urlopen = original
        if not validate_identity(args.out):
            code = 2
    return code


if __name__ == '__main__':
    raise SystemExit(main())
