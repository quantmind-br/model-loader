#!/bin/sh
# Run approved v6/v7 arms serially; keep native resource guards and passive cooling.
set -eu
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
RELEASE=${1:?Usage: sh v7-ab.sh RELEASE_ID}
BASE=qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15

wait_quiet() {
  python3 - <<'PY'
from pathlib import Path
import time
start = time.monotonic()
quiet_since = None
while True:
    heavy = []
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit():
            continue
        try:
            if (proc / 'comm').read_text().strip() != 'bun':
                continue
            status = dict(line.split(':', 1) for line in (proc / 'status').read_text().splitlines() if ':' in line)
            if int(status.get('VmRSS', '0').split()[0]) > 500000:
                heavy.append(proc.name)
        except (OSError, ValueError, ProcessLookupError):
            continue
    fields = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines() if ':' in line)
    anon = int(fields['AnonPages'].split()[0]) / 1024**2
    now = time.monotonic()
    quiet_since = (quiet_since if quiet_since is not None else now) if not heavy and anon < 10 else None
    print(f'quiet preflight: heavy_bun={heavy} anon_gib={anon:.2f} quiet_s={0 if quiet_since is None else now - quiet_since:.0f}', flush=True)
    if quiet_since is not None and now - quiet_since >= 60:
        break
    if now - start >= 3600:
        raise SystemExit('gave up waiting for quiet host after 60 minutes')
    time.sleep(10)
PY
}

run_arm() {
  size=$1
  arm=$2
  label=v7ab-${size}-${arm}
  case "$arm" in
    A*) profile=${BASE}-${size}; set -- --readonly ;;
    B*) profile=${BASE}-candidate-${size}; set -- --release "$RELEASE" --baseline v7 ;;
  esac
  [ "$size" != 500k ] || wait_quiet
  python3 "$S/tools/run_arm.py" "$profile" "$label" "$@" --probe --fills --controls --linger 480 --wait-harness > "$S/plans/$label.out" 2>&1 &
  probe_pid=$!
  sleep 4
  harness_rc=0
  python3 "$S/tools/harness_window.py" "$profile" "$label" --after cached-0.9 > "$S/plans/$label-harness.out" 2>&1 || harness_rc=$?
  probe_rc=0
  wait "$probe_pid" || probe_rc=$?
  printf '%s probe_rc=%s harness_rc=%s\n' "$label" "$probe_rc" "$harness_rc"
  [ ! -f "$S/runs/$label/lifecycle-invalid.json" ] || return 1
  [ "$probe_rc" -eq 0 ]
}

for size in 256k 500k; do
  for arm in A1 B1 B2 A2 A3 B3; do
    run_arm "$size" "$arm"
  done
done

for number in 1 2 3; do
  label=v7ab-256k-M${number}
  python3 "$S/tools/run_arm.py" "${BASE}-candidate-256k" "$label" --release "$RELEASE" --baseline v7 --env STRATA_GR_DOWN_MAX4=1 --probe > "$S/plans/$label.out" 2>&1
  printf '%s finished\n' "$label"
done

python3 "$S/tools/analyze.py" "$S"/runs/v7ab-*
for size in 256k 500k; do
  set -- "V6=v7ab-${size}-A1,v7ab-${size}-A2,v7ab-${size}-A3" "V7=v7ab-${size}-B1,v7ab-${size}-B2,v7ab-${size}-B3"
  [ "$size" != 256k ] || set -- "$@" MAX4=v7ab-256k-M1,v7ab-256k-M2,v7ab-256k-M3
  python3 "$S/tools/compare.py" "$@" > "$S/plans/v7ab-${size}-compare.json"
done
