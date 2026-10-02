#!/bin/sh
# canonical C tools window, after candidate B finished (serial GPU use)
cd /home/diogo/dev/model-loader
S=docs/reports/strata-implementation-2026-10-01/claude-session
until grep -q '"returncode"' $S/runs/v5-iq2-sd256-tools-B/attempt.json 2>/dev/null; do sleep 10; done
python3 $S/tools/run_arm.py qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k v5-iq2-256k-tools-C --readonly --probe --controls --linger 480 > $S/plans/v5-tools-C.out 2>&1 &
sleep 4
python3 $S/tools/harness_window.py qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k v5-iq2-256k-tools-C > $S/plans/v5-harness-C.out 2>&1
wait
