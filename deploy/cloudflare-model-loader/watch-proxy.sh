#!/usr/bin/env bash
# Start or stop the external-API gateway according to whether the
# model-loader proxy is listening on 127.0.0.1:4321. Intended as a
# systemd oneshot triggered by a 1s timer; never prints secrets.
set -euo pipefail

readonly PROXY_HOST='127.0.0.1'
readonly PROXY_PORT='4321'
readonly GATEWAY_UNIT='model-loader-api-gateway.service'

proxy_up() {
  # Probe the loopback listener without ss(8): systemd user units may
  # lack AF_NETLINK, while bash /dev/tcp only needs AF_INET.
  bash -c "echo >/dev/tcp/${PROXY_HOST}/${PROXY_PORT}" >/dev/null 2>&1
}

gateway_active() {
  systemctl --user is-active --quiet "$GATEWAY_UNIT"
}

if proxy_up; then
  if ! gateway_active; then
    systemctl --user start "$GATEWAY_UNIT"
  fi
  exit 0
fi

if gateway_active; then
  systemctl --user stop "$GATEWAY_UNIT"
fi

