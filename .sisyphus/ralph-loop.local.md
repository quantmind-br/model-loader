---
active: true
iteration: 2
max_iterations: 500
completion_promise: "DONE"
initial_completion_promise: "DONE"
started_at: "2026-05-09T00:40:29.647Z"
session_id: "ses_1f5d24573ffepmDUXlwch0gIML"
ultrawork: true
strategy: "continue"
message_count_at_start: 0
---
: Substituir o campo de texto livre `LlamaServerBinaryPath` no editor de perfil por uma seleção de fork/versão a partir de um catálogo conhecido. Cada fork (ex: llama.cpp upstream, buun-llama-cpp, llama-cpp-turboquant) deve ter um arquivo de schema de validação próprio — gerado automaticamente a partir do `--help` do binário mas editável manualmente pelo usuário. Esse arquivo de schema é a única fonte de verdade para validação das flags do perfil: o parse live do `--help` em tempo de execução deixa de existir.

O catálogo de forks deve ser gerenciável de duas formas: via TUI (adicionar fork apontando o binário, que dispara a geração automática do schema) e via edição direta de um arquivo de configuração. No editor de perfil, o usuário seleciona o fork desejado e a validação usa exclusivamente o schema vinculado àquele fork.

O design deve ser genérico o suficiente para que no futuro seja possível adicionar outros tipos de backend de servidor LLM (vLLM, TabbyAPI, SGLang, etc.), cada um com seu próprio schema de parâmetros. Isso significa que o conceito de "backend com schema de validação" não deve ser acoplado exclusivamente ao llama-server.
