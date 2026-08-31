# Changelog

All notable user-facing changes are documented here. This project follows
[Semantic Versioning](https://semver.org/) before and after the `1.0.0` release.

## [Unreleased]

### Added

- Reproducible Cloudflare Tunnel deployment for an externally authenticated,
  loopback-origin model-loader API. The Tunnel and Caddy gateway follow the
  local proxy listener on `127.0.0.1:4321` instead of remaining up at login.

## [0.1.0] - 2026-08-02

### Added

- Five-tab terminal UI and matching headless CLI for profiles, processes,
  models, backends, and benchmarks.
- Catalog support for nine local inference backend kinds.
- OpenAI-compatible proxy with Anthropic Messages, OpenAI Responses, and Gemini
  translation plus profile-driven hot swapping.
- Process recovery, health monitoring, GPU metrics, Hugging Face downloads, and
  twelve benchmark modes.
- Public contributor, security, governance, CI, and release documentation.

[Unreleased]: https://github.com/quantmind-br/model-loader/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/quantmind-br/model-loader/releases/tag/v0.1.0
