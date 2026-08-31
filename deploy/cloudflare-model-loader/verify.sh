#!/usr/bin/env bash
# Secret-safe behavioral verification for the model-loader external API.
# Consumes the gateway environment file installed by install.sh, proves the
# trust boundaries (authentication, routing, admin boundary, loopback-only
# listeners), service state, and file permissions, and prints only non-secret
# PASS/FAIL evidence. The API key never appears in argv: curl reads it from a
# mode-0600 header file inside a mode-0700 temporary directory.
set -euo pipefail

readonly HOSTNAME='model-loader.quantforge.com.br'
readonly CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/model-loader/external-api"
readonly ENV_FILE="$CONFIG_DIR/gateway.env"

# Fail early with a clear message instead of a raw exec error if the
# verification toolchain is incomplete on the deployed host.
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'PASS: %s\n' "$*"
}

for command in curl jq ss systemctl stat; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

[[ -r "$ENV_FILE" ]] || fail "missing $ENV_FILE"
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a
[[ -n "${MODELLOADER_API_KEY:-}" ]] || fail 'installed API key is empty'

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
chmod 0700 "$work_dir"
printf 'Authorization: Bearer %s\n' "$MODELLOADER_API_KEY" >"$work_dir/auth.header"
printf 'Authorization: Bearer definitely-wrong-key\n' >"$work_dir/wrong.header"
chmod 0600 "$work_dir"/*.header
unset MODELLOADER_API_KEY

http_code() {
  curl --silent --show-error --output "$work_dir/body" \
    --write-out '%{http_code}' "$@"
}

expect_code() {
  local expected="$1"
  shift
  local actual
  # `|| true` keeps the comparison (and its FAIL message) authoritative when
  # curl cannot connect at all: curl's --write-out still emits code 000.
  actual="$(http_code "$@" || true)"
  [[ "$actual" == "$expected" ]] || fail "expected HTTP $expected, got $actual for $*"
}

# ---------------------------------------------------------------------------
# Local trust boundaries
# ---------------------------------------------------------------------------
expect_code 200 http://127.0.0.1:4321/v1/models
pass 'model-loader remains directly accessible on loopback'

expect_code 401 http://127.0.0.1:4322/v1/models
expect_code 401 --header @"$work_dir/wrong.header" http://127.0.0.1:4322/v1/models
expect_code 200 --header @"$work_dir/auth.header" http://127.0.0.1:4322/v1/models
pass 'gateway enforces Bearer authentication locally'

curl --silent --show-error --dump-header "$work_dir/headers" \
  --output "$work_dir/body" http://127.0.0.1:4322/v1/models >/dev/null
tr -d '\r' <"$work_dir/headers" | grep -qix 'WWW-Authenticate: Bearer' \
  || fail '401 response lacks WWW-Authenticate: Bearer'
jq -e '.error.code == "invalid_api_key"' "$work_dir/body" >/dev/null \
  || fail '401 response lacks the expected JSON error'
pass 'gateway returns the documented authentication error'

# ---------------------------------------------------------------------------
# Public authentication and API routes (normal certificate validation only)
# ---------------------------------------------------------------------------
expect_code 401 "https://$HOSTNAME/v1/models"
expect_code 401 --header @"$work_dir/wrong.header" "https://$HOSTNAME/v1/models"
expect_code 200 --header @"$work_dir/auth.header" "https://$HOSTNAME/v1/models"
expect_code 200 --header @"$work_dir/auth.header" "https://$HOSTNAME/_status"
pass 'public hostname enforces Bearer authentication and reaches model-loader'

# ---------------------------------------------------------------------------
# Administrative boundary, proven non-destructive for the loaded profile
# ---------------------------------------------------------------------------
before_profile="$(curl --silent --show-error --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')"

admin_code="$(curl --silent --show-error --output "$work_dir/admin-body" \
  --write-out '%{http_code}' --header @"$work_dir/auth.header" \
  --header 'Content-Type: application/json' \
  --data '{"profile_id":"__external-api-auth-smoke__"}' \
  "https://$HOSTNAME/_admin/load")"
[[ "$admin_code" == 404 ]] \
  || fail "unknown-profile admin smoke expected model-loader HTTP 404, got $admin_code"
jq -e '.error.code == "model_not_found"' "$work_dir/admin-body" >/dev/null \
  || fail 'admin smoke did not return model-loader model_not_found JSON'

after_profile="$(curl --silent --show-error --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')"
[[ "$before_profile" == "$after_profile" ]] \
  || fail 'administrative smoke check changed the loaded profile'
pass 'authenticated administrative route reached model-loader without changing state'

# ---------------------------------------------------------------------------
# Services, listeners, and permissions
# ---------------------------------------------------------------------------
systemctl --user is-enabled --quiet model-loader-api-gateway.service \
  || fail 'gateway service is not enabled'
systemctl --user is-active --quiet model-loader-api-gateway.service \
  || fail 'gateway service is not active'
systemctl --user is-enabled --quiet model-loader-cloudflared.service \
  || fail 'cloudflared service is not enabled'
systemctl --user is-active --quiet model-loader-cloudflared.service \
  || fail 'cloudflared service is not active'
pass 'user services are enabled and active'

ss -ltnH '( sport = :4321 or sport = :4322 or sport = :49321 )' >"$work_dir/listeners"
# `ss -ltn` prints dual-stack wildcard binds as `*:PORT`, IPv4 as
# `0.0.0.0:PORT`, and IPv6 as `[::]:PORT`; all three are public.
if grep -Eq '(^|[[:space:]])(0\.0\.0\.0|\[::\]|\*):(4321|4322|49321)([[:space:]]|$)' "$work_dir/listeners"; then
  fail 'model-loader, gateway, or metrics listener is public'
fi
grep -Eq '(^|[[:space:]])127\.0\.0\.1:4321([[:space:]]|$)' "$work_dir/listeners" \
  || fail 'model-loader loopback listener missing'
grep -Eq '(^|[[:space:]])127\.0\.0\.1:4322([[:space:]]|$)' "$work_dir/listeners" \
  || fail 'gateway loopback listener missing'
grep -Eq '(^|[[:space:]])127\.0\.0\.1:49321([[:space:]]|$)' "$work_dir/listeners" \
  || fail 'cloudflared metrics loopback listener missing'
pass 'all API-related listeners are loopback-only'

for file in \
  "$CONFIG_DIR/gateway.env" \
  "$CONFIG_DIR/cloudflared.yml" \
  "$HOME/.cloudflared/cert.pem" \
  "$HOME/.cloudflared"/*.json; do
  [[ "$(stat -c '%a' "$file")" == 600 ]] || fail "unsafe permissions on $file"
done
[[ "$(stat -c '%a' "$CONFIG_DIR")" == 700 ]] || fail "unsafe permissions on $CONFIG_DIR"
[[ "$(stat -c '%a' "$HOME/.cloudflared")" == 700 ]] || fail "unsafe permissions on $HOME/.cloudflared"
pass 'secret and Tunnel files have restrictive permissions'

# ---------------------------------------------------------------------------
# Conditional streaming smoke test (never loads or swaps a model)
# ---------------------------------------------------------------------------
loaded_profile="$(curl --silent --show-error --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')"
if [[ -z "$loaded_profile" ]]; then
  printf 'SKIP: streaming smoke test; no profile is currently loaded\n'
else
  jq -n --arg model "$loaded_profile" '{
    model: $model,
    messages: [{role: "user", content: "Reply with OK"}],
    max_tokens: 8,
    stream: true
  }' >"$work_dir/stream-request.json"

  stream_code="$(curl --silent --show-error --no-buffer \
    --output "$work_dir/stream-body" --write-out '%{http_code}' \
    --header @"$work_dir/auth.header" \
    --header 'Content-Type: application/json' \
    --data-binary @"$work_dir/stream-request.json" \
    "https://$HOSTNAME/v1/chat/completions")"
  [[ "$stream_code" == 200 ]] || fail "streaming smoke returned HTTP $stream_code"
  grep -q '^data: ' "$work_dir/stream-body" \
    || fail 'streaming smoke returned no SSE data events'
  grep -qx 'data: \[DONE\]' "$work_dir/stream-body" \
    || fail 'streaming smoke returned no terminal DONE event'
  pass "streaming response traversed Cloudflare for profile $loaded_profile"
fi
