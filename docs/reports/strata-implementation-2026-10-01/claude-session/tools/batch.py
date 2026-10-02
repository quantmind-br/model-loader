#!/usr/bin/env python3
"""Run run_arm.py attempts strictly one after another; one JSON line per attempt in batch.log."""
import json
from pathlib import Path
import shlex
import subprocess
import sys
import time

TOOLS = Path(__file__).resolve().parent


def main():
    plan = Path(sys.argv[1])
    log = TOOLS.parent / 'batch.log'
    for line in plan.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        args = shlex.split(line)
        start = time.time()
        with open(log, 'a') as out:
            out.write(json.dumps({'start': start, 'plan': plan.name, 'args': args}) + '\n')
        rc = subprocess.run([sys.executable, str(TOOLS / 'run_arm.py'), *args]).returncode
        with open(log, 'a') as out:
            out.write(json.dumps({'done': time.time(), 'label': args[1], 'returncode': rc, 'seconds': round(time.time() - start)}) + '\n')


if __name__ == '__main__':
    main()
