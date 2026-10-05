# Auditoria do Strata para duas RTX 3090

Data: 2026-10-01. Escopo: fork instalado em `backends/strata-fork`, integração
com Model Loader e os seis profiles que atualmente apontam para esse backend.

## Síntese para decisão

O backend já explora recursos adequados à máquina: build SM86, kernels AVX-512,
cache de experts em VRAM, MTP, prefill em lote, KV int8 e divisão de camadas
24/24. O IQ2_XS original tem evidências locais de execução nas duas GPUs,
inclusive com contexto de 262144 e 1000000 tokens. A auditoria não encontrou
evidência de que essa divisão esteja genericamente quebrada.

Os problemas mais relevantes estão em três áreas:

1. **Memória dos quants maiores.** IQ3_XXS e IQ4_XS tiveram testes dual
   interrompidos por crescimento de swap. O modo que estabilizou IQ3 na GPU1,
   com complemento de experts residente em RAM, é explicitamente bloqueado
   com divisão de camadas. Estender esse modo às duas GPUs é a oportunidade
   estrutural mais promissora, ainda sem ganho de desempenho medido.
2. **Correção e confiabilidade.** Controles da API são ignorados; o packer pode
   misturar pesos ao reutilizar uma pasta; o rebuild local falha em uma pasta
   nova; a drenagem após cancelamento pode ficar esperando indefinidamente.
   Esses problemas merecem prioridade antes de novas otimizações de kernels.
3. **Medição e calibração.** O monitor do Model Loader lê somente a primeira
   GPU; o tamanho de prefill, a proporção CPU/PCIe e o ranking inicial de
   experts ainda oferecem experimentos específicos para este hardware.

**Correção de uma afirmação anterior na conversa:** crescimento de swap não
foi observado apenas no IQ3_XXS. O relatório e o profile IQ4_XS atuais também
registram esse problema. Isso não invalida os testes concluídos do IQ2_XS.

O swap atual é **zram com zstd**, e não uma partição de swap em SSD. Crescimento
global de swap, isoladamente, não prova paginação do processo Strata, falta
de RAM física ou leitura de experts do SSD. As leituras de arquivos mmap e o
swap de páginas anônimas são mecanismos diferentes. A causa das interrupções
permanece parcialmente aberta; este relatório não atribui causalidade apenas
a `vm.swappiness=180`.

Nenhum código de produção, profile, peso, limite de potência ou configuração
global foi alterado nesta auditoria. Foram acrescentados este relatório e
artefatos de diagnóstico. Nenhum modelo real foi carregado novamente.

## Escopo, método e ambiente verificado

O inventário abrange 280 arquivos de código, 73777 linhas, em `src`, `include`,
`serve`, `tools` e `ref`. Isso é a dimensão inventariada, **não uma alegação de
revisão linha a linha de todos os arquivos**. A inspeção detalhada seguiu os
caminhos ativos dos profiles nativos Qwen4Exp e as dependências desses caminhos.
HIP/Windows, visão, interface web e modelos diferentes não receberam validação
de desempenho. MCP foi exercitado pelos testes existentes, sem invocar
ferramentas reais do operador.

| Item | Estado observado |
|---|---|
| CPU | AMD Ryzen 9 9950X3D, 16 núcleos/32 threads, dois grupos de L3, AVX-512/VNNI/VBMI |
| GPUs | 2 × RTX 3090, 24576 MiB por placa, SM86 |
| Interconexão | PHB; sem NVLink; `nvidia-smi topo -p2p r/w` indica `OK` nos dois sentidos |
| Potência | 270 W por placa |
| VRAM ociosa inicial | GPU0 874 MiB; GPU1 15 MiB |
| RAM | 63423160 KiB de MemTotal, aproximadamente 60,49 GiB; MemAvailable inicial 46,85 GiB |
| Swap | `/dev/zram0`, aproximadamente 16 GiB lógicos; cerca de 5,78 GiB ocupados no primeiro snapshot; compressor zstd |
| Kernel/driver | Linux 7.2.7-arch1-1; NVIDIA 610.57.04 |
| Toolkit disponível | CUDA 13.4.92; não equivale a provar a versão usada no build antigo |
| Disco dos modelos | `/dev/nvme0n1p1`, aproximadamente 67 GiB livres, 93% ocupado |
| Disco do checkout | `/dev/nvme1n1p1`, aproximadamente 165 GiB livres |
| Backend | Strata 0.1.30, HEAD `97cb786`, com alterações locais adicionais |
| Runtime | Nenhuma instância ativa no início da auditoria |

O driver 610.43.02 citado em referências antigas da skill não é o atual.
P2P `OK` é evidência de capacidade do driver; não prova que Strata use cópias
P2P nesse caminho, nem mede sua largura de banda. Os aproximadamente
13,3/13,4 GB/s dos logs IQ2 são probes históricos **host→device**.

O binário usado por todos os seis profiles tem SHA-256
`545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999`.
`cuobjdump --list-elf` confirma cubins SM86. O checkout tem modificações em
`serve/server.py`, `tools/iq_pack.py`, `tools/test_iq_pack.py` e arquivos locais;
portanto, citar apenas HEAD não identifica integralmente o backend. Os hashes
de código e snapshots estão nos artefatos.

### Caminhos revisados

| Subsistema | Fontes principais | Conclusão da inspeção |
|---|---|---|
| Launch/schema | `internal/service/stratahelp`, `processmgr/args.go`, `serve/server.py` | HTTP gerenciado → engine stdin; configuração nativa externa ao schema |
| Empacotamento | `tools/iq_pack.py`, GGUF reader, testes de shards | Conversão BF16 explícita; risco confirmado de reaproveitamento por tamanho |
| RAM/VRAM | `expert_source.cpp`, `expert_cache.cpp`, `pinned.cu`, `session.cpp` | mmap, arena inteira e complemento residente são caminhos diferentes |
| Multi-GPU | `generate.cpp`, `verify.hpp/cpp`, `prefill.cpp` | Camadas sequenciais; cache e sessão por stage; handoff por memória host mapeada |
| CPU MoE | `pool.cpp`, `native_expert.cpp`, IQ AVX2/AVX512 | Pinning por núcleo físico; divisão de trabalho e agrupamento de tokens |
| CUDA | `iq_kernels.cu`, QSA/GDN, MMQ e GEMM de prefill | SM86 aproveitado; oportunidades de fusão exigem perfil de execução |
| MTP/sampling | `mtp.cpp`, `verify.cpp`, `draft_policy.cpp`, `sampler.cu` | Verificação do target; custos/aceitação variam por workload |
| Contexto/cache | `conversation_cache.hpp`, snapshots, `generate.cpp` | Prefixo ativo difere de estacionamento de várias conversas |
| PLE/I/O | `ple_reader.cpp`, `direct_file.cpp` | Cache de linhas limitado, I/O direto assíncrono via pool de threads no Linux |
| API/parser | `server.py`, `frontend.py`, tokenizer | Lacunas reproduzidas de contrato, cancelamento e semântica de sampling |
| Telemetria | `serve/telemetry.py`, `monitor/gpu.go`, `benchmark/gpu_sampler.go` | Strata tem métricas por GPU; agregação do Model Loader perde GPU1 |

## Baselines existentes e medições desta auditoria

Os resultados de inferência abaixo são **históricos**, recuperados dos
artefatos locais e confrontados com os profiles/configs atuais. Não formam
um A/B novo e controlado entre checkpoints; sampling, contexto, cache e
conteúdo das respostas diferem. TTFT em cache não mede ingestão de documento novo.

| Profile / workload | Resultado registrado | Limitação |
|---|---|---|
| IQ2_XS dual 32k, código | 135,5 tok/s; TTFT 0,326 s | Amostra histórica; não qualificado para agentes |
| IQ2_XS dual 256k, código | 135,7 tok/s; TTFT 0,320 s | Não comparar como ganho causal sobre 32k |
| IQ2_XS dual 256k, 259641 tokens frescos | TTFT 87,27 s; decode 91,7 tok/s; recuperação 3/3 | Oracle curto de recuperação |
| IQ2_XS dual 976k, 989136 tokens frescos | TTFT 744,86 s; decode 57,0 tok/s; recuperação 3/3 | YaRN; JSON veio com fence; qualidade geral não demonstrada |
| IQ3_XXS dual 32k, código | Mediana 98,2 tok/s, 3 amostras | Instável perante guard de swap; contexto quase cheio não validado |
| IQ3_XXS GPU1 residente, código | Mediana 81,3 tok/s; TTFT mediano 0,534 s, 3 amostras | Não qualificado para agentes |
| IQ3_XXS GPU1, 29831 tokens frescos | TTFT 107,959 s; decode 46,6 tok/s; recuperação 3/3 | Falhou JSON puro; estabilidade desse request passou |
| IQ4_XS dual 32k, código | Mediana 76,3 tok/s; TTFT 0,503 s, 3 amostras | Testes de contexto longo interrompidos por swap |

O residente IQ3 atingiu 23087 MiB na GPU1 e manteve crescimento de swap zero
nos testes de contexto e benchmark. Seu carregamento registrou aproximadamente
0,026 GiB de crescimento no arquivo de métricas; “zero” não descreve todo o
ciclo de vida. O mínimo histórico de RAM disponível no benchmark foi 8,61 GiB.

### Reproduções novas, sem inferência real

| Evidência | Método | Resultado |
|---|---|---|
| Build limpo | Mesmos argumentos CMake do script, diretório temporário | Configure 0; build do alvo `strata` falha com código 2 |
| API | HTTP local com `MockEngine`, saídas determinísticas | Controles de tools/JSON/stop não aplicados |
| Packer | Dois GGUFs sintéticos, pesos diferentes, mesma geometria/pasta | Experts antigos mantidos; dense atualizado |
| Ranking | Trace sintético + `make_profile.py` com base padrão | Arquivo byte a byte idêntico à base; zero pares vindos do trace |
| Cancelamento | Engine/pipe sintéticos sem DONE, depois EOF explícito | Fechamento bloqueado durante observação; liberado com EOF |
| Tokenização | Dois textos sintéticos de código/PT; três repetições por braço | Cache BPE em memória preservou todos os IDs e reduziu custo local |
| Python | Suite selecionada existente | 130 testes, sem falhas; 3 inicialmente pulados por descoberta do tokenizer |
| C++ | Build isolado e quatro testes de startup/memória/conversation cache | 4/4 passaram |
| Go | Pacotes `stratahelp` e `backendschema` completos | Passaram |
| Profiles | `model-loader profile validate` | 6/6 passaram; não equivale a estabilidade/qualidade |

O primeiro comando Python não iniciou a suite porque não encontrou `gguf-py`.
O log dessa falha foi preservado; a execução bem-sucedida informa
`STRATA_GGUF_PY` apontando para a dependência existente. Os três testes de
detokenização foram executados separadamente com `STRATA_TOKENIZER`: 3/3 passaram.
Assim, os 130 casos selecionados foram efetivamente exercitados ao final; ver logs.

## Achados priorizados

Estados usados: **comprovado** = reprodução ou comportamento diretamente
demonstrado; **derivado** = cálculo explícito a partir de medidas; **hipótese**
= direção plausível que exige experimento. “Comprovado” num defeito funcional
não significa que o ganho de desempenho da futura correção já foi medido.
Esforços são relativos: pequeno, médio ou grande, sem estimativa de dias.

### STR-01 — Generalizar residência complementar para duas GPUs

**Domínio:** memória/multi-GPU. **Estado:** derivado (bloqueio comprovado;
benefício ainda hipotético). **Impacto:** alto. **Confiança:** alta no bloqueio,
média na solução. **Esforço:** grande. **Prioridade:** P1.

Código: `backends/strata-fork/src/program/generate.cpp:1272`,
`backends/strata-fork/src/program/generate.cpp:3417`,
`backends/strata-fork/src/core/expert_source.cpp:500`.

O parser rejeita `resident_cpu_experts` junto com `layer_split` ou caches
remotos. `--resident-experts` ativa esse mesmo caminho. O motivo estrutural
é que a inicialização e as trocas residentes são organizadas em torno de um
cache principal. O planner já recebe `additional_gpu_pairs`, mas empréstimos
de slots ao prefill só são planejados quando essa lista está vazia. Não basta
remover a condição do parser.

O pack IQ3 tem 24576 experts de 2176000 bytes cada. O dual medido guardou
17408 na VRAM. **Derivação:** `(24576 − 17408) × 2176000 = 15597568000 bytes`,
ou **14,526 GiB** de complemento básico. Esse valor não inclui cópias dos
experts emprestados ao prefill, staging, KV, MTP, dense, runtime ou sistema.
É uma oportunidade de capacidade, não prova de que um profile dual residente
já caberia ou atingiria uma velocidade específica.

Proposta: construir um plano global com união dos experts residentes das duas
placas; manter propriedade por stage; representar empréstimos e refills de
cada cache; realizar trocas host↔GPU sem invalidar ponteiros em graphs; assegurar
que falhas parciais liberem recursos. Preservar divisão 24/24.

Validação: testes sintéticos de união sem duplicatas, lend/refill e rollback;
paridade de experts/logits; depois carga, PT, 32k fresco, tools e soak, com
telemetria por placa e por processo. Comparar com GPU1 residente e dual mmap.
**Veredito:** prototipar após instrumentação STR-02/08. Ganho não quantificado.

### STR-02 — Diagnosticar pressão de memória antes de atribuir culpa ao quant

**Domínio:** memória/I/O. **Estado:** hipótese causal; interrupções históricas
medidas. **Impacto:** alto. **Confiança:** média. **Esforço:** médio. **Prioridade:** P1.

Código: `backends/strata-fork/src/core/expert_source.cpp:422`,
`backends/strata-fork/src/core/expert_source.cpp:681`,
`backends/strata-fork/src/prefill/prefill.cpp:1134`.

| Pack instalado | Experts, bytes | GiB |
|---|---:|---:|
| IQ2_XS | 35454976000 | 33,020 |
| Orca IQ3_XXS | 53477376000 | 49,805 |
| Uncensored IQ4_XS | 65431142400 | 60,938 |

O modo mmap usa arquivo compartilhado e páginas recuperáveis pelo kernel.
Preencher caches GPU, ler misses na CPU, preparar prefill e adaptar rankings
podem tocar regiões amplas do arquivo. O residente copia apenas seu plano e
libera páginas do arquivo com `madvise`/`posix_fadvise` durante a preparação.
O padrão diferente pode explicar maior previsibilidade, mas falta a atribuição
temporal da pressão e dos processos paginados.

IQ3: primeira carga cresceu swap aproximadamente 2,588 GiB, e PT na segunda
carga aproximadamente 2,279 GiB. IQ4: os dois prefills interrompidos mantiveram
mais de 42 GiB de MemAvailable. Isso contradiz a explicação simples “acabou a
RAM disponível”. Zram comprime páginas anônimas na RAM; seu uso lógico e seu
consumo físico não são iguais. mmap file-backed pode gerar page faults e I/O
sem aparecer como swap do Strata.

Próximo experimento: sincronizar `smaps_rollup`, `VmSwap`, faults e `/proc/PID/io`
do Python e do filho nativo, `memory.stat/events` do cgroup, PSI, `pswpin/out`,
`pgmajfault`, zram `mm_stat`, uso por GPU e tempos de carga/prefill. Não capturar
conteúdo de memória. Repetir a mesma carga desde o mesmo estado possível,
anotando page cache quente/frio e outros consumidores.

Somente depois avaliar descarte seletivo de páginas já copiadas, orçamento
de host staging e residência complementar. Descartar cache demais pode
transformar reutilização de RAM em leitura de SSD. **Veredito:** medir antes
de mudar política. Manter os guards; não reduzir swappiness, limpar caches
globalmente ou fazer swapoff para produzir um resultado favorável.

### STR-03 — Aplicar ou rejeitar explicitamente controles da API

**Domínio:** correção/API. **Estado:** comprovado. **Impacto:** alto para agentes.
**Confiança:** alta. **Esforço:** médio/grande. **Prioridade:** P1.

Código: `backends/strata-fork/serve/frontend.py:141`,
`backends/strata-fork/serve/frontend.py:300`,
`backends/strata-fork/serve/server.py:1008`,
`backends/strata-fork/serve/server.py:1651`.

As reproduções HTTP com mock aceitaram `tool_choice=required` e retornaram
texto comum; `tool_choice=none` retornou chamada; `parallel_tool_calls=false`
retornou duas chamadas; `response_format=json_object` retornou texto comum;
`stop=["END"]` retornou também o texto depois de END. Todas retornaram HTTP 200.
O parser também aceita string inválida para argumento declarado integer.

Esses testes provam falta de enforcement do servidor, independentemente da
quantização. Não provam que essa seja a única causa dos loops observados nos
modelos. Os testes anteriores de integridade com `required` não qualificam
o contrato de tool choice.

Proposta: validar o corpo da requisição; suportar seleção de ferramentas e
restrição de contagem; implementar stop incremental com retenção do sufixo
parcial; definir suporte real a JSON/schema. Quando um controle ainda não
puder ser atendido, retornar erro explícito antes de gerar. Gramática ou
constrained decoding exige integração com seleção de tokens; instrução no
prompt e validação apenas ao final não garantem o contrato em SSE.

Validação: ampliar probes para todos os modos, streaming/não streaming,
fronteiras UTF-8, interrupções e schema inválido; repetir positivos, controles
e continuação após sucesso nos modelos reais. Trade-off: buffering para
validação aumenta latência, e rejeitar campos antes ignorados pode exigir
ajuste nos clientes. **Veredito:** corrigir o contrato; ganho em retries/tokens
desperdiçados não quantificado.

### STR-04 — Impedir mistura silenciosa de pesos no packer

**Domínio:** integridade de artefatos/startup. **Estado:** comprovado.
**Impacto:** alto. **Confiança:** alta. **Esforço:** médio. **Prioridade:** P1.

Código: `backends/strata-fork/tools/iq_pack.py:291`,
`backends/strata-fork/tools/iq_pack.py:331`,
`backends/strata-fork/tools/iq_pack.py:337`.

Ao executar novamente com outro GGUF na mesma pasta, o packer reescreve dense
e layout, mas considera `experts.bin` válido somente porque o tamanho bate.
Tokenizers já existentes também são reaproveitados por existência. A fixture
reproduziu dois rc=0, mesmo SHA de experts, SHA de dense diferente e layout
apontando para a segunda origem. Não foi demonstrada corrupção nos packs
instalados, criados em diretórios independentes e previamente verificados.

Proposta: manifesto de identidade dos shards, hashes, versão do packer,
modo BF16 e geometria; construção em diretório temporário; publicação atômica
do conjunto completo e recusa de origem divergente. Um `READY.json` criado
por scripts externos não protege sozinho o packer/loader se eles não o leem.

Validação: mesma origem idempotente, origem diferente de mesmo tamanho,
interrupção em cada fase, falta de espaço e tokenizer divergente. Hash completo
de dezenas de GiB custa I/O; pode ser amortizado na preparação, sem rehash
integral a cada request. **Veredito:** implementar antes de reutilizar pastas.

### STR-05 — Tornar rebuild SM86 reproduzível e publicação reversível

**Domínio:** build/operabilidade. **Estado:** comprovado. **Impacto:** alto no
próximo rebuild. **Confiança:** alta. **Esforço:** pequeno. **Prioridade:** P1.

Código: `backends/strata-fork/backend-build.sh:4`,
`backends/strata-fork/CMakeLists.txt:35`,
`backends/strata-fork/CMakeLists.txt:176`,
`backends/strata-fork/CMakeLists.txt:380`.

`CMAKE_CUDA_ARCHITECTURES=86` não ativa `STRATA_ENABLE_CUDA`, cujo default é
OFF. A reprodução em diretório novo configura com sucesso e falha em
`cmake --build ... --target strata`: `No rule to make target 'strata'`.
O binário atual existe porque veio do build anterior; isso não valida o script.

Proposta: acrescentar `-DSTRATA_ENABLE_CUDA=ON`, explicitar native experts,
toolchain e dependência GGML pinada; usar diretório limpo e smoke tests;
registrar commit+dirty diff+hash+arquitetura+versões. Publicar por rename de
temporário ou diretório versionado após validação. O script atual usa `install`
direto sobre o executável compartilhado.

Os seis profiles compartilham o mesmo executável, logo uma substituição futura
pode afetar todos. Validar candidato em entrada/config separada antes de promover.
**Veredito:** implementar antes de qualquer rebuild de produção. Nesta auditoria
o script foi apenas reproduzido em diretório temporário.

### STR-06 — Limitar drenagem após cancelamento e detectar filho travado

**Domínio:** confiabilidade/concorrência. **Estado:** comprovado no contrato de
espera; travamento real de GPU não reproduzido. **Impacto:** alto quando ocorre.
**Confiança:** alta. **Esforço:** médio. **Prioridade:** P1.

Código: `backends/strata-fork/serve/server.py:397`,
`backends/strata-fork/serve/server.py:1022`,
`backends/strata-fork/serve/server.py:1070`.

O caminho normal usa `lines.get(timeout=10)` para heartbeats; o `finally`
envia STOP e drena com `lines.get()` sem prazo. Esse fechamento acontece
mantendo o lock do serviço. Se o filho permanecer vivo sem DONE/ERR/EOF,
a fila inteira pode ficar presa. O probe controlado bloqueou até a inserção
explícita de EOF; a ausência de timeout foi confirmada no código.

Proposta: prazo específico para STOP/drain, watchdog por progresso e estado
do filho; caso exceda, invalidar o engine e encerrar somente seus processos
gerenciados, drenando o protocolo antes de aceitar outra geração. Não soltar
o lock prematuramente: isso reintroduziria a corrida corrigida em `2343e1d`.

Validação: filho vivo e silencioso, pipe fechado, DONE atrasado, cancelamento
na fila/prefill/decode e request seguinte. O prazo deve distinguir ausência
de progresso de um prefill longo legítimo. **Veredito:** implementar com
testes de lifecycle; sem alegar redução de TTFT normal.

### STR-07 — Corrigir seed zero e tornar limites de sampling explícitos

**Domínio:** correção/reprodutibilidade. **Estado:** comprovado.
**Impacto:** médio. **Confiança:** alta. **Esforço:** pequeno/médio. **Prioridade:** P2.

Código: `backends/strata-fork/serve/server.py:289`,
`backends/strata-fork/serve/server.py:323`,
`backends/strata-fork/src/program/generate.cpp:4614`.

Seed 0 é omitida pelo Python; no engine, zero seleciona seed baseada no relógio.
Assim, `seed=0` não fornece a mesma reprodutibilidade de seed positiva.
`top_k=0` ou valor acima de 64 vira 64; essa limitação é intencional no código,
mas não equivale a amostrar sem top-k. Os profiles instalados usam seed 42 e
k20, portanto esses dois casos não explicam os resultados normais deles.

Proposta: representar presença da seed separadamente do seu valor; validar
tipos/faixas finitas e documentar ou rejeitar top-k sem suporte. Validação:
protocolo e repetição real por seed, incluindo zero, sem comparar igualdade
entre versões de kernel diferentes. **Veredito:** corrigir/documentar antes
de usar parâmetros amplos em clientes ou benchmarks.

### STR-08 — Medir cada GPU no Model Loader

**Domínio:** observabilidade. **Estado:** comprovado por fonte e evidência histórica.
**Impacto:** alto nas decisões de capacidade. **Confiança:** alta. **Esforço:** médio.
**Prioridade:** P1.

Código: `internal/service/monitor/gpu.go:71`,
`internal/service/benchmark/gpu_sampler.go:73`,
`backends/strata-fork/serve/telemetry.py:171`.

O comando nvidia-smi retorna uma linha por placa; o monitor Go faz apenas um
`csv.Reader.Read()`. O benchmark histórico GPU1 residente registrou
`peakVramMb=875`, enquanto a telemetria independente mediu GPU1 em 23087 MiB.
Isso é defeito da camada Model Loader, não alocação misteriosamente pequena
do Strata. A telemetria própria do Strata já contempla várias GPUs.

Proposta: identificar métricas por UUID/índice físico; expor pico por placa,
total simultâneo e utilização; distinguir uso do processo do desktop.
Somar picos ocorridos em instantes diferentes não produz pico total correto.
Validação com fixture de duas linhas, GPU1 isolada, dual, GPU ausente e
remapeamento `CUDA_VISIBLE_DEVICES`. Preservar compatibilidade do JSON antigo
com semântica documentada. **Veredito:** corrigir antes de automatizar tuning
com base no agregado atual.

### STR-09 — Calibrar prefill e staging juntos no IQ3 residente

**Domínio:** prefill/memória/PCIe. **Estado:** hipótese. **Impacto:** alto no TTFT
fresco. **Confiança:** média. **Esforço:** médio. **Prioridade:** P2.

Código: `backends/strata-fork/src/prefill/prefill.cpp:70`,
`backends/strata-fork/src/prefill/prefill.cpp:81`,
`backends/strata-fork/src/program/generate.cpp:3387`,
`backends/strata-fork/src/program/generate.cpp:3431`.

O profile estável IQ3 usa chunk 512; o streaming abrangente de experts começa
em 1024. Abaixo do limiar usa staging de 8 slots; acima, o default depende da
fração de memória pinned. Os limiares foram calibrados originalmente em outra
GPU e não constituem um ótimo comprovado para a 3090. O prefill fresco de
29831 tokens em 107,959 s torna esse caminho relevante ao usuário.

Proposta de A/B: 512→1024→2048, depois ring/prefill borrowing, um fator por
vez; medir `bytes_needed`, slots emprestados/refill, bytes H2D, misses,
faults e CPU. A fração pinned usada no dimensionamento é definida antes da
residência complementar e preservada deliberadamente; só recalculá-la se o
novo ring também entrar no plano de RAM/VRAM.

Trade-off: chunk maior pode aumentar throughput do prefill e piorar cache de
decode, pressão de RAM ou arredondamentos. Validação: TTFT fresco curto/médio/
quase cheio, decode subsequente, copy exato, PT e tools. IQ4 já falhou com
8192 e 2048: reduzir chunk não é cura demonstrada para swap. **Veredito:**
prototipar/medir; nenhum multiplicador de velocidade previsto.

### STR-10 — Evitar uma recalibração de ranking que não altera o ranking

**Domínio:** cache/startup. **Estado:** comprovado. **Impacto:** médio.
**Confiança:** alta. **Esforço:** pequeno. **Prioridade:** P2.

Código: `backends/strata-fork/tools/make_profile.py:82`,
`backends/strata-fork/src/core/expert_cache.cpp:13`.

O utilitário acrescenta os traces **depois** dos pares da base. A base atual
já contém todos os 24576 pares; portanto, com defaults, nenhum trace pode
alterar a ordem. O probe produziu arquivo idêntico à base e mensagem
`24576 from the base, 0 from the traces`. Não é um erro contra a descrição
“preservar a base”, mas é uma armadilha concreta para quem espera retuning.

Proposta: documentar `--no-base` para ranking inteiramente derivado de traces;
adicionar modo explícito de reranking ou mistura ponderada. Usar corpus do
checkpoint uncensored com PT, código e ferramentas, separado do conjunto de
avaliação. Não sobrescrever `data/expert-profile.bin` compartilhado: gerar
arquivo por checkpoint. O índice atual verifica geometria, não identidade
do modelo que gerou o ranking.

Validação: hits/misses e wall time nas primeiras requests e após adaptação,
mesmo número de slots; não otimizar só hit rate. O contador atual de hits
exclui experts enviados via PCIe (`generate.cpp:4998`). **Veredito:** medir
ranking próprio; ganho pode ser pequeno no IQ2 32k, que já cacheava todos os
experts, e maior em quants com cache parcial.

### STR-11 — Calibrar CPU/PCIe com os dois grupos de L3 do 9950X3D

**Domínio:** CPU/transferência. **Estado:** hipótese. **Impacto:** médio/alto nos
misses. **Confiança:** média. **Esforço:** médio. **Prioridade:** P2.

Código: `backends/strata-fork/src/kernels/cpu/pool.cpp:59`,
`backends/strata-fork/src/kernels/cpu/pool.cpp:176`,
`backends/strata-fork/src/kernels/cpu/native_expert.cpp:85`,
`backends/strata-fork/src/program/generate.cpp:1983`.

O pool já escolhe um lógico por núcleo físico, respeita affinity e reserva o
primeiro; não há justificativa para simplesmente trocar 15 por 32 workers.
O sistema mostra grupos de L3 para núcleos 0–7 e 8–15. Não há escolha explícita
por capacidade de L3/CCD no planner. Além do pool MoE, o PLE usa threads de I/O
e o prefill pode usar threads de staging.

A auto proporção CPU/PCIe depende principalmente do probe H2D; não incorpora
todo o custo observado do kernel IQ3, contention de RAM ou workload em PT.
`strata_tune.pcie_frac` permite A/B por request, já existe no protocolo.

Experimentos: manter 15 como anchor; comparar 8/12/15 workers, pinning físico
controlado, e proporções PCIe ao redor da seleção automática, separadamente.
Medir wall time, `ms_pool`, migrations, bandwidth, zram/PSI e impacto no desktop.
Depois testar afinidade por grupo de L3; não supor que restringir a um CCD é
mais rápido. **Veredito:** medir antes de implementar scheduler novo. Ganho
não quantificado; preservar seleção simétrica de camadas GPU.

### STR-12 — Investigar fusão de kernels do MoE no SM86

**Domínio:** CUDA/decode. **Estado:** hipótese. **Impacto:** médio.
**Confiança:** média/baixa no ganho. **Esforço:** grande. **Prioridade:** P3.

Código: `backends/strata-fork/src/core/verify.cpp:688`,
`backends/strata-fork/src/kernels/cuda/iq_kernels.cu:711`,
`backends/strata-fork/src/kernels/cuda/iq_kernels.cu:736`.

O caminho grouped lança gate/up, SwiGLU, quantização Q8_1 e down em etapas.
Uma direção concreta é fundir SwiGLU e quantização por bloco, reduzindo
materialização de intermediários. Outra é autotuning de blocos para IQ2/IQ3
em SM86. O QSA já contém MMA/`cp.async` em Ampere e o prefill já usa MMQ/cuBLAS:
“ligar Tensor Cores” genericamente não é uma otimização nova.

Antes da implementação, usar `STRATA_VERIFY_PROFILE`, `STRATA_DECODE_TIMING`
e Nsight quando disponível para localizar tempo e stalls, com perturbação
do profiler registrada. `--stage-timing --no-capture --no-pool` não representa
a inferência completa: omite experts. CUDA Graphs já reduzem parte do custo
de launches, então contar launches não mede o ganho possível.

Validação: oracle independente CPU/GPU, erros numéricos, agrupamentos T=1..4,
PT/código/tool bytes, VRAM e wall time. Preservar a ordem de arredondamento;
fusões podem mudar tokens e aumentar registradores/reduzir ocupação.
**Veredito:** medir antes de otimizar. Sem estimativa percentual.

### STR-13 — Tratar MTP como economia medida e controlar paridade numérica

**Domínio:** especulação/correção. **Estado:** hipótese de otimização, com
restrições verificadas em fonte. **Impacto:** médio/alto. **Confiança:** média.
**Esforço:** médio. **Prioridade:** P2.

Código: `backends/strata-fork/src/program/generate.cpp:4793`,
`backends/strata-fork/src/spec/draft_policy.cpp:55`,
`backends/strata-fork/src/kernels/cpu/native_expert.cpp:92`,
`backends/strata-fork/include/strata/core/verify.hpp:73`.

O target verifica os drafts; aceitação não significa que o head uncensored
seja pareado ao checkpoint. IQ3 usa o MTP Q2_0 compartilhado. Sua aceitação de
código histórico foi 795/937; isso não prevê PT/tools. No IQ2 256k, reduzir
spec-min-p de 0,7 para 0,5 não melhorou o A/B realizado. Suffix drafting e
política adaptativa já existem; não reapresentá-los como funcionalidade ausente.

Os kernels CPU alternam vec_dot de um token e kernel de vários tokens a partir de
`STRATA_IQ_MT_MIN` (default 2). Essa diferença de aritmética pode afetar
reprodutibilidade entre janelas; o próprio código oferece valor 1 para a
comparação. Uma igualdade de outputs em poucas frases não prova paridade
geral. Sampling/history são propagados ao último stage pelo verifier: a
hipótese “GPU1 ignora sampling” foi rejeitada na inspeção.

Experimentos: janela/spec-min-p com corpus misto e wall time por token útil;
`STRATA_IQ_MT_MIN=1` como braço explícito de paridade; validar exact-copy e
tools antes de promover. `--serve` exige spec≥2/MTP no código atual: desligar
MTP não é um A/B disponível simplesmente usando spec=1 nesse servidor.
**Veredito:** medir; não aumentar especulação apenas pela taxa de aceitação.

### STR-14 — Cache BPE limitado reduz tokenização, com benefício absoluto modesto

**Domínio:** CPU/API. **Estado:** comprovado no microbenchmark/protótipo.
**Impacto:** baixo/médio. **Confiança:** alta nas fixtures, limitada para uso
real. **Esforço:** pequeno. **Prioridade:** P3.

Código: `backends/strata-fork/tools/strata_tokenizer.py:140`,
`backends/strata-fork/tools/strata_tokenizer.py:158`,
`backends/strata-fork/serve/server.py:919`.

O tokenizer recalcula BPE de cada fragmento; `Service.prepare` renderiza e
tokeniza o prompt completo a cada request, antes do lock de geração. O probe
instalou um cache apenas no objeto em memória, sem editar código de produção.

| Fixture | Tokens | Mediana sem cache | Cache inicialmente vazio | Cache aquecido |
|---|---:|---:|---:|---:|
| Código/PT, 201780 bytes | 63780 | 95,39 ms | 34,99 ms | 34,73 ms |
| PT, 217884 bytes | 70884 | 101,26 ms | 37,06 ms | 36,97 ms |

Três repetições por braço; todos os IDs foram idênticos. Os textos são
repetitivos e favorecem cache; não houve distribuição aleatória dos braços.
Não converter esse ganho em promessa de tok/s do modelo: a economia observada
é de dezenas de milissegundos, enquanto prefill fresco leva segundos/minutos.

Proposta: cache por instância de tokenizer, resultado imutável, limite de
16384 entradas e bypass de fragmentos acima de 256 caracteres, como no probe;
limpeza ao trocar tokenizer. Quantificar memória real, já que limite de
entradas não equivale a limite exato em bytes. Validar Unicode, tokens especiais,
texto de alta entropia, concorrência e equivalência com tokenizer de referência.
**Veredito:** pequena melhoria opcional depois das correções P1.

### STR-15 — Separar cache de prefixo de estacionamento de conversas

**Domínio:** contexto/cache. **Estado:** hipótese de otimização; limites
comprovados. **Impacto:** médio para múltiplas conversas. **Confiança:** média.
**Esforço:** médio/grande. **Prioridade:** P3.

Código: `backends/strata-fork/src/program/generate.cpp:1194`,
`backends/strata-fork/include/strata/core/conversation_cache.hpp:23`,
`backends/strata-fork/src/core/conversation_memory.cpp:36`.

Prefix checkpoints já funcionam com split e explicam TTFT baixo nos benchmarks
em cache. Estacionar sessões completas tem orçamento separado, default zero,
e é recusado com layer split. Ativá-lo não conserta paginação dos experts;
consome RAM adicional. O floor de memória de conversas lê MemAvailable do host,
enquanto o planner de experts também considera cgroups: a diferença importa
se futuramente o backend rodar em um serviço com limite de memória.

Proposta: só avaliar estacionamento se o workload realmente alternar chats;
no dual, antes suportar snapshot/restauração de todos os stages, MTP e páginas
KV autoritativas. Orçamento conjunto com complementos residentes e cgroup.
Validação: A→B→A, prefixo comum, isolamento de respostas, cache inválido,
eviction e picos transitórios. **Veredito:** adiar para demanda comprovada;
priorizar prefixos estáveis e contexto apropriado ao workload atual.

### STR-16 — Medir PLE e handoffs antes de mudar o transporte

**Domínio:** I/O/multi-GPU. **Estado:** hipótese. **Impacto:** médio condicionado
ao workload. **Confiança:** média. **Esforço:** médio/grande. **Prioridade:** P3.

Código: `backends/strata-fork/src/platform/direct_file.cpp:263`,
`backends/strata-fork/src/platform/direct_file.cpp:313`,
`backends/strata-fork/src/ngram/ple_reader.cpp:34`,
`backends/strata-fork/src/program/generate.cpp:3637`,
`backends/strata-fork/src/core/verify.cpp:1053`.

O PLE tem cache bounded e Linux `O_DIRECT` com 16 threads emissoras por default.
Há latência por leitura disponível no leitor. Comparar `STRATA_IO_THREADS`
4/8/16 e cache de linhas usando taxa de hits, p50/p95 de I/O, tempo bloqueado
e latência global; só considerar io_uring se syscall/threading aparecer como
custo material. Não carregar indiscriminadamente toda a tabela PLE na RAM.

O split atual é sequencial; handoffs usam pinned mapped host memory e o
verifier sincroniza antes de chamar o próximo stage. Não é tensor parallel
e não oferece duplicação automática de throughput. P2P direto poderia reduzir
o custo da transferência de fronteira, mas existe apenas uma fronteira por
janela numa divisão 24/24; não há evidência de que ela domine o tempo.

Validação: contabilizar bytes/latência por stage e por prefill, comparar com
decode completo, preservar sincronização e ownership. `NCCL_P2P_DISABLE` e
`GGML_CUDA_P2P` de outros backends não habilitam esse caminho no Strata.
**Veredito:** medir antes de reescrever transporte; residência/cache tem
prioridade maior na evidência atual.

## Conformidade dos profiles atuais

Todos usam o checkout em `model-loader/backends`, `max-context` coerente com o
sufixo, KV int8, MTP ativo, reserva 1536 MiB, 15 workers, nenhuma porta fixa
nos args e nenhum DRY. Todos validam no schema. Pinning do residente é feito
por `gpu:[1]` no JSON e `child_env`, não por flags de llama.cpp.

| Profile, abreviado pelo sufixo | Regra / severidade | Atual → esperado | Responsável pela próxima ação |
|---|---|---|---|
| IQ2_XS dual 32k | Qualidade de tools / aviso | Integridade passou, repetição após sucesso; não promover a agente sem revalidação | Correção API + troubleshoot de modelo |
| IQ2_XS dual 256k | Qualidade de tools / aviso | Contexto e long tools passaram; continuação curta repete | Idem; anchor de contexto nativo |
| IQ2_XS dual 976k | Contexto estendido / aviso | YaRN e recuperação quase cheia passaram; qualidade além do smoke não demonstrada | Full tuning/avaliação longa |
| IQ3_XXS GPU1 resident 32k | Tools / aviso | Estabilidade de geração/contexto passou; ferramentas e JSON puro falham | API + avaliação, manter status não qualificado |
| IQ3_XXS dual mmap 32k | Estabilidade / erro operacional | Guard de swap interrompeu testes; deveria completar a matriz | STR-01/02; experimental |
| IQ4_XS dual mmap 32k | Estabilidade / erro operacional | Short decode funciona; quase cheio interrompido | STR-02; experimental |

Ranking de conformidade com a finalidade de **geração/contexto**: IQ2 32k/256k
e IQ3 residente são os anchors; IQ2 976k tem escopo de qualidade mais limitado;
IQ3/IQ4 dual continuam experimentais. Não existe, entre esses profiles,
qualificação completa para agente autônomo. Ranking de conformidade não é
ranking de qualidade entre modelos.

## O que não foi identificado como oportunidade justificada

- **Migrar automaticamente para FP8/FP4 de compute:** SM86 não oferece a
  aceleração nativa esperada nessas gerações; KV int8 existente deve permanecer.
- **Aumentar workers para 32:** o pool já evita SMT duplicado e reconhece os
  16 núcleos; requer medição, não regra por contagem de threads lógicas.
- **Particionar camadas de forma desigual:** fora da política desta máquina;
  problemas de headroom devem ser resolvidos com orçamento por GPU e tuning.
- **Portar números da 5070/5090:** curvas e comentários upstream orientam
  experimentos, não constituem medidas para 3090 a 270 W.
- **Reescrever detokenização para torná-la incremental:** já é incremental
  com `token_bytes`; a versão quadrática só permanece como fallback de teste.
- **Ignorar sampling na segunda GPU:** `set_sampling` e `set_history`
  propagam ao próximo verifier; não confirmado como defeito.
- **Eliminar todas as sincronizações:** algumas protegem flags CPU/GPU,
  refills, ownership e passagem de stage; removê-las sem prova pode corromper
  respostas ou travar o engine.
- **Interpretar correção de build como melhoria de inferência:** ela restaura
  a capacidade de compilar; nenhum ganho de tokens/s decorre dela por si só.
- **Concluir que os packs instalados estão misturados:** o defeito do packer
  foi demonstrado em reuso de pasta; não há essa evidência para os packs atuais.

## Plano de execução e critérios de promoção

| Ordem | Entrega concreta | Critério de aceitação |
|---|---|---|
| 1 | Build explícito SM86, manifesto/pack transacional, API e drenagem | Reproduções deste relatório passam a rejeitar/atender corretamente os casos; invariantes de fila mantidas |
| 2 | Monitor por GPU + memória do processo/zram/cgroup | Reconciliar os mesmos timestamps com nvidia-smi e /proc; identificar o dono da pressão |
| 3 | Protótipo dual residente 24/24 | Correção de lend/refill/swap, paridade, nenhum guard violado na matriz |
| 4 | Prefill 512/1024/2048 e CPU/PCIe/ranking | A/B no mesmo binário/checkpoint, uma variável por vez, benefício repetível sem regressão de correção |
| 5 | MTP/paridade, kernels, PLE, tokenizer e conversas | Implementar apenas as hipóteses cuja instrumentação mostre custo relevante |

Para cada variante real: carregar somente por `model-loader instance start`,
inferir pelo proxy com o ID correto, preservar o profile anchor e restaurar o
estado inicial ao final. Não substituir o binário usado pelos seis profiles
durante o experimento.

A matriz mínima de promoção deve conter:

1. Carga em estado documentado, código curto aquecido, geração PT longa e
   interrupção/retomada do serviço.
2. Prefill fresco e prefixo em cache separados, nos fills 5/25/50/90%; entrada
   real contada pelo tokenizer, output e finish registrados.
3. Pelo menos três repetições aquecidas por braço, ordem alternada; mediana,
   dispersão, TTFT, tempo total e tokens úteis. Três pontos não sustentam p99.
4. Memória de cada GPU, RAM/zram por processo quando disponível, I/O, PSI,
   temperatura, clocks, potência e throttling; manter 270 W e 23552 MiB/card.
5. Guards existentes: mínimo 2 GiB de MemAvailable e máximo 2 GiB de crescimento
   global de swap, com três amostras consecutivas; diagnosticar antes de mudar
   seu significado. Não relaxá-los para aprovar o candidato.
6. Exact-copy, PT, recuperação longa, JSON/schema, tools positivos/controles
   e continuação após resultado de sucesso. Medir custo de chamadas repetidas.
7. Regressão nos anchors IQ2 32k/256k; quase cheio de 976k se a mudança tocar
   KV, prefill, cache, RoPE ou memória compartilhada por esse caminho.

Rollback: manter executável/config anteriores e selecionáveis, recusar
promoção por corrupção, Xid/OOM, violações dos guards ou regressão material
de latência fora da variabilidade observada. Para qualificação de agente,
usar os oracles e limites estatísticos da skill; startup, 10 casos de código
ou quatro casos de integridade não bastam.

## Artefatos e limites de confiança

Diretório da auditoria: [docs/reports/strata-audit-2026-10-01](docs/reports/strata-audit-2026-10-01/).

- [Ambiente](docs/reports/strata-audit-2026-10-01/environment.json),
  [P2P/arquitetura/zram](docs/reports/strata-audit-2026-10-01/environment-extra.json),
  [profiles e validações](docs/reports/strata-audit-2026-10-01/profiles.json),
  [inventário e hashes](docs/reports/strata-audit-2026-10-01/source-manifest.json).
- [Reprodução de build](docs/reports/strata-audit-2026-10-01/build-reproduction.json),
  [probes executáveis](docs/reports/strata-audit-2026-10-01/probe_contracts.py),
  [resultados de contratos](docs/reports/strata-audit-2026-10-01/contract-probes.json),
  [ranking](docs/reports/strata-audit-2026-10-01/ranking-probe.json).
- [Tokenização: script](docs/reports/strata-audit-2026-10-01/probe_tokenizer.py),
  [amostras](docs/reports/strata-audit-2026-10-01/tokenizer-probe.json),
  [tamanhos dos packs](docs/reports/strata-audit-2026-10-01/pack-footprints.json).
- [Python](docs/reports/strata-audit-2026-10-01/python-tests-with-deps.log),
  [detokenização](docs/reports/strata-audit-2026-10-01/detokenizer-tests.log),
  [C++](docs/reports/strata-audit-2026-10-01/cpp-tests.json),
  [Go](docs/reports/strata-audit-2026-10-01/go-tests-all.log).
- Relatórios anteriores: [IQ2 256k](docs/reports/strata-iq2-xs-256k-calibration-2026-10-01.md),
  [IQ2 contexto máximo](docs/reports/strata-iq2-xs-context-2026-10-01.md),
  [IQ3](docs/reports/strata-orca-iq3-xxs-calibration-2026-10-01.md),
  [IQ4](docs/reports/strata-iq4-xs-uncensored-2026-10-01.md).
  O [índice de evidências históricas](docs/reports/strata-audit-2026-10-01/historical-evidence-index.json)
  registra caminhos/hashes locais; nem todos os arquivos grandes são copiados para o repo.

As fixtures HTTP provam comportamento do adaptador, não acurácia de modelo.
Os quatro testes C++ não exercitam kernels CUDA nem residência dual ainda
inexistente. Os testes Go cobrem schema/apresentação, não geração. Não houve
novo benchmark de GPU, teste de banda P2P, comparação de qualidade entre
checkpoints ou atualização do upstream. O relatório se refere ao checkout
local e aos hashes registrados, não a uma versão futura do projeto.

| Estado principal dos 16 achados | Quantidade |
|---|---:|
| Comprovado | 8 — STR-03/04/05/06/07/08/10/14 |
| Derivado | 1 — STR-01 |
| Hipótese | 7 — STR-02/09/11/12/13/15/16 |

Há um protótipo com redução local de tempo medida (BPE, duas fixtures) e uma
derivação explícita de capacidade (complemento dual IQ3). Os demais ganhos de
otimização não foram quantificados. Não se somam ganhos de tokenização,
prefill, decode e capacidade como se fossem uma única economia.
