#!/usr/bin/env python3
"""Focused tests for openai-concurrency-probe.py.

The probe is driven only against a fake in-process HTTP server bound to an
ephemeral loopback port. Nothing here talks to the live proxy at :4321.
"""
import importlib.util
import io
import json
import pathlib
import threading
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = pathlib.Path(__file__).resolve().parent
PROBE_PATH = HERE / "openai-concurrency-probe.py"


def load_probe():
    spec = importlib.util.spec_from_file_location("probe_mod", PROBE_PATH)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class FakeHandler(BaseHTTPRequestHandler):
    """Streams N SSE chunks; behaviour tuned by class attributes set per test."""

    tokens = 8               # content chunks streamed per request
    fail_status = None       # if set, respond with this status instead
    requests_seen = None     # list of parsed request bodies (shared)
    lock = threading.Lock()

    def log_message(self, *_a):  # silence
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        try:
            body = json.loads(raw or b"{}")
        except ValueError:
            body = {}
        with FakeHandler.lock:
            if FakeHandler.requests_seen is not None:
                FakeHandler.requests_seen.append(body)

        if FakeHandler.fail_status is not None:
            self.send_response(FakeHandler.fail_status)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":"boom"}')
            return

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for i in range(FakeHandler.tokens):
            chunk = {
                "choices": [{"delta": {"content": f"t{i} "}}],
            }
            self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


class ServerCtx:
    def __init__(self):
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), FakeHandler)
        self.port = self.httpd.server_address[1]
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return f"http://127.0.0.1:{self.port}/v1/chat/completions"

    def __exit__(self, *a):
        self.httpd.shutdown()
        self.httpd.server_close()


probe = load_probe()


class PercentileTests(unittest.TestCase):
    def test_p50_p95_linear_interpolation(self):
        vals = [10.0, 20.0, 30.0, 40.0, 50.0]
        self.assertAlmostEqual(probe.percentile(vals, 50), 30.0, places=6)
        self.assertAlmostEqual(probe.percentile(vals, 95), 48.0, places=6)

    def test_unsorted_input_is_sorted(self):
        self.assertAlmostEqual(probe.percentile([50.0, 10.0, 30.0], 50), 30.0, places=6)

    def test_single_value(self):
        self.assertAlmostEqual(probe.percentile([7.0], 95), 7.0, places=6)

    def test_empty(self):
        self.assertEqual(probe.percentile([], 50), 0.0)


class PayloadTests(unittest.TestCase):
    def test_fixed_request_fields(self):
        p = probe.build_payload("some-model")
        self.assertEqual(p["max_tokens"], 256)
        self.assertTrue(p["ignore_eos"])
        self.assertEqual(p["seed"], probe.FIXED_SEED)
        self.assertEqual(p["temperature"], probe.FIXED_TEMPERATURE)
        self.assertTrue(p["stream"])
        # same fixed prompt every call -> deterministic
        self.assertEqual(probe.build_payload("m")["messages"],
                         probe.build_payload("m")["messages"])


class ConcurrencyScheduleTests(unittest.TestCase):
    def test_default_schedule(self):
        self.assertEqual(probe.DEFAULT_CONCURRENCIES, [1, 2, 4, 8, 16, 32])


class SweepTests(unittest.TestCase):
    def setUp(self):
        FakeHandler.tokens = 8
        FakeHandler.fail_status = None
        FakeHandler.requests_seen = []

    def test_warmup_excluded_and_request_count(self):
        with ServerCtx() as url:
            objs = probe.run_sweep(url, model="m", concurrencies=[1, 2],
                                   warmup=True)
        # 1 warmup + (1 + 2) sweep requests = 4 total hit the server
        self.assertEqual(len(FakeHandler.requests_seen), 4)
        # but only the two concurrency levels appear in output
        self.assertEqual([o["concurrency"] for o in objs], [1, 2])
        # warmup never inflates a stat object's request count
        self.assertEqual(objs[0]["requests"], 1)
        self.assertEqual(objs[1]["requests"], 2)

    def test_jsonl_schema_one_object_per_concurrency(self):
        with ServerCtx() as url:
            objs = probe.run_sweep(url, model="m", concurrencies=[1, 4],
                                   warmup=False)
        self.assertEqual(len(objs), 2)
        required = {
            "concurrency", "requests", "ok", "errors", "tokens",
            "duration_s", "wall_tokens_per_s", "request_tokens_per_s",
            "ttft_p50_s", "ttft_p95_s",
        }
        for o in objs:
            self.assertTrue(required.issubset(o.keys()),
                            f"missing keys: {required - set(o.keys())}")
            # each object must be JSON-serialisable as a single line
            line = json.dumps(o)
            self.assertNotIn("\n", line)

    def test_tokens_and_throughput_accounting(self):
        FakeHandler.tokens = 8
        with ServerCtx() as url:
            objs = probe.run_sweep(url, model="m", concurrencies=[4],
                                   warmup=False)
        o = objs[0]
        self.assertEqual(o["ok"], 4)
        self.assertEqual(o["errors"], 0)
        self.assertEqual(o["tokens"], 4 * 8)  # 4 requests x 8 content chunks
        self.assertGreater(o["wall_tokens_per_s"], 0.0)
        self.assertGreater(o["request_tokens_per_s"], 0.0)

    def test_http_error_accounting(self):
        FakeHandler.fail_status = 500
        with ServerCtx() as url:
            objs = probe.run_sweep(url, model="m", concurrencies=[4],
                                   warmup=False)
        o = objs[0]
        self.assertEqual(o["errors"], 4)
        self.assertEqual(o["ok"], 0)
        self.assertEqual(o["tokens"], 0)
        # failed requests contribute no throughput
        self.assertEqual(o["wall_tokens_per_s"], 0.0)
        self.assertEqual(o["request_tokens_per_s"], 0.0)

    def test_main_emits_jsonl_to_stdout(self):
        with ServerCtx() as url:
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = probe.main([
                    "--endpoint", url, "--model", "m",
                    "--concurrency", "1,2", "--no-warmup",
                ])
        self.assertEqual(rc, 0)
        lines = [l for l in buf.getvalue().splitlines() if l.strip()]
        self.assertEqual(len(lines), 2)
        parsed = [json.loads(l) for l in lines]
        self.assertEqual([p["concurrency"] for p in parsed], [1, 2])


if __name__ == "__main__":
    unittest.main(verbosity=2)
