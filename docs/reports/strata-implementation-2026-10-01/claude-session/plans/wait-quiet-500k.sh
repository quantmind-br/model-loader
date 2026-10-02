#!/bin/sh
# Launch the 500k validation only when no other heavy bun job runs and anon memory is moderate (max 60 min wait).
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
NEW=qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-500k
LOG=$S/plans/wait-quiet-500k.log
quiet=0; start=$(date +%s)
while :; do
  heavy=$(ps -eo rss,comm | awk '$2=="bun" && $1>500000' | wc -l)
  anon=$(awk '/^AnonPages/{print int($2/1048576)}' /proc/meminfo)
  if [ "$heavy" -eq 0 ] && [ "$anon" -lt 10 ]; then quiet=$((quiet+10)); else quiet=0; fi
  echo "$(date +%T) heavy_bun=$heavy anon_gib=$anon quiet_s=$quiet" >> $LOG
  [ $quiet -ge 60 ] && break
  [ $(( $(date +%s) - start )) -ge 3600 ] && { echo "gave up waiting" >> $LOG; exit 1; }
  sleep 10
done
echo "launching validate-3" >> $LOG
python3 $S/tools/run_arm.py $NEW baseline-500k-validate-3 --readonly --probe --fills --controls --linger 480 > $S/plans/baseline-500k-3.out 2>&1 &
sleep 4
python3 $S/tools/harness_window.py $NEW baseline-500k-validate-3 --after cached-0.9 > $S/plans/baseline-500k-harness-3.out 2>&1
wait
