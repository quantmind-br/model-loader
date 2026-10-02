#!/usr/bin/env python3
"""Minimal read-only GGUF header parser (KV + tensor directory only; never reads tensor payloads).

Usage: gguf_header.py FILE [--kv] [--tensors REGEX] [--dense-split K]
"""
import re
import struct
import sys

GEOM = {0: (1, 4), 1: (1, 2), 30: (1, 2), 2: (32, 18), 3: (32, 20), 6: (32, 22), 7: (32, 24), 8: (32, 34),
        9: (32, 36), 10: (256, 84), 11: (256, 110), 12: (256, 144), 13: (256, 176), 14: (256, 210),
        16: (256, 66), 17: (256, 74), 18: (256, 98), 20: (32, 18), 21: (256, 110), 22: (256, 82),
        23: (256, 136), 29: (256, 56), 42: (64, 18)}


class R:
    def __init__(self, f):
        self.f = f
        self.n = 0

    def take(self, fmt):
        s = struct.calcsize(fmt)
        b = self.f.read(s)
        self.n += s
        return struct.unpack("<" + fmt, b)

    def string(self):
        (ln,) = self.take("Q")
        b = self.f.read(ln)
        self.n += ln
        return b.decode("utf-8", "replace")


SCALAR = {0: "B", 1: "b", 2: "H", 3: "h", 4: "I", 5: "i", 6: "f", 7: "?", 10: "Q", 11: "q", 12: "d"}


def value(r, t, keep=True):
    if t in SCALAR:
        return r.take(SCALAR[t])[0]
    if t == 8:
        return r.string()
    if t == 9:
        (et,) = r.take("I")
        (n,) = r.take("Q")
        if et in SCALAR:
            sz = struct.calcsize(SCALAR[et])
            r.f.seek(sz * n, 1)
            r.n += sz * n
            return f"<array {n} x type{et}>"
        out = []
        for _ in range(n):
            v = value(r, et, keep=False)
            if len(out) < 4:
                out.append(v)
        return f"<array {n} x type{et}: {out}...>"
    raise ValueError(f"unknown type {t}")


def parse(path):
    with open(path, "rb") as f:
        r = R(f)
        magic = f.read(4)
        r.n += 4
        assert magic == b"GGUF", magic
        (ver,) = r.take("I")
        (nt,) = r.take("Q")
        (nkv,) = r.take("Q")
        kv = {}
        for _ in range(nkv):
            k = r.string()
            (t,) = r.take("I")
            kv[k] = value(r, t)
        tensors = []
        for _ in range(nt):
            name = r.string()
            (nd,) = r.take("I")
            dims = r.take("Q" * nd)
            (tt,) = r.take("I")
            (off,) = r.take("Q")
            ne = 1
            for d in dims:
                ne *= d
            be, bb = GEOM.get(tt, (None, None))
            nbytes = ne // be * bb if be else None
            tensors.append((name, dims, tt, off, nbytes))
        return ver, kv, tensors, r.n


def main():
    path = sys.argv[1]
    ver, kv, tensors, hdr = parse(path)
    args = sys.argv[2:]
    if "--kv" in args:
        for k, v in kv.items():
            print(f"KV {k} = {v}")
    if "--tensors" in args:
        rx = re.compile(args[args.index("--tensors") + 1])
        for name, dims, tt, off, nb in tensors:
            if rx.search(name):
                print(f"T {name} dims={dims} type={tt} bytes={nb}")
    print(f"# version {ver}, {len(tensors)} tensors, header {hdr} B")


if __name__ == "__main__":
    main()
