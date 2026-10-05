# Qwen3.8-Flash-Next em duas RTX 3090: estratégia local, SSD streaming e backend customizado

**Data da pesquisa:** 8 de setembro de 2026.  
**Máquina:** Ryzen 9 9950X3D, 64 GB de RAM instalada, duas RTX 3090 de 24 GiB, sem NVLink ativo.  
**Objetivo:** maximizar desempenho útil do Flash-Next nesta máquina, especialmente em sessões de programação com ferramentas e contexto longo. Não substituir o modelo solicitado pelo Qwen3.8-27B.

## 1. Decisão executiva

**A melhor base demonstrada nesta máquina continua sendo GGUF com PLE sob demanda no SSD e experts distribuídos entre RAM e VRAM. O melhor próximo comparador é EXL3/TabbyAPI; o investimento de engenharia mais promissor é evoluir o backend C++ existente, não reescrever um motor inteiro nem transplantar o stack Syv indiscriminadamente.**

Ordem recomendada:

1. **Preservar os dois finalistas existentes.** Cache96 é a referência de decode curto; ik FIFO é a referência de ingestão e contexto longo. Ambos já têm evidência local, inclusive 244.116 tokens efetivamente processados.
2. **Comparar EXL3 4.05 bpw**, inicialmente com PLE em disco, offload CPU e uma sequência, sem MTP. Há suporte real e relatos relevantes, mas também regressões e diferenças grandes entre CPUs. Não é vencedor comprovado neste host.
3. **Medir a PLE no Kingston KC3000 já instalado**, separando ganho de latência/I/O frio de ganho em contexto aquecido. Não comprar SSD nem mover os modelos de produção antes desse A/B.
4. **Evoluir cache de experts + pequenos lotes de verificação MTP**, corrigindo primeiro IDs duplicados nos slots fictícios, sincronização host/device e estabilidade de buffers. Simplesmente trocar `n_tokens == 1` por `<= 4` não é uma solução geral correta para CUDA.
5. **Otimizar ingestão e reaproveitamento de prefixo**, porque 14 minutos de ingestão em 244k podem dominar completamente uma economia de milissegundos por token gerado.
6. **Tratar W4A16/vLLM como uma segunda linha de pesquisa**, não como instalação pronta: INT4 é adequado à RTX 3090, mas os pesos precisam ser particionados sem duplicação no host e a PLE precisa permanecer fora da RAM residente. As receitas Ampere mais bem documentadas consultadas usam quatro 3090, não duas.

Não há evidência para prometer 100–150 tok/s do Flash-Next nesta máquina. Os números do 27B/Syv e de RTX PRO 6000 Blackwell não estabelecem essa possibilidade. Um backend customizado pode melhorar o resultado, mas seu teto depende da fração de tempo em CPU, cópias, QSA, indexer, PLE, sincronizações e geração especulativa.

### Respostas diretas

| Pergunta | Resposta |
|---|---|
| W4A16 é uma opção? | **Sim, como formato e caminho de kernel.** Não significa que qualquer checkpoint W4A16 caiba ou seja rápido em duas 3090 + 64 GB. |
| SSD streaming vale a pena? | **PLE: sim, já é parte da solução local. Experts: apenas como hierarquia com cache e objetivo bem definido; streaming sem residência suficiente limita a velocidade.** |
| Podemos reutilizar Syv? | Princípios de quantização seletiva, prefix cache, verificação especulativa e profiling, sim. Drafter do 27B, guards de atenção e flags Python, não diretamente. |
| Criar backend próprio? | **Sim, como fork pequeno e reproduzível do C++ existente.** Engine nova do zero não é a primeira escolha. |
| Qual alternativa pronta testar primeiro? | EXL3 4.05 bpw + TabbyAPI, confrontado com os finalistas em qualidade, TTFT e contexto ocupado. |
| Upgrade mais relevante, se desejado? | Mais RAM abre caminhos hoje bloqueados por bancos host/pinned. Não garante aumento proporcional de decode. A recomendação principal usa o hardware atual. |

## 2. Método, evidência e limites

Classificação usada ao longo do documento:

- **[LOCAL]**: fontes, profiles e artefatos de execução presentes nesta máquina. Métricas históricas não foram reexecutadas nesta pesquisa.
- **[PRIMÁRIA]**: configuração oficial, código, model card ou discussão dos próprios autores.
- **[RELATO]**: benchmark publicado por terceiros, sem reprodução neste host.
- **[INFERENCE]**: cálculo, projeto ou hipótese; não resultado medido.

A pesquisa combinou Perplexity, busca Exa, leitura de GitHub/Hugging Face, fóruns do Hugging Face e Reddit via OpenCLI, além da inspeção dos backends e registros locais. `agent-reach doctor` não estava disponível no PATH; isso não impediu a consulta das discussões via ferramentas acessíveis. Resultados de busca foram usados como descoberta, não como prova suficiente de suporte ou desempenho.

**Não foram feitos novos benchmarks de inferência, downloads de checkpoints grandes, alterações de profiles, swaps de backend, mudanças de clocks/potência, limpeza de cache global nem mudanças no sistema.** O entregável desta rodada é investigação e proposta de experimentos.

Foram corrigidas, durante a consolidação, imprecisões de sínteses intermediárias: número de experts ativos, largura das linhas PLE, contagem de bytes de checkpoints, distinção entre `pread` e `O_DIRECT`, orçamento de uploads por camada e atribuição de receitas de quatro GPUs a duas GPUs. Não há confirmação primária nesta pesquisa de uma receita W4A16 em duas 3090 superior aos finalistas locais.

## 3. Hardware real e suas consequências

### 3.1. Inventário observado

| Recurso | Evidência local | Consequência |
|---|---|---|
| CPU | Ryzen 9 9950X3D, 16 núcleos/32 threads, AVX-512 | Forte candidato a computar experts frios na CPU; não extrapolar resultados de AVX2 antigo. |
| L3 | Dois domínios de 96 MiB e 32 MiB | Afinidade pode alterar locality; o banco completo de experts continua muito maior que o L3. |
| RAM | 64 GB instalada; aproximadamente 60,48 GiB utilizáveis pelo Linux | Bancos pinned acima desse total são impossíveis, mesmo antes de OS/runtime. |
| GPUs | 2 × RTX 3090, 24.576 MiB por placa | Orçamento é por placa; 48 GiB agregados não formam memória unificada. |
| Comunicação | Topologia PHB, sem NVLink ativo; links x8 observados | TP/EP envolve comunicação entre GPUs pelo host bridge; layer split merece ser a referência. |
| SSD dos modelos | PCH-RVR 1 TB, ext4, controladora SM2263 atrás do chipset, `/home/diogo/models` | Alvo natural para A/B de leitura PLE contra o SSD do sistema. |
| SSD do sistema | Kingston KC3000 1 TB, ligado diretamente à CPU | Melhor candidato de I/O, mas não foi medido com o padrão real de PLE nesta rodada. |
| SSD de desenvolvimento | XPG S11 Pro 512 GB, `/home/diogo/dev` | Estava com pouco espaço; não usar por conveniência para novos checkpoints. |

No inventário, havia aproximadamente **139,7 GiB livres no volume de modelos**, **334,5 GiB no volume do sistema** e **38,7 GiB no de desenvolvimento**. São fotografias de disponibilidade, não reservas. Pesos EXL3, GGUF e W4A16 podem consumir essa margem rapidamente; downloads e conversões precisam de orçamento separado de temporários.

A leitura de `MemAvailable` foi feita com processos de modelo presentes. Não é baseline ocioso. ZRAM também estava em uso: sua capacidade lógica não deve ser somada à RAM física como se fosse memória rápida adicional para experts. Pesos quantizados podem comprimir pouco e paging causa caudas longas de latência.

### 3.2. Ampere: formato de armazenamento não é instrução nativa

A RTX 3090 não possui tensor cores FP8 ou FP4 nativos. Isso não proíbe armazenar pesos/KV nesses formatos: o runtime pode converter, usar kernels especializados ou executar matemática em FP16/BF16.

- **W4A16 INT4:** pesos compactados, ativações de 16 bits, caminho Marlin ou equivalente. É uma escolha tecnicamente coerente para SM86.
- **NVFP4:** não interpretar o nome como aceleração FP4 nativa nesta GPU. Repack/dequantização e suporte exato do kernel precisam ser demonstrados.
- **KV FP8:** é uma otimização de memória/tráfego distinta de GEMM FP8. Exige kernel e escalas compatíveis com QSA; a flag aceita não prova que o caminho executou corretamente.
- **W4A8 INT8 do Syv:** muda também a precisão de ativações; merece medição de qualidade, especialmente em GDN, router e indexer.

Não foram aferidos nesta pesquisa banda DDR5 sustentada, IOPS, leitura sequencial dos SSDs ou banda P2P. Valores de catálogo não são benchmarks desta máquina.

## 4. Arquitetura: por que Flash-Next não é um 27B maior

A configuração oficial identifica `Qwen4ExpForConditionalGeneration`, `model_type=qwen4_exp` [S1–S2].

| Componente | Configuração oficial | Impacto de engenharia |
|---|---|---|
| Modelo principal | Aproximadamente 125B, 6B ativos/token | Ativação esparsa reduz computação, não elimina pesos residentes. |
| Extras | Aproximadamente 51B de PLE + 4B MTP | Total lógico próximo de 180B; não contar apenas os 125B. |
| Camadas | 48 | Não compatível com seleção de camadas do drafter Syv de 64 camadas. |
| Fluxo residual | Hidden 2560, quatro ramos de gated residual | Não confundir dimensão do hidden com o residual alargado. |
| MoE | 512 experts roteados, top-10, shared expert | Cache temporal é plausível; lista estática de experts populares não está demonstrada. |
| Dimensão intermediária | 640 | Crucial para layout, quantização e divisibilidade em TP. |
| GDN | 36 camadas, estado configurado FP32 | Estado recorrente, checkpoints e rollback precisam permanecer corretos. |
| QSA | 12 camadas; 24 Q heads, 2 KV heads, head dim 256 | Diferente do Qwen3.8-27B e dos guards INT8 Syv. |
| Indexer | Budget 2048, compress ratio 4, head dim 128, 1 KV head | Selecionar 2048 tokens não significa armazenar apenas 2048 posições no KV. |
| PLE | Layer 2; n-gram até 3; 8 heads/ngram; dimensão 2560 | 16 buscas determinísticas por token; excelente candidato a I/O antecipado. |
| Contexto nativo | 262144 tokens | 256k configurados e 256k ocupados são evidências diferentes. |

A tabela PLE possui aproximadamente **51,2 bilhões de elementos**. Sua precisão muda radicalmente o tamanho:

- BF16: aproximadamente **95,37 GiB**.
- FP8: aproximadamente **47,68 GiB**.
- IQ4_NL no card ji-farthing: **26,84 GiB**.

O backbone MoE contém `48 × 512 × 3 × 2560 × 640 = 120.795.955.200` elementos de experts roteados, antes de escalas. O número automático de parâmetros do Hugging Face pode contar elementos de armazenamento `int32`/`uint8` compactados em vez dos pesos lógicos, e pode omitir PLE em sidecar. Não usar essa contagem para concluir que uma quantização mudou o modelo de 180B para 68B.

### 4.1. Contexto e estados: orçamento mínimo aproximado

**[INFERENCE]** Para as 12 camadas QSA, mantendo K e V completos:

`12 × 262144 × 2 KV heads × 256 × 2 (K,V) × 2 bytes = 6 GiB` em FP16/BF16.

Isso não inclui indexer, GDN, checkpoints, buffers de execução, draft nem reserva do allocator. Q8_0 reduz a carga de dados aproximadamente à metade, com overhead de blocos; a implementação concreta pode adicionar outros estados. GDN não cresce como KV convencional por token, mas **checkpoints de estado recorrente podem crescer com a política de retenção**.

QSA economiza cálculo de atenção ao selecionar posições. O indexer e a retenção do KV ainda podem crescer com contexto. Logo, “só atende 2048 tokens” não justifica uma previsão de decode constante até 256k.

## 5. O que já funciona localmente

Os relatórios anteriores estão em [resultados de tuning](qwen-flash-tuning-results.md) e [auditoria de 4 de setembro](qwen3.8-flash-next-dual-rtx3090-2026-09-04.md). O primeiro contém evidência posterior que supera algumas lacunas do segundo. Não tratar propostas antigas como trabalho ainda inédito.

### 5.1. Finalistas

**Cache96:** `qwen3.8-flash-next-ud-iq4xs-cache96-physical-nothink-256k`

- Backend ID `llama-cpp-qwen4exp-cache`.
- UD-IQ4_XS; PLE concorrente; indexer K-only; QSA gather.
- 96 slots por camada de experts host-resident; limite de 2 uploads por camada/passo.
- 16 threads e afinidade física; ubatch 512; Q8_0 KV; layer split `0.5,0.5`; main GPU 1.
- `spec-type=none`, thinking desativado. Tags históricas contendo `mtp` não refletem ativação: os argumentos efetivos são a autoridade.

**ik FIFO:** `qwen3.8-flash-next-iq4kt-ik-fifo-ub1024-256k`

- Backend ID `ik-llama-cpp-tuning`.
- IQ4_KT; PLE diferida; ubatch 1024; retenção FIFO de checkpoints.
- Correção de vazamento de `ignore_eos/logit_bias` entre requests.
- 16 threads; Q8_0 KV; layer split `0.5,0.5`; main GPU 1; MTP/thinking desativados.

Ambos colocam experts das camadas 0–13 e 23–38 no host: **30 camadas**, não todas as 48. A PLE permanece no caminho CPU/disco. O cache GPU replica parte dos experts dessas camadas; não remove automaticamente o banco original do host.

### 5.2. Desempenho preservado nos artefatos

**[LOCAL, histórico]** Mesmo harness, três repetições após warmup, prefixos reutilizados, saídas delimitadas. Esses TTFTs não representam ingestão fria.

| Contexto efetivo | Cache96 decode tok/s | ik FIFO decode tok/s | Cache96 TTFT ms | ik FIFO TTFT ms |
|---:|---:|---:|---:|---:|
| 12.592 | 38,69 | 32,56 | 102 | 110 |
| 62.291 | 31,36 | 29,86 | 153 | 180 |
| 124.651 | 25,38 | 27,12 | 200 | 253 |
| 224.101 | 19,82 | 24,07 | 283 | 388 |

Ingestão de **244.116 tokens**, sem truncamento, recuperação correta dos três valores sintéticos:

| Finalista | Prefill tok/s | Tempo de prefill |
|---|---:|---:|
| Cache96 | 154,00 | 1585,14 s, aproximadamente 26m25s |
| ik FIFO | 290,58 | 840,08 s, aproximadamente 14m00s |

Fontes: `docs/reports/qwen-flash-tuning-artifacts/final-comparison.json`, `.pi/qwen-flash-tuning/near-full-cache96-final-result.json` e `near-full-ik-fifo-result.json`. Os dois arquivos near-full foram inspecionados novamente durante esta pesquisa, não reexecutados.

Os finalistas passaram historicamente 12 casos delimitados de JSON/ferramentas e três pequenos programas executados em sandbox. Também há um caso multi-turn de ferramenta em 181.370 tokens. Isso não equivale a SWE-bench, avaliação ampla de qualidade, estabilidade por vários dias ou fidelidade BF16.

### 5.3. Experimentos que não devem ser repetidos como descoberta

- K-only eliminou fallback observado, mas não produziu grande ganho isolado de decode curto.
- QSA gather teve ganho A/B local em contexto longo; não cobre automaticamente toda verificação MTP.
- Cache64/96, pinned parcial de 2/4 blocos, threads 8/16/24 e afinidade já foram examinados.
- **Cache96 + ubatch1024 causou OOM no carregamento.** Continua configuração rejeitada.
- MTP2 no caminho ik regrediu no corpus testado. Taxa de aceitação não basta para prever ganho.
- FIFO melhorou TTFT de prefixo ao preservar checkpoint útil; não foi necessário inventar um novo gerenciador de prefixos.
- O harness histórico possuía `completionTokens` agregado zerado. Usar timings do servidor e não “corrigir” retrospectivamente registros como se tivessem sido medidos pelo código novo.

A comparação entre finalistas muda quantização, engine e configuração. É uma comparação de **receitas completas**, não prova causal isolada de uma única otimização.

## 6. Quantizações disponíveis e W4A16

### 6.1. Inventário de download

**[PRIMÁRIA]** Tamanhos abaixo provêm da API HF com metadados de arquivos, não da contagem automática de parâmetros. GB decimal e GiB não são intercambiáveis.

| Checkpoint | Bytes de safetensors / escopo | Observação |
|---|---:|---|
| `aixiaoma/...-W4A16` | 179.838.487.600, 30 shards | Cerca de 167,49 GiB. PLE BF16 e diversos componentes sensíveis não são INT4. |
| `cyankiwi/...-AWQ-INT4` | 188.286.106.928, 38 shards | Cerca de 175,36 GiB. Não assumir layout de shards de uma revisão antiga. |
| `arnomatic/...-W4A16-PLE8` | 131.838.278.848, 30 shards | Cerca de 122,78 GiB. PLE W8A16 g32; distribuição orientada a Strix Halo, não drop-in NVIDIA. |
| `nvidia/...-NVFP4` | 132.680.249.378, 11 shards | Cerca de 123,57 GiB. Não confundir com o layout RadixArk. |
| `garnermccloud/...-NVFP4-SSD-Stream` | 98.729.678.097 em safetensors | **O download total é 150.011.205.176 bytes**, pois inclui PLE sidecar e outros arquivos. |
| `turboderp/...-exl3`, branch `4.05bpw_h6_ng6` | 107.463.600.896 bytes de todos os arquivos | Cerca de 100,08 GiB incluindo metadados; não é previsão de RSS residente. |
| ji-farthing IQ4_KT | Card: 88,42 GiB de target | 58,59 GiB experts, 26,84 GiB PLE, 2,86 GiB demais; draft separado 2,21 GiB. |
| ji-farthing IQ3_KT | Card: 80,22 GiB de target | Gate/up com menor precisão; 50,39 GiB experts. Não equivalente em qualidade a IQ4_KT. |

Revisões consultadas: aixiaoma `75234a1d675cc7dd70569689872feb3d8aa1aca4`; cyankiwi `d39638a0e740fccb3e24ae0ea5cab34c15371ae6`; arnomatic `fbf547468c9652604e99261de054a2ef4e354f39`; NVIDIA `fc694b54fb0174e0913e6adf86691ef85a4ead47`; SSD Stream `7ac24134c76320339cb503c3b4071a312050b1fc`; EXL3 4.05 `55a732e0c4c3d4614bc42b68493bb930d9b02c0a`.

**Verificação antes de baixar:** somar os arquivos da revisão escolhida, identificar PLE/MTP/vision separadamente, verificar formato e shapes, orçamento de repack e espaço temporário. O nome W4A16 não informa tudo isso.

### 6.2. O caminho W4A16 viável em princípio

**[INFERENCE]** Uma composição plausível nesta máquina seria:

1. Experts INT4 de aproximadamente 60 GB brutos, mais escalas, divididos entre RAM e GPUs.
2. PLE lida por demanda de SSD, sem carregar 95 GiB BF16 no host.
3. Dense/GDN/indexer/router preservados inicialmente em precisão conservadora.
4. KV e estados dimensionados antes de maximizar residência de experts.
5. Loader incremental, sem manter cópia integral desquantizada, banco integral duplicado por rank ou staging proporcional a todo checkpoint.
6. CPU executando experts frios, ou movimentação seletiva para GPU: comparar, não presumir que enviar todos os experts ativos por PCIe seja melhor.

A receita [S10] de alesha-pro mostra uma construção Ampere relevante: AWQ + PLE FP8 externa + QSA KV FP8 calibrado + restauração de GDN/router sensíveis. O hardware medido tem **quatro 3090 e cerca de 125 GB de RAM**. O próprio documento alerta que o mínimo de 64 GB é teórico. Também usa uma revisão antiga de cyankiwi com layout distinto do catálogo atual: copiar números de shard sem verificar nomes/tensores seria incorreto.

O título dessa receita contém uma descrição imprecisa de parâmetros ativos; a arquitetura deste relatório vem do config/card oficial, não desse título.

**Divisibilidade TP:** para grupos de 128, dividir a dimensão intermediária 640 em duas partes produz 320, não múltiplo de 128. A receita W4A16-PLE8 explica uso de expert parallel para manter experts inteiros. Isso é uma restrição daquela composição de quant/kernel, não prova de que todo TP2 seja impossível. EP também não elimina seu custo de comunicação nem resolve sozinho o orçamento de host.

### 6.3. Qualidade e calibração

O card ji-farthing publica, contra BF16, Code PPL 1,8209 e KLD médio 0,125 para IQ4_KT; para IQ3_KT, 1,8273 e 0,135, em corpus e procedimento delimitados [S3]. É evidência melhor que julgamento visual, mas não uma comparação direta contra UD-IQ4_XS ou EXL3 no trabalho real do usuário.

Priorizar EXL3 4.05 antes de 3.05/2.05 preserva uma comparação menos agressiva de precisão. Quants menores e REAP/pruning são **outro eixo de qualidade**, não aceleração exata do mesmo modelo. Quantizar PLE ou cabeças também muda o modelo numérico, embora o acesso por SSD aos mesmos bytes não mude.

## 7. SSD streaming: separar PLE de experts

### 7.1. PLE é o caso favorável

O card SSD Stream documenta **16 linhas FP8 de 160 bytes por token**, ou **2560 bytes úteis/token** [S8]. Com páginas de 4 KiB, 16 leituras distintas representam aproximadamente 64 KiB, antes de casos de linhas que cruzem páginas. A 35 tok/s, isso é da ordem de 560 buscas/s e 2,2 MiB/s de páginas em um cenário simples.

A carga bruta é pequena; o problema é **latência, dependências, faults síncronos, eficiência do prefill e pressão de page cache**, não leitura sequencial de dezenas de GB/s.

No prefill, há muitos tokens conhecidos. A engine pode calcular índices antecipadamente, deduplicar linhas/páginas, emitir lotes de I/O e sobrepor leitura com GPU. No decode, só antecipar índices cujos tokens já são conhecidos; não usar resultados de uma previsão rejeitada como se pertencessem ao prefixo aceito.

### 7.2. O caminho local não usa O_DIRECT

**[LOCAL]** `backends/llama.cpp-qwen4exp-cache/src/llama-model.cpp` abre um descritor separado com `O_RDONLY | O_CLOEXEC` e recomenda `POSIX_FADV_RANDOM`. O comentário declara explicitamente que **evita `O_DIRECT`** por causa das pequenas leituras dispersas.

`src/llama-lazy-reader.h` deduplica linhas, usa workers e `pread`, com buffer de conversão. Logo:

- `LLAMA_ARG_LAZY_MODE=on-direct` é o **nome do modo do fork**, não prova de direct I/O do Linux.
- A leitura continua passando pelo page cache.
- Reutilizar pool de workers/buffers é uma hipótese de redução de overhead, a medir; criar mais threads não garante ganho.

### 7.3. Alternativas de I/O

| Técnica | O que oferece | Risco/custo | Decisão |
|---|---|---|---|
| `mmap` + hints | Simplicidade e cache do sistema | Fault síncrono, readahead inadequado, competição por páginas | Manter baseline; controlar políticas por região. |
| `pread` concorrente | Leitura explícita, deduplicação e buffers limitados | Continua bufferizado; overhead de workers/conversão | Já existe localmente. |
| `io_uring` bufferizado | Submissão em lote e concorrência sem thread por leitura | Ganho depende de queue depth e implementação | Experimento incremental possível. |
| `io_uring` + `O_DIRECT` | Evita cache do sistema para a PLE; memória de staging previsível | Alinhamento, read amplification, EOF/short reads, filesystem | Candidato, não melhoria presumida. |
| GDS/cuFile | Caminho storage→GPU em combinações suportadas | Depende de GPU, kernel, filesystem, modo real e topologia | Baixa prioridade; não assumir suporte nem fallback inevitável. |

A documentação GDS atual distingue modos `p2pdma`, `nvfs` e `compat`, e inclui mudanças recentes para BTRFS/ZFS no caminho de compatibilidade [S26]. Por isso seria incorreto concluir, sem probe, que toda chamada cuFile numa 3090 necessariamente usa um modo específico. Para esta PLE de poucos KiB por token, a alternativa POSIX/io_uring é mais simples e já tem evidência de utilidade. Não instalar kernel modules ou alterar ACS/IOMMU apenas por uma promessa de “zero copy”.

### 7.4. O que SSD Stream realmente provou

**[RELATO, fonte primária S8]** Em RTX PRO 6000 Blackwell 96 GB, com pesos principais em GPU, o autor reporta 164,7 tok/s com PLE em SSD e aproximadamente 64 MiB de RAM de trabalho, contra 148,5–156,2 tok/s com tabela em RAM. Requests com muitos índices inéditos ficaram em 126–137 tok/s.

O perfil portátil para GPUs de 24–32 GB é explicitamente experimental, recomenda **128 GB host**, usa contexto padrão **16k** e desativa **MTP e CUDA graphs**. A aceitação completa em placas de 24/32 GB estava pendente no card consultado. Isso demonstra o princípio da PLE, não uma receita comprovada de 256k rápido nas duas 3090.

### 7.5. Experts: aritmética correta do tráfego

**[INFERENCE]** Um expert tem aproximadamente:

`3 × hidden × intermediate = 3 × 2560 × 640 = 4.915.200 pesos`.

Em INT4 puro, antes de escalas/padding: **2.457.600 bytes = 2,34375 MiB**. Com top-10 em 30 camadas host-resident:

`30 × 10 × 2.457.600 = 737.280.000 bytes/token`, ou **0,737 GB/token**.

Se todos esses experts precisassem vir do SSD a cada token, 35 tok/s exigiriam **25,8 GB/s**. Para todas as 48 camadas, seriam 1,180 GB/token e 41,3 GB/s. Estes são modelos simplificados sem reutilização, não tráfego local medido.

Exemplos de teto ideal por banda efetiva, ignorando latência e compute:

| Banda de leitura assumida | 30 camadas, sem hits | 48 camadas, sem hits |
|---|---:|---:|
| 2 GB/s | 2,71 tok/s | 1,70 tok/s |
| 7 GB/s | 9,49 tok/s | 5,93 tok/s |

Esses valores **não são velocidades medidas dos SSDs instalados**. Com cache, usar `T ≤ B / ((1-h) × bytes_demandados)`; `h` deve ser hit rate ponderado por bytes na fronteira SSD, não o hit rate de um cache GPU contra a RAM.

**Não é correto declarar streaming de experts universalmente impossível.** É pouco promissor para superar os finalistas se a maioria dos bytes continuar vindo do SSD. Pode ser útil para capacidade, para poucos experts frios ou se houver forte locality medida.

### 7.6. Hierarquia e layout

A PR [S17] oferece streaming MoE com cache de slabs, O_DIRECT e prefill por ondas; foi validada pelo autor em OLMoE e GLM-5.2, não como substituta do pipeline Flash-Next local. Tem restrição de múltiplos contextos compartilhando o mesmo modelo/cache e desativa mmap, o que exige cuidado ao combinar com PLE.

Layout expert-contiguous pode reduzir faults e melhorar coalescência quando experts realmente são lidos do disco. Antes de reempacotar GGUF:

1. Medir quais tensores causam I/O físico, não apenas acessos virtuais.
2. Distinguir strides entre experts de locality interna das matrizes.
3. Preservar hashes/valores e atualizar offsets com alinhamento correto.
4. Medir o custo da conversão e o espaço da segunda cópia.
5. Não atribuir ganho de um modelo/quant diferente ao layout isoladamente.

A arquitetura preferida continua: **PLE no SSD; banco de experts frios na RAM; experts quentes ou camadas selecionadas na VRAM**. Streaming adicional SSD→RAM de experts é uma extensão posterior, não requisito da primeira otimização.

## 8. O que reaproveitar do Syv

Fontes: checkout `backends/syv-qwen38`, seu `UPSTREAM_COMMIT`, `docs/optimizations.md`, patches e card do drafter [S11–S12]. O baseline vLLM registrado no arquivo local é `0e951951bd4cbf0560921fdb6389df8721b445c7`.

| Otimização Syv | Transferibilidade ao Flash-Next | Recomendação |
|---|---|---|
| Requantizar embeddings/lm_head | Princípio transferível; shapes e caminhos distintos | Inspecionar tensores reais e proteger router/GDN/indexer. |
| Estado GDN FP16 | Possível eixo de capacidade/tráfego | Não aplicar sem validação longa de estado/rollback; default oficial Flash é FP32. |
| Marlin W4A8 INT8 seletivo | Potencial no prefill de um backend W4A16 | Provar dispatch MoE e semântica das escalas; não copiar regex por nome. |
| Correção de escalas negativas Marlin | Relevante só ao formato/caminho atingido | Verificar sinal, kernel e revisão; não aplicar universalmente. |
| Split-KV durante verify | Ideia de paralelismo aproveitável | QSA exige kernel/máscara/índices próprios. |
| Prefill attention INT8 | Patch direto não serve | Guard Syv exige 24 Q/4 KV; Flash tem 24/2, TP2 seria 12/1. |
| DFlash2 27B | **Incompatível diretamente** | Não usar o checkpoint do 27B como draft hidden-state do Flash. |
| Lookup/ngram drafting | Pode ajudar cópia/edição de código | Exige verificador exato e rollback correto; diferente de PLE. |
| Prefix cache híbrido | Alta relevância em uso agentic | Avaliar extensão/edição de prefixos, checkpoints e isolamento. |
| Sampler e CUDA graphs | Possível ganho se overhead dominar | Só depois de profiling; buffers e tabelas precisam de endereços/versões estáveis. |

### 8.1. Por que o drafter do 27B não serve

`syvai/Qwen3.8-27B-DFlash2-W4A16` tem hidden 5120, 64 camadas alvo e seleção `[5,19,33,47,61]`; Flash-Next tem hidden 2560 e 48 camadas. A camada 61 nem existe no alvo. O problema não é apenas dtype ou tokenizer: são representações internas e treinamento condicionado ao modelo alvo.

As buscas por DFlash/Flash-Next e a listagem HF consultada localizaram drafters MTP, inclusive MLX, mas não um DFlash2 Flash-Next verificado para CUDA. Isso é **ausência nas fontes consultadas**, não prova universal de inexistência.

Treinar um drafter novo exige corpus, extração de estados, treino, calibração e integração. É investimento muito maior que explorar o MTP nativo já disponível.

### 8.2. Prefix cache é mais importante que um número de pico

Para uma sessão de programação, o custo aproximado é:

`tempo de tarefa = ingestão não reutilizada + geração + execução de ferramentas + correções/retries`.

Uma ferramenta que reenvia 180k tokens com poucas mudanças pode ser muito mais beneficiada por restauração correta do estado híbrido que por +10% de decode. O ik já tem evidência local de benefício da política FIFO; o trabalho futuro é verificar comportamento em prefixos reais editados, ramificação de conversa e mensagens de ferramenta, não adicionar um segundo cache genérico.

## 9. Fóruns: sinais úteis e contradições

### 9.1. EXL3, uma 3090 e 64 GB

Na discussão HF [S5], um usuário com 7800X3D e 64 GB relata aproximadamente 30–35 tok/s, MTP e configuração de 262144. O mesmo tópico contém diferenças relevantes entre CPUs, revisões e quants, além de benchmarks em RTX PRO 6000 que não são equivalentes a offload no host.

Um contexto configurado não basta; a ingestão efetiva deve vir dos contadores do servidor. Relatos de ferramentas funcionando são úteis, mas não substituem baterias de tool choice, múltiplas chamadas e JSON fragmentado em streaming.

### 9.2. Comparativo recente EXL3/llama.cpp

No Reddit [S6], o autor usa **duas RTX 3080 modificadas de 20 GB, Xeon 6148 e 128 GB DDR4**. Reporta EXL3 4.05 com aproximadamente **25 tok/s** médios e **870 tok/s prefill**, mantendo geração até 160k, versus aproximadamente 13 tok/s e 270 tok/s em seu llama.cpp.

Limites:

- Hardware, quantização e configurações diferem dos finalistas daqui.
- A alegação de melhor qualidade EXL3 no post não foi validada em comparação controlada nesta pesquisa.
- O autor relata tool calling funcional numa sessão de três horas; não é garantia universal.
- Um comentarista com i5-13400F, 3090 e 64 GB relata apenas 4–5 tok/s EXL3 versus 20–35 tok/s em outro fork.
- Outro relato favorável a vLLM/ik envolve **Qwen3.8-27B**, não Flash-Next; não o incluir como resultado deste modelo.

**Conclusão:** EXL3 merece A/B sério. O fórum não sustenta escolhê-lo antecipadamente como vencedor.

### 9.3. Cache de experts e MTP

A PR #27861 [S13] reporta locality temporal, mas pouco benefício de uma lista fixa de experts populares. Seu autor mede LRU64 aproximadamente 67% e LRU128 aproximadamente 81% em um workload; outro usuário relata valores diferentes. São curvas dependentes do corpus, não constantes do modelo.

O comentário CUDA [S14], com duas 3090, documenta IDs duplicados por token colapsando no `mul_mat_id` batched. O comentário [S15] documenta tabelas host/device em versões diferentes durante prefill num caminho Vulkan. Este segundo caso não prova defeito CUDA local, mas revela um invariante de projeto que também precisa ser respeitado aqui.

Outros relatos mostram overhead persistindo mesmo com hit rate alto: CPU ainda faz preparação/zeros/barreiras; GPU ainda executa rotas fictícias; há cópias e sincronizações. Logo, hit rate não é o objetivo final: **tempo de tarefa correto é**.

## 10. Comparação das famílias de backend

| Família | Evidência para Flash-Next | Gargalo/risco nesta máquina | Prioridade |
|---|---|---|---|
| llama.cpp custom local | Execução e qualidade delimitada já medidas | Cache single-token, buffers, prefill, QSA longa | Base principal de evolução |
| ik_llama.cpp | IQ4_KT e longo contexto já medidos | MTP pode regredir; revisão deve preservar EOS/checkpoints | Referência de longo contexto |
| ExLlamaV3/TabbyAPI | Suporte e quants públicos; knobs locais existentes | CPU/quant/runtime muito influentes; integração tools | **Primeiro comparador novo** |
| vLLM W4A16 custom | Receita Ampere 4×3090 demonstrada | Residência host/GPU, PLE, TP/EP, Marlin/QSA específicos | Segunda linha |
| SGLang SSD Stream | PLE eficiente demonstrada em Blackwell 96 GB | Perfil 24 GB experimental, offload e RAM recomendada maior | Fonte de implementação PLE |
| FreeToken | PLE disco, expert offload e trabalho TP2 | Banco host/pinned NVFP4 excede RAM em composição conhecida | Pesquisa posterior, não execução direta |
| KTransformers | Pedido específico de integração [S20] aberto | Suporte específico não demonstrado pela issue consultada | Acompanhar; não assumir pronto |
| AirLLM | Autor documenta Flash em 5,95 GB VRAM com streaming de camadas [S21] | Capacidade, muito I/O e espaço de preparação | Baixa para latência interativa |
| SparkLab/FTW | Empacotamento para DGX Spark/GB10 [S22] | Plataforma de memória unificada diferente | Inspiração, não receita 3090 |
| FlexGen/PowerInfer-2 | Trabalhos sobre offload/esparsidade [S23–S24] | Não demonstram integração pronta qwen4_exp nesta rig | Referência conceitual |

Não reutilizar kernels Vulkan/RDNA/Metal/AMX apenas por prometerem velocidade em outro host. O Ryzen aqui oferece AVX-512, não AMX; a GPU é SM86. BeeLlama/Buun e outros forks locais são candidatos somente quando a revisão e o conjunto de patches permitem um comparativo atribuído, não um torneio de nomes.

### 10.1. FreeToken: separar portabilidade de capacidade

O trabalho de TP2 disponível no ecossistema FreeToken [S18–S19] não exige inventar toda a infraestrutura distribuída. Há abordagem de particionamento por ID de expert, router/hidden replicados e recomposição por all-reduce. Isso é distinto de sharding da dimensão intermediária e não implica necessariamente all-to-all.

Mesmo assim, particionar experts entre dois processos na **mesma máquina** não reduz a soma de RAM física se os bancos completos continuam no host. A composição NVFP4 previamente examinada mantém aproximadamente 63 GiB de experts pinned, maior que os 60,48 GiB totais disponíveis. PLE no SSD resolve a PLE, não esse banco.

Uma engine com propriedade exclusiva dos pesos entre host/GPU, carga incremental e cache de disco poderia mudar o orçamento. Isso é uma modificação de loader/residência, não simplesmente `--tp 2`.

## 11. Backend customizado recomendado

### 11.1. Escolha de base e não objetivos

**[INFERENCE, proposta]** Manter um fork C++ pequeno sobre a base do finalista cache, com ik como comparador independente. Primeiro conservar a quantização UD-IQ4_XS, o template e a API. Não fazer simultaneamente troca de quantização, reescrita de sampler, novo scheduler e novo protocolo.

O objetivo não é criar um framework universal. É otimizar `qwen4_exp` para **uma sequência de programação nesta máquina**, preservando coerência, ferramentas, prefix cache e contexto longo.

A API OpenAI e a supervisão do model-loader já existem. Um novo executável compatível pode ser registrado como outro backend da família existente, com schema fiel ao `--help`; não precisa criar um novo `BackendKind` só porque o checkout mudou. Se uma engine realmente nova exigir contrato próprio, seguir a integração completa de kind, schema, geração, argumentos e health probe — não nesta rodada de pesquisa.

### 11.2. Fase A: orçamento e profiling

Medir, por faixa de contexto e fase:

- Tempo em GDN, indexer/QSA, experts CPU, experts GPU, sampler e CPU/GPU sync.
- Bytes H2D/D2H, cópias por camada, uploads concluídos versus desperdiçados.
- PLE: latência exposta, filas, páginas físicas, deduplicação, uso de memória.
- Hit rate por camada e por bytes, misses, experts evictados antes de uso.
- VRAM por placa: pesos fixos, cache, KV/indexer, draft, buffers e margem.
- RAM: RSS/PSS, locked/pinned, pico no load, page faults e swap-in/out.

Amdahl é o filtro: se PLE consome 3% do tempo, eliminá-la inteiramente renderia no máximo cerca de 3,1%. Não investir em um leitor sofisticado antes de observar tempo exposto relevante.

### 11.3. Fase B: cache correto em verify MTP

Invariante por rota `(camada, token, posição top-k)`:

**O expert contribui exatamente uma vez: CPU ou GPU, nunca ambos e nunca nenhum.**

Projeto conservador:

1. Manter uma geração de mapeamento imutável durante a execução do grafo correspondente; host e device observam a mesma geração.
2. Publicar upload só após conclusão da cópia; nunca sobrescrever slot ainda referenciado por grafo em voo.
3. Remapear IDs em matrizes `[top_k, n_tokens]` sem perder correspondência entre token, peso de roteamento e saída.
4. Evitar IDs fictícios duplicados dentro de cada token. Para o top-10, uma proposta simples são **dez slots zero distintos**, um por posição de rota, em vez de um único dummy compartilhado. Slots zero podem ser reutilizados entre tokens se os kernels permitirem a identidade repetida entre tokens; o requisito problemático documentado é a duplicação dentro do mesmo token.
5. Reservar custo adicional desses slots: comparado a um dummy, nove experts zero por camada custariam aproximadamente **0,618 GiB** no total de 30 camadas, em INT4 puro; tipos mistos e alinhamento mudam isso.
6. Começar por verify pequeno; prefill completo pelo cache é uma etapa separada, pois envolve muitos ubatches e mutações de mapeamento.
7. No rollback MTP, descartar estados do modelo associados aos tokens rejeitados. O cache de pesos pode conservar experts carregados, pois pesos não dependem do prefixo; suas referências e tabelas ainda precisam estar sincronizadas.
8. Medir aceitação e custo total. Um draft que consome VRAM pode retirar slots úteis ou exigir mais experts na CPU.

Não alterar kernels globais `mul_mat_id` se a solução localizada de IDs distintos satisfizer o contrato. Compactação explícita de rotas ativas é uma alternativa mais ambiciosa, com maior potencial de eliminar trabalho fictício, mas exige kernels/interfaces próprias e testes numéricos de scatter/redução.

### 11.4. Fase C: retirar overhead, não apenas aumentar hit rate

Depois da correção:

- Um caminho all-hit pode evitar trabalho CPU e transferências desnecessárias; precisa ser medido contra o custo de decidir/selecionar o caminho.
- Agrupar uploads por camada/intervalo contíguo quando houver benefício real.
- Cancelar ou não priorizar uploads obsoletos ainda não consumidos; não descartar trabalho já em uso.
- Preservar buffers máximos necessários ao alternar prefill/decode, evitando realocações repetidas com crescimento de QSA.
- Dimensionar slots por placa/camada conforme custo observado. A GPU do desktop pode ter menos margem mesmo com split nominal 50/50.
- Preferir estabilidade de armazenamento para CUDA graphs; não trocar topologia ou endereços a cada decisão de cache.

**Conta de capacidade:** 30 camadas × 96 slots × 2,34375 MiB ≈ **6,59 GiB** antes de metadados, dummy e quantizações mistas. Não são “96 experts no total”.

**Conta de upload:** duas inserções **por camada/passo** podem representar até `30 × 2 × 2,34375 MiB` por passo. A 35 tok/s, aproximadamente **4,81 GiB/s** antes de overhead se todas as camadas atingirem o limite. Não confundir com dois uploads globais por token. É teto simplificado; a atividade real deve ser contada.

### 11.5. Fase D: leitor PLE limitado e assíncrono

Aproveitar o leitor existente antes de substituí-lo:

1. Pool de workers e buffers reaproveitados, com limite explícito de memória.
2. Deduplicação por linha e, em uma implementação direct, por página física.
3. Antecipação no prefill e overlap com computação anterior à layer 2.
4. Escolha baseada em medidas entre `pread`, io_uring bufferizado e direct I/O.
5. Leitura exata com tratamento de EINTR, short reads, EOF, offsets, alinhamento e conversão de quant por linha.
6. Erro de I/O deve produzir falha identificável; nunca linha zero silenciosa ou reutilização de dados de outra request.

Não misturar política de cache PLE com cache de experts: tamanhos, reutilização e custo de miss são diferentes.

### 11.6. Quando considerar uma engine W4A16 dedicada

Somente se o A/B mostrar que o limite do C++/EXL3 está em kernels/ingestão e não apenas em capacidade/PCIe. Uma arquitetura possível usaria:

- PLE sidecar com leitor assíncrono;
- experts INT4 com propriedade exclusiva de residência host/GPU;
- GEMV/GEMM CPU para misses, Marlin GPU para hits/prefill;
- QSA e GDN conservadores inicialmente;
- MTP nativo com estados reversíveis;
- prefix cache híbrido e servidor existente reaproveitados.

O custo real inclui loader, formatos, quantização, sincronização, kernels, template/tools, scheduler e manutenção upstream. **Reescrever tudo em Python/Triton do zero é a opção de maior risco e pior primeira aposta.**

## 12. Experimentos priorizados: custo, ganho procurado e interrupção

Todos os ganhos nesta seção são **critérios de decisão propostos**, não previsões. Alterações operacionais exigem janela de execução; manter produção e rollback intactos.

| ID | Experimento | Custo/recursos | Aceitar se | Parar/rejeitar se |
|---|---|---|---|---|
| E0 | Baseline reproduzível dos dois finalistas e perfil de tempo | Sem novos pesos; janela GPU | Mesmos casos e limites documentados | Ambiente muda sem registro ou contadores não permitem comparação |
| E1 | PLE/mesmo GGUF no KC3000 versus SSD atual | Cópia de um target, aproximadamente 90 GiB; sem build | Melhora repetida de TTFT/prefill ou p95 sem regressão | Apenas efeito de page cache/aquecimento explica o resultado |
| E2 | EXL3 4.05 sem MTP, PLE disco | Aproximadamente 100 GiB mais runtime; build/setup moderado | Vantagem útil de tarefa, por exemplo ≥15%, com qualidade mantida | OOM, paging sustentado, tools incorretos ou regressão longa |
| E3 | EXL3 MTP 1/2 versus off | Sem novo target; VRAM de draft | Menor tempo total e preservação de correção | Aceitação alta mas verify/VRAM pioram o resultado |
| E4 | Cache C++ com verify pequeno e IDs distintos | Engenharia média, kernels testados em SM86 | Correção numérica + ganho líquido sobre cache sem MTP | Qualquer duplicação/perda de contribuição, instabilidade ou regressão |
| E5 | Buffers estáveis/all-hit/menos sync | Engenharia média após E0/E4 | Redução observada do overhead dominante | Complexidade sem ganho mensurável |
| E6 | Pool PLE/io_uring/O_DIRECT | Engenharia média; sem checkpoint novo | Reduz latência exposta ou pico RAM em corpus diverso | Amplificação de I/O/custo de fila piora p95 |
| E7 | QSA gather durante verify; estado GDN/KV seletivo | Engenharia média/alta; validação numérica longa | Menos custo preservando retrieval e estabilidade | Drift, NaN, loops ou erro em rollback |
| E8 | IQ3_KT ou EXL3 3.05 como eixo de qualidade | Outro checkpoint; espaço separado | Melhor tempo até solução sob tolerância de qualidade explícita | Ganho depende de perda inaceitável de precisão |
| E9 | W4A16 custom com PLE disco + residência particionada | Alto custo; 130–190 GB de origem e temporários | Orçamento de load provado e superioridade end-to-end | Banco host duplicado/pinned excede capacidade |
| E10 | Streaming de experts frios, layout contíguo | Alto custo, alteração de loader/formato | Resolve gargalo de capacidade com miss rate SSD baixo | Decode vira espera de SSD; não supera objetivo definido |
| E11 | Novo drafter DFlash treinado para Flash | Muito alto: dados, treino, integração | MTP nativo esgotado e retorno estimado justificável | Falta de dados/capacidade de treino ou incompatibilidade do alvo |

### 12.1. Receita de exploração EXL3

O checkout Tabby consultado possui `ngram_ram`, `cpu_moe_offload_layers`, `cpu_moe_split_experts`, `cpu_moe_threads`, `draft_mode` e `draft_num_tokens` em `backends/tabby/common/config_models.py`.

Ordem:

1. Pin de revisão EXL3 e checkpoint 4.05; ler configuração efetiva, não apenas presets de fórum.
2. Uma sequência; contexto inicial pequeno; PLE `ngram_ram=false`; MTP desativado.
3. Alocar pesos entre GPUs conservadoramente e computar experts frios na CPU.
4. Comparar offload por camadas com `cpu_moe_split_experts`, sem ativar simultaneamente opções mutuamente exclusivas. O schema local declara que split-experts **não suporta tensor parallelism**; não montar TP2 + split-experts como receita presumida.
5. Expandir contexto real, preservar margem de VRAM e observar RAM no carregamento e prefill.
6. Só então MTP1/MTP2, tool parser, prefix cache e sessão prolongada.

### 12.2. O que não priorizar

- Aumentar cache indiscriminadamente até esgotar VRAM.
- Pinning integral de PLE ou experts no host de 64 GB.
- Reduzir top-k do router ou remover experts e chamar isso de otimização sem perda.
- Reutilizar DFlash2 27B ou extrapolar seus 120+ tok/s.
- Expandir para 1M contexto antes de consolidar qualidade e latência em 256k.
- Alterar clocks, firmware, ACS/IOMMU ou comprar NVLink/SSD antes do profiling.
- Repetir FreeToken/SGLang NVFP4 sem corrigir primeiro o orçamento de RAM.

## 13. Protocolo de validação e critérios de promoção

### 13.1. Separar quatro estados de cache

1. **Cold process:** inicialização, carregamento e picos de RAM/VRAM.
2. **Cold prefix:** servidor já pronto, mas texto novo; ingestão não satisfeita pelo prefix cache.
3. **Warm prefix:** mesma base de conversa, extensão curta e resposta de ferramenta.
4. **Cold/warm filesystem:** residências distintas das páginas PLE/experts.

Não chamar cold prefix de cold disk. Não usar `drop_caches` global numa workstation ativa. Para estudo de I/O, preferir arquivo experimental e controle por arquivo quando aplicável; se não puder garantir estado frio, declarar essa limitação.

### 13.2. Workload

- Faixas curtas, aproximadamente 64k, 128k, 224k e um caso de 244k tokens efetivos.
- Código real, diffs, JSON, prosa e identificadores diversos para não favorecer PLE por repetição artificial.
- Greedy para diagnóstico de invariantes; sampling de produção em uma avaliação separada.
- Modo thinking igual nos dois braços. Comparar no-thinking contra no-thinking e reasoning contra reasoning, incluindo tokens de raciocínio no tempo.
- Geração de comprimento delimitado para throughput, mais tarefas até conclusão para valor prático.
- Ferramentas: escolha obrigatória e automática, múltiplas chamadas, Unicode/escaping, resposta da ferramenta, recuperação de erro e streaming fragmentado.
- Sessão prolongada com edição do início/meio do prefixo e alternância entre tarefas.

### 13.3. Medições mínimas

- TTFT e tempo até primeiro conteúdo útil, distinguindo reasoning quando presente.
- Prefill de tokens **novos**; cache hits reportados separadamente.
- Decode de tokens gerados, excluindo prefill; tempo end-to-end.
- Mediana, p95 e variabilidade, com múltiplas repetições balanceadas AB/BA.
- RAM/pinned máximos, VRAM por GPU, swap e I/O físicos.
- Aceitação MTP, tokens aceitos/passo, tempo de proposal/verify/rollback.
- Correção da tarefa e número de retries, não só tok/s.

### 13.4. Gates numéricos para o backend customizado

- `mul_mat_id` com todos hits, todos misses, mistos, dummies repetidos e distintos; F16/BF16 e quants efetivos.
- Batches que cruzem dispatches CUDA, incluindo 1, 2, 4, 8, 9 e 16, em vez de testar apenas o lote que mascara o problema.
- Comparar saída numérica contra referência CPU sem cache, com tolerância adequada; não aceitar apenas ausência de crash.
- Updates de tabela e uploads concorrentes; multi-ubatch prefill; cancelamento e destruição do modelo com trabalho em voo.
- Rollback MTP e retomada de prefixo; nenhuma contribuição duplicada ou omitida.
- I/O PLE com duplicatas, ordem, EOF, short reads e dados reais não zerados.
- Três cold starts e sessão prolongada antes de promoção; um benchmark rápido não encerra validação.

Para quantizações diferentes, equivalência byte a byte de texto não é requisito sensato. Para mudanças supostamente exatas de cache/I/O, comparação de tensores/logits e falhas de reprodução são importantes; variações normais de arredondamento devem ser separadas de corrupção estrutural.

### 13.5. Critério final

Promover uma receita apenas se entregar **menor tempo até tarefa correta**, com limite de memória estável e ferramentas corretas. Uma melhora isolada de prefill pode justificar profile especializado mesmo sem ganhar decode; não forçar um único vencedor global.

## 14. Upgrade opcional de hardware

O plano principal não depende de compra. Se houver intenção de ampliar a máquina:

- **96/128 GB de RAM:** tornam mais plausíveis bancos host maiores e loaders que hoje falham. 128 GB oferece margem maior; memória adicional não aumenta necessariamente banda e mais DIMMs podem reduzir frequência. AM5 continua dual-channel.
- **SSD:** já existe KC3000 adequado para um comparativo. Comprar outro antes de medir latência PLE e I/O de experts seria prematuro.
- **Mais VRAM:** pode remover offload e mudar completamente a classe de desempenho, mas duas GPUs adicionais implicam slots, potência, refrigeração e lanes. Não é equivalente a uma simples flag TP4.
- **NVLink:** mesmo quando disponível fisicamente, não resolve PLE/RAM nem transforma a VRAM em pool automático; o benefício depende do backend e da comunicação realmente dominante.

## 15. Conclusão e decisão prática

**Hoje:** usar ik FIFO para ingestão/contexto longo e cache96 para decode curto, conforme o workload. Não substituir os finalistas por uma promessa de framework.

**Próxima rodada:** baseline instrumentado → A/B de PLE no KC3000 → EXL3 4.05 → MTP medido no vencedor → cache C++ com verify correto.

**Backend customizado:** faz sentido como evolução concentrada de cache, buffers, QSA e PLE no stack C++ já comprovado. A parte mais importante não é inventar kernels de baixo bit; é evitar trabalho e movimentação desnecessários sem corromper estado, roteamento ou prefixos.

**W4A16:** opção séria, mas não atalho. Sua viabilidade depende de onde cada tensor vive durante load, prefill e decode. Uma implementação incremental pode caber; as receitas públicas consultadas não provam superioridade em duas 3090 + 64 GB.

**SSD:** excelente destino da PLE; destino condicional para experts frios. A distinção entre poucos KiB de lookup e centenas de MB de experts por token é a decisão arquitetural central.

## 16. Fontes e rastreabilidade

Todas consultadas ou referenciadas na coleta de 8 de setembro de 2026. Benchmarks de terceiros permanecem relatos. Estado de issues/PRs é temporal, não garantia sobre futuras revisões.

### Modelo e quantizações

- **S1 — Qwen, card oficial:** https://huggingface.co/Qwen/Qwen3.8-Flash-Next — arquitetura, capacidades e contagem lógica.
- **S2 — Config oficial:** https://huggingface.co/Qwen/Qwen3.8-Flash-Next/blob/de4b8e4d43b917e7706784d8bb445c9af86a3540/config.json — shapes e parâmetros arquiteturais.
- **S3 — ji-farthing:** https://huggingface.co/ji-farthing/Qwen3.8-Flash-Next-ik-llama-GGUF — composição IQ3/IQ4_KT, MTP e avaliação contra BF16.
- **S4 — EXL3:** https://huggingface.co/turboderp/Qwen3.8-Flash-Next-exl3 — branches, requisitos e quants.
- **S5 — Fórum EXL3:** https://huggingface.co/turboderp/Qwen3.8-Flash-Next-exl3/discussions/2 — 3090/64 GB, MTP e resultados em outros hardwares.
- **S6 — Reddit EXL3 versus llama.cpp:** https://www.reddit.com/r/LocalLLaMA/comments/1wa1jkb/exllamav3_comfortably_beats_llamacpp_running/ — relato com duas 3080 20 GB e contrapontos.
- **S7 — W4A16:** https://huggingface.co/aixiaoma/Qwen3.8-Flash-Next-W4A16 — INT4 Ampere, componentes preservados.
- **S8 — SSD Stream:** https://huggingface.co/garnermccloud/Qwen3.8-Flash-Next-NVFP4-SSD-Stream — sidecar, memória de trabalho, benchmark Blackwell e limitações do perfil portátil.
- **S9 — W4A16-PLE8:** https://huggingface.co/arnomatic/Qwen3.8-Flash-Next-W4A16-PLE8 — requantização PLE e limites TP/grupo em receita Strix Halo.
- **S10 — Quatro 3090 AWQ:** https://github.com/alesha-pro/qwen38-flash-next-4x3090 — receita Ampere, restauração de GDN/gates e validação longa.
- **S27 — AWQ cyankiwi:** https://huggingface.co/cyankiwi/Qwen3.8-Flash-Next-AWQ-INT4 — checkpoint e inventário de shards.
- **S28 — NVIDIA NVFP4:** https://huggingface.co/nvidia/Qwen3.8-Flash-Next-NVFP4 — formato/metadata da variante NVIDIA.
- **S29 — RadixArk NVFP4:** https://huggingface.co/RadixArk/Qwen3.8-Flash-Next-NVFP4 — origem da composição SSD Stream, distinta da NVIDIA.
- **S30 — Unsloth GGUF:** https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF — família de quants usada no finalista cache.

### Syv, cache e engines

- **S11 — Syv:** https://github.com/syv-ai/qwen38-27b-rtx3090 — conjunto de otimizações do 27B; cotejado com o checkout local.
- **S12 — Drafter Syv:** https://huggingface.co/syvai/Qwen3.8-27B-DFlash2-W4A16/blob/4d30ec736ffc6b8688dc2ae2b502d9b48bdec279/config.json — hidden, camadas alvo e quantização.
- **S13 — Cache experts:** https://github.com/ggml-org/llama.cpp/pull/27861 — desenho, locality, custos, single-token e relatos.
- **S14 — IDs duplicados CUDA:** https://github.com/ggml-org/llama.cpp/pull/27861#issuecomment-5529656015 — reprodução e análise de `mul_mat_id`.
- **S15 — Tabelas e prefill:** https://github.com/ggml-org/llama.cpp/pull/27861#issuecomment-5543984918 — experiência Vulkan, MTP e versões host/device.
- **S16 — PLE concorrente:** https://github.com/ggml-org/llama.cpp/pull/28136 — proposta incorporada/adaptada no fork local.
- **S17 — Streaming experts:** https://github.com/ggml-org/llama.cpp/pull/25294 — cache de slabs, prefill por ondas e restrições de contexto.
- **S18 — FreeToken PLE:** https://github.com/FlashML-org/FreeToken/pull/311 — leitura PLE em disco.
- **S19 — FreeToken TP2:** https://github.com/FlashML-org/FreeToken/pull/385 — desenvolvimento de paralelismo qwen4_exp; não garantia de fit local.
- **S20 — KTransformers:** https://github.com/kvcache-ai/ktransformers/issues/2179 — pedido específico de suporte.
- **S21 — AirLLM:** https://github.com/lyogavin/airllm — Flash-Next em baixa VRAM via streaming de camadas, orientação de capacidade.
- **S22 — FTW/SparkLab:** https://huggingface.co/oakmindai/Qwen3.8-Flash-Next-NVFP4-FTW — artefato experimental para DGX Spark.
- **S23 — FlexGen:** https://arxiv.org/abs/2303.06865 — referência de offload/throughput, não receita qwen4_exp.
- **S24 — PowerInfer-2:** https://arxiv.org/abs/2406.06282 — referência de execução e I/O por esparsidade em outra plataforma.
- **S25 — Receita vLLM:** https://recipes.vllm.ai/Qwen/Qwen3.8-Flash-Next — stack de serving do modelo.
- **S26 — NVIDIA GDS:** https://docs.nvidia.com/gpudirect-storage/release-notes/index.html — modos, compatibilidade e mudanças recentes.
- **S31 — QSA gather:** https://github.com/ggml-org/llama.cpp/pull/28213 — atenção sobre posições selecionadas.
- **S32 — Indexer K-only:** https://github.com/ggml-org/llama.cpp/pull/28330 — retirada de V desnecessário no indexer.
- **S33 — MTP Flash:** https://github.com/ggml-org/llama.cpp/pull/28243 — integração do draft Flash-Next.
- **S34 — ExLlamaV3:** https://github.com/turboderp-org/exllamav3 — runtime alternativo; versão e kernels precisam ser pinados no experimento.
- **S35 — EXL3 offload, relato de problema:** https://github.com/turboderp-org/exllamav3/issues/326 — contraponto à extrapolação de benchmarks favoráveis.

### Evidência local reproduzível

- [Resultados consolidados](qwen-flash-tuning-results.md).
- [Auditoria anterior e fontes adicionais](qwen3.8-flash-next-dual-rtx3090-2026-09-04.md).
- [Patches, receitas e limitações de reprodução](qwen-flash-tuning-artifacts/README.md).
- [Comparação final preservada](qwen-flash-tuning-artifacts/final-comparison.json).
- `.pi/qwen-flash-tuning/near-full-cache96-final-result.json` e `near-full-ik-fifo-result.json`: timings de 244.116 tokens, artefatos locais ignorados pelo Git.
- `~/.config/model-loader/profiles/`: dois profiles descritos na seção 5, configuração efetiva consultada.
- `backends/llama.cpp-qwen4exp-cache/src/{llama-model.cpp,llama-lazy-reader.h,llama-graph.cpp}`: I/O PLE e guard single-token.
- `backends/tabby/common/config_models.py`: campos de PLE, offload, threads e draft.
- `backends/syv-qwen38/{UPSTREAM_COMMIT,docs/optimizations.md,patches/}`: base vLLM e limites dos patches.

Os diretórios `backends/` e `.pi/` são locais/gitignored; sua existência não é requisito de um clone limpo. O relatório e os artefatos de tuning em `docs/reports/` preservam a distinção entre código examinado, execução histórica e hipóteses futuras.
