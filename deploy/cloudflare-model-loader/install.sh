#!/usr/bin/env bash
# Deterministic provisioning installer for the model-loader external API.
# Consumes the Caddyfile, cloudflared and systemd unit templates that sit
# beside this script; produces a running gateway + Cloudflare Tunnel.
# The API key is only ever read from the caller environment and written to
# the mode-0600 gateway.env; it never appears in argv, logs, or output.
set -euo pipefail

readonly HOSTNAME='model-loader.quantforge.com.br'
readonly TUNNEL_NAME='model-loader-quantforge'
# Port constants mirror the values baked into the checked-in Caddyfile,
# cloudflared, and systemd templates provisioned by this installer.
# shellcheck disable=SC2034
readonly GATEWAY_PORT='4322'
# shellcheck disable=SC2034
readonly METRICS_PORT='49321'
SCRIPT_SOURCE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SOURCE_DIR="$SCRIPT_SOURCE_DIR"
readonly CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/model-loader/external-api"
readonly UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
readonly CLOUDFLARED_DIR="$HOME/.cloudflared"
readonly CERT_PATH="$CLOUDFLARED_DIR/cert.pem"

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

for command in caddy cloudflared curl jq python3 systemctl systemd-analyze ss install sed stat; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

# cloudflared binds most management flags to inherited TUNNEL_* environment
# variables: TUNNEL_ORIGIN_CERT (account selection), TUNNEL_CRED_FILE (where
# `tunnel create` writes the secret), TUNNEL_CREATE_SECRET (the generated
# tunnel secret), TUNNEL_FORCE_PROVISIONING_DNS (silent DNS overwrite) and
# others. Scrub the whole prefix plus NO_AUTOUPDATE so provisioning is driven
# only by the explicit pinned arguments below, never by caller state.
while IFS= read -r control_var; do
  unset -v "$control_var"
done < <(compgen -e | sed -n '/^TUNNEL_/p')
unset -v NO_AUTOUPDATE

# Every cloudflared management invocation runs through this pinned wrapper:
# no self-update, and the intended origin certificate inside CLOUDFLARED_DIR
# (the same path the login branch below checks and generates).
cf() {
  cloudflared --no-autoupdate --origincert "$CERT_PATH" "$@"
}

[[ -n "${MODELLOADER_API_KEY:-}" ]] || fail 'MODELLOADER_API_KEY is empty or unset'
[[ "$MODELLOADER_API_KEY" != *$'\n'* ]] || fail 'MODELLOADER_API_KEY must not contain a newline'
[[ "$MODELLOADER_API_KEY" != *$'\r'* ]] || fail 'MODELLOADER_API_KEY must not contain a carriage return'
# Boring character contract: the key must round-trip byte-identically through
# gateway.env parsed by Caddy's envfile loader and by systemd's
# EnvironmentFile, and through the Caddyfile {$MODELLOADER_API_KEY} expansion.
# Quotes, backslash, '#', '$', '%' or whitespace make at least one parser
# diverge (a trailing backslash breaks the quoted token; '#' truncates the
# Caddy-side value), so unsupported characters are rejected explicitly.
[[ "$MODELLOADER_API_KEY" =~ ^[A-Za-z0-9._~+/:,@=-]+$ ]] ||
  fail 'MODELLOADER_API_KEY must contain only letters, digits, or any of . _ ~ + / : , @ = -'
# Bash variables cannot contain NUL bytes, so the NUL constraint holds
# inherently. The key value itself is never printed.

# Reject any value destined for a sed replacement: '|' collides with the
# delimiter, '&' expands to the matched text, and a backslash is an escape
# character in GNU sed replacement text; newlines/CRs would split output.
require_safe_sed_value() {
  local label="$1" value="$2"
  [[ -n "$value" ]] || fail "$label is empty"
  [[ "$value" != *'|'* ]] || fail "$label must not contain '|'"
  [[ "$value" != *'&'* ]] || fail "$label must not contain '&'"
  [[ "$value" != *$'\\'* ]] || fail "$label must not contain a backslash"
  [[ "$value" != *$'\n'* ]] || fail "$label must not contain a newline"
  [[ "$value" != *$'\r'* ]] || fail "$label must not contain a carriage return"
}

# Values rendered into systemd unit files must additionally be absolute and
# free of characters systemd parses specially: whitespace would split
# ExecStart arguments into several words, quotes would alter parsing, '%'
# triggers specifier expansion, and '$'/backticks are expanded on ExecStart
# command lines. Unsupported paths are rejected explicitly, before any
# filesystem mutation.
require_safe_unit_path() {
  local label="$1" value="$2"
  require_safe_sed_value "$label" "$value"
  [[ "$value" == /* ]] || fail "$label must be an absolute path"
  [[ "$value" != *[[:space:]]* ]] || fail "$label must not contain whitespace"
  [[ "$value" != *'%'* ]] || fail "$label must not contain '%'"
  [[ "$value" != *'$'* ]] || fail "$label must not contain '\$'"
  [[ "$value" != *'`'* ]] || fail "$label must not contain a backtick"
  [[ "$value" != *'"'* ]] || fail "$label must not contain a double quote"
  [[ "$value" != *"'"* ]] || fail "$label must not contain a single quote"
}

for path_var in CONFIG_DIR UNIT_DIR CLOUDFLARED_DIR; do
  require_safe_unit_path "${path_var} (derived from XDG_CONFIG_HOME/HOME)" "${!path_var}"
done

# ---------------------------------------------------------------------------
# Secret-safe local file rendering
# ---------------------------------------------------------------------------
umask 077
install -d -m 0700 "$CONFIG_DIR" "$UNIT_DIR" "$CLOUDFLARED_DIR"
install -m 0600 "$SOURCE_DIR/Caddyfile" "$CONFIG_DIR/Caddyfile"

MODELLOADER_API_KEY="$MODELLOADER_API_KEY" python3 - "$CONFIG_DIR/gateway.env" <<'PY'
import os
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
value = os.environ["MODELLOADER_API_KEY"]
# Same contract as the shell check above (defense in depth). Inside the
# character set there are no quotes, backslashes, '$', '%' or '#', so the
# double-quoted line is parsed identically by Caddy's envfile loader,
# systemd's EnvironmentFile, and the Caddyfile {$...} expansion.
if not re.fullmatch(r"[A-Za-z0-9._~+/:,@=-]+", value):
    raise SystemExit("MODELLOADER_API_KEY contains unsupported characters")
path.write_text(f'MODELLOADER_API_KEY="{value}"\n', encoding="utf-8")
path.chmod(0o600)
PY

unset MODELLOADER_API_KEY

# ---------------------------------------------------------------------------
# Interactive Tunnel creation or exact-name reuse
# ---------------------------------------------------------------------------
if [[ ! -f "$CERT_PATH" ]]; then
  printf 'Cloudflare authorization is required for %s.\n' "$HOSTNAME"
  cf tunnel login
fi

existing_json="$(cf tunnel list --name "$TUNNEL_NAME" --output json)"
existing_count="$(jq 'length' <<<"$existing_json")"
case "$existing_count" in
  0)
    created_json="$(cf tunnel create --output json "$TUNNEL_NAME")"
    tunnel_id="$(jq -er '.id // .ID' <<<"$created_json")"
    ;;
  1)
    tunnel_id="$(jq -er '.[0].id // .[0].ID' <<<"$existing_json")"
    ;;
  *)
    fail "multiple Cloudflare Tunnels named $TUNNEL_NAME"
    ;;
esac

# Cloudflare Tunnel ids are lowercase UUIDs; anything else cannot reach the
# YAML/unit renderers below.
[[ "$tunnel_id" =~ ^[0-9a-fA-F-]{1,64}$ ]] || fail 'unexpected Cloudflare Tunnel id format'

credentials_file="$CLOUDFLARED_DIR/$tunnel_id.json"
[[ -f "$credentials_file" ]] || fail "Tunnel credential file not found: $credentials_file"
chmod 0600 "$credentials_file"
require_safe_unit_path 'credentials file path' "$credentials_file"

# ---------------------------------------------------------------------------
# Render and validate cloudflared configuration
# ---------------------------------------------------------------------------
sed \
  -e "s|__TUNNEL_ID__|$tunnel_id|g" \
  -e "s|__CREDENTIALS_FILE__|$credentials_file|g" \
  "$SOURCE_DIR/cloudflared.yml.tmpl" >"$CONFIG_DIR/cloudflared.yml"
chmod 0600 "$CONFIG_DIR/cloudflared.yml"
cf tunnel --config "$CONFIG_DIR/cloudflared.yml" ingress validate

# ---------------------------------------------------------------------------
# DNS route without silent overwrite
# ---------------------------------------------------------------------------
# --overwrite-dns=false is passed explicitly and TUNNEL_FORCE_PROVISIONING_DNS
# was scrubbed above, so neither caller state nor defaults can replace an
# existing record: a conflict stays a hard failure. The marker is written only
# after Cloudflare accepts the route.
readonly DNS_MARKER="$CONFIG_DIR/dns-route"
expected_route="$tunnel_id $HOSTNAME"

if [[ -f "$DNS_MARKER" && "$(<"$DNS_MARKER")" == "$expected_route" ]]; then
  printf 'DNS route already provisioned for %s.\n' "$HOSTNAME"
else
  cf tunnel route dns --overwrite-dns=false "$tunnel_id" "$HOSTNAME" \
    || fail "DNS route creation failed; inspect the existing $HOSTNAME record before retrying"
  printf '%s\n' "$expected_route" >"$DNS_MARKER"
  chmod 0600 "$DNS_MARKER"
fi

# ---------------------------------------------------------------------------
# Pre-migration guards: systemd capability, proxy port ownership
# ---------------------------------------------------------------------------
# Type=notify, WatchdogSec, BindsTo/PartOf and [Unit] StartLimit*= need a
# reasonably modern user systemd; refuse to half-provision on an old one.
systemd_version="$(systemctl --version | head -n 1 | grep -oE '[0-9]+' | head -n 1)"
[[ -n "$systemd_version" && "$systemd_version" -ge 240 ]] \
  || fail "user systemd is too old (found ${systemd_version:-unknown}, need >= 240) for Type=notify/watchdog lifecycle units"

# Resolve the model-loader binary once and pin its absolute path into the
# proxy unit. Never rely on PATH inside the unit environment.
# Idempotency: an existing identical proxy unit is left untouched so a
# rerun never restarts a healthy proxy.
if [[ "${MODEL_LOADER_BIN:-}" == "" ]]; then
  model_loader_bin="$(command -v model-loader)"
else
  model_loader_bin="$MODEL_LOADER_BIN"
fi
[[ -n "$model_loader_bin" ]] || fail 'model-loader not found in PATH; install it (make install) before provisioning (or export MODEL_LOADER_BIN)'
[[ -x "$model_loader_bin" ]] || fail "model-loader is not executable: $model_loader_bin"
require_safe_unit_path 'model-loader binary path' "$model_loader_bin"

# If 127.0.0.1:4321 is already listening and the new proxy unit does not own
# it, the legacy detached proxy is still running. Fail BEFORE mutating any
# unit so the operator can stop it first — installing the units underneath a
# live legacy proxy would split-brain the port.
if ss -ltnH 'src = 127.0.0.1:4321' | grep -q .; then
  if systemctl --user is-active --quiet model-loader-proxy.service 2>/dev/null; then
    printf 'Proxy port 127.0.0.1:4321 is held by the installed proxy unit; continuing.\n'
  else
    fail '127.0.0.1:4321 is already listening but model-loader-proxy.service is not active — stop the legacy proxy first (TUI Server tab Stop, or model-loader instance stop), then rerun this installer'
  fi
fi

# ---------------------------------------------------------------------------
# Private Caddy copy: a stable path the gateway unit can pin, immune to
# later PATH changes. No network download — the operator provides caddy.
# ---------------------------------------------------------------------------
system_caddy="$(command -v caddy)"
require_safe_unit_path 'caddy binary path' "$system_caddy"
"$system_caddy" version >/dev/null 2>&1 || fail 'caddy binary failed to run (caddy version)'
readonly PRIVATE_BIN_DIR="$HOME/.local/lib/model-loader/bin"
require_safe_unit_path 'private binary dir' "$PRIVATE_BIN_DIR"
install -d -m 0755 "$PRIVATE_BIN_DIR"
# Atomic copy: temp file + rename, so a concurrent gateway reload never
# executes a half-written binary.
tmp_caddy="$PRIVATE_BIN_DIR/.caddy.tmp.$$"
trap 'rm -f "$tmp_caddy"' EXIT
install -m 0755 "$system_caddy" "$tmp_caddy"
mv -f "$tmp_caddy" "$PRIVATE_BIN_DIR/caddy"
trap - EXIT
readonly CADDY_BIN="$PRIVATE_BIN_DIR/caddy"
"$CADDY_BIN" version >/dev/null 2>&1 || fail 'private caddy copy failed to run'

# ---------------------------------------------------------------------------
# Render user-systemd units using absolute paths (staged, then validated)
# ---------------------------------------------------------------------------
cloudflared_bin="$(command -v cloudflared)"
require_safe_unit_path 'cloudflared binary path' "$cloudflared_bin"

stage_dir="$(mktemp -d)"
trap 'rm -rf "$stage_dir"' EXIT
chmod 0700 "$stage_dir"

sed \
  -e "s|__CONFIG_DIR__|$CONFIG_DIR|g" \
  -e "s|__CADDY_BIN__|$CADDY_BIN|g" \
  "$SOURCE_DIR/model-loader-api-gateway.service.tmpl" \
  >"$stage_dir/model-loader-api-gateway.service"

sed \
  -e "s|__CONFIG_DIR__|$CONFIG_DIR|g" \
  -e "s|__CLOUDFLARED_BIN__|$cloudflared_bin|g" \
  -e "s|__TUNNEL_ID__|$tunnel_id|g" \
  "$SOURCE_DIR/model-loader-cloudflared.service.tmpl" \
  >"$stage_dir/model-loader-cloudflared.service"

sed \
  -e "s|__MODEL_LOADER_BIN__|$model_loader_bin|g" \
  "$SOURCE_DIR/model-loader-proxy.service.tmpl" \
  >"$stage_dir/model-loader-proxy.service"

# No placeholder may survive rendering; a leaked __TOKEN__ would fail
# loudly here instead of producing a broken unit.
if grep -rE '__[A-Z_]+__' "$stage_dir" 2>/dev/null; then
  fail 'unreplaced placeholder in rendered units'
fi

# Validate everything before touching the live unit dir.
systemd-analyze verify \
  "$stage_dir/model-loader-proxy.service" \
  "$stage_dir/model-loader-api-gateway.service" \
  "$stage_dir/model-loader-cloudflared.service" \
  || fail 'rendered units failed systemd-analyze verify'

# ---------------------------------------------------------------------------
# Install validated units; retire the 1s polling watcher
# ---------------------------------------------------------------------------
# Idempotency: byte-identical units are not rewritten, so a rerun neither
# restarts healthy services nor churns mtimes (existing secrets, Tunnel
# credentials, and DNS markers are never touched by this script).
units_changed=0
for unit in model-loader-proxy.service model-loader-api-gateway.service model-loader-cloudflared.service; do
  if [[ -f "$UNIT_DIR/$unit" ]] && cmp -s "$stage_dir/$unit" "$UNIT_DIR/$unit"; then
    printf 'Unit %s unchanged; leaving it alone.\n' "$unit"
  else
    install -m 0600 "$stage_dir/$unit" "$UNIT_DIR/$unit"
    units_changed=1
  fi
done
install -m 0600 "$stage_dir/model-loader-api-gateway.service" "$UNIT_DIR/model-loader-api-gateway.service"
install -m 0600 "$stage_dir/model-loader-cloudflared.service" "$UNIT_DIR/model-loader-cloudflared.service"
trap - EXIT
rm -rf "$stage_dir"

# ---------------------------------------------------------------------------
# Validate configs, retire polling, start the proxy unit
# ---------------------------------------------------------------------------
"$CADDY_BIN" validate \
  --config "$CONFIG_DIR/Caddyfile" \
  --adapter caddyfile \
  --envfile "$CONFIG_DIR/gateway.env"

if [[ "$units_changed" == 1 ]]; then
  systemctl --user daemon-reload
fi

# The 1s TCP-polling watcher is superseded by BindsTo lifecycle units.
# Remove it only now that the new topology validated and installed.
systemctl --user disable --now model-loader-api-gateway-watch.timer >/dev/null 2>&1 || true
systemctl --user disable --now model-loader-api-gateway-watch.service >/dev/null 2>&1 || true
rm -f "$UNIT_DIR/model-loader-api-gateway-watch.service" "$UNIT_DIR/model-loader-api-gateway-watch.timer"
systemctl --user daemon-reload

# The gateway and Tunnel are not enabled: BindsTo on the proxy unit starts
# and stops them with it. The proxy unit itself is never enabled at boot —
# the TUI (or the operator) starts it on demand. The gateway/Tunnel are only
# stopped here when their unit files actually changed (stale config on disk);
# otherwise a no-op rerun must not flap healthy services — and the BindsTo
# pull on the next proxy start (or the restart below) converges them.
# gateway.env (the API key) is read by caddy at startup, so a key rotation
# always restarts the gateway to pick it up.
if [[ "$units_changed" == 1 ]]; then
  systemctl --user disable --now model-loader-api-gateway.service >/dev/null 2>&1 || true
  systemctl --user disable --now model-loader-cloudflared.service >/dev/null 2>&1 || true
fi
if ! systemctl --user is-active --quiet model-loader-proxy.service; then
  systemctl --user start model-loader-proxy.service
fi
systemctl --user restart model-loader-api-gateway.service

if command -v loginctl >/dev/null 2>&1; then
  loginctl enable-linger "$USER" || fail 'could not enable user lingering for boot startup'
fi

"$SOURCE_DIR/verify.sh"
