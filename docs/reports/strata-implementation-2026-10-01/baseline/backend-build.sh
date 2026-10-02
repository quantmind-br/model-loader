#!/usr/bin/env bash
set -euo pipefail
backend_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cmake -S "$backend_root" -B "$backend_root/build-local-sm86" -DCMAKE_BUILD_TYPE=Release -DCMAKE_CUDA_ARCHITECTURES=86
cmake --build "$backend_root/build-local-sm86" --target strata -j "${STRATA_BUILD_JOBS:-8}"
install -m 755 "$backend_root/build-local-sm86/strata" "$backend_root/build-fork-sm86/strata"
