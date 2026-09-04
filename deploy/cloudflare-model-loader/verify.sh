#!/usr/bin/env bash
# Secret-safe behavioral verification for the model-loader external API.
# Consumes the gateway environment file installed by install.sh, proves the
# trust boundaries (authentication, routing, admin boundary, loopback-only
# listeners), service state, and file permissions, and prints only non-secret
# PASS/FAIL evidence. The API key never appears in argv: curl reads it from a
# mode-0600 header file inside a mode-0700 temporary directory. Every request
# runs through the vcurl wrapper, which disables curlrc/environment option
# injection (--disable) and never lets ambient proxies divert loopback checks
# (--noproxy).
set -euo pipefail

readonly HOSTNAME='model-loader.quantforge.com.br'
readonly CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/model-loader/external-api"
readonly ENV_FILE="$CONFIG_DIR/gateway.env"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'PASS: %s\n' "$*"
}

# Fail early with a clear message instead of a raw exec error if the
# verification toolchain is incomplete on the deployed host.
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
# The wrong-key probe is derived from the installed secret rather than hardcoded,
# so it can never accidentally *be* the installed key: the fixed suffix makes the
# probe strictly different from — and longer than — any value an equality
# matcher could accept. The derived value goes straight into a mode-0600 header
# file and is never printed.
printf 'Authorization: Bearer %s\n' "$MODELLOADER_API_KEY" >"$work_dir/auth.header"
printf 'Authorization: Bearer %s\n' \
  "${MODELLOADER_API_KEY}-definitely-not-the-installed-key" >"$work_dir/wrong.header"
chmod 0600 "$work_dir"/*.header
unset MODELLOADER_API_KEY

# Every curl invocation goes through this wrapper; the URL must be the final
# argument. `--disable` (curl requires it first) ignores ~/.curlrc, CURL_HOME,
# and option-setting environment variables, so a stray or hostile curlrc
# cannot add tracing/verbose output that would leak the Authorization header
# or otherwise alter what is being verified. Loopback URLs additionally get
# `--noproxy '*'` so ambient http(s)_proxy configuration can never divert a
# local trust-boundary probe through an external proxy.
vcurl() {
  (( $# >= 1 )) || fail 'vcurl called without a URL'
  local url="${!#}"
  local -a guard=(--disable)
  case "$url" in
    http://127.0.0.1:* | http://localhost:* | http://'[::1]':*)
      guard+=(--noproxy '*')
      ;;
  esac
  curl "${guard[@]}" --silent --show-error "${@:1:$#-1}" "$url"
}

http_code() {
  vcurl --output "$work_dir/body" --write-out '%{http_code}' "$@"
}

# Captures curl's exit status separately from the HTTP code: transport errors
# (proxy refused, DNS, connection refused) must fail with the exit code and
# the 000 code curl still wrote, never masquerade as a clean mismatch.
expect_code() {
  local expected="$1"
  shift
  local actual rc
  actual="$(http_code "$@")" && rc=0 || rc=$?
  [[ "$rc" == 0 ]] || fail "curl exit $rc (HTTP $actual) for $*"
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

vcurl --dump-header "$work_dir/headers" \
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
before_profile="$(vcurl --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')" \
  || fail 'could not read the loaded profile from /_status'

# The smoke target must be a syntactically valid profile id (the proxy rejects
# anything outside `^[A-Za-z0-9._-]+$` with 400, which would prove nothing about
# the admin boundary) that is *not* a real profile, so the request can never
# load or swap a model. Instead of trusting a fixed literal, derive a nonce id
# and prove it absent from the authenticated /v1/models listing (the same store
# /_admin/load resolves against) before using it.
vcurl --header @"$work_dir/auth.header" "https://$HOSTNAME/v1/models" \
  >"$work_dir/models.json" \
  || fail 'could not read the authenticated /v1/models listing'
jq -e '.object == "list" and (.data | type == "array")' "$work_dir/models.json" >/dev/null \
  || fail 'authenticated /v1/models did not return a model list'

admin_probe=''
for attempt in $(seq 0 9); do
  candidate="external-api-verify-$(date +%s%N)-${RANDOM:-$attempt}-$attempt"
  [[ "$candidate" =~ ^[A-Za-z0-9._-]+$ ]] || continue
  [[ "$candidate" != "$before_profile" ]] || continue
  if jq -e --arg id "$candidate" 'any(.data[]?; .id == $id)' \
    "$work_dir/models.json" >/dev/null; then
    continue
  fi
  admin_probe="$candidate"
  break
done
[[ -n "$admin_probe" ]] || fail 'could not derive a profile id absent from /v1/models'

admin_rc=0
admin_code="$(vcurl --output "$work_dir/admin-body" \
  --write-out '%{http_code}' --header @"$work_dir/auth.header" \
  --header 'Content-Type: application/json' \
  --data "$(jq -cn --arg id "$admin_probe" '{profile_id: $id}')" \
  "https://$HOSTNAME/_admin/load")" || admin_rc=$?
[[ "$admin_rc" == 0 ]] || fail "admin smoke curl failed (exit $admin_rc, HTTP $admin_code)"
[[ "$admin_code" == 404 ]] \
  || fail "unknown-profile admin smoke ($admin_probe) expected model-loader HTTP 404, got $admin_code"
jq -e '.error.code == "model_not_found"' "$work_dir/admin-body" >/dev/null \
  || fail 'admin smoke did not return model-loader model_not_found JSON'

after_profile="$(vcurl --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')" \
  || fail 'could not re-read the loaded profile from /_status'
[[ "$before_profile" == "$after_profile" ]] \
  || fail 'administrative smoke check changed the loaded profile'
pass 'authenticated administrative route reached model-loader without changing state'

# ---------------------------------------------------------------------------
# Proxy unit lifecycle, exposure topology, listeners, and permissions
# ---------------------------------------------------------------------------
# The proxy unit owns the lifecycle now: gateway + Tunnel bind to it via
# BindsTo/PartOf, so no polling timer exists anymore.
systemctl --user is-active --quiet model-loader-proxy.service \
  || fail 'model-loader-proxy.service is not active'
proxy_pid="$(systemctl --user show model-loader-proxy.service -p MainPID --value)"
[[ "$proxy_pid" =~ ^[1-9][0-9]*$ ]] || fail 'proxy unit has no valid MainPID'
pass "proxy unit active with MainPID $proxy_pid"

# Readiness done (READY=1 consumed) and watchdog armed.
systemctl --user show model-loader-proxy.service -p ActiveState --value | grep -qx 'active' \
  || fail 'proxy unit is not in active state'
watchdog_usec="$(systemctl --user show model-loader-proxy.service -p WatchdogUSec --value)"
# WatchdogUSec prints humanized ("15s") on systemd >= 250 and raw
# microseconds on older ones — accept both.
[[ "$watchdog_usec" =~ ^[1-9][0-9]* ]] || fail 'proxy unit watchdog is not configured'
pass 'proxy readiness complete and watchdog configured'

# Gateway and Tunnel follow the same lifecycle generation.
systemctl --user is-active --quiet model-loader-api-gateway.service \
  || fail 'gateway service is not active while the proxy is active'
systemctl --user is-active --quiet model-loader-cloudflared.service \
  || fail 'cloudflared service is not active while the proxy is active'
systemctl --user show model-loader-api-gateway.service -p BindsTo --value | grep -q 'model-loader-proxy.service' \
  || fail 'gateway is not BindsTo-bound to the proxy unit'
systemctl --user show model-loader-cloudflared.service -p BindsTo --value | grep -q 'model-loader-proxy.service' \
  || fail 'cloudflared is not BindsTo-bound to the proxy unit'
pass 'gateway and Tunnel follow the proxy unit lifecycle'

# The 1s polling watcher must be gone: timer dead, unit files retired.
if systemctl --user is-active --quiet model-loader-api-gateway-watch.timer 2>/dev/null; then
  fail 'legacy proxy watcher timer is still active'
fi
watch_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
if [[ -e "$watch_dir/model-loader-api-gateway-watch.service" || -e "$watch_dir/model-loader-api-gateway-watch.timer" ]]; then
  fail 'legacy proxy watcher unit files still installed'
fi
pass 'legacy polling watcher retired'

# Local reachability gates the rest: without the loopback proxy there is
# nothing to expose, and the units above already prove lifecycle state.
proxy_listening=0
if ss -ltnH 'src = 127.0.0.1:4321' | grep -q .; then
  proxy_listening=1
fi
[[ "$proxy_listening" == 1 ]] || fail 'proxy loopback listener missing while its unit is active'
pass 'external API services follow the proxy unit'

ss -ltnH '( sport = :4321 or sport = :4322 or sport = :49321 )' >"$work_dir/listeners"

# Allowlist, not blocklist: every local address selected on the API ports
# must itself be loopback. Wildcards (`*:PORT`, `0.0.0.0`, `[::]`) and any
# concrete external IPv4/IPv6 address — even coexisting with a correct
# loopback listener — are rejected. `ss -ltnH` fields are:
# State Recv-Q Send-Q LocalAddress:Port PeerAddress:Port.
bad_listeners="$(awk '
  NF < 5 { next }
  { addr = $4; sub(/:[0-9]+$/, "", addr) }
  addr != "127.0.0.1" && addr != "[::1]" && addr != "::1" { print addr }
' "$work_dir/listeners")"
[[ -z "$bad_listeners" ]] \
  || fail "non-loopback listener bound on an API port: $(tr '\n' ' ' <<<"$bad_listeners")"
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
# Private gateway binary and Tunnel health (no secrets involved)
# ---------------------------------------------------------------------------
private_caddy="$HOME/.local/lib/model-loader/bin/caddy"
[[ -x "$private_caddy" ]] || fail 'private caddy copy missing or not executable'
[[ "$(stat -c '%a' "$private_caddy")" == 755 ]] || fail 'private caddy copy has wrong permissions'
# The gateway unit must pin the private copy, never PATH.
systemctl --user show model-loader-api-gateway.service -p ExecStart --value | grep -Fq "$private_caddy" \
  || fail 'gateway unit does not pin the private caddy binary'
"$private_caddy" validate \
  --config "$CONFIG_DIR/Caddyfile" \
  --adapter caddyfile \
  --envfile "$CONFIG_DIR/gateway.env" >/dev/null \
  || fail 'private caddy copy rejects the installed Caddyfile'
pass 'private caddy copy is pinned, executable, and validates the config'

# cloudflared metrics on loopback: HA connections registered, no hard error
# counters tripping right now. Field names follow cloudflared's Prometheus
# exposition (cloudflared_tunnel_ha_connections, cloudflared_tunnel_errors).
metrics="$(vcurl http://127.0.0.1:49321/metrics)" || fail 'cloudflared metrics endpoint unreachable'
ha="$(grep -E '^cloudflared_tunnel_ha_connections [0-9]+' <<<"$metrics" | awk '{print $2}' | head -n 1)"
[[ -n "$ha" && "$ha" -ge 1 ]] 2>/dev/null || fail 'cloudflared reports no HA Tunnel connection'
pass "cloudflared Tunnel connected (ha_connections=$ha)"


# ---------------------------------------------------------------------------
# Conditional streaming smoke test (never loads or swaps a model)
# ---------------------------------------------------------------------------
loaded_profile="$(vcurl --header @"$work_dir/auth.header" \
  "https://$HOSTNAME/_status" | jq -r '.loaded_profile_id // ""')" \
  || fail 'could not read the loaded profile for the streaming smoke test'
if [[ -z "$loaded_profile" ]]; then
  printf 'SKIP: streaming smoke test; no profile is currently loaded\n'
else
  jq -n --arg model "$loaded_profile" '{
    model: $model,
    messages: [{role: "user", content: "Reply with OK"}],
    max_tokens: 8,
    stream: true
  }' >"$work_dir/stream-request.json"

  stream_rc=0
  stream_code="$(vcurl --no-buffer \
    --output "$work_dir/stream-body" --write-out '%{http_code}' \
    --header @"$work_dir/auth.header" \
    --header 'Content-Type: application/json' \
    --data-binary @"$work_dir/stream-request.json" \
    "https://$HOSTNAME/v1/chat/completions")" || stream_rc=$?
  [[ "$stream_rc" == 0 ]] || fail "streaming smoke curl failed (exit $stream_rc, HTTP $stream_code)"
  [[ "$stream_code" == 200 ]] || fail "streaming smoke returned HTTP $stream_code"
  grep -q '^data: ' "$work_dir/stream-body" \
    || fail 'streaming smoke returned no SSE data events'
  # At least one data event must carry content: `[DONE]` alone proves only
  # that a stream terminated, not that the model answered.
  grep '^data: ' "$work_dir/stream-body" | grep -vxq 'data: \[DONE\]' \
    || fail 'streaming smoke returned only the terminal DONE event, no content chunks'
  grep -qx 'data: \[DONE\]' "$work_dir/stream-body" \
    || fail 'streaming smoke returned no terminal DONE event'
  pass "streaming response traversed Cloudflare for profile $loaded_profile"
fi
