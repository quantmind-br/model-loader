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

for command in caddy cloudflared curl jq python3 systemctl install sed stat; do
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
# Render user-systemd units using absolute paths
# ---------------------------------------------------------------------------
caddy_bin="$(command -v caddy)"
cloudflared_bin="$(command -v cloudflared)"
watch_bin="$SOURCE_DIR/watch-proxy.sh"
require_safe_unit_path 'caddy binary path' "$caddy_bin"
require_safe_unit_path 'cloudflared binary path' "$cloudflared_bin"
require_safe_unit_path 'proxy watcher path' "$watch_bin"

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

sed \
  -e "s|__WATCH_BIN__|$watch_bin|g" \
  "$SOURCE_DIR/model-loader-api-gateway-watch.service.tmpl" \
  >"$UNIT_DIR/model-loader-api-gateway-watch.service"

install -m 0600 \
  "$SOURCE_DIR/model-loader-api-gateway-watch.timer.tmpl" \
  "$UNIT_DIR/model-loader-api-gateway-watch.timer"

chmod 0600 \
  "$UNIT_DIR/model-loader-api-gateway.service" \
  "$UNIT_DIR/model-loader-cloudflared.service" \
  "$UNIT_DIR/model-loader-api-gateway-watch.service"

# ---------------------------------------------------------------------------
# Validate configs, then enable the watcher timer. The gateway and Tunnel
# services are not enabled: they start and stop with the proxy listener.
# ---------------------------------------------------------------------------
caddy validate \
  --config "$CONFIG_DIR/Caddyfile" \
  --adapter caddyfile \
  --envfile "$CONFIG_DIR/gateway.env"

systemctl --user daemon-reload
systemctl --user disable --now model-loader-api-gateway.service >/dev/null 2>&1 || true
systemctl --user disable --now model-loader-cloudflared.service >/dev/null 2>&1 || true
systemctl --user enable --now model-loader-api-gateway-watch.timer
systemctl --user start model-loader-api-gateway-watch.service

if command -v loginctl >/dev/null 2>&1; then
  loginctl enable-linger "$USER" || fail 'could not enable user lingering for boot startup'
fi

"$SOURCE_DIR/verify.sh"
