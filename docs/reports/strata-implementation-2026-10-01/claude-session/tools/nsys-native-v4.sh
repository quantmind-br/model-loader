#!/bin/sh
# Prepared Nsight capture (same flags as ../../nsys-native.sh) around the v4 engine; STRATA_NSYS_OUT names the report.
exec /usr/bin/nsys profile --sample=none --cpuctxsw=none --trace=cuda,nvtx --cuda-graph-trace=node --delay=20 --duration=90 --kill=none --force-overwrite=true --output="${STRATA_NSYS_OUT:?}" /home/diogo/dev/model-loader/backends/strata-fork/releases/20261001T223017Z-f85ddc896414/strata "$@"
