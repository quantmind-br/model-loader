#!/bin/sh
# S18 A/B: STRATA_IO_THREADS 16 (default) / 32 / 8 on the IQ2 stage-dense 256k candidate (v6), diverse 58k prompts.
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
P=qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-candidate-stage-dense-256k
LOG=$S/plans/s18-ab.log
for arm in 16:a1 32:b1 8:c1 8:c2 32:b2 16:a2; do
  n=${arm%%:*}; tag=${arm##*:}; label=s18-io$n-$tag
  while pgrep -f 'tools/run_arm.py' > /dev/null; do sleep 10; done   # never overlap another attempt
  quiet=0
  while [ $quiet -lt 30 ]; do
    heavy=$(ps -eo rss,comm | awk '$2=="bun" && $1>500000' | wc -l)
    anon=$(awk '/^AnonPages/{print int($2/1048576)}' /proc/meminfo)
    if [ "$heavy" -eq 0 ] && [ "$anon" -lt 10 ]; then quiet=$((quiet+10)); else quiet=0; fi
    sleep 10
  done
  echo "$(date +%T) start $label" >> $LOG
  if [ "$n" = 16 ]; then envs="--env STRATA_PREFILL_TIMING=1"; else envs="--env STRATA_PREFILL_TIMING=1 --env STRATA_IO_THREADS=$n"; fi
  python3 $S/tools/run_arm.py $P $label $envs --probe --linger 150 > $S/plans/$label.out 2>&1 &
  sleep 3
  python3 $S/tools/ple_window.py $P $label > $S/plans/$label-window.out 2>&1
  wait
  echo "$(date +%T) done $label" >> $LOG
done
