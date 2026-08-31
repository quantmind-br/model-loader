# Cloudflare External API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the complete model-loader API at `https://model-loader.quantforge.com.br` through a named Cloudflare Tunnel, with external `Authorization: Bearer` authentication and unchanged unauthenticated loopback access.

**Architecture:** Keep model-loader on `127.0.0.1:4321`. Run a dedicated loopback-only Caddy gateway on `127.0.0.1:4322` that validates `MODELLOADER_API_KEY`, strips the credential, and proxies to model-loader; a named Cloudflare Tunnel maps the public hostname only to that gateway. Install both processes as hardened user systemd services and keep every secret/credential outside Git.

**Tech Stack:** Caddy 2.11.3, cloudflared 2026.5.2, user systemd 261, Bash, curl, jq, Cloudflare named Tunnels and proxied DNS.

## Global Constraints

- Public hostname is exactly `model-loader.quantforge.com.br`.
- Public scope includes `/v1/*`, `/v1beta/*`, `/_status`, and `/_admin/*`.
- External clients authenticate only with `Authorization: Bearer $MODELLOADER_API_KEY`.
- Direct access to `127.0.0.1:4321` remains unauthenticated.
- model-loader and Caddy remain bound only to loopback; no router forwarding or firewall opening.
- Never commit, print, log, or place the literal API key in a process command line.
- Never use cloudflared debug logging because it can log request headers.
- Cloudflare Tunnel credentials and the API-key environment file must be mode `0600`; their parent configuration directories must be mode `0700`.
- Caddy must strip `Authorization` before forwarding and preserve streaming responses without response buffering.
- A DNS conflict must stop provisioning; do not overwrite an existing hostname record silently.
- Do not change Go source or model-loader's local authentication behavior.
- Design authority: `docs/superpowers/specs/2026-08-31-cloudflare-external-api-design.md`.

## File Structure

Create one focused deployment directory:

```text
deploy/cloudflare-model-loader/
├── Caddyfile
├── cloudflared.yml.tmpl
├── install.sh
├── model-loader-api-gateway.service.tmpl
├── model-loader-cloudflared.service.tmpl
└── verify.sh
```

Responsibilities:

- `Caddyfile`: authentication boundary and reverse proxy only.
- `cloudflared.yml.tmpl`: exact public-hostname ingress plus terminal 404.
- `*.service.tmpl`: user-systemd lifecycle and hardening; installation replaces absolute-path tokens.
- `install.sh`: validates prerequisites and secret, performs interactive Cloudflare authorization, creates/reuses one named Tunnel, routes DNS, renders local configuration, installs/enables services, and invokes verification.
- `verify.sh`: secret-safe local/public behavioral checks, listener checks, service checks, and file-permission checks.
- `openwiki/operations.md`: operator-facing setup, rotation, status, and rollback commands.
- `CHANGELOG.md`: one Unreleased entry for the external deployment option.

---

### Task 1: Add and validate the authenticated Caddy gateway

**Files:**
- Create: `deploy/cloudflare-model-loader/Caddyfile`
- Create: `deploy/cloudflare-model-loader/model-loader-api-gateway.service.tmpl`

**Interfaces:**
- Consumes: environment variable `MODELLOADER_API_KEY`; model-loader origin `http://127.0.0.1:4321`.
- Produces: authenticated HTTP origin `http://127.0.0.1:4322`; user unit rendered to `~/.config/systemd/user/model-loader-api-gateway.service`.

- [ ] **Step 1: Create the gateway Caddyfile**

Create `deploy/cloudflare-model-loader/Caddyfile` with exactly:

```caddyfile
{
	admin off
	auto_https off
}

http://127.0.0.1:4322 {
	@authorized header Authorization "Bearer {$MODELLOADER_API_KEY}"

	handle @authorized {
		request_header -Authorization
		reverse_proxy 127.0.0.1:4321 {
			flush_interval -1
		}
	}

	handle {
		header Content-Type "application/json; charset=utf-8"
		header WWW-Authenticate "Bearer"
		respond `{"error":{"message":"invalid API key","type":"authentication_error","code":"invalid_api_key"}}` 401
	}
}
```

`flush_interval -1` disables periodic response buffering and flushes streaming chunks immediately. The authorization matcher covers every path, including administrative paths.

- [ ] **Step 2: Create the gateway unit template**

Create `deploy/cloudflare-model-loader/model-loader-api-gateway.service.tmpl`:

```ini
[Unit]
Description=model-loader external API authentication gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
EnvironmentFile=__CONFIG_DIR__/gateway.env
ExecStart=__CADDY_BIN__ run --config __CONFIG_DIR__/Caddyfile --adapter caddyfile
ExecReload=__CADDY_BIN__ reload --config __CONFIG_DIR__/Caddyfile --adapter caddyfile --force
Restart=on-failure
RestartSec=3s
TimeoutStopSec=10s
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=read-only
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX

[Install]
WantedBy=default.target
```

The service reads the secret from a mode-`0600` environment file. `ProtectHome=read-only` permits reading configuration but prevents gateway writes into the home directory.

- [ ] **Step 3: Validate Caddy syntax with a disposable key**

Run without using the real secret:

```bash
MODELLOADER_API_KEY='validation-only-not-a-real-key' \
  caddy validate \
  --config deploy/cloudflare-model-loader/Caddyfile \
  --adapter caddyfile
```

Expected: exit `0` and `Valid configuration`. Any warning about an empty API key is a failure.

- [ ] **Step 4: Smoke-test the gateway boundary locally**

Start a temporary Caddy instance through the process manager rather than a detached shell. Use the Caddyfile and environment `MODELLOADER_API_KEY=validation-only-not-a-real-key`. Then run:

```bash
curl --silent --output /tmp/model-loader-gateway-unauthorized.json \
  --write-out '%{http_code}' \
  http://127.0.0.1:4322/v1/models
```

Expected: status `401`; body equals the JSON error defined in the Caddyfile; response includes `WWW-Authenticate: Bearer` when repeated with `--dump-header` to a mode-`0600` temporary file.

Create a mode-`0600` temporary curl header file containing:

```text
Authorization: Bearer validation-only-not-a-real-key
```

Then run `curl --header @<header-file> http://127.0.0.1:4322/v1/models`. Expected: the response status equals direct `http://127.0.0.1:4321/v1/models`, proving authorized forwarding. Stop the temporary Caddy process.

- [ ] **Step 5: Commit the gateway assets**

```bash
git add deploy/cloudflare-model-loader/Caddyfile \
  deploy/cloudflare-model-loader/model-loader-api-gateway.service.tmpl
git commit -m "feat(ops): add authenticated model-loader gateway"
```

---

### Task 2: Add Cloudflare Tunnel configuration and service lifecycle

**Files:**
- Create: `deploy/cloudflare-model-loader/cloudflared.yml.tmpl`
- Create: `deploy/cloudflare-model-loader/model-loader-cloudflared.service.tmpl`

**Interfaces:**
- Consumes: Tunnel UUID `__TUNNEL_ID__`, credential path `__CREDENTIALS_FILE__`, Caddy origin `http://127.0.0.1:4322`.
- Produces: named Tunnel ingress for `model-loader.quantforge.com.br`; user unit rendered to `~/.config/systemd/user/model-loader-cloudflared.service`.

- [ ] **Step 1: Create the Tunnel configuration template**

Create `deploy/cloudflare-model-loader/cloudflared.yml.tmpl`:

```yaml
tunnel: __TUNNEL_ID__
credentials-file: __CREDENTIALS_FILE__

originRequest:
  connectTimeout: 10s
  tcpKeepAlive: 30s
  keepAliveTimeout: 90s
  noHappyEyeballs: false

ingress:
  - hostname: model-loader.quantforge.com.br
    service: http://127.0.0.1:4322
  - service: http_status:404
```

- [ ] **Step 2: Create the cloudflared unit template**

Create `deploy/cloudflare-model-loader/model-loader-cloudflared.service.tmpl`:

```ini
[Unit]
Description=Cloudflare Tunnel for model-loader external API
After=network-online.target model-loader-api-gateway.service
Wants=network-online.target
Requires=model-loader-api-gateway.service

[Service]
Type=simple
ExecStart=__CLOUDFLARED_BIN__ tunnel --no-autoupdate --config __CONFIG_DIR__/cloudflared.yml --loglevel info --metrics 127.0.0.1:49321 run __TUNNEL_ID__
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=read-only
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX

[Install]
WantedBy=default.target
```

Keep `--loglevel info`; never use `debug`. Bind metrics explicitly to loopback so cloudflared cannot choose a wildcard address.

- [ ] **Step 3: Validate a rendered disposable ingress document**

Copy the template to a temporary file, replace:

- `__TUNNEL_ID__` with `00000000-0000-0000-0000-000000000000`;
- `__CREDENTIALS_FILE__` with `/tmp/nonexistent-cloudflared-credentials.json`.

Run:

```bash
cloudflared tunnel --config <temporary-file> ingress validate
```

Expected: exit `0`; ingress validation checks rule structure and does not require the credential file to exist.

- [ ] **Step 4: Commit the Tunnel assets**

```bash
git add deploy/cloudflare-model-loader/cloudflared.yml.tmpl \
  deploy/cloudflare-model-loader/model-loader-cloudflared.service.tmpl
git commit -m "feat(ops): add model-loader Cloudflare Tunnel assets"
```

---

### Task 3: Add deterministic provisioning and safe secret installation

**Files:**
- Create: `deploy/cloudflare-model-loader/install.sh`

**Interfaces:**
- Consumes: non-empty `MODELLOADER_API_KEY`, checked-in templates, installed `caddy`, `cloudflared`, `curl`, `jq`, `systemctl`, and Cloudflare browser authorization.
- Produces: `~/.config/model-loader/external-api/{Caddyfile,gateway.env,cloudflared.yml}`, two rendered user units, named Tunnel `model-loader-quantforge`, and DNS route `model-loader.quantforge.com.br`.

- [ ] **Step 1: Implement prerequisite and secret validation**

Start `deploy/cloudflare-model-loader/install.sh` with:

```bash
#!/usr/bin/env bash
set -euo pipefail

readonly HOSTNAME='model-loader.quantforge.com.br'
readonly TUNNEL_NAME='model-loader-quantforge'
readonly GATEWAY_PORT='4322'
readonly METRICS_PORT='49321'
readonly SOURCE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
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
```

Bash variables cannot contain NUL bytes, satisfying the NUL constraint inherently. Do not print the key.

- [ ] **Step 2: Implement secret-safe local file rendering**

Continue the script with:

```bash
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
```

The literal key exists only in the caller environment and the mode-`0600` environment file; it is never placed in process arguments.

- [ ] **Step 3: Implement interactive Tunnel creation or exact-name reuse**

Add:

```bash
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
```


- [ ] **Step 4: Render and validate cloudflared configuration**

Add:

```bash
sed \
  -e "s|__TUNNEL_ID__|$tunnel_id|g" \
  -e "s|__CREDENTIALS_FILE__|$credentials_file|g" \
  "$SOURCE_DIR/cloudflared.yml.tmpl" >"$CONFIG_DIR/cloudflared.yml"
chmod 0600 "$CONFIG_DIR/cloudflared.yml"
cloudflared tunnel --config "$CONFIG_DIR/cloudflared.yml" ingress validate
```

The credentials path contains no expected sed metacharacters on this workstation. If it does contain `|` or `&`, stop with an explicit validation failure rather than generating ambiguous YAML.

- [ ] **Step 5: Create the DNS route without silent overwrite**

Add:

```bash
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
```

The marker is written only after Cloudflare accepts the route. A fresh conflicting DNS record remains a hard failure; the installer never uses `--overwrite-dns`. Public verification still detects a stale or externally changed route.

- [ ] **Step 6: Render user-systemd units using absolute paths**

Add:

```bash
caddy_bin="$(command -v caddy)"
cloudflared_bin="$(command -v cloudflared)"

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
```

Before rendering, reject replacement values containing `|`, `&`, newline, or carriage return.

- [ ] **Step 7: Validate and start services**

Add:

```bash
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
```

Start the gateway first; the Tunnel unit also has `Requires=` and ordering as defense in depth.

- [ ] **Step 8: Add shell static checks and commit**

Run:

```bash
bash -n deploy/cloudflare-model-loader/install.sh
shellcheck deploy/cloudflare-model-loader/install.sh
chmod 0755 deploy/cloudflare-model-loader/install.sh
git add deploy/cloudflare-model-loader/install.sh
git commit -m "feat(ops): provision model-loader external API"
```

If `shellcheck` is not installed, install it before continuing; do not silently skip the check.

---

### Task 4: Add secret-safe behavioral verification

**Files:**
- Create: `deploy/cloudflare-model-loader/verify.sh`

**Interfaces:**
- Consumes: installed gateway environment file, local ports `4321`/`4322`, public hostname, user service state, installed Tunnel credentials.
- Produces: non-secret pass/fail evidence for authentication, routing, admin boundary, loopback binding, service state, and permissions.

- [ ] **Step 1: Implement common verification helpers**

Create `deploy/cloudflare-model-loader/verify.sh`:

```bash
#!/usr/bin/env bash
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
  actual="$(http_code "$@")"
  [[ "$actual" == "$expected" ]] || fail "expected HTTP $expected, got $actual for $*"
}
```

The API key never appears in argv; curl reads it from a mode-`0600` header file.

- [ ] **Step 2: Verify local trust boundaries**

Append:

```bash
expect_code 200 http://127.0.0.1:4321/v1/models
pass 'model-loader remains directly accessible on loopback'

expect_code 401 http://127.0.0.1:4322/v1/models
expect_code 401 --header @"$work_dir/wrong.header" http://127.0.0.1:4322/v1/models
expect_code 200 --header @"$work_dir/auth.header" http://127.0.0.1:4322/v1/models
pass 'gateway enforces Bearer authentication locally'

curl --silent --show-error --dump-header "$work_dir/headers" \
  --output "$work_dir/body" http://127.0.0.1:4322/v1/models >/dev/null
tr -d '\r' <"$work_dir/headers" | grep -qx 'WWW-Authenticate: Bearer' \
  || fail '401 response lacks WWW-Authenticate: Bearer'
jq -e '.error.code == "invalid_api_key"' "$work_dir/body" >/dev/null \
  || fail '401 response lacks the expected JSON error'
pass 'gateway returns the documented authentication error'
```

Use the repository `grep` tool when authoring; the runtime verification script may use the standard `grep` binary because it is part of the deployed script, not an agent discovery command.

- [ ] **Step 3: Verify public authentication and API routes**

Append:

```bash
expect_code 401 "https://$HOSTNAME/v1/models"
expect_code 401 --header @"$work_dir/wrong.header" "https://$HOSTNAME/v1/models"
expect_code 200 --header @"$work_dir/auth.header" "https://$HOSTNAME/v1/models"
expect_code 200 --header @"$work_dir/auth.header" "https://$HOSTNAME/_status"
pass 'public hostname enforces Bearer authentication and reaches model-loader'
```

Use curl's normal certificate validation; never add `--insecure`.

- [ ] **Step 4: Verify the administrative boundary without changing the loaded model**

Append:

```bash
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
```

- [ ] **Step 5: Verify listeners, services, and permissions**

Append:

```bash
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
if grep -Eq '(^|[[:space:]])(0\.0\.0\.0|\[::\]):(4321|4322|49321)([[:space:]]|$)' "$work_dir/listeners"; then
  fail 'model-loader, gateway, or metrics listener is public'
fi
grep -q '127.0.0.1:4321' "$work_dir/listeners" || fail 'model-loader loopback listener missing'
grep -q '127.0.0.1:4322' "$work_dir/listeners" || fail 'gateway loopback listener missing'
grep -q '127.0.0.1:49321' "$work_dir/listeners" || fail 'cloudflared metrics loopback listener missing'
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
```

- [ ] **Step 6: Add conditional streaming verification**

Append:

```bash
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
```

The request names only the already-loaded profile. It must not unload or swap models.

- [ ] **Step 7: Validate and commit the verification script**

```bash
bash -n deploy/cloudflare-model-loader/verify.sh
shellcheck deploy/cloudflare-model-loader/verify.sh
chmod 0755 deploy/cloudflare-model-loader/verify.sh
git add deploy/cloudflare-model-loader/verify.sh
git commit -m "test(ops): verify model-loader external API exposure"
```

---

### Task 5: Provision the real Cloudflare exposure

**Files:**
- Create outside Git: `~/.config/model-loader/external-api/Caddyfile`
- Create outside Git: `~/.config/model-loader/external-api/gateway.env`
- Create outside Git: `~/.config/model-loader/external-api/cloudflared.yml`
- Create outside Git: `~/.cloudflared/cert.pem`
- Create outside Git: `~/.cloudflared/<tunnel-uuid>.json`
- Create outside Git: `~/.config/systemd/user/model-loader-api-gateway.service`
- Create outside Git: `~/.config/systemd/user/model-loader-cloudflared.service`

**Interfaces:**
- Consumes: real `MODELLOADER_API_KEY`; interactive authorization for the `quantforge.com.br` Cloudflare zone.
- Produces: live authenticated public API and boot-persistent user services.

- [ ] **Step 1: Make the real key available only for installer execution**

In the controlling shell, export the existing secret without pasting it into chat or a command recorded in repository history:

```bash
export MODELLOADER_API_KEY
```

If the variable is not already populated by the user's secret manager or shell configuration, stop and ask the user to populate it locally. Never generate a replacement without explicit instruction because clients may already depend on the intended key.

- [ ] **Step 2: Run the installer and complete browser authorization**

Run:

```bash
deploy/cloudflare-model-loader/install.sh
```

When `cloudflared tunnel login` opens its authorization URL, the user selects/authorizes the `quantforge.com.br` zone. Continue the same installer after `cert.pem` is written.

Expected provisioning results:

- exactly one active Tunnel named `model-loader-quantforge`;
- DNS route `model-loader.quantforge.com.br` targeting that Tunnel;
- validated local Caddy and cloudflared configurations;
- both user services enabled and active;
- user lingering enabled;
- installer-invoked verification passes.

- [ ] **Step 3: Inspect service logs without exposing headers**

Run:

```bash
systemctl --user status --no-pager model-loader-api-gateway.service
systemctl --user status --no-pager model-loader-cloudflared.service
journalctl --user -u model-loader-api-gateway.service -n 50 --no-pager
journalctl --user -u model-loader-cloudflared.service -n 50 --no-pager
```

Expected: Caddy listening on `127.0.0.1:4322`; cloudflared reports established connections; no API key or Authorization header appears. Do not switch cloudflared to debug logging.

- [ ] **Step 4: Run the verification script independently**

```bash
deploy/cloudflare-model-loader/verify.sh
```

Expected: every mandatory check prints `PASS`; streaming prints `PASS` when a profile is loaded or the exact documented `SKIP` otherwise.

---

### Task 6: Document operation, rotation, and rollback

**Files:**
- Modify: `openwiki/operations.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: installed commands and paths from Tasks 1–5.
- Produces: authoritative operator instructions consistent with the deployed system.

- [ ] **Step 1: Add the external API runbook to OpenWiki operations**

Add an `## External API through Cloudflare` section before `## Troubleshooting` in `openwiki/operations.md`. Document:

```bash
export MODELLOADER_API_KEY
deploy/cloudflare-model-loader/install.sh

deploy/cloudflare-model-loader/verify.sh
systemctl --user status model-loader-api-gateway.service
systemctl --user status model-loader-cloudflared.service
```

Document the client contract:

```bash
curl https://model-loader.quantforge.com.br/v1/models \
  -H "Authorization: Bearer $MODELLOADER_API_KEY"
```

Document key rotation without showing the key on argv:

1. stop editing if the value contains newline/carriage return;
2. write `MODELLOADER_API_KEY="<escaped-value>"` to `~/.config/model-loader/external-api/gateway.env` with mode `0600` using a local secret-aware editor or rerun `install.sh` with the new exported value;
3. run `systemctl --user restart model-loader-api-gateway.service`;
4. run `deploy/cloudflare-model-loader/verify.sh`.

Document rollback:

```bash
systemctl --user disable --now model-loader-cloudflared.service
systemctl --user disable --now model-loader-api-gateway.service
cloudflared tunnel delete model-loader-quantforge
```

The installed cloudflared CLI has no DNS-route deletion command. Document that the operator must first delete `model-loader.quantforge.com.br` in Cloudflare DNS, then run `cloudflared tunnel delete model-loader-quantforge`, and finally remove `~/.config/model-loader/external-api/` plus the two user unit files. Do not use `tunnel delete --force`, which can hide remaining dependencies.

State explicitly that the public key grants `/_admin/load` and `/_admin/unload`, while local `127.0.0.1:4321` remains unauthenticated.

- [ ] **Step 2: Add the changelog entry**

Under `## [Unreleased]`, add:

```markdown
### Added

- Reproducible Cloudflare Tunnel deployment for an externally authenticated,
  loopback-origin model-loader API.
```

- [ ] **Step 3: Verify documentation consistency**

Run searches for these exact facts:

- hostname `model-loader.quantforge.com.br` appears in deployment assets and operations docs;
- port `4322` appears only as the Caddy gateway port in this feature;
- metrics port `49321` appears only as loopback cloudflared metrics;
- no literal value of the real `MODELLOADER_API_KEY` exists in tracked files;
- docs do not claim model-loader itself authenticates requests.

Then run:

```bash
git diff --check
```

Expected: no whitespace errors or contradictory instructions.

- [ ] **Step 4: Commit documentation**

```bash
git add openwiki/operations.md CHANGELOG.md
git commit -m "docs: add Cloudflare external API runbook"
```

---

### Task 7: Final security and behavior gate

**Files:**
- Verify: `deploy/cloudflare-model-loader/*`
- Verify: `openwiki/operations.md`
- Verify: `CHANGELOG.md`
- Verify outside Git: installed configuration and user services

**Interfaces:**
- Consumes: complete implementation and live Cloudflare deployment.
- Produces: final evidence that the exposure meets the approved design.

- [ ] **Step 1: Run static deployment validation**

```bash
bash -n deploy/cloudflare-model-loader/install.sh
bash -n deploy/cloudflare-model-loader/verify.sh
shellcheck deploy/cloudflare-model-loader/install.sh deploy/cloudflare-model-loader/verify.sh
MODELLOADER_API_KEY='validation-only-not-a-real-key' \
  caddy validate --config deploy/cloudflare-model-loader/Caddyfile --adapter caddyfile
cloudflared tunnel --config "$HOME/.config/model-loader/external-api/cloudflared.yml" ingress validate
git diff --check
```

Expected: every command exits `0`.

- [ ] **Step 2: Run live end-to-end verification**

```bash
deploy/cloudflare-model-loader/verify.sh
```

Expected mandatory evidence:

- direct local `/v1/models`: `200` without a key;
- gateway/public missing and wrong keys: `401`;
- gateway/public correct key: `/v1/models` `200`;
- authenticated public `/_status`: `200`;
- authenticated unknown-profile admin smoke reaches model-loader without changing active profile;
- model-loader, Caddy, and cloudflared metrics listeners are loopback-only;
- both services enabled and active;
- secret and Tunnel files are `0600`, directories `0700`;
- streaming passes when a profile is already loaded, otherwise explicit `SKIP` only for that conditional check.

- [ ] **Step 3: Verify Cloudflare Tunnel identity and DNS**

Run JSON output and inspect without human-table parsing:

```bash
cloudflared tunnel list --name model-loader-quantforge --output json
```

Expected: exactly one non-deleted Tunnel with active connections. Resolve the public hostname and confirm it is proxied through Cloudflare; do not infer Tunnel health solely from DNS resolution.

- [ ] **Step 4: Confirm repository contains no secret material**

Search tracked files for:

- the actual API-key value;
- `cert.pem`;
- the Tunnel credential UUID JSON filename;
- copied `gateway.env` content.

Expected: no matches. Also run `git status --short`; expected: clean working tree after the planned commits.

- [ ] **Step 5: Record final deployment evidence**

Report only:

- public hostname;
- Tunnel name and UUID (not Tunnel secret);
- service active/enabled states;
- HTTP status matrix;
- listener addresses;
- streaming PASS or documented SKIP;
- commit hashes.

Never include the API key, Authorization header, `cert.pem`, or Tunnel credential contents.
