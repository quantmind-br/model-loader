# Qwen3.8-Flash-Next: auditoria de desempenho em duas RTX 3090 e 64 GB de RAM

Data: 2026-09-04, horário local. Algumas consultas e eventos registrados em UTC já correspondem a 2026-09-05.

## 1. Resumo executivo

**A estratégia atual é adequada para executar o modelo neste hardware, mas não há evidência suficiente para classificá-la como a de melhor desempenho disponível.**

A combinação GGUF + PLE sob demanda no SSD + experts distribuídos entre RAM e VRAM é tecnicamente fundamentada. O fork com MTP externo apresentou aproximadamente 30–34 tok/s em geração de código com contexto curto aquecido. Entretanto:

1. Os testes anteriores não validaram desempenho nem correção com 256k efetivamente preenchidos.
2. A instância atual sofreu falha de alocação CUDA durante a inicialização e carregou após fallback sem pipeline parallelism.
3. Há otimizações públicas relevantes que não estão no fork: leitura concorrente da PLE, QSA com atenção sobre os elementos selecionados e remoção de V-cache desnecessário do indexer.
4. Cache dinâmico de experts, EXL3 e quants específicos de ik-llama merecem comparação, mas não estão demonstrados como superiores nesta máquina.
5. Ganhos de diferentes PRs não podem ser somados: algumas otimizações single-token não são utilizadas durante a verificação mult-token do MTP.

**Recomendação:** preservar o profile atual como referência funcional; medir contexto longo e corrigir/quantificar os gargalos antes de baixar outros modelos ou substituir o backend.

Nenhum profile, backend, driver ou parâmetro de hardware foi alterado durante esta pesquisa. Não foi executado novo benchmark de inferência. Este documento consolida pesquisa web, Reddit, GitHub, inspeção local e resultados históricos, distinguindo suas limitações.

## 2. Requisitos e escopo

- Modelo: Qwen3.8-Flash-Next, não Qwen3.8-27B.
- Hardware: duas RTX 3090 de 24 GiB, Ryzen 9 9950X3D e 64 GB de RAM instalada.
- Contexto mínimo solicitado: 262.144 tokens, chamado de 256k neste relatório.
- Uso: coding agents, ferramentas, raciocínio e português brasileiro.
- Prioridade: desempenho útil, sem sacrificar silenciosamente contexto ou qualidade.
- Profile escolhido pelo operador: somente a variante Flash-Next com o fork `llama-cpp-qwen4exp-mtp`. Outros modelos/famílias continuam existindo no catálogo.

Pergunta central: a solução adotada é a melhor disponível para essas restrições?

A pesquisa não consegue provar um ótimo global. Pode identificar soluções viáveis, incompatibilidades, evidências comparáveis e os experimentos que faltam para decidir.

## 3. Metodologia e confiança nas evidências

### 3.1. Inspeção local

Foram consultados `lscpu`, `free`, `swapon`, `zramctl`, `nvidia-smi`, `lsblk`, `findmnt`, `lspci`, `stat`, `ps`, `/proc/<pid>/{status,smaps,smaps_rollup,io}`, o profile, logs e código do fork. Consultas Git locais foram somente leitura. Não houve carga/descarga de modelos, mudanças de afinidade, stress, limpeza de cache ou benchmark de disco.

### 3.2. Pesquisa pública

- Fontes oficiais: card Qwen, especificações AMD/NVIDIA e receitas de execução.
- GitHub: PRs, issues, descrições técnicas e comentários com medições.
- Hugging Face: cards de quants e discussões de execução.
- Reddit: buscas e leitura por OpenCLI, incluindo comentários. Buscadores gerais e Exa inicialmente não encontraram os posts; a busca direta no Reddit funcionou.
- Firecrawl self-hosted: consulta sem resultados; não sustentou conclusões.
- O executável `agent-reach` não estava disponível; usaram-se os backends disponíveis indicados pela skill, incluindo Exa, GitHub CLI e OpenCLI.

### 3.3. Critério de interpretação

1. Medição local controla melhor o hardware, mas os testes históricos eram curtos e pouco abrangentes.
2. Benchmarks públicos A/B com código, parâmetros e profundidade explícitos são candidatos a reprodução, não previsões de velocidade local.
3. Relatos de usuários e números de marketing têm confiança inferior.
4. Contexto configurado, contexto efetivamente ocupado, geração de tokens, prefill e throughput agregado são métricas diferentes.
5. Publicações e PRs podem mudar. Os estados neste documento são os observados durante a pesquisa.
6. Comentários recuperados por ferramentas podem omitir respostas recolhidas ou truncar corpos longos; não se afirma leitura integral de todos os comentários de cada thread.

## 4. Arquitetura do modelo e orçamento de memória

Segundo o card oficial [S1]:

| Componente | Característica |
|---|---|
| Modelo principal | 125B parâmetros, aproximadamente 6B ativos/token |
| PLE/n-gram | 51B parâmetros adicionais |
| MTP | 4B parâmetros adicionais, uma camada treinada com múltiplos passos |
| Camadas principais | 48 |
| Atenção híbrida | 36 Gated DeltaNet + 12 Qwen Sparse Attention |
| Experts | 512 roteados, top-10, mais um compartilhado |
| Hidden dimension | 2560 |
| Janela nativa | 262.144 tokens |
| QSA | Seleção com orçamento de 512 blocos ou 2048 tokens |

Portanto, **125B não inclui toda a tabela PLE e o MTP**. O conjunto de parâmetros armazenados aproxima-se de 180B, antes de considerar diferenças de conversão e componentes multimodais.

### 4.1. Por que PLE no SSD faz sentido

A PLE é uma tabela de embeddings acessada por índices derivados de n-grams, não uma matriz inteira multiplicada a cada token. A arquitetura usa aproximadamente 16 consultas de linhas por token. Isso torna possível manter o arquivo no SSD e buscar apenas as páginas necessárias.

O custo real depende de latência de leitura aleatória, amplificação por páginas, readahead, concorrência de I/O e estado do page cache. O fato de funcionar não significa custo zero, sobretudo no prefill de texto diverso.

### 4.2. Por que experts no SSD são outra questão

Experts ativos demandam leituras de pesos muito maiores. A solução deve manter o conjunto necessário de pesos computacionais em RAM/VRAM, evitando faltas recorrentes atendidas pelo SSD. PLE em disco e streaming contínuo de experts não são equivalentes.

### 4.3. RAM + VRAM não são memória unificada

A soma 64 GB + 48 GiB não representa uma única região rápida. Transferências, cópias, estados intermediários e buffers ocupam memória própria. Um backend que mantém todos os experts no host pode não aproveitar a VRAM para reduzir a necessidade de RAM, mesmo quando mantém cópias/cache desses experts nas GPUs.

## 5. Hardware efetivamente encontrado

| Componente | Observação |
|---|---|
| CPU | AMD Ryzen 9 9950X3D, 16 núcleos, 32 threads, SMT ativo |
| NUMA | Um nó exposto ao sistema |
| L3 | 128 MiB totais em dois domínios: 96 MiB e 32 MiB |
| Afinidade dos domínios L3 | 96 MiB: CPUs 0–7,16–23; 32 MiB: 8–15,24–31 |
| RAM utilizável | 60,48 GiB pelo Linux |
| RAM disponível na coleta | Aproximadamente 42–43 GiB; snapshot ocioso, não pico de inferência |
| Swap | ZRAM de aproximadamente 16 GiB lógicos; 7,5–7,7 GiB lógicos usados, cerca de 2,2 GiB físicos comprimidos na amostra do auditor |
| GPUs | Duas NVIDIA RTX 3090 de 24 GiB |
| Driver | 610.57.04 |
| Link entre GPUs | PHB, sem NVLink ativo |
| PCIe | Largura negociada x8 por GPU; capacidade máxima reportada x16, Gen4 |
| Estado ocioso | P8; Gen1 reportado em repouso, não evidência de limite Gen1 sob carga |
| P2P | Driver reporta leitura/escrita disponíveis nas duas direções |
| Power limit | 290 W por GPU |

### 5.1. CPU e RAM

O 9950X3D é um candidato forte ao caminho de experts em CPU, mas 128 MiB de L3 não comportam dezenas de GiB de pesos. Não se deve atribuir velocidades de terceiros exclusivamente ao V-Cache sem A/B. AVX-512 e kernels apropriados tornam EXL3 interessante, mas afinidade, frequência, memória e implementação também importam.

O profile usa 16 workers, não afinidade explícita de um worker por núcleo físico. O processo pode migrar entre CCDs com caches diferentes. Testar 8/12/16 workers e afinidades é razoável; afirmar que um CCD vencerá seria prematuro.

A velocidade efetiva dos DIMMs/EXPO e a banda de RAM não foram estabelecidas nesta pesquisa. Não se deve apresentar DDR5-6000 como configuração local confirmada.

Swap usado não comprova thrashing atual. Seriam necessárias taxas durante inferência, além de uso de CPU e pressão de memória.

### 5.2. GPUs e margem

Amostra durante ociosidade:

| Métrica | GPU0 | GPU1 |
|---|---:|---:|
| Uso global em uma das amostras | 18,87 GiB | 23,00 GiB |
| Uso atribuído ao servidor | 16,83 GiB | 22,97 GiB |
| Memória livre reportada pelo auditor | 4800 MiB | 577 MiB |
| Temperatura | 51 °C | 36 °C |

A leitura posterior da GPU0 oscilou com o desktop. Memória reservada pelo driver pode fazer `total - used` diferir do campo `free`.

A GPU1 é o limitante evidente. Split igual de pesos não implica uso final igual: drafter, buffers, saída e estados também consomem VRAM.

Não foi demonstrado gargalo térmico ou de potência atribuível a esse profile. Contadores históricos não podem ser tratados como medições sob a carga atual. Aumentar power limit não é a primeira intervenção recomendada.

O P2P reportado pelo driver não comprova engajamento ou banda no backend. Medições antigas da skill com driver 610.43.02 não são medições atuais.

### 5.3. SSDs

| Unidade | Papel observado |
|---|---|
| PCH-RVR-1TB, `/dev/nvme1n1p1` | `/home/diogo/models`, ext4, pesos do modelo |
| KINGSTON SKC3000S1024G | Sistema/home; não é a unidade efetiva dos pesos |
| XPG GAMMIX S11 Pro | `/home/diogo/dev`, checkout e ferramentas |

O SSD dos pesos usa PCIe 3.0 x4 via chipset. A controladora foi identificada como família Silicon Motion SM2263EN/SM2263XT; isso não basta para afirmar variante exata ou presença de DRAM.

Havia aproximadamente 325 GiB disponíveis no filesystem dos modelos. O SSD de desenvolvimento estava próximo de 90% ocupado. SMART/saúde/temperatura do SSD dos pesos não foram verificados por falta de permissão.

**Hipótese:** comparar a PLE no KC3000 pode reduzir custo de leitura fria. **Não demonstrado:** que o SSD atual seja o gargalo dominante do decode. Primeiro medir page faults, I/O e prefill real.

## 6. Solução atual

### 6.1. Artefatos

```text
Profile:
/home/diogo/.config/model-loader/profiles/qwen3.8-flash-next-ud-iq4xs-mtp-ngram-lazy-layer-256k.json

Backend:
/home/diogo/dev/model-loader/backends/llama.cpp-qwen4exp-mtp/

Modelo principal:
/home/diogo/models/huggingface/unsloth/Qwen3.8-Flash-Next-GGUF/UD-IQ4_XS/
Qwen3.8-Flash-Next-UD-IQ4_XS-00001-of-00003.gguf

Drafter:
/home/diogo/models/huggingface/unsloth/Qwen3.8-Flash-Next-GGUF/MTP/
mtp-Qwen3.8-Flash-Next-Q8_0.gguf

Logs:
/home/diogo/.local/state/model-loader/logs/
```

- Três shards do target: 87,25 GiB no total.
- Drafter: 3,85 GiB.
- Primeiro shard: aproximadamente 10,4 MiB; não representa sozinho o modelo.
- Fork HEAD: `2c967293c2632bd0d09096406628a7e4baf97b88`.
- `git describe`: `b10791-10-g2c967293c`.
- `.built-version` corresponde ao HEAD; compilação histórica reportada como build 10801.
- PR [#28243][S4]: aberta e draft durante a pesquisa; HEAD remoto consultado correspondia ao commit local.

### 6.2. Parâmetros efetivos

```json
{
  "batch-size": 2048,
  "ubatch-size": 512,
  "threads": 16,
  "ctx-size": 262144,
  "parallel": 1,
  "cache-type-k": "q8_0",
  "cache-type-v": "q8_0",
  "cache-type-k-draft": "q8_0",
  "cache-type-v-draft": "q8_0",
  "flash-attn": "on",
  "lazy-mode": "on",
  "load-mode": "mmap",
  "n-gpu-layers": 99,
  "main-gpu": 1,
  "split-mode": "layer",
  "tensor-split": "0.5,0.5",
  "override-tensor": "per_layer_token_embd\\.weight=CPU,blk\\.([0-9]|1[0-3]|2[3-9]|3[0-7])\\.ffn_.*_exps\\.weight=CPU",
  "spec-type": "draft-mtp,ngram-mod",
  "spec-draft-n-max": 2,
  "temperature": 1,
  "top-p": 0.95,
  "top-k": 20,
  "min-p": 0,
  "repeat-penalty": 1.05,
  "repeat-last-n": 256,
  "reasoning": "on",
  "reasoning-format": "deepseek",
  "jinja": true
}
```

Esse trecho é um extrato para análise, não um profile completo importável. `threads-batch` não está explícito e usa fallback para 16. O ambiente de lançamento contém `CUDA_DEVICE_ORDER=PCI_BUS_ID`.

### 6.3. Colocação real dos experts

- PLE no backend CPU, sob demanda.
- Experts dos blocos 0–13 e 23–37 no CPU: **29 blocos**.
- Restam **19 blocos** não selecionados pelo override CPU.

A descrição persistida ainda menciona `n-cpu-moe 16`, mas essa flag não está presente. Usando apenas a estimativa de 1,11 GiB/bloco da própria descrição, os experts CPU seriam aproximadamente 32,2 GiB, não 18 GiB. Isso é estimativa de pesos, não medição de RSS.

### 6.4. O que está correto

- A PLE não disputa VRAM com os cálculos principais.
- Os pesos computacionais são distribuídos entre RAM e VRAM.
- O contexto solicitado é mantido no parâmetro de lançamento.
- Q8_0 é uma referência conservadora para KV de ferramentas/contexto longo.
- MTP tem ganho local observado em código curto.
- Ubatch 512 limita pressão de buffers, embora não esteja provado como melhor para prefill.

### 6.5. Ressalvas conceituais

- PLE lazy não significa zero uso de RAM: páginas acessadas podem ficar no page cache.
- Neste fork, tensores lazy podem provocar criação de mmap mesmo sem `load-mode=mmap`. A afirmação de que esse load-mode específico é sempre obrigatório é excessiva.
- O código local evita mlock de tensores lazy; não se deve afirmar que mlock necessariamente prenderia o arquivo inteiro nesse fork.
- O orçamento de KV deve incluir target, draft, indexer, estados GDN e buffers, não apenas uma conta simplificada do target.
- `ngram-mod` de speculative decoding não é a tabela PLE do modelo. Desativar o primeiro não elimina a segunda.
- Medições com `reasoning_effort=low` não caracterizam automaticamente o modo padrão ou `xhigh`.

## 7. Resultados históricos e suas limitações

### 7.1. Fork MTP: números confirmados nos logs

Log histórico `...-44325.log`:

| Execução | Prefill efetivamente calculado | Decode | Aceitação MTP |
|---|---|---|---:|
| Primeira | 72 tokens a 5,53 tok/s | 900 tokens a 17,85 tok/s | 52,39% |
| Repetição | 4 tokens a 30,65 tok/s | 900 tokens a 27,68 tok/s | 52,31% |
| Outra tarefa | 60 tokens a 19,94 tok/s | 900 tokens a 30,35 tok/s | 72,68% |
| Repetição | 4 tokens a 30,64 tok/s | 900 tokens a 34,32 tok/s | 64,98% |

Contexto final dos slots: 971–989 tokens, menos de 0,4% da janela configurada.

**Correção:** 30–34 tok/s de decode estão documentados. Já os números de prefill após quatro tokens novos não representam ingestão longa. Não extrapolar esses valores para 128k ou 256k.

### 7.2. Comparações anteriores da sessão

| Variante | Resultado histórico aproximado | Limitação |
|---|---|---|
| UD-IQ4_XS sem MTP, nightly | Código 20–26 tok/s; prosa 26–31 tok/s | Poucos prompts, contexto curto, backend diferente |
| UD-IQ4_XS + MTP externo, fork | Código 30–34 tok/s; prosa 26–28 tok/s | Contexto curto; prefill sem corpus longo |
| MXFP4 + NextN embutido, ik | Código aproximadamente 28–37 tok/s entre execuções; prosa aquecida 19–22 tok/s | Quant, backend e colocação diferentes; não isola efeito de MTP embutido |

No ik, `n_max=1` e `n_max=3` ficaram aproximadamente em 26–28 tok/s no pequeno teste de código; `n_max=2` foi mantido naquele experimento. Isso não prova que 2 seja ótimo em toda profundidade ou backend.

As comparações não foram uma bateria ampla, pareada, randomizada e repetida com qualidade executada. Nenhum percentual causal preciso deve ser calculado pela simples comparação dos extremos das faixas.

### 7.3. Inicialização atual com fallback

PID observado: 2229731, endpoint interno `127.0.0.1:34923`, exposto pelo proxy `127.0.0.1:4321`.

No log ativo `...-34923.log`:

```text
allocating 1745.13 MiB on device 1: cudaMalloc failed: out of memory
compute buffer allocation failed, retrying without pipeline parallelism
...
n_ctx_slot = 262144
model loaded
```

O servidor inicializou após aproximadamente 42 segundos, mas **com fallback**, não sem problemas. O log ativo examinado não continha requisições após esse carregamento.

Eliminar o fallback é hipótese de melhoria, não ganho já medido. Também é necessário testar alocações que só aparecem com prompt grande.

### 7.4. Memória do processo

Na coleta ociosa:

- RSS: aproximadamente 136,9 MiB.
- PSS: aproximadamente 134 MiB.
- Swap do processo: aproximadamente 532 MiB.
- Pico histórico de RSS: 31,47 GiB.
- VRAM atribuída ao processo: aproximadamente 39,80 GiB somando GPUs.
- Mapeamentos GGUF: aproximadamente 63,86 GiB virtuais, com RSS/PSS zero no instante consultado.
- Contadores acumulados: 149.775 major faults e 61,83 GiB de `read_bytes`, incluindo inicialização.

RSS baixo em repouso não significa modelo totalmente sem RAM nem descarregado das GPUs. Page cache global, mapeamentos residentes, memória privada e VRAM são medidas distintas. Não somar RSS e cache indiscriminadamente, pois pode haver dupla contagem.

## 8. Otimizações no GitHub e presença local

| Mudança | Estado observado | Presença no fork atual | Benefício esperado, ainda não medido localmente |
|---|---|---|---|
| #28243: MTP Flash-Next | Aberta/draft | Sim, base do fork | MTP externo funcionando |
| #28123: rollback QWEN4EXP | Incorporada | Suporte presente | Evita caminho oneroso de checkpoint completo quando utilizado |
| #28136: PLE `on-direct` | Aberta | Não | Prefill frio, concorrência de leituras |
| #28213: QSA gather | Aberta | Não | Decode em contexto profundo, com ressalva MTP |
| #28330: remover V do indexer | Aberta | Não | Reduzir VRAM desperdiçada |
| #27861: cache LRU experts | Aberta | Não avaliada como recurso local; não configurada | Reduzir leituras/computação de experts no host |
| #28223: preservar override CUDA_Host sob mmap | Aberta | Não configurada | Transferência/prefill com experts pinned |
| ik #2404: QSA gather | Incorporada | Outro backend | Melhoria de decode em profundidade |

### 8.1. PLE com leituras concorrentes — #28136

A PR [S5] adiciona `--lazy-mode on-direct`. O autor observou que benchmarks com tokens repetidos acessavam pouca diversidade de PLE e superestimavam desempenho em relação a texto real.

Resultados publicados:

- DGX Spark: aproximadamente 300 → 750–800 tok/s de prefill real.
- Máquina com oito GPUs, incluindo duas 3090 e pouca RAM: aproximadamente +37% frio e +25% aquecido em 65k tokens.
- Strix Halo: +20–32% em prefill frio; aquecido quase inalterado em outra bateria.
- Teste RX 9070 XT: ganho principal associado à concorrência de leituras; uma thread teve ganho pequeno, pool de workers reduziu fortemente o custo local da etapa.

Não transportar os percentuais para o 9950X3D/3090. O caminho deve ser comparado com o mesmo corpus e residência controlada. O nome “direct” também não é prova, sozinho, de ausência total de page cache; a semântica concreta de I/O depende da implementação.

**Prioridade alta**, porque mantém modelo e quant e mira prefill/latência de leitura.

### 8.2. QSA gather — #28213 e alternativa #28244

Na implementação discutida, a seleção top-k era convertida em máscara sobre o KV inteiro, mantendo custo de atenção crescente. Gather compacta os K/V selecionados e executa atenção sobre eles [S6].

A/B publicado com duas RTX A6000, IQ4_XS, KV Q8:

| Profundidade | Antes | Depois |
|---|---:|---:|
| 31k | 36,5 tok/s | 38,5 tok/s |
| 62k | 26,5 tok/s | 31,6 tok/s |
| 130k | 15,7 tok/s | 23,6 tok/s |

A PR alternativa #28244 estava fechada durante a pesquisa. Não presumir incorporação pelo simples estado “closed”.

**Ressalva crítica:** o caminho single-token pode não ser usado na verificação MTP, que processa draft+1 tokens. Um relato nos comentários de #28213 encontrou ganho de +19% sem especulação a aproximadamente 101k, mas diferença dentro de ruído com MTP2. Uma adaptação experimental a pequenos batches reduziu o tempo de verificação, sem sustentar soma integral dos ganhos.

Mesmo com gather, pooling/normalização/seleção do indexer podem continuar escalando com contexto. “Atenção esparsa” não implica toda a iteração constante.

No ik, #2404 [S9] reporta +29% a 128k em RTX 4070; há comentário confirmando aproximadamente +22% em duas 3090 a 128k. O autor distingue passo draft single-token e verificação; embedded MTP e multi-GPU não estavam cobertos pela validação original.

### 8.3. V-cache do indexer — #28330

A PR [S7] identifica que o indexer usa somente chaves, mas a estrutura genérica aloca K e V.

Inspeção local:

- `src/llama-memory-hybrid-idx.cpp:49–63`: construção de `llama_kv_cache` com `type_k, type_v`, prefixo `idx_`.
- `src/llama-kv-cache.cpp:230–237`: criação de V quando `!is_mla`.
- `src/models/qwen4exp.cpp:818–826`: gravação/leitura de K no caminho examinado.

Há oportunidade concreta de economia de VRAM. Não foi calculada a economia exata do target+draft por GPU nem demonstrado que ela eliminará o fallback de 1745 MiB.

**Prioridade alta** por tratar de margem de memória sem reduzir contexto ou precisão dos valores utilizados.

### 8.4. Cache LRU de experts — #27861

A PR [S8], descoberta também por relato no Reddit [S15], mantém em VRAM cópias dos experts usados recentemente em camadas host-resident.

Medição publicada: duas 3090, UD-Q4_K_XL, 28 camadas de experts no host:

- 18,4 → 24,2 tok/s, aproximadamente +31%.
- 48 slots/camada, aproximadamente 4,1 GiB de VRAM.
- Limite de duas inserções por camada/etapa.

A análise de roteamento relatou pouca utilidade de uma lista estática global de experts populares, mas forte localidade temporal. Isso contradiz a simplificação “basta fixar os experts mais frequentes para sempre”.

Limitações:

- Os pesos originais permanecem no host; o cache não reduz automaticamente a necessidade de RAM.
- A descrição da PR usa `n_tokens == 1`: verificação mult-token MTP passa pelo caminho sem cache.
- Extensões experimentais nos comentários não equivalem a suporte consolidado para este modelo.
- A GPU1 atual não tem espaço para simplesmente acrescentar vários GiB de cache. Seria preciso redistribuir orçamento e medir.
- Colocar todos os experts do quant no host pode ultrapassar a RAM disponível; estratégias parciais precisam de orçamento próprio.

**Prioridade média/alta**, como alternativa controlada, não ganho cumulativo garantido sobre MTP.

### 8.5. Experts pinned sob mmap — #28223

A PR [S10] preserva overrides explícitos para `CUDA_Host`, que o caminho normal pode rejeitar ou substituir por CPU pageable sob mmap.

Medição em duas 3090, dual Xeon e 188 GiB DDR4, Q6 com 40 camadas host-resident:

- Prefill 26k frio: 166 → 330 tok/s.
- PLE aquecida no braço pinned: 379 tok/s.
- Decode curto: praticamente sem ganho.
- Custo: aproximadamente 89,6 GB pinned nesse teste e carregamento mais longo, reduzido por uma correção adicional.

O princípio pode ser útil, mas a receita publicada não cabe diretamente em 64 GB. Pinning deve respeitar RAM disponível, cópia temporária no carregamento e espaço para PLE/OS. Não desligar mmap nem fixar toda a PLE por tentativa indiscriminada.

### 8.6. Rollback e checkpoints

O suporte QWEN4EXP em `llm_arch_supports_rs_rollback()` já existe no fork, em `src/llama-arch.cpp:1107–1118`. Portanto, não atribuir um ganho futuro à simples inclusão dessa arquitetura na whitelist.

A PR #28118 [S11] propunha checkpoints on-device. Comentários relatam aborts em restore e recomendam rollback nativo quando possível; #28123 foi incorporada. Não aplicar um patch de checkpoint apenas porque anuncia grande ganho em Strix Halo.

### 8.7. Correção numérica GDN — #28068

A PR [S12] discute normalização `x * rsqrt(sum(x²)+eps)` versus divisão por `max(sqrt(sum(x²)),eps)`. Há debate sobre magnitude e relatos de mudanças de comportamento em geração longa.

Não é evidência de ganho de velocidade nem prova de defeito comportamental local. É motivo para incluir validação de qualidade/correção quando comparar builds, em vez de escolher exclusivamente por tok/s.

## 9. Alternativas de backend e quant

### 9.1. ik-llama não foi esgotado pelo teste MXFP4

O teste anterior usou jamesrogers MXFP4 com NextN embutido, aproximadamente 118 GiB. Foi removido por escolha do operador após a comparação.

Isso não representa todos os caminhos ik. O card ji-farthing [S13] apresenta:

| Quant | Total | Experts | PLE | Outros densos |
|---|---:|---:|---:|---:|
| IQ3_KT | 80,22 GiB | 50,39 GiB | 26,84 GiB | 2,86 GiB |
| IQ4_KT | 88,42 GiB | 58,59 GiB | 26,84 GiB | 2,86 GiB |

Drafter IQ4_KT: 2,21 GiB. O publicador relata cerca de 20 tok/s com RTX 4070 12 GB, 64 GB RAM, i7-11700K e 128k configurados, além de avaliações PPL/KLD contra BF16.

São quants mistos, com tipos diferentes por tensor. IQ3_KT reduz precisão dos experts gate/up e não deve ser declarado equivalente em qualidade ao UD-IQ4_XS sem teste. Mesmo IQ4_KT exige comparação real.

O ik já incorporou QSA gather. Pode ser candidato de contexto longo usando quant/drafter menores, não necessariamente o MXFP4 descartado.

### 9.2. TabbyAPI / ExLlamaV3

Há suporte real ao modelo e quants 2.05/3.05/4.05/5.05/6.05 bpw [S14]. O card consultado exige ExLlamaV3 1.4.5 ou dev.

Na discussão de 3090 + 64 GB [S16]:

- Autor usa Ryzen 7 7800X3D, DDR5 e MTP.
- Relata aproximadamente 30 tok/s, KV FP16 e contexto em torno de 200k.
- Outros usuários relatam desempenho significativamente inferior.
- Há críticas à repetição de texto no benchmark e discrepâncias de contagem entre ferramenta e logs.
- O próprio tópico distingue AVX-512 de CPUs AVX2; inferências sobre cache L3 feitas por modelos nos comentários não são experimentos controlados.

Issue #326 [S17]: duas 3090 + Threadripper 1950X + 64 GB, desempenho baixo/instável com offload. CPU e quant maiores impedem comparação direta com o 9950X3D.

PR #331 [S18]: otimização AVX2 com resultado 30,6 → 41 tok/s em 2.05 bpw, 3090, Ryzen 5900X e 128 GB. Não é resultado de 4-bit, nem de AVX-512, nem evidência de 256k ocupados.

**Veredito:** alternativa séria para A/B; não vencedora comprovada. Não extrapolar benchmarks all-GPU em RTX PRO 6000 para offload em duas 3090.

### 9.3. FreeToken

FreeToken possui cache de experts e suporte PLE em disco [S19–S20]. A PR #311 foi incorporada e usa leituras concorrentes, staging limitado e sobreposição com execução GPU.

Resultados publicados de PLE disk versus pinned:

- H100: aproximadamente −2,6% decode.
- RTX PRO 6000 Blackwell limitada a 32 GiB: diferença próxima de zero em decode.

O obstáculo local é memória: a receita NVFP4 discutida mantém **63,46 GiB de banco de experts pinned**, além do runtime. O sistema disponibiliza apenas 60,48 GiB totais. Retirar a PLE da RAM não resolve esse restante.

Uma variante com bancos de experts em disco, como a documentação SparkLab/FTW, é outra estratégia e tem custos próprios; não foi encontrada demonstração equivalente em SM86, duas 3090, 64 GB e contexto alvo.

A PR #385 [S21] oferece TP2 para qwen4_exp, mas estava aberta e foi medida em duas RTX 6000 Ada de 48 GiB com aproximadamente 503 GiB RAM. Não é uma receita diretamente equivalente.

**Veredito:** promissor para pesquisa futura ou maior RAM; não substituição comprovada neste hardware.

### 9.4. SGLang/vLLM AWQ e SSD Stream

A tentativa local histórica de SGLang SSD Stream apresentou OOM de host/pinned e GPU; foi removida. Isso descarta aquela composição testada, não todo backend futuro possível.

As receitas Ampere encontradas usam quatro ou oito RTX 3090. A receita AWQ de quatro GPUs [S22] inclui validação próxima de 250k e correções específicas de QSA/GDN, mas exige 96 GB de VRAM. A receita oficial vLLM [S23] também não documenta a equivalência procurada.

SSD Stream resolve PLE, não automaticamente memória/transferência dos experts [S24]. NVFP4/MXFP4 podem ser formatos de armazenamento utilizáveis com kernels de dequantização, mas a RTX 3090 não possui tensor cores nativos FP4/FP8. Benchmark Blackwell não se transfere por semelhança do nome da quantização.

**Veredito:** não encontrei receita publicamente demonstrada de melhor desempenho para duas 3090 + 64 GB + 256k. Não há justificativa para repetir o download anterior sem resolver primeiro orçamento e caminho SM86.

### 9.5. KTransformers e outros caminhos

A issue #2179 [S25] pede suporte `qwen4_exp`, apesar de menções genéricas no card oficial. Não confundir recomendação ampla de framework com integração específica disponível e testada.

Relatos R9V/RDNA4 e forks Strix Halo mostram potencial de kernels/QSA/PLE, mas são específicos de outro hardware. São inspiração de engenharia, não executáveis CUDA substitutos.

## 10. Evidências do Reddit

### 10.1. Duas 3090 com cache de experts

Post [S15]: dual Xeon E5-2696 v4, 192 GB DDR4, Q6, aproximadamente 261k configurados:

- Antes: aproximadamente 17 tok/s curto, 12 tok/s a 131k.
- Depois: aproximadamente 25–29 tok/s curto/intermediário, 17 tok/s a 131k.
- Uso do cache LRU #27861, discussão de pinned #28223.

O autor explica que cache reduz tráfego por token, não tamanho do banco host. Não copiar o layout de todos os experts no host para 64 GB. Comentários sugerindo que quatro DIMMs em AM5 aumentariam canais/banda não foram adotados: AM5 continua dual-channel e mais DIMMs podem reduzir frequência suportada.

### 10.2. Uma 3090 e 64 GB

Post [S26]: 3090, Ryzen 3950X, PCIe 3.0, IQ4_XS, experts RAM, PLE disco, aproximadamente 160 tok/s prefill e 16 tok/s decode. MTP foi descrito como mais lento mesmo com alta aceitação. A comparação é limitada por CPU, quant de KV e workload.

Isso mostra que alta aceitação MTP, sozinha, não prova ganho. É preciso incluir custo da proposta, verificação, rollback e movimentação de memória.

### 10.3. Falhas metodológicas recorrentes

- “256k context” significando somente parâmetro de inicialização.
- Tokens acumulados de várias chamadas apresentados como contexto ativo.
- Throughput agregado confundido com uma sequência.
- Prefill repetitivo que não exercita diversidade da PLE.
- Comparação de Flash-Next com números do 27B.
- Alegações de ganho de patch usando builds-base diferentes.
- Confusão entre PLE e lookup n-gram especulativo.

O post em r/ollama [S27], por exemplo, sugere `--lookup-cache-dynamic` como offload da tabela n-gram do modelo. Essa associação está incorreta: é cache de speculative decoding, não realocação da PLE.

### 10.4. Qualidade versus velocidade

Comentários discordam sobre a vantagem do Flash-Next frente ao 27B: alguns preferem precisão/menor número de passos; outros preferem throughput e concorrência do 27B. Não houve avaliação local suficiente para decidir tempo até tarefa correta entre os dois.

Este relatório mantém o modelo solicitado; trocar para 27B seria outra decisão, não otimização do mesmo modelo.

## 11. Matriz de decisão

| Caminho | Viabilidade atual | Evidência de melhor desempenho local | Prioridade |
|---|---|---|---|
| Profile atual | Inicialização e decode curto demonstrados | Referência, não ótimo | Preservar |
| Remover V indexer e rever margem GPU1 | Patch requer build/testes | Ainda não medida | Alta |
| PLE on-direct | Patch público, mesmo modelo | Ainda não medida | Alta |
| QSA gather + comparação MTP | Patch público; interação MTP precisa validação | Ainda não medida | Alta para contexto longo |
| Threads/afinidade/ubatch | Sem novos pesos | Ainda não medida | Média/alta |
| Cache LRU experts | Exige orçamento/patch e interação MTP | Ganho publicado em duas 3090, outra RAM | Média/alta |
| Experts pinned parciais | Exige orçamento e patch | Prefill melhor em outro host | Média |
| ik com quant/draft menor | Novos artefatos e teste | Não estabelecida | Segunda etapa |
| EXL3 | Suporte real, relatos contraditórios | Não estabelecida | Segunda etapa |
| FreeToken NVFP4 receita padrão | Banco pinned excede RAM utilizável | Não | Baixa sem mudança de estratégia |
| SGLang/vLLM receita encontrada | Hardware publicado maior | Não | Baixa |
| KTransformers | Integração específica não demonstrada | Não | Acompanhar |

## 12. Plano recomendado de validação

Este é um plano, não trabalho executado. Builds experimentais devem ficar separados do backend de produção. Não adicionar flags inexistentes ao profile atual.

### Etapa 1 — Baseline representativo

- Manter 262.144 tokens configurados.
- Medir profundidades reais de 8k, 32k, 64k, 128k e 240–250k, reservando saída.
- Usar corpus diverso de código/documentação e tarefas em português.
- Evitar um único texto curto repetido para preencher contexto.
- Separar: leitura fria de pesos/PLE; page cache aquecido sem prefixo reutilizado; prefixo já computado.
- Repetir medições e alternar ordem dos braços para controlar aquecimento.
- Medir TTFT até primeiro evento e até primeiro conteúdo útil, prefill de tokens novos, decode, latência total e contagem de raciocínio/saída.
- Registrar aceitação e comprimento médio de draft, VRAM por GPU, RAM disponível, RSS/PSS, major faults, I/O, swap, CPU, potência/temperatura.
- Validar tool calls, recuperação de fatos distribuídos e código com testes executados em sandbox.

### Etapa 2 — Margem de VRAM

- Testar remoção do V-cache inútil do indexer.
- Investigar colocação do drafter, buffers e experts mantendo split igual como controle.
- Confirmar se a inicialização deixa de cair no fallback.
- Executar prefill longo: inicialização não cobre picos posteriores.
- Não reduzir contexto silenciosamente para declarar sucesso.

### Etapa 3 — PLE e armazenamento

- Comparar `lazy-mode on` versus `on-direct` em build experimental.
- Mesmo target, draft, threads e corpus.
- Verificar I/O/faults e ganho end-to-end, não só etapa isolada.
- Só então considerar cópia controlada para KC3000 se houver espaço; não mover/apagar pesos de produção sem autorização.

### Etapa 4 — Especulação e QSA

Comparar, no mesmo build e contexto:

1. Sem especulação.
2. MTP.
3. MTP + ngram.
4. QSA otimizada em cada braço aplicável.

Verificar em logs/profiling se gather realmente é executado nas etapas de draft/verify. Não somar percentuais de PRs. Qualidade e estabilidade em várias requisições são obrigatórias.

### Etapa 5 — CPU, buffers e experts

- Sweep pequeno de threads 8/12/16 e threads-batch independentes.
- Afinidade por núcleos físicos e por CCD como experimentos, não pressupostos.
- Ubatch 256/512/1024 apenas com orçamento de VRAM.
- Cache LRU com banco host parcial e limite de inserções; verificar competição com MTP.
- Pinned parcial somente se a soma de alocações/OS/cache couber com margem.

### Etapa 6 — Outro backend/quant

- Comparar ik IQ4_KT/MTP menor ou EXL3 em profundidades iguais.
- Distinguir perda de qualidade da quantização e diferenças do runtime.
- Só promover se melhorar o objetivo real: tempo para tarefa correta, latência interativa e estabilidade, não pico isolado de tok/s.
- Preservar rollback para o profile atual.

## 13. Hardware: comprar algo agora?

Não há evidência suficiente para recomendar nova GPU, NVLink ou mais potência como primeira ação.

Mais RAM pode facilitar bancos host maiores, reduzir pressão de cache e viabilizar FreeToken/quant superior. Porém **mais capacidade não corrige automaticamente a banda da RAM, kernels QSA ou sincronização**. Uma expansão deve considerar frequência dos DIMMs, dois versus quatro módulos e capacidade necessária; não presumir que quatro módulos criem quatro canais no AM5.

Antes de compra, as oportunidades de software identificadas são mais diretamente testáveis. O SSD KC3000 já existente também permite comparação sem adquirir hardware, sujeito a espaço e execução autorizada.

## 14. Correções do histórico e estado preservado

- A escolha final foi manter apenas o profile **Flash-Next** com o fork MTP, não remover todos os profiles de outras famílias.
- Os profiles Flash-Next sem MTP e ik MXFP4 foram removidos anteriormente.
- O diretório jamesrogers e o MTP shared não utilizado foram removidos anteriormente, com aproximadamente 119 GiB + 2,6 GiB de arquivos reportados por `du` naquela operação. Isso não é nova limpeza realizada nesta pesquisa.
- O fork atual tem MTP funcional, mas continua ligado a PR draft.
- “256k rápido” não foi demonstrado.
- “Prefill warm 30–34 tok/s” não é um benchmark de prompt longo: as repetições mediram quatro tokens novos.
- A descrição do profile precisa ser reconciliada com 29 blocos CPU e com as ressalvas do benchmark. Não foi editada neste trabalho.
- A comparação ik versus fork não isolou backend, quantização e MTP: não permite concluir que MTP externo seja intrinsecamente mais rápido que embutido.
- O relatório anterior da sincronização disse que cada agente recebeu dois profiles elegíveis; o correto é **11 profiles elegíveis no catálogo, dos quais dois foram adicionados**. Os demais nove foram preservados. Os dois profiles de 32k ficaram fora do critério de contexto do skill.
- A inspeção posterior do JSON da sincronização confirmou arrays `changes.added/removed/updated` vazios para o estado pós-apply. A consulta inicial ao JSON havia usado campos incorretos; o esquema real usa `results[].changes`.

## 15. Conclusão

**Viabilidade:** demonstrada para inicialização e inferência curta.

**Desempenho de código curto aquecido:** aproximadamente 30–34 tok/s documentados.

**Qualidade, TTFT e decode com 256k ocupados:** não demonstrados.

**Melhor estratégia disponível:** não demonstrada.

**Direção recomendada:** manter GGUF + PLE sob demanda + distribuição RAM/VRAM como base, melhorar orçamento da GPU1 e experimentar PLE concorrente/QSA/indexer antes de migrar. Cache de experts e EXL3/ik são comparadores importantes, não vencedores estabelecidos.

O erro não foi escolher essa arquitetura geral. Foi encerrar a calibração cedo demais e generalizar um teste curto para o requisito de contexto longo.

## 16. Fontes

Os links abaixo são evidências consultadas ou fontes técnicas discutidas na pesquisa; números de terceiros permanecem alegações dos respectivos autores, não reproduções locais.

- **S1 — Qwen, card oficial:** https://huggingface.co/Qwen/Qwen3.8-Flash-Next
- **S2 — AMD 9950X3D:** https://www.amd.com/en/products/processors/desktops/ryzen/9000-series/amd-ryzen-9-9950x3d.html
- **S3 — NVIDIA RTX 3090:** https://www.nvidia.com/en-eu/geforce/graphics-cards/30-series/rtx-3090/
- **S4 — llama.cpp MTP Flash-Next #28243:** https://github.com/ggml-org/llama.cpp/pull/28243
- **S5 — PLE on-direct #28136:** https://github.com/ggml-org/llama.cpp/pull/28136
- **S6 — QSA gather #28213:** https://github.com/ggml-org/llama.cpp/pull/28213
- **S7 — V-cache indexer #28330:** https://github.com/ggml-org/llama.cpp/pull/28330
- **S8 — Cache LRU experts #27861:** https://github.com/ggml-org/llama.cpp/pull/27861
- **S9 — ik QSA gather #2404:** https://github.com/ikawrakow/ik_llama.cpp/pull/2404
- **S10 — CUDA_Host/mmap #28223:** https://github.com/ggml-org/llama.cpp/pull/28223
- **S11 — Checkpoints on-device #28118:** https://github.com/ggml-org/llama.cpp/pull/28118
- **S12 — Normalização GDN #28068:** https://github.com/ggml-org/llama.cpp/pull/28068
- **S13 — ji-farthing quants ik:** https://huggingface.co/ji-farthing/Qwen3.8-Flash-Next-ik-llama-GGUF
- **S14 — turboderp EXL3:** https://huggingface.co/turboderp/Qwen3.8-Flash-Next-exl3
- **S15 — Reddit, cache em duas 3090:** https://www.reddit.com/r/LocalLLaMA/comments/1w5vjp6/
- **S16 — EXL3 em 3090 + 64 GB:** https://huggingface.co/turboderp/Qwen3.8-Flash-Next-exl3/discussions/2
- **S17 — ExLlamaV3 offload lento #326:** https://github.com/turboderp-org/exllamav3/issues/326
- **S18 — ExLlamaV3 AVX2 #331:** https://github.com/turboderp-org/exllamav3/pull/331
- **S19 — FreeToken suporte/memória #214:** https://github.com/FlashML-org/FreeToken/issues/214
- **S20 — FreeToken PLE disk #311:** https://github.com/FlashML-org/FreeToken/pull/311
- **S21 — FreeToken TP2 #385:** https://github.com/FlashML-org/FreeToken/pull/385
- **S22 — AWQ em quatro 3090:** https://github.com/alesha-pro/qwen38-flash-next-4x3090
- **S23 — Receita vLLM:** https://recipes.vllm.ai/Qwen/Qwen3.8-Flash-Next
- **S24 — SSD Stream NVFP4:** https://huggingface.co/garnermccloud/Qwen3.8-Flash-Next-NVFP4-SSD-Stream
- **S25 — KTransformers suporte solicitado #2179:** https://github.com/kvcache-ai/ktransformers/issues/2179
- **S26 — Reddit 3090 + 64 GB:** https://www.reddit.com/r/LocalLLaMA/comments/1w0u24k/
- **S27 — Reddit, exemplo de confusão PLE/spec:** https://www.reddit.com/r/ollama/comments/1vzip5o/
- **S28 — Rollback incorporado #28123:** https://github.com/ggml-org/llama.cpp/pull/28123
- **S29 — QSA alternativa #28244:** https://github.com/ggml-org/llama.cpp/pull/28244
- **S30 — Indexer issue #28296:** https://github.com/ggml-org/llama.cpp/issues/28296
- **S31 — HF, três 3090 e perda em contexto profundo:** https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/40
- **S32 — HF, Strix Halo e QSA/MTP:** https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/45
- **S33 — peaster, duas 3090 + 128 GB:** https://peaster.io/articles/local-LLMs/qwen-3-8-flash-next-first-impressions
- **S34 — AtomicChat, layout/quantização:** https://atomic.chat/blog/guides/how-to-run-qwen-3-8-flash-next-locally
- **S35 — Análise M1 Max, detalhes dos quants mistos:** https://lilting.ch/en/articles/qwen38-flash-next-llamacpp-m1max-test
- **S36 — Unsloth guia:** https://unsloth.ai/docs/models/qwen3.8-next
- **S37 — Reddit requisitos mínimos:** https://www.reddit.com/r/LocalLLaMA/comments/1vzpt61/
- **S38 — Reddit PLE/mmap/layout:** https://www.reddit.com/r/LocalLLM/comments/1vz927j/
- **S39 — Reddit megathread:** https://www.reddit.com/r/LocalLLaMA/comments/1vyq2v4/
- **S40 — Reddit Strix Halo MTP:** https://www.reddit.com/r/StrixHalo/comments/1w0z7n1/
- **S41 — Reddit R9V, hardware RDNA4:** https://www.reddit.com/r/LocalLLaMA/comments/1w2z5qw/
- **S42 — FreeToken FTW/SparkLab:** https://huggingface.co/oakmindai/Qwen3.8-Flash-Next-NVFP4-FTW

[S4]: https://github.com/ggml-org/llama.cpp/pull/28243

## 17. Artefatos locais da pesquisa

Arquivos temporários da coleta, sujeitos a limpeza de `/tmp`:

```text
/tmp/qwen-strategy-research/
  pr-28213.json
  pr-28136.json
  pr-28330.json
  pr-28118.json
  pr-28068.json
  pr-27861.json
  pr-27861-comments.txt
  exl-326.json
  exl-286.json
  freetoken-readme.md
  reddit-3090.json
  reddit-exl3.json
  reddit-minspec.json
  reddit-ple.json
  reddit-mtp.json
  reddit-expertcache.json
  reddit-dual64.json
  reddit-single64.json
```

Transcrição da auditoria local:

```text
/tmp/pi-subagents-1000/home-diogo-dev-model-loader/
01a06d9b-d4d2-738b-9d90-eaf00e5f483d/tasks/bcab33dc-1a3d-478.output
```

Esses arquivos não são necessários para entender as conclusões deste relatório. Podem conter resultados longos ou comentários não usados como evidência. As referências públicas e fatos relevantes foram consolidados acima.
