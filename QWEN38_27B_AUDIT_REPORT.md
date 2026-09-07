# Auditoria Qwen3.8-27B / `syv-qwen38` — dual RTX 3090

**Data:** 2026-09-07 · **Referência:** [syv-ai/qwen38-27b-rtx3090](https://github.com/syv-ai/qwen38-27b-rtx3090) `0e951951`
**Hardware:** 2× RTX 3090 24 GiB, PHB via PCIe (sem NVLink), 290 W por placa, TP=2
**Evidências brutas:** `~/.local/state/model-loader/benchmark/qwen38-audit-20260906/`

A referência upstream é calibrada para **uma placa de 24 GiB com 64 requisições concorrentes**. Esta
máquina tem **duas placas e um operador**. Por isso várias receitas upstream foram medidas e
**rejeitadas**: elas não estão erradas, elas resolvem outro gargalo.

---

## 1. Conclusões

1. **O backend estava desatualizado.** vLLM 0.27.1 → **0.28.0** (torch 2.12.0+cu130 → 2.13.0+cu130),
   `UPSTREAM_COMMIT` 201d2378 → `0e951951`. O `dflash2-backport.patch` foi **aposentado**: DFlash2 é
   nativo no 0.28.0.
2. **Os paths estavam corretos**, mas o provisionamento não era confiável (§3).
3. **fp8 KV deixou de ser canônico.** No 0.28.0, `bf16` + `FLASH_ATTN` + verify split-KV entrega
   **+11,9% de decode** e **TTFT quente 2,5× menor** contra o mesmo perfil em fp8 (§4).
4. **INT8 de ativações restrito ao MLP é ganho real; em todas as camadas, não.** MLP-only mantém
   GSM8K **190/200** (igual ao W4A16) e corta 7,2 s do prefill de 44k. Todas as camadas caem para
   188/200 (§5).
5. **`VLLM_DFLASH2_LOOKUP=1` passou a valer a pena** neste stack: reprodução verbatim de contexto
   sobe de ~136 para ~210 tok/s, e o TTFT quente de 44k cai de 0,99 s para 0,49 s.
6. **Quantização real ≠ nome do checkpoint.** A variante *abliterated* servia `lm_head` e embeddings
   em BF16 (§6).
7. **Ganho líquido no profile principal: decode médio 180,2 → 193,3 tok/s (+7,2%)** e TTFT frio de
   44k de 27,96 s → 21,71 s, **sem reduzir contexto e com KV de maior precisão**.

**Profile recomendado:** `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-256k`.

---

## 2. Estado final aplicado (6 profiles `syv-qwen38`)

| Item | Valor |
|---|---|
| Backend | vLLM 0.28.0 patched, TP=2, `mp`, NCCL (custom all-reduce off) |
| Contexto | 262 144 · pool medido **286 013 tokens** (1,09× @ 256k) |
| KV cache | `bfloat16` |
| Atenção | `FLASH_ATTN` + `VLLM_SPEC_DECODE_ATTN=1` (verify split-KV) + `QMAX=8` |
| Estado GDN | `--mamba-ssm-cache-dtype float16` (conv state fp32) |
| Especulação | DFlash2 W4A16, `num_speculative_tokens=7`, `draft_sample_method=probabilistic` |
| Lookup | `VLLM_DFLASH2_LOOKUP=1`; chains **off** |
| Ativações | `VLLM_MARLIN_INPUT_DTYPE=int8` + `VLLM_MARLIN_INT8_INCLUDE_RE=mlp` (só checkpoints simétricos) |
| VRAM | `gpu-memory-utilization 0.82`, `VLLM_V2_CUDAGRAPH_MEM_MIB=1400` |
| Lote | `max-num-batched-tokens 2048`, `max-num-seqs 4` |

Confirmado no log de boot: `Using FLASH_ATTN`, `DFlash2 lookup-augmented drafting on (k=7 …)`,
`draft_logits=True`, `Using ['PYNCCL'] all-reduce backends`, `GPU KV cache size: 286,013 tokens`.

---

## 3. Backend, patches e paths

- **28 patches efetivos** de 30 arquivos (`dflash2-backport` aposentado; `spec-decode-scratch-token-units`
  substituído por `spec-decode-scratch-within-budget`) + **4 patches KVarN**.
  `PY=.venv/bin/python bash verify.sh --install` → **`verify: OK (0 failures)`**.
- **Paths conferidos:** modelos, drafter DFlash2, chat templates, wrapper, venv e executável — todos
  existem para os 7 profiles Qwen3.8-27B; `uv pip check` → 198 pacotes compatíveis.
- **`backend-build.sh` reescrito.** Dois defeitos reais:
  1. **Reinstalar a wheel não limpa arquivos adicionados por patch.** Sobreviventes do 0.27.1 faziam
     6 de 7 hunks do `speculator.py` falharem. O build agora **remove a árvore do pacote** antes de
     reinstalar.
  2. **Ordem de aplicação não era determinística.** Agora `LC_ALL=C`, com falha dura em hunk rejeitado
     e `verify.sh --install` como gate final.
- **Schema `syv-qwen38` atualizado** (378 flags) expondo os tipos KVarN instalados
  (`kvarn_k4v2_g128`, `kvarn_k4v4_g128`, `kvarn_k4v2_g64`, `kvarn_k4v4_g64`) — eles **não** existem no
  vLLM genérico.

---

## 4. Desempenho medido

3 prompts curtos × 2 repetições (média/mín/máx de decode, aquecido) + documento de 44 409 tokens
(TTFT frio e repetido, agulha verificada).

| Arm | decode médio | faixa | 44k frio | 44k quente |
|---|---:|---:|---:|---:|
| **Baseline como encontrado** — 0.27.1, fp8 | 180,2 | 166–203 | 27,96 s | 0,99 s |
| 0.28.0, fp8, mesmos args | 172,6 | 153–208 | 29,99 s | 1,05 s |
| 0.28.0, fp8 + lookup | 165,2 | 143–199 | 29,73 s | 1,04 s |
| 0.28.0, **bf16 + FLASH_ATTN + split-KV** + lookup | 192,5 | 174–224 | 29,28 s | 0,49 s |
| ... + INT8 em todas as camadas | 189,8 | 171–220 | **18,91 s** | 0,37 s |
| ... + **INT8 só no MLP** | 192,9 | 167–224 | 22,10 s | 0,42 s |
| **Final, carregado pelo `model-loader`** | **193,3** | 167–224 | 21,71 s | 0,40 s |

**fp8 é mais lento aqui porque força FlashInfer no SM86**, o que rebaixa o verify do DFlash2 para
grafos PIECEWISE e desliga o kernel split-KV (que é FLASH_ATTN/bf16-only).

**Arms rejeitados** (mesma bateria):

| Arm | decode | 44k frio | Motivo |
|---|---:|---:|---|
| GDN em bf16 | 187,7 | 18,93 s | habilita decode CUDA, sem ganho consistente |
| `max-num-batched-tokens 4096` | 191,1 | 18,42 s | ganho marginal; a 0,82 o KV não fecha 256k |
| `custom_ops +rms_norm,+silu_and_mul` | 184,1 | — | perda |
| KV `int8` | 180,3 | 34,01 s | mais lento que bf16 |
| KV `int4` | 177,2 | 33,29 s | mais lento e lossy |
| KV **KVarN** | 189,4 | 19,18 s | funciona, mas é lossy sem necessidade: bf16 já cabe |
| Drafter BF16 | ~148 (mediana) | — | contra ~165 do controle quantizado |
| MTP nativo k=4 | 154,4 | — | DFlash2 ganha em toda a faixa |

**Concorrência (prompts curtos):** 2 streams → **251,5 tok/s** agregados; 4 streams → **357,7 tok/s**.

---

## 5. Qualidade

| Teste | Resultado |
|---|---|
| GSM8K, 200 primeiros — W4A16 | **190/200** |
| GSM8K, 200 primeiros — INT8 só MLP | **190/200** |
| GSM8K, 200 primeiros — INT8 todas as camadas | 188/200 |
| Varredura de cópia verbatim, 128 comprimentos | **128/128 exatos** |
| Agulha a 249 807 tokens de prompt | correta a frio (TTFT 245,6 s) e quente (1,50 s) |
| Tool call + retorno da ferramenta | OK no profile principal |
| Smoke dos 6 profiles finais pelo `model-loader` | 6/6 aritmética + tool call |
| Proxy `127.0.0.1:4321` | OK |

**Regressões que barraram otimizações:**

- **`num_speculative_tokens=15`:** 459 tok/s na cópia (vs 304 do controle), mas **corrompeu o texto**
  (`"enabled": True,_00`). Rejeitado por correção, não por velocidade.
- **`VLLM_DFLASH2_CHAIN=1`:** `TimeoutError: RPC call to sample_tokens timed out` → `EngineDeadError`
  na bateria de matemática em TP=2. Rejeitado.
- **Custom all-reduce:** falha na inicialização de grafos; NCCL mantido.
- **`VLLM_PREFILL_ATTN=int8`:** o kernel upstream exige 24 heads Q / 4 KV; em TP=2 são 12/2, o
  predicado é falso. **Definir a variável não ativa nada** — não anunciar como ativo.

---

## 6. Quantização real (config + índices + headers safetensors)

| Checkpoint | Corpo | `lm_head` | Embeddings | MTP | Ação |
|---|---|---|---|---|---|
| `syvai/qwen3.8-27b-3090-fast-variant` | W4A16 g128 sym | INT4 | INT8 | INT4 | conforme a receita |
| `leminkozey/…-Uncensored-W4A16-AutoRound` | W4A16 g128 sym | INT8 | INT8 | BF16 | ok (o MTP não é usado pelo DFlash2) |
| `twolven/…-abliterated-AWQ-MTP` | AWQ **assimétrico** | BF16 | BF16 | BF16 | **requantizado em cópia** |
| `syvai/Qwen3.8-27B-DFlash2-W4A16` | W4A16 | — | — | — | correto |

Para o *abliterated* rodei `prepare/quant_heads_stream.py` upstream em uma **cópia**
(`…-abliterated-AWQ-MTP-syv-heads-int8`): heads/embeddings/MTP em INT8 **simétrico** g128, erro
relativo **0,69%** (`lm_head`) e **0,65%** (embeddings). O corpo AWQ assimétrico **não** foi tocado e
**as ativações INT8 ficam desligadas** nesse profile — misturar INT8 de ativação com corpo assimétrico
não foi validado. **O checkpoint original permanece intacto.**

---

## 7. Rota GGUF (`qwen3.8-27b-dirk-ud-q4kxl-tensor-mtp-256k`)

Testada isolada: **82,1 / 80,0 / 84,3 tok/s** — menos da metade da rota vLLM. Backend
`llama.cpp-stable` continua **fixado em b10686** (upstream já em v0.4.0); os patches Syv **não se
aplicam** ao llama.cpp. Mantido como está, por decisão de escopo.

---

## 8. Limitações

- **Não é prova de máximo global.** São arms de um knob por vez sobre o profile canônico.
- **A bateria completa de qualidade rodou no `fast`/Sharp.** Os outros checkpoints têm smoke,
  documento de 44k e tool call — não os 200 GSM nem as 128 cópias.
- **Faixas de 50–90% de preenchimento não foram remedidas no 0.28.0.** A calibração de 2026-09-01
  (0.27.1) media fp8 ganhando −23…39% nessas faixas; a decisão por bf16 aqui está apoiada em decode
  curto, prefill de 44k e agulha de 250k. **Se o uso for sessão de agente cheia, remedir antes de
  confiar.**
- **256k foi testado com um stream** (frio e quente). Concorrência só com prompts curtos.
- **GSM8K local com 200 problemas e prompt próprio** — não comparável ao número de manchete upstream.

---

## 9. Reversão

| O que | Onde |
|---|---|
| Backend anterior (0.27.1 completo) | `backends/syv-qwen38-backup-20260906/` |
| Profiles anteriores | `…/qwen38-audit-20260906/profiles-before.json` |
| Checkpoint abliterated original | `~/models/huggingface/twolven/Qwen3.8-27B-abliterated-AWQ-MTP` |

Restaurar profiles: `model-loader profile import <profiles-before.json> --mode overwrite`.
