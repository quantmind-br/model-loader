# Revisão independente do relatório consolidado Strata — 2026-10-01

**Documento revisado:** `/home/diogo/dev/model-loader/IDEATION_PERFORMANCE.md`
**SHA-256:** `0241f527530e3ab96aa6398dce732834ec15aafb346788fb0bdafa60ba7c2221`
(830 linhas; mesmo hash no início e no fim desta revisão e em
`docs/reports/strata-consolidation-2026-10-01/verification.json`).
**Revisor:** Claude Code, `claude-opus-5-5`, esforço `xhigh`, sessão nova e headless.
**Escopo:** qualidade factual e decisória do consolidado. Não refiz a auditoria,
não iniciei modelos, não executei GPU/SSD, não reexecutei suítes e não alterei
código, profiles, configs ou git. Artefatos de trabalho ficaram em
`/tmp/strata-consolidated-review-20261001/`.

## 1. Veredito

**Aprovado com ressalvas.** Não encontrei bloqueador. Nenhum achado P1
(S01–S04, S07–S10) contém erro factual. As afirmações conferem com o código
local, os logs do engine, a telemetria bruta e os artefatos citados. As
correções feitas sobre as auditorias originais estão corretas e fiéis: DMA no
prefill, 14,8 → 14,526 GiB, 26,8 → 26,373 GiB, seed 42 reclassificada e o fator
16× tratado como ideal. Hipóteses aparecem rotuladas e acompanhadas de
experimento.

As ressalvas tratam de completude e fidelidade em três pontos sensíveis para
decisão:

- residência durante o prompt;
- perfil-alvo da calibração de prefill;
- controle negativo da investigação de swap.

Há ainda quatro sugestões editoriais. Nenhuma delas muda a priorização nem o
plano de promoção. Todas reduzem o risco de leitura errada.

## 2. Problemas encontrados, por severidade

Severidade: **Bloqueador** (o documento induz uma decisão errada se não for
corrigido), **Menor** (omissão ou perda de fidelidade com impacto decisório
limitado e mitigado por outras ressalvas) e **Sugestão** (melhoria editorial).

### R1 — Menor: "24576/24576" e "totalmente residente" omitem o empréstimo de slots ao prompt nas duas GPUs

**Onde:** linhas 110 e 114–119 (tabela de profiles), 127–128, 396–397 (S09) e
493 (S13).

**Referência primária:**

- Os logs do engine em `~/.local/state/model-loader/logs/strata/*-engine.log`
  registram, para o IQ2 32k, `the prompt path borrows 3103 CUDA0 cache slots
  (4.08 GiB)` e `CUDA1 prompt path borrows 2975 of its 12288 slots (4.08 GiB)`.
- Para o IQ4 auto, os mesmos logs registram 1499 + 1499 slots (3,72 + 3,72 GiB).
  Com prefill 2048 são 495 + 495. Para o IQ3 dual, 186 + 186.
- Em `backends/strata-fork/src/program/generate.cpp:303-305`, o padrão é
  emprestar e depois fazer refill.
- Em `generate.cpp:4578-4608`, as linhas emprestadas viram `kNotResident`
  durante o prompt. São reabastecidas a partir da origem (mmap) depois dele.
- O log do GPU1 residente mostra `186 of the prompt path's 186 lendable slots
  keep their experts in RAM too`: o desenho atual guarda em RAM cópias dos
  slots emprestados.

**Problema:** fora do prompt, o IQ2 32k mantém os 24576 experts residentes.
Num prompt longo, cerca de 6078 slots (≈ 8,16 GiB) ficam não residentes e
são relidos do mmap depois. Para o S09, o "complemento básico" cresce se o
desenho dual mantiver essas cópias, como faz o GPU1 residente:

- IQ3 dual 512: 14,526 → ≈ 15,28 GiB.
- IQ4 dual auto: 26,373 → ≈ 33,81 GiB. Esse valor fica na ordem dos 33,14 GiB
  do IQ3 GPU1 residente, que operou com mínimo de 8,61 GiB de RAM disponível.

**Por que a ressalva atual não basta:**

- A linha 117 trata da variação entre starts, não do empréstimo por request.
- A linha 127 lista "lend/refill" entre as exclusões, mas sem magnitude.
- A linha 114 diz "incluindo empréstimos de prefill" só para o GPU1 residente.
  Isso sugere, por contraste, que os profiles dual não emprestam.
- A expressão "IQ2 32k totalmente residente" (linha 493) é verdadeira apenas
  fora do prompt.

**Correção mínima:**

- Acrescentar à tabela da seção 3 uma coluna "slots emprestados ao prompt (log)".
- Na linha 493, escrever "totalmente residente fora do prompt".
- No S09, registrar a ordem de grandeza derivada das cópias de empréstimo
  (+0,75 GiB no IQ3 512; +7,43 GiB no IQ4 auto), rotulada como derivada.

### R2 — Menor (fidelidade): o S12 generaliza para "os profiles IQ3" uma calibração que o original dirigia ao IQ3 residente

**Onde:** linhas 458–475, em especial 465 e 470.

**Referência primária:**

- O STR-09 original
  (`IDEATION_PERFORMANCE.original-2026-10-01.md:383`) se intitula "Calibrar
  prefill e staging juntos no IQ3 residente".
- O TTFT de 107,959 s que motiva o achado foi medido no GPU1 residente
  (`docs/reports/strata-orca-iq3-xxs-calibration-2026-10-01.md:93-95`).
- Em `backends/strata-fork/src/prefill/prefill.cpp:81-91` e `:1134-1150`, a
  partir de chunk 1024 (`stream_all`), todo expert não residente de cada camada
  é enviado em cada chunk. Experts não pinned passam por cópia do `Stager` a
  partir do mmap.

**Problema:** no IQ3 GPU1 residente, a origem é pinned e há DMA direto. No IQ3
dual mmap, os braços 1024/2048 ativam `stream_all` via `Stager`. Pelos slots
dos logs, isso dá ≈ 15,3 GiB lidos do mmap por chunk (7540 blobs, contando os
emprestados). É o mesmo regime em que o IQ4 violou o guard.

**Por que a ressalva atual não basta:** o texto exige estabilidade antes de
avançar, coloca a Etapa 4 depois do S10 e cita a falha do IQ4 com 8192 e 2048.
Isso reduz o risco, mas não diz em qual profile começar. A ação "512 → 1024 →
2048" pode ser aplicada ao IQ3 dual, o que repete o cenário de risco.

**Correção mínima:** "Primeiro no IQ3 GPU1 residente (origem pinned). No IQ3
dual mmap, chunks ≥ 1024 ativam `stream_all` via `Stager`; testar só depois
da atribuição do S10."

### R3 — Menor: a investigação de swap omite o controle negativo do IQ2 e enfraquece a hipótese de acoplamento via reclaim

**Onde:** linhas 36–41 (seção 1), 412–417 (S10), 646–647 (seção 6) e 699–701
(seção 7, item 7).

**Referência primária:** telemetria bruta. Valores recalculados em
`/tmp/strata-consolidated-review-20261001/iq2-swap-by-request.txt` e
`iq4-swap-by-request.txt`.

| Request | Fonte | Crescimento global de swap |
|---|---|---:|
| IQ2 256k, prompt fresco de 259641 tokens | `~/models/strata/calibration-256k-20261001/results/baseline/raw.jsonl` | +1,072 GiB (abaixo do guard de 2 GiB) |
| IQ2 976k, 989136 tokens | `~/models/strata/context-20261001/results/iq2-xs-1m-v1/raw.jsonl` | +0,234 GiB |
| IQ2 32k, 29825 tokens | `~/models/strata/iq4-tests-20261001/results/iq2-baseline-32k/raw.jsonl` | 0 |
| IQ4 auto / 2048 | `~/models/strata/iq4-tests-20261001/results/iq4-32k*/raw.jsonl` | +2,278 / +2,272 GiB, com RAM mínima de 42,27 / 44,91 GiB |
| IQ3 GPU1 residente (33,14 GiB pinned; páginas liberadas com `madvise`/`fadvise` na carga, `expert_source.cpp:681-690`) | telemetria do profile | 0 nos requests; +0,026 GiB na carga |

**Problema:** o documento mantém a causa em aberto, e isso está correto. Ainda
assim:

- Apresenta o crescimento de swap como fenômeno do IQ3/IQ4. O profile de
  referência IQ2 256k cresceu 1,07 GiB num prompt fresco longo. Esse é o
  controle natural para o S10.
- A frase "page faults de arquivos mmap e swap de páginas anônimas são
  fenômenos separados" é verdadeira quanto ao mecanismo. Numa seção de decisão,
  porém, ela desestimula a hipótese de acoplamento:
  - a pressão de page cache gerada por streaming e refill do mmap aciona
    reclaim;
  - com `swappiness=180`, o reclaim favorece páginas anônimas, que vão para o
    zram;
  - MemAvailable inclui page cache recuperável (o próprio código diz isso em
    `expert_source.cpp:214`). Logo, MemAvailable acima de 42 GiB não exclui
    pressão de reclaim.
- O original do Codex (linhas 200–204) tinha uma frase sobre o mmap tocar
  regiões amplas enquanto o residente libera páginas. Ela foi descartada.

**Por que a ressalva atual não basta:** a lista de contadores do S10 é boa. Mas
não inclui os discriminadores diretos do acoplamento (pgscan/pgsteal anônimo
versus arquivo, `workingset_refault_*`, pswpout por fase). Também não usa o IQ2
como controle. Sem isso, a regressão IQ2 (seção 7) pode passar mesmo com
crescimento de swap abaixo do limite.

**Correção mínima:**

- Na seção 1, escrever "mecanismos distintos, possivelmente acoplados via
  reclaim (hipótese)".
- No S10, incluir os números do IQ2 como controle e os contadores acima.
- Na seção 7, registrar o crescimento de swap como métrica contínua na
  regressão IQ2.

**Ressalva desta revisão:** os dados não mostram relação monotônica. O swap
inicial variou entre 0,97 e 8,9 GiB entre os runs, e o IQ3 dual violou o guard
já na carga. Não sustentam uma causa e não devem ser apresentados como causa.

### R4 — Sugestão: os rótulos P1/P2 não coincidem com a ordem das etapas

**Onde:** linhas 170–171 e 668–679. A definição diz "P2, a próxima rodada".
Mesmo assim, S05/S06 (P2) estão na Etapa 2, antes de S08/S09 (P1) na Etapa 3.
O S11 (P2) "pode ser prototipado" antes do S09 (P1).

**Correção:** uma frase dizendo que as etapas seguem dependências e os rótulos
P indicam importância.

### R5 — Sugestão: o S08 cita só o microteste adversarial

**Onde:** linha 159 (seção 4) e linhas 356–376 (S08). Os logs arquivados contêm
medidas menos adversariais, que não foram citadas:

- `affinity_inherit.log`: 4 copiadores de 2,18 MB, 26,0 → 19,3 GB/s após o pin.
- `affinity_inherit2.log`: 51,2 → 43,2 e 53,1 → 45,7 GB/s.

Mesmo após o pin, esses valores ficam acima do H2D pinned por GPU (13,35 GB/s)
e do agregado (26,7 GB/s). A banda de cópia do staging, sozinha, talvez não
limite o prefill. O impacto pode vir mais da latência do issuer/lançador. O
achado já está rotulado como hipótese e exige A/B, então não é erro. Citar
essas medidas calibraria a expectativa "impacto potencial alto".

### R6 — Sugestão: o S02 não diz que o script atual substitui o binário de produção no próprio lugar

**Onde:** linhas 226–239. A linha 6 de `backends/strata-fork/backend-build.sh`
instala por cima de `build-fork-sm86/strata`, binário usado pelos seis profiles.
Hoje o build falha antes disso (rc=2). Se alguém apenas ativar CUDA, o script
passa a fazer uma substituição irreversível. A ação "publicar por
rename/versionamento" cobre o caso implicitamente. Vale torná-lo explícito.

### R7 — Sugestão: falta ao S04 a magnitude do pior caso

**Onde:** linhas 270–281. Sem `max_tokens`, `prepare()` define `max_new` como
o resto do contexto (`serve/server.py:919-970`). No profile 976k, um cliente
não streaming que se desconectou pode prender a FIFO até o EOS ou até cerca de
1M tokens. O C1 original mencionava isso. Uma frase reforça o P1 sem exagero.

**Nota editorial (linha 803):** a suíte Python do Codex registra
`Ran 130 tests ... OK (skipped=3)`. Os 3 detokenizadores rodaram numa execução
separada de 3 testes. "Incluindo" é ambíguo; o melhor é "130 executados, 3
pulados e depois executados separadamente".

## 3. Bloqueadores versus sugestões

| ID | Tipo | Bloqueia uso do documento? |
|---|---|---|
| R1 | Menor | Não. Corrigir antes de usar a tabela da seção 3 em orçamento de RAM do S09. |
| R2 | Menor | Não. Corrigir antes de executar a Etapa 4 no IQ3. |
| R3 | Menor | Não. Incorporar no desenho experimental da Etapa 2 (S10). |
| R4–R7, nota | Sugestão | Não. |

## 4. Principais verificações que passaram

**Vínculo e proveniência**

- Hash do consolidado conferido.
- 37 links locais existem.
- As 67 citações `arquivo:linha` resolvem para linhas pertinentes (script em
  `/tmp/.../check_citations.py`).
- A cópia do original Codex (`a86e2016…`) e o relatório Claude (`e9914f2d…`,
  361 linhas) batem com `delegation.json`.
- O manifesto de fontes tem 280 arquivos e 73777 linhas.
- O binário (`545cfc5…`) é o mesmo citado. Ele embute apenas `0.1.30`. O
  `CMakeCache` de `build-fork-sm86` aponta para `/home/diogo/dev/Strata`.

**S01:** `iq_pack.py:337` reaproveita `experts.bin` só por tamanho.
`contract-probes.json` mostra o mesmo hash de experts com dense diferente. O
engine também só confere o tamanho (`expert_source.cpp:411-420`), o que confirma
a mistura silenciosa no pipeline.

**S02:** `CMakeLists.txt:35` (CUDA OFF), o bloco da linha 176 e o alvo da
linha 380 conferem. `build-reproduction.json` registra
`No rule to make target 'strata'`.

**S03:** as probes reproduzem as cinco violações. Não há tratamento de
`tool_choice`, `parallel_tool_calls`, `response_format` nem `stop` em `serve/`.

**S04:**

- O caminho não streaming só coleta no fim (`server.py:1670-1671`).
- `lines.get()` não tem prazo (`:397`) e roda dentro de `with self.fifo`
  (`:1022`), via `gen.close()`.
- O log de reprodução registra 301 tokens e 5,65 s, contra 25 tokens e 0,10 s
  em streaming.

**S05:** `TerminateTree` confirma apenas o líder. `/health` responde 200 com
`loaded`. O monitor considera saudável qualquer resposta 200.

**S06:** `seed > 0` e o limite de `top_k` estão em `server.py:300-325`. O
engine usa o relógio como seed quando ela é 0 (`generate.cpp:4620`). Os seis
configs usam seed 42 e k20.

**S07:**

- `gpu.go` lê um único registro CSV.
- `gpu_sampler.go` usa o pico desse único valor.
- `chunkTimings` só lê dois campos, enquanto o servidor envia `cache_n` e
  `draft_n`.
- `warn_tight_ram` retorna quando `arena_mib <= 0`.
- O 875 MiB versus 23087 MiB confere com a telemetria.

**S08:**

- O pin fica em `session.cpp:532-541` e é chamado em `generate.cpp:3252`.
- O `Stager` é criado em `Prefill::init` (`prefill.cpp:540-545`), via
  `init_pipeline` (`generate.cpp:3583`), depois do pin.
- `ExpertPool` (`:2325`, que fixa seus próprios workers) e a abertura da
  `PleTable` (`:1858`) vêm antes do pin.
- O log de herança confirma o CPU0.

**S09 e DMA/PCIe:**

- A recusa fica em `generate.cpp:1272-1276`, e `--resident-experts` entra nesse
  caminho (`:1162-1163`).
- O lend exige a lista `additional_gpu_pairs` vazia (`expert_source.cpp:540`).
- Sem complemento, `pcie_layer` retorna falso, a fração m é 0
  (`:1028-1029`) e os misses vão ao pool de CPU.
- O `Stager` copia para um buffer pinned, com fallback pageable
  (`prefill.cpp:175-182`), e então faz DMA.
- A correção do C3 é fiel ao título original ("desliga ... o DMA do prefill").

**Capacidade:**

- Os 24576 pares conferem como disco ÷ blob, para IQ3 e IQ4.
- Os complementos recalculados dão 14,526 e 26,373 GiB.
- As contagens de slots conferem com os logs: 12288+12288, 12201+11260,
  8523+6983, 8823+8585 e 7083+6857.

**S10 e telemetria:**

- IQ3: +2,588 / +2,279 GiB, conforme o relatório.
- IQ4: +2,278 / +2,272 GiB, com RAM mínima de 42,27 / 44,91 GiB (bruto).
- GPU1 residente: +0,026 GiB na carga e 0 nos requests; mínimo de 8,61 GiB;
  23087 MiB.

**S11:**

- Cada stage carrega o arena inteiro e o dense nativo
  (`generate.cpp:1930-1951`).
- O preset `--native` define `native_dense_gguf` (`:1395-1400`), premissa do
  script de cálculo.
- Os valores 1,41/1,46, 1,28/1,30 e 1,47/1,50 conferem com a saída preservada.

**S12–S20 (amostragem):**

- `stream_all_min` vale 1024.
- `--no-base` existe, e o probe de ranking é idêntico à base.
- O draft MTP em lote fica desativado com multi-GPU; o prefill do drafter é
  agrupado.
- `serve` exige spec ≥ 2 (`generate.cpp:3454`).
- O parking de conversas é recusado com split (`:1193-1196`). A memória de
  conversas lê `/proc/meminfo`; o planner de experts considera cgroup.
- O I/O PLE usa 16 threads.
- Os números de BPE (16384 entradas, bypass acima de 256 caracteres) e as
  amostras PLE conferem.

**Ambiente:**

- RAM de 60,49 GiB; zram de 16 GiB, com 5,78 GiB usados; swappiness 180.
- PCIe Gen4 x8 com topologia PHB; P2P r/w OK.
- L3 de 96/32 MiB; driver 610.57.04; nvcc 13.4.92.

**Baselines:**

- 135,5/0,326 (`comparison.json`, `iq2-tuned`, 270 W).
- 135,7/0,320; 87,27/91,7; 744,86/57,0.
- 98,2; 81,3/0,534; 107,959/46,6; 76,3/0,503.

**Testes alegados:**

- Python Codex: 130 testes OK com 3 pulados, mais 3 de detokenização OK.
- C++ Codex: 4 testes no CTest.
- Go Codex: 2 pacotes.
- Claude: Go em 6 pacotes e vet sem saída; 5 binários C++; suítes Python, com
  uma falha de cwd corrigida na reexecução, conforme a ressalva da linha 812.

**Profiles:** os seis são válidos. Prefill 512 só nos IQ3. Spec 4 em todos.
Min-p 0,5 nos IQ3 e 0,7 nos demais.

**Plano de promoção:** os guards (2 GiB de RAM, 2 GiB de crescimento de swap,
3 amostras consecutivas), o limite de 23552 MiB e os 270 W conferem com os
relatórios de calibração. A exigência de três repetições, mediana e dispersão,
sem p99, está correta.

## 5. Cobertura e limites desta revisão

| Área | Cobertura |
|---|---|
| P1, swap/cache, DMA/PCIe, afinidade, densos, capacidade | Leitura direta de código e logs, contas refeitas |
| Testes alegados | Resumos dos logs conferidos; suítes não reexecutadas |
| Proveniência | Hashes, `CMakeCache`, strings do binário, metadados de delegação |
| Plano de promoção | Coerência com guards, relatórios e dependências |
| S12–S20 | Amostragem das citações e das afirmações centrais |

Fora do escopo ou não verificado:

- Não li transcripts nem raciocínio dos auditores. A independência do Claude
  foi aceita com base em `delegation.json`.
- Não inspecionei os 280 arquivos do inventário, a numérica dos kernels,
  visão ou HIP.
- Não revisei S17 nem S20 além das citações.
- Não confirmei ext4 nem o espaço livre atual.
- Os números derivados em R1–R3 vêm de logs e código, não de novas medições.
  O blob médio do IQ2 é uma aproximação, porque os blobs variam por camada.

Nada foi executado em GPU, SSD ou modelo real. Nenhum arquivo fora deste parecer
e de `/tmp/strata-consolidated-review-20261001/` foi criado ou alterado.
