#!/usr/bin/env python3
"""Synthetic checks of serve/server.py request handling (no GPU):
1. the GEN line the real StrataEngine would send when the prepared config's sampling block has seed=42 and the client
   sends no seed (every request gets the same Philox seed -> identical sampled text for identical prompts);
2. whether the OpenAI `stop` field is honoured (MockEngine emits a fixed script containing the stop string).
"""
import json
import sys
import urllib.request

FORK = "/home/diogo/dev/model-loader/backends/strata-fork"
sys.path.insert(0, FORK)
sys.path.insert(0, FORK + "/tools")
import serve.server as S  # noqa: E402

CFG = json.load(open("/home/diogo/.config/model-loader/strata/qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k.json"))


class Capture(S.MockEngine):
    def __init__(self, *a, **k):
        super().__init__(*a, **k)
        self.lines = []

    def generate(self, ids, max_new, sampling, cancel, embeddings=None):
        # exactly what StrataEngine.generate writes on the engine's stdin (minus the ids)
        self.lines.append(f"GEN {int(max_new)}{S.StrataEngine.sampling_keys(sampling or {})}")
        yield from super().generate(ids, max_new, sampling, cancel, embeddings)


def post(port, body):
    req = urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions", data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=30).read())


def main():
    tok = S.ByteTokenizer()
    eng = Capture(tok, "alpha STOPHERE beta", max_context=32768)
    defaults = S.sampling_defaults_from_config(CFG)
    svc = S.Service(eng, tok, S.ChatTemplate(FORK + "/serve/chat_template.jinja"), model_name="mock",
                    sampling_defaults=defaults)
    httpd = S.serve(svc, host="127.0.0.1", port=0)
    port = httpd.server_address[1]
    for i in range(2):
        post(port, {"messages": [{"role": "user", "content": "same prompt"}], "max_tokens": 64})
    r = post(port, {"messages": [{"role": "user", "content": "x"}], "max_tokens": 64, "stop": ["STOPHERE"], "chat_template_kwargs": {"enable_thinking": False}})
    httpd.shutdown()
    print("config sampling defaults:", defaults)
    for line in eng.lines:
        print("engine line:", line)
    content = r["choices"][0]["message"]
    print("stop=['STOPHERE'] -> content:", repr(content), "| finish_reason:", r["choices"][0]["finish_reason"])


if __name__ == "__main__":
    main()
