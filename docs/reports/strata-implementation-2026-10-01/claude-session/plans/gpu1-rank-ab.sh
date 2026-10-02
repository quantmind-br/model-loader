#!/bin/sh
# S13 on the IQ3 GPU1-resident placement: A = shared base expert profile, B = checkpoint rerank v1; A,B,B,A,A,B (v6).
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
A=qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-32k
B=qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-rerank-32k
LOG=$S/plans/gpu1-rank-ab.log
for arm in A:A1 B:B1 B:B2 A:A2 A:A3 B:B3; do
  which=${arm%%:*}; label=gpu1-rank-${arm##*:}
  if [ "$which" = A ]; then prof=$A; else prof=$B; fi
  while pgrep -f 'tools/run_arm.py' > /dev/null; do sleep 10; done
  quiet=0
  while [ $quiet -lt 30 ]; do
    heavy=$(ps -eo rss,comm | awk '$2=="bun" && $1>500000' | wc -l)
    anon=$(awk '/^AnonPages/{print int($2/1048576)}' /proc/meminfo)
    if [ "$heavy" -eq 0 ] && [ "$anon" -lt 10 ]; then quiet=$((quiet+10)); else quiet=0; fi
    sleep 10
  done
  echo "$(date +%T) start $label" >> $LOG
  python3 $S/tools/run_arm.py $prof $label --probe > $S/plans/$label.out 2>&1
  echo "$(date +%T) done $label rc=$?" >> $LOG
done
