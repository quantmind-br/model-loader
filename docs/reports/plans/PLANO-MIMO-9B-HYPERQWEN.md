# Plano de implementação: MiMo 9B no HyperQwen/syv

Data: 2026-09-27. Estado: planejamento; nenhuma instalação, conversão ou execução de inferência foi feita para produzir este documento.

## 1. Objetivo e resultado esperado

Integrar `XiaomiMiMo/MiMo-V2.6-Distill-Qwen-9B` ao model-loader usando um ambiente HyperQwen isolado, medir as otimizações compatíveis com RTX 3090 e entregar um perfil reproduzível para código e ferramentas. Avaliar MTP e DFlash separadamente; promover somente alternativas que preservem correção e melhorem a carga real.

O resultado pode ser um perfil syv sem especulação, com MTP ou com DFlash. Se os experimentos não trouxerem benefício, entregar os resultados negativos e manter a referência mais confiável. Não pressupor os ganhos publicados para Qwen 27B.

## 2. Evidências e incertezas

| Item | Evidência atual | Consequência |
|---|---|---|
| Arquitetura | Qwen3.5, hidden size 4096, 32 camadas, 16 cabeças Q, 4 KV, head dim 256; contexto nativo configurado de 262144 | Família compatível com parte dos patches; dimensões distintas do Qwen 27B |
| MTP oficial | Configuração declara uma camada; índice de pesos oficial consultado não contém `mtp.*` | Não ativar MTP somente porque existe o campo no config |
| MTP comunitário ajustado | VitreousCut publica GGUF e relata ajuste da cabeça original Qwen3.5-9B em aproximadamente 32,7 milhões de tokens MiMo | Candidato principal para MTP; pesos HF treinados ainda precisam ser localizados |
| MTP transplantado | Há GGUF com cabeça do Qwen3.5-9B sem ajuste informado | Controle experimental, não equivalente à cabeça treinada |
| DFlash | z-lab publica drafter para Qwen3.5-9B; não foi encontrado um específico do MiMo 9B | Testar transferência; não assumir aceitação nem compatibilidade com DFlash2 |
| Prefill INT8 syv | Patch exige exatamente 24/4/256; MiMo usa 16/4/256 | Flag aceita pode permanecer inerte; portabilidade requer alteração e testes |
| Formato oficial | BF16, aproximadamente 17,5 GiB de tensores | Memória total inclui cache, gráficos, buffers e eventual drafter |
| Template | MiMo tem tokenizer/template próprios; exemplo oficial usa parser `mimo` no SGLang | Não herdar automaticamente os parsers Qwen dos launchers syv |

Código HyperQwen analisado: `da8a8e97835558b417dff631ed76e1a9a6f3d075`, baseado em vLLM 0.29.0. O checkout local `backends/syv-qwen38` existe, mas sua versão operacional deve ser auditada antes de reutilização. As referências locais da skill descrevem uma instalação anterior; fonte, binário e testes prevalecem.

## 3. Limites operacionais

- Começar em GPU1: `CUDA_DEVICE_ORDER=PCI_BUS_ID`, `CUDA_VISIBLE_DEVICES=1`; limite de 23 GiB por GPU e 46 GiB agregados.
- Manter o power cap atual de 270 W. Registrar driver, clocks, temperatura, potência e uso concorrente da GPU em cada rodada. Referências locais a 290 W e upstream a 250 W não são comparações controladas.
- Criar checkout/venv e entrada de backend separados. Preservar `syv-qwen38`, perfis existentes, modelos originais e alterações locais de outros trabalhos.
- Lançar inferência somente por `model-loader instance start <id>` e enviar requisições pelo proxy. Não iniciar servidores manualmente pelos launchers upstream.
- Não configurar `port` em perfis; o process manager aloca. `served-model-name` deve corresponder ao ID do perfil.
- Ler o schema vivo antes de selecionar argumentos. Flags reais ausentes do schema podem usar `extraArgs`; não inventar flags nem editar schemas gerados à mão.
- Usar a skill `huggingface-download` na execução dos downloads e `backend-schema-update` se houver mudança de backend/schema.
- Não usar NVFP4 como primeira rota para Ampere. O artefato comunitário NVFP4-MTP mira Blackwell e não comprova um caminho adequado para estas 3090.
- Treinar uma cabeça/drafter novo é uma extensão eventual, com orçamento e dados definidos separadamente. Não faz parte do caminho mínimo.

## 4. Fases e critérios de saída

### Fase 0 — inventário e congelamento de versões

- [ ] Auditar catálogo, schemas, perfis MiMo existentes, checkout syv, script de build, espaço livre e ocupação das GPUs.
- [ ] Identificar versões efetivas de vLLM, PyTorch, CUDA, Transformers e parsers; registrar commits e patches aplicados.
- [ ] Fixar revisões HF para target, quantizações e drafters; registrar hashes de config, tokenizer, template e índices.
- [ ] Inspecionar nomes, shapes e dtypes dos tensores, não apenas model cards. Verificar tokens especiais, EOS e vocabulário entre target e drafter.
- [ ] Localizar pesos HF/safetensors da cabeça VitreousCut, se publicados. Não contatar o autor automaticamente; se indisponíveis, registrar a dependência e continuar as demais rotas.
- [ ] Inspecionar configuração do DFlash z-lab: arquitetura, camadas-alvo, dimensões, sliding window, IDs especiais e versão de runtime exigida.

**Saída:** manifesto reproduzível e tabela de compatibilidade dos artefatos. Separar fatos verificados de alegações dos publicadores.

### Fase 1 — referência funcional e de qualidade

- [ ] Criar baseline MiMo sem especulação em backend compatível, preferindo o caminho oficial SGLang para referência funcional quando disponível.
- [ ] Começar com 8192 tokens; usar BF16 se couber no orçamento completo. Se não couber, usar uma quantização de alta fidelidade como controle e registrar a limitação.
- [ ] Extrair sampling do model card/metodologia e `generation_config.json`; fixar explicitamente os parâmetros efetivos nas requisições de comparação.
- [ ] Verificar template oficial, thinking on/off, EOS, streaming e contabilização de tokens de raciocínio e resposta.
- [ ] Validar ferramenta simples, múltiplos argumentos, Unicode, aspas, barras, quebras de linha e continuidade após retorno de ferramenta. Usar ferramentas simuladas, sem executar comandos produzidos pelo modelo.
- [ ] Congelar corpus e verificadores antes das otimizações: código com testes, português/inglês, JSON, cópia literal e tarefas agentivas de múltiplos turnos.

**Saída:** perfil de controle funcional, corpus versionado e resultados brutos. Repetições patológicas ou parsing incorreto impedem avançar para promoção de desempenho.

### Fase 2 — ambiente HyperQwen e integração mínima

- [ ] Criar `backends/syv-mimo9b/` com build reproduzível e pin do HyperQwen auditado, sem atualizar a instalação existente.
- [ ] Aplicar a série de patches na ordem declarada, com falha explícita em hunks rejeitados. Verificar dependências CUDA e importação do caminho real de serving.
- [ ] Registrar backend separado, por exemplo `syv-mimo9b`, com kind `vllm`. Não introduzir um novo kind apenas para representar esse fork.
- [ ] Criar perfil sem `speculative-config`; `SPEC=off` é conceito do launcher upstream, não uma flag vLLM a copiar para `args`.
- [ ] Validar carregamento Qwen3.5 e tratamento da ausência de pesos MTP, sem fabricar tensores para satisfazer metadados.
- [ ] Escolher os parsers realmente presentes nessa versão. Se necessário, adaptar o parser MiMo em patch isolado; testar raciocínio, ferramentas e SSE. Não manter `qwen3` por mera herança do launcher.
- [ ] Fazer o mesmo corpus passar pelo proxy model-loader e pelo perfil syv. Comprovar que o perfil encaminha nome, template e parâmetros corretamente.

**Saída:** MiMo servido por syv sem especulação, funcionalmente validado. `--help`, boot ou health check isolados não bastam.

### Fase 3 — quantização e otimizações não especulativas

- [ ] Auditar primeiro as quantizações MiMo já publicadas, especialmente AWQ/AutoRound; evitar recalibrar se um artefato existente for compatível e passar no controle de qualidade.
- [ ] Caso necessário, gerar W4A16 em formato suportado pelo Marlin com corpus representativo de código/ferramentas; preservar tokenizer e template.
- [ ] Adaptar preparação para checkpoint sem `mtp.*`, diferentes shards e quantization config. Não executar o requantizador 27B diretamente no BF16 oficial.
- [ ] Manter originais imutáveis; publicar conversões via arquivos temporários e rename, com índice consistente e sem tensores duplicados entre shards.
- [ ] Comparar W4A16 com referência de maior precisão. Medir separadamente quantização de `lm_head` e embeddings antes de adotá-la.
- [ ] Testar uma mudança por vez: estado GDN FP16, prefix caching, W4A8 apenas nas MLPs e outras seleções somente se houver benefício.
- [ ] Usar KV BF16 como controle; avaliar KV compacto apenas como experimento de capacidade/latência, com testes de contexto longo e ferramentas.

**Saída:** baseline syv otimizado sem especulação, com qualidade e ganho medidos. Não atribuir a patches syv a diferença causada apenas pela troca de quantização.

### Fase 4A — MTP ajustado ao MiMo

- [ ] Validar o GGUF VitreousCut num backend llama.cpp registrado que implemente o grafo Qwen3.5 MTP; não assumir que o fork citado no card é o único ou que todo build atual funciona.
- [ ] Comparar MTP ligado/desligado usando o MESMO GGUF, template, sampling e contexto. Comprovar propostas/aceitação nas métricas ou logs.
- [ ] Para portar ao syv, preferir pesos HF treinados originais. Validar shapes, namespaces, normas, embeddings compartilhados e logits antes de quantizar a cabeça.
- [ ] Se só existir GGUF, tratar a conversão reversa como rota opcional: Q8_0 não recupera os pesos treinados originais; desfazer corretamente convenções de RMSNorm, layouts e nomes. Exigir equivalência numérica ao artefato fonte.
- [ ] Transplante do MTP Qwen3.5-9B pode ser um controle separado; nunca rotulá-lo como a cabeça ajustada no MiMo.
- [ ] Se integrado ao vLLM, medir comprimentos de draft pequenos, inicialmente 1 e 2, e ampliar apenas quando houver ganho. Confirmar a API especulativa da versão instalada.
- [ ] Só depois considerar quantização da cabeça e vocabulário reduzido, calibrado nas saídas MiMo com conjunto de validação separado; não reutilizar a lista de IDs do 27B.

**Saída:** MTP validado no syv ou conclusão explícita de dependência/bloqueio de formato. O resultado GGUF é comparador útil, mas não conta como implementação HyperQwen concluída.

### Fase 4B — DFlash do Qwen3.5-9B

- [ ] Testar primeiro o drafter no Qwen3.5-9B original, quando viável, para separar falha de runtime de perda de aceitação causada pelo fine-tuning MiMo.
- [ ] Verificar suporte ao formato DFlash do z-lab na versão syv selecionada. Não confundir DFlash com o caminho DFlash2 do launcher nem assumir que trocar `DRAFT` basta.
- [ ] Se incompatível, fazer port mínimo isolado ou usar SGLang como prova de compatibilidade do par, registrando que isso ainda não entrega a integração syv.
- [ ] Medir MiMo + drafter com lookup desligado primeiro; validar camadas capturadas e remapeamento de tokens, se necessário.
- [ ] Escolher backends de atenção compatíveis com SM86; não copiar FA4/TRT-LLM/B200 do exemplo z-lab.
- [ ] Medir blocos pequenos antes de maiores; registrar tokens propostos, aceitos, emitidos por etapa, custo do draft e memória adicional.
- [ ] Ativar lookup somente após o drafter funcionar, com testes exatos de cópia de código, JSON e argumentos de ferramenta.

**Saída:** compatibilidade, aceitação e ganho reais documentados. Se a transferência não compensar, manter especulação desligada; fine-tuning do drafter é trabalho posterior, não pressuposto.

### Fase 5 — port opcional do prefill INT8

- [ ] Priorizar apenas se o perfil vencedor apresentar gargalo relevante de prefill.
- [ ] Generalizar o predicado 24/4/256 para suportar 16/4/256, auditando GQA e os limites de cada kernel. Não basta remover a condição.
- [ ] Validar numericamente contra atenção de referência em comprimentos curtos/longos, chunks, cache de prefixo e limites de blocos.
- [ ] Testar apenas as combinações de dtype/cache suportadas e preservar fallback para as demais.
- [ ] Comprovar acionamento por instrumentação; medir TTFT, memória e qualidade. Exportar patch reproduzível conforme o fluxo upstream.

**Saída:** kernel portado e comprovadamente benéfico, ou exclusão justificada do perfil final.

## 5. Matriz de medição e promoção

Comparações mínimas, sempre com o mesmo target/quant quando o objetivo for isolar runtime ou especulação:

| Braço | Finalidade |
|---|---|
| Controle funcional de maior precisão | Referência de qualidade e template |
| vLLM stock na mesma versão + mesma quantização | Isolar o efeito dos patches syv |
| syv sem especulação | Referência de velocidade para os drafts |
| syv + MTP, se disponível | Medir ganho da cabeça e custo de verificação |
| syv + DFlash, se compatível | Medir transferência do drafter Qwen para MiMo |
| GGUF MTP ligado/desligado | Validar o artefato comunitário em seu runtime de origem |

- Contextos: smoke em 8k; candidato inicial em 32k; depois 64k/128k/256k conforme memória e necessidade. Esses valores são etapas de medição, não promessa de capacidade.
- Para o contexto promovido, medir preenchimento aproximado de 5%, 25%, 50% e 90%, reservando espaço para saída.
- Concorrência 1 como principal; 2 e 4 como controles. Não otimizar para 64 usuários por reproduzir a tabela upstream.
- Aquecer compilação/gráficos antes de medir. Fazer pelo menos três repetições emparelhadas, alternando braços; ampliar somente se a variação impedir conclusão.
- Registrar TTFT frio/quente, prefill tok/s, decode tok/s, duração total, tokens efetivos, prefix-cache hit, pico VRAM por GPU, potência, aceitação e erros/timeouts.
- Manter fixtures pequenas determinísticas para parsing/exatidão e uma bateria de pelo menos 200 casos de qualidade para candidatos finais. Comparar escores por tarefa, com incerteza, sem exigir igualdade textual entre gerações amostradas.
- Greedy, se usado para diagnóstico de exatidão, não define o sampling do perfil agentivo final.

**Critérios propostos para promoção:**

1. Nenhuma nova falha de contrato na bateria determinística de ferramentas, JSON, streaming ou cópia literal.
2. Nenhum OOM/crash em contexto-alvo e sequência de múltiplos turnos; pico dentro do orçamento por GPU.
3. Qualidade sem regressão prática superior a 2 pontos percentuais na bateria fixada; resultado inconclusivo exige mais evidência, não promoção automática.
4. Ganho de pelo menos 5% na média geométrica das comparações de desempenho relevantes, superior à variabilidade observada; TTFT p95 sem regressão maior que 10%, salvo trade-off documentado de capacidade.
5. Especulação comprovadamente ativa; aceitação alta isoladamente não é critério de sucesso.

TP2 só entra depois, se capacidade ou medição justificarem. Manter `disable-custom-all-reduce` em vLLM/SM86, verificar P2P por logs e não usar particionamento assimétrico. Não mudar o power cap para perseguir números upstream.

## 6. Entregáveis, integração e rollback

- Build isolado e manifesto com versões, revisões HF, hashes, comandos e ordem dos patches.
- Scripts de preparação/conversão reexecutáveis; patches externos separados do código Go.
- Perfis schemaVersion 3 com naming do repositório, contexto como último segmento e sufixos `mtp`/`dflash`/`vision` apenas quando ativos.
- Exemplo de ID, ajustável à configuração real: `mimo-v2.6-distill-qwen-9b-w4a16-syv-textonly-32k`; inserir `mtp` ou `dflash` antes de `32k` somente após validação.
- Registrar descrição com quant, KV, contexto, placement, template, sampling, VRAM, latência e aceitação medidos. Perfil não calibrado deve dizer `calibration pending`.
- Relatório em `docs/reports/` com tabela A/B, logs, resultados negativos, limitações e recomendação final. Não editar páginas OpenWiki geradas.
- Alterar código Go apenas se houver lacuna real de integração. Para mudanças de schema/help, seguir regeneração e testes de apresentação; para mudanças Go, testes pertinentes e checks exigidos pelo repositório.
- Rollback: parar a instância experimental via model-loader, selecionar o perfil anterior e preservar os artefatos para diagnóstico. Não substituir defaults existentes durante os experimentos.

## 7. Ordem recomendada e condições de parada

Executar 0 → 1 → 2 → 3; em seguida 4A e 4B como experimentos independentes; executar 5 apenas se o perfil de tempo justificar. Finalizar pela matriz e documentação da seção 6.

Parar uma rota ao encontrar incompatibilidade de artefato sem conversão confiável, corrupção, ausência de ganho após comparação controlada ou excesso de memória. Preservar falhas completas e classificar a causa. Uma rota bloqueada não impede entregar o baseline ou a outra rota especulativa.

Não estimar horas de treinamento nem prometer percentuais de aceleração antes de resolver os formatos MTP/DFlash e medir o baseline local.

## 8. Fontes

- [HyperQwen no commit analisado](https://github.com/syv-ai/HyperQwen/tree/da8a8e97835558b417dff631ed76e1a9a6f3d075)
- [Série de patches](https://github.com/syv-ai/HyperQwen/blob/da8a8e97835558b417dff631ed76e1a9a6f3d075/PATCHES.md)
- [Prefill INT8](https://github.com/syv-ai/HyperQwen/blob/da8a8e97835558b417dff631ed76e1a9a6f3d075/patches/prefill-attn-int8.patch)
- [MiMo oficial](https://huggingface.co/XiaomiMiMo/MiMo-V2.6-Distill-Qwen-9B)
- [Configuração oficial](https://huggingface.co/XiaomiMiMo/MiMo-V2.6-Distill-Qwen-9B/blob/main/config.json)
- [Índice de pesos oficial](https://huggingface.co/XiaomiMiMo/MiMo-V2.6-Distill-Qwen-9B/blob/main/model.safetensors.index.json)
- [MTP ajustado: VitreousCut](https://huggingface.co/VitreousCut/MiMo-V2.6-Distill-Qwen-9B-MTP-GGUF)
- [MTP transplantado: Q8_0 comunitário](https://huggingface.co/IHaveNoClueAndIMustPost/MiMo-V2.6-Distill-Qwen-9B-Q8_0-MTP-GGUF)
- [DFlash Qwen3.5-9B](https://huggingface.co/z-lab/Qwen3.5-9B-DFlash)
- [MiMo NVFP4-MTP, direcionado a Blackwell](https://huggingface.co/ycui7/MiMo-V2.6-Distill-Qwen-9B-NVFP4-MTP)

Os links HF em `main` são mutáveis; a fase 0 deve substituí-los no manifesto por revisões fixas. Resultados de model cards são alegações dos autores até reprodução local.
