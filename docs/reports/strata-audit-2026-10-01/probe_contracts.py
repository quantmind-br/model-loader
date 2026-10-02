"""Read-only audit probes: synthetic artifacts and mock HTTP; no GPU/model launch.

Run from the repository root:
STRATA_GGUF_PY=$PWD/backends/strata-fork/build-fork-sm86/_deps/strata_llamacpp-src/gguf-py \
  python docs/reports/strata-audit-2026-10-01/probe_contracts.py
"""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import queue
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
BACKEND = ROOT / "backends/strata-fork"
sys.path[:0] = [str(BACKEND), str(BACKEND / "tools")]
from serve.frontend import ChatTemplate, openai_to_messages, parse_tool_call
from serve.server import ByteTokenizer, MockEngine, Service, StrataEngine, serve


def http_probe(script, additions):
    tok = ByteTokenizer()
    engine = MockEngine(tok, script + "<|im_end|>", max_context=8192)
    svc = Service(engine, tok, ChatTemplate(BACKEND / "serve/chat_template.jinja"))
    httpd = serve(svc, port=0)
    payload = {"model": "audit", "messages": [{"role": "user", "content": "test"}],
               "reasoning_effort": "none", "max_tokens": 1024, **additions}
    try:
        req = urllib.request.Request(
            f"http://127.0.0.1:{httpd.server_address[1]}/v1/chat/completions",
            data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=10) as response:
            result = json.load(response)
            return {"request": payload, "status": response.status,
                    "message": result["choices"][0]["message"],
                    "finish_reason": result["choices"][0]["finish_reason"]}
    finally:
        httpd.shutdown()
        httpd.server_close()


def pack_probe():
    import numpy as np
    from _paths import add_gguf_py
    add_gguf_py()
    from gguf import GGUFWriter, GGMLQuantizationType as Q, quants
    import iq_pack
    with tempfile.TemporaryDirectory(prefix="strata-pack-audit-") as temp:
        tmp = Path(temp)
        out = tmp / "out"
        (out / "tokenizer").mkdir(parents=True)
        # Avoid invoking the tokenizer exporter: this probe only exercises weight packing.
        (out / "tokenizer/vocab.json").write_text("{}")
        (out / "tokenizer/chat_template.jinja").write_text("fixture")
        hashes = []
        for tag, value in (("a", 1.0), ("b", 2.0)):
            source = tmp / f"{tag}.gguf"
            writer = GGUFWriter(source, "qwen4exp")
            writer.add_tensor("blk.0.ffn_gate_inp.weight",
                              quants.quantize(np.full((2, 32), value, dtype=np.float32), Q.BF16),
                              raw_dtype=Q.BF16)
            for role in ("gate", "up", "down"):
                writer.add_tensor(f"blk.0.ffn_{role}_exps.weight",
                                  np.full((2, 32, 32), value, dtype=np.float32))
            writer.write_header_to_file()
            writer.write_kv_data_to_file()
            writer.write_tensors_to_file()
            writer.close()
            with patch.object(sys, "argv", ["iq_pack", "--gguf", str(source), "--out", str(out), "--experts-bin"]):
                rc = iq_pack.main()
            hashes.append({"source": tag, "rc": rc,
                           "experts": hashlib.sha256((out / "experts.bin").read_bytes()).hexdigest(),
                           "dense": hashlib.sha256((out / "dense.bin").read_bytes()).hexdigest(),
                           "layout": (out / "native_experts.txt").read_text()})
        assert hashes[0]["experts"] == hashes[1]["experts"]
        assert hashes[0]["dense"] != hashes[1]["dense"]
        return {"runs": hashes, "stale_experts_with_new_dense": True}


def drain_probe():
    engine = StrataEngine.__new__(StrataEngine)
    engine.progress = None
    engine.prefill_tok_s_mean = None
    engine.proc = type("FakeProcess", (), {"stdin": io.StringIO()})()
    engine.lines = queue.Queue()
    engine.can_stop = True
    engine.lines.put("T 65")
    gen = engine.generate([1], 10, {}, threading.Event())
    assert next(gen) == 65
    closed = threading.Event()
    def close():
        gen.close()
        closed.set()
    worker = threading.Thread(target=close)
    worker.start()
    blocked = not closed.wait(0.2)
    engine.lines.put(None)  # explicit EOF safely releases this synthetic probe
    worker.join(2)
    assert closed.is_set()
    return {"blocked_without_DONE_or_EOF_after_200ms": blocked,
            "protocol_written": engine.proc.stdin.getvalue(), "released_on_EOF": True,
            "limitation": "200ms observation; no-timeout conclusion is also based on queue.get() source"}


def main():
    tool = {"type": "function", "function": {"name": "probe", "parameters": {
        "type": "object", "properties": {"value": {"type": "integer"}}, "required": ["value"]}}}
    call = "<tool_call>\n<function=probe>\n<parameter=value>\n7\n</parameter>\n</function>\n</tool_call>"
    results = {
        "required_tool": http_probe("plain answer", {"tools": [tool], "tool_choice": "required"}),
        "no_tool": http_probe(call, {"tools": [tool], "tool_choice": "none"}),
        "one_tool": http_probe(call + "\n" + call, {"tools": [tool], "parallel_tool_calls": False}),
        "json_format": http_probe("plain answer", {"response_format": {"type": "json_object"}}),
        "stop": http_probe("BEFORE END AFTER", {"stop": ["END"]}),
        "sampling_protocol": {str(seed): StrataEngine.sampling_keys({"seed": seed, "top_k": 0})
                              for seed in [0, 1, 42]},
        "invalid_typed_argument": parse_tool_call(
            "<function=probe><parameter=value>not_an_integer</parameter></function>", tool["function"]).__dict__,
        "packer": pack_probe(),
        "drain": drain_probe(),
    }
    assert not results["required_tool"]["message"].get("tool_calls")
    assert results["no_tool"]["message"].get("tool_calls")
    assert len(results["one_tool"]["message"]["tool_calls"]) == 2
    assert results["json_format"]["message"]["content"] == "plain answer"
    assert "AFTER" in results["stop"]["message"]["content"]
    assert "seed=" not in results["sampling_protocol"]["0"]
    (Path(__file__).parent / "contract-probes.json").write_text(json.dumps(results, indent=2, ensure_ascii=False))
    print("Confirmed: tool controls, JSON format, stop ignored; seed0 omitted; typed argument not enforced; stale pack; unbounded drain.")


if __name__ == "__main__":
    main()
