import hashlib, random, sys
sys.path.insert(0, '.')
from gguf_header import parse
files = {
 'iq2xs_shard2': '/home/diogo/models/huggingface/ISTA-DASLab/Qwen3.8-Flash-Next-GSQ-RCO-GGUF/IQ2_XS/Qwen3.8-Flash-Next-GSQ-RCO-IQ2_XS-00002-of-00002.gguf',
 'orca_shard1': '/home/diogo/models/huggingface/orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF/Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00001-of-00002.gguf',
 'iq4xs': '/home/diogo/models/huggingface/mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF/Qwen3.8-Flash-Next-Uncensored.i1-IQ4_XS.gguf',
}
loc = {}
for k, p in files.items():
    v, kv, tensors, hdr = parse(p)
    align = kv.get('general.alignment', 32)
    ds = (hdr + align - 1) // align * align
    t = [x for x in tensors if x[0] == 'per_layer_token_embd.weight'][0]
    loc[k] = (p, ds + t[3], t[4])
random.seed(7)
rows = sorted(random.sample(range(320001536), 256))
digests = {}
for k, (p, off, nbytes) in loc.items():
    h = hashlib.sha256()
    with open(p, 'rb') as f:
        for r in rows:
            f.seek(off + r * 90); h.update(f.read(90))
    digests[k] = h.hexdigest()[:16]
    print(k, 'table offset', off, 'bytes', nbytes, 'sample-256-rows sha256', digests[k])
print('identical samples:', len(set(digests.values())) == 1)
