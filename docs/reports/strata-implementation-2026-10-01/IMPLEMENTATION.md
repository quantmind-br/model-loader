# Implementação de IDEATION_PERFORMANCE.md

Execução em 2026-10-01, workstation com duas RTX 3090 a 270 W por placa.
Este relatório acompanha os vinte itens do plano; o relatório de auditoria
continua preservado como evidência anterior às mudanças.

## Isolamento e proveniência

O código está em `backends/strata-fork`, checkout independente e ignorado pelo
Git do Model Loader. A árvore `/home/diogo/dev/Strata` não foi alterada.
`baseline/` guarda diffs anteriores, profiles, catálogo, schemas e fontes
originais. Os arquivos que já estavam modificados foram preservados.

O catálogo `strata-fork` foi isolado em
`releases/baseline-20261001/serve/server.py`. Os candidatos usam o catálogo
`strata-perf-v2-20261001`, com release
`20261001T201850Z-8ae9e4e25528`. A publicação não substitui o executável ativo.
`final-release.json`, os `BUILD.json`, os configs arquivados e os logs de launch
identificam os caminhos efetivamente executados. A versão textual do engine,
0.1.30, sozinha não identifica as alterações locais.

## Mudanças funcionais

- **S01:** pack transacional em diretório temporário, manifesto SHA-256 de
  origem/conversor/base/artefatos, lock e publicação por rename. Origem diferente,
  pack corrompido ou diretório legado sem manifesto são recusados sem sobrescrita.
  Os packs grandes já instalados não foram reconvertidos nem apagados.
- **S02:** build limpo com CUDA explícito e SM86, dependências/toolchain/hash
  registrados, verificação de alteração da fonte durante o build e releases
  separadas. O script agora inclui também os testes e o diff rastreado na
  proveniência dos próximos builds.
- **S03:** validação antes de SSE, stop incremental entre chunks/Unicode,
  checagem de tipos primitivos dos argumentos e buffering de chamadas completas.
  `required`, chamada nomeada, `none` com tools, `parallel_tool_calls=false`
  com tools, schema estrito e JSON constrained são recusados com HTTP 400.
  Não há promessa de constrained decoding ou validação JSON Schema completa.
- **S04:** cancelamento de requests na fila e não streaming; STOP com drain
  limitado a 10 s, seguido de terminate/kill e reap antes de reiniciar.
  Cada pump conserva seu próprio processo e fila. A detecção não streaming usa
  EOF de socket; clientes que fazem half-close da escrita são considerados
  cancelados. O proxy e clientes HTTP usuais mantêm a conexão aberta.
- **S05:** saúde ready/unloaded/restarting/failed; falha inesperada/restart
  retorna 503. O monitor entende o estado e mantém compatibilidade com health
  legado. Terminação de grupo confirma também descendentes vivos, com prazo;
  zombies já não detêm recursos e não prolongam a espera.
- **S06:** seed zero presente é distinta de seed ausente; tipos, valores finitos
  e intervalos são verificados também no protocolo nativo. Top-k é 1–64.
  Timings preservam sampling/esforço efetivos e informações do engine.
- **S07:** monitor lê todas as GPUs com índice físico/UUID; benchmark registra
  picos por placa e total simultâneo, com versão de métrica 2. Histórico antigo
  permanece inalterado. A memória é do dispositivo inteiro, incluindo desktop;
  a amostragem auxiliar registra também memória de processos CUDA, RSS/VmSwap,
  cgroup, PSI, zram, I/O, clocks, temperatura e motivos de limitação de clocks.
- **S08:** auxiliares restauram a máscara permitida capturada antes do pin do
  serviço, excluindo seu núcleo físico quando possível. Pools de experts
  conservam sua política. Não se atribui um ganho isolado a essa mudança sem A/B.
- **S09:** complemento residente dual inclui a união dos caches e cópias dos
  slots emprestáveis; trocas adaptativas usam cache/device/stream do stage e
  índices de exchange globais. Reserva e orçamento antecedem a publicação.
  Suporte limitado ao caminho gerenciado; remote cache continua recusado.
- **S11:** `--stage-dense` opt-in filtra tensores de camadas não pertencentes ao
  stage nos dois loaders, preservando pesos compartilhados. Exige split
  explícito; o comportamento padrão não filtra. No IQ2 32k, os quinze outputs
  comparados com o mesmo número de slots foram idênticos, com cerca de 1,45 GiB
  economizados por placa. Esse ganho de capacidade não é ganho de decode.
- **S13:** `make_profile.py --rerank --checkpoint ... --out ...` coloca frequências
  do trace antes da base. Treino exige arquivo próprio e não pode ser mascarado
  silenciosamente por uma base completa. Metadados preservam identidade e hashes.
- **S16:** cache BPE por tokenizer, 16.384 entradas imutáveis; fragmentos maiores
  que 256 caracteres seguem sem cache. IDs são comparados contra o algoritmo
  sem cache, incluindo Unicode, alta entropia e chamadas concorrentes.

## Método de medição

Todos os modelos são iniciados pelo Model Loader; inferência usa o proxy em
127.0.0.1:4321. Não houve swapoff, limpeza global de caches, mudança de potência,
firmware, driver, clocks ou encerramento de aplicativos para forçar um resultado.

Guards: até 23 GiB por placa, MemAvailable mínimo de 2 GiB, crescimento global
de swap até 2 GiB e temperatura abaixo de 85 °C; três violações consecutivas
interrompem o braço. A partir da segunda fase, cada rodada começa depois de
resfriamento passivo abaixo de 65 °C. Logs das rodadas anteriores são mantidos.

Cada probe guarda requests, chunks, texto, token counts, timings e telemetria.
O warmup é separado das três amostras de código. Os fills são 5/25/50/90%,
com repetição em cache. Uma descoberta metodológica exigiu prefixos distintos:
as rodadas iniciais reutilizavam prefixo entre fills maiores. `cache_n` identifica
esse caso; não se usa esse TTFT como prefill integral. O corpus de recuperação
é repetitivo e contém uma agulha; isso não mede qualidade geral em contexto longo.

## Falhas e limites já confirmados

- IQ3 dual residente sem stage-dense: guard de swap durante carga (+2,055 GiB).
- IQ2 976k na release nova: guard térmico em GPU0 (85 °C); pontos frescos de
  49.552 e 249.547 tokens concluíram, mas o quase cheio não foi aprovado.
- IQ2 256k, repetição com prefixos novos: guard de swap (+2,080 GiB).
  Picos VmSwap do nativo/Python foram aproximadamente 1.442/100 MiB.
  MemAvailable permaneceu acima de 36 GiB; esse valor não neutraliza o guard.
- IQ3 GPU1 prefill1024: guard de swap durante carga; o braço 2048 dependente
  foi pulado. O controle 512 concluiu todos os fills.
- O teste IQ3 resident-stage-dense inicial coincidiu parcialmente com um build
  e testes CUDA. Seus números são exploratórios; a rodada `dual-prefill512`
  é a repetição isolada usada para decidir.
- O harness estrito de tools exige garantias que a API ainda não implementa.
  A rejeição explícita é correta, mas impede qualificação de agente. Smokes
  automáticos com o subconjunto suportado são registrados separadamente.
- `ple_parity` exige fixtures externas ausentes e `platform_memory_test`
  requer mlock acima do limite local de 8 MiB. Ambos têm label external_fixture;
  não estão entre os testes nativos aprovados. Nenhuma paridade externa foi
  presumida ou declarada aprovada.

## Calibração e decisões

A execução ficou **parcialmente concluída** quando a sessão foi transferida para
um sandbox sem dispositivos NVIDIA, sem sockets TCP e sem escrita em
`~/.config/model-loader`. `nvidia-smi` falhou nesse ambiente; isso não demonstra
falha do driver da workstation. Os processos anteriores não estão visíveis
neste namespace, portanto seu estado atual não foi inferido. Não foi tentado
contornar essas restrições.

Os valores abaixo são de rodadas exploratórias no hardware, com três chamadas
de código por rodada. Não são três reinicializações independentes alternadas.
Diferenças de texto/aceitação também aparecem entre braços, apesar da seed fixa.

| Rodada | Código tok/s, mediana (mín–máx) | TTFT fresco ~29k, s | Pico GPU0/1, MiB | Crescimento máximo swap, GiB |
|---|---:|---:|---:|---:|
| iq2-baseline-32k | 132.0 (124.0–132.4) | 10.12 | 21477/22581 | 1.052 |
| iq2-final-32k | 131.4 (125.5–131.5) | 9.95 | 21481/22583 | 0.000 |
| iq2-stage-dense | 131.6 (123.8–131.9) | 9.92 | 19995/21099 | 0.000 |
| gpu1-prefill512 | 86.5 (66.0–88.6) | 67.54 | 897/23085 | 0.928 |
| dual-prefill512 | 108.3 (107.7–108.5) | 18.55 | 22987/23083 | 0.000 |
| dual-prefill1024 | 118.2 (109.5–120.2) | 17.83 | 22981/23079 | 0.000 |
| dual-prefill2048 | 120.1 (107.0–121.4) | 11.12 | 22989/23081 | 1.514 |
| dual-pool8 | 120.1 (107.6–122.7) | — | 22935/23031 | 0.733 |
| dual-pool12 | 119.2 (108.4–121.1) | — | 22935/23031 | 0.000 |
| dual-ple8 | 107.5 (106.3–108.0) | — | 22933/23031 | 0.000 |

Nos IQ2 baseline/stage-dense iniciais, o último fill pode reaproveitar prefixo;
consultar `cache_n` antes de comparar TTFT. O fill IQ2-final-32k e os três
prefills IQ3 dual têm prefixos distintos e `cache_n=0` nos prompts frescos.
Todos os testes de exact-copy/JSON/agulha concluídos dessas linhas passaram.
A linha PLE8 tem sete respostas completas e `sampler.stop`, mas não recebeu
um código final na cópia de `matrix-status.json`; manter essa diferença.

**S10 — memória:** há atribuição parcial ao nativo/Python, fases e contadores
preservados em `resources.jsonl`. A pressão de outros aplicativos não foi
atribuída integralmente. Nenhuma mudança de swappiness, THP, page cache global
ou política de pré-leitura foi promovida. A tentativa GPU1/1024 terminou com
pico de +3,918 GiB após acionar o guard: a parada via CLI aguardou o startup.
O probe agora possui encerramento emergencial restrito ao grupo Setsid do
servidor que usa exatamente o config testado, com verificação de argv, grupo,
sessão e start ticks. Um teste real com três processos verifica que os grupos
com outro config e sem Setsid são preservados. O sampler também falha fechado
quando perde telemetria. Esse novo abort ainda precisa ser observado no hardware.

**S12 — prefill:** 512/1024/2048 dual passaram uma rodada; 2048 foi o mais rápido
no maior fill (11,12 s), com swap +1,514 GiB. No GPU1, 1024 falhou durante
carga e 2048 foi pulado pela dependência. Não houve seleção de default baseada
em rodada única.

**S13 — ranking:** utilitário e fixture aprovados. O profile com dump-routing
foi preparado, mas treino real e avaliação em corpus separado estão pendentes.
A base compartilhada não foi substituída.

**S14 — CPU/PCIe:** 8/12/15 workers medidos exploratoriamente. Fração PCIe
0/0,28/0,6 foi alternada no mesmo processo, três requests por valor: medianas
121,9/121,8/120,3 tok/s. Não se demonstrou vantagem para alterar a referência.
Máscaras de threads reais e A/B isolado da correção de afinidade ficam pendentes.

**S15 — MTP:** spec_min_p 0,5/0,7/0,9, alternado no mesmo processo, teve medianas
120,0/127,0/120,5 tok/s no código curto. Não generalizar para PT/tools. Janelas
spec2/spec6, paridade `STRATA_IQ_MT_MIN=1` e avaliação do prefill em lote no
último stage ainda precisam de execução. Nenhuma alteração de kernel MTP foi
promovida com base apenas nessa taxa de aceitação.

**S17 — CUDA:** timers de verify/CPU/draft/commit estão arquivados; o candidato
Nsight foi preparado, mas a captura não ocorreu. Fusão de kernels continua
condicional ao profiling e a um oracle numérico; não está implementada.

**S18 — PLE:** 8 e a referência 16 threads têm dados; 32 está pendente, e 64
não tem autorização experimental por tendência demonstrada. Nenhuma tabela
foi deduplicada/movida: identidade completa não foi provada para esse propósito.

**S19 — conversas:** A→B→A com ~5,6k tokens não reutilizou prefixo, abaixo do
intervalo de checkpoint de 16.384. Não prova defeito no cache. Um teste acima
desse limiar foi preparado. Parking dual permanece desabilitado (orçamento 0),
pois a predominância desse workload e orçamento conjunto não foram demonstrados.

**S20 — P2P:** no IQ3 dual, o log acumulado mediu host staging em torno de
0,22 ms/window no stage inicial, frente a ~22 ms/window nas últimas janelas.
Isso não mede isoladamente todos os bytes e sincronizações de handoff.
Não sustenta protótipo P2P nem promessa de ganho; a captura Nsight está pendente.

## Validação do código

Na workstation, antes das últimas extensões de fixtures/metadados, passaram:
`go test ./...`, `go vet ./...`, build; 94 testes Python do servidor (89 pass,
5 skip); 39 testes nativos da release, excluídas explicitamente as duas fixtures
externas. Evidência: `go-all-fixed.log`, `go-vet-fixed.log`,
`go-build-fixed.log`, `python-third.log`, `build-third.log`.

Na continuação restrita, sobre o código atual:

- `make build` e `go vet ./...`: passaram (`build-local.log`, `vet-local.log`).
- Go focado em monitor/métricas, seed exata até uint64 máximo e encerramento de
  grupo com filho sobrevivendo ao líder: passou (`go-local-focused.log`).
- Packer/tokenizador/ranking: 14 testes passaram (`python-tools-local.log`).
- Controles/cancelamento/filho silencioso/config gerenciada sem sockets:
  10 testes passaram (`python-local-focused.log`). Inclui rejeição de inteiros
  gigantes sem overflow na validação Python.
- Guard de processo: 1 teste passou (`guard-local-tests.log`).
- Native: 9 testes CPU passaram; 1 fixture dual foi explicitamente pulada por
  ausência de duas GPUs (`native-local-tests.log`). A fixture dual compilou e
  cobre união, empréstimo/refill, budget rejeitado e exchanges em devices
  distintos, mas **não foi executada no hardware**.
- Reexecuções integrais Go/Python foram bloqueadas por `socket: operation not
  permitted`. Saídas completas preservadas em `go-resume.log` e
  `python-resume.log`; isso não é apresentado como suíte aprovada.

O código atual inclui pequenas melhorias posteriores à release medida
(validação de inteiros gigantes, fsync de diretórios, proveniência de testes/diff
no build e fixtures adicionais). Portanto a release medida não é declarada
idêntica a toda a árvore atual. Uma nova publicação ainda exige o gate completo.

## Promoção, rollback e continuação

**Nenhum candidato foi promovido aos seis profiles canônicos.** Falhas de guard,
976k incompleto, fixture dual não executada, matrizes incompletas e ausência de
A/B alternado impedem essa promoção. IQ2/IQ3 continuam com a ressalva de uso
como agente; o IQ3 repetiu a chamada na continuação do smoke suportado.
O catálogo de baseline isolado é a referência preservada.

Rollback de profile: restaurar o JSON correspondente de `baseline/profiles`
pelo comando `model-loader profile edit <id> --file <backup>`, após descarregar
qualquer candidato. Preservar a entrada `strata-fork` apontando para
`releases/baseline-20261001/serve/server.py` e seu executável nativo original.
Não restaurar todo o catálogo indiscriminadamente, pois isso apagaria entradas
criadas posteriormente ou alterações alheias ao trabalho.

`pending-validation.json` lista os experimentos restantes. `matrix.json`,
`extra-experiments.json` e `prepared-config-snapshot/` preservam os parâmetros.
Continuar somente em sessão com as duas GPUs, sockets locais e os diretórios
de estado/configuração acessíveis. Usar um diretório de saída novo por tentativa:

```sh
python3 scripts/strata-performance-probe.py <candidate-id> --out <new-evidence-dir> --fills
python3 scripts/strata-summarize-probes.py docs/reports/strata-implementation-2026-10-01
```

Completar o A/B alternado (A,B,B,A,A,B), controles funcionais e guards antes de
atualizar os IDs existentes. Os profiles experimentais continuam presentes e
identificados como candidatos; não foram removidos porque a configuração está
fora dos diretórios graváveis desta sessão. Não houve remoção de modelos.


## Continuação com GPU (Claude, 2026-10-01, noite)

Executada com acesso efetivo às duas GPUs, ao proxy TCP e a `~/.config`.
Relatório completo em `CLAUDE_COMPLETION.md` e evidência em `claude-session/`.
As seções acima continuam valendo como registro da fase anterior.

- **Gates sobre a árvore atual:**
  - `go test ./...`, `go vet` e `make build`: rc 0.
  - Python: 94 testes do server (5 skip), 84 de tools e 1 do guard.
  - Nativo: 40/40 (v3) e 41/41 (v4), **incluindo `file_expert_source_dual_resident`
    executada nas duas GPUs**.
  - `ple_parity` e `platform_memory_test` seguem fora (fixtures externas).
- **Releases:**
  - v3 `20261001T222137Z-3430cc1a87e0` (`strata-perf-v3-20261001`).
  - v4 `20261001T223017Z-f85ddc896414` (`strata-perf-v4-20261001`), que é a v3
    mais o governador térmico opt-in do prompt (`STRATA_PREFILL_TEMP_PAUSE_C`).
    A árvore atual não tem drift em relação à v4.
- **Matriz:**
  - spec2 e spec6 rejeitados.
  - PLE32 sem efeito.
  - PLE8 reconciliado: completo, sem repetição.
  - IQ4 dual resident-stage-dense rejeitado (guard de swap na carga).
- **Ranking (S13):** treino por checkpoint e holdout A/B (3+3 starts). Hit do
  `code-0` 96,7 → 98,6%, experts na CPU por janela-camada −55%, trocas do
  tier −45% e custo por slot −2 a −8% nos primeiros requests.
- **CPU/MTP:**
  - `STRATA_IQ_MT_MIN=1` sem custo e sem determinismo.
  - `spec-min-p` 0,7 empata no código e dá +4–5% no PT.
  - Máscaras reais confirmam a correção S08.
- **Profiling:**
  - Fusão de decode e P2P rejeitados por evidência.
  - MTP em lote no último stage com teto de 2,6–5,2% do TTFT fresco.
  - Hotspot do prefill: dequant (19,5%).
- **Conversas:** A→B→A a 21k sem reuso (12,3 s), limite documentado, sem defeito.
- **IQ2 256k (protocolo com resfriamento antes de cada fill, 9 starts, sem guard):**
  - A release v4 reduz o prefill fresco em 6–8%.
  - `--stage-dense` dá +4% de decode, residência integral e −1,3 GiB na GPU0.
  - **Não foi promovido:** o server v4 recusa `parallel_tool_calls=false` e
    `tool_choice` forçado (S03), o que inutiliza o harness estrito de tools
    e muda o contrato para clientes.
- **976k:** o governador manteve a GPU0 em ≤82 °C (249.547 tokens frescos
  em 103,4 s com recuperação exata). As duas tentativas válidas pararam no guard
  global de swap por pressão de memória de outra sessão de agente na
  máquina; o quase cheio segue não aprovado.
- **Memória (S10):** abort emergencial observado no hardware (sem
  provocação), restrito ao grupo correto. Na carga de IQ3/IQ4 residentes há
  reclaim e compactação diretos; no 976k, kswapd sob pressão de outros processos.
- **Promoção:** nenhum dos seis profiles canônicos foi alterado. A retomada
  exata está na seção 9 do relatório de continuação.

## Continuação 2026-10-02: tools e promoção do IQ2 256k (Claude)

- **Servidor v6** (`strata-perf-v6-20261002`, release `20261002T100952Z-2eed88f1f51d`;
  nativo idêntico ao da v4):
  - `tool_choice` forçado (OpenAI `required`/nomeado, Anthropic `any`/`tool`)
    é honrado com uma abertura de chamada forçada no prompt e exige thinking
    desligado; `required` com um único tool força aquele tool.
  - `parallel_tool_calls=false` e `disable_parallel_tool_use` devolvem só a
    primeira chamada completa e encerram a geração.
  - Chamada forçada incompleta ou com nome não oferecido → erro explícito.
  - Gates: Python 104 (5 skip), tools 84, nativo 41/41 com fixture dual,
    Go test/vet rc 0.
- **Gate de tools pré-registrado aprovado.** No candidato v6: integrity 4/4
  (com stream) e auto 30+30 com 30 corretas e 0 espúrias, igual ao canônico
  (30/30 e 0/30). Controles iguais ou melhores (stop Unicode só passa na v6);
  continuação igual (repete a chamada nos dois).
- **Promovido:** `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k` →
  v6 + `--stage-dense`, ID e contexto preservados. Backup e rollback em
  `claude-session/promotion/iq2-256k-20261002T072035/`.
- **Confirmação:** 130,3 tok/s; fills frescos de 65k/130k/235k em
  18,6/36,2/72,0 s, exatos; pico de 21.612/22.957 MiB; sem guard.
- Os outros cinco canônicos seguem no baseline.
- Detalhes, incluindo o travamento da máquina às 07:34 (causa indeterminada,
  sem carga Strata no momento), em `CLAUDE_COMPLETION.md` §11.
- **976k (tentativa 4, v6):** 499.551 tokens frescos em 277,7 s, exatos, com
  GPU0 ≤82 °C. Parou no fill de 90% pelo guard global de swap, sob pressão de
  outra sessão de agente; a VRAM de desktop na GPU0 também cresceu. Permanece
  no baseline; repetir em janela dedicada.
- **Baseline de contexto longo:** o 976k foi substituído por
  `…-w15-500k` (512.000 tokens, YaRN fator 2 conforme o card, v6 +
  stage-dense + gate térmico), por decisão do operador. Quase cheio validado:
  460.341 tokens frescos em 243,4 s com recuperação exata, GPU0 ≤83 °C.
  Tools: integrity 4/4; auto com 4/30 espúrias (não qualificado como agente).
- **Deploy:** binário do Model Loader instalado e proxy reiniciado
  (S05/S07 ao vivo), com smoke test OK e rollback arquivado.
- **S18:** com prompts diversos de 58k, `STRATA_IO_THREADS` 8/16/32 não muda o
  TTFT (19,4/19,1/19,4 s), porque a espera do PLE se sobrepõe à GPU. O padrão
  16 foi mantido.
- **Rerank no IQ3 GPU1-resident (S13 na outra colocação):** o candidato de um
  único knob (`--expert-profile` = rerank v1, v6) acelera só o começo. No 1º
  request, o hit vai de 69,7 para 89,7% e o decode de 57,4 para 63,7 tok/s;
  no code-0, de 68,2 para 74,2 tok/s. Depois disso empata.
  - Controles: integrity 4/4 em todos os starts e auto igual ou melhor.
  - A continuação do harness reemitiu a tool em 3/5 starts contra 0/3 do
    base.
  - **Canônico GPU1 não alterado** (`CLAUDE_COMPLETION.md` §15).

## Continuação 2026-10-04: A/B final do Strata v7 e decisão de promoção

- **Release avaliada:** `strata-perf-v7-20261004`, release
  `20261004T050705Z-868e757a424d`, engine upstream `0.1.38` com o port local.
  Os seis braços principais (`A1/B1/B2/A2/A3/B3`) por tamanho concluíram todos
  os requests, sem guard, sem invalidação de ciclo de vida e com recuperação
  exata (`LARANJA-7391`).
- **256k:** V6 mediana de `131,2 tok/s`; V7 `141,4 tok/s` (`1,0777x`). A
  aceitação MTP foi `0,895` nos dois braços. As razões de TTFT fresco V7/V6
  foram `0,9705`, `0,9766`, `0,9758` e `0,9873` nos fills de 5%, 25%, 50% e
  90%. Picos V7: `21.921/22.956 MiB` (GPU0/GPU1); recuperação exata.
- **500k:** V6 mediana de `127,8 tok/s`; V7 `137,5 tok/s` (`1,0759x`). A
  aceitação MTP foi `0,868` no V6 e `0,861` no V7. As razões de TTFT fresco
  V7/V6 foram `0,9803`, `0,9791`, `0,9935` e `0,9849`. Picos V7:
  `22.985/23.014 MiB`; recuperação exata.
- **Gate de ferramentas:** 256k V7 passou o auto-harness nos três starts
  (`0,0,0`), mas falhou o integrity-harness nos três (`1,1,1`): os quatro
  checks HTTP/streaming passaram, porém a continuação detectou
  `reissued_tool_call`. 500k V7 passou integrity em dois starts e falhou em um
  (`0,0,1`), e falhou auto nos três (`3,3,3`). A falha de qualidade não foi
  convertida em mediana válida nem ignorada.
- **Decisão:** nenhum tamanho foi promovido. O critério de promoção exige
  todos os gates de ferramenta com retorno zero; os perfis canônicos permanecem
  no V6. Nenhuma chamada pública foi feita para um perfil V7.
- **Idle-unload:** gate funcional passou no candidato 256k com
  `idle_unload_s=120` temporário: `/health` reportou `loaded=false`, VRAM caiu
  de `21.729/22.890` para `1.111/18 MiB`, o PID `3717551` permaneceu estável e
  a recarga respondeu `ok` com TTFT `10,17 s`. O primeiro smoke falhou apenas
  por consultar exatamente aos 150 s, antes da thread de unload executar; o
  gate foi corrigido para esperar a transição observável por condição. A opção
  não foi deixada habilitada em nenhum perfil.
- **Limpeza e rollback:** os dois perfis candidatos, quatro sidecars e dois
  locks foram removidos; não restaram referências candidatas em
  `~/.config/model-loader`. As entradas de catálogo
  `strata-perf-v6-20261002` e `strata-perf-v7-20261004`, os releases V6/V7 e as
  evidências em `claude-session/runs/` foram preservados.
- **Estado final:** o perfil canônico
  `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k` foi restaurado no
  V6, PID `3929652`, e respondeu `ok` pelo proxy `127.0.0.1:4321`. O binário
  ativo foi confirmado como
  `releases/20261002T100952Z-2eed88f1f51d/serve/server.py`.
