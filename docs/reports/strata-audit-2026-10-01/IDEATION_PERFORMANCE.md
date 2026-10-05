# Strata + Model Loader — relatório consolidado de auditoria

**Data:** 2026-10-01. **Alvo:** `backends/strata-fork`, sua integração com Model
Loader e os seis profiles Strata instalados. **Resultado:** 20 achados
consolidados, com origem, evidência, prioridade e critérios de validação.

Este documento substitui as duas análises como referência para decidir o que
corrigir e medir. Preserva a distinção entre defeito demonstrado, cálculo de
capacidade e hipótese de ganho. Os documentos originais permanecem disponíveis
para rastreabilidade: [Codex](IDEATION_PERFORMANCE.original-2026-10-01.md) e
[Claude Opus 5.5](docs/reports/strata-independent-claude-opus-5-5-2026-10-01.md).

## 1. Conclusões para decisão

**O IQ2_XS original funciona nas duas RTX 3090.** Existem execuções históricas
concluídas com 32k, 256k e 1000000 tokens configurados. Não há evidência de que
o layer split esteja genericamente quebrado. IQ3_XXS e IQ4_XS uncensored dual,
porém, tiveram testes interrompidos pelo guard de crescimento global de swap.
O IQ3 na GPU1 com complemento residente completou a calibração de geração e
contexto. Nenhum desses profiles está plenamente qualificado para agentes.

As prioridades são:

1. **Integridade e confiabilidade:** impedir reaproveitamento incorreto de
   pesos, tornar o rebuild reproduzível, cumprir o contrato da API e corrigir
   os dois problemas distintos de cancelamento.
2. **Medição e concorrência:** registrar memória por GPU/processo, tornar a
   configuração efetiva visível e corrigir a herança de afinidade dos threads
   auxiliares. O impacto dessa afinidade na inferência ainda precisa de A/B.
3. **Capacidade dual:** generalizar o complemento residente para os dois
   caches e evitar carregar pesos densos de camadas alheias em cada GPU.
   São oportunidades fundamentadas; não há ganho de tok/s validado.
4. **Calibração:** prefill/staging, ranking de experts, CPU/PCIe, MTP e PLE,
   medidos em sequência, preservando os profiles de referência.

**A causa do crescimento de swap continua aberta.** O swap é zram/zstd;
page faults de arquivos mmap e swap de páginas anônimas são fenômenos
distintos, possivelmente acoplados via reclaim: pressão de page cache pode
favorecer a recuperação de páginas anônimas para zram. Essa é uma hipótese,
não causalidade estabelecida por swappiness 180. O guard global não identifica
qual processo foi paginado. O IQ4
ultrapassou o guard mantendo mais de 42 GiB de MemAvailable: “acabou a RAM”
não explica adequadamente a evidência existente. MemAvailable inclui memória
recuperável e não exclui atividade de reclaim. IQ2 256k também cresceu swap
(+1,072 GiB no request longo), abaixo do guard; funciona como controle para S10.

**Esta consolidação não aplica correções nem altera profiles.** Reutilizou
medições arquivadas e verificou trechos de código. Nenhum modelo real foi
iniciado; não houve novo benchmark de inferência, rebuild de produção,
alteração de potência, troca de pesos ou ajuste global do sistema.

## 2. Método, procedência e correções de interpretação

A análise Codex inventariou 280 arquivos/73777 linhas e revisou os caminhos
ativos. Esse inventário não significa revisão de todas as linhas. A análise
independente foi executada pelo Claude Code com `claude-opus-5-5`, esforço
`xhigh`, modo headless e `--allow-dangerously-skip-permissions`. O processo
terminou com sucesso, sem consultar o relatório ou as evidências do Codex.
Só depois do encerramento as conclusões foram confrontadas.

Ordem de preferência: código e resultados brutos → medições históricas com
configuração conhecida → derivações explícitas → hipóteses. Concordância
entre auditores não substitui reprodução. Números de workloads diferentes
não são somados nem tratados como A/B.

| Afirmação original | Tratamento no consolidado |
|---|---|
| mmap “desliga DMA do prefill” — Claude C3 | **Corrigida:** o prefill copia origem pageable para staging, normalmente pinned, e então faz DMA para a GPU. Há uma cópia adicional na CPU; o DMA do buffer de staging continua existindo. |
| Cache frio “a cada start” — Claude C4 | **Restringida:** `mincore` mediu 0% no momento observado. Não comprova o estado após todo reinício nem a causa da evicção. |
| Seed 42 é um defeito e produz sempre a mesma resposta — Claude C5 | **Reclassificada:** configuração de sampling; pode ser intencional em calibração. O probe comprovou a seed enviada, não identidade de textos de um modelo real. Seed zero e top-k silenciosamente limitado são questões separadas. |
| `reasoning_effort: none` é configuração oculta inadequada — Claude D5 | **Contextualizada:** preserva o modo não pensante usado na calibração. Precisa aparecer nos metadados efetivos; não deve ser removida automaticamente. |
| Prefill 8192 oferece “16×” — Claude D1/O5 | **Restringida:** 16 = 8192/512 é um fator ideal de amortização se tráfego por chunk e demais condições forem iguais. Não é ganho medido de TTFT/vazão; roteamento, borrowing e ring mudam. |
| Microteste de CPU 90 ms → 22,6 s — Claude C2 | **Mantido como teste adversarial:** demonstra o mecanismo de contenção; não quantifica a perda no engine real. |
| IQ3 precisa de aproximadamente 14,8 GiB complementares — estimativa Claude | **Refinada com logs:** 14,526 GiB para os slots registrados; exclui empréstimos de prefill, staging e demais alocações. |
| IQ4 precisa de aproximadamente 26,8 GiB complementares — estimativa Claude | **Refinada com logs:** 26,373 GiB para os slots registrados, com as mesmas exclusões. |
| Alterar `pcie_frac` em qualquer profile melhora a divisão CPU/GPU | **Restringida:** no caminho de misses do decode com mmap puro a fração efetiva é zero. Só calibrar esse parâmetro onde a origem permitir o caminho PCIe. |
| P2P daria cerca de 1–2% — Claude D9 | **Estimativa retirada:** capacidade P2P foi confirmada; ganho no Strata não foi medido. |
| SSD “DRAM-less” explica o custo — Claude C4 | **Não usado como causa estabelecida:** os microtestes demonstram custos de I/O nesse volume, sem isolar controlador, DRAM, ocupação ou topologia. |
| Remover a recusa de dual residente seria suficiente | **Rejeitada:** o planner aceita pares adicionais, mas lend/refill e trocas adaptativas ainda precisam de tratamento por stage. |

## 3. Ambiente e configuração de referência

### Hardware e revisões

| Item | Evidência consolidada |
|---|---|
| CPU | Ryzen 9 9950X3D, 16 núcleos/32 threads; AVX-512; dois grupos de L3; 96 MiB no CCD0 e 32 MiB no CCD1 conforme inventário independente |
| GPUs | 2 × RTX 3090, 24 GiB nominais, SM86; GPU0 também atende o desktop |
| Limites | 270 W/card; política de até 23552 MiB/23 GiB por placa |
| Transporte | PCIe 4.0 x8 por GPU, PHB, sem NVLink; leitura/escrita P2P e `cudaDeviceCanAccessPeer` disponíveis |
| H2D medido pelo Claude | Aproximadamente 13,35 GB/s pinned por GPU; 26,7 GB/s agregados em cópias simultâneas. Esses números não são banda P2P. |
| RAM/swap | Aproximadamente 60,49 GiB de RAM; zram/zstd de aproximadamente 16 GiB lógicos; 5,78 GiB usados no snapshot inicial; swappiness 180 |
| Sistema | Linux 7.2.7-arch1-1; NVIDIA 610.57.04; CUDA toolkit disponível 13.4.92 |
| Volume dos modelos | `/home/diogo/models`, ext4 em nvme0n1p1/PCH-RVR-1TB, aproximadamente 67 GiB livres no snapshot inicial |
| Volumes distintos | Checkout em `/home/diogo/dev`, nvme1n1p1/XPG; KC3000 em nvme2n1 hospeda outro volume. Espaço livre de um não deve ser atribuído ao outro. |
| Fork | Strata 0.1.30, HEAD `97cb786c006ef1f8cdfec67ca7ef7b58f464323d` + alterações locais |
| Model Loader | HEAD `adf12cb353acb41990c780d67d6a9dc3f0794189` + alterações locais, incluindo integração Strata |
| Identificação do binário | SHA-256 `545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999`; cubins SM86 verificados |

A versão antiga de driver 610.43.02 em referências gerais não descreve esta
auditoria. Os baselines Strata aqui citados foram registrados a 270 W/card;
números antigos de outros backends a 290 W não são comparadores equivalentes.
Espaço livre, RAM disponível e page cache são snapshots, não constantes.

### Profiles e orçamento de experts

Os seis profiles usam o mesmo binário e o servidor Python do checkout em
`backends/strata-fork`. O Model Loader gerencia porta e processo; o JSON
preparado contém as opções nativas. Comuns: KV int8, 15 workers, reserva de
1536 MiB e MTP com spec 4. Dual usa divisão 24/24. Os configs usam seed 42 e
os arquivos adjacentes `.shared-settings.json` usam `reasoning_effort: none`.

| Profile abreviado | Contexto | Modo / prefill | Experts residentes fora do prefill | Slots disponíveis para empréstimo ao prefill, conforme log |
|---|---:|---|---:|---|
| IQ2_XS dual 32k | 32768 | mmap / auto | 24576/24576 | 3103 + 2975 |
| IQ2_XS dual 256k | 262144 | mmap / auto | 23461/24576 | 3102 + 2966 |
| IQ2_XS dual 976k | 1000000 | mmap / auto, YaRN fator 4 | 15506/24576 | 3104 + 2984 |
| Orca IQ3_XXS dual 32k | 32768 | mmap / 512 | 17408/24576 | 186 + 186 |
| Orca IQ3_XXS GPU1 residente 32k | 32768 | complemento residente / 512 | Cache + complemento | 186, com cópias no complemento RAM |
| Uncensored IQ4_XS dual 32k | 32768 | mmap / auto | 13940/24576 | 1499 + 1499; no teste 2048: 495 + 495 |

Contagens são de execuções históricas identificadas, não promessa de um novo
start. VRAM disponível e dimensionamento automático podem alterá-las. IQ2
usa blobs de tamanhos diferentes; proporção de slots não é proporção de bytes.
O empréstimo efetivo é ajustado ao tamanho do segmento do request: prompt
curto pode emprestar menos. Durante um segmento grande, os slots emprestados
deixam de conter experts e são reabastecidos ao final. Portanto, IQ2 32k
inteiramente residente **fora do prefill** ainda usa a origem mmap no prompt
e no refill. Seus logs indicam aproximadamente 4,08 GiB de buffers emprestados
por GPU; não usar uma média de blob para transformar isso em bytes exatos de
experts. O caminho está em
`backends/strata-fork/src/program/generate.cpp:4572`.

| Pack | Experts em disco | Complemento básico derivado da residência dual registrada |
|---|---:|---|
| IQ2_XS | 35454976000 bytes = 33,020 GiB | Depende do contexto e dos experts específicos; não estimar por uma única média de blob |
| IQ3_XXS | 53477376000 bytes = 49,805 GiB | `(24576 − 17408) × 2176000 = 15597568000 bytes = 14,526 GiB` |
| IQ4_XS | 65431142400 bytes = 60,938 GiB | `(24576 − 13940) × 2662400 = 28317286400 bytes = 26,373 GiB` |

Esses complementos não incluem cópias para lend/refill, staging, dense, KV,
MTP, sistema ou outros processos. Não demonstram que dual residente já cabe.
Se o futuro plano residente mantiver também cópias de todos os slots
emprestáveis dos logs, como faz o residente de uma GPU, a conta será:

| Configuração histórica | Cópias adicionais para empréstimo | Complemento + essas cópias |
|---|---:|---:|
| IQ3 dual, prefill 512 | `372 × 2176000` = 0,754 GiB | 15,280 GiB |
| IQ4 dual, prefill auto | `2998 × 2662400` = 7,434 GiB | 33,806 GiB |
| IQ4 dual, teste prefill 2048 | `990 × 2662400` = 2,455 GiB | 28,827 GiB |

São derivações condicionais, ainda sem staging e demais custos. Uma nova
configuração pode alterar cache/chunk e os empréstimos, exigindo novo cálculo.

## 4. Baselines e medições aproveitados

### Inferência histórica

| Workload | Resultado registrado | Limitação decisiva |
|---|---|---|
| IQ2_XS dual 32k, código | 135,5 tok/s; TTFT 0,326 s | Amostra histórica, sem qualificação completa de agente |
| IQ2_XS dual 256k, código | 135,7 tok/s; TTFT 0,320 s | Não estabelece ganho causal sobre 32k |
| IQ2_XS 256k, 259641 tokens frescos | TTFT 87,27 s; decode 91,7 tok/s; recuperação 3/3 | Oracle curto de recuperação |
| IQ2_XS 976k, 989136 tokens frescos | TTFT 744,86 s; decode 57,0 tok/s; recuperação 3/3 | YaRN; resposta com fence; qualidade geral não demonstrada |
| IQ3 dual, código | Mediana 98,2 tok/s, 3 amostras | Instável nos testes com guard de swap |
| IQ3 GPU1 residente, código | Mediana 81,3 tok/s; TTFT 0,534 s, 3 amostras | Geração/contexto estáveis; ferramentas ainda inadequadas |
| IQ3 GPU1, 29831 tokens frescos | TTFT 107,959 s; decode 46,6 tok/s; recuperação 3/3 | JSON puro falhou |
| IQ4 dual, código | Mediana 76,3 tok/s; TTFT 0,503 s, 3 amostras | Contexto longo interrompido pelo guard de swap |

Sampling, checkpoint, conteúdo e cache diferem entre linhas. Isso não é um
benchmark controlado de qualidade ou velocidade entre quants. TTFT em prefixo
já processado e TTFT de documento fresco devem permanecer separados.

### Reproduções e microtestes das auditorias

| Evidência | Resultado | Alcance |
|---|---|---|
| Contratos HTTP com MockEngine | `tool_choice`, contagem de calls, JSON e stop não aplicados | Comportamento do adaptador; independente da quant |
| Packer sintético | Mesmo hash de experts antigos, dense novo e layout apontando para outra origem | Corrupção potencial por reuso de pasta; não demonstra packs instalados corrompidos |
| Build em pasta nova | CMake configure 0; alvo `strata` inexistente, build rc=2 | Defeito do script de rebuild |
| Drenagem sintética | Fechamento aguardou sem DONE/EOF e liberou após EOF | Espera sem prazo confirmada também na fonte |
| Desconexão do cliente | Não streaming: 301 tokens e request seguinte em 5,65 s; streaming: 25 tokens e 0,10 s | MockEngine; reproduz desperdício após desconexão |
| Afinidade | Threads criados após pin herdaram CPU0 | Semântica Linux e ordem de criação confirmadas |
| Contenção adversarial | Lançador: 90,4 ms sozinho; 22572,7 ms com quatro copiadores no mesmo CPU | Não é desempenho de inferência real |
| PCIe H2D | 13,35 GB/s por placa; 26,7 GB/s simultâneos | Cópias sintéticas, 256 MiB/GPU; não P2P nem TTFT |
| SSD dos modelos | Sequencial O_DIRECT 2,51 GB/s; blob frio mmap de 2,18 MB em 1,48 ms | Workloads específicos; sem A/B de engine em outro SSD |
| I/O aleatório de 4 KiB | 16 threads: 64805 IOPS, p50 235 µs/p99 446 µs; 64: 86988 IOPS, p50 633 µs/p99 1872 µs | Mais IOPS vieram com maior latência por leitura |
| Page cache | `mincore`: 0% dos três `experts.bin` naquele instante | Não prova estado recorrente nem causalidade de swap |
| PLE | 256 linhas amostradas com hashes iguais nos três modelos | Não prova identidade integral das tabelas |
| Ranking | 24576 pares da base, zero dos traces; saída idêntica | Defaults não reranqueiam uma base completa |
| Cache BPE | 63780 tokens: 95,39 → 34,99 ms; 70884: 101,26 → 37,06 ms | 3 repetições/braço, fixtures repetitivas, IDs idênticos; sem ganho de decode medido |

## 5. Achados consolidados

P1 indica importância alta; P2, média; P3, condicionado a demanda ou profiling.
As etapas do plano seguem dependências: um pré-requisito P2 pode preceder um
protótipo P1. **Comprovado** qualifica o comportamento observado, não o ganho
da futura correção. **Derivado** depende dos inputs e exclusões explicitados.
**Hipótese** só pode ser promovida após o experimento indicado. Esforço é relativo.

| ID | Tema | Evidência principal | Prioridade / esforço |
|---|---|---|---|
| S01 | Integridade do packer | Reprodução | P1 / médio |
| S02 | Rebuild e proveniência | Reprodução + configuração | P1 / pequeno |
| S03 | Contrato da API e tools | Reprodução | P1 / médio–grande |
| S04 | Cancelamento e drenagem | Duas reproduções distintas | P1 / médio |
| S05 | Saúde do filho e encerramento do grupo | Fonte; corrida ainda não reproduzida | P2 / médio |
| S06 | Sampling e modo efetivo | Protocolo reproduzido + configuração | P2 / pequeno–médio |
| S07 | Telemetria por GPU e por modo | Fonte + divergência histórica | P1 / médio |
| S08 | Afinidade dos auxiliares | Fonte + reprodução do mecanismo | P1 / médio |
| S09 | Caminhos mmap e complemento dual | Restrição comprovada; capacidade derivada | P1 / grande |
| S10 | Pressão de memória e cache frio | Custos medidos; causa de swap aberta | P1 / médio |
| S11 | Pesos densos por stage | Fonte + cálculo de cabeçalhos | P2 / médio |
| S12 | Prefill e staging | Hipótese; tráfego ideal derivado | P2 / médio |
| S13 | Ranking de experts | Reprodução; ganho não medido | P2 / pequeno–médio |
| S14 | CPU/PCIe e topologia | Hipótese sobre caminhos elegíveis | P2 / médio |
| S15 | MTP, prefill e paridade | Fonte + baselines; ganho não medido | P2 / médio |
| S16 | Tokenização | Protótipo medido em fixtures | P3 / pequeno |
| S17 | Kernels CUDA | Hipótese | P3 / grande |
| S18 | PLE e armazenamento | Microtestes; ganho no engine não medido | P2 / médio |
| S19 | Prefixos e conversas | Limites em fonte; otimização hipotética | P3 / grande |
| S20 | Handoff/P2P | Caminho em fonte; ganho não medido | P3 / médio–grande |

### S01 — Impedir mistura silenciosa de pesos no packer

**Impacto alto; confiança alta.** Origem: Codex STR-04.
Fontes: `backends/strata-fork/tools/iq_pack.py:291`,
`backends/strata-fork/tools/iq_pack.py:331`,
`backends/strata-fork/tools/iq_pack.py:337`.

O packer reescreve dense/layout para outra origem, mas reaproveita
`experts.bin` quando apenas o tamanho coincide. A reprodução com dois GGUFs
de mesma geometria retornou sucesso nos dois casos e manteve os experts
antigos. Tokenizers existentes também dependem de uma política de identidade.
Não foi demonstrada mistura nos packs instalados, preparados separadamente.

**Ação:** manifesto com identidade dos shards/tokenizer, geometria, versão e
modo de conversão; preparar conjunto em pasta temporária e publicar de forma
atômica ou versionada. Arquivo `READY.json` externo só protege se for validado.
**Aceitação:** origem igual idempotente; origem diferente de mesmo tamanho
rejeitada; interrupção/falta de espaço não publica conjunto parcial.
**Trade-off:** hashing integral custa I/O na preparação, não deve acontecer
por request. **Veredito:** corrigir antes de reutilizar diretórios de packs.

### S02 — Rebuild SM86 reproduzível e com proveniência

**Impacto alto no próximo rebuild; confiança alta.** Origens: STR-05, Claude C7.
Fontes: `backends/strata-fork/backend-build.sh:4`,
`backends/strata-fork/backend-build.sh:6`,
`backends/strata-fork/CMakeLists.txt:35`,
`backends/strata-fork/CMakeLists.txt:380`.

O script define arquitetura 86 sem ativar `STRATA_ENABLE_CUDA`, cujo default
é OFF. O build limpo não oferece o alvo `strata`. Separadamente, o cache
copiado em `build-fork-sm86` referencia `/home/diogo/dev/Strata`; reconstruir
nesse diretório pode operar sobre a árvore original. O binário embute apenas
0.1.30; HEAD isolado não identifica alterações locais.
Após compilar, a linha 6 do script executa `install` diretamente sobre
`build-fork-sm86/strata`, compartilhado pelos seis profiles. Corrigir apenas
a flag CUDA habilitaria essa substituição sem backup/versionamento automático.

**Ação:** diretório novo ligado ao checkout vendorizado, CUDA explícito,
dependências/toolchain identificados, commit+dirty diff+hash no manifesto e
informação de build. Publicar candidato por rename/versionamento após testes.
**Aceitação:** configure/build limpos, arquitetura SM86, fixtures e launch
por entrada separada; confirmar paths CMake/CTest e hash em uso.
**Trade-off:** todos os seis profiles compartilham o binário. Preservar versão
anterior e medir regressão antes de substituí-lo. Corrigir build não implica
melhoria de tok/s. **Veredito:** corrigir antes da próxima compilação de produção.

### S03 — Cumprir ou rejeitar explicitamente controles da API

**Impacto alto para agentes; confiança alta.** Origens: STR-03, Claude C6.
Fontes: `backends/strata-fork/serve/frontend.py:141`,
`backends/strata-fork/serve/frontend.py:300`,
`backends/strata-fork/serve/server.py:1008`.

HTTP 200 foi observado com texto para `tool_choice=required`, chamada para
`none`, duas chamadas apesar de `parallel_tool_calls=false`, texto não JSON
para `response_format` e conteúdo após `stop`. Argumento integer também foi
aceito como string inválida. Claude reproduziu stop ignorado independentemente;
outros campos, como `n` e `logprobs`, carecem de contrato explícito.

**Ação:** validar tipos/faixas e capacidades antes de gerar; implementar os
controles suportados e rejeitar explicitamente os demais. Stop incremental
precisa reter sufixos parciais entre chunks. Instrução textual não equivale a
constrained decoding; validar apenas no fim não protege tokens já emitidos.
**Aceitação:** matriz streaming/não streaming, UTF-8, stops entre chunks,
tools positivos/negativos, schema e continuação após sucesso.
**Trade-off:** buffering e compatibilidade de clientes. Não atribuir todos os
loops do modelo ao adaptador. **Veredito:** corrigir antes de qualificar agentes.

### S04 — Cancelar clientes desconectados e limitar a drenagem

**Impacto alto; confiança alta nas duas reproduções.** Origens: STR-06, Claude C1.
Fontes: `backends/strata-fork/serve/server.py:397`,
`backends/strata-fork/serve/server.py:1022`,
`backends/strata-fork/serve/server.py:1670`.

Há dois defeitos independentes. **Detecção:** a resposta não streaming só é
escrita ao final, então a desconexão não aciona o cancelamento durante a
coleta. O mock gerou 301 tokens e atrasou a próxima requisição em 5,65 s,
contra 25 tokens/0,10 s em streaming. **Finalização:** após STOP, o `finally`
drena com `lines.get()` sem prazo, mantendo o lock. Um filho vivo e silencioso
pode prender a fila; EOF explícito liberou a fixture.
Sem `max_tokens`, `prepare()` permite gerar até o contexto restante: no
profile 976k, a desconexão não streaming pode deixar a fila ocupada até EOS
ou o limite, potencialmente próximo de 1 milhão de tokens com prompt curto.
Isso é um limite de trabalho possível, não duração medida de travamento.

**Ação:** acompanhar cancelamento do cliente também no caminho não streaming;
introduzir deadline de drain e recuperação do filho gerenciado. Validar a
semântica de socket/half-close se usar sondagem de conexão. Não liberar a fila
enquanto restarem tokens da geração anterior. Um teto default de output seria
uma política adicional, não substituto para cancelamento.
**Aceitação:** desconexão na fila/prefill/decode, DONE atrasado, filho silencioso,
EOF e próxima request sem tokens cruzados. Diferenciar prefill longo legítimo
de ausência de progresso. **Veredito:** corrigir ambos mantendo a serialização.

### S05 — Distinguir saúde HTTP, engine carregado e grupo encerrado

**Impacto médio; confiança alta na fonte, limitada na corrida real.** Origens: D3/D4.
Fontes: `backends/strata-fork/serve/server.py:697`,
`backends/strata-fork/serve/server.py:1513`,
`internal/service/monitor/slots.go:64`,
`internal/service/internal/procutil/procutil.go:113`.

`/health` retorna 200 também com `loaded:false`; a próxima geração pode
reiniciar o filho. Isso não é, por si só, um contrato HTTP inválido, mas o
monitor atual não distingue servidor vivo de engine pronto. `TerminateTree`
sinaliza o grupo e varre remanescentes, porém confirma a saída do líder; a
liberação de recursos por descendentes não tem a mesma confirmação. A corrida
de VRAM durante troca de backend não foi reproduzida nesta auditoria.

**Ação:** expor pronto/descarregado/falho separadamente; preservar unload
intencional. Testar filho que sobrevive temporariamente ao líder antes de
mudar a política de confirmação do grupo, sempre com prazo e proteção de
identidade. Recolher o filho após kill no servidor.
**Aceitação:** mock de queda/unload/reload e processo filho de teste; depois
timestamps de parada e liberação real da GPU. **Veredito:** instrumentar e
corrigir estados; tratar a corrida como hipótese até reprodução.

### S06 — Sampling explícito: seed zero, top-k, seed fixa e raciocínio

**Impacto médio; confiança alta no protocolo.** Origens: STR-07, Claude C5/D5.
Fontes: `backends/strata-fork/serve/server.py:289`,
`backends/strata-fork/serve/server.py:323`,
`backends/strata-fork/serve/server.py:797`,
`backends/strata-fork/src/program/generate.cpp:4614`.

Seed zero é omitida e zero no engine seleciona relógio; top-k zero ou acima
de 64 torna-se 64. Os profiles usam seed 42/k20, portanto os casos de borda
não explicam seu comportamento normal. Defaults de seed 42 e de modo não
pensante são efetivamente enviados, mas são escolhas de configuração, não
falhas intrínsecas. Mesmo com seed fixa, residência adaptativa e diferenças
numéricas podem mudar outputs.

**Ação:** representar presença da seed separada do valor; validar/documentar
limites de top-k e tipos finitos; registrar sampling e esforço efetivos.
Manter seeds controladas nos A/B. Para uso interativo, avaliar um profile
separado sem seed fixa, sem presumir que isso corrige loops.
**Aceitação:** protocolo para seeds 0/1/42/ausente, top-k nos limites, precedência
request/config/shared-settings e avaliação real de repetição.
**Veredito:** corrigir semântica; mudança de preset exige objetivo e calibração.

### S07 — Métricas por GPU, por processo e por caminho efetivo

**Impacto alto nas decisões de capacidade; confiança alta.** Origens: STR-08, C3/D5/D10.
Fontes: `internal/service/monitor/gpu.go:71`,
`internal/service/benchmark/gpu_sampler.go:73`,
`internal/service/benchmark/client.go:279`,
`backends/strata-fork/serve/server.py:1127`,
`backends/strata-fork/serve/server.py:1734`.

O monitor lê só a primeira linha CSV de nvidia-smi. Um benchmark GPU1 registrou
875 MiB enquanto a medição por placa atingiu 23087 MiB. Além disso, o Strata
reporta uma `pcie_frac` escolhida pelo probe mesmo quando o caminho mmap a
inabilita no decode. O benchmark não preserva toda a informação de cache e
aceitação MTP; `arena_mib=0` em mmap faz o aviso de RAM perder esse caso.

**Ação:** índices físicos/UUID, memória por placa, pico total simultâneo,
separação desktop/processo; incluir `cache_n`, drafts oferecidos/aceitos,
esforço efetivo, slots e fração PCIe efetiva. mmap precisa de orçamento de
working set distinto do tamanho da arena residente.
**Aceitação:** duas linhas CSV, GPU1 isolada/dual, remapeamento, ausência de GPU;
reconciliar timestamps com telemetria externa e timings da API.
**Trade-off:** compatibilidade do JSON e significado dos campos antigos.
**Veredito:** corrigir antes de automatizar decisões de tuning.

### S08 — Corrigir herança de afinidade dos threads auxiliares

**Impacto potencial alto; confiança alta no mecanismo, média no impacto.** Origem: C2/O2.
Fontes: `backends/strata-fork/src/core/session.cpp:532`,
`backends/strata-fork/src/program/generate.cpp:3244`,
`backends/strata-fork/src/prefill/prefill.cpp:188`,
`backends/strata-fork/src/program/generate.cpp:4795`.

O thread de serviço é fixado no primeiro núcleo físico antes de criar Stager,
issuer, tarefas assíncronas, tier adaptativo e auxiliares. Linux transmite a
máscara aos novos threads: a reprodução confirmou CPU0. Os workers de experts
e de I/O PLE criados antes desse pin não são todos afetados. A perda extrema
no teste de lançador é adversarial e não deve ser apresentada como perda real.
Há também microtestes menos extremos: quatro copiadores de blobs de 2,18 MB
mediram 26,0 → 19,3 GB/s após pin; outra fixture, com lançador em yield,
mediu 51,2 → 43,2 GB/s (um stage) e 53,1 → 45,7 GB/s (dois stages).
As fixtures têm condições diferentes e não podem ser somadas às medidas H2D.
Não demonstram que banda de memcpy seja o limitador; disputa e latência do
issuer também precisam de observação no engine.

**Ação:** preservar a máscara original e definir afinidade dos auxiliares
explicitamente; manter o núcleo do serviço e o pool MoE protegidos. Comparar
máscara ampla e uso dos irmãos SMT; estes competem pelos mesmos recursos.
**Aceitação:** máscaras em `/proc/PID/task/*/status`, tempo de espera do issuer,
TTFT e decode antes/depois no mesmo profile, mais testes de cancelamento e
soak. **Veredito:** protótipo prioritário com A/B; não liberar todo o pinning
nem trocar automaticamente os 15 workers por 32.

### S09 — Complemento residente dual e limites reais do mmap

**Impacto potencial alto; bloqueio comprovado, capacidade derivada.** Origens: STR-01, C3/O1.
Fontes: `backends/strata-fork/src/program/generate.cpp:1272`,
`backends/strata-fork/src/core/expert_source.cpp:500`,
`backends/strata-fork/src/core/expert_source.cpp:827`,
`backends/strata-fork/src/core/expert_source.cpp:1028`,
`backends/strata-fork/src/prefill/prefill.cpp:1653`.

Com mmap puro, os misses desse caminho de decode vão ao pool CPU; a parcela
PCIe exige origem elegível/pinned. No prefill, os experts não residentes são
copiados para staging e daí para a GPU. `cudaHostAlloc` pode ainda cair em
fallback pageable: estado efetivo precisa ser medido. O modo residente é
recusado com layer split.

**Ação:** plano da união dos caches GPU0/GPU1; ownership por stage; lend/refill
por cache; trocas adaptativas seguras e rollback. `additional_gpu_pairs` já
existe, mas o lend atual depende de essa lista estar vazia. Simplesmente
remover a recusa não resolve essas invariantes. Os complementos de 14,526 e
26,373 GiB são apenas a base do orçamento, conforme seção 3.
Incluindo cópias de todos os slots emprestáveis registrados, seriam cerca de
15,280 GiB no IQ3/512 e 33,806 GiB no IQ4/auto, ainda sem os demais custos.
**Aceitação:** testes de união, ausência de duplicação, borrowing/refill,
paridade de experts/logits, falhas parciais; depois comparação dual mmap versus
dual residente versus GPU1 residente com guards intactos.
**Trade-off:** mais RAM não recuperável e maior pressão do sistema. Pode reduzir
I/O e habilitar outra divisão CPU/PCIe; ganho não quantificado.
**Veredito:** prototipar após S07/S10; esforço grande, não uma troca de flag.

### S10 — Atribuir pressão de memória e medir cache quente/frio

**Impacto alto; eventos/custos medidos, causa ainda hipotética.** Origens: STR-02, C4/O3.
Fontes: `backends/strata-fork/src/core/expert_source.cpp:407`,
`backends/strata-fork/src/core/expert_source.cpp:681`,
`backends/strata-fork/src/prefill/prefill.cpp:1134`.

IQ3 dual teve crescimento global de swap de aproximadamente 2,588 GiB na
primeira carga e 2,279 GiB numa geração posterior. IQ4 violou o guard com
MemAvailable acima de 42 GiB. Separadamente, cache frio e leitura de blob em
1,48 ms mostram um custo possível de misses em disco, sem estabelecer que
seja a causa da paginação. Zram usa RAM comprimida; bytes lógicos e físicos
não são equivalentes.

O IQ2 é um controle necessário: request fresco de 259641 tokens no 256k
registrou +1,072 GiB de swap; 989136 tokens no 976k, +0,234 GiB; 29825 tokens
no 32k, zero. São deltas de máximo menos primeira amostra de cada request,
não todo o ciclo de carga. Nos prefills IQ4 auto/2048 os deltas foram
+2,278/+2,272 GiB. Runs começaram com estados diferentes de swap/cache;
não há relação monotônica nem A/B causal. A hipótese a discriminar é se
streaming/refill mmap toca regiões amplas, induz reclaim e desloca páginas
anônimas para zram; o residente libera páginas de arquivo durante a cópia.

**Ação:** alinhar VmSwap/smaps, faults, `/proc/PID/io`, cgroup memory.stat/events,
PSI, pswpin/out, zram mm_stat e GPU por timestamp, tanto Python quanto filho.
Incluir pgscan/pgsteal e workingset_refault de anônimo/arquivo conforme os
contadores disponíveis no kernel/cgroup, além de pswpout por fase; distinguir
contadores globais dos atribuíveis ao processo. Repetir os controles IQ2.
Comparar primeiro e segundo prompts de mesmo tamanho com conteúdo distinto,
separando page cache de reuso de prefixo. Só depois experimentar pré-leitura
seletiva, descarte de páginas copiadas ou residência.
**Aceitação:** identificar quem paginou e em qual fase; A/B com o mesmo estado
documentado de cache. Prefetch deve respeitar orçamento e não apenas deslocar
o custo para depois de READY. THP depende do suporte do mapping/kernel.
**Veredito:** medir antes de mudar política; manter guards, swappiness e caches
globais. Não fazer swapoff ou encerrar aplicações para forçar aprovação.

### S11 — Carregar em cada GPU apenas os pesos densos necessários

**Impacto de capacidade médio; confiança alta na redundância, média no ganho.** Origem: C8/O4.
Fontes: `backends/strata-fork/src/program/generate.cpp:1941`,
`backends/strata-fork/src/core/weights.cpp:121`,
`backends/strata-fork/src/core/native_dense.cpp:60`.

Cada stage carrega o arena denso e as projeções nativas antes de restringir
sua faixa de camadas. A contabilidade por cabeçalhos/index, sem carregar pesos,
estimou a seguinte memória de camadas não executadas:

| Pack | GPU0 | GPU1 |
|---|---:|---:|
| IQ2_XS | 1,41 GiB | 1,46 GiB |
| IQ3_XXS | 1,28 GiB | 1,30 GiB |
| IQ4_XS | 1,47 GiB | 1,50 GiB |

Esses valores são **derivados**, não um delta observado de VRAM após patch.
Tornar IQ2 256k totalmente residente fora do prefill é uma hipótese plausível,
não garantia; empréstimos de slots durante o prompt continuam separados.
No contexto de 1M, KV/RoPE e demais estruturas continuam reduzindo o cache.

**Ação:** filtrar `blk.N.*` fora da faixa nos dois loaders, mantendo tensores
compartilhados necessários; preservar validações e compactação/alinhamento.
**Aceitação:** paridade primeiro com o mesmo número de slots; depois permitir
cache maior e medir VRAM/slots/hits/latência. Avaliar alocações posteriores ao
sizing, não apenas free VRAM do startup.
**Veredito:** protótipo de capacidade antes de fusões complexas de kernels.

### S12 — Calibrar prefill, ring e empréstimos de cache em conjunto

**Impacto potencial alto em prompts frescos; confiança média.** Origens: STR-09, D1/O5.
Fontes: `backends/strata-fork/src/prefill/prefill.cpp:70`,
`backends/strata-fork/src/prefill/prefill.cpp:81`,
`backends/strata-fork/src/program/generate.cpp:3387`.

Os profiles IQ3 usam chunk 512, abaixo do limiar 1024 para stream-all. Maior
chunk pode amortizar transferências por token, mas também muda workspace,
ring, empréstimos de slots e a relação com a memória pinned. O fator ideal
8192/512 não prevê vazão real. IQ4 já falhou nos guards com 8192 e 2048.

**Ação:** começar no **IQ3 GPU1 residente**, com origem pinned e o baseline de
107,959 s: 512 → 1024 → 2048; avançar só após estabilidade. No IQ3 dual mmap,
chunks ≥1024 habilitam stream-all via Stager quando o ring comporta esse modo.
Testar esse braço somente depois da atribuição de S10: todos os experts não
residentes de cada camada são percorridos por chunk, incluindo os emprestados.
Mesmo usando os empréstimos do profile 512, seriam 7540 blobs/15,280 GiB de
origem por chunk; chunks maiores exigem recalcular o lend e podem ampliar esse
conjunto. Bytes lidos do mmap não equivalem necessariamente a bytes de SSD.
Depois avaliar ring/borrowing, uma variável por vez. Não recalcular fração
pinned sem incorporá-la ao planejamento de memória e lend/refill.
**Aceitação:** prompt fresco curto/médio/quase cheio, H2D bytes, misses,
TTFT, decode posterior, RAM/VRAM e exact-copy/PT/tools.
**Veredito:** calibrar; não promover `auto` ou 8192 por uma conta de tráfego.

### S13 — Reranquear experts de fato, em arquivo por checkpoint

**Impacto médio; comportamento comprovado, ganho não medido.** Origem: STR-10.
Fontes: `backends/strata-fork/tools/make_profile.py:82`,
`backends/strata-fork/src/core/expert_cache.cpp:13`.

A base padrão já contém os 24576 pares. O utilitário acrescenta traces depois
dela e a reprodução produziu arquivo idêntico, sem nenhum par vindo do trace.
Isso corresponde ao modo de preservar a base, mas não a recalibrar a ordem.

**Ação:** usar `--no-base` para ranking completo por traces ou criar reranking
explícito; corpus do próprio checkpoint, separado da avaliação; arquivo novo,
sem sobrescrever a base compartilhada.
**Aceitação:** mesma capacidade, primeiros requests e regime após adaptação;
hits/misses, bytes H2D e wall time. Hits atuais não contabilizam todo o trabalho
PCIe como residência. **Veredito:** medir ranking próprio, sobretudo nos caches
parciais; não presumir benefício no decode IQ2 32k totalmente residente fora
do prefill. Ranking pode afetar quais experts são emprestados no prompt.

### S14 — Calibrar pool CPU e PCIe somente nos caminhos elegíveis

**Impacto potencial médio; confiança média.** Origens: STR-11, C3/O7.
Fontes: `backends/strata-fork/src/kernels/cpu/pool.cpp:59`,
`backends/strata-fork/src/kernels/cpu/native_expert.cpp:85`,
`backends/strata-fork/src/program/generate.cpp:1983`.

O pool já seleciona um lógico por núcleo físico. O probe H2D não mede todo o
custo dos kernels IQ3, disputa de RAM ou afinidade. A topologia de dois CCDs
justifica medir, não presumir que restringir a um deles seja superior.

**Ação:** após S08, comparar 8/12/15 workers e máscaras controladas; depois
`strata_tune.pcie_frac` onde a origem pinned habilitar esse caminho. Em mmap
puro, alterar a fração não ativa o ramo inabilitado. Examinar THP/TLB somente
se profiling mostrar custo e suporte do mapping específico for comprovado.
**Aceitação:** wall time, ms_pool, migrations, CPU/PCIe experts por janela,
PSI/zram e impacto no desktop. **Veredito:** medir; manter 15 como referência,
sem SMT duplicado ou scheduler novo por intuição.

### S15 — MTP: medir economia útil, paridade e prefill no último stage

**Impacto potencial médio/alto; confiança média.** Origens: STR-13, D2/O9.
Fontes: `backends/strata-fork/src/program/generate.cpp:3856`,
`backends/strata-fork/src/core/mtp.cpp:679`,
`backends/strata-fork/src/spec/draft_policy.cpp:55`,
`backends/strata-fork/src/kernels/cpu/native_expert.cpp:92`.

O target verifica drafts; isso não torna um head compartilhado ideal para
outro checkpoint. A aceitação histórica IQ3 de código (795/937) não prevê
PT/tools. IQ2 256k não melhorou ao reduzir spec-min-p de 0,7 para 0,5.
O código desativa `draft_kv` em lote com multi-GPU e usa prefill por grupos;
essa alternativa já agrega uploads e sincronização, não é o antigo caminho
com sincronização host por token.

**Ação:** A/B de janela/min-p com tokens úteis e custo total; testar
`STRATA_IQ_MT_MIN=1` para paridade dos kernels CPU. Separadamente, prototipar
prefill em lote no último stage, onde vive o drafter, com controle de device,
memória e checkpoints. O serve atual exige spec ≥2/MTP: spec=1 não fornece
um braço simples “MTP desligado”. Suffix drafting e adaptação já existem.
**Aceitação:** exact-copy/tools primeiro, aceitação e latência por workload,
path=token/batched e tempo real de prefill. **Veredito:** medir antes de ampliar
especulação; não usar taxa de aceitação isolada como ganho.

### S16 — Cache BPE limitado como melhoria pequena e verificável

**Impacto baixo/médio; confiança alta nas fixtures.** Origens: STR-14, O8.
Fontes: `backends/strata-fork/tools/strata_tokenizer.py:140`,
`backends/strata-fork/tools/strata_tokenizer.py:158`,
`backends/strata-fork/serve/server.py:919`.

O protótipo reduziu 95,39 para 34,99 ms e 101,26 para 37,06 ms, com todos os
IDs iguais. O cache usou limite de 16384 entradas e bypass de fragmentos
acima de 256 caracteres. As fixtures repetitivas favorecem o cache. O Claude
mediu cerca de 535 mil tokens/s em outro corpus; os números não são A/B entre
implementações e não devem ser combinados.

**Ação:** cache por tokenizer com resultados imutáveis e memória limitada.
Tokenização incremental de prefixo é trabalho distinto: respeitar fronteiras
BPE, template e invalidação; não concatenar tokens de fragmentos arbitrários.
**Aceitação:** Unicode, tokens especiais, entropia alta, concorrência, tokenizer
alterado e oracle de equivalência. **Veredito:** melhoria opcional após P1;
economia de milissegundos de CPU, sem promessa de decode mais rápido.

### S17 — Localizar custo antes de fundir kernels CUDA

**Impacto potencial médio; confiança baixa/média no ganho.** Origem: STR-12.
Fontes: `backends/strata-fork/src/core/verify.cpp:688`,
`backends/strata-fork/src/kernels/cuda/iq_kernels.cu:711`,
`backends/strata-fork/src/kernels/cuda/iq_kernels.cu:736`.

Gate/up, SwiGLU, Q8_1 e down têm etapas distintas. Fundir SwiGLU+quantização
ou ajustar blocos para SM86 pode reduzir intermediários. CUDA Graphs já
amortizam launches; contar kernels não mede a oportunidade. QSA/MMQ/cuBLAS
já usam recursos acelerados adequados, portanto “ativar Tensor Cores” não é
uma proposta nova.

**Ação:** timers do engine e profiler para localizar custo; só então protótipo.
**Aceitação:** oracle numérico independente, T=1..4, PT/código/tools, memória,
registradores/ocupação e latência completa. Microbench sem pool omite experts.
**Trade-off:** arredondamento e ocupação podem piorar. **Veredito:** profiling
antes de implementação; ganho não quantificado.

### S18 — PLE: I/O, localização e compartilhamento com identidade comprovada

**Impacto potencial médio; I/O medido, efeito no engine aberto.** Origens: STR-16, D6/O6.
Fontes: `backends/strata-fork/src/platform/direct_file.cpp:263`,
`backends/strata-fork/src/platform/direct_file.cpp:313`,
`backends/strata-fork/src/ngram/ple_reader.cpp:34`,
`backends/strata-fork/src/program/generate.cpp:1395`.

PLE usa cache limitado e O_DIRECT com 16 threads no Linux. A passagem de 16
para 64 threads elevou IOPS e também p50/p99: throughput de disco não implica
menor TTFT. Amostras de 256 linhas coincidiram entre modelos, sem provar
identidade integral. Leituras repetidas podem ser atendidas pelo cache de
linhas do engine; não assumir que toda consulta lógica acessa o SSD.

**Ação:** comparar `STRATA_IO_THREADS` 8/16/32 e só então 64, com ms_ple, hits
e latência do engine. Avaliar outro volume se o I/O dominar, após confirmar
capacidade e banda. Compartilhar/mover a tabela exige identidade completa e
manifesto; ao trocar `--ple-gguf`, preservar resolução dos shards densos, com
`--native-dense-gguf` explícito quando necessário.
**Aceitação:** mesmo prompt/cache/config, TTFT e I/O por fase, integridade dos
shards e memória. **Veredito:** medir; io_uring, carregar a tabela inteira e
migração de armazenamento não são ações automáticas.

### S19 — Preservar prefix checkpoints; parking dual exige novo orçamento

**Impacto condicionado a múltiplas conversas; confiança média.** Origens: STR-15, D8.
Fontes: `backends/strata-fork/src/program/generate.cpp:1194`,
`backends/strata-fork/include/strata/core/conversation_cache.hpp:23`,
`backends/strata-fork/src/core/conversation_memory.cpp:36`.

Checkpoints de prefixo já funcionam com split. Parking de sessões completas
tem orçamento separado, default zero, e é recusado com split. Ativá-lo não
conserta o armazenamento de experts; acrescenta RAM. A checagem de memória de
conversas usa MemAvailable do host, enquanto o planner de experts considera
cgroups, diferença relevante num serviço com limite próprio.

**Ação:** só implementar parking dual se alternância A→B→A dominar o workload;
snapshot de todos os stages, MTP e KV com orçamento conjunto/cgroup.
**Aceitação:** isolamento, restauração exata, eviction, cache inválido e pico
transitório. **Veredito:** adiar até demanda demonstrada; preservar o reuso
existente e não descrever todo prompt alternado como necessariamente integral.

### S20 — Medir handoff antes de introduzir P2P direto

**Impacto potencial incerto; confiança média no caminho, baixa no ganho.** Origens: STR-16, D9.
Fontes: `backends/strata-fork/src/program/generate.cpp:3637`,
`backends/strata-fork/src/core/verify.cpp:1053`.

Layer split é sequencial e usa memória host mapeada nas fronteiras, com
sincronização de ownership. P2P está disponível no driver, mas os probes H2D
não medem esse transporte. Não há evidência de que a fronteira entre as duas
metades domine o tempo de geração.

**Ação:** medir bytes/tempo de passagem por janela/chunk; prototipar P2P somente
se o custo for material. Flags NCCL/llama.cpp de outros backends não ativam
esse caminho no Strata.
**Aceitação:** paridade, ordenação, falhas do device e latência completa.
**Veredito:** prioridade inferior a cache/residência e afinidade; nenhum ganho
percentual assumido e nenhuma expectativa de dobrar vazão por usar duas GPUs.

## 6. Situação dos profiles e orientação de uso

Todos os seis passaram na validação de schema arquivada, usam o checkout em
`backends`, contexto coerente com o sufixo, KV int8, MTP e porta gerenciada.
A validação não comprova estabilidade nem qualidade. As linhas abaixo são
constatações consolidadas, não alterações de configuração.

| Profile | Situação / severidade | Orientação e critério pendente |
|---|---|---|
| IQ2_XS dual 32k | Geração/contexto validados; tools: aviso | Referência de contexto curto. Repetição após sucesso impede qualificação de agente. |
| IQ2_XS dual 256k | Geração/contexto validados; tools: aviso | Referência de contexto nativo; revalidar continuação curta e auto tools após S03/S06. |
| IQ2_XS dual 976k | Contexto estendido: aviso | Usar quando necessário; YaRN e maior KV reduzem expert cache. Recuperação 3/3 não prova qualidade geral nem tools em 1M preenchido. |
| IQ3_XXS GPU1 residente 32k | Geração/contexto estáveis; tools/JSON: aviso | Referência uncensored atualmente estável; não qualificado para agente. Preservar enquanto se testa dual. |
| IQ3_XXS dual mmap 32k | Estabilidade: erro operacional | Experimental; resolver atribuição da pressão e completar matriz sem violar guards. |
| IQ4_XS dual mmap 32k | Estabilidade: erro operacional | Experimental; código curto funciona, contexto longo falhou. Reduzir chunk não resolveu o guard. |

O IQ3 residente atingiu 23087 MiB na GPU1. Teve crescimento de swap zero nos
requests de contexto/benchmark, mas cerca de 0,026 GiB durante carga; não
descrever todo o ciclo como zero. O benchmark registrou mínimo de 8,61 GiB
de RAM disponível. A reserva de VRAM é dimensionada antes de algumas
alocações posteriores, portanto validar picos durante o request.

A divergência `qwen3-8` no ID versus `qwen3.8` em config/log do IQ2 32k é
cosmética e dificulta correlação; não impede launch. Eventual padronização
precisa atualizar todas as referências, sem renomear por conveniência nesta
consolidação. Ranking de estabilidade: IQ2 32k/256k e IQ3 residente são as
referências; IQ2 976k tem escopo mais limitado; IQ3/IQ4 dual são experimentais.
Isso não é ranking de qualidade de modelos.

## 7. Plano único de execução e critérios de promoção

| Etapa | Trabalho | Gate para avançar |
|---|---|---|
| 1 — Base confiável | S01 pack transacional, S02 build/proveniência, S03 API, S04 cancelamento | Reproduções passam; fila/protocolo sem contaminação; rollback selecionável |
| 2 — Medição e controle | S07 métricas; S10 atribuição da pressão; S05 lifecycle; S06 parâmetros efetivos | Memória/tempos reconciliados; limitações explícitas; dono da pressão identificado ou lacuna documentada |
| 3 — Protótipos isolados | S08 afinidade, S11 densos por stage, S09 complemento dual | Paridade/ownership primeiro; depois memória e vazão com mesmo build/checkpoint |
| 4 — Calibração | S12 prefill/staging, S13 ranking, S14 CPU/PCIe, S15 MTP, S18 PLE | A/B repetível sem regressão funcional nem violação dos guards |
| 5 — Otimizações condicionais | S16 BPE, S17 kernels, S19 parking, S20 P2P | Custo relevante demonstrado no workload e ganho completo medido |

S08/S11 podem ser prototipados antes do complemento dual. S09 depende de
telemetria e de invariantes de memória; não deve ser promovido apenas porque
a conta do complemento parece caber. Alterações em grupos diferentes não
devem ser misturadas no mesmo braço experimental.

Para qualquer futura mudança de inferência:

1. Preservar o binário/config/profile de referência; usar candidato separado.
   Iniciar pelo `model-loader instance start` e inferir pelo proxy com o ID
   correto. Os seis profiles atuais compartilham código e executável.
2. Manter 270 W e até 23552 MiB por GPU. Preservar guards existentes: pelo
   menos 2 GiB de MemAvailable e no máximo 2 GiB de crescimento global de
   swap, com três amostras violadas consecutivas. Diagnosticar antes de mudar
   a interpretação desses guards; não relaxá-los para aprovar candidato.
3. Separar cold start, page cache quente/frio e prefixo novo/reutilizado;
   registrar tokens reais, sampling, esforço, build, cache, MTP e potência.
4. Comparar ao menos três repetições aquecidas por braço, ordem alternada,
   mesmo checkpoint e uma variável por vez. Reportar mediana e dispersão;
   três pontos não sustentam p99. Medir TTFT, tempo total e tokens úteis.
5. Medir memória por placa/processo, I/O, PSI/zram, clocks, temperatura e
   throttling durante carga, prefill e decode, não só no startup.
6. Testar PT, exact-copy, recuperação longa, JSON/schema, tools positivos e
   controles, continuação após resultado de sucesso e cancelamento/restart.
7. Regressão IQ2 32k/256k obrigatória quando o backend compartilhado mudar;
   incluir 976k quase cheio quando tocar KV, RoPE, prefill, cache ou memória
   desse caminho. Os fills 5/25/50/90% devem separar prompts frescos e em cache.
   Registrar crescimento de swap como métrica contínua, inclusive abaixo de
   2 GiB; passar no guard não implica ausência de regressão nessa métrica.

Rejeitar promoção por corrupção, tokens cruzados, Xid/OOM, violação dos guards
ou regressão material além da variabilidade. Qualificação de agente exige
avaliação própria; quatro casos de integridade ou startup bem-sucedido não
bastam. Nunca somar ganhos de CPU, capacidade, prefill e decode como economia
única nem projetar ganhos de microtestes diretamente em tokens/s.

## 8. Rastreabilidade dos achados originais

Todos os 16 achados Codex e os C1–C8, D1–D12 e O1–O9 do Claude foram
considerados. A tabela registra agrupamentos e reclassificações; não existem
45 defeitos independentes para corrigir.

| Origem Codex | Destino consolidado |
|---|---|
| STR-01 | S09 |
| STR-02 | S10 |
| STR-03 | S03 |
| STR-04 | S01 |
| STR-05 | S02 |
| STR-06 | S04 |
| STR-07 | S06 |
| STR-08 | S07 |
| STR-09 | S12 |
| STR-10 | S13 |
| STR-11 | S14 |
| STR-12 | S17 |
| STR-13 | S15 |
| STR-14 | S16 |
| STR-15 | S19 |
| STR-16 | S18 e S20 |

| Origem Claude | Destino / decisão |
|---|---|
| C1 | S04; detecção de desconexão separada do drain |
| C2 / O2 | S08; mecanismo aceito, magnitude real pendente |
| C3 | S09/S07; formulação sobre DMA corrigida |
| C4 / O3 | S10; snapshot aceito, recorrência e causa não comprovadas |
| C5 | S06; seed fixa reclassificada como escolha de configuração |
| C6 | S03 |
| C7 | S02 |
| C8 / O4 | S11; bytes derivados, delta real ainda não medido |
| D1 / O5 | S12; fator ideal não é ganho de vazão |
| D2 / O9 | S15; fallback já contém otimizações de upload/sincronização |
| D3 / D4 | S05; não confundir sinais ao grupo com confirmação de toda a saída |
| D5 | S06/S07; modo não pensante intencional deve ser visível |
| D6 / O6 | S18; amostra PLE não autoriza deduplicação integral |
| D7 | S11 e seções 6/7; considerar alocações pós-sizing |
| D8 | S19 |
| D9 | S20; percentual de ganho retirado |
| D10 | S07/S10 |
| D11 | Seções 3/6; YaRN e custo de contexto |
| D12 | Seção 6; cosmético |
| O1 | S09; inclui lend/refill e adaptação, não só pares adicionais |
| O7 | S10/S14; THP adiado até prova de custo/suporte |
| O8 | S16; cache BPE medido e prefixo incremental são propostas distintas |

Também permanecem rejeitadas como propostas automáticas: compute FP8/FP4
nativo em SM86; 32 workers apenas pela contagem lógica; split assimétrico;
flags P2P emprestadas de outros backends; reescrever detokenização que já é
incremental; remover sincronizações sem prova de ownership; afirmar que GPU1
ignora sampling; concluir que os packs atuais estão corrompidos.

## 9. Evidências, verificação e limites

### Arquivos primários

- [Auditoria Codex original](IDEATION_PERFORMANCE.original-2026-10-01.md) e
  [auditoria independente Claude](docs/reports/strata-independent-claude-opus-5-5-2026-10-01.md).
- [Ambiente](docs/reports/strata-audit-2026-10-01/environment.json),
  [P2P/SM86/zram](docs/reports/strata-audit-2026-10-01/environment-extra.json),
  [profiles/configs](docs/reports/strata-audit-2026-10-01/profiles.json) e
  [manifesto de fontes](docs/reports/strata-audit-2026-10-01/source-manifest.json).
- [Probes de contrato e packer](docs/reports/strata-audit-2026-10-01/contract-probes.json),
  [script das reproduções](docs/reports/strata-audit-2026-10-01/probe_contracts.py),
  [build](docs/reports/strata-audit-2026-10-01/build-reproduction.json),
  [ranking](docs/reports/strata-audit-2026-10-01/ranking-probe.json) e
  [BPE](docs/reports/strata-audit-2026-10-01/tokenizer-probe.json).
- [Desconexão](docs/reports/strata-claude-evidence-2026-10-01/repro_nonstream_disconnect.log),
  [afinidade](docs/reports/strata-claude-evidence-2026-10-01/affinity_inherit.log),
  [contenção adversarial](docs/reports/strata-claude-evidence-2026-10-01/launcher_latency.log),
  [PCIe](docs/reports/strata-claude-evidence-2026-10-01/pcie_probe.log),
  [I/O aleatório](docs/reports/strata-claude-evidence-2026-10-01/ple_randread.log),
  [leitura fria](docs/reports/strata-claude-evidence-2026-10-01/coldread.log),
  [page cache](docs/reports/strata-claude-evidence-2026-10-01/mincore.log),
  [amostragem PLE](docs/reports/strata-claude-evidence-2026-10-01/ple_same.log) e
  [cálculo dos densos](docs/reports/strata-claude-evidence-2026-10-01/dense_per_stage.py),
  com [saídas preservadas do cálculo e do inventário de discos](docs/reports/strata-consolidation-2026-10-01/claude-selected-results.json).
- [Manifesto dos artefatos Claude](docs/reports/strata-claude-evidence-2026-10-01/manifest.json),
  [metadados da delegação](docs/reports/strata-audit-2026-10-01/delegation.json) e
  [notas de checagem posterior](docs/reports/strata-claude-evidence-2026-10-01/README.md).
- Calibrações históricas: [IQ2 256k](docs/reports/strata-iq2-xs-256k-calibration-2026-10-01.md),
  [IQ2 976k](docs/reports/strata-iq2-xs-context-2026-10-01.md),
  [IQ3](docs/reports/strata-orca-iq3-xxs-calibration-2026-10-01.md),
  [IQ4](docs/reports/strata-iq4-xs-uncensored-2026-10-01.md) e
  [índice de evidências históricas](docs/reports/strata-audit-2026-10-01/historical-evidence-index.json).

### Testes existentes aproveitados

| Auditoria | Validação registrada | Limite |
|---|---|---|
| Codex | Suíte de 130 casos: 127 passaram e 3 foram pulados; esses 3 detokenizadores passaram depois em execução separada, totalizando 130 casos exercitados; 4 testes C++; Go stratahelp/backendschema; 6 profiles válidos | Fixtures e testes sem modelo real |
| Claude | Suites serve/tools, 3 detokenizadores pulados naquela execução; 5 binários C++ sem modelo; Go stratahelp/backendschema/processmgr/httpproxy/benchmark/domain e vet em 3 pacotes | Há sobreposição com a suíte Codex; não somar contagens como cobertura única |
| Consolidação | Releitura dos caminhos divergentes, confronto dos logs/cálculos e verificação de links, citações, hashes e cobertura dos IDs | Não repetiu inferência nem promoveu qualquer otimização |

Logs: [Python Codex](docs/reports/strata-audit-2026-10-01/python-tests-with-deps.log),
[detokenização](docs/reports/strata-audit-2026-10-01/detokenizer-tests.log),
[C++ Codex](docs/reports/strata-audit-2026-10-01/cpp-tests.json),
[Go Codex](docs/reports/strata-audit-2026-10-01/go-tests-all.log),
[Go Claude](docs/reports/strata-claude-evidence-2026-10-01/go-validation.log).
Falhas iniciais de descoberta/importação e cwd foram preservadas; não são
tratadas como regressões do runtime após correção do comando de teste.

Cobertura forte: API/lifecycle, preparação/build, experts/memória, afinidade,
integração e configuração. Cobertura parcial: numérica completa de kernels,
MTP em workloads amplos, qualidade de agentes e estabilidade prolongada.
Visão, HIP/Windows e outros modelos não foram profundamente auditados.

As estimativas de VRAM do Claude são aproximações; prevalecem os slots de
logs identificados quando disponíveis. Nenhuma auditoria mediu ganho de
inferência após patch. A redução de BPE é local às fixtures; o cancelamento
usa mock; as medidas PCIe/SSD são microtestes. Não há benchmark P2P novo nem
prova de identidade integral das tabelas PLE.

A [verificação desta consolidação](docs/reports/strata-consolidation-2026-10-01/verification.json)
registra hashes, originais preservados e cobertura dos 20 achados. Os arquivos
`verification.json` e `delegation.json` da auditoria anterior descrevem aquele
instante: o SHA antigo de `IDEATION_PERFORMANCE.md` corresponde agora à cópia
`IDEATION_PERFORMANCE.original-2026-10-01.md`, não a este documento consolidado.

## 10. Revisão independente do consolidado e tratamento das ressalvas

A pedido do usuário, este consolidado recebeu uma revisão em nova sessão de
Claude Code, `claude-opus-5-5`, esforço `xhigh`, headless. O
[parecer independente](docs/reports/strata-consolidated-independent-review-2026-10-01.md)
foi **aprovado com ressalvas, sem bloqueadores**. O revisor confirmou os
achados P1 e a correção das 67 citações e 37 links da versão submetida.

O parecer refere-se ao SHA-256
`0241f527530e3ab96aa6398dce732834ec15aafb346788fb0bdafa60ba7c2221`, preservado na
[cópia submetida](docs/reports/strata-consolidation-2026-10-01/independent-review-evidence/report-submitted.md).
Após receber o parecer, o auditor principal verificou e incorporou as
ressalvas abaixo. O texto atual inclui essas emendas; elas não receberam uma
segunda revisão independente. O parecer original não foi alterado.

| Item da revisão | Tratamento no documento final |
|---|---|
| R1 — residência durante o prompt | Seção 3 e S09 distinguem slots de decode e empréstimos por request; incluem contas condicionais de cópias adicionais. IQ2 misto não usa blob médio como tamanho exato. S11/S13 qualificam “totalmente residente”. |
| R2 — alvo do A/B de prefill | S12 começa explicitamente no IQ3 GPU1 residente; dual mmap depende de S10 e de recálculo do lend para cada chunk. |
| R3 — controle IQ2 e reclaim | Seção 1/S10 incorporam deltas de swap IQ2 e hipótese de acoplamento; plano registra deltas contínuos e contadores de reclaim disponíveis, sem afirmar causalidade. |
| R4 — prioridade versus ordem | Prioridade representa importância; etapas representam dependências. |
| R5 — afinidade | S08 inclui microtestes menos extremos e limita sua comparação com H2D. |
| R6 — instalação do binário | S02 explicita o `install` sobre o executável compartilhado e a ausência de backup/versionamento no script. |
| R7 — limite de output no cancelamento | S04 registra o contexto restante como limite possível sem max_tokens, sem converter isso em duração medida. |
| Contagem Python | Seção 9 explicita 127 sucessos + 3 inicialmente pulados e depois aprovados separadamente. |

Evidências adicionais conferidas: [empréstimos nos logs](docs/reports/strata-consolidation-2026-10-01/prefill-borrowing-evidence.json),
[controles de swap recalculados](docs/reports/strata-consolidation-2026-10-01/swap-controls.json),
[artefatos do revisor](docs/reports/strata-consolidation-2026-10-01/independent-review-evidence/manifest.json)
e [execução da revisão](docs/reports/strata-consolidation-2026-10-01/review-delegation.json).
