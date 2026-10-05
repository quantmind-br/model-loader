# Qwen3.8 Flash-Next: resultados locais de tuning

## Estado

Rodada extensa executada em duas RTX3090, Ryzen9950X3D e64GB RAM. Contexto configurado262144; ambos finalistas testados com244116 tokens reais sem truncamento. Não é prova de ótimo global nem avaliação abrangente de qualidade. Resultados e scripts brutos estão em `.pi/qwen-flash-tuning/`. Não houve alteração de power limit, clocks ou firmware.

### Local cleanup — 2026-10-04

The following sections describe the original tuning campaign, not the current
installation. The six `llama.cpp-qwen4exp-{cache,direct,mtp,pinned,qsa,tuning}`
checkouts and `ik_llama.cpp-tuning`, their seven catalog entries and their schemas
were removed after confirming that no current profile or process used them.
Model weights, historical measurements and the other backends were not removed.

Recovery archive:
`~/.local/state/model-loader/cleanup-backups/qwen-flash-tuning-20261004T110659Z/sources-with-git.tar.zst`.
It preserves all seven source trees, complete Git repositories, local changes
and new files; disposable `build/` directories and Python bytecode are excluded.
An extraction check confirmed identical revisions, local diffs and untracked
files; Git object connectivity and archive checksums passed. Catalog and schema
snapshots, checksums and the cleanup manifest are in the same backup directory.
The archive uses paths relative to `backends/`; extract into a separate recovery
directory before rebuilding or registering any historical backend.

The measured allocation under `backends/` decreased by 4.42 GiB; net recovery
after retaining the 0.71 GiB source backup was 3.72 GiB. All 14 current profiles
remained unchanged and valid;
the canonical Strata v6 process retained its PID and answered `ok` through the proxy.

## Finalistas mantidos no catálogo

- `qwen3.8-flash-next-ud-iq4xs-cache96-physical-nothink-256k`: backend `llama-cpp-qwen4exp-cache`; UD-IQ4_XS, K-only, PLE concorrente, QSA gather, cache96, ubatch512,16 núcleos físicos, sem thinking/MTP. Favorece decode curto/médio.
- `qwen3.8-flash-next-iq4kt-ik-fifo-ub1024-256k`: backend `ik-llama-cpp-tuning`; IQ4_KT, PLE diferida, ubatch1024, FIFO32 checkpoints, tolerância5, correção EOS, sem thinking/MTP. Favorece ingestão e contexto longo.
- Original `qwen3.8-flash-next-ud-iq4xs-mtp-ngram-lazy-layer-256k` preservado para rollback. Não alterado como consequência da seleção.

Profiles intermediários criados nesta rodada arquivados em `.pi/qwen-flash-tuning/archived-profiles/`; pesos e builds preservados. Demais famílias no catálogo mantidas. Nenhum default global foi mudado.

## Comparação equivalente sem thinking

Média de três repetições após warmup, mesmo harness/corpus e geração256 tokens (128 na última faixa). Prefixos reutilizados: TTFT abaixo NÃO é ingestão inicial.

| Tokens reais | Cache96 decode tok/s | ik FIFO decode tok/s | ik FIFO TTFT ms |
|---:|---:|---:|---:|
|12592|38.69|32.56|110|
|62291|31.36|29.86|180|
|124651|25.38|27.12|253|
|224101|19.82|24.07|388|

Raw completion de244116 tokens, registros únicos, fatos nas posições aproximadamente25/50/75%, sem truncamento:

| Configuração exata | Prefill tok/s | Tempo de prefill | Recuperação |
|---|---:|---:|---|
|Cache96 physical no thinking|154.00|26m25s|3/3 valores|
|ik corrigido FIFO ubatch1024|290.58|14m00s|3/3 valores|

O endpoint raw pode incluir tags de thinking antes do JSON: recuperação passou, formato JSON estrito raw não. Os testes de chat estruturado usam outra rota e passaram. Respostas near-full têm comprimentos diferentes: não usar seu decode curto como ranking causal.

## Estratégias implementadas/testadas

- PR28330 K-only indexer: eliminou fallback pipeline observado, mas buffers adicionais consumiram memória liberada. Ganho curto isolado ~1%, não significativo.
- PR28136 PLE concorrente: adaptada ao loader MTP preservando mtp_flags/n_layer_all. `LLAMA_ARG_LAZY_MODE=on-direct` usado pois enum curado não reconhece opção do fork.
- PR28213 QSA: A/B no mesmo binário, sem MTP, chegou a+35% decode em224k (11.95→16.13tok/s). Path single-token não cobre toda verificação MTP.
- PR27861 cache experts:64 e96 slots testados, limite2 uploads/passo. Cache96 melhor decode; cache64 deixa espaço para ubatch1024.
- PR28223 pinned parcial:2 e4 blocos testados.4 não melhorou decode/expansão proporcionalmente e usa mais RAM; não selecionado para finalistas.
- Threads8/16/24:16 melhor observado. Afinidade16 físicos/CCD0/CCD1:16 físicos marginalmente melhor, CCD1 pior.
- Ubatch256/512/1024: cache96+1024 falhou por OOM no carregamento; cache64+1024 passou e melhorou prefill. ik+1024 passou e melhorou prefill.
- Drafter GPU0 testado: apenas deslocou gargalo de VRAM, sem ganho demonstrado.
- ik IQ4_KT alternativo baixado e tensores inspecionados: IQ4_KT,IQ4_NL,Q8_0,F16,F32; sem tipos abaixo de4bits. MTP IQ4_KT também inspecionado/testado. MTP2 regrediu no corpus testado; não selecionado.
- Thinking ligado esgotou orçamento em tarefa simples; variantes sem thinking passaram nas tarefas delimitadas. Não generalizar para problemas difíceis.

## Defeitos descobertos e correções

### ik: ignore_eos persiste entre requisições

Base `fe215a8ccdce6b844d2a3a3bbde08ae76a6284bf`. `examples/server/server-context.cpp`: restaurar `slot.sparams.logit_bias = default_sparams.logit_bias` antes de aplicar biases da requisição. Antes: sequência normal/ignore_eos/normal/reset terminou stop/length/length/length. Depois:stop/length/stop/stop. Benchmark seguido de qualidade sem reinício passou. Patch e reprodução em `qwen-flash-tuning-artifacts/`.

### ik: eviction de checkpoint aumenta TTFT

Variance removia checkpoint próximo ao fim do prompt; repetições reprocessavam851/3819/mais tokens. Em224k,TTFT23.18s. Configuração FIFO preservou o checkpoint recente e reduziu para388ms. Tolerância0 sozinha não resolveu. Não foi necessário alterar código para isso.

### model-loader: completionTokens agregado zerado

`internal/service/benchmark/llamabench_probe.go`: soma já era acumulada, faltava atribuição `res.CompletionTokens = tgSum / ok`. Asserção adicionada ao teste existente. Suite inteira do pacote e go vet passaram. JSONs históricos permanecem com zero; logs do servidor são a evidência dos tokens gerados. Não reescrever histórico como se tivesse sido medido pelo código corrigido.

## Verificação executada

- Finalistas:12/12 casos de JSON/ferramentas multi-turn pelo proxy e3/3 pequenos programas executados em bwrap sem rede, após benchmark no mesmo processo.
- Programas:merge de intervalos sem mutação, LRU com capacidade zero e eviction, ordenação topológica com ciclos e isolados. Suite pequena, não SWE-bench nem auditoria geral de código.
- Reader PLE: F32/Q8_0/IQ4_XS, ordem/duplicatas/concorrência/vazio/EOF, driver ASan/UBSan. Dados quantizados zerados: não prova dequantização de todo tensor real, nem biblioteca inteira instrumentada.
- `test-arg-parser` executado com sucesso nos builds cache e pinned.
- `go test ./internal/service/benchmark -count=1` e `go vet ./internal/service/benchmark`: sucesso.
- Providers: dry-run revisado, suite do sync, apply e pós-dry-run.12 destinos sem alterações pendentes. Dois novos finalistas adicionados; outros profiles preservados.

## Verificações adicionais e limitações

- Gate completo: `go test ./...`, `go vet ./...` e build do CLI aprovado.
- Patches exportados aplicados a índices temporários das bases declaradas: correspondem aos fontes atuais, incluindo arquivos novos. Receitas e scripts em `qwen-flash-tuning-artifacts/`.
- Regressão ik com ignore_eos, reset explícito, logit_bias explícito e mapa vazio passou; biases não vazaram para requisição seguinte. Defaults de CLI não vazios não receberam teste end-to-end separado.
- Ferramenta multi-turn em181370 tokens: ambos finalistas recuperaram SKU/armazém de posições distintas, chamaram ferramenta corretamente e usaram quantidade/lote retornados. Primeira chamada cache96:1075.754s; ik:534.577s. Follow-up:4.524s/3.637s. Caso sintético único, não benchmark representativo de todas as tarefas.
- Cache0/cache96 no mesmo binário: ambos passaram12 casos JSON/tools e3 programas. Dois programas tiveram texto diferente, mas passaram nos testes; não alegar equivalência bit a bit.
- Cache de filesystem não controlado com drop_caches; ordem sequencial pode produzir aquecimento. Dados near-full sintéticos não substituem repos/conversas reais. Catálogo sincronizado não equivale a smoke test de cada cliente.
- EXL3 e outras alternativas não foram implementadas nesta rodada. Foram testadas duas quantizações/famílias de backend e os patches concretos priorizados; não há prova de ótimo global.
- Nenhuma alteração de clocks/potência/firmware foi feita. Limites de memória respeitados com rollback dos experimentos que falharam. OOM cache96/ubatch1024 permanece configuração rejeitada, não defeito considerado resolvido por omissão.
