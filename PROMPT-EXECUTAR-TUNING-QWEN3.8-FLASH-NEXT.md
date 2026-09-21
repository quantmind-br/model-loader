# Prompt para agente: executar toda a calibração do Qwen3.8-Flash-Next

Copie o conteúdo a partir de **Missão** para uma nova sessão com acesso à workstation e ao repositório. Este documento transfere trabalho; não afirma que os testes pendentes foram executados.

## Missão

Você é o agente responsável por **executar os testes previstos e buscar a melhor performance útil possível do Qwen3.8-Flash-Next nesta workstation**, sem sacrificar silenciosamente qualidade, ferramentas ou contexto. Trabalhe até concluir os experimentos viáveis, comparar os resultados, promover somente configurações aprovadas e salvar um relatório Markdown na raiz do repositório.

Repositório: `/home/diogo/dev/model-loader`.

Leia primeiro:

1. As instruções do repositório e do operador que estiverem carregadas na sessão.
2. `QWEN3.8-FLASH-NEXT-OTIMIZACAO-DUAL-RTX3090.md`: pesquisa, fontes, arquitetura e plano E0–E11.
3. `docs/reports/qwen-flash-tuning-results.md`.
4. `docs/reports/qwen-flash-tuning-artifacts/README.md`.
5. A skill `rtx3090-inference-profiles`, seu workflow `workflows/full-tuning.md` e referências `dual-gpu.md`, `model-research.md`, `exllama-tabby.md`, `llama-family.md`, `ik-llama.md` e `speculative.md`, conforme o caminho executado.
6. Skills de debugging, verificação e desenvolvimento aplicáveis antes de modificar código.

Não refaça toda a pesquisa bibliográfica. Confirme apenas fatos que afetem a execução atual, versões, schemas, formatos e hipóteses que os testes precisarem resolver.

## Resultado obrigatório

- Medições novas, com respostas brutas, comandos, logs e telemetria reproduzíveis.
- A/B dos finalistas existentes, comparação entre SSDs, EXL3 conservador e calibrado, cache C++/MTP correto e otimizações do gargalo observado.
- Avaliação de todos os experimentos E0–E11 abaixo. Nenhum desaparece do relatório: resultado medido, rejeição demonstrada, dependência ou bloqueio explícito.
- Profiles vencedores separados por workload quando necessário; nunca um vencedor universal inventado.
- Para configurações promovidas: qualidade, ferramentas, contexto longo e memória verificados em execução real.
- Relatório na raiz: `QWEN3.8-FLASH-NEXT-RESULTADOS-EXPERIMENTAIS.md`.
- Evidências duráveis, scripts úteis, patches e manifesto em `docs/reports/qwen-flash-perf-artifacts/`; scratch em `.pi/`. Não deixar a única evidência importante em `/tmp`, transcrição de agente ou diretório externo.
- Não declarar “melhor performance possível” como ótimo matemático global. Declarar **melhor configuração observada na matriz executada**, com fronteiras da busca e alternativas rejeitadas.

## Hardware e limites

Estado inventariado anteriormente; reconfirme no início:

- Ryzen 9 9950X3D, 16 núcleos físicos/32 threads, AVX-512, L3 em domínios de 96 MiB + 32 MiB.
- 64 GB RAM instalada, aproximadamente 60,48 GiB utilizáveis pelo Linux.
- Duas RTX 3090, 24 GiB cada, SM86, sem NVLink ativo; topologia PHB.
- GPU0 serve desktop. Respeitar **até 23 GiB por GPU e 46 GiB agregados**, incluindo atividade externa observada; não confundir reserva do backend com consumo total.
- Split de layers/tensores **simétrico**; llama `tensor-split=0.5,0.5`. Não compensar desktop com split assimétrico.
- Não alterar clocks, potência, firmware, ACS/IOMMU, driver ou configuração global de memória.
- Não usar `drop_caches` global nem desligar swap numa workstation ativa.
- Não reduzir KV abaixo de Q8/8,8 para perfis de ferramentas.
- Não usar quant abaixo de 4 bits, poda/REAP, redução de top-k de roteamento ou redução de contexto como ganho sem obter aprovação específica do operador para mudar o eixo de qualidade/capacidade.
- Contexto alvo: **262144 configurados**, com casos efetivamente ocupados até aproximadamente 244k. Diagnóstico pode começar em contexto pequeno; isso não satisfaz o gate final.
- O modelo deve continuar sendo **Qwen3.8-Flash-Next**. O Qwen3.8-27B/Syv não é substituto desta tarefa.

Inicie servidores somente pelo `model-loader instance start <profile-id>`. Requisições devem passar pelo proxy gerenciado. Testes de kernels/operações podem executar seus próprios binários de teste, mas não lançar o servidor de inferência à mão.

## Primeiro resolver exclusividade operacional

Uma rodada anterior sofreu interferência de outro cliente:

- Proxy `127.0.0.1:4321`, single-active.
- Um processo OMP, PID histórico `3980684`, estava conectado ao proxy.
- Durante teste do Flash, apareceu uma carga de `qwen3-embedding-0.6b-32k`; o Flash foi encerrado e recarregado com novo PID.
- Houve HTTP 502/EOF no harness. Isso invalida afirmações de estabilidade e taxa de sucesso dessa rodada.

**PIDs são históricos.** Use `/_status`, `model-loader instance list --json`, `ss -tnp` e logs atuais para identificar clientes/instâncias. Não suspenda, mate ou reconfigure outros clientes sem autorização do operador. A sessão anterior perguntou sobre exclusividade, e o operador escolheu salvar este prompt em vez de autorizar suspensão.

Antes de throughput ou testes longos, obtenha uma janela sem indexação/embeddings/requisições externas que causem swap. Um segundo proxy só ajuda se também houver isolamento real de recursos/registro; não presuma que duas portas tornam as mesmas GPUs exclusivas. Não execute dois modelos grandes simultaneamente.

Enquanto aguarda exclusividade, pode inspecionar arquivos e preparar código isolado. Não medir desempenho com downloads, cópias grandes, compilação ou outro teste disputando SSD/CPU/RAM/GPU. Qualquer mudança de PID durante um braço precisa de explicação e classificação explícita da amostra.

## Estado já preparado

### Relatório e evidências históricas

Finalistas originais preservados:

1. `qwen3.8-flash-next-ud-iq4xs-cache96-physical-nothink-256k`
   - Backend ID `llama-cpp-qwen4exp-cache`.
   - UD-IQ4_XS, PLE concorrente, K-only indexer, QSA gather, cache96, ubatch512, 16 físicos, Q8 KV, MTP/thinking off.
2. `qwen3.8-flash-next-iq4kt-ik-fifo-ub1024-256k`
   - Backend ID `ik-llama-cpp-tuning`.
   - IQ4_KT, PLE diferida, FIFO, ubatch1024, 16 threads, Q8 KV, MTP/thinking off, correção de EOS/bias entre requests.

Referência histórica de decode (não medições novas):

| Tokens reais | Cache96 tok/s | ik FIFO tok/s |
|---:|---:|---:|
| 12592 | 38,69 | 32,56 |
| 62291 | 31,36 | 29,86 |
| 124651 | 25,38 | 27,12 |
| 224101 | 19,82 | 24,07 |

Em 244116 tokens, prefill histórico cache96 ~154 tok/s e ik ~290,58 tok/s. A leitura de três fatos passou; isso não é prova geral de qualidade longa.

Não repetir como descoberta: cache96+ubatch1024 já falhou por OOM; MTP2 no ik já regrediu em corpus anterior; QSA/PLE/cache64/96/threads/pinned parcial foram examinados. Novos testes devem responder uma dúvida específica ou usar uma revisão/hipótese diferente.

### Diretório da rodada iniciada

`.pi/qwen-flash-perf-20260908/` contém:

- `initial-status.json`: estado antes da intervenção.
- `original-profiles/`: cópias dos profiles Flash e do modelo inicialmente ativo.
- `capture.py`: wrapper de comando que salva stdout/stderr, status e telemetria por GPU, `/proc/meminfo`, swap/faults e I/O do processo.
- `probe.py`: probe SSE uniforme com corpus real do repositório, warmup e repetições; ainda não executado nesta rodada.
- `quality.mjs` e `code-quality.mjs`: cópias dos testes existentes com destino de saída alterado para não sobrescrever histórico.
- `ple-io.py`: microbenchmark ABBA de linhas PLE reais, `pread` concorrente, checksums e latências; preparado, **não executado**.
- `ple-layout.json`: offset, formato e dimensão reais da PLE do GGUF.
- `ssd-profile.json`: variante SSD.
- `cache96-integrity/`: gate novo bem-sucedido.
- `cache96-auto/`: gate novo inconclusivo/falho, com respostas e telemetria.
- `cache-mtp-base-revision.json`, `cache-mtp-manifest.json`, `cache-mtp-patch.diff`: saída do agente de preparação do patch, **não validada**.

Modelo inicialmente ativo, conforme snapshot: `qwen3.8-27b-w4a16-dflash2-syv-tp2-sharptmpl-256k`. Reconfirme o estado atual; não restaure cegamente por esse nome se o operador já mudou o ambiente.

### Testes realmente executados na rodada iniciada

**Integridade cache96:**

```bash
python3 .pi/qwen-flash-perf-20260908/capture.py --label cache96-integrity -- \
  python3 .claude/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py \
  --model qwen3.8-flash-next-ud-iq4xs-cache96-physical-nothink-256k \
  --mode integrity --stream --continuation --timeout 600
```

Saiu 0: três chamadas forçadas não-streaming, uma streaming e continuação limpas. Isso não prova seleção autônoma nem estabilidade de sessão longa.

**Seleção autônoma cache96:** 30 positivos + 30 controles, temperature1/top_p0.95/top_k20. Saiu **3**:

- Positivos: 29 chamadas corretas e **1 `malformed_call`**.
- Controles: 29 no-call corretos e **1 HTTP502/EOF**.
- O comando solicitado foi alterado pelo modelo, que acrescentou `; echo "exit_status=$?"` ao final. O teste exige bytes exatos.
- O erro HTTP ocorreu na rodada com troca externa do modelo; não atribuir automaticamente o comando alterado à mesma interferência.
- Decisão do harness: **inconclusive** por erro de infraestrutura. Corrigir ambiente e reexecutar; preservar também a falha semântica para diagnóstico.

Não enfraquecer o teste nem aceitar o acréscimo de comando só para obter PASS.

### SSD alternativo já preparado

Target GGUF copiado para:

`/home/diogo/model-loader-experiments/ssd-kc3000/UD-IQ4_XS/`

Origem:

`/home/diogo/models/huggingface/unsloth/Qwen3.8-Flash-Next-GGUF/UD-IQ4_XS/`

Foram copiados os três shards. `cmp` dos shards grandes 2 e 3 terminou sem diferenças. Conferir também shard1 e registrar manifesto completo antes de usar como prova de igualdade de todo o target. A cópia e os `cmp` aqueceram páginas; nenhum estado cold-disk foi garantido.

Profile criado, mas **não carregado/medido**:

`qwen3.8-flash-next-ud-iq4xs-cache96-kc3000-256k`

Mesmo backend e args de cache96, mudando apenas caminho do modelo e identidade/descrição. Nenhum profile original foi intencionalmente alterado.

PLE inspecionada no shard2: `per_layer_token_embd.weight`, shape `[160, 320001536]`, IQ4_NL, 28800138240 bytes, offset528499744. O `on-direct` do fork é `pread` **bufferizado**, não `O_DIRECT` do Linux.

### EXL3 já baixado

Diretório:

`/home/diogo/models/huggingface/turboderp/Qwen3.8-Flash-Next-exl3-4.05bpw_h6_ng6`

Repo: `turboderp/Qwen3.8-Flash-Next-exl3`.

Revision: `55a732e0c4c3d4614bc42b68493bb930d9b02c0a` (branch `4.05bpw_h6_ng6`).

O comando terminou com sucesso e os nove shards de modelo, tabela ngram, index e config estão presentes. **Ainda verificar integridade/completude da revisão e tensores MTP**, não apenas nomes/arquivos não vazios.

`hf` não existe no PATH global. Foi usado:

```bash
env HF_HUB_DISABLE_XET=1 HF_HUB_DOWNLOAD_TIMEOUT=30 HF_HUB_ETAG_TIMEOUT=30 \
  backends/tabby/.venv/bin/hf download turboderp/Qwen3.8-Flash-Next-exl3 \
  --revision 55a732e0c4c3d4614bc42b68493bb930d9b02c0a \
  --local-dir /home/diogo/models/huggingface/turboderp/Qwen3.8-Flash-Next-exl3-4.05bpw_h6_ng6 \
  --max-workers 4
```

Não baixar outra vez sem conferir cache e manifesto. O volume de modelos tinha ~139,7 GiB livres **antes** do download de ~100 GiB. Recalcular espaço, inclusive arquivos temporários, antes de qualquer outro checkpoint.

### Código experimental cache/MTP

Cópia isolada:

`/home/diogo/model-loader-experiments/qwen-flash-cache-mtp`

Base original:

`backends/llama.cpp-qwen4exp-cache`, commit `2c967293c2632bd0d09096406628a7e4baf97b88`, mais mudanças locais/untracked da calibração anterior.

O agente preparou código para slots zero distintos, remapeamento `[top_k,n_tokens]` e opção de verify pequeno. **Não houve build, teste numérico ou execução GPU.** Diretório build estava ausente no handoff.

Não aceitar as afirmações do manifesto como verificação. Há problemas concretos no material sugerido:

- `runtime_server_mtp_verify` do manifesto usa **caminho de modelo inexistente**, flags não confirmadas, lançamento direto e `-sm tensor`; **não executar esse comando**. O draft externo deve permanecer em layer e o servidor deve ser gerenciado pelo model-loader.
- O teste adicionado `tests/test-moe-cache-verify.cpp` usa `ggml_graph_compute_with_ctx` e cabeçalhos CPU. Não é prova de execução dos kernels CUDA que causavam o problema; acrescentar/testar o backend CUDA real e quantizações efetivas.
- Guard por `n_tokens <=16` não prova que o caminho é apenas verify: últimos ubatches de prefill também podem ser pequenos. Revisar semântica de fase, publicação de tabelas e todas as transições.
- Default “single-token” não garante footprint idêntico se a implementação agora reserva dez dummies. Medir memória e regressão do default.
- Verificar exportação do arquivo novo: um `git diff` simples pode omitir arquivos untracked. O patch exportado precisa reproduzir a cópia, não apenas mudanças tracked.

Revisar o diff completo, as APIs, callsites e ownership antes de compilar. Corrigir na cópia isolada, nunca no backend original preservado. Não executar compilação concorrente com benchmark CPU-offload.

Build candidato, após revisão e confirmação das opções:

```bash
cmake -S /home/diogo/model-loader-experiments/qwen-flash-cache-mtp \
  -B /home/diogo/model-loader-experiments/qwen-flash-cache-mtp/build \
  -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_MAKE_PROGRAM=/usr/bin/ninja \
  -DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=86-real \
  -DGGML_NATIVE=ON -DLLAMA_BUILD_TESTS=ON
cmake --build /home/diogo/model-loader-experiments/qwen-flash-cache-mtp/build \
  --target llama-server test-moe-cache-verify test-backend-ops -j 4
```

O comando é orientação, não resultado. Confirmar suporte dos targets/opções na cópia. Registrar novo backend/schema/profile sem substituir o binário de produção.

## Matriz obrigatória de execução

Crie um todo para **cada item E0–E11** e também para preparação/exclusividade, validação final, promoção/rollback e relatório. Respeite dependências; paralelize somente investigação/preparação que não contamine medições.

### E0 — Baselines e profiling

1. Preservar estado/configs/profiles/binaries e registrar versões, hashes, argv resolvido, env relevante e hardware.
2. Resolver exclusividade e aguardar fim de downloads/cópias/builds.
3. Medir os dois finalistas existentes, sem alterar args originais, usando corpus real idêntico e sampling igual.
4. Usar o benchmark nativo `model-loader benchmark run --profile <id> --mode llama-bench --json`, preservando stdout/stderr e dados persistidos pelo benchmark.
5. Complementar com probe SSE e corpus diverso. O benchmark nativo recicla HumanEval; não o usar sozinho para inferir locality PLE/MTP em conversas reais.
6. Medir custo de PLE, CPU experts, GPU kernels, QSA/indexer, H2D/D2H e sincronização. `nsys` estava disponível; `perf`, `iostat`, `pidstat` não estavam no PATH. Encontrar método de profiling gerenciado ou instrumentação limitada, sem lançar servidor manualmente nem instalar globalmente sem necessidade.
7. Registrar picos por GPU, RAM/PSS/RSS, pinned, major faults, I/O físico e swap-in/out.
8. Restaurar baseline e reexecutar após mudanças para distinguir drift do sistema de efeito do knob.

### E1 — SSD atual versus KC3000

1. Verificar identidade dos três shards.
2. Rodar `ple-io.py` com rótulo novo, ABBA, mesmos índices, diferentes volumes e workers controlados. A ferramenta mede `pread`+Python/threadpool, não a latência exata do grafo C++.
3. Rodar A/B end-to-end com os profiles cache96 original/KC3000, mesmo corpus, código, args, tamanho de contexto e geração.
4. Distinguir cache do servidor, filesystem e processo. Não chamar cold-disk uma leitura sem controle de residência.
5. Medir prefill em texto novo/diverso e decode aquecido. Se só houver benefício frio ou redução de p95, relatar assim.
6. Não mover/remover o original nem alterar search_paths global apenas por um resultado preliminar.

### E2 — EXL3 conservador

1. Conferir revisão, quant config, shards, dtype/shape, template, MTP e tamanho de PLE.
2. Ler schema real `~/.config/model-loader/backends/schemas/tabby.json` e wrapper `backends/tabby/tabby-serve.sh`.
3. Confirmar no código instalado: `qwen4_exp` declara `supports_tp=False`. **Não testar TP2 como se fosse suportado.** Usar layer/autosplit, budget simétrico.
4. Profile experimental: modelo é diretório EXL3; max-seq-len/cache-size262144; KV8,8; max-batch-size1; chunk512; PLE `ngram-ram=false`; MTP disabled; CPU threads16; vision false; tool-format `qwen3_coder`.
5. Ponto de partida sugerido: `cpu-moe-split-experts=240`, `gpu-split=22,22`. É estimativa de fit, não garantia. Comparar depois com offload por camadas, inicialmente ~23, ajustando pelo orçamento observado.
6. `cpu-moe-offload-layers` e `cpu-moe-split-experts` são mutuamente exclusivos. Não configurar ambos positivos.
7. `EXL3_MOE_ARENA_HUGEPAGE=0` evita compactação síncrona durante load; o efeito de desempenho ainda precisa ser medido.
8. Configurar `template_vars_force.enable_thinking=false` pelo mecanismo **real** de configuração do Tabby; não assumir que `reasoning=false` desliga a geração de raciocínio. A env `TABBY_MODEL_TEMPLATE_VARS_FORCE` é lida como string pelo loader; verificar se o Pydantic converte JSON para dict antes de confiar. Alternativa: `tabby_config.yml` isolado no diretório experimental conforme formato aceito.
9. Evitar três mecanismos redundantes de força de raciocínio sem verificar semântica. Inspecionar prompt renderizado e saída do endpoint.
10. Validar profile, carregar via manager, testar chat/tools e só então throughput/contexto longo.

### E3 — EXL3: placement, threads, chunk e MTP

Uma mudança por comparação:

- Split-experts versus camadas completas em CPU, com memória comparável.
- Threads8/16/24 quando válidos e afinidade baseada nos núcleos reais.
- Quantidade de experts host/GPU em torno da fronteira de fit.
- Chunk512/1024 e maior apenas se houver margem, incluindo prefill longo.
- Draft disabled versus MTP1/MTP2; MTP4/adaptativo somente se houver evidência para expandir a busca.
- `draft-cache-mode` usa aliases como Q8/FP16, não par `8,8`.
- Medir aceitação, proposal/verify/rollback, throughput útil, perda de slots/VRAM e tools.
- Não interpretar alta aceitação como speedup automático.

### E4 — Cache C++ com verify MTP correto

1. Revisar/corrigir a cópia experimental e teste proposto, incluindo os riscos enumerados no handoff.
2. Preservar invariante: cada rota contribui exatamente uma vez, CPU ou GPU.
3. IDs de dummies distintos **dentro de cada token**, mapeados por posição de top-k; shapes/strides/contiguidade corretos.
4. Host/device observam a mesma geração de tabela durante execução; publicar upload somente depois de concluir cópia; não evictar slot em uso.
5. Testar all-hit, all-miss, mistos, slots reutilizados, batches1/2/4/8/9/16, F16/BF16 e quants reais. Rodar CPU e CUDA, comparando valores com referência; ausência de crash não basta.
6. Falsificar a reprodução retirando a correção em cópia controlada: ela deve falhar no caso que protege. Restaurar antes de prosseguir.
7. Exercitar transições prefill→decode, último ubatch pequeno, cancelamento, rollback MTP, novos prompts e reuso de contexto.
8. Registrar backend/profile novos; spec MTP com sidecar correto, `split-mode=layer`, draft KV Q8 ou conservador.
9. Calibrar cache64/80/96 ou faixa segura e MTP1/2 sem infringir memória. A cabeça draft compete com cache e buffers.
10. Comparar default novo contra original e MTP on/off no mesmo binário. Uma mudança de peso/backend não é A/B causal de um knob.

### E5 — Reduzir overhead dominante

Basear mudanças no profiling, não no apelo do patch:

- All-hit sem trabalho/cópias CPU desnecessárias.
- Compactação de rotas ativas versus rotas dummy, se justificável.
- Uploads agrupados/obsoletos e granularidade de sincronização.
- Buffers de tamanho estável ao alternar prefill/decode e crescer QSA.
- Medir custo da seleção de caminhos; não trocar overhead por outra regressão.

Se uma hipótese não dominar, executar probe suficiente para rejeitá-la e registrar a economia máxima por Amdahl. Não implementar abstração sem benefício demonstrado.

### E6 — PLE reader

- Comparar leitor atual com pool reutilizado de workers/buffers.
- Se relevante, io_uring bufferizado versus O_DIRECT com alinhamento, deduplicação por página e staging limitado.
- Preservar ordem, duplicatas, conversão quant, short read, EINTR, EOF e erros.
- Não trocar falha de leitura por zeros/fallback silencioso.
- Testar dados reais não zerados e prefill diverso. Medir latência exposta e p95, não só banda sintética.

### E7 — QSA/GDN/KV

- Verificar QSA gather no verify pequeno e seu custo em contexto longo.
- Medir indexer separadamente: budget2048 não elimina retenção/custo dependente de contexto.
- Qualquer redução do estado GDN ou alteração de KV precisa validação longa, numérica e de rollback; não generalizar o sucesso do 27B.
- Não portar prefill INT8 Syv sem conferir guards: 27B patch exige 24Q/4KV, Flash tem24Q/2KV; flag aceita não prova execução.
- Não misturar precision change com um patch exato no mesmo braço.

### E8 — Quants menores como alternativa condicionada

IQ3_KT/EXL3 3.05 constam da pesquisa como **eixo de qualidade diferente**. Não foram autorizados especificamente nesta transferência e conflitam com a restrição conservadora de coding >=4bit.

Registrar E8 como condicionado à aprovação explícita. Se autorizado, baixar a revisão exata com orçamento e comparar qualidade/tempo de tarefa contra 4bit. Não chamar perda de precisão de otimização exata. Sem autorização, continuar os demais experimentos e deixar a justificativa no relatório.

### E9 — W4A16 custom

- Verificar shape, dtype, grupos, PLE/MTP/dense e orçamento de pico no loader antes de download grande.
- Banco de experts host/pinned não pode exceder RAM total; duas ranks na mesma máquina não criam RAM adicional.
- PLE em disco, loader incremental e ausência de cópia integral/duplicação por rank são pré-requisitos.
- Verificar divisibilidade640/grupo128 e estratégia TP/EP realmente suportada; não transpor receita4x3090 para2x.
- Confirmar caminho de kernel SM86 Marlin/W4A16 com operação real antes de servir o modelo completo.
- Se viável, executar baseline real e comparar com o vencedor C++/EXL3. Se não, registrar evidência do bloqueio de capacidade/kernel e o que seria necessário para superá-lo.
- Não repetir automaticamente uma composição FreeToken/SGLang que mantém ~63GiB de experts pinned num host com ~60GiB totais.

### E10 — Streaming de experts frios

- Medir tráfego físico/hit rate por bytes na fronteira SSD, não confundir hit rate GPU→RAM com SSD.
- Experts top10 ×30camadas em INT4 puro representam ~0,737GB/token antes de cache/overheads. Corrigir estimativas conforme os tensores reais.
- Experimentar layout contíguo/cache em camadas somente se houver capacidade/banda e motivo observado.
- PR25294 é referência, não implementação Flash validada. Conferir interações de mmap, PLE, múltiplos contextos e prefill por ondas.
- Mesmo se não for competitivo em velocidade, registrar utilidade de capacidade e limites. Não declarar universalmente impossível nem prometer superar GGUF residente sem prova.

### E11 — Drafter DFlash dedicado

- O drafter Syv27B é incompatível diretamente: hidden5120/64camadas versus Flash2560/48camadas.
- Verificar se surgiu checkpoint específico Flash e runtime SM86 compatível. Se existir, validar tensores/treino alvo antes de usar.
- Se não existir, avaliar dados, captura de estados, treino e custo antes de propor sua construção.
- Não iniciar treino massivo ou consumir recursos externos pagos sem aprovação. Registrar dependência e priorizar MTP nativo medido.
- Não marcar “testado” quando apenas se demonstrou indisponibilidade de um drafter.

## Protocolo comum e controles

- Separar cold process, cold prefix, warm prefix e cold/warm filesystem.
- Usar corpus idêntico entre braços e registrar hash, tokenizer/template e contagem real de tokens do servidor.
- Balancear A/B/B/A; descartar warmup do cálculo de decode estável, mas guardar seu tempo para análise de load/primeira chamada.
- Múltiplas repetições e variação reportada. Diferenças pequenas com ranges sobrepostos são empate, não vitória automática.
- Medir TTFT, prefill de tokens novos, decode excluindo prefill, E2E, picos de memória, swap e I/O.
- Avaliar no-thinking contra no-thinking; reasoning contra reasoning com budgets iguais. Contar raciocínio no tempo da tarefa.
- Throughput rápido com erros não é sucesso.
- Conferir logs e PID antes/depois de cada execução. Invalidar amostras contaminadas por troca externa/build/download, preservando evidência.
- `capture.py` usa labels exclusivos: não reutilizar diretório já existente.
- Scripts preparados são auxiliares **a revisar e executar**, não harness já comprovado. Verificar accounting de tokens, falhas HTTP, bloqueios, timeouts e resultados efetivamente observáveis.
- Scripts de qualidade antigos usam greedy para diagnóstico determinístico. Não os apresentar como taxa de seleção autônoma em sampling de produção.

Comando modelo para captura:

```bash
python3 .pi/qwen-flash-perf-20260908/capture.py --label ROTULO-NOVO -- \
  model-loader benchmark run --profile PROFILE --mode llama-bench --json
```

## Gates finais dos vencedores

1. Near-full com cerca de244k tokens reais, sem truncamento, recuperação distribuída e resposta coerente.
2. Multi-turn de ferramentas em50% e90% do contexto, editando prefixos e consumindo respostas reais de ferramentas.
3. Gate de integridade obrigatório:

```bash
python3 .claude/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py \
  --model PROFILE --mode integrity --stream --continuation --timeout 600
python3 .claude/skills/rtx3090-inference-profiles/scripts/tool-call-probe.py \
  --model PROFILE --mode auto --promotion --timeout 600 \
  --temperature 1 --top-p 0.95 --top-k 20
```

O caminho `scripts/tool-call-probe.py` na raiz não existia; a cópia está na skill acima. Se a skill mudar de localização, localizar o arquivo real.

4. Código gerado executado em sandbox sem rede, além de JSON/tools. Não executar código do modelo diretamente no host sem isolamento.
5. Geração longa/não inglesa, fim de resposta, reasoning accounting, ausência de loops e preservação de argumentos shell-hostile.
6. Cold starts repetidos, cancelamento/retomada, prefix cache e sessão prolongada.
7. Limites de memória satisfeitos no pior prefill, não só idle; sem swap sustentado ou OOM.
8. Revisão do código novo, testes das operações afetadas e reprodução que falha sem a correção. Não rodar suíte Go inteira se nenhum código Go mudou; rodar validação que cubra a mudança real.

## Promoção, restauração e relatório

- Preservar profiles/binaries originais e demais modelos.
- Somente promover receitas que passaram nos gates. Atualizar descriptions com quant, KV, contexto, placement, picos reais/card, desempenho, aceitação quando houver, sampling e fonte do template.
- Se necessário, manter vencedor curto e vencedor longo separados. Não alterar default global sem necessidade/autorização.
- Sincronizar catálogos de clientes apenas para profiles efetivamente promovidos, pelas ferramentas/skills existentes, sem tocar namespaces alheios.
- Ao terminar, restaurar modelo/serviço conforme o estado acordado com o operador e registrar alterações mantidas. Não inferir que o snapshot antigo ainda representa a intenção atual.
- Remover somente scratch criado por você que não faça parte da evidência; não apagar pesos, builds ou profiles do usuário para liberar espaço sem autorização.
- Exportar patch reproduzível incluindo novos arquivos, base exata e dependências locais. Verificar aplicação em cópia limpa da base adequada e registrar limitações.

O relatório `QWEN3.8-FLASH-NEXT-RESULTADOS-EXPERIMENTAIS.md` deve conter:

1. Decisão executiva e profiles escolhidos.
2. Estado inicial e controle da interferência externa.
3. Matriz E0–E11 completa: executado/aprovado/rejeitado/bloqueado, com evidência por item.
4. Tabela de cold/warm TTFT, prefill, decode, E2E, memória por GPU/host e qualidade por contexto.
5. Comparações causais separadas de comparações entre receitas completas.
6. Falhas, OOMs, corrupção, empates e testes inconclusivos preservados.
7. Mudanças de código com invariantes e provas CPU/CUDA.
8. Caminhos, hashes, revisões, comandos de reprodução e rollback.
9. Limites e alternativas que dependem de aprovação, hardware ou assets externos.
10. Estado final da workstation e o que permaneceu alterado.

Só encerrar execução como concluída depois de verificar o estado real dos entregáveis. Se houver bloqueio externo, terminar tudo que for alcançável, registrar exatamente o impedimento e não marcar os testes bloqueados como aprovados.
