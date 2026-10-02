import json, sys, time
from pathlib import Path
FORK = "/home/diogo/dev/model-loader/backends/strata-fork"
sys.path.insert(0, FORK + "/tools")
import strata_tokenizer as ST
tp = Path("/home/diogo/models/strata/tokenizers/iq2-xs")
t0 = time.time()
vocab = json.loads((tp / "vocab.json").read_text(encoding="utf-8"))
tokens = [None] * len(vocab)
for t, i in vocab.items():
    tokens[i] = t
tok = ST.Tokenizer(tokens, (tp / "merges.txt").read_text(encoding="utf-8").split("\n"), json.loads((tp / "token_type.json").read_text()))
print(f"tokenizer load {time.time()-t0:.2f}s")
src = (Path(FORK) / "src/program/generate.cpp").read_text() + (Path(FORK) / "serve/server.py").read_text()
for reps in (1, 4, 16):
    text = "\n".join(src for _ in range(reps))
    a = time.time(); ids = tok.encode(text, parse_special=True); b = time.time()
    ids2 = tok.encode(text, parse_special=True); c = time.time()
    print(f"{len(ids):>8d} tokens: first encode {b-a:6.2f}s ({len(ids)/(b-a):8.0f} tok/s), repeat {c-b:6.2f}s ({len(ids)/(c-b):8.0f} tok/s)")
