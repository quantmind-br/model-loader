# Revisão de confiabilidade e observabilidade — módulo de benchmarks

**Escopo:** `internal/service/benchmark` (todos os 12 modos registrados), `internal/service/benchmarkstore`, disparo via CLI (`internal/cli/benchmark.go`) e via TUI (`internal/ui/pages/benchmark_*.go`), e a configuração compartilhada (`internal/app/benchmark_config.go`, `internal/config/config.go`).
**Objetivo:** reduzir falhas intermitentes em ambiente real (proxy, GPU, judge, subprocessos, cancelamento) e garantir que cada item/problema deixe rastro claro de **fase** (inferência / grading / harness) e **erro acionável**. Não é meta que todo item complete sempre; é meta que toda falha seja **atribuível, não-silenciosa e não-cascateante**.

**Definição de "melhor" usada neste relatório:**
1. Uma falha transitória de infraestrutura (swap de modelo, crash de backend, judge fora do ar, Docker lento) custa no máximo o item em que ocorreu — nunca deprime silenciosamente o score de itens subsequentes nem aborta o run inteiro sem necessidade.
2. Depois de um run ruim, o operador consegue responder "qual item falhou, em qual fase, com qual erro" **sem** reproduzir o run — via run JSON, transcript ou log do app.
3. Runs parciais/degradados são visivelmente distintos de runs completos em toda superfície (dashboard, compare, `--list`, gate `--min-solve`).

Notação de prioridade: **P0** = maior retorno de confiabilidade/diagnóstico; **P1** = importante; **P2** = opcional/refino.

**Status de implementação (2026-07-08):** os defeitos confirmados desta revisão foram registrados como série **BR1–BR8** em `BUGS.md`/`AGENTS.md` e **corrigidos** nesta sessão (testes de regressão em `internal/service/benchmark/br_regression_test.go` e `internal/cli/benchmark_test.go`): BR1 = falha de grading em ragas/summary vira `res.Err` (§T2, §2.4); BR2 = run 100% falho vira erro de run, fora do leaderboard, e `--min-solve` mostra itens errados (§T6); BR3 = exit não-zero do harness com resultado incompleto vira parcial (§2.9); BR4 = codegen falha em `Prepare` sem `python3` (§2.3); BR5 = extensão de deadline por prefill no longctx (§T3/§2.6); BR6 = llama-bench degrada para reps colhidas (§2.7); BR7 = output do harness em arquivo `<state>/benchmark/harness/<modo>-<ts>.log` com tail limitado em RAM (§T7/§2.8); BR8 = lista de modos da CLI derivada de `ModesInOrder()` (§3). As demais recomendações abaixo — **T1** (logging `slog` estruturado), **T3** (defaults `timeout_sec`×`max_tokens`), **T5** (campo `FailPhase`), **T7** (persistência incremental do run) e todas marcadas P1/P2 sem BR — permanecem propostas ainda **não implementadas**.

**Atualização (2026-07-08, 2ª rodada):** as recomendações transversais **T1, T3, T5 e T7 também foram implementadas** (testes em `internal/service/benchmark/reliability_regression_test.go`). Os trechos abaixo que as descrevem como "não implementadas" estão superados:
- **T1** — `benchmark.Config.Logger` (fallback `log.Nop()` via `Runner.logger()`); logs estruturados `benchmark_run_start`/`_backend_loaded`/`_run_done`/`_run_partial`, `benchmark_item_failed` (por item, com `run_id`/`problem_id`/`phase`) e `benchmark_harness_start`/`_exit` (agentic). Logger injetado no CLI e na TUI.
- **T3** — deadline por item = `Timeout + MaxTokens/decodeFloorTPS` (`decodeFloorTPS=15`), via `Runner.inferTimeout()`, aplicado a todas as chamadas de inferência do modelo-sob-teste (judge/ragas/summary/math/mmlu/codegen/instruction); chamadas de grading mantêm o `Timeout` base. Defaults de config **não** alterados — o mismatch some porque o deadline agora escala.
- **T5** — campo aditivo `ProblemResult.FailPhase` (`infer`/`score`/`harness`), setado em todo site que grava `Err`.
- **T7** — `RunConfig.Checkpoint`: persistência incremental (throttle 15 s) dos modos in-Go seriais (o choke point `executeSerialBench`: math/mmlu/codegen/ragas/summary/instruction), com o parcial marcado `Err="in progress"` (excluído do leaderboard) até o save final sobrescrever; ligado no CLI e na TUI. **judge** (loop próprio com scoring concorrente) e os modos **agentic** seguem com persistência só ao fim — o judge é uma série limitada (SWE-bench Lite), e os agentic já têm artefatos reais em disco via BR7. **T8** e refinos P2 continuam como proposta.

---

## 1. Temas transversais

### T1 (P0) — O pacote `benchmark` não emite nenhum log estruturado

Não há uma única chamada `slog` em `internal/service/benchmark/*.go` (verificado por busca). Todo rastro em tempo de execução vive em dois canais efêmeros: eventos `Progress` (best-effort, com *drop* — ver T6) e a string de erro final do run. Isso viola a convenção da própria camada de serviço do repo (§7 do `AGENTS.md`: todo serviço recebe `Config` + `WithLogger`, com fallback `log.Nop()`), que `benchmark.Config` (`config.go:7-177`) não segue — não existe campo de logger nem como injetá-lo.

Consequência prática: após um run com 40 itens `Err`, o log do app (`~/.local/state/model-loader/logs`) não contém nada — nem o `run_id`, nem quando o load do modelo terminou, nem quando o watchdog matou o harness, nem quais itens falharam.

**Proposta:**
- Adicionar logger ao `Runner` (opção funcional no `NewRunner` ou campo em `Config`), com `run_id` como atributo correlacionador em todas as linhas.
- Logar, no mínimo: início/fim do run (perfil, modo, config efetiva de timeout/max_tokens/limit); `ensureLoaded` (duração do load, PID, `reused`); por item, **falhas** com id + fase + erro (sucesso pode ficar em nível debug); chamadas de judge que falharam; eventos de watchdog (disparo, motivo, SIGTERM/SIGKILL); persistência do run (caminho, parcial ou não).
- CLI e TUI já têm `svc.Logger` disponível nos pontos de construção (`internal/cli/benchmark.go:238`, `cmd/model-loader/main.go:118`) — é só encanar.

### T2 (P0) — Falha do judge/grader é engolida e vira nota 0 em ragas e summary

Tratamento **inconsistente** entre modos quando a chamada de grading falha (judge externo fora do ar, 429/5xx, timeout, verdict não-JSON):

| Modo | Comportamento em falha de grading | Evidência |
|---|---|---|
| judge | `res.Err` setado → item vira `Errored`, **excluído** dos denominadores | `runner.go:271-274` (`scoreProblem`) |
| ragas | `gradeResult{}` (score 0) por critério; erro só vai a `tr.JudgeRaw` (transcript); `res.Err` **não** é setado | `ragasbench.go:95-98` |
| summary | `coherence = 0` silencioso; média `(coverage+0)/2`; erro só em `tr.JudgeRaw` | `summarybench.go:131-141` |
| instruction/refusal | fallback ao heurístico, com `JudgedBy="heuristic"` gravado (aceitável e rastreável) | `instructionbench.go:219-227` |

Em ragas, um judge indisponível durante o run produz um item "respondido" com `faithfulness=0 relevancy=0 precision=0`, contado no denominador e derrubando o `SolveRate` do perfil — indistinguível de um modelo ruim, exceto lendo o transcript (que só existe com `save_transcripts` habilitado). É exatamente o padrão de falha intermitente que o leaderboard depois compara como se fosse qualidade do modelo.

**Proposta:** uniformizar no comportamento do modo judge — falha de grading ⇒ `res.Err` (com prefixo de fase, ver T5), item excluído dos denominadores (`aggregate()` já faz isso via `Errored`, `result.go:220-274`). Se preferirem preservar sub-scores parciais (ragas tem 3 critérios), setar `res.Err` quando **qualquer** critério falhar e registrar quais falharam em `SubScores`/`Detail`. O ponto inegociável: outage de judge nunca pode virar score legítimo.

### T3 (P0) — Defaults incompatíveis: `timeout_sec=120` × `max_tokens=32768`

`config.go:289-291` fixa `benchmark.max_tokens=32768` e `benchmark.timeout_sec=120`. Todo modo de dataset usa `context.WithTimeout(ctx, r.cfg.Timeout)` por requisição com `MaxTokens: r.cfg.MaxTokens` (ex.: `runner.go:232-239`, `codegenbench.go:145-155`, `ragasbench.go:63-72`). Um modelo local a 20–60 tok/s precisa de 9–27 min para preencher 32k tokens; qualquer modelo de raciocínio que use o orçamento estoura os 120 s e o item vira `context deadline exceeded` — a causa mais provável de "timeout intermitente" em ambiente real, especialmente em perfis com reasoning habilitado.

O próprio pacote já reconhece o problema em dois lugares e resolve só localmente:
- llama-bench estende o deadline com piso de prefill (`llamabench_probe.go:103-106`);
- longctx **não** estende, apesar de mandar prompts de dezenas de milhares de tokens (`longcontext_probe.go:156-166`) — um probe de 100k+ tokens (o knob `long_context_tokens` existe para isso) estoura 120 s só no prefill em muitos backends.

**Proposta:** derivar o deadline por requisição de um orçamento composto (`base + MaxTokens/tokens_por_segundo_piso + prompt/prefill_piso`), como o llama-bench já faz, ou pelo menos: (a) subir o default de `timeout_sec` para algo coerente com 32k tokens, (b) aplicar a extensão de prefill também ao longctx, (c) documentar no config que os dois knobs se acoplam. Registrar no erro do item o timeout efetivo usado ("timed out after 120s (timeout_sec)") para o operador saber qual knob girar.

### T4 (P1) — Crash/swap de backend no meio do run queima uma sequência de itens

`Complete` faz retry único e imediato em erro de transporte/5xx (`client.go:68-75`), sem backoff. Se o backend morre no meio do run: a próxima requisição dispara o relaunch implícito do proxy, que bloqueia até o health-check (default 360 s); o `reqCtx` do item expira em 120 s antes disso; o retry (mesmo `reqCtx`, já expirado) falha; item vira `Err`. Os itens seguintes repetem o padrão até o backend ficar saudável — tipicamente 2–4 itens queimados por crash, todos com o mesmo erro genérico de deadline, sem nada indicando "backend estava reiniciando".

Agrava: não há disjuntor. Com o backend permanentemente morto (ex.: OOM em loop) ou judge morto, o loop serial (`handler.go:12-34`) roda **todos** os N itens, cada um esperando o timeout inteiro — um run de 50 itens gasta ~100 min para produzir 50 `Err` e, pior, termina **sem** `run.Err` (Execute retorna `nil`), ou seja, vira "run completo" com SolveRate 0 (ver T6).

**Proposta:**
- Disjuntor de falhas consecutivas: K itens seguidos com `Err` de infraestrutura (deadline/transporte/5xx — distinguível do 4xx) ⇒ abortar com `run.Err` explícito ("aborted after K consecutive infrastructure failures"), preservando o parcial.
- Opcional: antes de marcar item como `Err` por deadline, consultar `/_status` do proxy; se um swap/relaunch está em andamento, esperar a saúde (bounded) e re-tentar o item uma vez — remove a classe inteira de "crash queima N itens".
- Backoff curto (1–2 s) no retry de 5xx do `Complete`, para não re-bater num backend que acabou de morrer.

### T5 (P1) — Nenhum marcador estruturado de *fase* na falha por item

`ProblemResult` tem só `Err string` (`result.go:136`); tanto falha de inferência (`runner.go:240-243`) quanto de scoring (`runner.go:271-274`) escrevem nele o texto cru do erro. Depois do fato, "context deadline exceeded" não diz se foi o modelo-sob-teste ou o judge que travou — e para os modos agentic nem existe granularidade de fase (agent/gather/eval aparecem só no texto do erro do run).

**Proposta:** campo aditivo `FailPhase string` (`"infer" | "score" | "harness" | "sandbox"`) em `ProblemResult` (o arquivo já segue a convenção de campos estruturados aditivos, `result.go:138-151`), preenchido nos dois pontos do runner + nos modos que gradam inline (ragas/summary/instruction, junto com T2). Alternativa mínima: prefixar o erro (`"infer: …"` / `"score: …"`), mas o campo estruturado permite agregação ("run teve 12 falhas de judge, 0 de inferência") no `Aggregate` e nas UIs.

### T6 (P1) — Run 100% errado conta como "completo": leaderboard e `--min-solve` enganam

- `aggregate()` exclui itens `Err` dos denominadores (correto), mas um run em que **todos** os itens falharam retorna de `Execute` sem erro ⇒ `run.Err == ""` ⇒ o dashboard da TUI o trata como o "run completo mais recente" do perfil (leaderboard, `benchmark_dashboard.go`), exibindo SolveRate 0 como se fosse desempenho medido.
- O gate CLI `--min-solve` compara só `run.Aggregate.SolveRate` (`benchmark.go:292-295`): um run com 90% dos itens `Errored` e 100% de acerto nos 10% restantes passa o gate; o inverso reprova por infra, não por qualidade.
- O caso mais concreto: codegen sem `python3` no PATH grava um run de **1 item sintético** com `Err="python3 not on PATH…"` e retorna sucesso (`codegenbench.go:225-230`) — esse run vira "o último run de codegen" do perfil no dashboard.

**Proposta:** definir um limiar de validade (ex.: `Errored/total > X%` ⇒ marcar o run como degradado — seja setando `run.Err`, seja um campo `Degraded bool`) e: excluí-lo do leaderboard como já se faz com parciais; fazer `--min-solve` reprovar (ou avisar em stderr) quando `Errored` ultrapassa o limiar. O caso "modo pulado por dependência ausente" deveria ser um erro de `Prepare` (fail-fast antes de carregar o modelo, como tb/deep-swe/swe-bench-pro já fazem — `terminalbench.go:66-76`), não um run persistido.

### T7 (P1) — Persistência só no fim: crash do model-loader perde o run inteiro

`store.Save(run)` acontece apenas após `Run()` retornar (CLI `benchmark.go:150-164`; TUI `benchmark_run.go:88-124`). Um SIGKILL/panic/queda de energia no meio de um run de horas perde tudo — inclusive nos modos agentic, onde os resultados reais existiam no disco mas dentro de `os.MkdirTemp` com `defer os.RemoveAll` (`terminalbench.go:105-109`, `deepswe.go:123-127`, `swebenchpro.go:132-136`): sem o processo vivo para parsear, o artefato órfão em `/tmp` é efetivamente irrecuperável.

**Proposta:**
- Mover o diretório de trabalho dos harnesses para o state dir (`~/.local/state/model-loader/benchmark/harness/<run-id>/`), retido em falha e removido apenas em sucesso — isso também dá ao operador um caminho estável para inspecionar `results.json`/logs de um run que morreu.
- Opcional (modos de dataset): checkpoint incremental leve — ex.: append de cada `ProblemResult` num sidecar JSONL, promovido a run JSON no fim. Custo baixo, elimina a perda total.

### T8 (P2) — Canal de progresso com *drop* silencioso e vocabulário de fases pobre

`send()` é não-bloqueante e descarta quando o buffer (32) enche (`runner.go:326-334`). Para a TUI (re-arm por mensagem) é adequado; para a CLI, o listener imprime linha por evento (`benchmark.go:265-274`) e eventos perdidos significam "pulos" na numeração — cosmético, mas confunde ("o item 17 rodou?"). As fases são só `launch|infer|score|done`; não existe evento de progresso para "item falhou" nem para eventos de watchdog além de texto no `ProblemName`. Com T1 resolvido, o log vira a fonte confiável e o canal pode continuar lossy — documentar isso no contrato de `Progress` e considerar um evento `Phase:"error"` por item para as UIs marcarem falhas em tempo real.

---

## 2. Por modo

### 2.1 judge (SWE-bench Lite)

- **Robustez boa:** inferência serial + scoring em goroutines com semáforo 2 (`handlers.go:26-85`); degradação graciosa para amostras já colhidas quando uma chamada do judge falha no meio (`scorer.go:78-83`); cancelamento não estranha goroutines (semáforo com `ctx.Done()`, `handlers.go:73-77`); `scoreProblem` tem teto `samples×Timeout` (`runner.go:267`).
- (P1) Falha do judge na **primeira** amostra seta `res.Err` — correto — mas o texto não distingue "judge fora do ar" de "verdict não parseou" (`scorer.go:78-81`, `verdict.go:35-37`). Com T5, marcar `FailPhase="score"` + tipo do erro. O raw do verdict que falhou o parse vai só ao transcript.
- (P2) `judgeScoreConcurrency=2` com judge lento significa até 2×Timeout de cauda depois do último item; ok, mas com T1 vale logar duração média das chamadas do judge para diagnosticar "run lento por judge".

### 2.2 math-bench / mmlu-bench (objetivos)

- Sem dependências externas de grading; falha só por inferência (T3/T4 dominam). Extração heurística (`extractFinalAnswer`, `extractMCLetter`) nunca erra "para fora": resposta inextraível vira unresolved com `Detail` explicativo — adequado.
- (P2) Com T5: um `Err` aqui é sempre `infer`; nada mais a fazer além dos temas transversais.

### 2.3 codegen-bench

- **Sandbox bem construído:** bwrap com fallback a subprocess registrado em `Sandbox` (`codegenbench.go:94-104`), process group próprio + SIGKILL no grupo + `WaitDelay` (`:103-105`), stderr truncado, timeout de execução dedicado.
- (P1) O caso "python3 ausente" gera run de 1 item persistido como se fosse resultado (ver T6); deveria falhar em `Prepare` (`codegenbench.go:205` retorna `nil, nil` hoje; a checagem vive em `Execute:226-230`, depois do load do modelo — exatamente o que o comentário de `Prepare` em `runner.go:162-163` diz que se quer evitar).
- (P2) `codeGenTimeout` fixo em 5 s (`:137`) é justo para testes pesados em máquina carregada (GPU saturada afeta CPU); considerar configurável. Timeout de execução é reportado como `timed out` no `Detail` — rastreável.
- (P2) Falha de `MkdirTemp`/`WriteFile` retorna `pyResult` com stderr sintético mas sem sandbox/fase — cosmético.

### 2.4 ragas-bench / summary-bench

- (P0) **Tema T2 na forma mais aguda**: 3 chamadas de grading por item (ragas) / 1 (summary), qualquer falha vira 0 silencioso. Além do T2, notar que são 3–4 chamadas **sequenciais** por item, cada uma com `Timeout` próprio (`ragasbench.go:88-101`): com judge lento, um item pode legitimamente levar 4×120 s — outro motivo para o vocabulário de fase (o operador vê "item 12 (score)" parado 6 min e acha que travou).
- (P2) Sem judge externo configurado, o self-judge (o próprio modelo-sob-teste avalia a si mesmo, `runner.go:303-309`) roda no mesmo backend — as chamadas de grading competem com a inferência do item seguinte? Não: os modos ragas/summary gradam inline no loop serial, então não há concorrência — mas o custo é o dobro de requisições ao backend por item; `JudgedBy` registra o arranjo (bom).

### 2.5 instruction-bench

- Refusal: fallback heurístico com `JudgedBy="heuristic"` e erro do grader preservado no transcript (`instructionbench.go:219-227`) — **este é o padrão a copiar** para T2 quando um fallback local existir.
- Consistency: fallback embeddings→lexical é silencioso por design mas **rastreável** (`SimMethod`, limiar ajustado, `instructionbench.go:298-305`; `similarity.go:37-45`) — adequado. (P2) A razão da queda para lexical (endpoint `/v1/embeddings` ausente vs timeout) não é registrada em lugar nenhum; um log (T1) resolveria.
- (P2) Consistency faz N amostras sequenciais e **aborta o item** na primeira falha de inferência (`:259-263`) — poderia degradar para os pares já colhidos (como o judge faz com amostras), mas o custo de re-rodar um item é baixo; opcional.

### 2.6 longctx

- (P0/T3) Único modo de prompt gigante **sem** extensão de deadline por prefill (`longcontext_probe.go:156-166`); com `long_context_tokens` alto, timeout é quase garantido. Aplicar o mesmo piso de prefill do llama-bench (`llamabench_probe.go:103-106`).
- Seed gravado para reprodutibilidade (`:148-149`) — bom. `Count()==1` significa que o run inteiro é 1 item: qualquer falha transitória perde o run; aceitável para um probe, mas com T4 (retry consciente de recuperação) ficaria robusto de graça.

### 2.7 llama-bench

- (P1) **Uma falha em qualquer rep aborta o preset inteiro** (`llamabench_probe.go:160-164` retorna no primeiro erro), inclusive quando 2 de 3 reps já mediram com sucesso. Degradar para as amostras colhidas (o mecanismo de "short dropped" já existe, `:169-171,213-215`) e só marcar `Err` quando `ok==0`.
- (P2) Falhas de warmup são descartadas sem rastro (`:123-125`, `_, _ =`); um warmup que falha prediz reps falhando — logar (T1) e considerar abortar cedo o preset com erro claro.
- Overshoot de contexto foi tratado (BM1; margens em `:83-106`) e o `Detail` é exemplar (budget, n, stddev, origem dos timings — `:208-215`).

### 2.8 terminal-bench (agentic)

- **Pontos fortes:** fail-fast de `tb`/Docker em `Prepare` (`terminalbench.go:66-76`); process group + SIGKILL via `cmd.Cancel` + `WaitDelay` (`:138-140`); watchdog de stall/pós-conclusão com semântica correta de sucesso-vs-parcial (`:190-211`, `:254-287`); erro de results.json ausente/corrompido inclui caminho + tail do output (`:169-178`).
- (P0) **Output do harness só em memória** (`strings.Builder`, `:141-143`): um run de horas com log verboso cresce sem teto na RAM; se o model-loader morre, o log morre junto; o operador só vê o *tail* e apenas quando há erro; o log completo exige `save_transcripts`. **Proposta:** tee para arquivo no state dir (junto com T7 — mesmo diretório do run), com o caminho gravado no run/erro e logado. Vale idêntico para deep-swe (`deepswe.go:159-161`) e swe-bench-pro (`swebenchpro.go:147`).
- (P1) Eventos do watchdog (disparo, motivo, SIGTERM, escalada) não deixam rastro persistente — apenas um `Progress` ("all tasks scored — waiting…") e o texto do erro final. Com T1: logar cada transição.
- (P2) `Count` pode ser 0 (dataset ainda não cacheado, `:328-353`) — a UI mostra "problem N" sem denominador (tratado em `progress.go`); o total só firma quando o aggregate results.json aparece. Documentado; ok.
- (P2) Trial com `is_resolved==null` vira "unresolved fail" para manter SolveRate==accuracy do tb (`:483-504`) — decisão consciente; com T5, valeria ao menos distinguir no `Detail`/`FailPhase` os trials que **crasharam** dos que rodaram e falharam (o `tbDetail` já traz `failure_mode` quando o tb o fornece — conferir se é sempre propagado).

### 2.9 deep-swe (agentic)

- Estruturalmente idêntico ao tb pós-UIUX-013 (watchdog compartilhado, `deepswe.go:169-178`); mesmas recomendações (output em arquivo, watchdog no log).
- (P1) `deepParseRunDir` só falha quando **zero** trials existem (`:343-377`); um run onde pier morreu no meio com 10 de 50 trials gravados retorna os 10 como parcial **apenas** se `ctx`/watchdog capturarem a causa — se o pier sair com erro próprio (exit != 0) mas trials existirem, `runErr` de `cmd.Wait` é **ignorado** no caminho de sucesso (`:180`, usado só na mensagem de erro de parse). Ou seja: pier falhando com resultados parciais no disco vira "run completo". Conferir e, provavelmente, tratar `runErr != nil` como parcial (`run.Err`) mesmo com trials parseados. **O mesmo vale para tb** (`terminalbench.go:163`, `runErr` só aparece na mensagem quando results.json falta).
- (P2) `deepAPIBase` reescreve loopback para `host.docker.internal` (`:285-301`) — armadilha operacional conhecida (proxy precisa estar num bind alcançável); um probe de conectividade fail-fast em `Prepare` (tentar `GET <api_base>/models` de dentro de um container é caro; ao menos avisar quando o proxy está em 127.0.0.1 e o override não foi setado) economizaria runs de horas que falham 100% dos trials por rede.

### 2.10 swe-bench-pro (agentic)

- Estágios sequenciais com process group e erro por estágio nomeado ("agent/gather/eval step failed", `swebenchpro.go:263-276`) + tail do output — o melhor rótulo de fase entre os agentic hoje.
- (P1) **Etapa agent sem watchdog de stall** (decisão documentada em UIUX-013 por falta de sinal de progresso confiável) e `timeout_sec` default 0: um agente wedged flooda o proxy indefinidamente. Mitigação barata sem watchdog frágil: aplicar à etapa agent um teto default não-zero (ex.: derivado de nº de instâncias × orçamento por instância) OU ao menos emitir um `Progress`/log periódico "agent step running for Xh" para o operador enxergar o wedge; hoje a fase agent é um único evento estático (`:167`).
- (P1) A armadilha do `raw_sample_path` com colunas UPPERCASE (score tudo False silenciosamente — documentada em `config.go:85-89`) é detectável: fail-fast em `Prepare` sniffando o cabeçalho/keys do arquivo por `FAIL_TO_PASS`/`PASS_TO_PASS` maiúsculos. Custo mínimo, elimina a pior falha silenciosa do modo.
- (P2) `eval_results.json` vazio já é tratado como erro com explicação (`:237-240`) — bom. Verdicts são só `{instance_id: bool}` (`:376-383`): sem `Detail` por instância (qual teste falhou); os `*_output.json` por instância existem no diretório do eval e são descartados com o tempdir — com T7 (dir retido), viram material de diagnóstico de graça.

---

## 3. Disparo — CLI e TUI

### CLI (`internal/cli/benchmark.go`)

- (P1) Mensagem de modo desconhecido está **desatualizada**: `"unknown mode %q (want judge|longctx|llama-bench)"` (`:78`) omite 9 dos 12 modos (o help do flag `--mode` em `:169` está correto). Derivar a lista de `benchmark.ModesInOrder()` para nunca mais drift.
- SIGINT → parcial persistido com aviso (`:150-163`) — correto. (P2) Um run que falha **antes** do primeiro item (launch/Prepare) não deixa nada além de uma linha em stderr; com T1, o log do app cobriria.
- `--min-solve`: ver T6.
- (P2) O listener de progresso ignora a fase `done` e imprime cada evento numa linha nova — em runs de centenas de itens com judge, o stderr fica com milhares de linhas; aceitável para pipe/CI, mas um modo `--quiet`/resumo periódico é refino possível.

### TUI (`internal/ui/pages/benchmark_run.go`)

- Ciclo de vida correto: `context.WithCancel` por run, esc→confirm→cancel (`:144-169`), `Cleanup` no quit cancela e espera até 5 s pelo ack do engine (`:131-142`), parcial persistido com flash distinto ("run incomplete (saved partial)", `:108-112`), fluxo de flash-clear corrigido (UIUX-011).
- (P2) `Cleanup` espera 5 s, mas a escalada SIGKILL dos harnesses via `cmd.Cancel` é imediata no cancel do ctx com `WaitDelay=10s` de força — na prática o grupo morre; o único risco é o model-loader sair antes do `Wait` colher o processo (órfão já morto). Aceitável.
- (P1) O flash de erro mostra só a primeira linha útil do erro; erros de harness com tail multi-linha (`terminalbench.go:172-173`) não são legíveis num flash. Com T7 (dir retido) + T1 (log), o flash pode terminar com "details: <caminho>" em vez de truncar.

---

## 4. Resumo priorizado

| # | Tema | Modos afetados | Ganho |
|---|------|----------------|-------|
| P0 | T1: logger estruturado com `run_id` no pacote benchmark | todos | diagnóstico pós-morte passa a existir |
| P0 | T2: falha de grading ⇒ `Err`/exclusão, nunca nota 0 silenciosa | ragas, summary (e uniformização geral) | scores deixam de ser corrompidos por outage de judge |
| P0 | T3: acoplar timeout a max_tokens/prefill (e aplicar ao longctx) | todos os single-turn | elimina a maior fonte de timeout intermitente |
| P0 | Output de harness em arquivo no state dir (tee), caminho no run/erro | tb, deep-swe, swe-bench-pro | diagnóstico dos modos mais caros; remove risco de RAM |
| P1 | T4: disjuntor de falhas consecutivas + retry consciente de swap/crash | modos de dataset | crash de backend deixa de queimar sequência de itens |
| P1 | T5: `FailPhase` estruturado por item | todos | "qual fase falhou" sem ler transcript |
| P1 | T6: run degradado (tudo-Err / modo pulado) fora do leaderboard e do `--min-solve` | dashboard, CLI gate | superfícies param de mentir |
| P1 | T7: workdir dos harnesses retido em falha; (opc.) checkpoint incremental | agentic; todos | run de horas sobrevive a crash do model-loader |
| P1 | `runErr` do harness ignorado quando resultados parciais parseiam | tb, deep-swe | harness que falhou não vira "run completo" |
| P1 | Fail-fast: python3 em `Prepare` (codegen); sniff de raw_sample UPPERCASE (swe-bench-pro); aviso de api_base loopback (deep-swe) | codegen, swe-bench-pro, deep-swe | erros de configuração falham em segundos, não horas |
| P1 | llama-bench: degradar para reps colhidas em vez de abortar preset | llama-bench | menos runs de velocidade perdidos |
| P1 | CLI: lista de modos do erro derivada de `ModesInOrder()` | CLI | mensagem deixa de esconder 9 modos |
| P2 | Watchdog/warmup/embeddings-fallback logados; backoff no retry; evento de progresso "error"; timeout do sandbox configurável; `Detail` por instância no swe-bench-pro | diversos | refinos de observabilidade |

**O que está bom e não deve ser mexido sem motivo:** semântica de run parcial (`Run.Err` + aggregates + persistência do parcial em CLI/TUI); watchdog compartilhado de stall/pós-conclusão; process groups + `WaitDelay` em todos os subprocessos; exclusão de itens `Err` dos denominadores; campos estruturados aditivos em `ProblemResult`; fail-fast em `Prepare` dos modos agentic; o padrão refusal (fallback + `JudgedBy`) como referência de degradação rastreável.
