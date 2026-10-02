#!/bin/sh
# Extend the continuation sample to 5 starts per arm (A,B,A,B,A); A-3 hit the load-time swap guard.
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
A=qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-32k
B=qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-rerank-32k
LOG=$S/plans/gpu1-rank-controls-ext.log
for arm in A:4 B:4 A:5 B:5 A:6; do
  which=${arm%%:*}; label=gpu1-rank-$which-controls-${arm##*:}
  if [ "$which" = A ]; then prof=$A; else prof=$B; fi
  while pgrep -f 'tools/run_arm.py' > /dev/null; do sleep 10; done
  quiet=0
  while [ $quiet -lt 60 ]; do
    heavy=$(ps -eo rss,comm | awk '$2=="bun" && $1>500000' | wc -l)
    anon=$(awk '/^AnonPages/{print int($2/1048576)}' /proc/meminfo)
    if [ "$heavy" -eq 0 ] && [ "$anon" -lt 10 ]; then quiet=$((quiet+10)); else quiet=0; fi
    sleep 10
  done
  echo "$(date +%T) start $label" >> $LOG
  python3 $S/tools/run_arm.py $prof $label --probe --controls > $S/plans/$label.out 2>&1
  echo "$(date +%T) done $label rc=$?" >> $LOG
done
echo "$(date +%T) all done" >> $LOG
