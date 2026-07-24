# huggingface-download — dev notes

Notas de desenvolvimento e proveniência. Nada aqui é carregado como instrução da skill — o conteúdo operacional vive só no `SKILL.md`.

## Proveniência das métricas

A tabela de throughput no `SKILL.md` vem de benchmark medido em 2026-06-19 com o modelo
`cyankiwi/Qwen3.6-35B-A3B-AWQ-4bit` (repo de 25 GB, 6 shards safetensors de ~5 GB).

Metodologia:
- Métrica: `bytes_no_disco / segundos_decorridos` → MiB/s.
- Cache frio por teste: `HF_HOME` temporário + diretório destino apagado entre runs.
- Testes single-stream no shard de 870 MB; teste paralelo em 3 shards de 5 GB (janela fixa de 60 s, throughput agregado).
- Sequencial (exceto o teste paralelo) para não competir por banda.

Resultados (MiB/s): `hf` 3-paralelo 75.3 · `aria2c -x16` 59.2 · `hf` 1-stream 46.1 · `hf` Xet ON 43.7 · `aria2c -x1` 37.7.

Conclusões validadas:
- Throughput escala com nº de conexões, não com banda nem com tuning de single-stream.
- `aria2c -x1` < `hf` single-stream → o ganho do aria2 vem só das conexões paralelas.
- Xet não travou neste run (43.7, empate técnico com o clássico) — o stall é intermitente, não determinístico; desabilitar segue como default seguro.
- Ressalva: single-stream usou arquivo de 870 MB (slow-start do TCP subestima ~5-10%); o número paralelo de 5 GB é o regime sustentado mais limpo. A ordem do ranking não muda.

## Origem das diretrizes

As regras de Setup/Core/Pitfalls derivam das notas globais do usuário em `~/.claude/CLAUDE.md`
(seção "Downloading HuggingFace models"), confirmadas por pesquisa web (hub v1.0 deprecou `hf_transfer`;
Xet com concorrência adaptativa, performance mista em vários links).
