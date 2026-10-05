# Reprodução dos finalistas

Os patches são experimentais, não releases upstream. Consulte ../qwen-flash-tuning-results.md para limites e evidências.

## Cache96 llama.cpp

Base revision: see `llama-base-revision.txt` (MTP fork PR #28243). The original
checkouts were removed on 2026-10-04. Recover the source and Git history from the
verified archive documented in [the tuning report](../qwen-flash-tuning-results.md#local-cleanup--2026-10-04);
do not assume this revision is available in mainline. Rebuild before re-registering.

```sh
git checkout --detach "$(cat /caminho/llama-base-revision.txt)"
git apply --check /caminho/llama-cache-combined.patch
git apply /caminho/llama-cache-combined.patch
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=86-real -DGGML_NATIVE=ON -DLLAMA_BUILD_TESTS=ON
cmake --build build --target llama-server test-arg-parser -j 4
./build/bin/test-arg-parser
```

O patch inclui os headers/sources novos do reader e cache. Não basta salvar somente git diff sem os arquivos não rastreados.

## ik EOS fix

Base: fe215a8ccdce6b844d2a3a3bbde08ae76a6284bf de ikawrakow/ik_llama.cpp.

```sh
git checkout --detach fe215a8ccdce6b844d2a3a3bbde08ae76a6284bf
git apply --check /caminho/ik-request-local-logit-bias.patch
git apply /caminho/ik-request-local-logit-bias.patch
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DGGML_CUDA=ON -DCMAKE_CUDA_ARCHITECTURES=86 -DGGML_NATIVE=ON
cmake --build build --target llama-server -j 4
```

Sempre conferir ldd: libs do build correto, não _isolated_libs antigas. Recipes reproduzem opções usadas; builds de reconstrução limpa não foram repetidos a partir destes arquivos exportados.

## Scripts de avaliação

Scripts originais mantêm caminhos relativos `.pi/qwen-flash-tuning` e endpoint local4321. Executar da raiz model-loader, não deste diretório. `quality.mjs` e `code-quality.mjs` recebem profile id; `near-full.mjs` e `repro-ignore-eos.mjs` usam backend atualmente carregado. Conferir /_status ANTES de execução. Scripts mudam estado de cache e consomem muitos tokens; não rodar contra produção compartilhada.

`code-quality.mjs` usa bwrap sem rede, mounts read-only e timeout; testes são pequenos, não prova de segurança de código arbitrário. `test-lazy-reader.cpp` é driver independente; quantizados zerados limitam cobertura.

`final-comparison.json` preserva métricas históricas, inclusive completionTokens zerado pelo bug do harness posteriormente corrigido.
