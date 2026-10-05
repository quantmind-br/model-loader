# Continuação delegada ao Claude — Strata / IDEATION_PERFORMANCE

O usuário pediu: «demande ao claude o que falta». Continue a implementação já
autorizada de `IDEATION_PERFORMANCE.md`, conclua as validações possíveis e
registre com precisão o que continuar pendente. Execute o trabalho; não produza
somente outro plano. Responda em português. Não delegue a outros agentes.

## Escopo e limites de acesso

- Repositório: `/home/diogo/dev/model-loader`.
- Fork de trabalho: `backends/strata-fork`, checkout separado e gitignored.
  Não altere a cópia original `/home/diogo/dev/Strata`.
- Leia `AGENTS.md` e as instruções de diretório antes de editar. O repositório
  principal e o fork já contêm alterações anteriores: preserve-as. Não use
  reset/clean/revert amplo e não faça commit/push sem pedido específico.
- Respeite integralmente a sandbox, as permissões e a política de aprovação
  da sua sessão. Não tente obter acesso ao host por SSH, socket de daemon,
  tmux, outra ferramenta, outro agente ou alteração de configuração.
- A tentativa anterior de delegação ocorreu numa sessão restrita, sem acesso
  às GPUs, sockets TCP ou escrita no config externo. Depois dela, o ambiente
  foi atualizado para acesso irrestrito ao filesystem e rede habilitada.
  O acesso efetivo às GPUs ainda deve ser verificado na sessão executora;
  não trate o bloqueio anterior como estado atual nem presuma que desapareceu.
  Faça somente trabalho permitido pelas permissões vigentes.
- O usuário mostrou `nvidia-smi` funcionando no próprio terminal, em
  2026-10-01 18:45:20: duas RTX 3090, 24 GiB cada, 270 W por placa,
  temperaturas 50/54 °C e sem processo de inferência na listagem. Isso prova
  acesso no terminal dele, não no ambiente do agente nem o estado atual.
- Verifique seu acesso real e processos antes de testar. Se GPU/rede local
  ou permissões necessárias estiverem indisponíveis, registre o bloqueio.
  Não anuncie execução ou resultados que não ocorreram.

## Fontes de verdade

Leia, nesta ordem:

1. `docs/reports/strata-implementation-2026-10-01/IMPLEMENTATION.md`.
2. `IDEATION_PERFORMANCE.md` (audit original; não sobrescreva).
3. `docs/reports/strata-implementation-2026-10-01/pending-validation.json`.
4. No mesmo diretório: `implementation-state.json`, `final-release.json`,
   `matrix-status.json`, `matrix.json`, `extra-experiments.json`,
   `candidates.json`, `variants.json`, `summaries.json` e
   `prepared-config-snapshot/` (25 configs e manifestos arquivados).
5. `.agents/skills/rtx3090-inference-profiles/SKILL.md` e referências pertinentes,
   incluindo `workflows/full-tuning.md`, `references/strata.md` e
   `references/self-improvement.md`. Para alterações de schema, leia também
   `.agents/skills/backend-schema-update/SKILL.md`.

Fontes e testes prevalecem sobre os resumos. Não reexecute cegamente scripts
antigos em `/tmp`: dependem de PIDs antigos e podem reutilizar evidências.

## Estado preservado

As principais correções de API/controles, cancelamento, health, métricas GPU,
afinidade auxiliar, experts residentes dual, stage-dense, pack transacional,
releases reproduzíveis, ranking e cache BPE já estão implementadas.

Nenhum candidato foi promovido aos seis profiles canônicos. O catálogo
`strata-fork` aponta para `releases/baseline-20261001/serve/server.py`,
preservando o baseline e o executável nativo original. Não substitua esse
baseline nem restaure o catálogo inteiro.

Release medida: `backends/strata-fork/releases/20261001T201850Z-8ae9e4e25528`,
catálogo `strata-perf-v2-20261001`, SHA256 nativo
`156297b9563a76e4a695c165c3977c4941e604bf5a5a00dee0277e83262996be`.
A árvore atual tem mudanças posteriores; consulte `implementation-state.json`.
Não declare a árvore atual inteira validada no hardware com base nessa release.

Resultados exploratórios, ainda sem A/B de três reinicializações por braço:

- IQ2 32k: baseline ~132,0 tok/s; release medida ~131,4; stage-dense ~131,6.
  Stage-dense poupou ~1,45 GiB por GPU e manteve os 15 outputs comparados.
- IQ3: GPU1 prefill512 ~86,5 tok/s, TTFT fresco ~29k de 67,54 s;
  dual512 ~108,3 e 18,55 s; dual1024 ~118,2 e 17,83 s;
  dual2048 ~120,1 e 11,12 s, com crescimento swap +1,514 GiB.
- IQ3 dual residente sem stage-dense falhou no guard de swap; GPU1/1024
  também. Não repetir sem mudança justificada.
- IQ2 256k com prefixos realmente novos falhou no guard de swap (+2,080 GiB).
  IQ2 976k interrompido por temperatura GPU0 >=85 °C após fills frescos
  de 49.552 e 249.547 tokens; quase cheio não aprovado.
- IQ3 repetiu tool call após resultado bem-sucedido; não está qualificado
  como agente. O harness estrito falha porque garantias não implementadas
  recebem HTTP 400 explícito. Não transforme esse resultado em aprovação.
- Fills antigos compartilharam prefixos; use `cache_n` e não compare como
  prefill integral. Os probes corrigidos variam a primeira linha por fill.
- PLE8 tem 7 requests completos e sampler.stop, mas falta código final no
  matrix-status arquivado. Conserve a distinção, sem inventar conclusão.

## Trabalho pendente, em ordem

1. **Estado e gates.** Inspecione proxy/processos/configs/revisões atuais,
   sem presumir visibilidade do host. Se o ambiente permitir, rode os gates
   completos Go/Python/native sobre a árvore atual e a fixture nativa real
   `file_expert_source_dual_resident --dual-resident` nas duas GPUs. Ela
   compilou, mas foi pulada na última sessão por ausência de GPUs.
   Publique nova release imutável somente após os gates aplicáveis.

2. **Matriz restante, serial.** PLE32, spec2/spec6 e IQ4 resident-stage-dense;
   reconcilie a ambiguidade PLE8 antes de decidir uma repetição necessária.
   Os IDs e parâmetros estão em pending-validation/matrix/config snapshot.
   Use diretório novo por tentativa e guarde logs completos.

3. **IQ2 longo.** Teste o candidato IQ2 stage-dense 256k preparado, com
   prefixos novos. Para 976k, proponha e execute somente uma mudança com
   justificativa para o problema térmico; preserve os guards. O profile
   976k pode permanecer no baseline se não passar. Não reduza silenciosamente
   contexto nem renomeie os seis profiles canônicos.

4. **Ranking.** Execute treino com o candidato dump-routing e corpus próprio,
   use `make_profile.py --rerank --checkpoint <identidade completa> --out
   <arquivo próprio>` e compare em holdout separado. Não substitua a base
   compartilhada e não treine no corpus de avaliação.

5. **CPU/MTP/afinidade.** Execute paridade `STRATA_IQ_MT_MIN=1`, confirme
   máscaras reais das threads e compare knobs isolados. As medições de
   spec_min_p/pcie_frac no mesmo processo são exploratórias e não justificam
   mudança geral de default. Avalie qualidade em português e tools também.

6. **Profiling.** Execute o candidato Nsight preparado. O wrapper está em
   `docs/reports/strata-implementation-2026-10-01/nsys-native.sh` e usa
   `--sample=none --cpuctxsw=none --trace=cuda,nvtx --cuda-graph-trace=node
   --delay=20 --duration=90 --kill=none`, com STRATA_VERIFY_PROFILE=1.
   Profile antes de decidir implementar fusão CUDA, P2P ou MTP batched no
   último stage; essas otimizações continuam condicionais à evidência e
   oracle numérico. Host staging ~0,22 ms/window frente a ~22 ms total não
   mede isoladamente handoff completo e não justifica P2P por si só.

7. **Cache de conversas.** Execute A→B→A acima do checkpoint 16.384 tokens
   (probe atualizado para ~21k). O teste antigo de ~5,6k não prova defeito.
   Parking só se demanda e orçamento de memória conjunto justificarem.

8. **Seleção.** Para candidatos promissores, A,B,B,A,A,B com aquecimento e
   três starts por braço, um knob por comparação, incluindo correção de
   outputs, PT/tools, fills, recursos e aceitação. Rejeite falhas de guard.
   Registre também hipóteses rejeitadas; nem toda sugestão exige mudança
   quando o experimento não demonstra benefício.

9. **Promoção e limpeza.** Apenas se os gates aplicáveis passarem e sua sessão
   tiver permissão de escrita nos configs: promover via Model Loader,
   preservando IDs/contexto/rollback. Mudanças compartilhadas exigem regressões
   IQ2 32k/256k e perto do limite976k se afetarem memória/prefill. Preserve o
   baseline nos profiles ainda não aprovados. Remova só candidatos/configs
   criados por esta tarefa após registrar decisões; não remova modelos ou
   alterações do usuário. Atualize a skill somente se permitido e dentro
   do escopo já pedido; não promova aprendizados automaticamente.

## Regras de execução e evidência

- Inicie modelos exclusivamente por `model-loader instance start`; inferência
  pelo proxy `127.0.0.1:4321`. Não execute servidor de inferência por fora.
- Uma carga/benchmark GPU por vez; não sobreponha builds/testes CUDA.
- 270 W por GPU; máximo 23.552 MiB por placa; MemAvailable >=2 GiB;
  crescimento global swap <=2 GiB; temperaturas <85 °C. Três violações
  consecutivas interrompem o braço. Resfriamento passivo <65 °C antes de start.
- Sem swapoff, limpeza global de cache, mudanças de potência/clocks/driver,
  encerramento de apps do desktop ou relaxamento dos guards.
- O probe atual possui guard emergencial que encerra somente o grupo Setsid
  com argv do config exato e identidade de processo revalidada. O teste de
  três processos passou; comportamento real de abort ainda não foi observado.
  Não provoque sobreaquecimento/pressão de memória só para validar o guard.
- Use `scripts/strata-performance-probe.py`,
  `scripts/strata-resource-sampler.py`, `scripts/strata-summarize-probes.py` e
  `scripts/strata_probe_lifecycle.py`. Requests sem usage/timings finais são
  incompletos, não resultados válidos.
- Antes: Go completo/vet/build, Python89 pass5skip e native39 passaram no host.
  Depois das últimas mudanças, apenas gates locais focados passaram; suítes
  integrais Go/Python foram bloqueadas por sockets. Preserve esses qualificadores.
- `ple_parity` depende de fixtures externas ausentes; `platform_memory_test`
  exige mlock acima do limite local de8MiB. Não declare que passaram.
- Para os testes tools, a dependência GGUF local está em
  `/home/diogo/dev/Strata/build-fork-sm86/_deps/strata_llamacpp-src/gguf-py`
  (`STRATA_GGUF_PY` e `PYTHONPATH=tools`). Caches locais podem usar
  `GOCACHE=/tmp/strata-go-cache` e `CCACHE_DIR=/tmp/strata-ccache`.

## Entrega

Escreva `docs/reports/strata-implementation-2026-10-01/CLAUDE_COMPLETION.md`
com acesso efetivo, alterações, comandos/resultados/evidências, matriz de
S01–S20 (implementado, validado, rejeitado por evidência ou pendente), decisões
de profiles e bloqueios concretos. Atualize IMPLEMENTATION.md e os índices
de pendências apenas com resultados verificados, preservando histórico.
Não edite OpenWiki gerado nem o audit original. Não diga «concluído» enquanto
houver trabalho obrigatório pendente; se bloqueado, deixe a retomada exata.
