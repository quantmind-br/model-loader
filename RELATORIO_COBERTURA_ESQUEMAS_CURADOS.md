# Relatório — Cobertura dos Esquemas Curados de Backend

**Data:** 2026-05-22
**Branch:** `feat/benchmark-engine-expansion`
**Âmbito:** Verificar se os esquemas curados (`internal/service/backendschema/curated_*.go` + `*help`) contemplam **todas** as configurações possíveis de cada backend, conforme a intenção original: *toda* flag deve ser configurável e validável; a curadoria deveria servir apenas para **organizar e explicar** a apresentação — nunca para limitar o conjunto de flags.

---

## 1. Veredito

**A intenção original está a ser violada.** Os esquemas curados não são uma camada de apresentação sobre o conjunto completo de flags — são um **allowlist rígido** que define *quais* flags existem para efeitos de configuração e validação. Tudo o que ficou de fora da curadoria é tratado como **erro bloqueante** e impede o arranque do perfil.

Resumo da cobertura (esquema **efetivamente servido** por cada gerador, comparado com a fonte autoritativa do backend):

| Backend | Flags curadas (servidas) | Fonte autoritativa | Cobertura | Em falta | Flags "estranhas" na curadoria |
|---|---|---|---|---|---|
| **llama-server** | 139 | 188 (`--help` v7376, golden) | **~74%** | **81** | 32 (aliases/renomeações/flags de fork) |
| **buun-llama-cpp** | 158 | ≥188 (upstream) + flags do fork | **<84%** | **73 (só do upstream)** | — |
| **vLLM** | 283 (≈229 reais + 52 espelhos `--no-*`) | 260 (`EngineArgs` + `cli_args`) | **~88%** | **31** | 0 (limpo) |
| **SGLang** | 380 | 404 (`ServerArgs.add_cli_args`) | **~78%** | **90** | **66** (contaminação de um fork de difusão) |
| **DFlash** | 15 | argparse de `scripts/server.py` | wrapper fino (subset por design) | n/d | — |

> Nota metodológica: contagens de llama/buun são *exatas* (diff interno contra o golden `testdata/help-v7376.golden.json`, 188 flags). As de vLLM/SGLang vêm de comparação contra o código-fonte upstream atual (`arg_utils.py`/`cli_args.py` e `server_args.py`). "Em falta" = flag aceite pelo binário mas ausente do esquema curado.

---

## 2. Como o sistema funciona hoje

### 2.1 Duas camadas de esquema por backend

1. **Esquema servido (a sério):** os mapas estáticos grandes em `internal/service/backendschema/curated_*.go`. Os geradores (`vllm_generator.go`, `sglang_generator.go`, `buun_generator.go`, `generator.go` para llama) devolvem **diretamente** estes mapas, sem fazer parsing de `--help` em runtime.
   - Exceção: o **DFlash** serve `dflashhelp.EmbeddedSchema()` (15 flags) diretamente, porque é lançado por um wrapper script.
2. **Fallbacks degradados:** os pacotes `internal/service/*help/` (`llamahelp`, `buunhelp`, `vllmhelp`, `sglanghelp`, `dflashhelp`) contêm esquemas minúsculos de 13–15 "essenciais", usados apenas quando o binário não pode ser resolvido/parseado. **Não são** o que o utilizador vê normalmente.

O `llamahelp` é o único que tem um **parser de `--help` real** (`exec_parser.go` + golden de 188 flags). Ou seja: o projeto **sabe** extrair o conjunto completo do llama-server — mas o gerador **não o usa**; usa o `CuratedLlamaSchema()` de 139 flags.

### 2.2 A curadoria é, por documentação própria, um subconjunto

O comentário de `CuratedLlamaSchema()` declara isto explicitamente:

> *"It intentionally includes **only flags that are relevant** to llama-server runtime/profile configuration."*

E o teste `TestCuratedLlama_ValidatesMTPProfile` documenta um incidente real causado por esta mesma filosofia:

> *"an earlier curation gap left 7 of them out and typed `n-gpu-layers` as string, so numeric profiles failed validation with **8 errors**."*

Isto é a confirmação de que a abordagem "curar = filtrar" já partiu perfis no passado.

---

## 3. Causa-raiz: a curadoria atua como allowlist bloqueante

O validador trata qualquer flag ausente do esquema como **erro**, tanto no formulário (`Args`) como na via de escape (`ExtraArgs`):

```
internal/service/validator/rules.go:17-23   (Args)
    spec, ok := schema.Lookup(canonical)
    if !ok { ... Message: "unknown flag (not in backend schema)", Severity: SeverityError }

internal/service/validator/rules.go:198-204 (ExtraArgs)
    if !ok { ... Message: "unknown flag in extra args (not in backend schema)", Severity: SeverityError }
```

E o erro é **bloqueante** e **impede o arranque**:

```
internal/service/validator/validator.go:32-33  HasBlockingErrors() == true sempre que há SeverityError
internal/ui/pages/profiles_launch.go:154-160    if rep.HasBlockingErrors() { return launchErrMsg{...} }  // launch abortado
```

**Consequência:** uma flag legítima do backend que não esteja na curadoria não é apenas "não apresentada" — torna o perfil **impossível de lançar**, mesmo quando o utilizador a passa manualmente em `ExtraArgs`. A via de escape não escapa ao esquema. Isto contradiz diretamente o objetivo "toda flag deve ser configurável".

---

## 4. Evidência por backend

### 4.1 llama-server — 81 flags em falta (139/188)

Categorias relevantes ausentes do `CuratedLlamaSchema()` (todas aceites pelo binário v7376):

- **Offload de MoE (crítico p/ modelos grandes):** `cpu-moe`, `n-cpu-moe`, `override-tensor`, `override-kv`, `cpu-moe-draft`, `n-cpu-moe-draft`, `override-tensor-draft`
- **Speculative/draft (nomenclatura upstream):** `model-draft`, `ctx-size-draft`, `device-draft`, `threads-draft`, `threads-batch-draft`, `n-gpu-layers-draft`, `draft-n`, `draft-n-min`, `draft-p-min`, `hf-repo-draft`, `cache-type-k-draft`, `cache-type-v-draft`, `spec-replace`
- **KV cache / defrag:** `kv-unified`, `defrag-thold`, `cache-ram`, `cache-list`
- **Slots / estado:** `slot-save-path`, `slot-prompt-similarity`, `swa-checkpoints`
- **Afinidade de CPU (variantes batch):** `cpu-mask-batch`, `cpu-range`, `cpu-range-batch`, `cpu-strict-batch`, `poll-batch`, `prio-batch`
- **Samplers:** `temp`, `top-nsigma`, `typical`, `xtc-probability`, `xtc-threshold`, `sampler-seq`
- **HF / auth:** `hf-token`, `hf-file-v`, `hf-repo-v`, `model-vocoder`
- **Distribuído / servidor:** `rpc`, `models-autoload`, `models-dir`, `models-max`, `models-preset`, `reasoning-format`, `rerank`, `reverse-prompt`, `special`, `spm-infill`, `media-path`
- **Logging:** `log-colors`, `log-disable`, `log-file`, `log-prefix`, `log-timestamps`, `log-verbose`, `log-verbosity`, `verbose-prompt`
- **Presets de conveniência:** `fim-qwen-*`, `gpt-oss-*-default`, `vision-gemma-*`, `embd-gemma-default`

### 4.2 buun-llama-cpp — 73 flags do upstream em falta (158)

O buun é um *fork* do llama.cpp e deveria ser um **superset** do upstream. No entanto, faltam-lhe 73 flags que existem mesmo no llama-server base — incluindo, surpreendentemente, núcleos como `flash-attn`, `cpu-mask`, `cpu-strict`, além de todo o bloco de MoE/draft/logging acima. Ou seja, o esquema do fork está **menos completo** do que o próprio upstream em vários eixos. (As mais-valias do fork — tipos KV `turbo*`, `spec-dflash-*` — existem no fallback `buunhelp`, mas convém garantir que estão também no `curated_buun.go` servido.)

### 4.3 vLLM — 31 flags em falta (~88%, o mais saudável)

O `curated_vllm.go` é o mais limpo: **zero** flags estranhas e ~88% de cobertura. As 283 entradas incluem ~52 espelhos `--no-*` legítimos (negações booleanas que o vLLM gera automaticamente). Lacunas mais relevantes para um operador:

- `tool-server`, `trust-request-chat-template`, `exclude-tools-when-tool-choice-none` (serving de ferramentas / chat templates por pedido)
- `enable-log-outputs`, `enable-log-deltas`, `log-config-file` (logging de produção)
- `quantization-config` (companheiro estruturado de `quantization`)
- `disable-hybrid-kv-cache-manager`, `dcp-kv-cache-interleave-size` (tuning de KV)
- `enable-tokenizer-info-endpoint`
- Bloco de *data-parallel supervisor* (6 flags de health-check para load-balancing multi-porta)

### 4.4 SGLang — duplo problema: 90 em falta **e** 66 estranhas (~78%)

1. **90 flags upstream em falta**, incluindo blocos inteiros importantes: SSL/TLS (`ssl-certfile`, `ssl-keyfile`, `enable-http2`), speculative/EAGLE/NGRAM avançado, DSA/linear-attn (DeepSeek-v3.2), CUDA-graph piecewise, expert-parallel/DeepEP, prefill context-parallel, priority scheduling (`default-priority-value`, `disable-priority-preemption`), `use-ray`, `radix-cache-backend`.
2. **66 flags na curadoria que NÃO existem no SGLang upstream.** A maioria pertence a um **fork de difusão/geração de vídeo** (estilo FastVideo): `vae-path`, `vae-precision`, `dit-precision`, `dit-cpu-offload`, `t5-config.*`, `flow-shift`, `embedded-cfg-scale`, `dmd-denoising-steps`, `warmup-resolutions`, etc. Há ainda knobs de paralelismo renomeados (`sp-degree`/`ulysses-degree`/`ring-degree` → hoje `attention-context-parallel-size`/`*-cp-mode`). Estas entradas inflacionam a contagem mas **validam flags que o `sglang.launch_server` real rejeita**.

### 4.5 DFlash — 15 flags (wrapper, subset por design)

O DFlash é lançado por um wrapper (`scripts/server.py`) e os seus 15 argumentos são curados à mão. É o caso onde "subconjunto curado" é legítimo, mas mesmo aqui convém validar contra o `argparse` real do `server.py` para não bloquear flags válidas.

---

## 5. Dois problemas colaterais descobertos

1. **Divergência de nomenclatura (afeta llama/buun):** mesmo conceitos "cobertos" usam, por vezes, uma chave diferente da que o binário aceita. Exemplos: curadoria tem `temperature`/`typical-p`/`top-n-sigma`/`spec-draft-model`, enquanto o binário usa `temp`/`typical`/`top-nsigma`/`model-draft`. Se não houver `alias` a mapear, o utilizador que escreva o nome real recebe "unknown flag" (bloqueante), e o nome curado pode não ser reconhecido pelo binário. **Requer auditoria flag-a-flag dos aliases.**
2. **Contaminação do esquema SGLang** com 66 flags de um produto diferente (difusão/vídeo). Isto não é só "ruído de apresentação" — são flags que passam na validação mas falham no binário real.

---

## 6. O que **não** é um gap (por design)

- **`model` / `model-path` / `target` ausentes** dos esquemas é **intencional**: o caminho do modelo vem de `Profile.Model` e é injetado no arranque por `processmgr.buildArgs`/`buildDFlashArgs` (`internal/service/processmgr/args.go:45,87,138,185`). Não deve ser uma flag configurável no formulário.
- Os fallbacks `*help` de 13–15 flags **devem** mesmo ser mínimos — são o último recurso degradado.

---

## 7. Recomendações

Alinhar o sistema com a intenção original — **validação completa, curadoria apenas de apresentação**:

1. **Separar "conjunto de flags" de "apresentação".** O conjunto de flags válidas deve ser o **completo** do backend; o `Presentation` (grupos/ordem/destaques) é que escolhe o que mostrar e como explicar. Hoje os dois estão fundidos no `Flags` map.
2. **llama/buun: gerar o conjunto completo a partir do parser que já existe.** O `llamahelp.exec_parser` já produz as 188 flags. Usar o `--help` parseado como base de `Flags` (mantendo a curadoria só como `Presentation`), com fallback para o embedded. Isto elimina os 81/73 gaps de uma vez.
3. **Não bloquear flags desconhecidas em `ExtraArgs`.** Rebaixar "unknown flag in extra args" de `SeverityError` para `SeverityWarning` (ou permitir um modo permissivo), para que a via de escape volte a ser uma via de escape. Caso contrário, qualquer flag nova do backend exige uma atualização de código antes de poder ser usada.
4. **vLLM/SGLang: completar a partir do upstream.** Idealmente gerar a partir de `EngineArgs`/`ServerArgs` (script offline que dá build do mapa), em vez de manter 280–380 entradas à mão.
5. **SGLang: remover as 66 flags do fork de difusão** e os knobs renomeados; corrigir a contaminação antes de adicionar as 90 em falta.
6. **Auditar aliases de nomenclatura** (`temp`↔`temperature`, `model-draft`↔`spec-draft-model`, etc.) para que o nome real do binário seja sempre aceite.
7. **DFlash:** validar a curadoria contra o `argparse` real de `scripts/server.py`.

---

## Anexo — Metodologia

- llama/buun: diff exato entre as chaves de `CuratedLlamaSchema()`/`CuratedBuunSchema()` e `testdata/help-v7376.golden.json` (188 flags), via harness Go temporário.
- vLLM: comparação das 283 chaves curadas contra `vllm/engine/arg_utils.py` + `vllm/entrypoints/openai/cli_args.py` (main).
- SGLang: comparação das 380 chaves curadas contra `python/sglang/srt/server_args.py` (`add_cli_args`, main).
- Severidade/bloqueio: rastreado em `validator/rules.go`, `validator/validator.go` e `ui/pages/profiles_launch.go`.
- Injeção do modelo: `processmgr/args.go`.
