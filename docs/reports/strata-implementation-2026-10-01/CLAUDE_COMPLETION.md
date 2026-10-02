# Continuação Claude — Strata / IDEATION_PERFORMANCE (2026-10-01)

Execução da tarefa de `CLAUDE_TASK.md` por Claude Code (`claude-opus-5-5`), na
workstation, com acesso efetivo às duas GPUs. Evidência bruta em
`claude-session/` (índice em `claude-session/NOTES.md`). Nada aqui substitui
o histórico de `IMPLEMENTATION.md`; os resultados das sessões anteriores
continuam válidos com as ressalvas originais.

## Status

**Atualização 2026-10-02 (§11):**

- O bloqueio S03 × tools foi resolvido pela opção (a). O server v6 honra
  `tool_choice` forçado com uma abertura de chamada forçada (exige thinking
  desligado) e `parallel_tool_calls=false` devolvendo só a primeira chamada
  completa.
- Gate de tools pré-registrado aprovado.
- **O IQ2_XS dual 256k canônico foi promovido** para a release v6 com
  `--stage-dense`. ID e contexto preservados, rollback arquivado, confirmação
  limpa.
- O 976k quase cheio está em §11.6.
- §12: o 976k foi substituído pelo baseline de 500k, validado quase cheio.
- §13: deploy do Model Loader feito.
- §14: S18 concluído, sem ganho de TTFT.
- §15: rerank no IQ3 GPU1-resident medido e não promovido.

Estado de 2026-10-01, preservado:

**Parcialmente concluído, com bloqueios registrados.** Os nove itens da
tarefa foram executados. Ainda há trabalho obrigatório pendente: a promoção,
que depende de uma decisão de política S03 × tools, e o 976k quase cheio,
bloqueado pelo guard global de swap por carga de terceiros. A retomada exata
está em §9.

- Gates completos Go/Python/nativo passaram, e a fixture dual foi executada
  nas duas GPUs. Duas releases imutáveis foram publicadas (v3, v4); a árvore
  atual não tem drift em relação à v4.
- Matriz concluída:
  - spec2/spec6 rejeitados;
  - PLE32 sem efeito;
  - PLE8 reconciliado;
  - IQ4 resident-stage-dense rejeitado (guard de swap na carga).
- IQ2 stage-dense 256k com prefixos novos aprovado em desempenho, memória e
  guards (9 starts no protocolo v2). **Não promovido** por bloqueio no gate de tools (S03).
- 976k: governador térmico opt-in implementado, testado e eficaz (≤82 °C);
  quase cheio não aprovado.
- Ranking por checkpoint treinado e validado em holdout (3+3 starts).
- Paridade CPU, máscaras de threads, `spec-min-p` isolado, Nsight, custo do
  MTP no prompt e A→B→A a 21k medidos.
- **Nenhum dos seis profiles canônicos foi alterado.** Foram removidos 15
  candidatos rejeitados, com arquivo prévio.

## 1. Acesso efetivo

Verificado às 19:19 (horário local), antes de qualquer teste:

- `nvidia-smi`: duas RTX 3090, 24.576 MiB, limite de 270 W por placa,
  896/15 MiB em uso (desktop na GPU0), 50/44 °C, nenhum processo de inferência.
  `/dev/nvidia*` acessível; NVML funcional.
- Proxy `127.0.0.1:4321` ativo, sem profile carregado. Ele também é exposto
  pelo caddy + cloudflared (API externa). Requests externos não foram observados
  durante as rodadas, mas não havia como bloqueá-los.
- RAM: MemAvailable 40,4 GB; swap zram/zstd com 6,2 de 16 GiB em uso;
  swappiness 180; THP `always`; `ulimit -l` 8 MiB. Nenhum lock de TUI.
- O binário instalado do proxy/CLI (`~/.local/bin/model-loader`, 05:56,
  `v0.1.0-41-gadf12cb-dirty`) é **anterior** às mudanças Go desta campanha.
  `make build` gerou apenas `bin/model-loader`; não rodei `make install` nem
  reiniciei o proxy. Por isso, S05/S07 do lado Go foram validados apenas por testes.

## 2. Gates e releases

| Gate | Resultado | Evidência |
|---|---|---|
| `go test ./...` | rc 0 (toolchain go1.27.1) | `gates/go-test.log` |
| `go vet ./...` | rc 0 | `gates/go-vet.log` |
| `make build` | rc 0 | `gates/make-build.log` |
| Python `serve/test*.py` | 94 testes, OK (5 skip) | `gates/python-serve.log` |
| Python `tools/test_*.py` (com `STRATA_GGUF_PY`) | 84 testes, OK | `gates/python-tools.log` |
| Guard do probe (`test_strata_probe_lifecycle`) | 1 teste, OK | `gates/python-probe-guard.log` |
| Nativo, release v3 | 40/40 ctest, incluindo `file_expert_source_dual_resident` **executada nas duas GPUs** (PASS, 0,58 s) | `gates/native-release-build.log`, `gates/native-dual-resident-verbose.log` |
| Nativo, release v4 | 41/41 ctest (acrescenta `thermal_gate_test`), fixture dual PASS | `gates/native-release-build-v4.log` |

`ple_parity` e `platform_memory_test` continuam excluídos (`external_fixture`:
fixtures externas ausentes e mlock acima de 8 MiB). **Não passaram e não são
declarados aprovados.**

Releases imutáveis publicadas por `tools/build_release.py`, ambas após os gates:

- **v3** `releases/20261001T222137Z-3430cc1a87e0`, catálogo `strata-perf-v3-20261001`,
  binário `310169d5…`. Fontes do engine (`src/`, `include/`) idênticas à release
  medida anteriormente (v2). O hash do binário difere mesmo assim (CMake, testes e ferramentas).
- **v4** `releases/20261001T223017Z-f85ddc896414`, catálogo `strata-perf-v4-20261001`,
  binário `a3c58bbe971914a27dba62d84fcfe7078207687656a2a7b8c542c5c6ee428cfd`.
  É a v3 mais o governador térmico opt-in do prompt (§3). A árvore atual do fork
  **não tem drift** em relação à v4: `source_identity()` foi comparada com
  `BUILD.json`. Python e Go não mudaram depois dos gates.

O catálogo `strata-fork` (baseline isolado) e o executável nativo original
não foram alterados.

## 3. Alterações desta sessão

**Engine (fork):** governador térmico do caminho do prompt, desligado por padrão.

- `include/strata/platform/thermal_gate.hpp`: parsing estrito (valor
  malformado desliga o gate e registra o motivo) e espera testável.
- `src/prefill/prefill.cpp`: leitura NVML por `dlopen` (mapeada pelo PCI bus
  ID, correta com `CUDA_VISIBLE_DEVICES`) e chamada antes de cada chunk, por stage.
- `src/platform/thermal_gate_test.cpp` + registro no CMake.
- Com `STRATA_PREFILL_TEMP_PAUSE_C=<C>`, o stage espera entre chunks enquanto
  a própria GPU estiver em C °C ou mais, até cair abaixo de
  `STRATA_PREFILL_TEMP_RESUME_C` (padrão C−3) ou atingir o teto
  `STRATA_PREFILL_TEMP_MAX_WAIT_S` (padrão 60 s por chunk). A espera continua
  cancelável. Não altera potência, clocks nem ventoinhas. Temperatura ilegível
  nunca gera espera. Sem a variável, o comportamento é o mesmo de antes.

**Model Loader:**

- `scripts/strata-performance-probe.py`: resfriamento passivo abaixo de 65 °C
  antes de **cada** fill fresco (`cooldown-fills.json`), aplicado a partir dos
  rótulos `ab256v2-*`. Os guards não mudaram.
- `scripts/strata-resource-sampler.py`: passa a registrar também os contadores
  `compact_*`, `pgmigrate*`, `allocstall*`, `kswapd*`, `thp_*` e `pageoutrun`
  (somente leitura), para atribuir o reclaim (S10).
- Catálogo: `strata-perf-v3-20261001` e `strata-perf-v4-20261001` adicionados.
- Novos candidatos (configs em `~/.config/model-loader/strata/perf-20261001/`):
  `…-resident-stage-dense-rerank-32k` (S13) e `…-resident-stage-dense-minp07-32k` (S15).
- Os candidatos usados passaram a apontar para a v4 (exe/log por tentativa).
  Os originais estão em `claude-session/config-backups/*.orig.json`.

**Ferramentas de evidência** (`claude-session/tools/`): `run_arm.py` (uma
tentativa isolada; config refeito a partir do backup, log do engine por
tentativa, captura de máscaras de threads, modo `--readonly` para o canônico),
`batch.py` (execução estritamente serial), `analyze.py` (ms/janela e custo
por slot, independentes da aceitação) e `compare.py` (braços).

## 4. Método desta sessão

- Inicialização exclusivamente por `model-loader instance start`, sempre via
  `scripts/strata-performance-probe.py`; inferência só pelo proxy.
  Uma carga por vez, em série (`batch.py`); nenhum build durante as medições.
- Guards inalterados no sampler: ≤23.552 MiB/placa, MemAvailable ≥2 GiB,
  crescimento global de swap ≤2 GiB, <85 °C, com três amostras consecutivas.
  Antes de cada start, resfriamento passivo abaixo de 65 °C.
- **Protocolo v2 (a partir de `ab256v2-*`):** também resfriamento passivo
  abaixo de 65 °C antes de **cada** fill fresco. É mais estrito que a regra e
  vale igualmente para todos os braços (motivo em §6.1).
- Métrica de engine: `ms/janela` e `ms por slot verificado` (ms/janela ÷ T
  médio), extraídos do `strata decode timing`. O tok/s de decode entre starts
  segue a trajetória do texto e a aceitação MTP, não o knob. Exemplos: o
  `code-0` fica em ~107–109 tok/s em todos os braços; starts idênticos da
  referência caem em duas trajetórias (108 e 116–118 tok/s). Nem com
  `STRATA_IQ_MT_MIN=1` a saída fica independente do start.
- Cada tentativa tem diretório próprio em `claude-session/runs/<rótulo>/`, com
  o config e o profile efetivos, o log completo do engine, o log da instância,
  requests/SSE, telemetria e as máscaras de threads.

## 5. Resultados

### 5.1 Matriz IQ3 restante (dual resident-stage-dense, v4, prefill 512)

Starts únicos, exploratórios, cercados por referências (`ref-a`/`ref-b`/`ref-c`):

| Braço | Código tok/s | ms/janela | tok/janela | Aceitação | PT tok/s | Swap +GiB | Decisão |
|---|---:|---:|---:|---:|---:|---:|---|
| Referência (spec4, PLE16), 3 starts | 116,6 / 116,4 / 118,5 | 28,2–29,3 | 3,34–3,43 | 0,86 | 71,7–75,8 | 0–1,04 | referência |
| PLE32 | 106,4 | 29,07 | 3,12 | 0,78 | 76,1 | 1,962 | sem efeito (trajetória) |
| spec2 | 98,7 | 19,45 | 1,92 | 0,89 | 76,6 | 0,012 | **rejeitado** (−15% no código) |
| spec6 | 110,1 | 39,36 | 4,27 | 0,74 | 67,4 | 0 | **rejeitado** (9,2 vs 8,5 ms/token) |

O custo do prompt curto (ms/token, 30–35 tokens) foi de 6,64 com PLE8 (v2),
6,6–7,0 com PLE16 e 6,67 com PLE32: o número de threads de PLE não muda o
decode nem o prompt curto. O corpus dos fills é repetitivo e deixa o PLE em
cache, por isso o efeito em prompt longo não repetitivo continua sem medição.
O padrão 16 permanece e 64 continua sem autorização.

**PLE8 (reconciliação):** `dual-ple8` tem 7/7 requests completos com
usage/timings, zero strikes de guard e `sampler.stop`. Falta apenas a linha
`DONE` do runner, interrompido na transferência de sessão, e nenhuma rodada
daquela fase gravava `run-status.json`. Não houve repetição: o braço é um
exploratório completo e o PLE32 confirmou ausência de efeito.

### 5.2 Ranking por checkpoint (S13)

- **Treino:** rodada `v4-iq3-training-1` (candidato `…-training-32k` com
  `--dump-routing`), corpus próprio do probe (`--training`, 6 prompts).
  O trace tem 14.653.056 bytes, ou seja, 166.512 registros completos
  (48 camadas × 3.469, k=10, nenhum ID inválido) e 22.354 pares distintos.
- **Ranking:** `make_profile.py --rerank --checkpoint <repo@revisão, sha256
  dos shards, pack, trace> --out claude-session/ranking/orca-iq3-xxs-rerank-v1.bin`.
  A base compartilhada `data/expert-profile.bin` permaneceu intacta (sha256
  conferido antes e depois).
- **Holdout:** prompts padrão do probe, sem interseção com o corpus de treino.
  A/B A,B,B,A,A,B, 3 starts por braço, um knob só (`--expert-profile`).

| Request | Hit A→B | Experts CPU/janela-camada A→B | ms por slot A→B |
|---|---|---|---|
| warmup | 96,9 → 98,5% | 0,79 → 0,44 | 8,83 → 8,16 (−7,6%) |
| code-0 | 96,7 → 98,6% | 1,10 → 0,50 | 8,25 → 7,74 (−6,2%) |
| code-1 | 98,5 → 99,2% | 0,51 → 0,28 | 7,69 → 7,38 (−4,0%) |
| code-2 | 99,2 → 99,5% | 0,27 → 0,20 | 7,50 → 7,32 (−2,4%) |
| PT | 97,8 → 98,1% | 0,48 → 0,41 | 9,02 → 9,03 (0%) |

As trocas com o tier de VRAM por sessão caíram de ~3.900 para ~2.100.
Exact-copy/JSON passaram nos 6 starts e o PT ficou coerente. O tok/s do
código (A 108,1; B 121,2) mistura trajetória e não deve ser lido como o ganho
do ranking. **Conclusão:** o ranking por checkpoint reduz misses e trocas e
corta 2–8% do custo por slot nos primeiros requests, efeito que desaparece
conforme o tier adaptativo converge. A generalização para cargas fora de
código/PT não foi medida.

### 5.3 CPU, MTP e afinidade (S08, S14, S15)

- **`STRATA_IQ_MT_MIN=1` (2 starts):** 118,3/117,7 tok/s e 28,5–28,6
  ms/janela, contra a referência (116,4–118,5; 28,2–29,3), sem o custo de
  −1 a −3% citado upstream. As saídas das duas tentativas coincidem em
  warmup/code-0/code-1 e divergem em code-2 e no PT. Os controles PT/tools
  ficaram iguais à referência. **Sem mudança de padrão.**
- **`spec-min-p` 0,5 vs 0,7 em starts separados (A,B,B,A,A,B):** código com
  medianas de 108,8 e 107,5 tok/s e ms/token por request alternando entre os
  braços, o que é empate. PT de 72,2–73,9 contra 74,8–77,1 tok/s, intervalos
  sem sobreposição (+4–5%). O ganho de +6% visto no mesmo processo **não se
  reproduziu no código**. O 0,5 continua no candidato IQ3; o sinal no PT fica
  registrado para uma calibração futura específica de PT/tools.
- **`pcie_frac`:** só há as medições anteriores no mesmo processo
  (0/0,28/0,6, medianas de 121,9/121,8/120,3). Sem benefício, nenhuma mudança
  foi proposta e por isso não houve A/B.
- **Pool 8/12 vs 15:** os "ganhos" anteriores (120 vs 108) vêm de trajetória;
  o `code-0` ficou em 107,6/108,4/108,3 e os drafts aceitos coincidem por
  grupo de trajetória. **Rejeitado**; 15 permanece.
- **Máscaras reais** (`/proc/<pid>/task/*/status`, capturadas durante o decode):

  | Threads | Máscara | Observação |
  |---|---|---|
  | Host thread | CPU 0 | intencional |
  | 15 workers do pool | CPUs 1..15, um por núcleo físico | política própria do pool |
  | 8 auxiliares criados no request | `1-15,17-31` | correção S08 efetiva |
  | 2 threads de baixa atividade | CPU 0 | provavelmente leitor de stdin e watchdog (4 e 8 trocas voluntárias); impacto desprezível, sem mudança |
  | 20 threads anteriores ao pin, incluindo 16 de I/O PLE | `0-31` | — |

  Um A/B isolado da correção S08 exigiria um interruptor ou um binário só sem
  ela, o que não existe. Nenhum ganho é atribuído a S08.

### 5.4 Profiling (S17, S20, S15 último stage)

**Nsight:** `v4-iq3-nsys-1`, wrapper `claude-session/tools/nsys-native-v4.sh`
com as mesmas flags preparadas e `STRATA_VERIFY_PROFILE=1`, instância mantida
por `--linger 150` até o relatório ser gravado. Arquivos `iq3-nsys.nsys-rep`
(80 MB), `iq3-nsys.sqlite` e CSVs `nsys-stats_*`.

- **Kernels de decode (fase de requests):**

  | Kernel | Fração do tempo de kernel |
  |---|---:|
  | `native_gu<18>` | 12,1% |
  | `mmvq<18>` | 11,4% |
  | `native_down<20>` | 10,0% |
  | `gr_down`/`gr_up_multi` | 16,4% |
  | Quantizações Q8_1 (somadas) | ~1,7% |
  | `gpu_stamp` (instrumentação de profiling) | 3,6% |

  **S17:** fundir SwiGLU com a quantização mira no máximo ~1,7% do tempo de
  kernel, menos ainda do tempo de parede (kernels ocupam ~40% de cada GPU).
  **Rejeitado no decode por evidência.**
- **Prefill (`STRATA_PREFILL_TIMING=1`, 29.024 tokens frescos, rodada
  `v4-iq3-pftiming-fills`):** a linha do tempo GPU mostra dequant 19,5%,
  espera de cópia de experts 15,9%, embed 13,1%, GEMM gate/up 11,4%,
  GDN 7,7%, GEMM down 6,8% e agrupamento host 6,7%. Se houver fusão, o alvo
  com evidência é dequant+GEMM no prefill (MMQ IQ3). Isso exige oracle
  numérico e PT/tools e não foi implementado.
- **Handoff (S20):** por janela, o D2H é de 12 KB (~1,8 µs) e o restante passa
  por memória host mapeada lida no kernel (~3,5 µs), contra ~28 ms por janela.
  As linhas "Peer-to-Peer" do Nsight (7,2 GB) **não são GPU↔GPU**: os tipos
  de origem/destino são pinned/device, ou seja, trocas da GPU1 com o
  complemento page-locked registrado no contexto da GPU0. **P2P rejeitado**
  (custo de handoff imaterial). O tráfego relevante são as trocas do tier
  (~7.800 cópias de 2,18 MB na janela), reduzidas pelo ranking (§5.2).
- **MTP em lote no último stage (S15):** no multi-GPU, o KV do drafter no
  prompt passa pelo caminho por token (`generate.cpp:3918`, `!multi_gpu`).
  O tempo pós-chunk do último stage (camada de draft e progresso) foi de
  40/272/557/1.028 ms para 1.177/7.732/15.921/29.031 tokens frescos, ou
  2,6–5,2% do tempo de prompt. Esse é o **teto** do ganho, sujeito a
  sobreposição com o stage 0 e ao custo do próprio lote. Não implementado:
  o benefício máximo de ~5% do TTFT fresco não justificou, nesta sessão, a
  mudança e o oracle necessários. Fica como candidato com o teto medido.

### 5.5 Cache de conversas (S19)

A→B→A com 21.022 tokens por prompt (acima do intervalo de checkpoint de
16.384), medido em 2 rodadas (v3 e v4 MT_MIN): `a1`/`b`/`a2` todos com
`cache_n=0` e ~12,3 s de prefill cada. O A1 grava 2 checkpoints, e o B, com
prefixo distinto desde o primeiro token, substitui a sessão. Os checkpoints
só valem para a sessão viva; com split, o parking continua desabilitado
(orçamento 0). Isso confirma o limite documentado. **Não é defeito.** O custo
por troca de conversa de ~21k tokens é ~12,3 s no IQ3 dual. Parking não foi
implementado: não há demanda A→B→A demonstrada no uso real, e o orçamento
conjunto não comporta (IQ3 com 22,9/23,0 GiB de VRAM e 12,6 GiB de
complemento pinned).

### 5.6 Memória e guard (S10, S09)

- **Abort emergencial observado no hardware, sem provocação:** em
  `v4-iq3-ref-a`, o swap global cresceu 2,92 GiB durante a carga do IQ3 dual
  resident-stage-dense. Na terceira amostra, `stop_guarded_group` enviou
  SIGKILL apenas ao grupo `server.py` com o config exato (PID 1068543, start
  ticks conferidos). O proxy registrou a saída antes do health, e nenhum
  outro processo foi afetado.
- **Atribuição:**
  - O reclaim aconteceu na carga. A `ref-a` começou com 41,1 GiB de page
    cache vindo do IQ2 256k anterior. Resultado: `pgsteal_file` 42,5 GiB,
    `pgsteal_anon` 2,94 GiB e `pswpout` 2,89 GiB, com VmSwap de apenas
    ~0,17 GiB no nativo e ~0,15 GiB no server.
  - O AnonPages do sistema caiu de 6,9 para 4,1 GiB, ou seja, as vítimas
    foram páginas anônimas de outros processos.
  - O salto final (0,84 → 2,92 GiB) ocorreu com MemFree de 12–14 GiB e PSI
    de memória de 14%.
  - Não há limite de cgroup (`memory.max`/`high` = max, zero eventos).
  - A zona Movable está vazia; a Normal tem 59 GiB.
  - A v3 `ref`, com 18,7 GiB iniciais de cache, teve só 0,36 GiB de
    `pgsteal_anon`.
  - Os contadores globais mostram compactação e reclaim direto
    (`compact_stall`, `allocstall_*`). Os deltas medidos na carga do IQ4
    (§6.3) confirmam isso: 6.158 `allocstall` e 12.859 `compact_stall` em 16 s.
  - **Hipótese mais provável, não provada:** a alocação page-locked grande do
    complemento (12,6 GiB) dispara compactação e reclaim direto, que levam
    páginas anônimas ao zram com swappiness 180. Os contadores `compact_*`
    entraram no sampler nesta sessão. Nenhuma política global foi alterada.
- **Descarte de páginas copiadas:** já existe no caminho do complemento
  (`madvise DONTNEED` + `posix_fadvise DONTNEED` por camada,
  `expert_source.cpp`). Depois do IQ3, o page cache caiu para 2,5 GiB e o IQ2
  seguinte carregou sem swap.
- **Dependência do estado anterior:** as cargas IQ3 residentes passaram em
  todos os demais starts. Os crescimentos de swap foram 0,327 (v3), 1,962
  (ple32), 1,782 (rank-B2), 1,035 (ref-c) e 0–0,49 nos outros. O resultado
  depende do estado de page cache e fragmentação deixado pelo modelo
  anterior; o candidato **não é limpo de guard** de forma reprodutível.

## 6. IQ2 longo, 976k e IQ4

### 6.1 IQ2 stage-dense 256k com prefixos novos

Primeira execução (`v4-iq2-stage-dense-256k-fills-1`, partindo de 52 °C):
**passou**. Fills frescos de 12.652/65.077/130.611/235.476 tokens, com
recuperação exata 4/4 e TTFT de 5,95/18,45/36,45/71,31 s. Swap +0,004 GiB
(o mesmo candidato sem stage-dense tinha falhado com +2,080 GiB na v2).
24.576/24.576 experts residentes fora do prefill (eram 23.446 sem
stage-dense). Pico de 21.614/22.957 MiB e 82/74 °C.

**Problema térmico em todos os braços (protocolo v1, fills em sequência):**
A1, B1, B2 e A2 dispararam o guard de 85 °C na GPU0 durante o fill de 0,9,
e o canônico C1 chegou a 84 °C. Em todas elas, a GPU0 entrava no fill de 0,9
já a ~79 °C, aquecida pelos fills anteriores. É uma dependência de ordem, não
do braço; o abort emergencial agiu corretamente nas quatro. Essas rodadas
(`ab256-*`) ficam como evidência e foram excluídas da comparação.

**Protocolo v2 (resfriamento <65 °C antes de cada fill), C,A,B,B,A,C,C,A,B,
9 starts, 0 disparos de guard:**

| Braço | Código tok/s | ms/janela | Aceitação | PT tok/s | TTFT 0,25 / 0,5 / 0,9 (s) | Pico GPU0/1 MiB | Swap +GiB | Experts residentes |
|---|---|---|---|---|---|---|---|---|
| C canônico (baseline) | 123,5–124,4 | — | 0,85–0,86 | 84,1–92,8 | 20,1–20,3 / 39,4–39,6 / 76,8–78,0 | 22.940 / 22.997 | 0 | (não exposto) |
| A v4 sem stage-dense | 122,2–123,9 | 23,1–23,5 | 0,86–0,87 | 83,3–92,9 | 18,6–18,7 / 36,4–36,8 / 72,3–72,6 | 22.942 / 22.997 | ≤0,09 | 23.446 |
| B v4 stage-dense | 127,7–128,3 | 22,8–23,0 | 0,895 | 92,2–92,3 | 18,6–18,8 / 36,2–36,3 / 72,3–72,7 | **21.596** / 22.957 | ≤0,20 | **24.576** |

As 9 rodadas têm recuperação exata em todos os fills e com cache, e
exact-copy/JSON corretos. A temperatura máxima foi 82–83 °C na GPU0 em todos
os braços.

- **C → A (um knob: release):** prefill fresco 6–8% mais rápido, com
  intervalos sem sobreposição. O tempo nativo do prompt cai de 76,7 para
  72,1 s a 235k e de 39,3 para 36,3 s a 130k; o overhead do servidor no TTFT
  cai de ~0,5 para ~0,2 s. Decode empatado (−0,7%).
- **A → B (um knob: `--stage-dense`):** decode +4,0%, intervalos sem
  sobreposição. Isso combina −1,8% de ms/janela (residência integral) com
  trajetória estável: as três saídas B são idênticas e a aceitação é 0,895.
  TTFT empatado, −1,3 GiB na GPU0, swap desprezível nos dois braços.

### 6.2 IQ2 976k: uma mudança para o problema térmico

**Diagnóstico** (telemetria da rodada `iq2-final-976k` da sessão anterior):
durante o prefill as duas GPUs ficam presas em ~268 W, o limite de potência.
Com a mesma potência, a GPU0 chegou a 85 °C e a GPU1 estabilizou em ~75 °C.
O gargalo é o resfriamento da GPU0, que também atende o desktop.

Alternativas descartadas por evidência ou por regra:

- Chunk de prefill menor: a potência da GPU0 no prefill foi de 252–260 W
  com chunk 512, 1024 ou 2048 (IQ3 dual), sem alívio.
- Split assimétrico: proibido pela política do operador (`SKILL.md`).
- Potência, clocks e ventoinha: proibidos.
- Resfriar mais antes do start: não resolve um único request de ~13 min.

**Mudança escolhida:** o governador térmico opt-in do prompt (§3), com
`STRATA_PREFILL_TEMP_PAUSE_C=80` (retomada a 77 °C, teto de 60 s por chunk),
aplicado só ao candidato 976k. O guard de 85 °C continua intacto e é o limite
rígido; o gate só agenda pausas entre chunks.

**Tentativa 1 (`v4-iq2-976k-gate80`, protocolo v2):**

- Bloco curto completo: código e PT.
- Fill fresco de 49.552 tokens: TTFT 16,9 s, recuperação exata.
- O gate atuou dezenas de vezes no fill de ~250k, com pausas de 1,8–2,6 s
  entre 81–82 °C e retomada a 76 °C. A temperatura máxima da GPU0 foi
  **82 °C**, contra 85 °C na rodada anterior sem gate.
- A rodada **parou no guard global de swap** (+2,10 GiB) durante o fill de
  0,25, antes do quase cheio. A atribuição não aponta o candidato:
  - server.py ficou com ~0,2 GiB anônimos e o nativo com 2,5 GiB, ambos estáveis;
  - o AnonPages do sistema oscilou entre 8 e 16 GiB por processos fora do
    Strata (outra sessão de agente rodando build/testes: `bun …`,
    tsserver, node);
  - o reclaim foi só de kswapd, sem `allocstall` nem `compact_stall`;
  - o page cache estava ocupado pelo pack IQ2 (~38–43 GiB), do qual o modo
    mmap depende.
- **Resultado:** a mitigação térmica funcionou no trecho executado; o
  quase-cheio continua **não aprovado** (inconclusivo por pressão externa de
  memória).

**Tentativa 2 (`v4-iq2-976k-gate80-r2`):** interrompida por mim com SIGINT
logo após a carga e antes de qualquer fill, para relançar fora do limite de
10 minutos dos jobs de fundo da ferramenta. O `finally` do probe descarregou
a instância. Não há medição.

**Tentativa 3 (`v4-iq2-976k-gate80-r3`):**

- Fills frescos de 49.552 tokens (TTFT 16,7 s) e de **249.547 tokens**
  (TTFT **103,4 s**), ambos com recuperação exata e repetição em cache exata.
- O gate fez 42 pausas e manteve a GPU0 em no máximo **82 °C**. O custo foi
  de +28% de TTFT a 250k em relação aos 81,0 s sem gate na v2, rodada em que
  a GPU0 atingiu 85 °C.
- No fill de 0,5 (~494k tokens), o **guard global de swap disparou de novo**
  (+2,75 GiB) e o abort encerrou o grupo correto. Desta vez as páginas
  anônimas frias do próprio nativo foram para o zram: o RSS anônimo caiu de
  3,05 para 0,42 GiB e o VmSwap subiu para 2,44 GiB, via kswapd e sem
  reclaim direto.
- O gatilho foi pressão externa: workers `vitest` de outra sessão de agente
  (~1 GB cada) iniciaram naquele momento, somando-se a `bun` e tsserver,
  enquanto o page cache estava ocupado pelo pack IQ2 do modo mmap (41–44 GiB).

**Conclusão para o 976k:**

- A mudança térmica é eficaz; o limite de 85 °C deixou de ser o problema.
- O quase cheio **continua não aprovado**: nas duas tentativas válidas, o
  guard global de swap disparou por carga de terceiros na workstation.
- O profile 976k permanece no baseline.
- Retomada: §9, item 2, em máquina sem outras cargas pesadas.

### 6.3 IQ4 dual resident-stage-dense (preparado)

`v4-iq4-resident-stage-dense-fills`: **falhou no guard de swap durante a
carga** (+2,525 GiB em 16 s, antes de a GPU1 encher). Os contadores mostram
reclaim e compactação diretos: 6.158 `allocstall`, 12.859 `compact_stall`,
`pgsteal_anon` 2,6 GiB e `pgsteal_file` 8,2 GiB. O abort emergencial matou
só o grupo do config (PID 1622607). Com complemento de ~26 GiB pinned e pack
de 61 GiB, o IQ4 dual residente não cabe na política de swap desta
workstation. **Rejeitado** sem mudança justificada adicional, coerente com
a falha do IQ3 dual residente sem stage-dense e com o IQ4 mmap anterior.

## 7. Matriz S01–S20

Legenda: **implementado** = código presente; **validado** = testes e/ou
hardware nesta árvore (v4) com a evidência citada; **rejeitado** = hipótese
de ganho refutada ou não demonstrada; **pendente** = falta evidência obrigatória.

| ID | Estado | Evidência / observação |
|---|---|---|
| S01 pack transacional | Implementado e validado por testes | `tools/test_pack_transaction`, `test_iq_pack` (84 OK). Packs instalados não foram reconvertidos. |
| S02 build/proveniência | Validado | Builds limpos v3/v4 com ctest 40/41, `BUILD.json`, publicação por rename, catálogo separado; baseline intacto. |
| S03 contrato API/tools | Validado (contrato); agente **pendente**; **bloqueia promoção** | 400 explícito para `tool_choice` forçado, `parallel_tool_calls=false`, `response_format`, `seed` booleano e `top_k=0`. Stop Unicode, exact-copy e bytes do tool OK; o stop Unicode falha no server canônico e passa na v4. O harness estrito (integrity e auto) exige `parallel_tool_calls=false` e fica inutilizável contra a v4 (20/20 HTTP 400). O proxy traduz `disable_parallel_tool_use`/`any`/`tool` do Anthropic para esses campos. O IQ3 repete a chamada após sucesso; o IQ2 256k não repetiu (1 amostra, B e C). |
| S04 cancelamento/drain | Validado por testes | Python 94 OK. Desconexão do cliente em hardware não foi exercitada especificamente nesta sessão. |
| S05 saúde/encerramento | Validado por testes | Go/Python OK. O proxy instalado é anterior às mudanças Go: o comportamento ao vivo do monitor não foi exercitado. O kill do guard foi reportado corretamente pelo proxy. |
| S06 sampling efetivo | Validado | Seed 0 efetiva (`effective_settings.seed=0`); `top_k=0` e seed booleana recusados. Identidade textual com prefixo em cache vs fresco não é garantida. |
| S07 métricas por GPU | Implementado; validado por testes | Monitor/benchmark Go testados. O sampler do probe registra as duas GPUs. Deploy do proxy pendente (`make install` + restart). |
| S08 afinidade | Validado (mecanismo) | Máscaras reais confirmadas (§5.3). Prefill da release v4 6–8% mais rápido que o baseline (§6), mas sem A/B isolado; nenhum ganho atribuído só a S08. |
| S09 complemento dual | Validado (fixture + hardware) com ressalva | Fixture dual PASS nas duas GPUs (v3/v4); IQ3 dual resident-stage-dense funcional. Não é limpo de guard: o swap depende do estado anterior (§5.6). IQ4: ver §6. |
| S10 memória | Atribuição parcial | O reclaim na carga empurra páginas anônimas de outros processos para o zram; MemFree alto; hipótese de compactação/reclaim direto com contadores adicionados. Nenhuma política alterada. |
| S11 stage-dense | Validado (desempenho/memória/guards); promoção bloqueada por S03 (§8) | IQ2 256k: A/B com 3+3 starts, decode +4%, 24.576/24.576 residentes, −1,3 GiB na GPU0, sem guard. |
| S12 prefill | Calibrado, sem mudança de padrão | Perfil do prefill (§5.4); chunk 512/1024/2048 não alivia a potência (252–260 W); sem default novo. |
| S13 ranking | Validado no candidato; GPU1-resident medido e **não promovido** | Holdout A/B com 3 starts por braço (§5.2). No IQ3 GPU1-resident (§15), só os 2 primeiros requests ganham (+9..11% tok/s), e a continuação de tools reemite em 3 de 5 starts contra 0 de 3 no base. Canônicos sem rerank. |
| S14 CPU/PCIe | Rejeitado (sem benefício) | Pool 8/12 e `pcie_frac` sem efeito acima da variação de trajetória; 15 workers mantidos. |
| S15 MTP | spec2/spec6 **rejeitados**; min-p 0,7 **não demonstrado** no código (+4–5% no PT); MT_MIN=1 sem custo nem determinismo | MTP em lote no último stage com teto de 2,6–5,2% do TTFT fresco; não implementado. |
| S16 cache BPE | Validado por testes | `test_bpe_cache`. Overhead do servidor no TTFT de 235k: 528 → 190 ms (release inteira, não isolado). |
| S17 kernels | Fusão no decode **rejeitada** por evidência | Quantização ~1,7% do tempo de kernel. Hotspot do prefill: dequant 19,5%; fusão MMQ não implementada (exige oracle). |
| S18 PLE | **Concluído:** 8/16/32 sem ganho de TTFT | Prompt curto e prompt diverso de 58k (§14): mais threads reduzem a espera do host, mas não o TTFT (overlap com a GPU); 16 mantido, 64 não justificado. |
| S19 conversas | Limite confirmado, sem defeito | A→B→A a 21k sem reuso (12,3 s); parking não justificado. |
| S20 P2P | **Rejeitado** | Handoff de 12 KB/janela (µs); as linhas "P2P" do Nsight são trocas pinned-host. |

## 8. Decisões sobre os seis profiles canônicos

> Em 2026-10-02 o IQ2_XS dual 256k foi promovido (§11). A tabela abaixo
> registra as decisões de 2026-10-01.

**Nenhum profile canônico foi promovido ou alterado.** O catálogo
`strata-fork`, os configs canônicos e o executável nativo original seguem
intactos, e não há rollback a executar.

| Profile canônico | Decisão | Motivo |
|---|---|---|
| IQ2_XS dual 32k | Mantido no baseline | Nenhum candidato mostrou ganho no 32k; a regressão da release (v2) ficou empatada (131,4 vs 132,0). |
| IQ2_XS dual 256k | Mantido no baseline; **candidato v4 + `--stage-dense` validado em desempenho, memória e guards** | Ver §6.1. **Bloqueio:** gate de tools. O server v4 recusa com 400 `parallel_tool_calls=false` e `tool_choice` forçado (S03). O harness estrito padrão exige esses campos e falhou em 20/20 requests, onde o canônico dá 10/10 chamadas corretas e 1/10 espúria. Clientes Anthropic com `disable_parallel_tool_use` ou `tool_choice any/tool` passariam a receber 400. Isso não é aprovação. |
| IQ2_XS dual 976k | Mantido no baseline | O governador térmico controlou a temperatura no trecho executado, mas o quase cheio não foi aprovado (§6.2). |
| Orca IQ3_XXS GPU1 residente 32k | Mantido | Nenhum candidato testado nesse placement nesta sessão. |
| Orca IQ3_XXS dual mmap 32k | Mantido (experimental) | O candidato resident-stage-dense (com ranking por checkpoint) funciona e melhora misses, mas o swap na carga depende do estado anterior (§5.6), então não é limpo de guard. Ainda não está qualificado como agente (repete a chamada após sucesso). |
| IQ4_XS dual mmap 32k | Mantido (experimental) | Resident-stage-dense falhou no guard de swap na carga (§6.3). |

O preset de sampling e o template permanecem os já registrados nos profiles.
As skills não foram editadas; as propostas estão em §9.

## 9. Bloqueios e retomada exata

1. **Política S03 × tools (bloqueia o IQ2 256k).** Decisão do operador entre:
   - (a) honrar `parallel_tool_calls=false` no server, entregando só a primeira
     chamada completa já bufferizada, sem constrained decoding, e manter o
     400 para `tool_choice` forçado;
   - (b) manter a recusa e adaptar o harness e a tradução do proxy;
   - (c) manter o canônico no server baseline.

   Depois de (a) ou (b), em release nova com os gates completos:

   ```sh
   python3 docs/reports/strata-implementation-2026-10-01/claude-session/tools/run_arm.py \
     qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-candidate-stage-dense-256k <rótulo-novo> --probe --controls
   python3 .agents/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py \
     --model qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-candidate-stage-dense-256k --mode auto --promotion
   ```

   Se passar, promova o 256k preservando ID e contexto:
   - backup do profile e de `~/.config/model-loader/strata/qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k.json`;
   - no config, `exe` passa ao binário da release e entra `--stage-dense`;
   - `model-loader profile edit qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k --backend <catálogo da release>`;
   - `model-loader profile validate`;
   - uma rodada `--readonly --probe --fills --controls` de confirmação;
   - rollback: restaurar os dois backups e `--backend strata-fork`.
2. **976k quase cheio.** Repetir só com a máquina sem outras cargas pesadas
   (cargas de outra sessão de agente, com build e testes, derrubaram as
   tentativas 1 e 3 pelo guard global de swap):

   ```sh
   python3 docs/reports/strata-implementation-2026-10-01/claude-session/tools/run_arm.py \
     qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-candidate-976k <rótulo-novo> \
     --env STRATA_PREFILL_TEMP_PAUSE_C=80 --probe --fills
   ```

   Rode via `nohup`, já que leva de 45 a 60 min.
3. **IQ3/IQ4 dual residente (S09/S10).** Atribuir o reclaim direto na carga
   com os contadores `compact_*`/`allocstall`, partindo de estados prévios
   controlados (IQ2 → IQ3 vs IQ3 → IQ3). Só então testar, um de cada vez:
   alocar o complemento antes do preenchimento da VRAM; pinning em blocos
   menores (`cudaHostRegister` sobre memória já faltada); ou complemento
   pageable para o IQ4. Swappiness e THP globais continuam fora de escopo.
4. **Deploy Go (S05/S07 ao vivo).** `make install` e reinício do serviço
   `model-loader-proxy`. É uma ação visível para clientes externos
   (caddy/cloudflared) e depende do operador.
5. **Otimizações condicionais com teto medido:**
   - MTP em lote no último stage: teto de 2,6–5,2% do TTFT fresco;
   - fusão dequant+GEMM (MMQ IQ3) no prefill: 19,5% da linha do tempo GPU a 29k.

   Ambas exigem oracle numérico e PT/tools.
6. **S18:** prompt longo **não repetitivo** comparando `STRATA_IO_THREADS` 8/16/32.
7. **Fixtures externas:** `ple_parity` e `platform_memory_test` continuam sem
   execução (fixtures ausentes; mlock acima de 8 MiB).
8. **Skill (proposta, não aplicada):** em
   `references/strata.md`/`full-tuning.md`, registrar:
   - fills de contexto longo precisam de resfriamento <65 °C antes de cada
     fill fresco, porque em sequência o fill de 0,9 do 256k atinge 85 °C na
     GPU0 a 270 W;
   - o tok/s de decode entre starts do Strata segue a trajetória (comparar
     ms/janela e ms por slot);
   - o harness estrito exige `parallel_tool_calls=false`, incompatível com
     servers que recusam controles não garantidos;
   - o guard global de swap é sensível a outras cargas da máquina.

## 10. Limpeza

As decisões foram registradas antes de qualquer remoção, e o profile e o
config de cada candidato foram arquivados com SHA256 em
`claude-session/removed-candidates/<id>/`.

**Removidos** via `model-loader profile delete` + config em
`strata/perf-20261001/` (log em `removed-candidates/removal.log`), 15 candidatos
criados por esta tarefa com decisão final:

- IQ3 dual resident-stage-dense: PLE8, PLE32, pool8, pool12, spec2, spec6,
  prefill1024, prefill2048, cpu-parity, nsys, minp07;
- IQ3 GPU1: prefill1024 e prefill2048;
- IQ3 dual resident sem stage-dense;
- IQ4 dual resident-stage-dense.

**Mantidos**, necessários à retomada ou referência:

- IQ2: `candidate-32k`, `candidate-256k`, `candidate-976k`,
  `candidate-stage-dense-32k`, `candidate-stage-dense-256k`;
- IQ3: `candidate-32k`, `candidate-resident-stage-dense-32k`, `…-rerank-32k`,
  `…-training-32k`, `candidate-stage-dense-32k`, `gpu1-resident-w15-candidate-32k`;
- IQ4: `candidate-32k`.

Os candidatos que rodei nesta sessão apontam para a v4 (`strata-perf-v4-20261001`).
Os originais preparados estão em `claude-session/config-backups/` e
`prepared-config-snapshot/`.

**Não removidos:**

- releases `v1`–`v4`, que são evidência imutável;
- entradas de catálogo `strata-perf-*`;
- modelos, packs, tokenizers e a base `data/expert-profile.bin`;
- alterações do usuário.

O arquivo de ranking treinado fica em `claude-session/ranking/` porque é
referenciado pelo candidato `…-rerank-32k`.

## 11. Continuação 2026-10-02: política de tools, releases v5/v6 e promoção do 256k

Por decisão do usuário, segui a opção recomendada (a) de §9.1, e para o
`integrity` adotei o forçamento real da chamada.

### 11.1 Mudança no servidor (`backends/strata-fork/serve/`)

- **`controls.py`:**
  - `tool_choice()` normaliza OpenAI (`auto`/`none`/`required`/função
    nomeada) e Anthropic (`auto`/`any`/`tool`/`none`).
  - `single_call()` cobre `parallel_tool_calls=false` e `disable_parallel_tool_use`.
  - `tool_policy()` devolve o prefixo forçado e o limite de chamadas.
  - A validação recusa com 400: escolha forçada sem tools, nome não
    oferecido, `none` com tools, forçamento ou limite com MCP, e formas
    desconhecidas.
- **`server.py`:**
  - `prepare()` anexa ao prompt a abertura `<tool_call>\n<function=` (mais
    `NOME>\n` quando há nome).
  - `run()` trata essa abertura como já gerada, exige que a chamada se
    complete com um tool oferecido (senão erro explícito, nunca texto) e, sob
    o limite, encerra a geração após a primeira chamada completa, sem
    devolver o que vier depois.
  - O forçamento exige thinking desligado (400 caso contrário), como na API
    Anthropic.
  - Com **um único** tool oferecido, `required` equivale a forçar aquele
    tool. Na v5 o prefixo aberto deixou o modelo inventar `exec_command`
    quando só `bash` era oferecido, e o servidor recusou com erro explícito.
- **`test_tool_policy.py`:** 10 testes (validação, política, chamada forçada
  nomeada e `required`, nome inválido e chamada incompleta → erro, thinking
  ligado → 400, limite de uma chamada em JSON e SSE, rota Anthropic).
- SHA256: `controls.py` `ba0fed35…`, `server.py` `88906c8c…`,
  `test_tool_policy.py` `819e1d73…`.
- O candidato stage-dense 256k ganhou o `.shared-settings.json`
  (`reasoning_effort: none`) igual ao do canônico. Antes ele não tinha, então
  o harness rodava com thinking ligado.

### 11.2 Gates e releases

| Release | Catálogo | Gates |
|---|---|---|
| v5 `20261002T095307Z-13018fd9fe4e` | `strata-perf-v5-20261002` | Python 103 OK (5 skip), tools 84 OK, nativo 41/41 com fixture dual, Go test/vet rc 0 |
| v6 `20261002T100952Z-2eed88f1f51d` | `strata-perf-v6-20261002` | Python 104 OK (5 skip), tools 84 OK, nativo 41/41 com fixture dual (Go sem mudança desde a v5) |

O binário nativo de v4, v5 e v6 é **idêntico** (`a3c58bbe…`), então as
medições de hardware da v4 valem para o engine promovido. A árvore atual não
tem drift em relação à v6.

### 11.3 Gate de tools (critério pré-registrado em `claude-session/plans/tools-gate.md`)

Cada braço teve um start com `--controls --linger 480` e, na janela, o harness
padrão em `integrity --stream --continuation` e `auto --promotion` (30+30)
(`tools/harness_window.py`).

| Rodada | Integrity (forçado) | Auto 30+30 | Controles do probe | Guard |
|---|---|---|---|---|
| C canônico (`v5-iq2-256k-tools-C`) | 4/4; continuação repete a chamada | 30/30 corretas, 0/30 espúrias, `accept` | stop Unicode **falha** | disparou em t=249 s, depois de todas as medições, com o modelo ocioso e alta anônima externa |
| B v5 (`v5-iq2-sd256-tools-B`) | — | — | — | **swap na carga** (+2,42 GiB): pack IQ2 frio com page cache cheio (27,8 GiB via kswapd, 1,8 GiB direto, 7.169 `allocstall`, 16.114 `compact_stall`) |
| B v5 (`v5-iq2-sd256-tools-B2`, pack quente) | 0/4: o modelo nomeou `exec_command`, inexistente; 400 explícito | 30/30, 0/30, `accept` | todos passam | limpo |
| **B v6** (`v6-iq2-sd256-tools-B3`) | **4/4** (com stream), bytes exatos; continuação repete a chamada (= C) | **30/30, 0/30, `accept`**, 0 erros | todos passam | limpo (+0,025 GiB) |

**O gate passa:** integrity completo, auto igual ao canônico, controles iguais
ou melhores (o stop Unicode só passa no B) e nenhum guard no B3.

### 11.4 Promoção do IQ2_XS dual 256k

- **Backup** (profile, config, shared-settings e `SHA256SUMS`):
  `claude-session/promotion/iq2-256k-20261002T072035/`.
- **Mudança aplicada:**
  - config `~/.config/model-loader/strata/qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k.json`:
    `exe` passa a `releases/20261002T100952Z-2eed88f1f51d/strata` e
    `--stage-dense` entra após `--mmap-experts`;
  - `model-loader profile edit … --backend strata-perf-v6-20261002 --description …`
    com o contrato de saída da skill (valores medidos, limites da API, rollback);
  - `model-loader profile validate`: OK.
- ID, contexto 262144, sampling, template, placement 24/24, KV int8,
  15 workers e reserva de VRAM ficaram inalterados.
- **Confirmação** no canônico promovido (`promoted-iq2-256k-confirm`, somente
  leitura, protocolo v2):
  - código a 130,3 tok/s, aceitação 0,895;
  - fills frescos de 12.652/65.077/130.611/235.476 tokens em
    6,0/18,6/36,2/72,0 s, com recuperação exata;
  - pico de 21.612/22.957 MiB, swap +0,26 GiB, 82/72 °C;
  - integrity 4/4, auto 30/30 corretas e 0/30 espúrias;
  - todos os controles do probe passaram e nenhum guard disparou.
- **Rollback:** restaurar `config.json` e `profile.json` do backup
  (`model-loader profile edit qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k
  --file <backup>/profile.json` e copiar o config) e conferir
  `--backend strata-fork`.
- **Mudança visível para clientes** (registrada na descrição):
  - `response_format` JSON, schemas estritos, logprobs e `tool_choice none`
    com tools recebem 400;
  - `tool_choice` forçado exige thinking desligado;
  - `parallel_tool_calls=false` devolve só a primeira chamada.
- Os demais cinco canônicos seguem no baseline.

### 11.5 Travamento da máquina (2026-10-02 ~07:34)

A workstation travou e reiniciou às 08:38. No momento não havia modelo Strata
carregado (a confirmação já tinha terminado) e as GPUs estavam ociosas
(49/42 °C). O journal do boot anterior não tem Xid/NVRM/GSP, OOM, hung task,
MCE nem erro de nível `err` fora do cloudflared. Só registra falhas de DNS na
rede local e o `model-loader-cloudflared` reiniciando em loop (contador 688)
até parar sem mensagem. A causa ficou **indeterminada**; não há evidência
ligando o travamento a este trabalho. Depois do reboot conferi config/profile
promovidos (válidos), releases, catálogo, ferramentas e testes, todos íntegros.

### 11.6 976k quase cheio: tentativa 4 (`v6-iq2-976k-gate80-r4`, release v6, logo após o reboot)

**O que passou:**

- Gate `STRATA_PREFILL_TEMP_PAUSE_C=80`, com 70 pausas; GPU0 em no máximo
  **82 °C** e GPU1 em 71 °C.
- Fills frescos com recuperação exata e repetição em cache exata:

  | Tokens | TTFT |
  |---:|---:|
  | 49.552 | 18,6 s |
  | 249.547 | 81,2 s |
  | **499.551** | **277,7 s** |

  É a primeira vez que o fill de 50% conclui nesta campanha.

**Onde parou:** no fill de 90% (~900k tokens), cerca de 270 s após o início
do prefill, o **guard global de swap** disparou (+3,14 GiB). O abort SIGKILL
atingiu só o grupo do config (PID 16784). Houve dois fatores externos:

- **Pressão anônima externa:** outra sessão rodava
  `bun tools/generate-source-tree.ts` (2 GB, 100% de CPU), além do
  navegador/WebEngine e do clamd; AnonPages do sistema em ~15 GiB. Mais uma
  vez, as páginas anônimas frias do próprio nativo foram para o zram
  (VmSwap 0 → 1,15 GiB) com o page cache ocupado pelo pack mmap.
- **VRAM da GPU0:** o total do dispositivo passou do teto de 23.552 MiB em
  amostras isoladas (máximo 23.637 MiB, nunca 3 strikes seguidos). O
  processo Strata ficou estável em 22.386–22.468 MiB; quem cresceu foi o uso
  de desktop/gráfico na GPU0, de ~713 para 1.169 MiB (navegador aberto após
  o reboot).

**Conclusão:**

- O problema térmico está resolvido pelo gate.
- O quase cheio continua **não aprovado**: as tentativas 1, 3 e 4 pararam
  por guards globais disparados pelo ambiente (outras sessões de agente e
  desktop), não pelo candidato.
- O profile 976k permanece no baseline.
- **Retomada:** em janela dedicada (sem outras sessões de agente com
  build/teste e sem navegador pesado na GPU0), repetir o comando de §9.2 com
  a release v6. Se a VRAM do desktop continuar perto do teto, uma segunda
  mudança justificável é aumentar `--vram-reserve-mib` no candidato 976k.
  Ela muda o tamanho do cache e exige nova medição; não foi aplicada aqui
  por estar fora da regra de mudança única.

## 12. Baseline de contexto longo: 976k substituído por 500k (decisão do usuário, 2026-10-02)

O usuário considerou o 976k excessivo para este hardware. As quatro tentativas
de quase cheio esbarraram em guards; o fill de 50% (~500k tokens) foi o maior
aprovado. Por isso o profile de contexto longo passou a ser 500k.

**Profile novo:** `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-500k`
(fixado, backend `strata-perf-v6-20261002`):

| Item | Valor | Motivo |
|---|---|---|
| Contexto | 512.000 tokens (rótulo `500k` pela convenção de nomes do repositório: floor binário) | decisão do usuário |
| YaRN | fator **2** / original 262144 | o card oficial `Qwen/Qwen3.8-Flash-Next` recomenda fator 2.0 para uso típico de ~524.288 tokens; o YaRN estático com fator 4 piora textos curtos |
| Plataforma | release v6 + `--stage-dense` | igual à do 256k promovido (mesmo pack e placement); o binário nativo é o mesmo validado na v4 |
| Térmico | `STRATA_PREFILL_TEMP_PAUSE_C=80` | prefills de ~460–500k tokens passam de 85 °C na GPU0 sem o gate (rodada v2 do 976k); com o gate ficaram ≤82 °C (r3/r4) |
| Sem mudança | pack, tokenizer/template, sampling Qwen não-thinking, KV int8, 24/24, 15 workers, reserva de 1536 MiB, MTP spec4/minp0.7 | — |

**Removidos, com backup:** os profiles `…-976k` (canônico) e
`…-candidate-976k` (tarefa). Os configs e shared-settings foram retirados do
diretório ativo. Tudo, com `SHA256SUMS`, está em
`claude-session/promotion/iq2-976k-to-500k-20261002T104813/`. Nenhum cliente
externo referenciava o ID 976k.

**Rollback:**

- copiar `removed-from-config/*976k*` de volta para `~/.config/model-loader/strata/`;
- `model-loader profile create --id qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-976k --backend strata-fork --file <backup>/profile-976k.json`;
- `model-loader profile pin …-976k`;
- opcionalmente, `model-loader profile delete …-500k`.

### 12.1 Validação no hardware

- **Tentativa 1** (`baseline-500k-validate-1`): guard de swap **na carga**
  (+2,11 GiB). O pack estava parcialmente frio, com só 1,8 GiB livres e jobs
  `bun` de outra sessão rodando; houve 2.783 `allocstall` e 9.507 `compact_stall`.
  O config foi aplicado corretamente (log: YaRN fator 2, mscale 1,069; cache
  da GPU0 com 11.810 slots).
- **Tentativa 2** (pack aquecido): carga limpa. Código, PT, controles e fills
  de 25.147 e 127.552 tokens passaram (9,2 e 34,8 s, exatos). Depois disso,
  três jobs `bun` externos (~4,5 GiB) levaram páginas anônimas frias do
  nativo para o zram e o guard disparou (+3,0 GiB).
- **Tentativa 3** (`baseline-500k-validate-3`, disparada por
  `plans/wait-quiet-500k.sh` só depois de 60 s sem jobs `bun` pesados):

  | Fill fresco (tokens) | TTFT | Recuperação |
  |---:|---:|---|
  | 25.147 | 9,2 s | exata |
  | 127.552 | 34,8 s | exata |
  | 255.546 | 81,8 s | exata |
  | **460.341** | **243,4 s** | exata |

  - Repetições em cache exatas.
  - Código a 128,4 tok/s, aceitação MTP 0,856.
  - GPU0 em no máximo 83 °C, com 55 pausas do governador.
  - Pico de 22.970/23.018 MiB; swap +0,27 GiB durante todas as medições.
  - Controles do probe OK (exact-copy, JSON, stop Unicode, bytes do tool,
    continuação sem repetir, controle sem tool).
  - Harness integrity 4/4 (com stream), com continuação terminando em
    resposta final.
  - Harness auto: 30/30 corretas, mas **4/30 espúrias** nos controles sem tool
    (inconclusivo). Isso é coerente com o aviso do card sobre o custo do YaRN
    estático em textos curtos; o 976k antigo tinha 1/10.
  - O guard de swap disparou em t=909 s, depois de todas as medições (probe
    até 539 s, harness até 618 s), com o modelo ocioso e alta anônima externa.

**Conclusão:** o 500k é o baseline de contexto longo, validado no quase cheio
(460k frescos) dentro dos limites térmicos e de VRAM. Ele **não** está
qualificado como agente (espúrias 4/30), assim como o 976k não estava. A
descrição do profile registra esses valores.

## 13. Deploy do Model Loader (S05/S07 ao vivo, 2026-10-02 11:14)

Autorizado pelo pedido do usuário de concluir as pendências, e feito com nada
carregado no proxy:

1. `make build`, rc 0. O código Go não mudou desde os gates (`go test`/`go vet` rc 0).
2. Backup do binário instalado (`model-loader-bin-20261002T111340/model-loader.installed-before`
   e `SHA256SUMS` em `claude-session/promotion/`).
3. Troca atômica (`install` + `mv`) de `~/.local/bin/model-loader`.
4. `systemctl --user restart model-loader-proxy.service`: proxy, gateway e
   cloudflared ativos; `/_status` OK.

**Smoke test** `deploy-smoke-256k` (256k promovido, somente leitura):
132,4 tok/s, exact-copy e JSON OK, nenhum guard, instância encerrada limpa,
journal do proxy sem erros.

As métricas por instância (`instance metrics`) só são gravadas pelo monitor
da TUI, por isso aparecem vazias com o `serve` headless. O caminho por GPU do
S07 continua validado pelos testes Go e pelo sampler do probe.

**Rollback:** copiar `model-loader.installed-before` de volta para
`~/.local/bin/model-loader` (troca atômica) e reiniciar o serviço.

## 14. S18 — threads de I/O do PLE em prompt longo não repetitivo (2026-10-02)

**Método:** candidato IQ2 stage-dense 256k (v6) com `STRATA_PREFILL_TIMING=1`.
Em cada start, depois do bloco padrão do probe, `tools/ple_window.py` enviou
3 prompts frescos e **diversos** (fatias distintas de código e documentação
do repositório, 57.758–59.520 tokens). Ordem `STRATA_IO_THREADS` 16, 32, 8,
8, 32, 16, com 2 starts por valor e espera de máquina quieta antes de cada start
(`plans/s18-ab.sh`; resumo em `s18-summary.json`).

**Diagnóstico prévio** (`s18-ple16-diag`): com texto diverso, o stage 0 fica
~1,9–2,6 s por prompt bloqueado esperando linhas do PLE. Nos fills
repetitivos eram 57 ms. A diferença entre parede e linha do tempo GPU fica
em ~2,2 s em 17,9 s.

| Threads de I/O | TTFT mediano (6 prompts) | Faixa | Espera do host por PLE | Parede − GPU |
|---|---:|---|---:|---:|
| 8 | 19,44 s | 18,73–19,71 | 2,51 s | 2,35 s |
| **16 (padrão)** | **19,10 s** | 18,45–19,53 | 1,87 s | 2,35 s |
| 32 | 19,43 s | 18,53–21,42 | 1,49 s | 2,50 s |

Todos os starts terminaram limpos, sem guard, com decode de ~132 tok/s em
todos os braços.

**Conclusão:** mais threads reduzem a espera do host pelo PLE, mas o TTFT não
melhora e a diferença parede − GPU não muda: a leitura do PLE se sobrepõe à
computação da GPU e não está no caminho crítico. **Padrão 16 mantido; 64 não
se justifica.** As bolhas de GPU restantes (~2,3 s) vêm de outra parte do
caminho do prompt, que o profiling de §5.4 (espera de cópia, dequant)
continua indicando.

## 15. S13 no IQ3 GPU1-resident: rerank medido, não promovido (2026-10-02)

**Candidato:** `qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-candidate-rerank-32k`.
É idêntico a `…-gpu1-resident-w15-candidate-32k` (release v6) com uma única
diferença: `--expert-profile` = `claude-session/ranking/orca-iq3-xxs-rerank-v1.bin`,
o mesmo rerank treinado em §5.2. Os dois braços têm 8.407 experts residentes
e o mesmo complemento page-locked de 33,14 GiB.

**Desempenho:** ordem A,B,B,A,A,B. O A1 caiu no guard de swap na carga e foi
substituído pelo A4. Os números são medianas de 3 starts por braço (arquivos
`claude-session/gpu1-rank-compare.json` e `gpu1-rank-requests.txt`).

| Request | tok/s A → B | Hit A → B | CPU experts por layer-window | ms/window |
|---|---|---|---|---|
| 1º (warmup) | 57,4 → **63,7** | 69,7 → **89,7%** | 7,44 → **2,65** | 53,1 → **36,3** |
| code-0 | 68,2 → **74,2** | 84,8 → 87,7% | 4,48 → 3,85 | 43,7 → 43,1 |
| code-1 / code-2 | 87,7 / 89,6 → 88,6 / 86,4 | ~92% nos dois | ~2,6 nos dois | ~38,7 nos dois |
| PT | 50,4 → 51,3 | 82,4 → 82,8% | 3,57 → 3,28 | 36,3 → 35,3 |

O ganho fica no início da sessão, antes de o cache de residentes convergir.
Depois o cache converge e os braços empatam. As trocas do tier VRAM somam
~13,9k nos dois braços. Os checks exact/JSON passaram em todos os starts.

**Controles funcionais:** `--controls` com o harness estrito, 6 starts no A e
5 no B (`gpu1-rank-controls-summary.json`). A-3, A-4 e A-5 caíram no guard na
carga, então sobraram 3 A completos contra 5 B.

| | Base (A, 3 completos) | Rerank (B, 5 completos) |
|---|---|---|
| Integrity forçado (com SSE) | 4/4 em todos | 4/4 em todos |
| Auto, corretas de 10 | 8 / 9 / 7 | 9 / 8 / 8 / 9 / 9 |
| Auto, espúrias de 10 | 1 / 0 / 0 | 0 em todos |
| Continuação do harness após o tool result | limpa 3/3 | **reemitiu a tool em 3/5** |
| `continuation_no_repeat` do probe | 1/3 | 1/5 |
| Exact copy, JSON, stop Unicode, tool bytes, controle sem tool | passam | passam |

**Decisão: o canônico GPU1 não foi alterado.**

- O benefício se limita aos dois primeiros requests depois da carga.
- A continuação mostra repetição onde o base para. O gate pré-registrado
  (`plans/tools-gate.md`, critério 3) trata isso como regressão. A diferença
  não é significativa (Fisher unilateral p ≈ 0,18), e a mesma config alterna
  entre os dois desfechos de um start para outro. Mesmo assim, o ganho é
  pequeno demais para justificar resolvê-la com uma série longa de starts.
- Arquivo de rerank, candidato e evidência foram preservados para retomada.
  Os preparativos de promoção (cópia durável e backup) foram desfeitos sem
  tocar o canônico.

**Guard na carga:**

- O braço base disparou o guard global de swap (> 2 GiB) na carga em 4 de 10
  starts. O rerank, em 0 de 8; o B1 chegou a um pico de +2,21 GiB sem 3
  strikes.
- O mecanismo é o de S10. O pin do complemento leva o MemAvailable de ~47
  para ~15 GiB, e o kswapd joga anon de outros processos no zram (AnonPages
  de 4,1 para 1,1 GiB).
- O tamanho do complemento e a cópia camada a camada são iguais nos dois braços,
  então a diferença não foi atribuída ao rerank. Houve A com cache frio que
  passou (+0,23 GiB) e B com cache quente que chegou perto (+2,21 GiB).
- Isso reforça que a mitigação do reclaim na carga (§9) vale também para o
  placement GPU1-resident, não só para o dual.
