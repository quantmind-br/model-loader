from __future__ import annotations

import argparse
import contextlib
import importlib.util
import io
import json
import os
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest import mock

SKILL_DIR = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("model_loader_agents_sync", SKILL_DIR / "sync.py")
sync = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(sync)


def ns(**values):
    """Build an argparse.Namespace covering every path/provider-id/mode field
    sync.py's helpers and target syncers read, with realistic defaults so a
    test only needs to override what it cares about."""
    defaults = {
        # paths / URLs
        "config": None,
        "profiles_dir": None,
        "pi_models_json": None,
        "feynman_models_json": None,
        "omp_models_yml": None,
        "opencode_config_json": None,
        "forge_provider_json": None,
        "hermes_config_yml": None,
        "crush_config_json": None,
        "droid_settings_json": None,
        "zeroclaw_config_toml": None,
        "proxy_url": None,
        # provider ids
        "pi_provider": sync.PI_PROVIDER_DEFAULT,
        "feynman_provider": sync.FEYNMAN_PROVIDER_DEFAULT,
        "omp_provider": sync.OMP_PROVIDER_DEFAULT,
        "opencode_provider": sync.OPENCODE_PROVIDER_DEFAULT,
        "forge_provider": sync.FORGE_PROVIDER_DEFAULT,
        "hermes_provider": sync.HERMES_PROVIDER_DEFAULT,
        "crush_provider": sync.CRUSH_PROVIDER_DEFAULT,
        "zeroclaw_bin": sync.ZEROCLAW_BIN_DEFAULT,
        # mode
        "apply": False,
        "update_provider_config": False,
        "default_max_tokens": sync.DEFAULT_MAX_TOKENS,
        "min_context": sync.MIN_CONTEXT_DEFAULT,
        "include_embeddings": False,
        "seed_overrides": False,
        "overrides": None,
        "overrides_out": None,
        "json": False,
        "quiet": False,
    }
    defaults.update(values)
    return argparse.Namespace(**defaults)


def make_profile_file(dirpath: Path, pid: str, *, name: str | None = None,
                       ctx: int = 200_000, vision: bool = False,
                       reasoning: bool = False, tags: list | None = None) -> None:
    """Write a minimal model-loader profile JSON fixture under dirpath."""
    args: dict = {"ctx-size": ctx}
    if vision:
        args["mmproj"] = "/models/mmproj.gguf"
    if reasoning:
        args["reasoning-parser"] = "deepseek_r1"
    data = {"id": pid, "name": name or pid, "tags": tags or [], "args": args}
    (dirpath / f"{pid}.json").write_text(json.dumps(data), encoding="utf-8")


def load_parts(profiles_dir: str, min_context: int | None = None) -> dict:
    profiles = sync.enumerate_profiles(profiles_dir)
    threshold = sync.MIN_CONTEXT_DEFAULT if min_context is None else min_context
    return sync.select(profiles, threshold, include_embeddings=False)


class PathResolutionTests(unittest.TestCase):
    def test_target_names_cover_all_agents(self):
        self.assertEqual(
            sync.TARGET_NAMES,
            ("pi", "feynman", "omp", "opencode", "crush", "forge", "hermes",
             "droid", "zeroclaw"),
        )

    def test_crush_path_precedence(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            with mock.patch.dict(os.environ, {"HOME": td, "CRUSH_CONFIG_JSON": str(home / "env-crush.json")}, clear=True):
                self.assertEqual(sync.resolve_crush_config_json(ns(crush_config_json="~/explicit.json")), str(home / "explicit.json"))
                self.assertEqual(sync.resolve_crush_config_json(ns()), str(home / "env-crush.json"))
            with mock.patch.dict(os.environ, {"HOME": td, "CRUSH_GLOBAL_CONFIG": str(home / "gcfg")}, clear=True):
                self.assertEqual(sync.resolve_crush_config_json(ns()), str(home / "gcfg" / "crush.json"))
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_crush_config_json(ns()), str(home / ".config" / "crush" / "crush.json"))

    def test_droid_path_precedence(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            with mock.patch.dict(os.environ, {"HOME": td, "DROID_SETTINGS_JSON": str(home / "env-droid.json")}, clear=True):
                self.assertEqual(sync.resolve_droid_settings_json(ns(droid_settings_json="~/explicit.json")), str(home / "explicit.json"))
                self.assertEqual(sync.resolve_droid_settings_json(ns()), str(home / "env-droid.json"))
            with mock.patch.dict(os.environ, {"HOME": td, "FACTORY_HOME": str(home / "factory")}, clear=True):
                self.assertEqual(sync.resolve_droid_settings_json(ns()), str(home / "factory" / "settings.json"))
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_droid_settings_json(ns()), str(home / ".factory" / "settings.json"))

    def test_zeroclaw_path_precedence(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            with mock.patch.dict(os.environ, {"HOME": td, "ZEROCLAW_CONFIG_TOML": str(home / "env-zc.toml")}, clear=True):
                self.assertEqual(sync.resolve_zeroclaw_config_toml(ns(zeroclaw_config_toml="~/explicit.toml")), str(home / "explicit.toml"))
                self.assertEqual(sync.resolve_zeroclaw_config_toml(ns()), str(home / "env-zc.toml"))
            with mock.patch.dict(os.environ, {"HOME": td, "ZEROCLAW_CONFIG_DIR": str(home / "zcdir")}, clear=True):
                self.assertEqual(sync.resolve_zeroclaw_config_toml(ns()), str(home / "zcdir" / "config.toml"))
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_zeroclaw_config_toml(ns()), str(home / ".zeroclaw" / "config.toml"))

    def test_forge_path_precedence(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            legacy = home / "forge"
            legacy.mkdir()
            with mock.patch.dict(os.environ, {"HOME": td, "FORGE_CONFIG": str(home / "env-forge")}, clear=False):
                self.assertEqual(sync.resolve_forge_provider_json(ns(forge_provider_json="~/explicit.json")), str(home / "explicit.json"))
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(home / "env-forge" / "provider.json"))
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(legacy / "provider.json"))
                legacy.rmdir()
                self.assertEqual(sync.resolve_forge_provider_json(ns()), str(home / ".forge" / "provider.json"))

    def test_hermes_home_precedes_default(self):
        with tempfile.TemporaryDirectory() as td:
            with mock.patch.dict(os.environ, {"HOME": td, "HERMES_HOME": str(Path(td) / "state")}, clear=True):
                self.assertEqual(sync.resolve_hermes_config_yml(ns()), str(Path(td) / "state" / "config.yaml"))

    def test_hermes_default_falls_back_to_dot_hermes(self):
        with tempfile.TemporaryDirectory() as td:
            with mock.patch.dict(os.environ, {"HOME": td}, clear=True):
                self.assertEqual(sync.resolve_hermes_config_yml(ns()), str(Path(td) / ".hermes" / "config.yaml"))

    def test_all_skips_missing_but_explicit_rejects_it(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            self.assertFalse(sync.target_available("/missing/config", explicit=False, target="forge"))
        self.assertIn("forge", err.getvalue())
        with self.assertRaises(SystemExit) as raised:
            sync.target_available("/missing/config", explicit=True, target="forge")
        self.assertEqual(raised.exception.code, 2)

    def test_requested_targets(self):
        self.assertEqual(sync.requested_targets("all"), sync.TARGET_NAMES)
        self.assertEqual(sync.requested_targets("forge"), ("forge",))


class PiFamilyTests(unittest.TestCase):
    def test_feynman_uses_pi_schema_and_preserves_unrelated_data(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-me-256k", ctx=262144, vision=True, reasoning=True)
            target_path = Path(td, "feynman-models.json")
            target_path.write_text(json.dumps({
                "providers": {
                    "other-provider": {"baseUrl": "https://example.com", "models": [{"id": "z"}]},
                    "model-loader": {
                        "baseUrl": "http://custom:9/v1",
                        "apiKey": "custom-key",
                        "api": "openai-completions",
                        "compat": {"custom": True},
                        "models": [
                            {"id": "stale-model", "name": "Stale", "contextWindow": 999, "input": ["text"], "reasoning": False},
                        ],
                    },
                },
                "unrelated": {"keep": True},
            }))
            parts = load_parts(str(profiles_dir))
            paths = {"feynman_models_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            args = ns(apply=True)
            result = sync.sync_feynman(parts, paths, {}, args)
            self.assertEqual(result["target"], "feynman")
            self.assertTrue(result["provider_existed"])
            self.assertEqual(result["changes"]["added"], ["keep-me-256k"])
            self.assertEqual(result["changes"]["removed"], ["stale-model"])
            written = json.loads(target_path.read_text())
            self.assertEqual(written["unrelated"], {"keep": True})
            self.assertEqual(written["providers"]["other-provider"], {"baseUrl": "https://example.com", "models": [{"id": "z"}]})
            ml = written["providers"]["model-loader"]
            self.assertEqual(ml["baseUrl"], "http://custom:9/v1")
            self.assertEqual(ml["apiKey"], "custom-key")
            self.assertEqual(ml["compat"], {"custom": True})
            ids = [m["id"] for m in ml["models"]]
            self.assertEqual(ids, ["keep-me-256k"])
            entry = ml["models"][0]
            self.assertEqual(entry["contextWindow"], 262144)
            self.assertEqual(entry["input"], ["text", "image"])
            self.assertTrue(entry["reasoning"])
            backup = Path(str(target_path) + ".bak")
            self.assertTrue(backup.exists())

    def test_pi_wrapper_keeps_existing_behavior(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "steady-200k", ctx=204800, name="Steady")
            target_path = Path(td, "pi-models.json")
            target_path.write_text(json.dumps({
                "providers": {
                    "model-loader": {
                        "baseUrl": sync.PROXY_URL_DEFAULT, "api": "openai-completions",
                        "apiKey": "model-loader", "compat": dict(sync.PI_COMPAT_DEFAULT),
                        "models": [{"id": "steady-200k", "name": "Steady", "contextWindow": 204800,
                                     "input": ["text"], "reasoning": False, "maxTokens": 16000}],
                    },
                },
            }))
            parts = load_parts(str(profiles_dir))
            paths = {"pi_models_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            args = ns(apply=True)
            result = sync.sync_pi(parts, paths, {}, args)
            self.assertEqual(result["target"], "pi")
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["steady-200k"]})
            written = json.loads(target_path.read_text())
            entry = written["providers"]["model-loader"]["models"][0]
            self.assertEqual(entry["maxTokens"], 16000)
            self.assertEqual(written["providers"]["model-loader"]["apiKey"], "model-loader")

    def test_non_object_root_is_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            target_path = Path(td, "pi-models.json")
            target_path.write_text("[]")
            parts = load_parts(str(profiles_dir))
            paths = {"pi_models_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            args = ns(apply=True)
            with self.assertRaises(SystemExit) as raised:
                sync.sync_pi(parts, paths, {}, args)
            self.assertEqual(raised.exception.code, 2)
            self.assertEqual(target_path.read_text(), "[]")


class OmpTests(unittest.TestCase):
    OMP_FIXTURE = """\
# top-level omp config comment
providers:
  other-vendor:
    baseUrl: https://example.com/v1
    models: []
  llama.cpp:
    baseUrl: http://localhost:4321/v1
    api: openai-completions
    auth: none
    models:
      # a hand-placed family comment
      - id: stale-model
        name: Stale
        reasoning: false
        input: [text]
        contextWindow: 999
        maxTokens: 4096
"""

    def test_preserves_comments_unrelated_and_updates_in_place(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144, reasoning=True)
            target_path = Path(td, "models.yml")
            target_path.write_text(self.OMP_FIXTURE, encoding="utf-8")
            parts = load_parts(str(profiles_dir))
            paths = {"omp_models_yml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            args = ns(apply=True)
            result = sync.sync_omp(parts, paths, {}, args)
            text = target_path.read_text(encoding="utf-8")
            self.assertIn("# top-level omp config comment", text)
            yml = sync._yaml()
            data = yml.load(text)
            self.assertEqual(data["providers"]["other-vendor"]["baseUrl"], "https://example.com/v1")
            llama = data["providers"]["llama.cpp"]
            self.assertEqual(llama["baseUrl"], "http://localhost:4321/v1")
            self.assertEqual(llama["auth"], "none")
            ids = [m["id"] for m in llama["models"]]
            self.assertEqual(ids, ["keep-256k"])
            entry = llama["models"][0]
            self.assertEqual(entry["contextWindow"], 262144)
            self.assertTrue(entry["reasoning"])
            self.assertEqual(result["changes"]["removed"], ["stale-model"])
            self.assertEqual(result["changes"]["added"], ["keep-256k"])

    def test_idempotent_second_dry_run(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608)
            target_path = Path(td, "models.yml")
            target_path.write_text("providers: {}\n", encoding="utf-8")
            parts = load_parts(str(profiles_dir))
            paths = {"omp_models_yml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_omp(parts, paths, {}, ns(apply=True))
            result = sync.sync_omp(parts, paths, {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})


class OpenCodeTests(unittest.TestCase):
    def _paths(self, target_path):
        return {"opencode_config_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_writes_keyed_models_and_preserves_config(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "vision-model-256k", ctx=262144, vision=True)
            target_path = Path(td, "opencode.json")
            target_path.write_text(json.dumps({
                "$schema": "https://opencode.ai/config.json",
                "plugin": ["some-plugin"],
                "mcp": {"srv": {"type": "local", "command": ["x"]}},
                "provider": {
                    "other-vendor": {"npm": "@ai-sdk/anthropic", "models": {"claude": {"name": "Claude"}}},
                    "model-loader": {
                        "npm": "@ai-sdk/openai-compatible",
                        "name": "Model Loader",
                        "options": {"baseURL": "http://custom-host:1/v1", "apiKey": "model-loader", "timeout": 5000},
                        "models": {
                            "stale-model": {"name": "Stale", "limit": {"context": 1000, "output": 2000}, "modalities": {"input": ["text"], "output": ["text"]}, "reasoning": False},
                        },
                    },
                },
            }))
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            args = ns(apply=True)
            result = sync.sync_opencode(parts, paths, {}, args)
            written = json.loads(target_path.read_text())
            self.assertEqual(written["$schema"], "https://opencode.ai/config.json")
            self.assertEqual(written["plugin"], ["some-plugin"])
            self.assertEqual(written["mcp"], {"srv": {"type": "local", "command": ["x"]}})
            self.assertEqual(written["provider"]["other-vendor"], {"npm": "@ai-sdk/anthropic", "models": {"claude": {"name": "Claude"}}})
            ml = written["provider"]["model-loader"]
            self.assertEqual(ml["options"]["baseURL"], "http://custom-host:1/v1")
            self.assertEqual(ml["options"]["timeout"], 5000)
            self.assertNotIn("stale-model", ml["models"])
            entry = ml["models"]["vision-model-256k"]
            self.assertEqual(entry["limit"]["context"], 262144)
            self.assertEqual(entry["modalities"]["input"], ["text", "image"])
            self.assertEqual(entry["modalities"]["output"], ["text"])
            self.assertIs(entry["reasoning"], False)
            self.assertNotIn("variants", entry)
            self.assertEqual(result["changes"]["removed"], ["stale-model"])
            self.assertEqual(result["changes"]["added"], ["vision-model-256k"])

    def test_new_provider_uses_inline_placeholder_key(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-192k", ctx=196608)
            target_path = Path(td, "opencode.json")
            target_path.write_text(json.dumps({"provider": {}}))
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_opencode(parts, paths, {}, ns(apply=True))
            written = json.loads(target_path.read_text())
            ml = written["provider"]["model-loader"]
            self.assertEqual(ml["npm"], "@ai-sdk/openai-compatible")
            self.assertEqual(ml["options"], {"baseURL": sync.PROXY_URL_DEFAULT, "apiKey": "model-loader"})
            self.assertNotIn("env", ml)

    def test_second_sync_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608, reasoning=True)
            target_path = Path(td, "opencode.json")
            target_path.write_text(json.dumps({"provider": {}}))
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_opencode(parts, paths, {}, ns(apply=True))
            result = sync.sync_opencode(parts, paths, {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})

    def test_non_object_root_is_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            target_path = Path(td, "opencode.json")
            target_path.write_text("[]")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            with self.assertRaises(SystemExit) as raised:
                sync.sync_opencode(parts, paths, {}, ns(apply=True))
            self.assertEqual(raised.exception.code, 2)
            self.assertEqual(target_path.read_text(), "[]")


class ForgeTests(unittest.TestCase):
    def _paths(self, target_path):
        return {"forge_provider_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_normalizes_chat_completions_url_once(self):
        cases = {
            "http://127.0.0.1:4321": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1/": "http://127.0.0.1:4321/v1/chat/completions",
            "http://127.0.0.1:4321/v1/chat/completions": "http://127.0.0.1:4321/v1/chat/completions",
        }
        for src, expected in cases.items():
            self.assertEqual(sync.forge_chat_completions_url(src), expected)

    def test_new_provider_is_no_auth(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-192k", ctx=196608)
            target_path = Path(td, "provider.json")
            target_path.write_text("[]")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_forge(parts, paths, {}, ns(apply=True))
            written = json.loads(target_path.read_text())
            ml = next(p for p in written if p["id"] == "model-loader")
            self.assertEqual(ml["auth_methods"], [])
            self.assertNotIn("api_key_vars", ml)
            self.assertEqual(ml["url"], "http://127.0.0.1:4321/v1/chat/completions")
            self.assertEqual(ml["response_type"], "OpenAI")
            self.assertEqual(ml["url_param_vars"], [])

    def test_existing_auth_and_unrelated_providers_survive(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144, reasoning=True, vision=True)
            target_path = Path(td, "provider.json")
            target_path.write_text(json.dumps([
                {"id": "other-vendor", "api_key_vars": "OTHER_KEY", "url_param_vars": [], "response_type": "OpenAI", "url": "https://x/v1/chat/completions", "auth_methods": ["api_key"], "models": []},
                {"id": "model-loader", "api_key_vars": "SOME_KEY", "url_param_vars": [], "response_type": "OpenAI", "url": "http://old:1/v1/chat/completions", "auth_methods": ["api_key"],
                 "models": [{"id": "stale-model", "name": "Stale", "description": "d", "context_length": 1, "tools_supported": True, "supports_parallel_tool_calls": True, "supports_reasoning": False, "input_modalities": ["text"]}]},
            ]))
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            result = sync.sync_forge(parts, paths, {}, ns(apply=True))
            written = json.loads(target_path.read_text())
            self.assertEqual(len(written), 2)
            other = next(p for p in written if p["id"] == "other-vendor")
            self.assertEqual(other["api_key_vars"], "OTHER_KEY")
            ml = next(p for p in written if p["id"] == "model-loader")
            self.assertEqual(ml["api_key_vars"], "SOME_KEY")
            self.assertEqual(ml["url"], "http://old:1/v1/chat/completions")
            ids = [m["id"] for m in ml["models"]]
            self.assertEqual(ids, ["keep-256k"])
            entry = ml["models"][0]
            self.assertEqual(entry["context_length"], 262144)
            self.assertTrue(entry["supports_reasoning"])
            self.assertEqual(entry["input_modalities"], ["text", "image"])
            self.assertTrue(entry["tools_supported"])
            self.assertTrue(entry["supports_parallel_tool_calls"])
            self.assertEqual(result["changes"]["removed"], ["stale-model"])

    def test_refresh_clears_stale_auth(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "provider.json")
            target_path.write_text(json.dumps([
                {"id": "model-loader", "api_key_vars": "SOME_KEY", "url_param_vars": [], "response_type": "OpenAI", "url": "http://old:1/v1/chat/completions", "auth_methods": ["api_key"], "models": []},
            ]))
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_forge(parts, paths, {}, ns(apply=True, update_provider_config=True))
            written = json.loads(target_path.read_text())
            ml = next(p for p in written if p["id"] == "model-loader")
            self.assertNotIn("api_key_vars", ml)
            self.assertEqual(ml["auth_methods"], [])
            self.assertEqual(ml["url"], "http://127.0.0.1:4321/v1/chat/completions")

    def test_non_array_root_is_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            target_path = Path(td, "provider.json")
            target_path.write_text("{}")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            with self.assertRaises(SystemExit) as raised:
                sync.sync_forge(parts, paths, {}, ns(apply=True))
            self.assertEqual(raised.exception.code, 2)
            self.assertEqual(target_path.read_text(), "{}")

    def test_second_sync_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608)
            target_path = Path(td, "provider.json")
            target_path.write_text("[]")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_forge(parts, paths, {}, ns(apply=True))
            result = sync.sync_forge(parts, paths, {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})


class HermesTests(unittest.TestCase):
    def _paths(self, target_path):
        return {"hermes_config_yml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_static_catalog_is_authoritative_and_preserves_config(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144, vision=True)
            target_path = Path(td, "config.yaml")
            target_path.write_text(
                "# top-level comment\n"
                "model:\n"
                "  default: some-model\n"
                "  provider: model-loader\n"
                "fallback_providers: [quantmind]\n"
                "providers:\n"
                "  # auth block comment\n"
                "  model-loader:\n"
                "    base_url: http://localhost:4321/v1\n"
                "    key_env: MODEL_LOADER_API_KEY\n"
                "    name: Model Loader\n"
                "    discover_models: true\n"
                "    models:\n"
                "      stale-model:\n"
                "        context_length: 1000\n"
                "        supports_vision: false\n"
                "  quantmind:\n"
                "    base_url: https://api.quantmind.com.br/v1\n"
                "    api_key: secret\n",
                encoding="utf-8",
            )
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            result = sync.sync_hermes(parts, paths, {}, ns(apply=True))
            text = target_path.read_text(encoding="utf-8")
            self.assertIn("# top-level comment", text)
            self.assertIn("# auth block comment", text)
            data = sync._yaml().load(text)
            self.assertEqual(data["model"], {"default": "some-model", "provider": "model-loader"})
            self.assertEqual(list(data["fallback_providers"]), ["quantmind"])
            ml = data["providers"]["model-loader"]
            self.assertEqual(ml["base_url"], "http://localhost:4321/v1")
            self.assertEqual(ml["key_env"], "MODEL_LOADER_API_KEY")
            self.assertIs(ml["discover_models"], False)
            self.assertNotIn("stale-model", ml["models"])
            entry = ml["models"]["keep-256k"]
            self.assertEqual(entry["context_length"], 262144)
            self.assertIs(entry["supports_vision"], True)
            self.assertEqual(data["providers"]["quantmind"]["api_key"], "secret")
            self.assertEqual(result["changes"]["removed"], ["stale-model"])

    def test_new_provider_has_no_required_key(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-192k", ctx=196608)
            target_path = Path(td, "config.yaml")
            target_path.write_text("providers: {}\n", encoding="utf-8")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_hermes(parts, paths, {}, ns(apply=True))
            data = sync._yaml().load(target_path.read_text())
            ml = data["providers"]["model-loader"]
            self.assertEqual(ml["base_url"], sync.PROXY_URL_DEFAULT)
            self.assertEqual(ml["name"], "Model Loader")
            self.assertIs(ml["discover_models"], False)
            self.assertNotIn("api_key", ml)
            self.assertNotIn("key_env", ml)

    def test_never_writes_provider_cache(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-192k", ctx=196608)
            target_path = Path(td, "config.yaml")
            target_path.write_text("providers: {}\n", encoding="utf-8")
            cache_path = Path(td, "provider_models_cache.json")
            cache_path.write_bytes(b'{"sentinel": true}')
            before = cache_path.read_bytes()
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_hermes(parts, paths, {}, ns(apply=True))
            self.assertEqual(cache_path.read_bytes(), before)

    def test_yaml_comments_survive(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-192k", ctx=196608)
            target_path = Path(td, "config.yaml")
            target_path.write_text(
                "# banner comment\nunrelated:\n  # nested comment\n  keep: true\nproviders: {}\n",
                encoding="utf-8",
            )
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_hermes(parts, paths, {}, ns(apply=True))
            text = target_path.read_text(encoding="utf-8")
            self.assertIn("# banner comment", text)
            self.assertIn("# nested comment", text)

    def test_non_mapping_root_is_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            target_path = Path(td, "config.yaml")
            target_path.write_text("- a\n- b\n", encoding="utf-8")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            with self.assertRaises(SystemExit) as raised:
                sync.sync_hermes(parts, paths, {}, ns(apply=True))
            self.assertEqual(raised.exception.code, 2)
            self.assertEqual(target_path.read_text(), "- a\n- b\n")

    def test_second_sync_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608, vision=True)
            target_path = Path(td, "config.yaml")
            target_path.write_text("providers: {}\n", encoding="utf-8")
            parts = load_parts(str(profiles_dir))
            paths = self._paths(target_path)
            sync.sync_hermes(parts, paths, {}, ns(apply=True))
            result = sync.sync_hermes(parts, paths, {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})

class CrushTests(unittest.TestCase):
    def _paths(self, target_path):
        return {"crush_config_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_new_provider_schema_required_fields(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "solo-256k", ctx=262144, reasoning=True, vision=True)
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({"providers": {}}))
            parts = load_parts(str(profiles_dir))
            sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True))
            data = json.loads(target_path.read_text())
            prov = data["providers"]["model-loader"]
            self.assertEqual(prov["type"], "openai-compat")
            self.assertEqual(prov["base_url"], "http://127.0.0.1:4321/v1")
            self.assertEqual(prov["api_key"], "model-loader")
            self.assertFalse(prov["discover_models"])
            m = prov["models"][0]
            for fld in sync.CRUSH_MANAGED_FIELDS:
                self.assertIn(fld, m)
            self.assertEqual(m["cost_per_1m_in"], 0)
            self.assertEqual(m["cost_per_1m_out"], 0)
            self.assertEqual(m["cost_per_1m_in_cached"], 0)
            self.assertEqual(m["cost_per_1m_out_cached"], 0)
            self.assertEqual(m["context_window"], 262144)
            self.assertEqual(m["default_max_tokens"], sync.DEFAULT_MAX_TOKENS)
            self.assertTrue(m["can_reason"])
            self.assertTrue(m["supports_attachments"])
            self.assertNotIn("reasoning_levels", m)
            self.assertNotIn("default_reasoning_effort", m)
            self.assertNotIn("options", m)

    def test_preserves_top_level_other_providers_and_extra_fields(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({
                "$schema": "https://charm.land/crush.json",
                "mcp": {"srv": {}},
                "providers": {
                    "other-vendor": {"id": "other-vendor", "type": "openai-compat",
                                     "base_url": "https://x/v1", "api_key": "$OTHER",
                                     "models": [{"id": "z", "name": "Z", "cost_per_1m_in": 1,
                                                 "cost_per_1m_out": 1, "cost_per_1m_in_cached": 0,
                                                 "cost_per_1m_out_cached": 0, "context_window": 1,
                                                 "default_max_tokens": 1, "can_reason": False,
                                                 "supports_attachments": False}]},
                    "model-loader": {"id": "model-loader", "name": "Model Loader",
                                     "type": "openai-compat", "base_url": "http://old/v1",
                                     "api_key": "$MINE", "extra_headers": {"X": "1"},
                                     "discover_models": False,
                                     "models": [{"id": "stale", "name": "Stale", "cost_per_1m_in": 0,
                                                 "cost_per_1m_out": 0, "cost_per_1m_in_cached": 0,
                                                 "cost_per_1m_out_cached": 0, "context_window": 9,
                                                 "default_max_tokens": 9, "can_reason": False,
                                                 "supports_attachments": False}]},
                },
            }))
            parts = load_parts(str(profiles_dir))
            result = sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True))
            data = json.loads(target_path.read_text())
            self.assertEqual(data["$schema"], "https://charm.land/crush.json")
            self.assertIn("srv", data["mcp"])
            self.assertEqual(data["providers"]["other-vendor"]["api_key"], "$OTHER")
            ml = data["providers"]["model-loader"]
            self.assertEqual(ml["api_key"], "$MINE")        # connection preserved
            self.assertEqual(ml["base_url"], "http://old/v1")
            self.assertEqual(ml["extra_headers"], {"X": "1"})
            self.assertEqual([m["id"] for m in ml["models"]], ["keep-256k"])
            self.assertEqual(result["changes"]["removed"], ["stale"])

    def test_update_provider_config_resets_connection(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({"providers": {"model-loader": {
                "id": "model-loader", "type": "openai-compat", "base_url": "http://old/v1",
                "api_key": "$MINE", "discover_models": True, "models": []}}}))
            parts = load_parts(str(profiles_dir))
            sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True, update_provider_config=True))
            ml = json.loads(target_path.read_text())["providers"]["model-loader"]
            self.assertEqual(ml["base_url"], "http://127.0.0.1:4321/v1")
            self.assertEqual(ml["api_key"], "model-loader")
            self.assertFalse(ml["discover_models"])

    def test_discover_models_forced_false_even_when_true(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({"providers": {"model-loader": {
                "id": "model-loader", "type": "openai-compat", "base_url": "http://127.0.0.1:4321/v1",
                "api_key": "model-loader", "discover_models": True, "models": []}}}))
            parts = load_parts(str(profiles_dir))
            sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True))
            ml = json.loads(target_path.read_text())["providers"]["model-loader"]
            self.assertFalse(ml["discover_models"])

    def test_malformed_roots_rejected_without_write(self):
        for payload in ("[]", json.dumps({"providers": []}),
                        json.dumps({"providers": {"model-loader": []}}),
                        json.dumps({"providers": {"model-loader": {"models": {}}}})):
            with tempfile.TemporaryDirectory() as td:
                profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
                make_profile_file(profiles_dir, "keep-256k", ctx=262144)
                target_path = Path(td, "crush.json")
                target_path.write_text(payload)
                parts = load_parts(str(profiles_dir))
                with self.assertRaises(SystemExit) as raised:
                    sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True))
                self.assertEqual(raised.exception.code, 2)
                self.assertEqual(target_path.read_text(), payload)

    def test_seed_overrides_translate_native_fields(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({"providers": {"model-loader": {"models": [
                {"id": "m1", "name": "M One", "cost_per_1m_in": 0, "cost_per_1m_out": 0,
                 "cost_per_1m_in_cached": 0, "cost_per_1m_out_cached": 0, "context_window": 200000,
                 "default_max_tokens": 8192, "can_reason": True, "supports_attachments": True}]}}}))
            out_path = Path(td, "seed.json")
            sync.sync_crush({"passed": []}, self._paths(target_path), {},
                            ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = json.loads(out_path.read_text())["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "reasoning": True,
                                          "input": ["text", "image"], "maxTokens": 8192})

    def test_second_dry_run_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608)
            target_path = Path(td, "crush.json")
            target_path.write_text(json.dumps({"providers": {}}))
            parts = load_parts(str(profiles_dir))
            sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=True))
            result = sync.sync_crush(parts, self._paths(target_path), {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})


class DroidTests(unittest.TestCase):
    def _paths(self, target_path):
        return {"droid_settings_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_exact_shape_prefix_placeholder_vision_reasoning(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "vis-256k", ctx=262144, vision=True, reasoning=True)
            make_profile_file(profiles_dir, "txt-256k", ctx=262144)
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({"customModels": []}))
            parts = load_parts(str(profiles_dir))
            sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True))
            models = json.loads(target_path.read_text())["customModels"]
            by_model = {m["model"]: m for m in models}
            vis = by_model["vis-256k"]
            self.assertEqual(vis["displayName"], "Model Loader · vis-256k")
            self.assertEqual(vis["baseUrl"], "http://127.0.0.1:4321/v1")
            self.assertEqual(vis["apiKey"], "model-loader")
            self.assertEqual(vis["provider"], "generic-chat-completion-api")
            self.assertEqual(vis["maxContextLimit"], 262144)
            self.assertEqual(vis["maxOutputTokens"], sync.DEFAULT_MAX_TOKENS)
            self.assertNotIn("noImageSupport", vis)
            self.assertEqual(vis["reasoningEffort"], "high")
            txt = by_model["txt-256k"]
            self.assertTrue(txt["noImageSupport"])
            self.assertNotIn("reasoningEffort", txt)

    def test_preserves_unmanaged_and_existing_api_key(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({
                "trustedFolders": {"/home": {}},
                "customModels": [
                    {"model": "user-model", "id": "custom:User-0", "displayName": "My Own",
                     "baseUrl": "https://x/v1", "apiKey": "sk-secret", "provider": "generic-chat-completion-api"},
                    {"model": "keep-256k", "displayName": "Model Loader · keep-256k",
                     "baseUrl": "http://127.0.0.1:4321/v1", "apiKey": "sk-local-existing",
                     "provider": "generic-chat-completion-api", "maxContextLimit": 262144,
                     "maxOutputTokens": 4096},
                    {"model": "stale", "displayName": "Model Loader · stale",
                     "baseUrl": "http://127.0.0.1:4321/v1", "apiKey": "model-loader",
                     "provider": "generic-chat-completion-api", "maxContextLimit": 1, "maxOutputTokens": 1},
                ],
            }))
            parts = load_parts(str(profiles_dir))
            result = sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True))
            data = json.loads(target_path.read_text())
            self.assertEqual(data["trustedFolders"], {"/home": {}})
            models = data["customModels"]
            self.assertEqual(models[0]["apiKey"], "sk-secret")       # unmanaged untouched, kept first
            self.assertEqual(models[0]["model"], "user-model")
            keep = next(m for m in models if m["model"] == "keep-256k")
            self.assertEqual(keep["apiKey"], "sk-local-existing")     # preserved existing key
            self.assertNotIn("stale", [m["model"] for m in models])
            self.assertEqual(result["changes"]["removed"], ["stale"])

    def test_update_provider_config_resets_key_and_baseurl(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({"customModels": [
                {"model": "keep-256k", "displayName": "Model Loader · keep-256k",
                 "baseUrl": "http://old/v1", "apiKey": "sk-old",
                 "provider": "generic-chat-completion-api", "maxContextLimit": 1, "maxOutputTokens": 1}]}))
            parts = load_parts(str(profiles_dir))
            sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True, update_provider_config=True))
            m = json.loads(target_path.read_text())["customModels"][0]
            self.assertEqual(m["apiKey"], "model-loader")
            self.assertEqual(m["baseUrl"], "http://127.0.0.1:4321/v1")

    def test_file_mode_is_0600_after_apply(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "keep-256k", ctx=262144)
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({"customModels": []}))
            parts = load_parts(str(profiles_dir))
            sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True))
            self.assertEqual(oct(os.stat(target_path).st_mode & 0o777), oct(0o600))

    def test_malformed_root_and_customModels_rejected(self):
        for payload in ("[]", json.dumps({"customModels": {}})):
            with tempfile.TemporaryDirectory() as td:
                profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
                make_profile_file(profiles_dir, "keep-256k", ctx=262144)
                target_path = Path(td, "settings.json")
                target_path.write_text(payload)
                parts = load_parts(str(profiles_dir))
                with self.assertRaises(SystemExit) as raised:
                    sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True))
                self.assertEqual(raised.exception.code, 2)
                self.assertEqual(target_path.read_text(), payload)

    def test_seed_overrides_translate_display_and_fields(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({"customModels": [
                {"model": "m1", "displayName": "Model Loader · M One", "baseUrl": "http://127.0.0.1:4321/v1",
                 "apiKey": "model-loader", "provider": "generic-chat-completion-api",
                 "maxContextLimit": 200000, "maxOutputTokens": 8192, "reasoningEffort": "high"},
                {"model": "unmanaged", "displayName": "Other"},
            ]}))
            out_path = Path(td, "seed.json")
            sync.sync_droid({"passed": []}, self._paths(target_path), {},
                            ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = json.loads(out_path.read_text())["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "maxTokens": 8192,
                                          "input": ["text", "image"], "reasoning": True})
            self.assertNotIn("unmanaged", seed)

    def test_second_dry_run_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608)
            target_path = Path(td, "settings.json")
            target_path.write_text(json.dumps({"customModels": []}))
            parts = load_parts(str(profiles_dir))
            sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=True))
            result = sync.sync_droid(parts, self._paths(target_path), {}, ns(apply=False))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})


class ZeroClawTests(unittest.TestCase):
    """A fake `zeroclaw` executable applies the emitted RFC 6902 JSON Patch to
    the target TOML so the adapter's CLI contract is exercised end to end
    without touching a real config or its encrypted secrets."""

    FAKE_ZC = r'''#!/usr/bin/env python3
# Faithful stand-in for `zeroclaw` 0.8.3 covering only what this adapter drives:
#   zeroclaw --config-dir <dir> config patch <file> --json
#   zeroclaw --config-dir <dir> agents delete <alias> --yes
# It enforces the real CLI's validation contract so the tests exercise the same
# rules the installed binary does:
#   * config patch accepts SCALAR-LEAF paths only; an object-valued add at a map
#     key or a whole-subtable remove is path_not_found (exit 1).
#   * an enabled agent requires a risk_profile resolving to a configured
#     [risk_profiles.<alias>]; else required_field_empty (exit 1).
#   * an agent's model_provider must resolve to an existing provider alias;
#     else dangling_reference (exit 1). Final-state validated (single patch).
#   * agents delete removes the whole [agents.<alias>] subtable (exit 1 if absent).
import sys, json, tomllib, os
argv = sys.argv[1:]
config_dir = "."
rest = []
i = 0
while i < len(argv):
    a = argv[i]
    if a == "--config-dir":
        config_dir = argv[i + 1]; i += 2; continue
    rest.append(a); i += 1
path = os.path.join(config_dir, "config.toml")
try:
    with open(path, "rb") as fh:
        root = tomllib.load(fh)
except FileNotFoundError:
    root = {}

def fail(code):
    print(json.dumps({"code": code})); sys.exit(1)

def split(p):
    return [seg.replace("~1", "/").replace("~0", "~") for seg in p.split("/")[1:]]

def dump(node, prefix=""):
    scalars, tables = {}, {}
    for k, v in node.items():
        (tables if isinstance(v, dict) else scalars)[k] = v
    out = ""
    if prefix and (scalars or not tables):
        out += "[" + prefix + "]\n"
    for k, v in scalars.items():
        out += k + " = " + json.dumps(v) + "\n"
    if prefix and scalars:
        out += "\n"
    for k, v in tables.items():
        out += dump(v, (prefix + "." + k) if prefix else k)
    return out

def save():
    with open(path, "w") as fh:
        fh.write(dump(root))

if rest[:2] == ["config", "patch"]:
    patch_file = next(a for a in rest[2:] if not a.startswith("-"))
    with open(patch_file) as fh:
        ops = json.load(fh)
    cap = os.environ.get("ML_ZC_CAPTURE")
    if cap:
        with open(cap, "a") as fh:
            fh.write(json.dumps(ops) + "\n")
    # Apply onto a deep copy; only commit if the final state validates.
    import copy
    work = copy.deepcopy(root)
    for op in ops:
        # scalar-leaf contract: object values are rejected outright
        if op["op"] in ("add", "replace") and isinstance(op.get("value"), dict):
            fail("path_not_found")
        segs = split(op["path"])
        node = work
        for s in segs[:-1]:
            node = node.setdefault(s, {})
        key = segs[-1]
        if op["op"] == "remove":
            if not isinstance(node.get(key), (str, int, float, bool)):
                # whole-subtable / missing leaf removal is rejected
                fail("path_not_found")
            node.pop(key, None)
        else:
            node[key] = op["value"]
    # final-state validation
    agents = work.get("agents", {})
    customs = work.get("providers", {}).get("models", {}).get("custom", {})
    rps = work.get("risk_profiles", {})
    for alias, a in agents.items():
        if not isinstance(a, dict):
            continue
        mp = a.get("model_provider")
        if isinstance(mp, str) and mp.startswith("custom."):
            if mp.split(".", 1)[1] not in customs:
                fail("dangling_reference")
        if a.get("enabled") is True:
            rp = a.get("risk_profile")
            if not (isinstance(rp, str) and rp and rp in rps):
                fail("required_field_empty")
    root.clear(); root.update(work); save()
    print(json.dumps({"results": [{"op": o["op"], "path": o["path"]} for o in ops]}))
    sys.exit(0)

if rest[:2] == ["agents", "delete"]:
    alias = rest[2]
    agents = root.get("agents", {})
    if alias not in agents:
        fail("not_configured")
    agents.pop(alias, None)
    save()
    print("deleted agents." + alias)
    sys.exit(0)

sys.stderr.write("fake-zeroclaw: unsupported command %r\n" % (rest,))
sys.exit(2)
'''

    def _fake_bin(self, td):
        p = Path(td, "zeroclaw")
        p.write_text(self.FAKE_ZC)
        os.chmod(p, 0o755)
        return str(p)

    def _paths(self, target_path):
        return {"zeroclaw_config_toml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}

    def test_slug_derivation(self):
        self.assertEqual(sync._zeroclaw_slug("Qwen3.6-27B_AWQ"), "qwen3_6_27b_awq")
        self.assertEqual(sync._zeroclaw_slug("--foo..bar--"), "foo_bar")

    def test_creates_provider_and_agent_per_profile(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144, reasoning=True)
            make_profile_file(profiles_dir, "plain-192k", ctx=196608)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            parts = load_parts(str(profiles_dir))
            result = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                        ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            custom = data["providers"]["models"]["custom"]
            agents = data["agents"]
            self.assertIn("model_loader_text_256k", custom)
            self.assertIn("model_loader_plain_192k", custom)
            p1 = custom["model_loader_text_256k"]
            self.assertEqual(p1["uri"], "http://127.0.0.1:4321/v1")
            self.assertEqual(p1["model"], "text-256k")
            self.assertEqual(p1["wire_api"], "chat_completions")
            self.assertEqual(p1["context_window"], 262144)
            self.assertEqual(p1["max_tokens"], sync.DEFAULT_MAX_TOKENS)
            self.assertTrue(p1["think"])
            self.assertNotIn("think", custom["model_loader_plain_192k"])
            self.assertEqual(agents["model_loader_text_256k"]["model_provider"], "custom.model_loader_text_256k")
            self.assertEqual(sorted(result["changes"]["added"]), ["plain-192k", "text-256k"])
            self.assertEqual(result["provider"], "model_loader_*")

    def test_preserves_secrets_extra_and_unmanaged(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.other]\n'
                'uri = "https://x/v1"\nmodel = "foo"\napi_key = "enc2:zzz"\n\n'
                '[providers.models.custom.model_loader_text_256k]\n'
                'uri = "http://old/v1"\nmodel = "text-256k"\nwire_api = "chat_completions"\n'
                'context_window = 1\nmax_tokens = 1\napi_key = "enc2:secret"\n\n'
                '[agents.other]\nmodel_provider = "custom.other"\n\n'
                '[agents.model_loader_text_256k]\nmodel_provider = "custom.model_loader_text_256k"\n'
                'risk_profile = "supervised"\n'
            )
            parts = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            custom = data["providers"]["models"]["custom"]
            self.assertEqual(custom["other"]["api_key"], "enc2:zzz")
            mine = custom["model_loader_text_256k"]
            self.assertEqual(mine["api_key"], "enc2:secret")       # secret preserved
            self.assertEqual(mine["uri"], "http://127.0.0.1:4321/v1")  # managed field recalculated
            self.assertEqual(mine["context_window"], 262144)
            self.assertEqual(data["agents"]["other"]["model_provider"], "custom.other")
            self.assertEqual(data["agents"]["model_loader_text_256k"]["risk_profile"], "supervised")

    def test_stale_pair_removed(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_gone_256k]\n'
                'uri = "http://127.0.0.1:4321/v1"\nmodel = "gone-256k"\n'
                'wire_api = "chat_completions"\ncontext_window = 262144\nmax_tokens = 32768\n\n'
                '[agents.model_loader_gone_256k]\nmodel_provider = "custom.model_loader_gone_256k"\n'
            )
            parts = load_parts(str(profiles_dir))
            result = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                        ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            custom = data["providers"]["models"]["custom"]
            # Provider cannot be dropped (real CLI can't remove a subtable): it
            # decommissions to inert residue — every adapter-owned leaf scrubbed
            # (api_key/extras would remain), so _zc_has_owned is false.
            self.assertFalse(sync._zc_has_owned(custom.get("model_loader_gone_256k", {})))
            # Agent is fully deleted via `zeroclaw agents delete`.
            self.assertNotIn("model_loader_gone_256k", data.get("agents", {}))
            self.assertIn("model_loader_text_256k", custom)
            self.assertEqual(result["changes"]["removed"], ["gone-256k"])

    def test_stale_partial_provider_scrubbed_preserves_key(self):
        # A partial-failure residue: owned leaf (uri) + api_key, no model. It is
        # not a live model, must be scrubbed of owned leaves, keep api_key, and
        # be a no-op on the second run.
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_partial_256k]\n'
                'uri = "http://127.0.0.1:4321/v1"\napi_key = "enc2:keep"\n'
            )
            parts = load_parts(str(profiles_dir))
            fake = self._fake_bin(td)
            r1 = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                    ns(apply=True, zeroclaw_bin=fake))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            resid = data["providers"]["models"]["custom"]["model_loader_partial_256k"]
            self.assertFalse(sync._zc_has_owned(resid))       # owned leaves scrubbed
            self.assertEqual(resid["api_key"], "enc2:keep")   # extra preserved
            # partial residue has no model id, so it is not a reported removal
            self.assertEqual(r1["changes"]["removed"], [])
            # Second run as APPLY: no ops -> wrote is False (dry-run's wrote is
            # always False, so an apply is the real idempotency proof).
            r2 = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                    ns(apply=True, zeroclaw_bin=fake))
            self.assertFalse(r2["wrote"])
            self.assertEqual(r2["orphan_agent_removals"], [])
            self.assertEqual(r2["changes"], {"added": [], "removed": [],
                                             "updated": [], "unchanged": ["text-256k"]})

    def test_recommission_partial_target_gains_leaves_drops_think(self):
        # A target alias residue with a stray think=true + api_key but no model:
        # recommission must write every desired owned leaf, drop the stale think
        # (profile is non-reasoning), and preserve api_key.
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)   # no reasoning
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_text_256k]\n'
                'think = true\napi_key = "enc2:keep"\n'
            )
            parts = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            prov = data["providers"]["models"]["custom"]["model_loader_text_256k"]
            self.assertEqual(prov["model"], "text-256k")
            self.assertEqual(prov["uri"], "http://127.0.0.1:4321/v1")
            self.assertEqual(prov["context_window"], 262144)
            self.assertNotIn("think", prov)              # stray think scrubbed
            self.assertEqual(prov["api_key"], "enc2:keep")   # extra preserved

    def test_backup_written_on_apply(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            original = "schema_version = 3\n"
            target_path.write_text(original)
            parts = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            self.assertEqual(Path(str(target_path) + ".bak").read_text(), original)

    def test_malformed_toml_rejected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("this is = = not toml\n")
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            self.assertEqual(raised.exception.code, 2)

    def test_alias_conflict_referencing_other_provider(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[agents.model_loader_text_256k]\nmodel_provider = "anthropic.something-else"\n'
            )
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            self.assertEqual(raised.exception.code, 2)

    def test_cli_failure_is_error(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            fail_bin = Path(td, "zeroclaw")
            fail_bin.write_text("#!/bin/sh\necho boom >&2\nexit 1\n")
            os.chmod(fail_bin, 0o755)
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=str(fail_bin)))
            self.assertEqual(raised.exception.code, 2)

    def test_post_apply_mismatch_detected(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            noop_bin = Path(td, "zeroclaw")
            noop_bin.write_text("#!/bin/sh\necho '[]'\nexit 0\n")   # applies nothing
            os.chmod(noop_bin, 0o755)
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=str(noop_bin)))
            self.assertEqual(raised.exception.code, 2)

    def test_seed_conflict_on_divergent_models(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_a]\nmodel = "dup"\nmax_tokens = 1\n\n'
                '[providers.models.custom.model_loader_b]\nmodel = "dup"\nmax_tokens = 2\n'
            )
            out_path = Path(td, "seed.json")
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw({"passed": []}, self._paths(target_path), {},
                                   ns(seed_overrides=True, overrides_out=str(out_path)))
            self.assertEqual(raised.exception.code, 2)

    def test_seed_consolidates_by_model(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_m1]\nmodel = "m1"\nmax_tokens = 8192\nthink = true\n'
            )
            out_path = Path(td, "seed.json")
            sync.sync_zeroclaw({"passed": []}, self._paths(target_path), {},
                               ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = json.loads(out_path.read_text())["overrides"]
            self.assertEqual(seed["m1"], {"maxTokens": 8192, "reasoning": True})

    def test_second_dry_run_is_idempotent(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "idem-192k", ctx=196608)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            parts = load_parts(str(profiles_dir))
            fake = self._fake_bin(td)
            sync.sync_zeroclaw(parts, self._paths(target_path), {}, ns(apply=True, zeroclaw_bin=fake))
            result = sync.sync_zeroclaw(parts, self._paths(target_path), {}, ns(apply=False, zeroclaw_bin=fake))
            self.assertEqual(result["changes"], {"added": [], "removed": [], "updated": [], "unchanged": ["idem-192k"]})

    def test_second_apply_is_noop_no_cli_no_new_backup(self):
        # A managed provider/agent pair already matching the profile must not
        # invoke the CLI again nor overwrite the good .bak (native patch could
        # rewrite TOML/comments on an identical replace).
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            parts = load_parts(str(profiles_dir))
            fake = self._fake_bin(td)
            sync.sync_zeroclaw(parts, self._paths(target_path), {}, ns(apply=True, zeroclaw_bin=fake))
            bak = Path(str(target_path) + ".bak")
            self.assertTrue(bak.exists())
            bak.write_text("SENTINEL\n")   # would be clobbered by a real second patch
            missing_bin = str(Path(td, "does-not-exist-zeroclaw"))
            r2 = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                    ns(apply=True, zeroclaw_bin=missing_bin))
            self.assertFalse(r2["wrote"])                   # no ops -> no apply
            self.assertEqual(bak.read_text(), "SENTINEL\n")  # backup untouched

    def test_non_config_toml_basename_rejected_without_touching_files(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            wrong = Path(td, "zeroclaw-config.toml")
            original = "schema_version = 3\n"
            wrong.write_text(original)
            sibling = Path(td, "config.toml")   # the CLI would target this
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, {"zeroclaw_config_toml": str(wrong),
                                           "proxy_url": sync.PROXY_URL_DEFAULT},
                                   {}, ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            self.assertEqual(raised.exception.code, 2)
            self.assertEqual(wrong.read_text(), original)   # bytes untouched
            self.assertFalse(Path(str(wrong) + ".bak").exists())
            self.assertFalse(sibling.exists())

    def test_missing_binary_is_error(self):
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            parts = load_parts(str(profiles_dir))
            with self.assertRaises(SystemExit) as raised:
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=str(Path(td, "nope-zeroclaw"))))
            self.assertEqual(raised.exception.code, 2)

    def test_emitted_patch_is_scalar_leaf_only(self):
        # Guards the exact native incompatibility that forced the rewrite: real
        # `zeroclaw config patch` (0.8.3) rejects an object-valued add at a map
        # key and a whole-subtable remove. Every emitted op MUST end at a scalar
        # leaf; no value may be a dict; no path may stop at a container/alias.
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144, reasoning=True)
            make_profile_file(profiles_dir, "gone-256k", ctx=262144)   # will be stale
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            # seed a stale pair so remove-ops are exercised too
            first = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(first, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            (profiles_dir / "gone-256k.json").unlink()
            parts = load_parts(str(profiles_dir))
            cap = Path(td, "ops.jsonl")
            with mock.patch.dict(os.environ, {"ML_ZC_CAPTURE": str(cap)}):
                sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                   ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            container_paths = {
                "/providers", "/providers/models", "/providers/models/custom",
                "/agents",
            }
            saw = 0
            for line in cap.read_text().splitlines():
                for op in json.loads(line):
                    saw += 1
                    self.assertNotIsInstance(op.get("value"), dict)  # no object value
                    self.assertNotIn(op["path"], container_paths)    # not a container
                    # A leaf path never stops at the alias: providers reach
                    # /providers/models/custom/<alias>/<owned-field>, agents
                    # /agents/<alias>/<field where field is model_provider|enabled>.
                    segs = op["path"].split("/")[1:]
                    if segs[0] == "providers":
                        self.assertEqual(len(segs), 5)
                        self.assertIn(segs[4], sync.ZEROCLAW_MANAGED_FIELDS)
                    elif segs[0] == "agents":
                        self.assertEqual(len(segs), 3)
                        self.assertIn(segs[2], ("model_provider", "enabled"))
            self.assertGreater(saw, 0)

    def test_new_agent_created_disabled(self):
        # Real zeroclaw rejects an enabled agent with a model_provider but no
        # risk_profile; a newly created managed agent must be enabled=false.
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text("schema_version = 3\n")
            parts = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            self.assertIs(data["agents"]["model_loader_text_256k"]["enabled"], False)

    def test_existing_enabled_agent_preserved(self):
        # An operator who enabled the agent with a valid risk_profile keeps that
        # choice across a provider refresh (enabled + risk_profile untouched).
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[providers.models.custom.model_loader_text_256k]\n'
                'uri = "http://old/v1"\nmodel = "text-256k"\nwire_api = "chat_completions"\n'
                'context_window = 1\nmax_tokens = 1\n\n'
                '[risk_profiles.rp]\nlevel = "supervised"\n\n'
                '[agents.model_loader_text_256k]\n'
                'model_provider = "custom.model_loader_text_256k"\n'
                'enabled = true\nrisk_profile = "rp"\n'
            )
            parts = load_parts(str(profiles_dir))
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            ag = data["agents"]["model_loader_text_256k"]
            self.assertIs(ag["enabled"], True)          # operator choice kept
            self.assertEqual(ag["risk_profile"], "rp")
            self.assertEqual(data["providers"]["models"]["custom"]
                             ["model_loader_text_256k"]["uri"],
                             "http://127.0.0.1:4321/v1")  # provider still refreshed

    def test_orphan_agent_reported_and_deleted(self):
        # An orphan managed agent (no live provider) is deleted via the CLI and
        # surfaced in orphan_agent_removals, not mixed into changes["removed"].
        with tempfile.TemporaryDirectory() as td:
            profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
            make_profile_file(profiles_dir, "text-256k", ctx=262144)
            target_path = Path(td, "config.toml")
            target_path.write_text(
                'schema_version = 3\n\n'
                '[agents.model_loader_orphan_256k]\n'
                'model_provider = "custom.model_loader_orphan_256k"\nenabled = false\n'
            )
            parts = load_parts(str(profiles_dir))
            dry = sync.sync_zeroclaw(parts, self._paths(target_path), {},
                                     ns(apply=False, zeroclaw_bin=self._fake_bin(td)))
            self.assertEqual(dry["orphan_agent_removals"], ["model_loader_orphan_256k"])
            self.assertNotIn("model_loader_orphan_256k", dry["changes"]["removed"])
            sync.sync_zeroclaw(parts, self._paths(target_path), {},
                               ns(apply=True, zeroclaw_bin=self._fake_bin(td)))
            with open(target_path, "rb") as fh:
                data = tomllib.load(fh)
            self.assertNotIn("model_loader_orphan_256k", data.get("agents", {}))



class SeedOverridesTests(unittest.TestCase):
    """Each target's seed emits the shared target-neutral override schema
    ({id: {name?, reasoning?, input?, maxTokens?}}), translated from that
    target's own native managed-entry fields. A target that cannot express a
    field (e.g. Hermes has no name/reasoning/maxTokens) simply omits it."""

    def _seed(self, out_path):
        return json.loads(Path(out_path).read_text(encoding="utf-8"))

    def test_pi_and_feynman_seed_native_fields(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "pi-models.json")
            target_path.write_text(json.dumps({"providers": {"model-loader": {"models": [
                {"id": "m1", "name": "M One", "contextWindow": 200000, "input": ["text", "image"], "reasoning": True, "maxTokens": 8192},
            ]}}}))
            out_path = Path(td, "seed.json")
            paths = {"pi_models_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_pi({"passed": []}, paths, {}, ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = self._seed(out_path)["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "reasoning": True, "input": ["text", "image"], "maxTokens": 8192})

    def test_omp_seeds_native_fields(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "models.yml")
            target_path.write_text(
                "providers:\n  llama.cpp:\n    models:\n      - id: m1\n        name: M One\n"
                "        reasoning: true\n        input: [text]\n        contextWindow: 200000\n"
                "        maxTokens: 4096\n",
                encoding="utf-8",
            )
            out_path = Path(td, "seed.json")
            paths = {"omp_models_yml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_omp({"passed": []}, paths, {}, ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = self._seed(out_path)["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "reasoning": True, "input": ["text"], "maxTokens": 4096})

    def test_opencode_seed_translates_limit_and_modalities(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "opencode.json")
            target_path.write_text(json.dumps({"provider": {"model-loader": {"models": {
                "m1": {"name": "M One", "limit": {"context": 200000, "output": 8192}, "modalities": {"input": ["text", "image"], "output": ["text"]}, "reasoning": True},
            }}}}))
            out_path = Path(td, "seed.json")
            paths = {"opencode_config_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_opencode({"passed": []}, paths, {}, ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = self._seed(out_path)["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "reasoning": True, "input": ["text", "image"], "maxTokens": 8192})

    def test_forge_seed_translates_capability_fields_without_max_tokens(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "provider.json")
            target_path.write_text(json.dumps([
                {"id": "model-loader", "models": [
                    {"id": "m1", "name": "M One", "context_length": 200000, "supports_reasoning": True, "input_modalities": ["text", "image"]},
                ], "auth_methods": []},
            ]))
            out_path = Path(td, "seed.json")
            paths = {"forge_provider_json": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_forge({"passed": []}, paths, {}, ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = self._seed(out_path)["overrides"]
            self.assertEqual(seed["m1"], {"name": "M One", "reasoning": True, "input": ["text", "image"]})
            self.assertNotIn("maxTokens", seed["m1"])

    def test_hermes_seed_only_translates_vision_as_input(self):
        with tempfile.TemporaryDirectory() as td:
            target_path = Path(td, "config.yaml")
            target_path.write_text(
                "providers:\n  model-loader:\n    models:\n      m1:\n        context_length: 200000\n"
                "        supports_vision: true\n",
                encoding="utf-8",
            )
            out_path = Path(td, "seed.json")
            paths = {"hermes_config_yml": str(target_path), "proxy_url": sync.PROXY_URL_DEFAULT}
            sync.sync_hermes({"passed": []}, paths, {}, ns(seed_overrides=True, overrides_out=str(out_path)))
            seed = self._seed(out_path)["overrides"]
            self.assertEqual(seed["m1"], {"input": ["text", "image"]})
            self.assertNotIn("name", seed["m1"])
            self.assertNotIn("reasoning", seed["m1"])
            self.assertNotIn("maxTokens", seed["m1"])


class OverrideApplicationTests(unittest.TestCase):
    """Forward path: a non-empty overrides entry must flow into each
    build_*_entry and win over profile-derived/inherited values. Targets that
    cannot express a field (Forge: no output limit; Hermes: only vision) must
    ignore it rather than emit an unsupported key."""

    def _profile(self, pid="m1"):
        p = sync.Profile(f"/tmp/{pid}.json")
        p.name = "Derived Name"
        p.ctx = 200_000
        p.vision = False
        p.reasoning = False
        return p

    OV = {"m1": {"name": "Forced Name", "reasoning": True,
                 "input": ["text", "image"], "maxTokens": 12345}}

    def test_pi_entry_honors_overrides(self):
        entry = sync.build_pi_entry(self._profile(), self.OV, None)
        self.assertEqual(entry["name"], "Forced Name")
        self.assertTrue(entry["reasoning"])
        self.assertEqual(entry["input"], ["text", "image"])
        self.assertEqual(entry["maxTokens"], 12345)

    def test_omp_entry_honors_overrides(self):
        entry = sync.build_omp_entry(self._profile(), self.OV, None, sync.DEFAULT_MAX_TOKENS)
        self.assertEqual(entry["name"], "Forced Name")
        self.assertTrue(entry["reasoning"])
        self.assertEqual(list(entry["input"]), ["text", "image"])
        self.assertEqual(entry["maxTokens"], 12345)

    def test_opencode_entry_honors_overrides(self):
        entry = sync.build_opencode_entry(self._profile(), self.OV, None, sync.DEFAULT_MAX_TOKENS)
        self.assertEqual(entry["name"], "Forced Name")
        self.assertTrue(entry["reasoning"])
        self.assertEqual(entry["modalities"]["input"], ["text", "image"])
        self.assertEqual(entry["limit"]["output"], 12345)

    def test_forge_entry_honors_overrides_but_ignores_max_tokens(self):
        entry = sync.build_forge_entry(self._profile(), self.OV)
        self.assertEqual(entry["name"], "Forced Name")
        self.assertTrue(entry["supports_reasoning"])
        self.assertEqual(entry["input_modalities"], ["text", "image"])
        self.assertNotIn("maxTokens", entry)

    def test_hermes_entry_honors_only_vision_override(self):
        entry = sync.build_hermes_entry(self._profile(), self.OV)
        self.assertTrue(entry["supports_vision"])
        self.assertNotIn("name", entry)
        self.assertNotIn("supports_reasoning", entry)
        self.assertNotIn("maxTokens", entry)

    def test_crush_entry_honors_all_overrides(self):
        entry = sync.build_crush_entry(self._profile(), self.OV, None, sync.DEFAULT_MAX_TOKENS)
        self.assertEqual(entry["name"], "Forced Name")
        self.assertTrue(entry["can_reason"])
        self.assertTrue(entry["supports_attachments"])   # from input override
        self.assertEqual(entry["default_max_tokens"], 12345)

    def test_droid_entry_honors_all_overrides(self):
        entry = sync.build_droid_entry(self._profile(), self.OV, None,
                                        sync.DEFAULT_MAX_TOKENS, sync.PROXY_URL_DEFAULT)
        self.assertEqual(entry["displayName"], "Model Loader · Forced Name")
        self.assertEqual(entry["reasoningEffort"], "high")
        self.assertNotIn("noImageSupport", entry)        # image present via override
        self.assertEqual(entry["maxOutputTokens"], 12345)

    def test_zeroclaw_provider_honors_only_reasoning_and_max_tokens(self):
        obj = sync.build_zeroclaw_provider(self._profile(), self.OV, None,
                                            sync.DEFAULT_MAX_TOKENS, sync.PROXY_URL_DEFAULT)
        self.assertTrue(obj["think"])                    # reasoning override honored
        self.assertEqual(obj["max_tokens"], 12345)       # maxTokens override honored
        self.assertEqual(obj["model"], "m1")             # name/input ignored
        self.assertNotIn("name", obj)
        self.assertNotIn("input", obj)
        self.assertNotIn("supports_attachments", obj)


class CliIntegrationTests(unittest.TestCase):
    """Every invocation below passes an explicit path for all nine targets
    plus --profiles-dir/--config, so main() never falls through to a real
    installed agent config on the machine running these tests."""

    def _cli_paths(self, td):
        return {
            "pi": str(Path(td, "pi.json")),
            "feynman": str(Path(td, "feynman.json")),
            "omp": str(Path(td, "omp.yml")),
            "opencode": str(Path(td, "opencode.json")),
            "crush": str(Path(td, "crush.json")),
            "forge": str(Path(td, "forge-provider.json")),
            "hermes": str(Path(td, "hermes.yaml")),
            "droid": str(Path(td, "droid-settings.json")),
            "zeroclaw": str(Path(td, "zc", "config.toml")),
        }

    def _fake_zc_bin(self, td):
        p = Path(td, "fake-zeroclaw")
        p.write_text(ZeroClawTests.FAKE_ZC)
        os.chmod(p, 0o755)
        return str(p)

    def _write_fixture_files(self, paths, skip=()):
        if "pi" not in skip:
            Path(paths["pi"]).write_text(json.dumps({"providers": {}}))
        if "feynman" not in skip:
            Path(paths["feynman"]).write_text(json.dumps({"providers": {}}))
        if "omp" not in skip:
            Path(paths["omp"]).write_text("providers: {}\n")
        if "opencode" not in skip:
            Path(paths["opencode"]).write_text(json.dumps({"provider": {}}))
        if "crush" not in skip:
            Path(paths["crush"]).write_text(json.dumps({"providers": {}}))
        if "forge" not in skip:
            Path(paths["forge"]).write_text("[]")
        if "hermes" not in skip:
            Path(paths["hermes"]).write_text("providers: {}\n")
        if "droid" not in skip:
            Path(paths["droid"]).write_text(json.dumps({"customModels": []}))
        if "zeroclaw" not in skip:
            Path(paths["zeroclaw"]).parent.mkdir(parents=True, exist_ok=True)
            Path(paths["zeroclaw"]).write_text("schema_version = 3\n")

    def _argv(self, td, paths, extra=()):
        profiles_dir = Path(td, "profiles")
        return [
            "--profiles-dir", str(profiles_dir),
            "--config", str(Path(td, "nonexistent.toml")),
            "--pi-models-json", paths["pi"],
            "--feynman-models-json", paths["feynman"],
            "--omp-models-yml", paths["omp"],
            "--opencode-config-json", paths["opencode"],
            "--crush-config-json", paths["crush"],
            "--forge-provider-json", paths["forge"],
            "--hermes-config-yml", paths["hermes"],
            "--droid-settings-json", paths["droid"],
            "--zeroclaw-config-toml", paths["zeroclaw"],
            "--zeroclaw-bin", self._fake_zc_bin(td),
        ] + list(extra)

    def _write_profiles(self, td):
        profiles_dir = Path(td, "profiles"); profiles_dir.mkdir()
        make_profile_file(profiles_dir, "cli-256k", ctx=262144)
        return profiles_dir

    def test_all_dispatches_every_existing_target_and_skips_missing(self):
        with tempfile.TemporaryDirectory() as td:
            self._write_profiles(td)
            paths = self._cli_paths(td)
            self._write_fixture_files(paths, skip=("hermes",))
            argv = self._argv(td, paths, extra=["--target", "all", "--json"])
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = sync.main(argv)
            self.assertEqual(code, 0)
            payload = json.loads(out.getvalue())
            targets_synced = {r["target"] for r in payload["results"]}
            self.assertEqual(targets_synced, {"pi", "feynman", "omp", "opencode", "crush", "forge", "droid", "zeroclaw"})
            unavailable = {u["target"] for u in payload["unavailable_targets"]}
            self.assertEqual(unavailable, {"hermes"})
            self.assertFalse(Path(paths["hermes"]).exists())

    def test_explicit_missing_target_exits_two(self):
        with tempfile.TemporaryDirectory() as td:
            self._write_profiles(td)
            missing_hermes = str(Path(td, "hermes-missing.yaml"))
            argv = [
                "--profiles-dir", str(Path(td, "profiles")),
                "--config", str(Path(td, "nonexistent.toml")),
                "--hermes-config-yml", missing_hermes,
                "--target", "hermes",
            ]
            with self.assertRaises(SystemExit) as raised:
                sync.main(argv)
            self.assertEqual(raised.exception.code, 2)

    def test_seed_overrides_accepts_each_explicit_target_and_rejects_all(self):
        with tempfile.TemporaryDirectory() as td:
            self._write_profiles(td)
            paths = self._cli_paths(td)
            self._write_fixture_files(paths)
            for target in sync.TARGET_NAMES:
                out_path = Path(td, f"seed-{target}.json")
                argv = self._argv(td, paths, extra=["--target", target, "--seed-overrides", "--overrides", str(out_path)])
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    code = sync.main(argv)
                self.assertEqual(code, 0, target)
                self.assertTrue(out_path.exists(), target)

            argv_all = self._argv(td, paths, extra=["--target", "all", "--seed-overrides"])
            with self.assertRaises(SystemExit) as raised:
                sync.main(argv_all)
            self.assertEqual(raised.exception.code, 2)

    def test_apply_creates_backups_and_second_dry_run_has_no_changes(self):
        with tempfile.TemporaryDirectory() as td:
            self._write_profiles(td)
            paths = self._cli_paths(td)
            self._write_fixture_files(paths)
            apply_argv = self._argv(td, paths, extra=["--target", "all", "--apply", "--json"])
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = sync.main(apply_argv)
            self.assertEqual(code, 0)
            for key in ("pi", "feynman", "omp", "opencode", "crush", "forge", "hermes", "droid", "zeroclaw"):
                self.assertTrue(Path(paths[key] + ".bak").exists(), key)

            dry_argv = self._argv(td, paths, extra=["--target", "all", "--json"])
            out2 = io.StringIO()
            with contextlib.redirect_stdout(out2):
                code2 = sync.main(dry_argv)
            self.assertEqual(code2, 0)
            payload = json.loads(out2.getvalue())
            self.assertEqual(len(payload["results"]), 9)
            for r in payload["results"]:
                self.assertEqual(r["changes"]["added"], [], r["target"])
                self.assertEqual(r["changes"]["updated"], [], r["target"])
                self.assertEqual(r["changes"]["removed"], [], r["target"])

    def test_existing_pi_and_omp_flags_retain_meaning(self):
        with tempfile.TemporaryDirectory() as td:
            self._write_profiles(td)
            pi_path = Path(td, "pi.json"); pi_path.write_text(json.dumps({"providers": {}}))
            argv = [
                "--profiles-dir", str(Path(td, "profiles")),
                "--config", str(Path(td, "nonexistent.toml")),
                "--pi-models-json", str(pi_path),
                "--pi-provider", "custom-pi-provider",
                "--target", "pi", "--apply", "--json",
            ]
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = sync.main(argv)
            self.assertEqual(code, 0)
            written = json.loads(pi_path.read_text())
            self.assertIn("custom-pi-provider", written["providers"])


class SkillDocumentationTests(unittest.TestCase):
    def setUp(self):
        self.text = (SKILL_DIR / "SKILL.md").read_text(encoding="utf-8")
        self.refs = (SKILL_DIR / "references" / "targets.md").read_text(encoding="utf-8")

    def test_all_nine_targets_named(self):
        for name in ("pi", "Feynman", "omp", "OpenCode", "Crush", "Forgecode",
                     "Hermes", "Factory Droid", "ZeroClaw"):
            self.assertIn(name, self.text)

    def test_target_choices_documented(self):
        self.assertIn("pi,feynman,omp,opencode,crush,forge,hermes,droid,zeroclaw,all",
                      self.text.replace(" ", ""))

    def test_forge_endpoint_and_hermes_path_documented(self):
        self.assertIn("/v1/chat/completions", self.refs)
        self.assertIn("config.yaml", self.text)

    def test_new_adapter_schemas_documented_in_references(self):
        for token in ("openai-compat", "customModels", "model_loader_",
                      "config.toml", "generic-chat-completion-api"):
            self.assertIn(token, self.refs)

    def test_hermes_discover_models_false_documented(self):
        self.assertIn("discover_models", self.text)
        self.assertIn("false", self.text.lower())

    def test_dry_run_apply_guidance_present(self):
        self.assertIn("--apply", self.text)
        self.assertIn("dry", self.text.lower())

    def test_no_changelog_history_language(self):
        forbidden = ("changelog", "change history", "version history")
        low = self.text.lower()
        for word in forbidden:
            self.assertNotIn(word, low)


if __name__ == "__main__":
    unittest.main()
