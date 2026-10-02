#!/usr/bin/env python3
"""Quantify the dense VRAM each layer-split stage holds for layers it never runs.

Reads only GGUF headers (tensor directory) and the pack's index.txt. Mirrors:
  - NativeDense::eligible / native_mmvq_supported (src/core/native_dense.cpp, native_mmvq.cu)
  - WeightTable::pool_bytes with the skip set (src/core/weights.cpp)
Usage: dense_per_stage.py PACK_DIR SPLIT_K SHARD1 [SHARD2 ...]
"""
import re
import sys
from collections import defaultdict

sys.path.insert(0, "/tmp/strata-claude-independent-20261001")
from gguf_header import parse  # noqa: E402

SUPPORTED = {2, 6, 8, 11, 12, 13, 14, 20, 23, 42, 16, 17, 18, 21, 22, 29}
SUFFIXES = (".attn_qkv.weight", ".attn_gate.weight", ".ssm_out.weight", ".attn_q.weight", ".attn_k.weight",
            ".attn_v.weight", ".attn_output.weight", ".ffn_gate_shexp.weight", ".ffn_up_shexp.weight",
            ".ffn_down_shexp.weight")
NATIVE_PLE_KEY = {42, 18, 23}


def layer_of(name):
    m = re.match(r"blk\.(\d+)\.", name)
    return int(m.group(1)) if m else None


def main():
    pack, k = sys.argv[1], int(sys.argv[2])
    shards = sys.argv[3:]
    native_bytes = defaultdict(int)
    served = set()
    for s in shards:
        _, _, tensors, _ = parse(s)
        for name, dims, tt, off, nb in tensors:
            if not name.startswith("blk."):
                continue
            elig = name.endswith(SUFFIXES) or (name == "blk.1.ple_key.weight" and tt in NATIVE_PLE_KEY)
            if elig and tt in SUPPORTED and len(dims) == 2:
                served.add(name)
                native_bytes[layer_of(name)] += nb
    align = 64
    dense = defaultdict(int)
    other = 0
    with open(f"{pack}/index.txt") as f:
        for line in f:
            if line.startswith("#"):
                m = re.search(r"align (\d+) pool", line)
                if m:
                    align = int(m.group(1))
                continue
            p = line.split()
            name, dst_bytes = p[0], int(p[6])
            if name in served or name == "token_embd.weight" or name == "output.weight":
                continue  # skipped by the engine (native-served, native embed, native head)
            b = (dst_bytes + align - 1) // align * align
            L = layer_of(name)
            if L is None:
                other += b
            else:
                dense[L] += b
    gib = 1 << 30
    tot_native = sum(native_bytes.values())
    tot_dense = sum(dense.values())
    lo_n = sum(v for L, v in native_bytes.items() if L < k)
    hi_n = tot_native - lo_n
    lo_d = sum(v for L, v in dense.items() if L < k)
    hi_d = tot_dense - lo_d
    print(f"pack={pack} split K={k}")
    print(f"  native dense (all 48 layers): {tot_native/gib:.3f} GiB  [layers <K: {lo_n/gib:.3f}, >=K: {hi_n/gib:.3f}]")
    print(f"  pack arena blk.* (compacted):  {tot_dense/gib:.3f} GiB  [layers <K: {lo_d/gib:.3f}, >=K: {hi_d/gib:.3f}]")
    print(f"  pack arena non-layer tensors:  {other/gib:.3f} GiB (embedding/head excluded)")
    print(f"  CUDA0 holds for layers it never runs (>=K):  {(hi_n+hi_d)/gib:.3f} GiB")
    print(f"  CUDA1 holds for layers it never runs (<K):   {(lo_n+lo_d)/gib:.3f} GiB")


if __name__ == "__main__":
    main()
