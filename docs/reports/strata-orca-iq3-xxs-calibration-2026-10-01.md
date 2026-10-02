# OrcaRouter uncensored IQ3_XXS: instalação e calibração local

O IQ3_XXS publicado pela OrcaRouter foi baixado, preparado e instalado no
Model Loader em 2026-10-01. A configuração que completou os testes de código,
português e contexto de 32k usa **GPU1 e experts complementares residentes na
RAM**. A variante inicial dual 24/24 com mmap simples foi interrompida duas
vezes pelo limite de crescimento de swap; permanece experimental.

Perfil que passou nos testes de estabilidade:
`qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-32k`.
Não está qualificado para agentes autônomos: a avaliação de ferramentas
registrou chamadas incorretas e repetição após resultado de sucesso.

Os exemplos [profile-resident.json](../examples/strata-orca-iq3-xxs/profile-resident.json)
e [engine-resident.json](../examples/strata-orca-iq3-xxs/engine-resident.json)
registram a configuração que completou os testes. Os arquivos ativos ficam
em `~/.config/model-loader/profiles/` e `~/.config/model-loader/strata/`.

## Origem e preparação

Repositório: `orcarouter/Qwen3.8-Flash-Next-Uncensored-GGUF`, revisão
`0434906af7b5202b676d43f108cf4f73d25691ef`. Os dois shards somam
85202668032 bytes (79,35 GiB); ambos passaram na conferência dos hashes
publicados, registrados no [procedimento de preparação](strata-orca-iq3-xxs-profile-2026-10-01.md).

O pack independente foi criado em
`/home/diogo/models/strata/packs/orca-iq3-xxs` pelo packer da cópia em
`backends/strata-fork`, usando `--compat-bf16 --experts-bin`, sem `--base`.
Conversão concluída em 184,07 segundos:

- 459 pequenas projeções convertidas para BF16, 1436139520 bytes;
  dense final 1485606400 bytes (1,38 GiB).
- 48 camadas com gate/up IQ3_XXS e down IQ4_NL; experts totalizam
  53477376000 bytes (49,80 GiB).
- Experts preservam a quantização publicada. Não houve reconversão para outra
  quantização; a expansão das pequenas projeções para BF16 não recupera os
  pesos originais em precisão completa.
- Geometria de todos os 1224 tensores e limites de payload verificados;
  48 layouts de camadas e 144 experts amostrados coincidem byte a byte com
  os shards. Essa amostragem não é uma comparação exaustiva de todo expert.
- Tokenizer exportado do próprio GGUF; template idêntico ao checkpoint de
  origem e ao template oficial, SHA-256
  `c3cf9e34abf4f9e36c2d72165aa9c132d3e2a725b6c2586aaa3a8af9d7a81041`.

O engine nativo continua com SHA-256
`545cfc5f04f581c29ebcf2c1969807efb3dd1bb1ae07fa3f14f396458aba6999`.
Nenhuma alteração em código de inferência foi necessária nesta instalação.
As nove verificações focadas do packer passaram. Outros packs e perfis não
foram convertidos nem modificados.

## Configuração e escolha de memória

Backend registrado: `strata-fork`, executável
`/usr/bin/python /home/diogo/dev/model-loader/backends/strata-fork/serve/server.py`.
Todas as cargas foram feitas com `model-loader instance start`; toda inferência
passou pelo proxy em `127.0.0.1:4321`.

O perfil GPU1 usa `gpu:[1]`, sem `layer_split`, `--resident-experts`,
15 workers, contexto 32768, KV int8, prefill 512, reserva de 1536 MiB e cache
GPU automático. O log confirma 8407 experts na GPU e **33,14 GiB de experts
complementares em RAM page-locked/mapped**, incluindo os slots emprestados ao
prefill. As trocas adaptativas operam entre esses tiers; os requests medidos
registraram zero leituras de blobs do arquivo após preparar a residência.

A GPU0 fica disponível para o desktop. Limites de potência mantidos em
270 W por placa. Não houve mudança global de swappiness, limpeza global de
page cache ou encerramento de outros aplicativos.

Sampling segue a recomendação do model card OrcaRouter: temperatura 1,
top_p 0,95, top_k 20, min_p 0; presence/repetition 0/1, seed 42 e experimental
speed projection desativada. Modo non-thinking selecionado via shared-settings;
o template exportado é o do próprio checkpoint. Os campos de configuração
de geração do repositório fonte retornaram 401; a recomendação pública do
model card foi usada e sua cópia mantida nos artefatos.

MTP: runtime original Q2_0 compartilhado, spec 4/min-p 0,5. É o head usado no
procedimento manual do Strata, não um head adaptado à versão uncensored.
O log e os timings confirmam sua atuação; aceitação depende do texto.

O modo residente complementar atual do backend **não suporta layer_split**;
combinar esses dois knobs é rejeitado pelo parser. Por isso ele foi testado
na GPU1, sem inventar um modo dual de residência.

## Resultados de contexto e geração

| Teste | Resultado |
|---|---:|
| Código curto aquecido, mediana de 3 | 81,3 tokens/s |
| Amostras de código aquecido | 77,3 / 84,3 / 81,3 tokens/s |
| TTFT mediano de código aquecido | 0,534 s |
| MTP nessas amostras | 795/937 aceitos (84,8%) |
| Explicação em português | 50,1 tokens/s; TTFT 0,875 s; `stop` |
| Contexto fresco, entrada real | 29831 tokens |
| TTFT do contexto fresco | 107,959 s |
| Decode no contexto fresco | 46,6 tokens/s |
| Registros no início/meio/fim | 3/3 corretos |
| JSON puro | falhou: bloco Markdown com JSON |
| Pico GPU0/GPU1 no contexto | 875/23087 MiB (0,85/22,55 GiB) |
| RAM disponível mínima no contexto | 11,28 GiB |
| Crescimento de swap no contexto | 0 GiB |

A primeira amostra de código foi excluída da mediana aquecida. O pedido de
contexto tinha prefixo fresco (`cache_n=0`). O JSON retornou os valores exatos,
mas envoltos em uma fence Markdown; recuperação passou e o oracle de JSON
puro falhou. O texto em português foi coerente, sem markup bruto de reasoning,
e terminou normalmente. Essas são medições de amostras específicas, não uma
garantia de velocidade em todo workload.

A amostra curada de HumanEval passou **10/10** no sampling do perfil, com
imports/helpers do prompt e execução de código isolada por bwrap. É uma
amostra pequena, insuficiente para medir qualidade geral ou provar equivalência
ao checkpoint de maior precisão.

## Benchmark do Model Loader

Run ID:
`qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-gpu1-resident-w15-32k-1790866419648537362`.
Os quatro presets completaram, com três repetições aquecidas por preset e
reuso da instância ativa:

| Fill nominal | Entrada real | Decode médio ± SD | TTFT em cache |
|---|---:|---:|---:|
| 5% | 1526 tokens | 72,1 ± 3,4 tok/s | 82 ms |
| 25% | 7714 tokens | 69,9 ± 1,9 tok/s | 91 ms |
| 50% | 15681 tokens | 88,5 ± 4,7 tok/s | 98 ms |
| 90% | 28382 tokens | 84,5 ± 4,2 tok/s | 121 ms |

Esses TTFTs são de prefixos em cache, não o tempo para ler um documento novo;
o teste fresco de 29831 tokens levou 107,959 s. Algumas respostas encerraram
antes do cap de geração; completion tokens foram 237/232/81/128 no resumo.
As comparações por fill dependem também do conteúdo gerado, não só do contexto.

A telemetria independente mediu pico de 875/23087 MiB, mínimo de 8,61 GiB de
RAM disponível e zero crescimento de swap durante esse benchmark. O campo
agregado `peakVramMb` do run contém 875 MiB (GPU0); ele não representa o pico
da GPU1. Os valores por placa deste relatório vêm das amostras de nvidia-smi.

## Ferramentas

Harness estrito da skill, sem alterações nos oracles, 30 positivos + 30
controles, seeds e variantes registrados:

- Integridade de argumentos: 4/4, incluindo reconstrução SSE.
- Seleção automática: 25/30 corretos, 3 malformados, 2 sem chamada.
  Dois casos vazaram o comando para `content`; um emitiu duas chamadas.
- Controles: 1/30 chamada indevida; 29/30 sem chamada.
- Continuação curta após sucesso: **falhou**, emitindo uma chamada novamente.
- SLO estatístico: **inconclusivo**. Limite inferior unilateral de 95% para
  chamada correta 0,681; limite superior para chamada indevida 0,149.
  Não atende aos limites exigidos para promover o perfil.

No servidor atual, `serve/frontend.py:openai_to_messages` ignora `tool_choice`
e `parallel_tool_calls`; a presença desses campos no request não os transforma
em restrições de geração. Assim, os 4/4 são evidência de integridade das
respostas com pedido `required`, não prova de enforcement pelo backend.
Nenhum comando do modelo foi executado no host por esse harness.

Canários de contexto longo, com temperatura 0 apenas no oracle de integridade
e o sampling normal do perfil preservado:

| Fill alvo | Tokens de texto user / entrada total | Integridade | Continuação |
|---|---:|---|---|
| 50% | 15878 / 16144 | falhou: `stop`, sem chamada | não executada: não havia call ID |
| 90% | 28985 / 29251 | passou, comando e canário exatos | passou, resposta limpa com `stop` |

As duas entradas eram frescas; a continuação de 90% reutilizou 29333 tokens
do prefixo. A telemetria desses pedidos permaneceu em 875/23087 MiB, com
mínimo de 10,70 GiB de RAM disponível e crescimento de swap zero. O resultado
de 90% não anula a falha de 50% nem a repetição na continuação curta.

## Por que o dual mmap não foi promovido

Perfil experimental:
`qwen3-8-flash-next-uncensored-iq3-xxs-mtp-strata-dual-mmap-w15-32k`.
Usa a divisão 24/24, 15 workers e o mesmo pack/MTP/contexto/prefill, com mmap
simples. O log confirmou 17408 experts em GPU (8823/8585).

A primeira carga cresceu swap em aproximadamente 2,588 GiB em 31,7 segundos
e foi parada pelo monitor, embora a RAM disponível mínima fosse 42,14 GiB.
A segunda carga passou, mas uma geração em português voltou a crescer swap
em cerca de 2,279 GiB e foi interrompida. Três amostras anteriores de código
aqueceram a 89,5/98,2/109,4 tokens/s (mediana 98,2); esse resultado parcial
não compensa o guard de memória nem valida contexto longo.

O sistema tinha `vm.swappiness=180`; isso é contexto observado, não prova da
causa nem autorização para modificar a configuração do sistema. Global swap
já estava ocupada antes das cargas; os monitores medem **crescimento**, não
swap total zero. Limites mantidos: 2 GiB de RAM mínima, 2 GiB de crescimento
de swap e 23552 MiB de VRAM/card, com três amostras violadas consecutivas.
Na variante residente os testes concluídos não acionaram esses limites.

## Evidências

Artefatos e scripts reproduzíveis em
`/home/diogo/models/strata/orca-iq3-xxs-20261001/`: download/revisão/hashes,
shard geometry, `compat-bf16.json`, `READY.json`, verificação do pack,
logs e telemetria de cargas (incluindo a tentativa interrompida), requests,
respostas, código de teste e resultados dos oracles.

A remoção do antigo IQ2_XS uncensored permanece concluída: seus perfis,
modelos/packs/tokenizer exclusivos foram apagados; resultados históricos e
o IQ4_XS fonte permanecem. A instalação deste IQ3 usa aproximadamente 131 GiB,
deixando cerca de 67 GiB livres antes dos últimos pequenos artefatos.

Após os testes, o backend e o proxy pertencentes à tarefa foram encerrados
com conferência de PID, start ticks e estado ocioso. A lista de instâncias está
vazia e a porta do proxy fechada, restaurando o estado inicial. Limites de
potência permanecem em 270 W/card. Ambos os perfis validam; o dual conserva
explicitamente os avisos de instabilidade, e o residente conserva os limites
de ferramentas. Os três perfis IQ2_XS originais e seus assets seguem preservados.
