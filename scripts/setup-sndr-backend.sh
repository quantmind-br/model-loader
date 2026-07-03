#!/usr/bin/env bash
# setup-sndr-backend.sh — provision the SNDR (Genesis) vLLM backend variant.
#
# SNDR Core Engine (https://github.com/Sandermage/sndr_core_engine) is a
# runtime patch overlay for vLLM: ~321 patches applied in-memory at process
# startup via the vllm.general_plugins entry point (nothing rewritten on
# disk). It pins an exact vLLM nightly, so it gets its own checkout + venv
# under backends/sndr-vllm/.
#
# NEVER install the SNDR plugin into the vllm-stable/vllm-nightly venvs:
# the entry point auto-activates in EVERY vLLM process of the environment
# it is installed in, which would silently patch the stock profiles and
# invalidate any A/B comparison.
#
# Layout produced:
#   backends/sndr-vllm/.venv/             python3.12 venv, vLLM pinned (dev424)
#   backends/sndr-vllm/sndr_core_engine/  plugin checkout (editable install)
#   backends/sndr-vllm/sndr-serve.sh      catalog executable (kind: vllm)
#
# Overrides:
#   SNDR_VLLM_PIN    vLLM version to pin (default dev424; rollback pin: dev301
#                    per SNDR's <=2-pin policy)
#   SNDR_WHEEL_INDEX vLLM wheel index (default the rotating nightly channel).
#                    The rotating /nightly index keeps ONLY the latest build, so
#                    once SNDR's pinned dev build ages out you get "No matching
#                    distribution". vLLM also publishes PERSISTENT per-commit
#                    wheels at https://wheels.vllm.ai/<full-git-sha>/ — set this
#                    to that URL (full 40-char sha of the pin's +g<sha> suffix)
#                    to install an aged-out pin. E.g. for dev424+g3f5a1e173:
#                    SNDR_WHEEL_INDEX=https://wheels.vllm.ai/3f5a1e1733200760169ff31ebe60a271072b199e/
#   SNDR_REF         git ref of sndr_core_engine to check out (default main)
#   PYTHON_BIN       interpreter used to create the venv (default python3.12)
set -euo pipefail

VLLM_PIN="${SNDR_VLLM_PIN:-0.23.1rc1.dev424+g3f5a1e173}"
SNDR_WHEEL_INDEX="${SNDR_WHEEL_INDEX:-https://wheels.vllm.ai/nightly}"
SNDR_REPO="${SNDR_REPO:-https://github.com/Sandermage/sndr_core_engine.git}"
SNDR_REF="${SNDR_REF:-main}"
PYTHON_BIN="${PYTHON_BIN:-python3.12}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${1:-$ROOT/backends/sndr-vllm}"
VENV="$DEST/.venv"
PLUGIN_DIR="$DEST/sndr_core_engine"
WRAPPER="$DEST/sndr-serve.sh"

log() { printf '==> %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# --- prerequisites -----------------------------------------------------------
command -v "$PYTHON_BIN" >/dev/null 2>&1 ||
    die "$PYTHON_BIN not found — SNDR requires Python >= 3.12 (set PYTHON_BIN)"
"$PYTHON_BIN" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 12) else 1)' ||
    die "$PYTHON_BIN is older than 3.12"
command -v git >/dev/null 2>&1 || die "git not found"

# SNDR's pinned wheels are torch cu130 builds; the docs require driver
# >= 580.126.09. Warn only — the operator may be mid-upgrade.
if command -v nvidia-smi >/dev/null 2>&1; then
    drv="$(nvidia-smi --query-gpu=driver_version --format=csv,noheader | head -n1)"
    case "$drv" in
        58*|59*|6*) ;;
        *) warn "NVIDIA driver $drv < 580.126.09 — SNDR's cu130 wheels may not load" ;;
    esac
else
    warn "nvidia-smi not found — cannot verify driver >= 580.126.09"
fi

# --- venv + pinned vLLM ------------------------------------------------------
mkdir -p "$DEST"
if [[ ! -x "$VENV/bin/python" ]]; then
    log "creating venv at $VENV"
    "$PYTHON_BIN" -m venv "$VENV"
fi
PIP=("$VENV/bin/pip")
"${PIP[@]}" install -q --upgrade pip wheel setuptools

log "installing pinned vLLM $VLLM_PIN (index: $SNDR_WHEEL_INDEX)"
"${PIP[@]}" install --pre "vllm==$VLLM_PIN" \
    --extra-index-url "$SNDR_WHEEL_INDEX" ||
    die "vLLM install failed. If it was 'No matching distribution', the pin has
aged out of the rotating nightly index — set SNDR_WHEEL_INDEX to the persistent
per-commit wheel URL (https://wheels.vllm.ai/<full-sha>/; see header) and re-run."

got="$("$VENV/bin/python" -c 'import vllm; print(vllm.__version__)')"
[[ "$got" == "$VLLM_PIN" ]] ||
    die "vLLM version mismatch: wanted $VLLM_PIN, venv has $got"

# --- SNDR plugin (editable, per upstream install.sh) --------------------------
if [[ -d "$PLUGIN_DIR/.git" ]]; then
    log "updating sndr_core_engine checkout ($SNDR_REF)"
    git -C "$PLUGIN_DIR" fetch origin "$SNDR_REF"
    git -C "$PLUGIN_DIR" checkout -q FETCH_HEAD
else
    log "cloning sndr_core_engine ($SNDR_REF)"
    git clone --depth 1 --branch "$SNDR_REF" "$SNDR_REPO" "$PLUGIN_DIR"
fi
"${PIP[@]}" install -q --no-deps -e "$PLUGIN_DIR"
"${PIP[@]}" install -q pandas scipy xxhash   # SNDR runtime extras

# --- smoke test: patches must apply against the pinned vLLM -------------------
log "running SNDR patch smoke test (python -m sndr.apply)"
"$VENV/bin/python" -m sndr.apply ||
    die "sndr.apply failed — the checkout and the vLLM pin disagree; try SNDR_REF/SNDR_VLLM_PIN of a matching release"

# --- catalog executable --------------------------------------------------------
log "writing $WRAPPER"
cat > "$WRAPPER" <<'EOF'
#!/usr/bin/env bash
# sndr-serve.sh — catalog executable for the SNDR-patched vLLM (kind: vllm).
# The SNDR patches auto-apply via the vllm.general_plugins entry point of
# this venv; the wrapper only pins the venv, forces loopback, and normalizes
# the model argument: model-loader emits `--model <path>` (this wrapper's
# path carries no "serve" token, so buildVLLMArgs picks the flag shape),
# while `vllm serve` wants the model positional — accept both.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/.venv/bin/activate"

MODEL=""
ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --model)   MODEL="$2"; shift 2 ;;
        --model=*) MODEL="${1#--model=}"; shift ;;
        --host)    shift 2 ;;   # loopback is forced below
        --host=*)  shift ;;
        *)         ARGS+=("$1"); shift ;;
    esac
done

exec vllm serve ${MODEL:+"$MODEL"} --host 127.0.0.1 "${ARGS[@]}"
EOF
chmod +x "$WRAPPER"

log "done — SNDR backend provisioned at $DEST (vLLM $VLLM_PIN)"
cat <<EOF

Register it in the catalog (kind stays "vllm" — the process is a stock
"vllm serve", only runtime-patched):

  model-loader backend add sndr-vllm --executable "$WRAPPER" --kind vllm

Then point profiles at it via launch.backendId = "sndr-vllm".
See docs/sndr-backend.md for the full runbook.
EOF
