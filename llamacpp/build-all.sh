#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NPROC="$(nproc)"

# CUDA 13.2 não suporta GCC 16 (host compiler). Usar GCC 14.
export CC=/usr/bin/gcc-14
export CXX=/usr/bin/g++-14
export CUDACXX=/opt/cuda/bin/nvcc

log()  { echo ""; echo "==> $1"; echo "-----------------------------------------------------------------"; }
ok()   { echo "    [OK] $1"; }
fail() { echo "    [FAIL] $1"; exit 1; }

# ── 1. Git pull em todos os repositórios ──────────────────────────────────
log "Git pull em todos os repositórios"

repos=(
    "$SCRIPT_DIR/llama.cpp"
    "$SCRIPT_DIR/buun-llama-cpp"
    "$SCRIPT_DIR/llama-cpp-turboquant"
)

for repo in "${repos[@]}"; do
    name="$(basename "$repo")"
    echo -n "  $name ... "
    if git -C "$repo" pull --ff-only --stat 2>&1; then
        ok "$name"
    else
        fail "$name"
    fi
done

# ── 2. Build: llama.cpp (original) ───────────────────────────────────────
log "Build: llama.cpp (original)"
cd "$SCRIPT_DIR/llama.cpp"
cmake -B build \
    -DGGML_CUDA=ON \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CUDA_HOST_COMPILER=/usr/bin/g++-14
cmake --build build -j"$NPROC"
ok "llama.cpp -> build/bin/llama-server"

# ── 3. Build: buun-llama-cpp (TCQ) ────────────────────────────────────────
log "Build: buun-llama-cpp (TCQ)"
cd "$SCRIPT_DIR/buun-llama-cpp"
cmake -B build \
    -DGGML_CUDA=ON \
    -DGGML_NATIVE=ON \
    -DGGML_CUDA_FA=ON \
    -DGGML_CUDA_FA_ALL_QUANTS=ON \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CUDA_HOST_COMPILER=/usr/bin/g++-14
cmake --build build -j"$NPROC"
ok "buun-llama-cpp -> build/bin/llama-server"

# ── 4. Build: llama-cpp-turboquant ────────────────────────────────────────
log "Build: llama-cpp-turboquant"
cd "$SCRIPT_DIR/llama-cpp-turboquant"
git checkout feature/turboquant-kv-cache
cmake -B build \
    -DGGML_CUDA=ON \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_CUDA_HOST_COMPILER=/usr/bin/g++-14
cmake --build build -j"$NPROC"
ok "llama-cpp-turboquant -> build/bin/llama-server"

# ── 5. Resumo ─────────────────────────────────────────────────────────────
log "Resumo dos binários"
for repo in "${repos[@]}"; do
    name="$(basename "$repo")"
    bin="$repo/build/bin/llama-server"
    if [ -f "$bin" ]; then
        size="$(du -h "$bin" | cut -f1)"
        echo "  $name/build/bin/llama-server  ($size)"
    else
        echo "  $name/build/bin/llama-server  NAO ENCONTRADO"
    fi
done

echo ""
echo "Build concluído com sucesso."
