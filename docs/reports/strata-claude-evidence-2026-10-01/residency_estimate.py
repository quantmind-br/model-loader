"""Derived estimate: how much of each stage's experts fits its GPU's expert cache, per profile.
Inputs: native_experts.txt (blob sizes, 256 B slot rounding as generate.cpp:2405), dense bytes per stage (dense_per_stage.py),
free VRAM measured by pcie_probe (CUDA0 22.44, CUDA1 23.29 GiB with a CUDA context up), session KV/rope/pooled sizes
from layer.cpp:512-545 (int8: 1056 B/cell/layer + 128 B pooled + 1 B page table; rope 256 B/cell per stage),
reserve 1536 MiB (configs), windows 96 MiB, MTP drafter rt 0.77 GiB + head on CUDA1, + 0.30 GiB unaccounted (eager
modules, cuBLAS, scratch).  Everything here is arithmetic, not a measurement of the engine."""
GiB = 1 << 30
packs = {
    "iq2-xs": ("/home/diogo/models/strata/packs/iq2-xs/native_experts.txt", 2.872, 0.41),
    "orca-iq3-xxs": ("/home/diogo/models/strata/packs/orca-iq3-xxs/native_experts.txt", 2.589, 0.41),
    "uncensored-iq4-xs": ("/home/diogo/models/strata/packs/uncensored-iq4-xs/native_experts.txt", 2.978, 0.49),
}
def per_layer(path):
    out = {}
    for line in open(path):
        if line.startswith("#") or not line.strip():
            continue
        f = line.split()
        out[int(f[0])] = (int(f[4]) + 255) // 256 * 256 * 512
    return out
def session(ctx, qsa_layers=6):
    return (qsa_layers * ctx * (1056 + 128 + 1) + ctx * 256 + ctx * 4) / GiB + 0.05
for name, (path, dense, head) in packs.items():
    lay = per_layer(path)
    lo = sum(v for l, v in lay.items() if l < 24) / GiB
    hi = sum(v for l, v in lay.items() if l >= 24) / GiB
    for ctx in (32768, 262144, 1000000):
        if name != "iq2-xs" and ctx != 32768:
            continue
        s = session(ctx)
        c0 = 22.44 - dense - s - 1.5 - 0.094 - 0.30
        c1 = 23.29 - dense - s - 1.5 - 0.094 - 0.30 - 0.77 - head
        print(f"{name:18s} ctx={ctx:>7d}: experts L0-23 {lo:5.2f} GiB, L24-47 {hi:5.2f} GiB | cache room CUDA0 ~{c0:5.2f}, "
              f"CUDA1 ~{c1:5.2f} GiB | share of bytes resident ~{min(1,c0/lo)*100:4.0f}% / {min(1,c1/hi)*100:4.0f}% | "
              f"non-resident (RAM/page cache) ~{max(0,lo-c0)+max(0,hi-c1):5.2f} GiB | +dup dense if carved ~{dense/2:4.2f} GiB/GPU")
