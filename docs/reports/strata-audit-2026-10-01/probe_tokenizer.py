"""CPU-only baseline and bounded in-memory BPE cache prototype; no source changes."""
import functools
import hashlib
import json
from pathlib import Path
import statistics
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "backends/strata-fork/tools"))
from strata_tokenizer import Tokenizer


def main():
    assets = Path('/home/diogo/models/strata/tokenizers/iq2-xs')
    vocab = json.loads((assets / 'vocab.json').read_text())
    tokens = [None] * len(vocab)
    for text, index in vocab.items():
        tokens[index] = text
    merges = (assets / 'merges.txt').read_text().split('\n')
    types = json.loads((assets / 'token_type.json').read_text())
    tok = Tokenizer(tokens, merges, types)
    original = tok._bpe
    # Per-tokenizer lifetime; immutable results; exceptionally long pieces bypass cache.
    cached = functools.lru_cache(maxsize=16384)(lambda word: tuple(original(word)))
    def bounded(word):
        return cached(word) if len(word) <= 256 else original(word)
    workloads = {
        'code_2000_functions': ''.join(
            f'def calculate_{i}(value):\n    # Validação do resultado em português.\n'
            f'    return value * {i} + 7\n\n' for i in range(2000)),
        'portuguese_3000_records': ''.join(
            f'Registro {i}: análise de memória, desempenho e contexto; valor {i * 37}.\n'
            for i in range(3000)),
    }
    results = {}
    for name, text in workloads.items():
        timings = {'baseline': [], 'cache_cold': [], 'cache_warm': []}
        tok._bpe = original
        expected = tok.encode(text, parse_special=True)
        for _ in range(3):
            for arm in ['baseline', 'cache_cold', 'cache_warm']:
                tok._bpe = original if arm == 'baseline' else bounded
                if arm == 'cache_cold':
                    cached.cache_clear()
                before = time.perf_counter()
                actual = tok.encode(text, parse_special=True)
                timings[arm].append(time.perf_counter() - before)
                assert actual == expected
        results[name] = {'input_bytes': len(text.encode()), 'tokens': len(expected),
                         'sha256': hashlib.sha256(text.encode()).hexdigest(),
                         'seconds': timings, 'median_seconds': {k: statistics.median(v) for k,v in timings.items()},
                         'cache_info': cached.cache_info()._asdict(), 'exact_token_identity': True}
    (Path(__file__).parent / 'tokenizer-probe.json').write_text(json.dumps(results, indent=2))
    print(json.dumps(results, indent=2))


if __name__ == '__main__':
    main()
