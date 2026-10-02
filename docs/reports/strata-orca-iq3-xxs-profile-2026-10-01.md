# OrcaRouter IQ3_XXS: procedimento para criar o perfil

O Strata documenta suporte explícito a
[`orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF`](https://huggingface.co/orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF),
quantização **IQ3_XXS**. O
[`docs/ORCA.md` do upstream](https://github.com/Niko1221/Strata/blob/main/docs/ORCA.md)
foi conferido com a cópia local em 2026-10-01. Esse procedimento não usa o menu
do instalador e não depende da versão IQ2_XS reconvertida.

Após esta análise, o modelo foi baixado, preparado e instalado. Os JSONs dual
abaixo conservam a receita inicial, que acionou o limite de swap nos testes.
A variante que completou a calibração de estabilidade usa GPU1 e residência
complementar em RAM; veja o
[relatório local e seus limites](strata-orca-iq3-xxs-calibration-2026-10-01.md).
A compatibilidade upstream foi testada em uma RTX 5090 de 32 GB e 128 GB de RAM;
suas medições não representam nosso hardware.

## O que a conversão faz

`tools/iq_pack.py --compat-bf16` desquantiza pequenas projeções de atenção,
hyperconnections, routers e PLE que os kernels exigem em BF16, arredondando
com nearest-even. Isso não recupera os pesos originais em BF16. Os experts
continuam em IQ3_XXS gate/up e IQ4_NL down; embeddings, atenção nativa e a
tabela PLE de aproximadamente 28,8 GB permanecem em seus formatos originais.
O pack registra as conversões em `compat-bf16.json`.

Na versão local do packer, acrescentar **`--experts-bin`**: ele materializa
`experts.bin`, exigido pela configuração `--mmap-experts` escolhida aqui.
Sem esse argumento, o packer atual deixa os experts para leitura dos GGUFs
e o modo mmap deste engine recusa o pack.

O nosso packer preserva chaves PLE em formatos nativos suportados, incluindo
IQ3_XXS. O loader e o kernel local aceitam esse tipo. Por isso, o número exato
de pequenas conversões pode diferir dos 460 tensores relatados pelo upstream.
As nove verificações focadas de `tools/test_iq_pack.py` passaram. Elas verificam
o mecanismo de conversão; não substituem a execução do checkpoint completo.

É necessário um **pack independente**, com seu próprio `dense.bin` e tokenizer
exportado do GGUF. Não usar `--base` apontando para packs de outro modelo.
Não renomear os shards para imitar modelos GSQ-RCO. Tanto `--native` quanto
`--ple-gguf` usam o **primeiro shard**, porque a PLE deste checkpoint está nele.

## Arquivos e espaço

Revisão HF consultada: `0434906af7b5202b676d43f108cf4f73d25691ef`.

| Arquivo | Bytes | SHA-256 publicado |
|---|---:|---|
| `Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00001-of-00002.gguf` | 44637691008 | `aaf57046943c6638480e8984835ce5ec29c486180ff71169d3c22c6929851b7b` |
| `Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00002-of-00002.gguf` | 40564977024 | `a19cf9bbe87bce45f312ef11401c88b675f3784c927d02fa2272822229e6a224` |

Os dois shards somam **85,20 GB / 79,35 GiB** segundo a API, acima da estimativa
antiga de aproximadamente 73 GB do model card. Ambos devem terminar de baixar
antes de empacotar. GGUF + pack precisam de aproximadamente **131 GiB** de
disco, considerando cerca de 49,8 GiB de experts e 1,43 GiB de dense no pack.
Ainda são necessárias folgas para download e preparação. Após remover o IQ2_XS
uncensored, há aproximadamente **197 GiB livres** neste filesystem.

## Configuração inicial para o nosso hardware

Os exemplos [profile.json](../examples/strata-orca-iq3-xxs/profile.json),
[engine.json](../examples/strata-orca-iq3-xxs/engine.json) e
[shared-settings.json](../examples/strata-orca-iq3-xxs/shared-settings.json)
usam caminhos absolutos deste workstation e o backend já registrado:

- Backend `strata-fork`, executável e cwd em `model-loader/backends/strata-fork`.
- Contexto inicial **32768**, int8 KV, sem YaRN.
- Duas RTX 3090 com **24/24 camadas**, reserva de 1536 MiB/card e 15 workers.
- Experts por **mmap**, cache GPU automático e prefill inicial **512**, como
  recomendado no procedimento manual. Não herdar prefill auto/8192 sem medir.
- MTP original Q2_0 já preparado em `/home/diogo/models/strata/mtp/rt`,
  spec 4/min-p 0,5, conforme o manual. É compartilhado e não é um head adaptado
  à versão uncensored; medir aceitação e correção.
- Sampling do model card Orca: temperatura 1,0, top_p 0,95, top_k 20, min_p 0.
  O exemplo seleciona modo non-thinking e desativa experimental speed projection.
  O tokenizer/template do próprio checkpoint deve ser verificado após exportação.
- Não configurar `port` no perfil; o Model Loader aloca a porta.

O exemplo original usa uma arena de experts de cerca de 49,8 GiB na RAM,
além de runtime/MTP. Isso é apertado em nossa máquina de aproximadamente
60 GiB. Mmap evita a cópia residente dessa arena e foi a estratégia medida em
outros perfis Strata locais; seu comportamento com este IQ3 ainda precisa ser
testado. VRAM, RAM e swap devem ser monitorados durante o primeiro carregamento
e prompts longos. Manter 270 W/card e o teto de 23552 MiB/card.

Começar com 32k. O setup original limita IQ3_XXS a 128k em máquinas de 64 GB;
essa advertência não estabelece automaticamente 128k ou 256k seguros para nós.
Dois GPUs acrescentam cache de experts e capacidade de KV, mas não eliminam
o working set na RAM. Não prometer desempenho ou janela máxima sem calibrar.

O HF também publica `Qwen3.8-Flash-Next-Uncensored-MTP-draft.gguf` para
llama.cpp. Não passá-lo diretamente em `--mtp`: Strata espera um diretório
runtime próprio, e `mtp_rt.py` exige nomes, shapes e layout Q2_0 específicos.
O manual usa o head original preparado; um head uncensored exige análise e
conversão separadas.

## Comandos para preparar e registrar

Os comandos abaixo documentam a receita inicial. O download e o packing foram
executados; a variante dual registrada ficou experimental. Usar somente os dois
arquivos indicados, sem baixar todas as quants. O `hf` não está no PATH desta
sessão; o download local foi executado com `uv tool run --from huggingface_hub hf`:

```sh
export HF_HUB_DISABLE_XET=1 HF_HUB_DOWNLOAD_TIMEOUT=30 HF_HUB_ETAG_TIMEOUT=30
hf download orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF \
  Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00001-of-00002.gguf \
  Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00002-of-00002.gguf \
  --revision 0434906af7b5202b676d43f108cf4f73d25691ef \
  --max-workers 2 \
  --local-dir /home/diogo/models/huggingface/orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF
```

Verificar tamanhos e hashes com a tabela antes do packing. O `/usr/bin/python`
local tem numpy/regex; o gguf-py correto vem da dependência pinada deste build:

```sh
STRATA_GGUF_PY=/home/diogo/dev/model-loader/backends/strata-fork/build-fork-sm86/_deps/strata_llamacpp-src/gguf-py \
  /usr/bin/python /home/diogo/dev/model-loader/backends/strata-fork/tools/iq_pack.py \
  --gguf /home/diogo/models/huggingface/orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF/Qwen3.8-Flash-Next-Uncensored-IQ3_XXS-00001-of-00002.gguf \
  --out /home/diogo/models/strata/packs/orca-iq3-xxs --compat-bf16 --experts-bin
```

Depois de inspecionar `compat-bf16.json`, índice, formatos de experts e
tokenizer/template, instalar a configuração e registrar o perfil:

```sh
cd /home/diogo/dev/model-loader
mkdir -p /home/diogo/.config/model-loader/strata
cp docs/examples/strata-orca-iq3-xxs/engine.json \
  /home/diogo/.config/model-loader/strata/qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k.json
cp docs/examples/strata-orca-iq3-xxs/shared-settings.json \
  /home/diogo/.config/model-loader/strata/qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k.shared-settings.json
model-loader profile create \
  --id qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k \
  --backend strata-fork --file docs/examples/strata-orca-iq3-xxs/profile.json
model-loader profile validate \
  qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k
model-loader instance start \
  qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k
```

Inferência e calibração devem passar pelo proxy `127.0.0.1:4321`, usando o ID
do perfil no campo `model`. Validar startup não basta: confirmar divisão/cache
por placa e MTP no log, resposta em português/código, retrieval próximo a 32k,
integridade de ferramentas/SSE, seleção automática e continuação após resultado.
Não usar a medição upstream de 77,7 tok/s em uma 5090 como previsão para nós.

## Remoção da versão uncensored IQ2_XS reconvertida

A pedido do operador, foram removidos os perfis 32k e 256k, seus históricos de
undo, configurações de engine/shared-settings, o GGUF reconvertido, pack e
tokenizer exclusivos. Foram cruzadas as referências dos perfis restantes e
das configurações antes da remoção; não havia instância ativa.

Espaço recuperado medido: **116,79 GiB**. O IQ2_XS original, seus três perfis,
o IQ4_XS uncensored de origem e o MTP compartilhado foram preservados.
Relatórios, resultados de benchmarks e pequenos snapshots de metadados
permanecem como histórico. Auditoria:
`/home/diogo/models/strata/uncensored-iq2-xs-20261001/removal-result.json`.
