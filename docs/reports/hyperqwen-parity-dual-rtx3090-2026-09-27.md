# Paridade HyperQwen × `syv-qwen38` e re-tune dos profiles — dual RTX 3090

**Data:** 2026-09-27 · **Upstream:** [syv-ai/HyperQwen](https://github.com/syv-ai/HyperQwen) `da8a8e9`
(antes: `1cf8665`) · **Hardware:** 2× RTX 3090 24 GiB, PCIe PHB sem NVLink, P2P OK (driver 610.57.04),
**270 W por placa** · **Evidências brutas:** `~/.local/state/model-loader/benchmark/hyperqwen-parity-20260927/`

Todas as medições desta rodada são a 270 W. Os números de 2026-09-07 e 2026-09-23/25 foram a 290 W e
não são comparáveis diretamente; cada A/B abaixo tem base e arm medidos na mesma sessão, na mesma
potência.

---

## 1. Conclusões

1. **Patches: paridade completa.** Entre `1cf8665` e `da8a8e9` entrou um patch novo,
   `spec-attn-smem-fit` (o verify split-KV reduz o tile de KV quando o Triton reporta
   `OutOfResources`; alvo é Turing/sm75). Aplicado ao venv, `verify.sh --install` → `OK (0 failures)`.
   O restante do drift é `prepare/` (publicação atômica #195–#203, grupos de heads simétricos #197),
   `verify.sh` e docs. Os checkpoints locais já declaram grupos de heads `symmetric: true`, inclusive o
   AWQ assimétrico abliterated. Árvore local = upstream `da8a8e9`, exceto
   `prepare/repack_gptq_ct.py`, criado por outro agente nesta data.
2. **A lacuna real estava no launcher, não nos patches.** `syv-serve.sh` não executa
   `single-user/start_qwen.sh`. O launcher upstream, em TP>1, liga o custom all-reduce e define
   `PYTORCH_CUDA_ALLOC_CONF=expandable_segments:False`. Os profiles locais tinham
   `disable-custom-all-reduce: true` + `expandable_segments:True`.
3. **Custom all-reduce funciona em SM86 e é o maior ganho da rodada: +4–15% de decode em todos os
   modelos.** A regra antiga ("custom AR quebrado em SM86") atribuía a falha ao lugar errado.
   Reproduzido hoje: custom AR + `expandable_segments:True` →
   `Cuda error /workspace/csrc/custom_all_reduce.cuh:164 'invalid argument'` → `EngineCore failed to start`.
   É o `cudaIpcGetMemHandle` do buffer de grafo, e um segmento VMM expansível não tem handle IPC
   (gotcha 3 upstream, issue #163). Com `expandable_segments:False`, os 11 profiles sobem com
   `Using ['CUSTOM', 'PYNCCL'] all-reduce backends`.
4. **Knob morto removido:** `VLLM_V2_CUDAGRAPH_MEM_MIB` não tem leitor no vLLM 0.29 (o boot avisa
   `Unknown vLLM environment variable`; upstream `docs/vllm-0.29.md` confirma a aposentadoria).
5. **MiMo estava sem `draft_sample_method`** (`draft_logits=False`) e com `QMAX=16` para k=7.
   Corrigido para `probabilistic` e `QMAX=8` (k+1).
6. **Rejeitados com medição:** `custom_ops +rms_norm,+silu_and_mul` (default do launcher upstream),
   FlashInfer sampler, INT8-MLP e MTP k=4 no turbo-heretic, GDN float16 no Ornith.

---

## 2. Interferência de outro agente (e como foi tratada)

Entre 17:08 e 17:28 (BRT), outro agente usou o mesmo proxy: carregou `hemmingway-1-w4a16-mtp-syv-tp2-256k`
(que falha no load com `AttributeError: 'MergedColumnParallelLinear' object has no attribute 'data'`) e
`ornith-1.5-35b-a3b-autoround-dflash2-syv-tp2-256k`, além de reescrever shards de
`syvai/Hemmingway-1-W4A16` em disco. Auditoria por arm, contando `POST /v1/chat/completions` no log de
cada instância contra as requisições que o harness enviou:

| Arm | Situação | Destino |
|---|---|---|
| base, car (run 1), cops, mimo-dsm, th-base | contagem de requisições = a do harness, sem troca de backend | válidos |
| mimo-base | sem requisição alheia, mas em paralelo com a reescrita de shards (17:10–17:11): `sampled-code` 260→201→182 tok/s entre reps | descartado, remedido |
| th-int8 | preemptado pelo load do Ornith às 17:26:53 | descartado, remedido |
| car soak | inválido por bug meu: o filler subestimava tokens em ~46% e estourou 262 144 → HTTP 400 | refeito com calibração via `/tokenize` |

As linhas `GET /slots` (404) no log eram do monitor da TUI e não geram trabalho de GPU. O harness agora
aborta se `loaded_pid` mudar no meio de um arm e grava `foreign_requests`. Todos os arms remedidos
saíram com `foreign_requests: 0` e sem troca de backend.

**Achado fora do meu escopo:** os 4 profiles `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-pipeline-b2048-192k*`
existiam às 16:31 (estão em `profiles-before/`) e foram apagados por outra sessão. Não os restaurei.

---

## 3. A/B medidos (270 W, harness `parity_bench.py`)

Mediana de 3 reps (100k: 1 rep). `st/s` = decode ÷ tokens por passo. Esse valor isola a velocidade do
passo quando o arm muda o caminho de saída e, com ele, a aceitação.

### 3.1 Qwen3.8-27B Sharp (`...-sharptmpl-256k`): custom all-reduce

| Workload | NCCL (base) | custom AR | Δ |
|---|---|---|---|
| decode-code | 304,9 tok/s | 329,3 | +8,0% |
| decode-prose | 149,4 | 157,0 | +5,1% |
| sampled-code | 264,9 | 283,6 | +7,1% |
| edit-copy | 377,2 | 397,6 | +5,4% |
| TTFT 32k | 22,91 s | 22,21 s | −3% |
| 100k (st/s) | 26,9 | 28,0 | +4% |
| KV pool | 311 150 | 316 642 tokens | +1,8% |

Soak: prompt de 243 075 tokens + 2×29,9k concorrentes, todos completos, sem OOM (TTFT 249 s sob
contenção). GSM8K 29/30, cópia exata, tool call shell-hostile exato. O run 1 do mesmo arm deu
+3,5…+8,7%.

Rejeitados sobre o mesmo base: `custom_ops +rms_norm,+silu_and_mul` → −2,1 / −7,8 / −5,0% e −16% a
100k. FlashInfer sampler (sobre o custom AR) → st/s −2,8 / −0,2 / +1,0%, ganho aparente em
`sampled-code` só por outra trajetória de amostragem.

### 3.2 Turbo-heretic (MTP k=3)

| Workload | base | custom AR | INT8-MLP | k=4 |
|---|---|---|---|---|
| decode-code | 191,8 | **199,0** | 164,1 | 191,2 |
| decode-prose | 134,8 | **139,9** | 121,3 | 129,9 |
| sampled-code | 171,3 | **177,3** | 157,5 | 167,0 |
| edit-copy | 176,8 | **186,1** | 169,6 | 193,6 |
| TTFT 32k | 30,49 s | 30,48 | **22,96** | 30,98 |
| 100k | 83,5 | **88,8** | 86,6 | 84,9 |

Custom AR: saídas greedy byte-idênticas ao base, GSM8K 30/30, tools exato. INT8-MLP corta 25% do TTFT
frio, mas perde 4–15% de decode, e o prefix cache amortiza o prefill em uso agêntico. Não adotado. k=4
só ganha em cópia.

### 3.3 MiMo 9B (DFlash v1 k=7)

| Workload | base | +probabilistic +QMAX 8 | + custom AR |
|---|---|---|---|
| decode-code | 451,3 | 463,2 | **483,7** |
| decode-prose | 159,1 | 160,9 | **171,0** |
| sampled-code | 242,8 | 277,9 | **296,3** |
| edit-copy | 451,9 | 478,6 | **506,5** |
| 100k | 81,2 | 101,4 | **102,0** |

GSM8K n=100: 92/92/92. O probe de tool call devolve `run_shell` com `arguments: {}` em 3/3 repetições
**nos três arms, inclusive no base**. O problema é do modelo/parser, anterior a esta mudança, e está em
aberto.

### 3.4 Ornith 35B-A3B (DFlash2 k=7) e Nex 35B-A3B (sem spec)

| Workload | Ornith base | Ornith custom AR | Nex base | Nex GDN-fp16 | Nex GDN-fp16 + custom AR |
|---|---|---|---|---|---|
| decode-code | 281,2 | **299,2** | 174,7 | 182,8 | **200,3** |
| decode-prose | 186,2 | **210,8** | 174,4 | 179,1 | **199,6** |
| sampled-code | 272,0 | **285,7** | 169,4 | 169,6 | **192,1** |
| edit-copy | 596,1 | **648,6** | 179,8 | 181,0 | **199,5** |
| TTFT 32k | 6,65 s | 6,60 | 6,42 | 6,41 | **6,04** |
| 100k | 91,5 | **102,2** | 134,7 | 133,0 | **146,8** |
| KV pool | 509 449 | 568 803 | 961 194 | 975 519 | 973 907 |
| GSM8K n=100 | 94 | 95 | 95 | — | 93 |

GDN float16 no Ornith: st/s −0,4…−3%, sem ganho em `max-num-seqs 1`, rejeitado. No Nex, GDN float16
sozinho ficou em 29/30, igual ao base, com n=30. O 95→93 do arm final está dentro do ruído de n=100
(erro-padrão de ~2,3 pontos).

---

## 4. Estado aplicado (import `--mode overwrite`, 11 profiles)

Comum a todos: `disable-custom-all-reduce: false`,
`PYTORCH_CUDA_ALLOC_CONF=expandable_segments:False`, `VLLM_V2_CUDAGRAPH_MEM_MIB` removido.
Os profiles com arm medido foram verificados por igualdade de `args`/`env` contra o arm.

| Profile | Mudanças extras | Boot (KV pool) | Smoke GSM8K 30 / tools |
|---|---|---|---|
| `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-256k` | — | 316 642 | 29 / exato |
| `qwen3.8-27b-w4a16-dflash2-syv-tp2-256k` | — | 316 642 | 29 / sem call¹ |
| `qwen3.8-27b-abliterated-...-sharptmpl-256k` | — | 307 559 | 29 / exato |
| `qwen3.8-27b-abliterated-...-256k` | — | 307 559 | 30 / sem call¹ |
| `qwen3.8-27b-uncensored-...-sharptmpl-256k` | — | 307 559 | 29 / exato |
| `qwen3.8-27b-uncensored-...-256k` | — | 307 559 | 28 / sem call¹ |
| `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-pipeline-192k` | — | 318 151 | 29 / exato |
| `qwen3.8-27b-turbo-heretic-w4a16-mtp-syv-tp2-256k` | — | 322 607 | 30 / exato |
| `mimo-v2.6-distill-qwen-9b-awq-dflash-k7-syv-tp2-256k` | `draft_sample_method: probabilistic`, `QMAX 16→8` | 799 147 | 27 / args vazios² |
| `ornith-1.5-35b-a3b-autoround-dflash2-syv-tp2-256k` | — | 568 803 | 28 / exato |
| `nex-n2.5-mini-35b-a3b-autoround-syv-tp2-256k` | `mamba-ssm-cache-dtype: float16` | 973 907 | 29 / exato |

¹ Template original com thinking ligado gasta os 512 tokens do probe raciocinando. O mesmo resultado
aparece nos arquivos de 2026-09-23 (`hyperqwen-029-20260923/029-canon-tools.json`, `sw-*-256k-tools.json`),
anteriores a esta mudança. Os profiles Sharp-template passam.
² Anterior à mudança (§3.3).

Wrapper `backends/syv-qwen38/syv-serve.sh` passou a espelhar o default do launcher upstream:
`expandable_segments:False` quando TP>1 sem `--disable-custom-all-reduce`/`--enforce-eager`; um
`PYTORCH_CUDA_ALLOC_CONF` explícito no profile continua vencendo. Testado por simulação em 7
combinações de args.

Profile ativo restaurado: `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-256k` (proxy `:4321` respondeu,
`CUSTOM` no boot, GPU0 22,2 GiB / GPU1 20,7 GiB).

---

## 5. Não coberto / em aberto

- Faixas de 50–90% de fill do KV e concorrência C4/C8 com custom AR não foram remedidas; o soak cobre
  um prompt de 243k com 2 concorrentes.
- Os profiles derivados (fast, abliterated, uncensored, pipeline) herdaram o ganho medido no Sharp.
  Neles foram provados boot, `CUSTOM` e smoke de qualidade, não velocidade.
- GSM8K n=30/100 é gate de quebra grosseira, não de diferença fina.
- Tool call da MiMo com argumentos vazios, anterior a esta mudança.
- `hemmingway-1-w4a16-mtp-syv-tp2-256k` (de outra sessão) não sobe; não mexi.

## 6. Reversão

- Profiles: `model-loader profile import ~/.local/state/model-loader/benchmark/hyperqwen-parity-20260927/before-apply.bundle.json --mode overwrite`.
- Backend: `UPSTREAM_COMMIT.before` + `backend-src-1cf8665.tgz` + `spec_decode_attn.py.before` no mesmo diretório.
- Arms A/B apagados do disco: `ab-arms.bundle.json`.
