#!/bin/sh
exec /usr/bin/nsys profile --sample=none --cpuctxsw=none --trace=cuda,nvtx --cuda-graph-trace=node --delay=20 --duration=90 --kill=none --force-overwrite=true --output=/home/diogo/dev/model-loader/docs/reports/strata-implementation-2026-10-01/iq3-nsys /home/diogo/dev/model-loader/backends/strata-fork/releases/20261001T201850Z-8ae9e4e25528/strata "$@"
