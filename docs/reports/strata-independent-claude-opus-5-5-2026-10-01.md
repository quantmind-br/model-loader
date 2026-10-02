# Strata (fork) + Model Loader — auditoria independente

- **Data:** 2026-10-01
- **Autor:** Claude Opus 5.5 (`claude-opus-5-5`, effort xhigh), delegado pelo usuário, sem delegação a outros modelos
- **Natureza:** somente auditoria. Nenhum código de produção, profile, catálogo, peso, índice git ou configuração global foi alterado. Nenhum modelo real foi iniciado. Todos os artefatos próprios estão em `/tmp/strata-claude-independent-20261001/`.

---

## 0. Identificação, independência e escopo

### 0.1 Revisões e artefatos inspecionados

| Item | Valor |
|---|---|
| Fork Strata | `backends/strata-fork`, branch `fix/linux-pinning-request-race`, HEAD `97cb786c006ef1f8cdfec67ca7ef7b58f464323d` ("fix(cuda): register Linux arenas fully…"), 2 commits à frente de `upstream/main` = `origin/main` = `30ec18e` (Engine 0.1.30) |
| Alterações não commitadas no fork | `serve/server.py` (+39: `managed_config`, `--model`, `--max-context`), `tools/iq_pack.py`, `tools/test_iq_pack.py`; não rastreados: `backend-build.sh`, `serve/test_managed_config.py` (preservados) |
| Binário em uso | `build-fork-sm86/strata`, SHA-256 `545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999`, versão embutida apenas `0.1.30`; contém as strings introduzidas em `97cb786` |
| `serve/server.py` em uso | SHA-256 `c3d97162dd0668bc…` (diff idêntico ao de `/home/diogo/dev/Strata`) |
| Dependência llama.cpp/ggml do build | `_deps/strata_llamacpp-src` em `3cf03257f` (2026-09-20) |
| Model Loader | HEAD `adf12cb353acb41990c780d67d6a9dc3f0794189` + 18 arquivos modificados e `internal/service/stratahelp/`, `backendschema/strata_generator*.go` não rastreados (integração Strata em andamento, preservada) |
| Catálogo | backend `strata-fork`, kind `strata`, executável `/usr/bin/python …/strata-fork/serve/server.py` |
| Profiles Strata (campos `args`/`launch`/`model`) | 6 profiles em `~/.config/model-loader/profiles/*strata*.json` |
| Configs preparados do engine | 6 JSON + 6 `.shared-settings.json` em `~/.config/model-loader/strata/` |

### 0.2 Independência

Não li: `IDEATION_PERFORMANCE.md`, nenhum arquivo em `docs/reports/` (inclusive `docs/reports/strata-audit-2026-10-01/`), `docs/strata-backend.md`, `bench/results/` (do fork ou do Model Loader), diretórios de resultados em `/home/diogo/models/strata/{bench-*,calibration-*,context-*,iq4-tests-*,orca-iq3-xxs-*,uncensored-iq2-xs-*,research}`, memórias/handoffs, descrições/nomes/tags de profiles, nem os logs de engine em `~/.local/state/model-loader/logs/strata/` (apenas tamanhos via `ls`). Também não li os arquivos que o harness criou em `/tmp/strata-claude-independent-20261001/` (`task.md`, `stream.jsonl`, `stderr.log`, `help.txt`, `version.txt`). Listei nomes em `docs/reports/` apenas para evitar colisão. Li o `AGENTS.md` (orientação do repositório) e o `README.md` do próprio fork (documentação upstream, apenas para orientação; toda afirmação deste relatório foi verificada no código).

Todas as conclusões vêm de: código-fonte, configuração factual atual, observação somente-leitura do hardware e reproduções sintéticas próprias.

### 0.3 Classificação usada

- **Comprovado:** demonstrado por reprodução própria e/ou por leitura de código sem ambiguidade, com referência `arquivo:linha`.
- **Derivado:** consequência lógica do código/configuração/medições, mas sem reprodução ponta a ponta no engine real (proibido iniciar modelos).
- **Hipótese:** oportunidade cujo ganho depende de medição no engine real; sempre acompanhada de teste discriminante. Não atribuo ganhos numéricos não medidos.

---

## 1. Sumário executivo

| ID | Achado | Classe | Prioridade | Confiança |
|---|---|---|---|---|
| **C1** | Requisição **não-streaming**: desconexão do cliente não cancela o engine; a FIFO fica bloqueada até `max_new` (que, sem `max_tokens`, é o resto do contexto — até ~1M tokens no profile 976k) | Comprovado (reproduzido) | **Alta** | Alta |
| **C2** | O thread de serviço é fixado em `cpu0` antes de criar Stager/issuer/`std::async`/tier adaptativo/stdin/watchdog; no Linux todos **herdam afinidade `{0}`** e disputam um único CPU | Comprovado (código + semântica reproduzida); impacto no engine: hipótese | **Alta** | Alta (defeito) / Média (magnitude) |
| **C3** | `--mmap-experts` (todos os profiles dual) **desliga a fração PCIe do decode** e força cópias host→pinned (Stager) para 100% dos experts transmitidos no prefill; INFO/log reportam `pcie_frac 0.28` que nunca é usado | Comprovado (código) | **Alta** p/ Orca e IQ4_XS dual; baixa p/ IQ2_XS 32K | Alta |
| **C4** | Experts não residentes em VRAM ficam **frios**: `experts.bin` estava 0% no page cache; SSD dos modelos é um SM2263 DRAM-less atrás do chipset; miss frio custa ~1,48 ms por blob de 2,18 MB | Comprovado (medido) | **Alta** p/ primeira requisição após cada start | Alta |
| **C5** | `seed: 42` dos configs é injetado em **toda** requisição → Philox(42, posição) idêntico em todas as requisições (sem diversidade; e nem reprodutível, pela residência adaptativa) | Comprovado (reproduzido) | Média | Alta |
| **C6** | Campo OpenAI `stop` (e `ignore_eos`, `min_tokens`, `n`, `logprobs`) é ignorado silenciosamente | Comprovado (reproduzido) | Média-baixa | Alta |
| **C7** | Proveniência do build: `build-fork-sm86` foi configurado a partir de `/home/diogo/dev/Strata` (CMakeCache, `_deps`, CTest); binário só embute `0.1.30` | Comprovado | Média-baixa | Alta |
| **C8** | Em split multi-GPU **cada GPU carrega os pesos densos das 48 camadas**: ~1,28–1,50 GiB de VRAM por GPU para camadas que ela nunca executa | Comprovado (código + quantificação) | Média | Alta |
| D1 | `--prefill 512` nos profiles Orca fica abaixo de `stream_min` (1024) → ~16× mais bytes de experts por token no prefill do que com chunk 8192 | Derivado | Média-alta | Média |
| D2 | Em split, o prefill do drafter MTP usa o caminho por grupos (≤8 tokens/grafo), não o caminho em lote E-9 | Derivado | Média-baixa | Média |
| D3 | `TerminateTree` confirma só a morte do líder (Python); o engine filho pode ainda segurar VRAM quando o swap prossegue | Derivado (corrida) | Média | Baixa-média |
| D4 | `/health` responde 200 com engine morto; o Model Loader não detecta a queda e a próxima requisição paga o reload inteiro | Derivado | Baixa-média | Alta |
| D5 | Lacunas de telemetria/transparência: `reasoning_effort: none` injetado pelos `.shared-settings.json`, hit rate, drafts aceitos e reuso de prompt não chegam aos resultados do Model Loader | Derivado | Média | Alta |
| D6 | Tabela PLE (28,8 GB, lida com O_DIRECT a cada token/chunk) está no SSD lento; amostras indicam que é **idêntica** nos três modelos | Medido + derivado | Média-baixa | Média |
| D7–D12 | Orçamento de VRAM, parking de conversas sem split, P2P não usado, aviso de RAM inativo no modo mmap, nomes inconsistentes, YaRN no 976k | Derivado | Baixa | Média |

Oportunidade de maior alavanca (hipótese, seção 5.3): **O1 — complemento residente/pinned também com layer split**, que habilita DMA no prefill, fração PCIe no decode e elimina o risco de page cache frio/evicto para Orca e IQ4_XS dual.

---

## 2. Hardware verificado (somente leitura + micro-testes próprios)

| Componente | Fato | Fonte |
|---|---|---|
| CPU | AMD Ryzen 9 9950X3D, 16C/32T; CCD0 = cpus 0–7,16–23 com 96 MiB L3 (V-Cache), CCD1 = 8–15,24–31 com 32 MiB L3; irmãos SMT = N e N+16; AVX-512 F/BW/VL/DQ/VNNI/VBMI/BF16; governor `performance`, `amd_pstate` ativo, `amd_x3d_vcache` modo `frequency` | `lscpu`, sysfs |
| RAM | MemTotal 60,5 GiB; MemAvailable 44–46 GiB durante a auditoria; swap zram 16 GiB (5,7 GiB usados), `vm.swappiness=180`; THP `always` (defrag `madvise`); `HugePages_Total=0`; `ulimit -l` = 8 MiB | `/proc/meminfo`, `sysctl`, `swapon` |
| GPUs | 2× RTX 3090; `cudaMemGetInfo` total 23,56 GiB cada; livre medido 22,44 GiB (GPU0, com desktop ~874 MiB) e 23,29 GiB (GPU1); limite **270 W** em ambas (padrão 370/350 W); PCIe Gen4 x8 cada (link em Gen1 ocioso); topologia `PHB`; **sem NVLink**; P2P leitura/escrita `OK` e `cudaDeviceCanAccessPeer=1` nos dois sentidos; BAR1 32 GiB | `nvidia-smi`, `pcie_probe` |
| Driver/toolchain | NVIDIA 610.57.04 **open kernel module** compilado localmente (CUDA UMD 13.3); nvcc 13.4; gcc 16.2.1; CMake 4.4.3; Python 3.14.7; Go 1.27.1 local (go.mod 1.26.2); kernel 7.2.7 | `/proc/driver/nvidia/version` |
| H2D medido | pinned 13,34–13,36 GB/s por GPU (réplica exata de `probe_pcie_h2d_gbps`); simultâneo nas duas: **26,7 GB/s** (links independentes); pageable 10,09 GB/s | `pcie_probe.log` |
| Armazenamento dos modelos | `/home/diogo/models` = ext4 em **nvme0n1 "PCH-RVR-1TB" (controlador SM2263 DRAM-less) atrás do chipset** (06:00.0), 93% cheio (67 GB livres). O KC3000 ligado à CPU (02:00.0) hospeda `/` e `/home` (btrfs, 205 GB livres). `read_ahead_kb=128` | `findmnt`, sysfs, `lspci -tv` |
| SSD dos modelos medido | sequencial O_DIRECT 8 MiB: 2,51 GB/s (1 thread), 2,16 (4); aleatório 4K O_DIRECT: QD1 8,1k IOPS p50 131 µs; **16 threads (padrão do engine) 64,8k IOPS p50 235 µs p99 446 µs**; 64 threads 87,0k IOPS p50 633 µs; toque frio via mmap de blob de 2,18 MB: **1,48 ms** (1,47 GB/s) | `ple_randread.log`, `coldread.log` |
| Page cache | `experts.bin` dos três packs: **0%** residente no momento da auditoria | `mincore.log` |

Observação: o orçamento declarado de ≤23 GiB/card é compatível com `--vram-reserve-mib 1536` apenas se o consumo pós-cache couber na reserva (ver D7).

---

## 3. Configuração factual em uso

Resumo dos `args` efetivos (os profiles só passam `config`, `host`, `max-context`; o Model Loader acrescenta `--engine strata --model <GGUF> --port N`, `internal/service/processmgr/args.go:51-54`).

| Config | Pack / GGUF nativo | GPUs | Experts fora da VRAM | Prefill | Spec | Contexto | Sampling |
|---|---|---|---|---|---|---|---|
| IQ2_XS dual 32K / 256K | `packs/iq2-xs` (experts 33,0 GiB) | `[0,1]`, `layer_split "24"` | `--mmap-experts` | `auto` | `--spec 4 --spec-min-p 0.7` | 32768 / 262144, `--kv int8` | T 0,7, top-p 0,8, top-k 20, presence 1,5, **seed 42** |
| IQ2_XS dual 976K | idem | idem | idem | `auto` | idem | 1 000 000 + `--rope-scaling yarn --rope-scale 4 --yarn-orig-ctx 262144` | idem |
| Orca IQ3_XXS dual 32K | `packs/orca-iq3-xxs` (49,8 GiB) | `[0,1]`, split 24 | `--mmap-experts` | **512** | min-p 0,5 | 32768 | T 1,0, top-p 0,95, top-k 20, **seed 42** |
| Orca IQ3_XXS GPU1 residente 32K | idem | `[1]` | `--resident-experts` | **512** | 0,5 | 32768 | idem |
| IQ4_XS uncensored dual 32K | `packs/uncensored-iq4-xs` (60,9 GiB) | `[0,1]`, split 24 | `--mmap-experts` | `auto` | 0,7 | 32768 | como IQ2_XS |

Comuns: `--expert-cache auto`, `--expert-profile data/expert-profile.bin` (24 576 pares ranqueados), `--pool-workers 15` (igual ao padrão: núcleos físicos − 1), `--vram-reserve-mib 1536`, `--mtp /home/diogo/models/strata/mtp/rt`. Todos os `.shared-settings.json` contêm `{"reasoning_effort":"none"}`.

Efeitos implícitos derivados do código:
- `--spec 4` vira janela de verificação 6 (MTP até 4; drafter de sufixo +2), `src/program/generate.cpp:1379-1382`.
- `--ple-gguf` aponta para o arquivo que contém `per_layer_token_embd.weight` em cada caso (IQ2_XS: shard 2; Orca: shard 1; IQ4_XS: arquivo único) — verificado nos cabeçalhos GGUF; **configuração correta**.
- `managed_config` (`serve/server.py:541-568`) valida `--model` contra `--native` e substitui `--max-context` — correto e testado.

---

## 4. Fluxo de geração (derivado do código)

1. Model Loader → `python serve/server.py --engine strata --config … --model … --max-context … --port N` (processo líder do grupo, `Setsid`).
2. `server.py` monta `StrataEngine` → `strata --serve <args>` (filho no mesmo grupo), protocolo stdin/stdout `GEN max_new keys ids` → `T <id>`/`PP`/`DONE` (`serve/server.py:156-410`). Tokenização BPE em Python (`tools/strata_tokenizer.py`) e template Jinja do próprio modelo (`tokenizer/chat_template.jinja`).
3. Engine: pesos densos (pack `dense.bin` compactado + projeções nativas do GGUF) por GPU; sessão "carvada" por faixa de camadas (`src/core/session.cpp:53-128`); cache de experts por GPU preenchido pelo profile (`generate.cpp:2533-2608`); drafter MTP e head no último stage.
4. Decode: janelas de verificação (`src/core/verify.cpp`). Por camada: atenção/GDN na GPU → roteador → doorbell → (a) experts residentes na VRAM pela GPU; (b) misses: parte via PCIe (só se a origem for pinned) e o resto no pool de CPU (`expert_pool_dispatch_multi`, `src/core/expert_source.cpp:978-1187`). Quando todos os experts do grupo são residentes, o plano é feito no device sem ida ao host (E-6, `verify.cpp:620-624, 664-666`).
5. Prefill: chunks em lote; experts não residentes transmitidos para a GPU por anel (DMA se pinned; senão cópia host por threads `Stager`, `src/prefill/prefill.cpp:142-260`); em split, os stages encadeiam por memória host mapeada.

---

## 5. Achados

### 5.1 Defeitos comprovados

#### C1 — Requisição não-streaming não é cancelada quando o cliente desconecta
- **Prioridade:** Alta · **Confiança:** Alta · **Classe:** Comprovado (reproduzido)
- **Evidência:** no modo não-streaming a resposta só é escrita no fim (`serve/server.py:1670-1671` `openai_collect`; `:1699-1700` `anthropic_collect`); `cancel.set()` só ocorre quando uma **escrita** de streaming falha (`:1681-1683`, `:1711-1713`). Sem `max_tokens`, `max_new` = resto do contexto (`:959-964`). O proxy do Model Loader encaminha pedidos não-streaming como não-streaming (`internal/service/httpproxy/anthropic_translate.go:26`, `responses_translate.go:23`, `gemini_translate.go:15`). O benchmark do Model Loader sempre usa streaming (`internal/service/benchmark/client.go:96`) e não é afetado.
- **Reprodução própria** (`repro_nonstream_disconnect.py`, MockEngine a 50 tok/s, script de 300 tokens, cliente cai após 0,5 s):
  - streaming: engine parou em 25 tokens; requisição seguinte esperou **0,10 s**;
  - não-streaming: engine gerou **301** tokens; requisição seguinte esperou **5,65 s** (`repro_nonstream_disconnect.log`).
- **Impacto:** agentes/clientes não-streaming (Anthropic/Responses/Gemini via proxy, ferramentas com `stream:false`) que expiram ou são cancelados deixam o engine ocupado; como há uma única FIFO, todas as requisições seguintes esperam. No profile 976k, sem `max_tokens`, o pior caso é ~1M tokens.
- **Proposta:** (1) no caminho não-streaming, consumir o gerador num laço que, a cada heartbeat (`None`) e a cada N tokens, testa o socket (`select` + `recv(MSG_PEEK)` em `self.connection`) e faz `cancel.set()` + `close()`; (2) opcional: teto padrão de `max_new` para não-streaming (`default_max_tokens` no config) quando o cliente não informa.
- **Tradeoffs:** `MSG_PEEK` em HTTP/1.0 é simples; um teto padrão muda a semântica "sem limite" documentada.
- **Teste discriminante:** o próprio script de reprodução deve passar a mostrar ~25 tokens e ~0,1 s no caso não-streaming.

#### C2 — Threads auxiliares herdam o pin do thread de serviço em `cpu0`
- **Prioridade:** Alta · **Confiança:** Alta para o defeito; Média para a magnitude · **Classe:** Comprovado (código + semântica Linux reproduzida); impacto no engine = hipótese
- **Evidência:** `SessionLoopScratch::init` fixa o thread chamador em `physical_cores(false)[0]` = `cpu0` (`src/core/session.cpp:532-541`) e é chamado incondicionalmente antes do bloco `--serve` (`src/program/generate.cpp:3244-3255`); o escopo dura toda a sessão. Threads criados **depois**, pelo thread fixado:
  - `Stager` do prefill: 4 threads por stage (`src/prefill/prefill.cpp:188`, contagem em `:542-545`), criados em `Prefill::init` dentro do bloco serve (`generate.cpp:3583-3609`);
  - issuer do prefill por chunk (`prefill.cpp:1199-1201`), `std::async` do PLE (`:1110`) e do próximo chunk (`:1843`);
  - tier adaptativo a cada `adapt_every` janelas (`generate.cpp:4795-4798`, `5598-5601`), leitor de stdin (`:4009`), watchdog (`:4126`);
  - cópia do complemento residente (6 threads, `src/core/expert_source.cpp:696-701`, chamada em `generate.cpp:3431`).
  Os workers do pool de experts (`generate.cpp:2325`) e o I/O do PLE (`:1858`) são criados **antes** do pin e não são afetados. O pool em si é topologicamente correto (um worker por núcleo físico via sysfs, `src/kernels/cpu/pool.cpp:57-94`).
- **Reprodução própria:**
  - `affinity_inherit.log`: `std::thread` e `std::async` criados após `pthread_setaffinity_np({0})` têm afinidade `{0}` (antes: `{0-31}`).
  - cópia estilo Stager (blobs de 2,18 MB): 4 threads livres 26,0 GB/s vs. após pin 19,3 GB/s; com lançador em `yield()`: 51,2 vs 43,2 GB/s (1 stage) e 53,1 vs 45,7 GB/s (2 stages) — perda moderada de banda (`affinity_inherit*.log`).
  - **latência do lançador** (passos de 20 µs com `yield()`, como `Stager::wait`/issuer): sozinho 90 ms; com 4 copiadores CPU-bound no mesmo CPU **22 573 ms**; com 8, **45 043 ms** (`launcher_latency.log`). É um teste adversarial (copiadores nunca bloqueiam), que demonstra o mecanismo de fome, não a perda real do engine.
- **Impacto esperado (hipótese):** no modo mmap todos os experts transmitidos no prefill passam pelo Stager; os 4 (ou 8, com dois stages) copiadores + issuer + thread de lançamento disputam `cpu0`, ao lado do spin do host. Também atinge o tier adaptativo no decode (cópias pageable de até `adapt_swaps=96` blobs a cada 4 janelas).
- **Proposta:** capturar a máscara original no início de `main` (antes de qualquer pin) e, na entrada de cada thread auxiliar, aplicar uma máscara explícita: preferencialmente os irmãos SMT dos núcleos do pool (16–31) ou a máscara original sem `cpu0`. Alternativa mínima: restaurar a máscara original nos lambdas de `Stager::work`, issuer, `adapt` e `std::async` (substituir por `std::thread` com pin explícito).
- **Tradeoffs:** auxiliares em irmãos SMT dividem recursos de núcleo com workers do pool (aceitável: o pool é limitado por memória); deixar livre (`0-31`) pode colocar cópias sobre núcleos do pool.
- **Teste discriminante:** durante um prompt de 16–32K num profile dual mmap: `for t in /proc/$(pgrep -f build-fork-sm86/strata)/task/*; do grep -H Cpus_allowed_list $t/status; done` (espera-se ver vários threads com `0`) e `top -H -p <pid>`; depois A/B de `PP tok/s` e de tok/s de decode com o patch.

#### C3 — `--mmap-experts` desliga a fração PCIe do decode e o DMA do prefill; telemetria engana
- **Prioridade:** Alta para Orca/IQ4_XS dual; Baixa para IQ2_XS 32K (quase tudo residente) · **Confiança:** Alta · **Classe:** Comprovado (código)
- **Evidência:**
  - `FileExpertSource::pinned()` e `pcie_layer()` só retornam verdadeiro com o complemento residente pinned (`src/core/expert_source.cpp:827-844`). Em mmap puro: `pcie_ok=false` ⇒ `m=0` ⇒ **nenhum** miss vai para a GPU pela PCIe (`:1028-1029`); todos vão ao pool de CPU.
  - Prefill: experts não pinned viram jobs do Stager (`src/prefill/prefill.cpp:1144-1147`, `:1630`, `:1653-1659`); `pinned_share=0` ⇒ empréstimo de slots limitado a 85% e anel de 96 (`generate.cpp:3356-3366`, `3396-3400`, `prefill.cpp:86-95`).
  - Mesmo assim o probe PCIe roda e define `pcie_frac` (`generate.cpp:1552-1567`, `1978-1985`) e o `INFO` reporta `pcie_frac=…` (`:4101-4114`) — valor não efetivo. Com o hardware atual a fórmula daria 0,282 (medido 13,35 GB/s), mas no modo mmap o efetivo é 0.
  - O modo residente (`--resident-cpu-experts`/`--resident-experts`), que habilitaria pinned+DMA, é recusado com layer split (`generate.cpp:1272-1277`). Por isso todos os profiles dual estão em mmap.
- **Impacto (derivado):** pela estimativa da seção 6, ficam fora da VRAM ~14,8 GiB (Orca) e ~26,8 GiB (IQ4_XS). No decode esses misses só usam a CPU; no prefill tudo passa por memcpy host (10,1 GB/s pageable medido vs 13,35 GB/s DMA pinned) e pelo problema C2.
- **Proposta:** ver **O1** (complemento pinned com split). Correção imediata e barata de telemetria: reportar `pcie_frac` efetivo (0 quando `!src.pcie_layer(...)`), e não executar o probe quando a origem não é pinned.
- **Teste discriminante:** acrescentar `"env": {"STRATA_DECODE_TIMING": "1"}` ao config (aplicado por `child_env`, `serve/server.py:589-590`); o engine imprime por requisição `per layer-window: CPU experts …, VRAM hits …, PCIe …` (`generate.cpp:4850-4861`, contador em `expert_source.cpp:1084`). Esperado: coluna `PCIe` = 0,00 nos profiles dual mmap e > 0 no `gpu1-resident` para o mesmo prompt.

#### C4 — Experts não residentes chegam frios do SSD lento a cada start
- **Prioridade:** Alta para a primeira requisição após cada carga (inclui todo swap de profile) · **Confiança:** Alta · **Classe:** Comprovado (medido)
- **Evidência:** `FileExpertSource::open` faz `mmap(MAP_SHARED)` sem `madvise`/`fadvise` (`src/core/expert_source.cpp:407-426`); no start só são lidos os experts que vão para a VRAM (preenchimento pelo profile, `generate.cpp:2533-2608`). `mincore` mostrou os três `experts.bin` 0% no page cache (`mincore.log`). SSD: SM2263 DRAM-less atrás do chipset; blob frio de 2,18 MB em 1,48 ms (vs. ordem de 0,05 ms a ~40 GB/s quando quente — estimativa); sequencial 2,51 GB/s.
- **Impacto (derivado):** o primeiro prompt longo após o start transmite os experts não residentes inteiros (Orca ~14,8 GiB, IQ4_XS ~26,8 GiB) a ≤1,5–2,5 GB/s; misses frios no decode custam ~1,5 ms cada, multiplicados por camadas/janelas. Com `swappiness=180` e outras cargas, o cache não sobrevive entre sessões (observado: 0%).
- **Proposta:** (a) após o preenchimento do cache, disparar pré-leitura em background dos intervalos não residentes em ordem de arquivo (`posix_fadvise(WILLNEED)`/`readahead(2)`/toque multi-thread), com prioridade baixa; (b) ou adotar **O1**, que copia o complemento para RAM no start; (c) mover packs/tabela PLE para o KC3000 (decisão do usuário; espaço livre atual de 205 GB comporta o pack Orca de 52 GB ou o IQ2_XS de 35 GB).
- **Tradeoffs:** (a) aumenta o tempo até o page cache "esquentar" mas não o tempo até `READY`; (c) consome espaço no SSD do sistema.
- **Teste discriminante:** `./mincore …/experts.bin` imediatamente após `READY` e após o primeiro prompt; comparar `PP tok/s` do primeiro e do segundo prompt idêntico (sem reuso de cache de conversa: prompts distintos de mesmo tamanho).

#### C5 — `seed: 42` injetado em todas as requisições
- **Prioridade:** Média · **Confiança:** Alta · **Classe:** Comprovado (reproduzido)
- **Evidência:** `Service.run` mescla os defaults do config em toda requisição (`serve/server.py:1010-1013`); `sampling_keys` envia `seed=` (`:323-325`). O engine, sem seed, usaria um seed temporal por requisição (`generate.cpp:4620-4621`); com seed, o sorteio é Philox(seed, posição) (`:5326`).
- **Reprodução própria** (`repro_sampling_stop.log`): três requisições sem seed geraram a mesma linha `GEN 64 temperature=0.7 top_p=0.8 top_k=20 penalty_present=1.5 penalty_last_n=64 seed=42 cvec=0`.
- **Impacto:** (1) prompts idênticos → mesmo texto amostrado ("regenerar" não muda, retries de agentes repetem o erro); (2) prompts diferentes compartilham os mesmos uniformes por posição (correlação entre amostras de benchmark); (3) ainda assim **não há reprodutibilidade garantida**, porque a residência adaptativa muda entre requisições e GPU/CPU arredondam diferente (o próprio engine avisa, `generate.cpp:2508-2517`).
- **Proposta:** remover `seed` do bloco `sampling` dos configs (manter a opção por requisição).
- **Teste discriminante:** duas requisições idênticas com T=0,7 antes/depois da remoção; esperado: idênticas antes, diferentes depois.

#### C6 — `stop` e outros campos OpenAI ignorados
- **Prioridade:** Média-baixa · **Confiança:** Alta · **Classe:** Comprovado (reproduzido)
- **Evidência:** nenhum tratamento de `stop`, `ignore_eos`, `min_tokens`, `n`, `logprobs` em `serve/server.py`/`serve/frontend.py`. Reprodução: `stop:["STOPHERE"]` devolveu `"alpha STOPHERE beta"` integral (`repro_sampling_stop.log`). O probe de throughput do Model Loader já tolera `ignore_eos` ignorado (`internal/service/benchmark/llamabench_probe.go:133-138`).
- **Proposta:** implementar `stop` em `Service.run` (buscar no texto destokenizado; ao casar, truncar, `finish=stop` e encerrar o gerador, que envia `STOP` ao engine).
- **Teste discriminante:** o mesmo script; esperado `"alpha "` e `finish_reason=stop`.

#### C7 — Proveniência do build ambígua
- **Prioridade:** Média-baixa · **Confiança:** Alta · **Classe:** Comprovado
- **Evidência:** `build-fork-sm86/CMakeCache.txt` tem `CMAKE_HOME_DIRECTORY=/home/diogo/dev/Strata`, `FETCHCONTENT_BASE_DIR=/home/diogo/dev/Strata/build-fork-sm86/_deps`, `STRATA_PLE_FIXTURE_DIR=/home/diogo/dev/Strata/bench/micro`; `CTestTestfile.cmake` executa `/home/diogo/dev/Strata/build-fork-sm86/*`. O binário (21:57:05) antecede em 30 s o commit `97cb786` (21:57:35) e contém suas strings; versão embutida só `0.1.30` (`CMakeLists.txt:12`). As duas árvores têm o mesmo HEAD hoje, mas working trees diferentes (o fork tem mudanças em `tools/iq_pack.py`). `backend-build.sh` compila em `build-local-sm86` (inexistente hoje) e instala sobre `build-fork-sm86/strata`.
- **Impacto:** `cmake --build build-fork-sm86` ou `ctest` nesta árvore compilariam/testariam **outra** árvore; não há como provar, a partir do binário, de qual commit/estado ele veio.
- **Proposta:** embutir `git describe --dirty --always` em `STRATA_VERSION` e no `INFO engine=`; recriar o diretório de build a partir desta árvore; o Model Loader pode registrar `engine` (do `INFO`/`/props`) e o SHA-256 do executável nos metadados da instância.
- **Teste discriminante:** `strings strata | grep <hash>` e `GET /props` → `build_info`.

#### C8 — Pesos densos duplicados por GPU no layer split
- **Prioridade:** Média · **Confiança:** Alta (defeito) / Média (benefício) · **Classe:** Comprovado (código + quantificação)
- **Evidência:** cada stage aloca o arena denso inteiro e carrega todas as linhas do pack (`generate.cpp:1941-1947` → `WeightTable::load`, `src/core/weights.cpp:121-185`) e todas as projeções nativas elegíveis (`generate.cpp:1948-1951` → `NativeDense::load`, `src/core/native_dense.cpp:60-160`, sem filtro por camada). O mecanismo de `skip` com compactação já existe (`weights.cpp:169-181`).
- **Quantificação própria** (`dense_per_stage.py`, só cabeçalhos GGUF + `index.txt`, split 24):

| Pack | Denso total por GPU | Denso de camadas não executadas — GPU0 / GPU1 |
|---|---|---|
| IQ2_XS | 2,87 GiB | 1,41 / 1,46 GiB |
| Orca IQ3_XXS | 2,59 GiB | 1,28 / 1,30 GiB |
| IQ4_XS | 2,98 GiB | 1,47 / 1,50 GiB |

- **Impacto (derivado, seção 6):** para IQ2_XS 256K, os ~1,4 GiB por GPU provavelmente tornariam as duas metades 100% residentes (hoje ~98%/92% estimados); para Orca/IQ4_XS, ~+600/+560 slots por GPU — pelo próprio modelo de massa do engine ((r+1)^-1,2, `generate.cpp:2007-2019`) o ganho de massa roteada é da ordem de 0,2%, isto é, pequeno.
- **Proposta:** acrescentar ao `skip` de cada stage (e da CUDA0) os tensores `blk.N.*` com N fora de `[lb, le)`; filtrar `NativeDense::load` pela mesma faixa; garantir que `Verifier::init`/`Prefill::init` só resolvem pesos das próprias camadas (já que a sessão é carvada).
- **Tradeoffs:** mudança de inicialização em dois loaders; precisa de teste de paridade.
- **Teste discriminante:** `INFO expert_slots` e a linha "layer split: CUDA1 … expert cache N slots" antes/depois; paridade greedy token a token com a mesma residência (forçando o mesmo número de slots) antes/depois do patch; depois, com o cache maior, `STRATA_DECODE_TIMING=1` deve mostrar `CPU experts` por camada-janela menor no IQ2_XS 256K.

### 5.2 Defeitos e riscos derivados

#### D1 — `--prefill 512` nos profiles Orca
- **Prioridade:** Média-alta (prompts longos) · **Confiança:** Média
- **Evidência:** com chunk < `stream_min` (1024, `src/prefill/prefill.cpp:81-84`), o prefill transmite, por camada e por chunk, os experts roteados não residentes por um anel de 8 (`:72`, `:1625-1746`); a 512 tokens × 10 roteamentos por camada quase todos os 512 experts são tocados. Aritmética: ~14,8 GiB não residentes (dual, estimativa) ÷ 512 tokens ≈ 30 MiB/token vs ÷ 8192 ≈ 1,9 MiB/token (~16×). Tetos só de transferência: dual a 512 ≈ 430–860 tok/s (um ou dois links de 13,35 GB/s), a 8192 ≈ 6,9–13,8k tok/s; no profile GPU1 residente (~32 GiB não residentes) a 512 ≈ 200 tok/s.
- **Ressalva:** desconheço o motivo da escolha de 512 (pode ter sido memória/estabilidade); por isso é derivado, não comprovado.
- **Teste discriminante:** mesmo prompt de 16K com `--prefill 512` vs `--prefill auto`, comparando `PP` (tok/s) e estabilidade (sem `prompt allocation failed`).

#### D2 — Prefill do drafter MTP sem lote em split
- **Prioridade:** Média-baixa · **Confiança:** Média
- **Evidência:** `const bool batched = !multi_gpu && sp.draft_kv(...)` (`generate.cpp:3856`); em split cai em `MtpDrafter::prefill`, que processa grupos de ≤`max_t` tokens com um grafo cada (`src/core/mtp.cpp:679-724`): ~1,4 mil lançamentos por chunk de 8192, no último stage.
- **Proposta:** permitir `draft_kv` no `Prefill` do último stage (onde o drafter e as linhas R do chunk já estão).
- **Teste discriminante:** tempo de prefill por chunk com `STRATA_SNAPSHOT_VERIFY=1` (que imprime `path=token|batched`) antes/depois.

#### D3 — Parada do backend confirma só o líder
- **Prioridade:** Média · **Confiança:** Baixa-média
- **Evidência:** `procutil.TerminateTree` envia sinais ao grupo, mas espera apenas `Alive(pid)` do líder (`internal/service/internal/procutil/procutil.go:113-171`); o engine recebe SIGTERM junto e não tem handler. `StrataEngine.close()` faz `kill()` sem `wait()` se o pipe já quebrou (`serve/server.py:404-410`). Há janela em que o líder morreu e o engine ainda está liberando ~20+ GiB de VRAM em duas GPUs.
- **Proposta:** para `kind=strata` (e backends multiprocesso), confirmar que o grupo inteiro saiu (`kill(-pgid, 0)` → ESRCH) antes de liberar o swap; no Python, `wait(timeout)` também após `kill()`.
- **Teste discriminante:** parar uma instância via Model Loader registrando o instante de retorno de `Kill` e o de liberação de memória em `nvidia-smi --query-compute-apps=pid,used_memory --format=csv -lms 100`.

#### D4 — `/health` 200 com engine morto
- **Prioridade:** Baixa-média · **Confiança:** Alta
- **Evidência:** `/health` retorna 200 com `loaded=false` (`serve/server.py:1513-1516`); o reload acontece dentro da próxima requisição (`ensure_loaded`, `:697-730`). O monitor do Model Loader só checa `/health` e `/slots` (`internal/service/monitor/slots.go:50,75`).
- **Proposta:** no Model Loader, tratar `loaded=false` como degradado para `kind=strata`; ou, no servidor, 503 quando o engine morreu (e não foi descarregado de propósito).

#### D5 — Transparência de configuração e telemetria
- **Prioridade:** Média · **Confiança:** Alta
- **Evidência:** `.shared-settings.json` com `reasoning_effort: none` é aplicado a todo cliente que não informa esforço (`serve/server.py:789-809`, `1987-1995`) — o benchmark do Model Loader não envia esforço, então mede o modelo **sem raciocínio**, e isso não aparece no profile nem nos resultados. O `timings` final já traz `cache_n`, `draft_n`, `draft_n_accepted` (`:1127-1145`) e o `DONE` traz hits/lookups, mas o Model Loader só lê `prompt_per_second`/`predicted_per_second` (`internal/service/benchmark/client.go:279-282`). `INFO` também traz `expert_slots`, `kv`, `pool_workers`.
- **Proposta:** registrar no resultado do benchmark e nos metadados da instância: esforço efetivo, `cache_n`, aceitação de drafts, hit rate, `INFO` do engine.

#### D6 — Tabela PLE no SSD lento; provavelmente idêntica nos três modelos
- **Prioridade:** Média-baixa · **Confiança:** Média
- **Evidência:** leitura O_DIRECT com pool de 16 threads (`src/platform/direct_file.cpp:62-66, 313`); medido no SSD dos modelos: 64,8k IOPS com 16 threads, 87,0k com 64. Um chunk de 8192 tokens pede até 131k leituras de linha (16 por token, menos acertos do cache de 1M linhas). Amostra de 256 linhas (sha256) **idêntica** nos três GGUFs (`ple_same.log`) — forte indício, não prova, de que a tabela de 28,8 GB é a mesma.
- **Propostas (hipóteses):** `STRATA_IO_THREADS=32` ou `64` via `env` do config (mais IOPS, mais latência); uma cópia única da tabela no KC3000. Atenção: para Orca/IQ4_XS, apontar `--ple-gguf` para outro arquivo exige `--native-dense-gguf` explícito, senão o arquivo PLE é anexado aos shards densos e falha na validação de split (`generate.cpp:1395-1401`, `src/core/native_dense.cpp:81-90`).
- **Teste discriminante:** tempo `ms_ple` do prefill (stats do engine) e `PP tok/s` em A/B de `STRATA_IO_THREADS` e de local da tabela.

#### D7 — Reserva de VRAM consumida após o dimensionamento do cache
- **Prioridade:** Baixa · **Confiança:** Média
- **Evidência:** o cache automático deixa `vram_reserve_mib` livre (`generate.cpp:2354-2376`, `2485-2497`, `2037-2046`), mas depois ainda são alocados janelas (~75 MiB), handles cuBLAS do prefill, histórico de penalidade, bind do MTP. Com `cudaMemGetInfo` total de 23,56 GiB e reserva de 1,5 GiB, o uso em regime pode ficar perto de 22–22,5 GiB na GPU1 (estimativa) — dentro, mas próximo, do limite de 23 GiB declarado.
- **Teste discriminante:** `nvidia-smi --query-gpu=memory.used -lms 200` durante prompt de 32K + decode.

#### D8 — Parking de conversas indisponível com split
- **Evidência:** `generate.cpp:1193-1196` recusa `--conversation-cache-mib` com `--layer-split`. Cargas multiagente alternando conversas relêem prompts inteiros (checkpoints de prefixo ainda ajudam). Informativo/oportunidade.

#### D9 — P2P disponível e não usado nas passagens entre stages
- **Evidência:** passagens por memória host mapeada (`generate.cpp:3647-3659`; `verify.cpp:749-755`); `cudaDeviceCanAccessPeer=1`. ~51 KB por token; ganho esperado pequeno (≲1–2% no prefill). Baixa prioridade.

#### D10 — Aviso de RAM inativo no modo mmap
- **Evidência:** `INFO arena_mib` = bytes do complemento (0 em mmap) (`generate.cpp:4113`), e `warn_tight_ram` só avisa com `arena_mib>0` (`serve/server.py:1734-1751`). Justamente IQ4_XS dual (maior pressão de page cache) não recebe aviso.

#### D11 — YaRN no profile 976k
- **Evidência:** `rope_scaling` estático aplica-se a todas as posições (`generate.cpp:1656-1675`); com 1M células, KV+rope+pool ≈ 6,9 GiB por GPU (seção 6), deixando ~64% dos experts do IQ2_XS residentes. Usar este profile só quando >256K for necessário.

#### D12 — Nomes inconsistentes
- O profile `qwen3-8-flash-next-iq2-xs-…-32k` usa config/log/shared-settings `qwen3.8-flash-next-…-32k`. Funciona, mas dificulta correlação de logs.

### 5.3 Oportunidades de otimização (hipóteses, com teste discriminante)

| ID | Proposta | Mecanismo (fonte) | Afeta | Teste discriminante |
|---|---|---|---|---|
| **O1** | Complemento residente **pinned** também com layer split: passar os pares dos caches dos stages como `additional_gpu_pairs` (parâmetro já existente, `expert_source.cpp:500-575`), remover a recusa de `generate.cpp:1272-1277` só para esse caso, desabilitar/adaptar as trocas adaptativas (`resident_stage_swaps` usa o cache da CUDA0, `generate.cpp:115-143`) para camadas dos stages | `cudaHostAllocMapped|Portable` → DMA no prefill, fração PCIe 0,28 no decode, nenhum miss frio após o start | Orca dual (~14,8 GiB pinned), IQ4_XS dual (~26,8 GiB) | `PP tok/s` de prompt 16K e `STRATA_DECODE_TIMING=1` (ms/janela, CPU experts e PCIe por camada-janela) vs mmap; `majflt` do processo ≈ 0 |
| O2 | Corrigir C2 (afinidade) | ver C2 | todos, sobretudo mmap | ver C2 |
| O3 | Pré-leitura dos experts não residentes após `READY` (barata) | ver C4 | dual mmap | `mincore` + primeiro vs segundo prompt |
| O4 | Carvar densos por stage (C8) | +1,3–1,5 GiB de cache/GPU | IQ2_XS 256K (provável residência total), Orca/IQ4 (pequeno) | `INFO expert_slots`, hit rate |
| O5 | `--prefill auto` (ou ≥1024) nos profiles Orca (D1) | volume de streaming por token ÷ ~16 | Orca dual e GPU1 | `PP tok/s` |
| O6 | `STRATA_IO_THREADS` e local da tabela PLE (D6) | IOPS do SSD | prefill longo | `ms_ple` |
| O7 | `madvise(MADV_HUGEPAGE)` no mmap de experts / complemento em memória anônima com THP | menos TLB misses no pool de CPU | dual mmap | tok/s de decode com misses; `perf stat -e dTLB-load-misses` |
| O8 | Tokenização incremental do prefixo estável | medido 535k tok/s ⇒ ~0,2 s por turno de 100k tokens re-tokenizado (`tok_bench.log`) | agentes com contexto longo | TTFT por turno |
| O9 | `draft_kv` em lote no último stage (D2) | menos lançamentos | prefill dual | ver D2 |

### 5.4 Verificado e sem defeito

- Pool de CPU: um worker por núcleo físico (sysfs `core_id`), workers em `cpu1–15`, host em `cpu0`; não há dupla ocupação de SMT (`pool.cpp:57-94`, `session.cpp:532-541`).
- MMQ do prefill está compilado no build CUDA (a opção `STRATA_PREFILL_MMQ` é só para HIP, `CMakeLists.txt:37-39, 749-771`); tipos dos packs suportados.
- Decode com todos os experts residentes não faz ida ao host (E-6, `verify.cpp:620-624, 664-666, 697, 708-711`).
- `timings` do Strata são compatíveis com o parser do benchmark (`serve/server.py:1127-1145` × `client.go:279-282`).
- `--ple-gguf` aponta para o arquivo correto em todos os configs (verificado nos cabeçalhos).
- `managed_config`/`--model`/`--max-context` corretos (testes passam).
- Probe PCIe calcula corretamente para x8 (0,282) — o problema é só ser inefetivo em mmap (C3).
- `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES` do config (`serve/server.py:580-595`) mapeiam GPU0/GPU1 de forma estável.

---

## 6. Estimativas de residência e VRAM (derivadas, não medidas no engine)

Premissas (`residency_estimate.py`): livre medido CUDA0 22,44 GiB e CUDA1 23,29 GiB; densos da tabela C8; sessão = 6 camadas QSA × contexto × (1056 B KV int8 + 128 B pool do indexer + 1 B tabela de páginas) + 256 B/célula de RoPE + 0,05 GiB (`src/core/layer.cpp:512-545`, `session.cpp:53-72`); reserva 1,5 GiB; janelas 96 MiB; drafter MTP 0,77 GiB + head (0,41–0,49 GiB) na CUDA1; 0,30 GiB não contabilizados (módulos eager, cuBLAS, scratch); slots arredondados a 256 B (`generate.cpp:2405`). Erro esperado ±0,3–0,5 GiB por GPU.

| Pack / contexto | Experts camadas 0–23 / 24–47 | Espaço de cache CUDA0 / CUDA1 | Residente (bytes) | Fora da VRAM (RAM/page cache) |
|---|---|---|---|---|
| IQ2_XS 32K | 16,19 / 16,83 GiB | ~17,4 / ~17,1 GiB | ~100% / ~100% | ~0 |
| IQ2_XS 256K | idem | ~15,8 / ~15,5 GiB | ~98% / ~92% | ~1,7 GiB |
| IQ2_XS 1M | idem | ~10,8 / ~10,4 GiB | ~66% / ~62% | ~11,8 GiB |
| Orca IQ3_XXS 32K | 24,90 / 24,90 GiB | ~17,7 / ~17,4 GiB | ~71% / ~70% | ~14,8 GiB |
| IQ4_XS 32K | 30,47 / 30,47 GiB | ~17,3 / ~16,9 GiB | ~57% / ~55% | ~26,8 GiB |

Leitura: os achados de modo mmap (C3, C4, O1) quase não afetam IQ2_XS 32K, mas são centrais para Orca e IQ4_XS dual e para o 976k. Confirmação barata: linhas `expert cache auto … slots` e `layer split: CUDA1 runs layers … expert cache N slots` do log do engine (não consultadas por independência).

---

## 7. Testes e reproduções executados

Todos em `/tmp/strata-claude-independent-20261001/` (fontes, binários e logs). Python com `PYTHONDONTWRITEBYTECODE=1`; Go com `GOCACHE` no diretório temporário, `GOPROXY=off`, `GOFLAGS=-mod=readonly`. `git status` do fork e do Model Loader inalterados ao final.

| Teste | Tipo | Resultado |
|---|---|---|
| `repro_nonstream_disconnect.py` | reprodução própria (MockEngine) | C1 reproduzido: 301 vs 25 tokens; 5,65 s vs 0,10 s |
| `repro_sampling_stop.py` | reprodução própria | C5 (`seed=42` em toda linha `GEN`) e C6 (`stop` ignorado) reproduzidos |
| `affinity_inherit.cpp`, `affinity_inherit2.cpp`, `launcher_latency.cpp` | micro-testes de CPU | herança `{0}` comprovada; banda −15–26%; fome do lançador 90 ms → 22,6–45,0 s (adversarial) |
| `pcie_probe.cu` | micro-teste CUDA (256 MiB/GPU) | 13,35 GB/s pinned por GPU; 26,7 GB/s simultâneo; 10,09 GB/s pageable; P2P = 1 |
| `ple_randread.cpp` | leitura O_DIRECT aleatória (~160 MB) | QD1 131 µs; 16 thr 64,8k IOPS; 64 thr 87,0k IOPS |
| `coldread.cpp` | leitura sequencial O_DIRECT 1 GiB + 64 blobs via mmap | 2,51 GB/s; 1,48 ms/blob frio |
| `mincore.c` | residência de page cache (sem I/O de dados) | 0% nos três `experts.bin` |
| `ple_same.py` | sha256 de 256 linhas da tabela PLE nos três GGUFs | idênticas |
| `gguf_header.py`, `dense_per_stage.py` | só cabeçalhos GGUF + `index.txt` | tabela C8 |
| `residency_estimate.py` | aritmética | tabela da seção 6 |
| `tok_bench.py` | tokenizador real (arquivos do tokenizer) | ~535k tok/s |
| Fork: `serve/test_server.py` (53), `serve/test_mcp.py` (24), `serve/test_managed_config.py` (5), `serve/test_detok.py` (3 skip) | suíte existente | OK |
| Fork: `tools/test_iq_pack.py` (9, com `STRATA_GGUF_PY` = gguf-py do `_deps`), `test_calibrate` (10), `test_shards` (7), `test_setup_rope` (19), `test_conversation_cache_parity` (12), `test_conversation_cache_disabled` (6) | suíte existente | OK; `test_conversation_cache_disabled` falha 1 caso se executado fora da raiz do fork (caminho relativo ao cwd no teste), passa na raiz — artefato do teste |
| Fork: binários `arena_policy_test`, `prefill_startup_test`, `file_expert_source_test`, `pinned_shared_test`, `prefill_resources_test` | suíte existente model-free | todos OK |
| Model Loader: `go test` de `stratahelp`, `backendschema`, `processmgr`, `httpproxy`, `benchmark`, `domain`; `go vet` de `stratahelp`/`backendschema`/`processmgr` | suíte existente | todos OK; vet limpo |

Não executado (por autorização): nenhum modelo real, nenhum benchmark GPU pesado, nenhum build CMake (o binário existente foi analisado por `strings`/SHA-256).

---

## 8. Mapa de cobertura e limites

| Área | Cobertura | Arquivos principais |
|---|---|---|
| Arquitetura/fluxo, opções e startup | Alta | `src/program/generate.cpp` (1–2860, 3230–4140, trechos do loop serve) |
| Experts: mmap, complemento, dispatch CPU/PCIe | Alta | `src/core/expert_source.cpp`, `src/core/expert_cache.cpp`, `src/core/pinned.cu`, `src/platform/memory.cpp`, `include/strata/platform/arena_policy.hpp` |
| Pool de CPU e afinidade | Alta | `src/kernels/cpu/pool.cpp`, `src/core/session.cpp` |
| Multi-GPU (stages, cache por stage, split auto, hand-off) | Alta | `generate.cpp` 1197–2200, 2562–2610, 3464–3700 |
| Memória/VRAM/KV | Média-alta | `src/core/layer.cpp` 475–740, `src/core/session.cpp`, `src/core/weights.cpp`, `src/core/native_dense.cpp` |
| Prefill (Stager, streaming, MMQ, pipeline de inicialização) | Média-alta | `src/prefill/prefill.cpp`, `include/strata/prefill/startup.hpp` |
| Decode/verify | Média | `src/core/verify.cpp` (pre/post, E-6) |
| MTP/sampling | Média | `src/core/mtp.cpp` (prefill), seed/sampler no loop serve |
| PLE/I/O | Média | `src/platform/direct_file.cpp`, opções PLE |
| Servidor HTTP, tokenização, parsing, lifecycle, cancelamento | Alta | `serve/server.py`, `serve/frontend.py` (roles, esforço) |
| Integração Model Loader (schema, args, launch, kill, proxy, benchmark, monitor) | Alta | `stratahelp`, `strata_generator.go`, `processmgr/{args,launch,manager}.go`, `procutil.go`, `httpproxy/*_translate.go`, `benchmark/{client,llamabench_probe}.go`, `monitor/slots.go` |
| Build/testes | Média | `CMakeLists.txt`, `CMakeCache.txt`, `backend-build.sh`, suites executadas |
| Kernels CUDA (numérica de QSA/GDN/sampler/IQ), KV streaming, parking de conversas, remote experts, MCP, telemetria Python, `setup.py`/`calibrate.py`, visão, HIP, Windows | Baixa ou não auditado (fora da prioridade ou fora do caminho usado) | — |

Limites:
- Sem execução do engine real: nenhum tok/s, TTFT ou hit rate real; impactos de C2, C3, O1–O9 são hipóteses com testes discriminantes descritos.
- As residências da seção 6 são estimativas aritméticas; a VRAM livre foi medida com um contexto CUDA mínimo, não com o do engine (que carrega módulos eager).
- A identidade da tabela PLE foi verificada por amostragem (256 linhas), não byte a byte.
- O micro-teste de fome do lançador (C2) é deliberadamente adversarial.
- Não consultei logs de execuções anteriores, relatórios ou resultados de calibração (exigência de independência), o que impede confirmar números já observados pelo usuário.

---

## 9. Plano recomendado (ordem de ataque)

1. **C1** (cancelamento não-streaming) e **C5** (remover `seed` dos configs): baratos, sem risco de desempenho.
2. **C2** (afinidade dos threads auxiliares): patch pequeno; medir com o teste discriminante antes/depois em Orca dual.
3. **D1/O5**: A/B `--prefill auto` vs 512 nos profiles Orca.
4. **C4/O3**: pré-leitura dos experts não residentes após `READY` (ou mover packs para o KC3000).
5. **O1**: complemento pinned com split (maior alavanca para Orca/IQ4_XS dual; trabalho moderado).
6. **C8/O4**: carvar densos por stage (habilita provável residência total do IQ2_XS 256K).
7. **C3 (telemetria)**, **C6**, **C7**, **D3–D5**: correções de transparência, compatibilidade e ciclo de vida.

Para qualquer comparação de vazão, lembrar que as GPUs estão a 270 W (AGENTS.md §7) e que o SSD dos modelos está frio após cada start (C4): medir sempre o primeiro e o segundo prompt separadamente.
