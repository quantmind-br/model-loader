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

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

for command in caddy cloudflared curl jq python3 systemctl install sed stat; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

[[ -n "${MODELLOADER_API_KEY:-}" ]] || fail 'MODELLOADER_API_KEY is empty or unset'
[[ "$MODELLOADER_API_KEY" != *$'\n'* ]] || fail 'MODELLOADER_API_KEY must not contain a newline'
[[ "$MODELLOADER_API_KEY" != *$'\r'* ]] || fail 'MODELLOADER_API_KEY must not contain a carriage return'
# Bash variables cannot contain NUL bytes, so the NUL constraint holds inherently.
# The key value itself is never printed.

# Reject any value destined for a sed replacement: a '|' would collide with the
# delimiter and an '&' would expand to the matched text, silently corrupting
# rendered YAML/units. Newlines and carriage returns would split the unit files.
require_safe_sed_value() {
  local label="$1" value="$2"
  [[ -n "$value" ]] || fail "$label is empty"
  [[ "$value" != *'|'* ]] || fail "$label must not contain '|'"
  [[ "$value" != *'&'* ]] || fail "$label must not contain '&'"
  [[ "$value" != *$'\n'* ]] || fail "$label must not contain a newline"
  [[ "$value" != *$'\r'* ]] || fail "$label must not contain a carriage return"
}

for path_var in CONFIG_DIR UNIT_DIR CLOUDFLARED_DIR; do
  require_safe_sed_value "${path_var} (derived from XDG_CONFIG_HOME/HOME)" "${!path_var}"
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
import sys

path = pathlib.Path(sys.argv[1])
value = os.environ["MODELLOADER_API_KEY"]
if "\n" in value or "\r" in value or "\x00" in value:
    raise SystemExit("invalid API key")
escaped = value.replace("\\", "\\\\").replace('"', '\\"')
path.write_text(f'MODELLOADER_API_KEY="{escaped}"\n', encoding="utf-8")
path.chmod(0o600)
PY

unset MODELLOADER_API_KEY

# ---------------------------------------------------------------------------
# Interactive Tunnel creation or exact-name reuse
# ---------------------------------------------------------------------------
if [[ ! -f "$CLOUDFLARED_DIR/cert.pem" ]]; then
  printf 'Cloudflare authorization is required for %s.\n' "$HOSTNAME"
  cloudflared tunnel login
fi

existing_json="$(cloudflared tunnel list --name "$TUNNEL_NAME" --output json)"
existing_count="$(jq 'length' <<<"$existing_json")"
case "$existing_count" in
  0)
    created_json="$(cloudflared tunnel create --output json "$TUNNEL_NAME")"
    tunnel_id="$(jq -er '.id // .ID' <<<"$created_json")"
    ;;
  1)
    tunnel_id="$(jq -er '.[0].id // .[0].ID' <<<"$existing_json")"
    ;;
  *)
    fail "multiple Cloudflare Tunnels named $TUNNEL_NAME"
    ;;
esac

credentials_file="$CLOUDFLARED_DIR/$tunnel_id.json"
[[ -f "$credentials_file" ]] || fail "Tunnel credential file not found: $credentials_file"
chmod 0600 "$credentials_file"

require_safe_sed_value 'tunnel id' "$tunnel_id"
require_safe_sed_value 'credentials file path' "$credentials_file"

# ---------------------------------------------------------------------------
# Render and validate cloudflared configuration
# ---------------------------------------------------------------------------
sed \
  -e "s|__TUNNEL_ID__|$tunnel_id|g" \
  -e "s|__CREDENTIALS_FILE__|$credentials_file|g" \
  "$SOURCE_DIR/cloudflared.yml.tmpl" >"$CONFIG_DIR/cloudflared.yml"
chmod 0600 "$CONFIG_DIR/cloudflared.yml"
cloudflared tunnel --config "$CONFIG_DIR/cloudflared.yml" ingress validate

# ---------------------------------------------------------------------------
# DNS route without silent overwrite
# ---------------------------------------------------------------------------
readonly DNS_MARKER="$CONFIG_DIR/dns-route"
expected_route="$tunnel_id $HOSTNAME"

if [[ -f "$DNS_MARKER" && "$(<"$DNS_MARKER")" == "$expected_route" ]]; then
  printf 'DNS route already provisioned for %s.\n' "$HOSTNAME"
else
  cloudflared tunnel route dns "$tunnel_id" "$HOSTNAME" \
    || fail "DNS route creation failed; inspect the existing $HOSTNAME record before retrying"
  printf '%s\n' "$expected_route" >"$DNS_MARKER"
  chmod 0600 "$DNS_MARKER"
fi

# ---------------------------------------------------------------------------
# Render user-systemd units using absolute paths
# ---------------------------------------------------------------------------
caddy_bin="$(command -v caddy)"
cloudflared_bin="$(command -v cloudflared)"
require_safe_sed_value 'caddy binary path' "$caddy_bin"
require_safe_sed_value 'cloudflared binary path' "$cloudflared_bin"

sed \
  -e "s|__CONFIG_DIR__|$CONFIG_DIR|g" \
  -e "s|__CADDY_BIN__|$caddy_bin|g" \
  "$SOURCE_DIR/model-loader-api-gateway.service.tmpl" \
  >"$UNIT_DIR/model-loader-api-gateway.service"

sed \
  -e "s|__CONFIG_DIR__|$CONFIG_DIR|g" \
  -e "s|__CLOUDFLARED_BIN__|$cloudflared_bin|g" \
  -e "s|__TUNNEL_ID__|$tunnel_id|g" \
  "$SOURCE_DIR/model-loader-cloudflared.service.tmpl" \
  >"$UNIT_DIR/model-loader-cloudflared.service"

chmod 0600 \
  "$UNIT_DIR/model-loader-api-gateway.service" \
  "$UNIT_DIR/model-loader-cloudflared.service"

# ---------------------------------------------------------------------------
# Validate and start services (gateway first; the Tunnel unit also carries
# Requires= and ordering as defense in depth)
# ---------------------------------------------------------------------------
caddy validate \
  --config "$CONFIG_DIR/Caddyfile" \
  --adapter caddyfile \
  --envfile "$CONFIG_DIR/gateway.env"

systemctl --user daemon-reload
systemctl --user enable --now model-loader-api-gateway.service
systemctl --user enable --now model-loader-cloudflared.service

if command -v loginctl >/dev/null 2>&1; then
  loginctl enable-linger "$USER" || fail 'could not enable user lingering for boot startup'
fi

"$SOURCE_DIR/verify.sh"
