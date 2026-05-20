# Plano de Implementação — Formulário de Profile Dinâmico e Multi-Backend

**Data:** 2026-05-20
**Branch:** `feature/backend-compatibility`
**Objetivo:** Tornar o grupo "Essentials" do editor de profiles totalmente dirigido pelo schema do backend selecionado, eliminando o viés llama-server hoje hardcoded, de modo que vLLM e SGLang ganhem campos de primeira classe sem código por-backend novo a cada flag.

---

## 1. Contexto e diagnóstico

### 1.1 O que já funciona (não mexer)

- **Rebuild ao trocar backend** — `editor.go:633-637` (`forwardToForm`) detecta `draft.BackendID != lastBackendID`, chama `reloadSchema()` que recarrega o `FlagSchema` e reconstrói o form via `buildForm`.
- **Aba Advanced 100% schema-driven** — `newAdvancedTable` / `schemaRows` (`draft.go:362-407`) lista todas as flags do schema; edição inline com conversão de tipo via `parseFlagValue` (`draft.go:438-474`).
- **Validação genérica** — `validator.Validate(profile, schema)` (`validator/validator.go:57`) já é agnóstica de backend.
- **Resolução de schema** — `resolveBackendSchema` (`editor.go:713-745`) carrega o `BackendValidationSchema` correto do catálogo.

### 1.2 O que é hardcoded para llama-server (o alvo)

| Local | Problema |
|-------|----------|
| `draft.go:46-73` (`Draft`) | Campos fixos `NGL, CtxSize, BatchSize, UBatchSize, Port, FlashAttn, CacheTypeK, CacheTypeV` — conceitos llama.cpp. |
| `draft.go:256-267` (`buildForm`) | Grupo 2 só é montado para `kind == llama-server`, com `.Value(&d.NGL)` etc. vLLM/SGLang não têm Essentials. |
| `draft.go:126-172` (`ApplyToWithSchema`) | Mapeia os 8 campos para chaves literais (`"ngl"`, `"ctx-size"`, `"flash-attn"`…). |
| `profiles_crud.go:160-184` (`newDraftDefaults`) | Semeia defaults llama (`NGL:"99"`, `CtxSize:"8192"`…). |
| `profiles_crud.go:225-254` (`startEditSelected`) | Hidrata os 8 campos a partir de `pr.Args[...]` e copia o resto para `Draft.Args` com skip-list literal dos 8 nomes. |
| `sizing.go:30` + `editor.go:296-301` | SizingTab lê/escreve `d.NGL` (conceito GPU-layers, llama-only). |

### 1.3 Restrições técnicas observadas

1. **huh exige ponteiro tipado estável** — `huh.NewInput().Value(&x)` onde `x` é `string`. Não é possível tomar endereço de elemento de mapa em Go (`&m[k]` é ilegal). Campos dinâmicos precisam de uma indireção própria.
2. **`Draft` é copiado por valor** — `openSnapshot = dp` (`editor.go:162`), `committed := *e.draft` (`editor.go:650`), `CurrentDraft()` retorna `*e.draft`. O dirty-check usa `reflect.DeepEqual(*e.draft, e.openSnapshot)` (`editor.go:225`). Portanto o armazenamento de valores essenciais **deve ser um mapa de valores** (`map[string]string`), não de ponteiros — caso contrário o snapshot compartilharia os mesmos ponteiros do draft vivo e o dirty-check nunca detectaria mudanças.
3. **Schema do llama é gerado em runtime** — o parser de `--help` (`llamahelp/parser.go`) produz `FlagSpec`s sem nenhum marcador "essential". **Logo, "quais flags são essenciais" NÃO pode ser um campo do `FlagSpec`**, senão o llama (parseado em runtime) ficaria sem essentials. A lista de essenciais precisa viver fora do schema, indexada por `BackendKind`.
4. **Chave legada divergente** — profiles salvos hoje guardam `Args["ngl"]` (short), mas o long canônico do schema é `"n-gpu-layers"`. Migração de chave precisa de cuidado no caminho de edição (`handleEditorCommitted` usa `ApplyToWithSchema(existing, schema)`, `profiles_crud.go:60`, que preserva `existing.Args`).
5. **flash-attn legado pode ser bool** — `FlashAttnToString` (`draft.go:219-231`) coage `true→"on"`. Hidratação genérica precisa de hook de coerção por-campo.

---

## 2. Decisões de design (com justificativa)

### D1 — Registry de essenciais por `BackendKind`, resolvido contra o schema vivo

Em vez de marcar flags no schema, definimos em código uma lista ordenada de campos "promovidos" por backend. Cada entrada referencia uma flag por nome (long/short/alias) e carrega **apenas dicas de UX** (label, descrição, bounds, validador especial, default, coerção). Tipo, enum, help e default reais vêm de `schema.Lookup()`.

**Por quê:** sobrevive ao schema parseado em runtime (llama), funciona com schemas embutidos (vLLM/SGLang), e concentra a curadoria de UX num único lugar. Se uma flag essencial não existir no schema atual, é silenciosamente omitida (degradação graciosa).

### D2 — Armazenamento de valores em `Draft.Essentials map[string]string` (long-keyed); ponteiros de binding no `Editor`

`Draft` ganha `Essentials map[string]string` (chave = `spec.Long` canônico, valor = string do editor). Copiável por valor → dirty-check e snapshot continuam corretos.

Os ponteiros que o huh exige (`map[string]*string`) ficam no `Editor` (`essentialPtrs`), reconstruídos a cada `buildForm`, semeados a partir de `draft.Essentials`. Após cada `form.Update`, o Editor drena `essentialPtrs` → `draft.Essentials` (sync), de modo que dirty-check, preview e validação leiam sempre o draft atualizado.

**Por quê:** combina a estabilidade de ponteiro que o huh precisa com o modelo de valor-copiável que o snapshot/dirty exige.

### D3 — Hidratação centralizada em `Editor.Open()`

A página (`profiles_crud.go`) passa a entregar o `Draft` com `Args` = **todas** as flags do profile (sem peeling) + `Model`/`BackendID`/restart/env. O `Editor.Open()`, depois de `loadSchemaForDraft()` (que define `kind` e `schema`), executa `hydrateEssentials()`: para cada campo essencial do kind presente no schema, **move** o valor de `draft.Args` (procurando por long/short/aliases) para `draft.Essentials` e **remove** as chaves correspondentes de `Args` (evita duplicação na aba Advanced). Campos ausentes são semeados pelo default da registry.

**Por quê:** remove o conhecimento de "quais campos são essenciais" da página; corrige a migração de chave legada num só ponto; elimina os defaults llama hardcoded de `newDraftDefaults`.

### D4 — `ApplyToWithSchema` genérico com normalização para o long canônico

Reescrito para iterar `draft.Essentials` (já long-keyed), parsear cada valor via `parseFlagValue(raw, schema, flag)` e incluí-lo sob `spec.Long`. Precedência mantida: `base.Args` (filtrado) → `Draft.Args` (Advanced) → `Essentials`. Na etapa de preservação de `base.Args`, **descartar** qualquer chave cujo `Lookup().Long` já seja um essencial (dedupe da chave legada `"ngl"` vs `"n-gpu-layers"`).

**Por quê:** unifica Essentials e Advanced como views do mesmo `map[string]any`; migra a chave legada de forma limpa no próximo save.

### D5 — Sizing permanece llama-only, mas escreve em `Essentials["n-gpu-layers"]`

`SizingTab` continua gated por `backendKind == llama-server`. Lê `draft.Essentials["n-gpu-layers"]` e a sugestão grava nele (+ refresh do ponteiro/form). Escopo inalterado.

---

## 3. Novas estruturas

### 3.1 `internal/ui/pages/profile_editor/essentials.go` (novo arquivo)

```go
package profile_editor

import "github.com/quantmind-br/model-loader/internal/domain"

// EssentialField promove uma flag do schema para campo de primeira classe
// no grupo Essentials. Tipo/enum/help/default reais vêm de schema.Lookup(Flag);
// os campos abaixo são apenas dicas de UX e validação.
type EssentialField struct {
	Flag        string             // nome para schema.Lookup (long, short ou alias)
	Label       string             // título na UI (fallback: Flag)
	Description string             // descrição na UI (fallback: HelpText do schema)
	Min, Max    *int               // bounds opcionais para FlagTypeInt
	IsPort      bool               // usa portValidator()
	AllowEmpty  bool               // int opcional pode ficar em branco
	Default     string             // valor-string default quando ausente em Args
	Coerce      func(any) string   // hidratação custom (default: ArgString)
}

func iptr(v int) *int { return &v }

// essentialFields lista, por backend, as flags promovidas a Essentials.
// Ordem define a ordem de exibição. Flags ausentes no schema são omitidas.
var essentialFields = map[domain.BackendKind][]EssentialField{
	domain.BackendKindLlamaServer: {
		{Flag: "n-gpu-layers", Label: "ngl (gpu layers)", Description: "Number of GPU layers to offload", Min: iptr(-1), Max: iptr(9999), Default: "99"},
		{Flag: "ctx-size", Label: "ctx-size", Description: "Context window size in tokens", Min: iptr(0), Max: iptr(1024 * 1024), Default: "8192"},
		{Flag: "batch-size", Label: "batch-size", Description: "Prompt processing batch size", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true, Default: "2048"},
		{Flag: "ubatch-size", Label: "ubatch-size", Description: "Physical batch size", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true, Default: "512"},
		{Flag: "port", Label: "port", Description: "Port to bind the inference server", IsPort: true, Default: "4321"},
		{Flag: "flash-attn", Label: "flash-attn", Description: "Flash Attention mode", Default: "auto", Coerce: FlashAttnToString},
		{Flag: "cache-type-k", Label: "cache-type-k", Description: "Key cache quantization", Default: "q8_0"},
		{Flag: "cache-type-v", Label: "cache-type-v", Description: "Value cache quantization", Default: "q8_0"},
	},
	domain.BackendKindVLLM: {
		{Flag: "tensor-parallel-size", Label: "tensor-parallel-size", Description: "GPUs for tensor parallelism", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "gpu-memory-utilization", Label: "gpu-memory-utilization", Description: "Fraction of GPU memory (0.0–1.0)", Default: "0.9"},
		{Flag: "max-model-len", Label: "max-model-len", Description: "Max context length (0 = auto)", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true},
		{Flag: "dtype", Label: "dtype", Description: "Weights/activations dtype", Default: "auto"},
		{Flag: "quantization", Label: "quantization", Description: "Quantization method", Default: "None"},
		{Flag: "port", Label: "port", Description: "Port to listen on", IsPort: true, Default: "8000"},
		{Flag: "served-model-name", Label: "served-model-name", Description: "Name advertised in /v1/models"},
	},
	domain.BackendKindSGLang: {
		{Flag: "tp-size", Label: "tp-size", Description: "Tensor parallelism size", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "dp-size", Label: "dp-size", Description: "Data parallelism size", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "mem-fraction-static", Label: "mem-fraction-static", Description: "GPU memory reserved for KV cache", Default: "0.9"},
		{Flag: "dtype", Label: "dtype", Description: "Weights dtype", Default: "auto"},
		{Flag: "quantization", Label: "quantization", Description: "Quantization method"},
		{Flag: "context-length", Label: "context-length", Description: "Max context length (0 = model default)", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true},
		{Flag: "port", Label: "port", Description: "Server port", IsPort: true, Default: "30000"},
		{Flag: "served-model-name", Label: "served-model-name", Description: "Name exposed in the API"},
	},
}

// essentialsFor retorna os campos essenciais de um kind que existem no schema.
func essentialsFor(kind domain.BackendKind, schema domain.FlagSchema) []EssentialField {
	fields := essentialFields[kind]
	if len(fields) == 0 {
		// fallback: kind desconhecido → trata como llama p/ não regredir
		fields = essentialFields[domain.BackendKindLlamaServer]
	}
	out := make([]EssentialField, 0, len(fields))
	for _, f := range fields {
		if _, ok := schema.Lookup(f.Flag); ok {
			out = append(out, f)
		}
	}
	return out
}

// coerce converte um valor armazenado em Args para a string do editor,
// honrando o hook custom do campo (ex.: flash-attn bool→"on").
func (f EssentialField) coerce(v any) string {
	if f.Coerce != nil {
		return f.Coerce(v)
	}
	return ArgString(v)
}
```

> Nota: `model`/`model-path` **não** entram na registry — o caminho do modelo é editado pelo campo único "Model" do grupo 1 e mapeado para `domain.Profile.Model`. Ver §7 (dependência de launch a verificar).

---

## 4. Implementação por fase

Cada fase compila e passa nos testes isoladamente, permitindo PRs incrementais.

### Fase 0 — Fundação (sem mudança de comportamento)

**Arquivo novo:** `internal/ui/pages/profile_editor/essentials.go` (§3.1).

**Teste novo:** `essentials_test.go` — para cada kind, `essentialsFor(kind, <embedded schema>)` resolve N campos esperados e omite os ausentes. Usa `llamahelp.EmbeddedSchema()`, `vllmhelp.EmbeddedSchema()`, `sglanghelp.EmbeddedSchema()`.

**Critério:** `go build ./... && go test ./internal/ui/pages/profile_editor/`.

---

### Fase 1 — Migração do modelo `Draft`

**`draft.go`:**

1. Remover campos `NGL, CtxSize, BatchSize, UBatchSize, Port, FlashAttn, CacheTypeK, CacheTypeV` da struct `Draft` (linhas 53-60).
2. Adicionar:
   ```go
   // Essentials guarda os valores (string do editor) das flags promovidas
   // ao grupo Essentials, chaveadas pelo long name canônico do schema.
   Essentials map[string]string
   ```
3. Garantir inicialização não-nil onde `Draft` é criado (Open semeia; ver Fase 3).

**Auditar e atualizar todas as referências aos campos removidos** (lista exata obtida por grep — atualizar nestes pontos):
- `draft.go:127-172` (`ApplyToWithSchema`) → reescrito na Fase 3.
- `draft.go:256-267` (`buildForm`) → reescrito na Fase 2.
- `editor.go:298` (`suggestAppliedMsg` handler) → Fase 4.
- `sizing.go:30` (`newSizingTabForDraft`) → Fase 4.
- `profiles_crud.go:164-171, 232-239, 250` → Fase 3.
- **Testes:** rodar `grep -rn '\.NGL\|\.CtxSize\|\.BatchSize\|\.UBatchSize\|\.FlashAttn\|\.CacheType\|d\.Port' internal/ --include='*_test.go'` e migrar cada asserção para `Draft.Essentials["..."]`.

> Esta fase só fecha junto com 2–4 (a remoção dos campos quebra os call sites). Tratar Fases 1–4 como um único PR coeso, ou manter os campos como aliases deprecados temporariamente. **Recomendado:** PR único 1–4.

---

### Fase 2 — `buildForm` dinâmico + binding por ponteiros

**`draft.go` — nova assinatura e corpo:**

```go
// buildForm monta o form e devolve os ponteiros de binding dos campos
// essenciais (long-name → *string), que o Editor sincroniza de volta
// para draft.Essentials após cada Update.
func buildForm(d *Draft, schema domain.FlagSchema, backendOpts []huh.Option[string], kind domain.BackendKind) (*huh.Form, map[string]*string) {
	ptrs := map[string]*string{}

	// Grupo 1: identidade + modelo + seletor de backend (inalterado, exceto label do Model)
	g1Fields := []huh.Field{
		huh.NewInput().Title("Name").Description("Unique profile identifier").Value(&d.Name),
		huh.NewInput().Title("Description").Value(&d.Description),
		huh.NewInput().Title("Tags").Description("comma-separated").Value(&d.Tags),
		huh.NewInput().Title(modelLabel(kind)).Description(modelDesc(kind)).Value(&d.Model),
	}
	if len(backendOpts) > 0 {
		g1Fields = append(g1Fields, huh.NewSelect[string]().Title("Backend").
			Description("Which backend engine to use").Options(backendOpts...).Value(&d.BackendID))
	}
	groups := []*huh.Group{huh.NewGroup(g1Fields...)}

	// Grupo 2: Essentials dinâmicos a partir da registry + schema
	if ess := essentialsFor(kind, schema); len(ess) > 0 {
		fields := make([]huh.Field, 0, len(ess))
		for _, f := range ess {
			spec, _ := schema.Lookup(f.Flag)
			// ponteiro estável semeado do valor atual do draft
			val := d.Essentials[spec.Long]
			p := &val
			ptrs[spec.Long] = p
			fields = append(fields, essentialField(f, spec, p))
		}
		groups = append(groups, huh.NewGroup(fields...))
	}

	// Grupo 3: restart policy (inalterado)
	groups = append(groups, huh.NewGroup(/* restart fields como hoje */))

	return huh.NewForm(groups...).WithShowHelp(true), ptrs
}

// essentialField gera o huh.Field certo para o tipo do spec.
func essentialField(f EssentialField, spec domain.FlagSpec, p *string) huh.Field {
	title := f.Label
	if title == "" {
		title = spec.Long
	}
	title = decorateLabel(title, spec) // anexa help/default (ver labelWithHelp atual)
	desc := f.Description
	if desc == "" {
		desc = spec.HelpText
	}
	switch spec.Type {
	case domain.FlagTypeEnum:
		return huh.NewSelect[string]().Title(title).Description(desc).
			Options(toOptions(spec.EnumValues)...).Value(p)
	case domain.FlagTypeBool:
		return huh.NewSelect[string]().Title(title).Description(desc).
			Options(toOptions([]string{"true", "false"})...).Value(p)
	case domain.FlagTypeFloat:
		return huh.NewInput().Title(title).Description(desc).Value(p).Validate(floatValidator(f.AllowEmpty))
	case domain.FlagTypeInt:
		return huh.NewInput().Title(title).Description(desc).Value(p).Validate(intValidatorFor(f))
	default: // string
		return huh.NewInput().Title(title).Description(desc).Value(p)
	}
}

// intValidatorFor escolhe portValidator/intRange conforme as dicas do campo.
func intValidatorFor(f EssentialField) func(string) error {
	if f.IsPort {
		return portValidator()
	}
	min, max := -1<<30, 1<<30
	if f.Min != nil { min = *f.Min }
	if f.Max != nil { max = *f.Max }
	return intRange(min, max, f.AllowEmpty)
}

func floatValidator(allowEmpty bool) func(string) error { /* parse ParseFloat, "" se allowEmpty */ }

func modelLabel(kind domain.BackendKind) string {
	if kind == domain.BackendKindLlamaServer || kind == "" {
		return "Model path (.gguf)"
	}
	return "Model (HF repo id or local path)"
}
func modelDesc(kind domain.BackendKind) string { /* idem, ctrl+p para picker */ }
```

- Manter `intRange`, `portValidator`, `toOptions`, `selectOptions` existentes.
- Renomear/generalizar `labelWithHelp` → `decorateLabel(title, spec)` (mesma lógica, recebe spec direto).

**`editor.go`:**

1. Novo campo na struct `Editor`: `essentialPtrs map[string]*string`.
2. Em todo call site de `buildForm`, capturar o segundo retorno:
   - `Open` (`:165`), `SetModelPath` (`:197`), `reloadSchema` (`:672`), `forwardToForm` submit-rebuild (`:641`, `:647`).
   ```go
   e.form, e.essentialPtrs = buildForm(e.draft, e.schema, e.backendOptions, e.backendKind)
   ```
3. Novo método de sync:
   ```go
   func (e Editor) syncEssentials() Editor {
       if e.draft == nil { return e }
       if e.draft.Essentials == nil { e.draft.Essentials = map[string]string{} }
       for k, p := range e.essentialPtrs {
           if p != nil { e.draft.Essentials[k] = *p }
       }
       return e
   }
   ```
4. Em `forwardToForm`, logo após `e.form.Update(msg)` (`:629-632`):
   ```go
   e = e.syncEssentials()
   ```
   Isso garante que dirty-check, preview (`View` `:254`) e validação de submit (`:644`) enxerguem os valores digitados.

**Critério:** abrir editor de cada backend mostra os campos certos; trocar backend troca os campos; digitar reflete no preview.

---

### Fase 3 — `ApplyToWithSchema` genérico + hidratação em `Open()`

**`draft.go` — `ApplyToWithSchema` reescrito:**

```go
func (d Draft) ApplyToWithSchema(base domain.Profile, schema domain.FlagSchema) domain.Profile {
	args := map[string]any{}
	hasSchema := len(schema.Flags) > 0

	// conjunto de longs essenciais p/ dedupe da chave legada
	essLong := map[string]bool{}
	for k := range d.Essentials { essLong[k] = true }

	include := func(key string, val any) {
		if !hasSchema { args[key] = val; return }
		if _, ok := schema.Lookup(key); ok { args[key] = val }
	}

	// 1. base.Args válidos, exceto os que colidem com um essencial (por long canônico)
	for k, v := range base.Args {
		if hasSchema {
			if spec, ok := schema.Lookup(k); ok && essLong[spec.Long] {
				continue // será sobrescrito pelo Essentials sob o long canônico
			}
		}
		include(k, v)
	}

	// 2. Advanced
	for k, v := range d.Args { include(k, v) }

	// 3. Essentials (precedência), sob o long canônico, parseados por tipo
	for long, raw := range d.Essentials {
		if strings.TrimSpace(raw) == "" { continue } // vazio = usar default/unset
		if !hasSchema { args[long] = raw; continue }
		spec, ok := schema.Lookup(long)
		if !ok { continue }
		val, err := parseFlagValue(raw, schema, long)
		if err != nil { continue } // validador já barrou; defensivo
		args[spec.Long] = val
	}

	out := base
	out.ID = d.ID
	out.Name = d.Name
	out.Description = d.Description
	out.Tags = ParseTags(d.Tags)
	out.Model = d.Model
	out.Args = args
	out.Launch.DefaultBackground = true
	out.Launch.BackendID = d.BackendID
	if len(d.Env) > 0 {
		out.Launch.Env = append([]domain.EnvVar(nil), d.Env...)
	} else {
		out.Launch.Env = nil
	}
	out.Launch.RestartPolicy = domain.RestartPolicy(d.RestartPolicy)
	if v, err := strconv.Atoi(d.MaxRestarts); err == nil { out.Launch.MaxRestarts = v }
	if v, err := strconv.Atoi(d.BackoffSeconds); err == nil { out.Launch.BackoffSeconds = v }
	return out
}
```

**`editor.go` — hidratação em `Open()`** (após `loadSchemaForDraft`, antes de `buildForm`, ~`:164`):

```go
func (e Editor) hydrateEssentials() Editor {
	if e.draft == nil { return e }
	if e.draft.Essentials == nil { e.draft.Essentials = map[string]string{} }
	if e.draft.Args == nil { e.draft.Args = map[string]any{} }
	for _, f := range essentialsFor(e.backendKind, e.schema) {
		spec, ok := e.schema.Lookup(f.Flag)
		if !ok { continue }
		// procura valor existente em Args sob long/short/aliases
		candidates := append([]string{spec.Long, spec.Short}, spec.Aliases...)
		found := false
		for _, key := range candidates {
			if key == "" { continue }
			if v, present := e.draft.Args[key]; present {
				if _, set := e.draft.Essentials[spec.Long]; !set {
					e.draft.Essentials[spec.Long] = f.coerce(v)
				}
				delete(e.draft.Args, key) // evita duplicar na aba Advanced
				found = true
			}
		}
		// semeia default se ausente
		if !found {
			if _, set := e.draft.Essentials[spec.Long]; !set && f.Default != "" {
				e.draft.Essentials[spec.Long] = f.Default
			}
		}
	}
	// re-snapshot para o dirty-check refletir o estado pós-hidratação
	e.openSnapshot = *e.draft
	return e
}
```

Em `Open` (`:164-165`):
```go
e = e.loadSchemaForDraft()
e = e.hydrateEssentials()
e.form, e.essentialPtrs = buildForm(e.draft, e.schema, e.backendOptions, e.backendKind)
```

> **Importante:** o re-snapshot dentro de `hydrateEssentials` deve acontecer *depois* da semeadura de defaults, senão abrir um profile novo já apareceria "dirty". `Open` hoje faz `e.openSnapshot = dp` em `:162` — mover/garantir que o snapshot final seja pós-hidratação.

**Troca de backend (`reloadSchema`, `editor.go:659-685`):** após recarregar schema/kind, chamar `e = e.hydrateEssentials()` antes de `buildForm`, para semear defaults do novo backend e peelar flags que agora viraram essenciais. (Não re-snapshotar aqui — trocar backend É uma mudança suja legítima; usar uma variante `hydrateEssentials(reSnapshot bool)` ou extrair o peel/seed sem o snapshot.)

**`profiles_crud.go` — simplificar:**

1. `newDraftDefaults` (`:160-184`): remover os seeds `NGL/CtxSize/...`; manter `Name`, `RestartPolicy`, `MaxRestarts`, `BackoffSeconds`, `IsNew`, `BackendID` (do catálogo). `Essentials` fica nil → `Open` semeia defaults do kind. `Args: map[string]any{}`.
2. `startEditSelected` (`:225-254`): remover o preenchimento dos 8 campos e a skip-list literal (`:232-254`); passar `Args: <cópia de todo pr.Args>`. `Open` faz o peeling.
   ```go
   d := profile_editor.Draft{
       ID: pr.ID, Name: pr.Name, Description: pr.Description,
       Tags: profile_editor.FormatTags(pr.Tags), Model: pr.Model,
       BackendID: pr.Launch.BackendID,
       Env: append([]domain.EnvVar(nil), pr.Launch.Env...),
       RestartPolicy: string(pr.Launch.RestartPolicy),
       MaxRestarts: strconv.Itoa(pr.Launch.MaxRestarts),
       BackoffSeconds: strconv.Itoa(pr.Launch.BackoffSeconds),
       Args: copyArgs(pr.Args),
   }
   ```

**Critério:** editar um profile llama existente (com `Args["ngl"]`, `Args["ctx-size"]`…) carrega os valores nos campos certos; salvar re-grava sob os longs canônicos (`n-gpu-layers`, `ctx-size`…) sem duplicar a chave legada; aba Advanced não mostra as flags essenciais.

---

### Fase 4 — Sizing + suggest

**`sizing.go:24-38` (`newSizingTabForDraft`):**
```go
if ngl, err := strconv.ParseUint(d.Essentials["n-gpu-layers"], 10, 64); err == nil {
	totalLayers = ngl
}
```

**`editor.go:296-301` (handler `suggestAppliedMsg`):**
```go
if suggest, ok := msg.(suggestAppliedMsg); ok {
	if e.draft != nil {
		if e.draft.Essentials == nil { e.draft.Essentials = map[string]string{} }
		e.draft.Essentials["n-gpu-layers"] = fmt.Sprintf("%d", suggest.ngl)
		// reflete no ponteiro de binding e reconstrói o form
		if p := e.essentialPtrs["n-gpu-layers"]; p != nil { *p = e.draft.Essentials["n-gpu-layers"] }
		e.form, e.essentialPtrs = buildForm(e.draft, e.schema, e.backendOptions, e.backendKind)
		return e, e.form.Init()
	}
	return e, nil
}
```

**Critério:** na aba Sizing (llama), pressionar `s` sugere NGL e o valor aparece no campo `n-gpu-layers` ao voltar para Essentials.

---

### Fase 5 — Polimento de UX (opcional, mesmo PR ou follow-up)

- Label/descrição do campo Model já tornados kind-aware na Fase 2 (`modelLabel`).
- `schemaRows` (`draft.go:381-407`): o skip de `"model-path"` (`:391`) deve também pular flags essenciais do kind atual, para não duplicá-las na aba Advanced. Generalizar:
  ```go
  essential := map[string]bool{}
  for _, f := range essentialsFor(kind, schema) {
      if spec, ok := schema.Lookup(f.Flag); ok { essential[spec.Long] = true }
  }
  // dentro do loop: if essential[name] { continue }
  ```
  Isso exige passar `kind` para `newAdvancedTable`/`schemaRows`. Atualizar as 2 chamadas (`Open`/`reloadSchema`/`New`).
  > Alternativa mais simples: como `hydrateEssentials` já deleta as chaves essenciais de `Args`, e `schemaRows` lista `schema.Flags` (não `Args`), as flags essenciais ainda apareceriam na tabela mas com valor vazio. Decidir: (a) escondê-las da Advanced (recomendado, evita dois pontos de edição), ou (b) deixá-las visíveis como atalho. **Recomendado (a).**

---

### Fase 6 — Testes e documentação

**Testes (tabela por backend):**
- `essentials_test.go` (Fase 0).
- `draft_test.go`: `ApplyToWithSchema` para llama/vllm/sglang — Essentials viram args sob long canônico; dedupe da chave legada `ngl`→`n-gpu-layers`; precedência base<Advanced<Essentials; valor vazio é omitido.
- `editor_test.go`:
  - `Open` de profile llama legado (Args com `ngl`,`ctx-size`,`flash-attn` bool) hidrata Essentials corretamente e limpa Args.
  - Trocar backend (`forwardToForm` com BackendID novo) reconstrói essentials do novo kind e semeia defaults.
  - `syncEssentials`: digitar num campo essencial reflete em `CurrentDraft().Essentials` e em `Dirty()`.
  - profile novo recém-aberto **não** está `Dirty()` (re-snapshot pós-hidratação).
  - suggest grava em `n-gpu-layers`.
- `profiles_test.go`: ajustar os testes existentes que referenciam campos removidos (grep da Fase 1).

**Docs:**
- `CLAUDE.md` (seção do profile_editor) e `internal/ui/pages/profile_editor` (se houver AGENTS.md): documentar a registry `essentialFields`, o contrato `Draft.Essentials` (long-keyed) e o binding via `essentialPtrs`+`syncEssentials`.
- `docs/` se houver spec do editor.

**Critério final:** `make build && make tests` verdes; validação manual via `/run` ou TUI nos 3 backends.

---

## 5. Migração e back-compat

| Cenário | Comportamento |
|---------|---------------|
| Profile llama antigo (`Args["ngl"]`, `Args["flash-attn": true]`) | `hydrateEssentials` lê via short/alias, coage bool→"on", limpa Args; save re-grava `Args["n-gpu-layers"]`, `Args["flash-attn":"on"]`. |
| Chave legada vs canônica no edit | `ApplyToWithSchema` etapa 1 descarta `base.Args["ngl"]` (Lookup→long `n-gpu-layers` ∈ essLong) e re-emite sob o canônico. Sem duplicação. |
| Profile vLLM/SGLang sem essentials salvos | `hydrateEssentials` semeia defaults da registry (não marca dirty por causa do re-snapshot). |
| Backend kind desconhecido | `essentialsFor` cai no conjunto llama (não regride). |
| Flag essencial ausente no schema (ex.: build antigo) | omitida do form (sem crash). |

Nenhuma migração de arquivo em disco é necessária: a normalização acontece no próximo save do profile.

---

## 6. Riscos e mitigações

| Risco | Mitigação |
|-------|-----------|
| huh não permite `&map[k]` | Ponteiros heap dedicados em `essentialPtrs`, sync explícito (D2). |
| Dirty-check falso-positivo ao abrir | Re-snapshot pós-hidratação em `Open` (§Fase 3). |
| Perda de bounds curados (`intRange(-1,9999)`) | Bounds movidos para `EssentialField.Min/Max`; `port` via `IsPort`. |
| flash-attn bool legado | `Coerce: FlashAttnToString` na entrada da registry. |
| Duplicação Essentials↔Advanced | Peel deleta de `Args` + Advanca esconde essenciais (Fase 5a). |
| PR grande (Fases 1–4 acopladas) | Revisar como um todo; cobertura de testes por backend antes do merge. |

---

## 7. Fora de escopo / premissas a verificar

1. **Mapeamento `Profile.Model` → flag do backend no launch.** O form mantém um único campo "Model" (grupo 1) que alimenta `domain.Profile.Model`. **Verificar** se a camada de launch (`processmgr`/`backendcatalog` resolver/argbuilder) traduz `Profile.Model` para `--model` (llama/vllm) e `--model-path` (sglang). Se *não* traduzir, será preciso, num follow-up, ou (a) incluir `model`/`model-path` como essencial por kind, ou (b) ajustar o argbuilder. **Não tratar neste plano** — apenas confirmar para não quebrar vLLM/SGLang no launch.
2. **Refresh de schema embutido (vLLM/SGLang).** Schemas estáticos podem defasar; fora deste escopo (gap separado já levantado).
3. **Validação semântica cross-flag** (ex.: `tensor-parallel-size` divide nº de GPUs): fora de escopo.
4. **`gpu-memory-utilization`/`mem-fraction-static` bounds (0,1]:** o `floatValidator` aqui só valida parse; um bound float opcional pode ser adicionado à `EssentialField` num follow-up se desejado.

---

## 8. Checklist de execução

- [ ] Fase 0: `essentials.go` + `essentials_test.go`.
- [ ] Fase 1: migrar `Draft` (remover 8 campos, add `Essentials`); grep de referências em código e testes.
- [ ] Fase 2: `buildForm` dinâmico + `essentialPtrs` + `syncEssentials` em `forwardToForm`.
- [ ] Fase 3: `ApplyToWithSchema` genérico + `hydrateEssentials` em `Open`/`reloadSchema`; simplificar `profiles_crud.go`.
- [ ] Fase 4: `sizing.go` + handler suggest → `Essentials["n-gpu-layers"]`.
- [ ] Fase 5: esconder essenciais da aba Advanced; label do Model kind-aware.
- [ ] Fase 6: testes por backend + docs.
- [ ] `make build && make tests` verdes.
- [ ] Verificar premissa §7.1 (Model→flag no launch).
- [ ] `gitnexus_detect_changes()` antes do commit.

---

## 9. Arquivos tocados (resumo)

| Arquivo | Mudança |
|---------|---------|
| `internal/ui/pages/profile_editor/essentials.go` | **novo** — registry + helpers. |
| `internal/ui/pages/profile_editor/draft.go` | `Draft` (campos), `buildForm` (dinâmico), `ApplyToWithSchema` (genérico), `schemaRows` (esconder essenciais), helpers de validador/label. |
| `internal/ui/pages/profile_editor/editor.go` | `essentialPtrs`, `syncEssentials`, `hydrateEssentials`, call sites de `buildForm`, handler suggest. |
| `internal/ui/pages/profile_editor/sizing.go` | ler NGL de `Essentials`. |
| `internal/ui/pages/profiles_crud.go` | `newDraftDefaults` e `startEditSelected` simplificados. |
| `internal/ui/pages/profile_editor/*_test.go`, `profiles_test.go` | cobertura nova + migração de asserções. |
| `CLAUDE.md` (+ AGENTS.md se aplicável) | documentar o novo contrato. |

> Núcleo do risco concentrado em `draft.go` + `editor.go`. `domain.FlagSpec` **não** é alterado (decisão D1) — zero blast radius fora da UI.
