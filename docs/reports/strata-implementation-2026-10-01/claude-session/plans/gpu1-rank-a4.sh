#!/bin/sh
# Extra A start replacing A1 (cache-transition load failure); same guards and no overlap.
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
A=qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-32k
LOG=$S/plans/gpu1-rank-ab.log
until grep -q 'done gpu1-rank-B3' $LOG; do sleep 10; done
while pgrep -f 'tools/run_arm.py' > /dev/null; do sleep 10; done
quiet=0
while [ $quiet -lt 30 ]; do
  heavy=$(ps -eo rss,comm | awk '$2=="bun" && $1>500000' | wc -l)
  anon=$(awk '/^AnonPages/{print int($2/1048576)}' /proc/meminfo)
  if [ "$heavy" -eq 0 ] && [ "$anon" -lt 10 ]; then quiet=$((quiet+10)); else quiet=0; fi
  sleep 10
done
echo "$(date +%T) start gpu1-rank-A4" >> $LOG
python3 $S/tools/run_arm.py $A gpu1-rank-A4 --probe > $S/plans/gpu1-rank-A4.out 2>&1
echo "$(date +%T) done gpu1-rank-A4 rc=$?" >> $LOG
