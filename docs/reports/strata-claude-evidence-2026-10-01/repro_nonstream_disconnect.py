#!/usr/bin/env python3
"""Synthetic reproduction (no GPU, no model): does serve/server.py stop the engine when a NON-streaming client
disconnects?  And for comparison, a streaming client.

MockEngine yields one token every DELAY seconds from a long script.  We send a request, drop the socket after
DROP seconds, then measure (a) how many tokens the engine still produced and (b) how long a second request waits
in the FIFO.  Run with PYTHONDONTWRITEBYTECODE=1 so nothing is written into the fork's tree.
"""
import json
import socket
import sys
import threading
import time

FORK = "/home/diogo/dev/model-loader/backends/strata-fork"
sys.path.insert(0, FORK)
sys.path.insert(0, FORK + "/tools")
import serve.server as S  # noqa: E402

DELAY = 0.02          # 50 tok/s mock engine
SCRIPT_TOKENS = 300   # ~6 s of generation
DROP = 0.5


class CountingMock(S.MockEngine):
    def __init__(self, *a, **k):
        super().__init__(*a, **k)
        self.yielded = 0

    def generate(self, ids, max_new, sampling, cancel, embeddings=None):
        for t in super().generate(ids, max_new, sampling, cancel, embeddings):
            self.yielded += 1
            yield t


def start():
    tok = S.ByteTokenizer()
    eng = CountingMock(tok, "x" * SCRIPT_TOKENS, max_context=32768, delay_s=DELAY)
    svc = S.Service(eng, tok, S.ChatTemplate(FORK + "/serve/chat_template.jinja"), model_name="mock")
    httpd = S.serve(svc, host="127.0.0.1", port=0)
    return eng, svc, httpd.server_address[1]


def raw_request(port, body, drop_after=None):
    s = socket.create_connection(("127.0.0.1", port))
    data = json.dumps(body).encode()
    s.sendall(b"POST /v1/chat/completions HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\n"
              b"Content-Length: " + str(len(data)).encode() + b"\r\n\r\n" + data)
    if drop_after is not None:
        time.sleep(drop_after)
        s.close()
        return None
    out = b""
    while True:
        chunk = s.recv(65536)
        if not chunk:
            break
        out += chunk
    s.close()
    return out


def scenario(stream):
    eng, svc, port = start()
    body = {"messages": [{"role": "user", "content": "hi"}], "stream": stream, "max_tokens": SCRIPT_TOKENS + 10}
    t0 = time.time()
    raw_request(port, body, drop_after=DROP)
    at_drop = eng.yielded
    # second request: how long does it wait behind the abandoned one?
    t1 = time.time()
    raw_request(port, {"messages": [{"role": "user", "content": "again"}], "stream": False, "max_tokens": 5})
    wait = time.time() - t1
    # the first request's tokens counted when the second had the engine
    print(f"stream={stream}: tokens at disconnect={at_drop}, engine tokens after both requests={eng.yielded}, "
          f"second request latency={wait:.2f}s (expected ~0.1s if the first was cancelled), "
          f"total {time.time()-t0:.2f}s")


if __name__ == "__main__":
    scenario(stream=True)
    scenario(stream=False)
