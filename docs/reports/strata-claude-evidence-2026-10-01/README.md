# Evidências da auditoria independente do Claude

O [relatório original](../strata-independent-claude-opus-5-5-2026-10-01.md)
foi produzido por Claude Code com `claude-opus-5-5`, esforço `xhigh`, em modo
headless, incluindo `--allow-dangerously-skip-permissions`. A execução terminou
com código 0 e resultado `success`, sem recusas de permissão.

Esta pasta preserva os scripts e logs textuais produzidos em
`/tmp/strata-claude-independent-20261001/`, além do prompt, help e versão da CLI.
O manifesto registra origem, tamanho e SHA-256. Binários temporários, caches de
compilação e o transcript bruto não foram copiados. As referências `/tmp/` do
relatório continuam apontando aos originais; os arquivos arquivados aqui mantêm
os mesmos nomes e conteúdo.

## Notas de verificação posterior pelo auditor principal

Os dois relatórios foram preservados, sem incorporar conclusões de um ao outro.
Estas notas delimitam a evidência apresentada pelo Claude:

- **C1 e C2:** os logs sustentam a reprodução de desconexão sem streaming e a
  herança de afinidade. O teste extremo de disputa de CPU é sintético; sua
  diferença de tempo não representa ganho medido na inferência real.
- **C3:** o caminho mmap exige staging em memória pinned antes da transferência
  para a GPU. A expressão “desliga o DMA do prefill” deve ser entendida como
  ausência de DMA direto a partir da origem dos experts, não ausência de DMA
  na transferência do buffer de staging para a GPU. O microteste de cópia
  pageable não mede, sozinho, o desempenho do pipeline completo do engine.
- **C4:** `mincore.log` comprova cache frio no momento observado. Não demonstra
  cache frio após **todo** reinício ou troca de profile, nem estabelece
  `swappiness=180` como causa. Esses pontos precisam de observação longitudinal.
- **C5:** o teste comprova a propagação de `seed=42`. Seed fixa pode ser desejada
  em calibrações; removê-la é uma decisão de configuração. O teste não demonstra
  identidade dos textos de um modelo real, e o próprio relatório reconhece
  outras fontes de não determinismo.
- **D1 e oportunidades O1–O9:** estimativas de tráfego, residência e ganhos
  continuam condicionadas a testes no engine real. Nenhum modelo foi carregado
  nesta auditoria. Aumentar o chunk de prefill exige verificar memória e
  estabilidade, além de vazão.

A verificação final confirmou que os 280 arquivos do manifesto de fontes, os
seis profiles e configs preparados, o binário compartilhado e o relatório do
auditor principal mantiveram o conteúdo anterior à entrega independente.
Detalhes da execução e dos checks estão em
[delegation.json](../strata-audit-2026-10-01/delegation.json) e
[verification.json](../strata-audit-2026-10-01/verification.json).
