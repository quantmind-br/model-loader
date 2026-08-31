# External API through Cloudflare — Design

## Objective

Expose the existing model-loader HTTP API at `https://model-loader.quantforge.com.br` without opening an inbound port or changing the proxy's loopback trust boundary. Every request arriving through the public hostname must present the configured API key as `Authorization: Bearer <key>`.

## Confirmed requirements

- Public hostname: `model-loader.quantforge.com.br`.
- Public scope: the complete model-loader API, including `/v1/*`, `/v1beta/*`, `/_status`, and `/_admin/*`.
- External authentication: `Authorization: Bearer $MODELLOADER_API_KEY`.
- Local compatibility: direct access to `127.0.0.1:4321` remains unauthenticated.
- Cloudflare provisioning: named Cloudflare Tunnel authorized through interactive `cloudflared tunnel login`.
- Secrets must not be committed or printed in logs.

## Current state

- model-loader listens on `127.0.0.1:4321`.
- The application deliberately has no built-in authentication and documentation warns against binding it publicly.
- `cloudflared` and Caddy are installed.
- No Cloudflare origin certificate or named Tunnel configuration exists on this machine.
- `MODELLOADER_API_KEY` is not available in the current process or user systemd environment. Provisioning the secret is therefore an explicit implementation prerequisite.

## Architecture

```text
External client
  │ HTTPS
  │ Authorization: Bearer <MODELLOADER_API_KEY>
  ▼
Cloudflare edge and proxied DNS
  │ outbound Cloudflare Tunnel
  ▼
cloudflared user service
  │ http://127.0.0.1:4322
  ▼
Caddy authentication gateway
  ├─ invalid/missing Bearer → 401 JSON
  └─ valid Bearer
       │ strips Authorization
       ▼
model-loader proxy
  │ http://127.0.0.1:4321
  ▼
selected inference backend
```

Both origin listeners remain loopback-only. The workstation requires no public listener, router forwarding, or firewall exception. Cloudflare terminates public TLS; traffic from `cloudflared` to Caddy stays on loopback.

## Components

### model-loader

No application-code change is required. Its existing bind address remains `127.0.0.1`, preserving TUI, benchmark, supervisor, and local client behavior.

### Caddy authentication gateway

A dedicated Caddy instance listens with plain HTTP on `127.0.0.1:4322`; automatic HTTPS and the Caddy admin API are disabled. Public TLS terminates at Cloudflare, and the Tunnel-to-gateway hop never leaves loopback.

Request handling:

1. Match the complete `Authorization` header against `Bearer {$MODELLOADER_API_KEY}`.
2. For a match, remove `Authorization` before proxying to `127.0.0.1:4321`.
3. Otherwise return HTTP `401`, `WWW-Authenticate: Bearer`, and an OpenAI-shaped JSON error.
4. Do not log request headers or the API key.

The gateway intentionally applies one policy to every path. Because the approved scope includes administrative routes, a valid key can remotely load and unload models.

### API-key storage

The API key is stored outside the repository in a user-owned file with mode `0600`. A user systemd service loads it into Caddy's environment through an `EnvironmentFile`; the Caddyfile references the environment placeholder rather than containing the literal secret.

The setup must fail before starting either external service when:

- the variable is absent or empty;
- the secret file permissions are broader than `0600`;
- the key contains unsupported newline or NUL characters.

The implementation must not echo the key, embed it in command-line arguments, commit it, or place it in a world-readable unit/Caddy configuration.

### Cloudflare Tunnel

Provision a named Tunnel dedicated to this API. Its ingress configuration contains exactly:

1. `model-loader.quantforge.com.br` → `http://127.0.0.1:4322`;
2. a terminal `http_status:404` catch-all.

`cloudflared tunnel route dns` creates the proxied DNS route. Tunnel credentials remain in the user's Cloudflare configuration directory and are never added to the repository.

### User systemd services

Two user services provide restart and boot behavior:

- `model-loader-api-gateway.service`: Caddy gateway; starts only after network availability and restarts on failure.
- `model-loader-cloudflared.service`: named Tunnel; ordered after and requires the gateway service; restarts on failure.

The Cloudflare service must not depend on the model-loader process itself. If model-loader is down, Caddy returns a gateway error while the Tunnel stays established; recovery needs no Tunnel restart.

## Data and error flow

### Authorized request

1. Client connects to Cloudflare over HTTPS.
2. Cloudflare routes the hostname through the named Tunnel.
3. Caddy validates the exact Bearer header.
4. Caddy strips the external credential.
5. model-loader handles the original method, path, query, body, and streaming response.

Caddy must preserve streaming responses and must not buffer SSE or chunked inference output.

### Missing or incorrect key

Caddy returns `401` without contacting model-loader. Response:

```json
{
  "error": {
    "message": "invalid API key",
    "type": "authentication_error",
    "code": "invalid_api_key"
  }
}
```

The response includes `WWW-Authenticate: Bearer` and does not disclose whether the key was absent or merely incorrect.

### Origin unavailable

If Caddy cannot reach `127.0.0.1:4321`, it returns its normal `502` response. Authentication still occurs first, so an unauthenticated caller cannot use origin health as an oracle.

## Security properties and accepted risks

- No public origin port exists; all Cloudflare connectivity is outbound.
- The public hostname cannot bypass Caddy because Tunnel ingress targets only port `4322`.
- The model-loader listener remains inaccessible beyond loopback.
- The external Bearer is removed before reaching model-loader and inference backends.
- Cloudflare Access service tokens are not used; standard OpenAI-compatible clients only need one Bearer credential.
- A leaked key grants the full approved API scope, including `/_admin/load` and `/_admin/unload`. Rotation replaces the local secret and restarts only the Caddy gateway.
- Cloudflare account compromise and local-user compromise remain trusted-boundary failures.

## Verification

Implementation is complete only after all checks below pass against the actual services:

1. `127.0.0.1:4321/v1/models` succeeds without authentication.
2. `127.0.0.1:4322/v1/models` without a key returns `401`.
3. The gateway with an incorrect Bearer returns `401`.
4. The gateway with the configured Bearer reaches `/v1/models`.
5. The public hostname without a key returns `401`.
6. The public hostname with an incorrect Bearer returns `401`.
7. The public hostname with the configured Bearer reaches `/v1/models`.
8. The public hostname with the configured Bearer reaches `/_status`.
9. An authenticated administrative request reaches `/_admin/load` or an equivalent non-destructive validation of that route's authentication boundary.
10. A streaming inference response traverses Cloudflare and Caddy without buffering or truncation when an available profile permits the smoke test.
11. Listener inspection confirms model-loader and Caddy are bound only to loopback.
12. Service inspection confirms both user systemd units are enabled and running.
13. Files containing the API key and Tunnel credentials are not tracked by Git and are not group/world-readable.

## Rollback

1. Disable and stop `model-loader-cloudflared.service`.
2. Disable and stop `model-loader-api-gateway.service`.
3. Delete the Cloudflare DNS route and named Tunnel if the exposure is being permanently removed.
4. Remove the local gateway configuration and secret files.

Rollback does not modify model-loader configuration or interrupt direct local access on `127.0.0.1:4321`.

## Non-goals

- Adding authentication to model-loader itself.
- Exposing the backend instance ports.
- Adding per-route keys, user accounts, quotas, or rate limiting.
- Adding Cloudflare Access browser login or service-token headers.
- Changing model-loader's API schemas or request semantics.
