# Configweb UI/UX Polish & Functional Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the on-demand web profile/backend editor (`internal/service/configweb`) more beautiful and functional: refined dark visual system, inline+statusbar validation, save-blocking on errors, lightweight toasts, keyboard shortcuts, and several correctness fixes — all within the existing HTMX + Alpine + pure-CSS (no build step) stack.

**Architecture:** Pure-CSS design-token refactor (no pipeline) drives the visual layer. Validation stays server-rendered (`#issues` partial with `data-field`) but a small client-side decorator reads that partial after every HTMX swap and toggles inline field errors. Save runs the validator server-side and refuses to persist when errors exist, returning the same issues partial instead of an `HX-Redirect`. A tiny Alpine toast store surfaces Customize-mode action feedback. Keyboard shortcuts and the done-page behavior are thin client additions.

**Tech Stack:** Go `html/template`, HTMX, Alpine.js, hand-written CSS with custom properties (all embedded via `assets.FS`). No Node, no bundler, no new dependencies.

---

## Decisions (from grill-me interview, 2026-05-24)

1. **Scope:** all three surfaces — Configure (`configure.gohtml`), Customize (`customize.gohtml`), Backend editor (`backend.gohtml`).
2. **Nature:** visual polish + functional UX + structural refinement.
3. **Stack:** tokenized pure CSS, **no build pipeline**; keep HTMX/Alpine, assets embedded.
4. **Aesthetic:** refine the existing dark navy + blue-accent theme (no light mode).
5. **Configure layout:** keep master-detail (sidebar groups + content), refine it.
6. **Validation feedback:** inline field highlight + message **and** statusbar summary (wire the dead `data-field`/`.error` hooks).
7. **Save with errors:** block on errors, allow warnings.
8. **Customize action feedback:** lightweight Alpine toasts.
9. **Keyboard shortcuts:** `Ctrl/Cmd+S` = Save (respects error block), `Esc` = Cancel (confirm only if dirty), `/` = focus search.
10. **Done page:** attempt `window.close()` then show a clean English fallback (fixes current Portuguese page).
11. **Process:** written plan first (this document).

## Pre-flight Facts (verified against code at plan time)

- Tests in `internal/service/configweb/*_test.go` are **assertion-based (`strings.Contains`)**, not golden-file. Markers that must be preserved: `name="arg.ctx-size"`, `value="4096"`, `Essentials`, `Cross-field rules`, `/customize/flag` (see `render_test.go:22-49`).
- `TestSaveHandler_PersistsAndCompletes` (`handlers_test.go:76-101`) saves a **valid** profile (model set, valid int) → the save-blocking change must NOT break it; it must still redirect/complete.
- `renderIssues` (`handlers.go:86-101`) already emits `<div class="issue error|warn" data-field="...">`. `.field input.error` CSS (`app.css:343-347`) and `.field-error` (`app.css:356-361`) already exist but nothing toggles/fills them.
- Templates have **no** custom JS yet (clean slate for listeners).
- Missing CSS classes used by templates: `env-section`, `env-list`, `env-row`, `env-key`, `env-value`, `secondary` (button in `configure.gohtml:48-63`), and `.tab-btn.hl` (referenced in `configure.gohtml:5-7`). Toast styles do not exist.
- Routes live in `server.go`; done pages are `handleClosed` (`handlers.go:150-153`, **Portuguese**) and `handleBackendClosed`.

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `internal/service/configweb/assets/static/app.css` | All visual styling + new tokens, env-vars, toasts, inline-error, done page | Modify (largest change) |
| `internal/service/configweb/assets/templates/base.gohtml` | Page shell: add toast container, global shortcut + inline-validation + dirty-tracking JS | Modify |
| `internal/service/configweb/assets/templates/configure.gohtml` | Field error slots, env-vars markup classes, search affordance | Modify |
| `internal/service/configweb/assets/templates/customize.gohtml` | Toast feedback on actions, remove-flag DOM removal, replace `alert()` | Modify |
| `internal/service/configweb/assets/templates/backend.gohtml` | Toast container, shortcut hook, consistent field error slots | Modify |
| `internal/service/configweb/handlers.go` | Save blocks on validator errors; English done page | Modify |
| `internal/service/configweb/backend_handlers.go` | English backend done page (if Portuguese) | Modify (verify first) |
| `internal/service/configweb/handlers_test.go` | New test: save blocked on validation error | Modify |
| `internal/service/configweb/render_test.go` | Optional: assert new markers (toast root, error slots) | Modify |

---

## Task 1: Refine design tokens & visual system (CSS foundation)

Pure visual; no Go logic. Verified by loading the editor and observing. This task only touches the `:root` token block and global element styling so later tasks build on a refreshed base.

**Files:**
- Modify: `internal/service/configweb/assets/static/app.css:1-44` (`:root` + body)

- [ ] **Step 1: Expand and refine the token block**

Replace the existing `:root{...}` block (lines 1-32) with an extended, more deliberate scale. Keep every existing variable name (downstream rules depend on them) and ADD new ones:

```css
:root{
  /* Surfaces — layered for depth */
  --bg:#0a0e1a;
  --panel:#111729;
  --panel-2:#161d33;
  --elevated:#1a2238;
  --ink:#e8ecf5;
  --ink-dim:#aeb6c9;
  --muted:#7a8499;
  --accent:#6ea8fe;
  --accent-hover:#86b8ff;
  --accent-dim:rgba(110,168,254,0.12);
  --accent-glow:rgba(110,168,254,0.30);
  --err:#ff6b6b;
  --err-dim:rgba(255,107,107,0.15);
  --warn:#ffcf66;
  --warn-dim:rgba(255,207,102,0.15);
  --ok:#5fd38a;
  --ok-dim:rgba(95,211,138,0.15);
  --border:#1e2742;
  --border-strong:#2d3a5c;
  --input-bg:#0a0e18;
  --input-bd:#253050;
  --hover-bg:rgba(255,255,255,0.04);
  /* Type scale */
  --text-xs:0.75rem;
  --text-sm:0.875rem;
  --text-base:1rem;
  --text-md:1.125rem;
  --text-lg:1.35rem;
  --text-xl:1.6rem;
  /* Spacing */
  --sp-1:0.25rem;
  --sp-2:0.5rem;
  --sp-3:0.75rem;
  --sp-4:1rem;
  --sp-5:1.25rem;
  --sp-6:1.5rem;
  --sp-8:2rem;
  --sp-10:2.5rem;
  /* Radii */
  --radius-sm:4px;
  --radius-md:8px;
  --radius-lg:12px;
  --radius-xl:16px;
  /* Elevation */
  --shadow:0 4px 16px rgba(0,0,0,0.45);
  --shadow-sm:0 1px 3px rgba(0,0,0,0.4);
  --shadow-lg:0 12px 32px rgba(0,0,0,0.55);
  /* Motion */
  --ease:cubic-bezier(0.16,1,0.3,1);
  --dur:0.18s;
}
```

- [ ] **Step 2: Refine body typography baseline**

Replace the `body{...}` rule (lines 39-44) with:

```css
body{
  font:var(--text-base)/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
  background:
    radial-gradient(1200px 600px at 80% -10%, rgba(110,168,254,0.06), transparent 60%),
    var(--bg);
  background-attachment:fixed;
  color:var(--ink);
  -webkit-font-smoothing:antialiased;
  text-rendering:optimizeLegibility;
}
```

- [ ] **Step 3: Build and load the editor to verify the base reads correctly**

Run: `cd /home/diogo/dev/model-loader && make build`
Expected: builds with no errors. (Visual verification of the running editor happens at the end of Task 7; the token change is non-breaking because all old names are preserved.)

- [ ] **Step 4: Commit**

```bash
git add internal/service/configweb/assets/static/app.css
git commit -m "style(configweb): expand and refine design token system"
```

---

## Task 2: Style the missing components (env vars, secondary button, highlighted tab, toasts, inline error)

Adds the CSS that templates already reference but that does not exist, plus the toast and inline-error styling that later tasks depend on. Pure CSS — append to the end of `app.css`.

**Files:**
- Modify: `internal/service/configweb/assets/static/app.css` (append at end, after line 717)

- [ ] **Step 1: Append env-vars, secondary button, highlighted-tab, toast, and inline-error styles**

Append to the end of `app.css`:

```css
/* ── Environment variables section ── */
.env-section{
  margin:var(--sp-6) 0 var(--sp-8);
  padding:var(--sp-5);
  background:var(--panel);
  border:1px solid var(--border);
  border-radius:var(--radius-lg);
}
.env-section .section-header{margin-bottom:var(--sp-4)}
.env-section .section-header h3{
  font-size:var(--text-md);
  font-weight:700;
  margin:0;
  color:var(--ink);
}
.env-list{
  display:flex;
  flex-direction:column;
  gap:var(--sp-3);
  margin-bottom:var(--sp-4);
}
.env-row{
  display:grid;
  grid-template-columns:minmax(0,1fr) minmax(0,2fr) auto;
  gap:var(--sp-3);
  align-items:center;
}
.env-row input{
  background:var(--input-bg);
  border:1px solid var(--input-bd);
  color:var(--ink);
  border-radius:var(--radius-md);
  padding:0.6rem 0.85rem;
  font-size:var(--text-sm);
  outline:none;
  transition:border-color var(--dur) var(--ease),box-shadow var(--dur) var(--ease);
}
.env-row input.env-key{font-family:'SF Mono',ui-monospace,monospace}
.env-row input:focus{
  border-color:var(--accent);
  box-shadow:0 0 0 3px var(--accent-dim);
}
.env-row .ghost{
  padding:0.45rem 0.7rem;
  font-size:var(--text-md);
  line-height:1;
}

/* ── Secondary button (add env var, low-emphasis adds) ── */
button.secondary{
  background:var(--accent-dim);
  border:1px solid transparent;
  color:var(--accent);
  padding:0.5rem 1rem;
  border-radius:var(--radius-md);
  font-size:var(--text-sm);
  font-weight:600;
  cursor:pointer;
  transition:all var(--dur) var(--ease);
}
button.secondary:hover{
  background:var(--accent-glow);
  color:var(--accent-hover);
}

/* ── Highlighted sidebar group (Essentials) ── */
.tab-btn.hl .star::before{content:"★ ";color:var(--warn)}
.tab-btn.hl{font-weight:600}

/* ── Inline field error ── */
.field-error{
  display:none;
}
.field.has-error .field-error{
  display:block;
}
.field.has-error input,
.field.has-error select{
  border-color:var(--err);
  box-shadow:0 0 0 3px var(--err-dim);
}

/* ── Toasts ── */
.toast-root{
  position:fixed;
  bottom:var(--sp-6);
  right:var(--sp-6);
  z-index:1000;
  display:flex;
  flex-direction:column;
  gap:var(--sp-3);
  pointer-events:none;
}
.toast{
  pointer-events:auto;
  min-width:200px;
  max-width:360px;
  padding:var(--sp-3) var(--sp-4);
  border-radius:var(--radius-md);
  background:var(--elevated);
  border:1px solid var(--border-strong);
  box-shadow:var(--shadow-lg);
  font-size:var(--text-sm);
  font-weight:500;
  color:var(--ink);
  display:flex;
  align-items:center;
  gap:var(--sp-3);
  animation:toast-in var(--dur) var(--ease);
}
.toast.ok{border-left:3px solid var(--ok)}
.toast.error{border-left:3px solid var(--err)}
.toast.info{border-left:3px solid var(--accent)}
@keyframes toast-in{
  from{opacity:0;transform:translateY(8px)}
  to{opacity:1;transform:translateY(0)}
}

/* ── Done page ── */
.done-page{
  min-height:100vh;
  display:flex;
  flex-direction:column;
  align-items:center;
  justify-content:center;
  gap:var(--sp-4);
  text-align:center;
  padding:var(--sp-8);
}
.done-page .done-icon{
  width:64px;height:64px;
  border-radius:50%;
  display:flex;align-items:center;justify-content:center;
  font-size:2rem;
  background:var(--ok-dim);
  color:var(--ok);
}
.done-page h2{margin:0;font-size:var(--text-xl);font-weight:700}
.done-page p{margin:0;color:var(--muted)}

/* ── Responsive: collapse 2-col grids on narrow viewports ── */
@media (max-width:720px){
  .top-fields,.fields-grid,.flag-fields-grid{grid-template-columns:1fr!important}
  .layout{grid-template-columns:1fr!important}
  .sidebar{display:none}
}
```

- [ ] **Step 2: Build to verify CSS parses (served as static asset)**

Run: `cd /home/diogo/dev/model-loader && make build`
Expected: builds with no errors (CSS is embedded verbatim; a syntax slip won't fail the build but will be caught visually in Task 7).

- [ ] **Step 3: Commit**

```bash
git add internal/service/configweb/assets/static/app.css
git commit -m "style(configweb): add env-vars, secondary button, toast, inline-error and done-page styles"
```

---

## Task 3: Mark the Essentials group with the highlighted-tab affordance

The sidebar already adds class `hl` when `.Highlighted`, but renders no star marker. Add the `.star` span so the new CSS lights up.

**Files:**
- Modify: `internal/service/configweb/assets/templates/configure.gohtml:1-13` (sidebar block)

- [ ] **Step 1: Add the star marker to highlighted groups**

Replace the sidebar `{{define "sidebar"}}` block (lines 1-13) with:

```gotemplate
{{define "sidebar"}}
<div class="sidebar-title">Groups</div>
{{range .Groups}}
<button
  class="tab-btn{{if .Highlighted}} hl{{end}}"
  @click="activeGroup='{{.Name}}'"
  :class="(activeGroup==='{{.Name}}' ? 'tab-btn active' : 'tab-btn') + '{{if .Highlighted}} hl{{end}}'"
>
  {{if .Highlighted}}<span class="star"></span>{{end}}
  {{.Name}}
  <span class="count">{{len .Fields}}</span>
</button>
{{end}}
{{end}}
```

- [ ] **Step 2: Build**

Run: `cd /home/diogo/dev/model-loader && make build`
Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/service/configweb/assets/templates/configure.gohtml
git commit -m "feat(configweb): show star marker on highlighted Essentials group"
```

---

## Task 4: Inline validation decorator + per-field error slots

Server keeps returning the `#issues` partial (with `data-field`). A small client-side decorator runs after every HTMX swap, reads the issues, and toggles `.has-error` + fills `.field-error` on the matching field. Statusbar summary stays. No server change needed for this task.

**Files:**
- Modify: `internal/service/configweb/assets/templates/configure.gohtml:77-99` (field rendering — add error slot + stable id)
- Modify: `internal/service/configweb/assets/templates/base.gohtml` (add decorator script + statusbar summary helper)

- [ ] **Step 1: Add an error slot and field id to each field**

Replace the field loop inner block (lines 77-99 of `configure.gohtml`) with (note the added `data-field` on the wrapper and the `<small class="field-error">` slot):

```gotemplate
      {{range .Fields}}
      <div class="field{{if eq .Widget "text"}} full-width{{end}}" data-field="{{.Flag}}" x-show="search.trim()==='' || fuzzyMatch(search, '{{.Flag}} {{.Label}} {{.Help}}')">
        <label>{{.Label}}{{if .Required}} <span class="req">required</span>{{end}}</label>
        {{if eq .Widget "select"}}
          {{$val := .Value}}
          <select name="arg.{{.Flag}}">
            <option value=""{{if eq $val ""}} selected{{end}}>(default)</option>
            {{range .Options}}<option value="{{.}}"{{if eq . $val}} selected{{end}}>{{.}}</option>{{end}}
          </select>
        {{else if eq .Widget "toggle"}}
          {{$val := .Value}}
          <select name="arg.{{.Flag}}">
            <option value=""{{if eq $val ""}} selected{{end}}>(default)</option>
            <option value="on"{{if eq $val "on"}} selected{{end}}>on</option>
            <option value="off"{{if eq $val "off"}} selected{{end}}>off</option>
          </select>
        {{else if eq .Widget "number"}}
          <input type="number" name="arg.{{.Flag}}" value="{{.Value}}" placeholder="{{if .Default}}{{.Default}} (default){{else}}not configured{{end}}"{{if .Min}} min="{{.Min}}"{{end}}{{if .Max}} max="{{.Max}}"{{end}}>
        {{else}}
          <input name="arg.{{.Flag}}" value="{{.Value}}" placeholder="{{if .Default}}{{.Default}} (default){{else}}not configured{{end}}">
        {{end}}
        {{if .Help}}<small class="muted">{{.Help}}</small>{{end}}
        <small class="field-error"></small>
      </div>
      {{end}}
```

Also add `data-field` slots to the top meta fields. Replace the ID/Name/Model field wrappers (lines 34-45) with:

```gotemplate
    <div class="field" data-field="id">
      <label>ID {{if not .Draft.ID}}<span class="req">*</span>{{end}}</label>
      <input name="id" value="{{.Draft.ID}}" placeholder="profile identifier (slug)">
      <small class="field-error"></small>
    </div>
    <div class="field" data-field="name">
      <label>Name {{if not .Draft.Name}}<span class="req">*</span>{{end}}</label>
      <input name="name" value="{{.Draft.Name}}" placeholder="profile name">
      <small class="field-error"></small>
    </div>
    <div class="field" data-field="model">
      <label>Model <span class="req">*</span></label>
      <input name="model" value="{{.Draft.Model}}" placeholder="model path or repo">
      <small class="field-error"></small>
    </div>
```

- [ ] **Step 2: Add the decorator + statusbar summary script to base.gohtml**

In `base.gohtml`, replace the trailing `<script>...fuzzyMatch...</script>` block (lines 31-42) with the version below (keeps `fuzzyMatch`, adds `decorateIssues` + an `htmx:afterSwap` listener + a statusbar count):

```gotemplate
<script>
function fuzzyMatch(q, text) {
  if (!q) return true;
  q = q.toLowerCase();
  text = text.toLowerCase();
  var qi = 0;
  for (var ti = 0; ti < text.length && qi < q.length; ti++) {
    if (text.charAt(ti) === q.charAt(qi)) qi++;
  }
  return qi === q.length;
}

// Read the server-rendered #issues partial and decorate matching fields inline.
function decorateIssues() {
  document.querySelectorAll('.field.has-error').forEach(function (f) {
    f.classList.remove('has-error');
    var slot = f.querySelector('.field-error');
    if (slot) slot.textContent = '';
  });
  var issues = document.querySelectorAll('#issues .issue[data-field]');
  var errs = 0, warns = 0;
  issues.forEach(function (el) {
    var field = el.getAttribute('data-field');
    var isErr = el.classList.contains('error');
    isErr ? errs++ : warns++;
    if (!field) return;
    var wrap = document.querySelector('.field[data-field="' + CSS.escape(field) + '"]');
    if (!wrap) return;
    if (isErr) {
      wrap.classList.add('has-error');
      var slot = wrap.querySelector('.field-error');
      if (slot && !slot.textContent) slot.textContent = el.textContent.replace(/^[^:]*:\s*/, '');
    }
  });
}

document.addEventListener('htmx:afterSwap', function (e) {
  if (e.target && e.target.id === 'issues') decorateIssues();
});
</script>
```

- [ ] **Step 3: Build and manually verify inline errors appear**

Run: `cd /home/diogo/dev/model-loader && make build`
Then launch the editor (via the TUI Profiles tab → New profile, or by writing a tiny harness) and clear the Model field.
Expected: after the 300ms debounce, the Model field gets a red border and an inline message, and the statusbar still lists the error.

- [ ] **Step 4: Commit**

```bash
git add internal/service/configweb/assets/templates/configure.gohtml internal/service/configweb/assets/templates/base.gohtml
git commit -m "feat(configweb): inline field-level validation errors driven by the issues partial"
```

---

## Task 5: Block Save when the validator reports errors

`handleSave` must run the validator and, if there are errors, return the `#issues` partial (reusing `renderIssues`) WITHOUT `HX-Redirect`, so HTMX swaps it into `#issues` and the Task-4 decorator highlights the fields. Warnings do not block. Save target is already `#issues`.

**Files:**
- Modify: `internal/service/configweb/handlers.go:107-142` (`handleSave`)
- Modify: `internal/service/configweb/handlers_test.go` (new test)

- [ ] **Step 1: Write the failing test for save-blocking-on-error**

Append to `internal/service/configweb/handlers_test.go`:

```go
func TestSaveHandler_BlocksOnValidationError(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := newMemProfileStore()
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	// Missing required model -> validator must produce an error and block save.
	form := url.Values{
		"isNew": {"true"}, "id": {"qwen"}, "name": {"Qwen"},
		"backendId": {"llama"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)

	if rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("expected no HX-Redirect when validation fails")
	}
	if _, err := ps.Get("qwen"); err == nil {
		t.Fatalf("invalid profile must not be persisted")
	}
	if !strings.Contains(rec.Body.String(), `id="issues"`) {
		t.Fatalf("expected issues partial in body, got: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/diogo/dev/model-loader && go test ./internal/service/configweb/ -run TestSaveHandler_BlocksOnValidationError -v`
Expected: FAIL — current `handleSave` persists regardless and sets `HX-Redirect`.

> NOTE: confirm the validator actually errors on a missing model for `BackendKindLlamaServer`. If `model` is not validated as required by `validator.New(nil).Validate`, adjust the test to trigger a guaranteed error instead — e.g. give `ctx-size` a `Min`/`Max` and pass an out-of-range `arg.ctx-size` value:
> ```go
> Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Min: intPtr(1), Max: intPtr(10)}},
> // ... form: "arg.ctx-size": {"99999"}
> ```
> Add `func intPtr(i int) *int { return &i }` to the test file if not already present. Verify which trigger fires by reading `internal/service/validator/`.

- [ ] **Step 3: Implement save-blocking in handleSave**

Replace the body of `handleSave` (lines 107-142) with:

```go
func (s *Session) handleSave(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fs := schema.ToFlagSchema()

	// Block persistence when the profile has validation errors. Warnings pass.
	rep := validator.New(nil).Validate(d.ToProfile(fs), fs, schema.BackendKind)
	if len(rep.Errors) > 0 {
		renderIssues(w, rep)
		return
	}

	var perr error
	if d.IsNew {
		perr = s.deps.Profiles.Create(d.ToProfile(fs))
	} else {
		lookup := d.OrigID
		if lookup == "" {
			lookup = d.ID
		}
		existing, gerr := s.deps.Profiles.Get(lookup)
		if gerr != nil {
			perr = s.deps.Profiles.Create(d.ToProfile(fs))
		} else {
			perr = s.deps.Profiles.Save(d.ApplyTo(existing, fs))
		}
	}
	if perr != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div id="issues" hx-swap-oob="true"><div class="issue error">` + htmlEscape(perr.Error()) + `</div></div>`))
		return
	}
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, ProfileID: d.ID})
}
```

- [ ] **Step 4: Run the new test and the existing save test together**

Run: `cd /home/diogo/dev/model-loader && go test ./internal/service/configweb/ -run TestSaveHandler -v`
Expected: PASS for both `TestSaveHandler_PersistsAndCompletes` (valid → redirects/persists) and `TestSaveHandler_BlocksOnValidationError` (invalid → blocked).

- [ ] **Step 5: Surface a toast on blocked save (client)**

In `base.gohtml`, extend the `htmx:afterSwap` listener added in Task 4 so a blocked save also pushes a toast. Replace the listener with:

```gotemplate
document.addEventListener('htmx:afterSwap', function (e) {
  if (e.target && e.target.id === 'issues') {
    decorateIssues();
    if (e.detail && e.detail.requestConfig && e.detail.requestConfig.path === '/save'
        && document.querySelectorAll('#issues .issue.error').length > 0) {
      window.dispatchEvent(new CustomEvent('toast', {detail:{msg:'Fix errors before saving',type:'error'}}));
    }
  }
});
```

(The `toast` event is consumed by the toast store added in Task 6. This step only emits it.)

- [ ] **Step 6: Commit**

```bash
git add internal/service/configweb/handlers.go internal/service/configweb/handlers_test.go internal/service/configweb/assets/templates/base.gohtml
git commit -m "feat(configweb): block Save on validation errors, surface them inline"
```

---

## Task 6: Toast store + Customize action feedback + remove-flag DOM fix

Add a global Alpine toast store rendered in `base.gohtml` (and `backend.gohtml`), wire Customize actions to emit toasts, fix remove-flag to actually drop the card, and replace `alert()` in `saveRules`.

**Files:**
- Modify: `internal/service/configweb/assets/templates/base.gohtml` (toast root + store)
- Modify: `internal/service/configweb/assets/templates/backend.gohtml` (toast root + store)
- Modify: `internal/service/configweb/assets/templates/customize.gohtml` (toasts, remove-flag swap, replace alert)

- [ ] **Step 1: Add the toast root + Alpine store to base.gohtml**

In `base.gohtml`, add the toast root just before `</body>` (after the `<footer>` line 30). Insert:

```gotemplate
<div class="toast-root" x-data="toastHost()" x-init="init()">
  <template x-for="t in items" :key="t.id">
    <div class="toast" :class="t.type" x-text="t.msg"></div>
  </template>
</div>
```

And add the `toastHost` factory inside the existing `<script>` block (after the `htmx:afterSwap` listener):

```gotemplate
function toastHost(){return{
  items:[],
  init(){
    var self=this;
    window.addEventListener('toast', function(e){ self.push(e.detail.msg, e.detail.type||'info'); });
  },
  push(msg,type){
    var id=Date.now()+Math.random();
    this.items.push({id:id,msg:msg,type:type});
    var self=this;
    setTimeout(function(){ self.items=self.items.filter(function(x){return x.id!==id}); }, 2600);
  }
}}
```

- [ ] **Step 2: Toasts on Customize flag save / add via htmx after-request**

In `customize.gohtml`, add an after-request toast to the per-flag save form. Change the existing flag-card form open tag (line 48) from:

```gotemplate
      <form class="flag-card" hx-post="/customize/flag" hx-target="#issues">
```

to:

```gotemplate
      <form class="flag-card" hx-post="/customize/flag" hx-target="#issues"
        hx-on::after-request="if(event.detail.successful) window.dispatchEvent(new CustomEvent('toast',{detail:{msg:'Flag “{{.Flag}}” saved',type:'ok'}}))">
```

And the "new flag" form (line 10) from:

```gotemplate
    <form class="flag-card flag-card-new" hx-post="/customize/flag/add">
```

to:

```gotemplate
    <form class="flag-card flag-card-new" hx-post="/customize/flag/add"
      hx-on::after-request="if(event.detail.successful){window.dispatchEvent(new CustomEvent('toast',{detail:{msg:'Flag added — reload to edit it',type:'ok'}}))}">
```

- [ ] **Step 3: Fix remove-flag to drop its card from the DOM + toast**

In `customize.gohtml`, replace the Remove button (lines 100-101) with one that targets and removes its own card and shows a toast:

```gotemplate
          <button class="ghost danger" type="button"
            hx-post="/customize/flag/remove" hx-vals='{"backendId":"{{$.BackendID}}","flag":"{{.Flag}}"}'
            hx-target="closest .flag-card" hx-swap="outerHTML swap:0.15s"
            hx-on::after-request="if(event.detail.successful) window.dispatchEvent(new CustomEvent('toast',{detail:{msg:'Flag “{{.Flag}}” removed',type:'ok'}}))">Remove</button>
```

Because `handleCustomizeRemoveFlag` returns an empty 200 body, swapping `outerHTML` removes the card. Confirm the handler writes no body on success (it currently calls `w.WriteHeader(http.StatusOK)` only — good).

- [ ] **Step 4: Replace alert() in saveRules with toasts**

In `customize.gohtml`, replace the `saveRules` method (lines 174-178) with:

```gotemplate
      async saveRules(){
        const r=await fetch('/customize/rules?backendId='+encodeURIComponent(this.backendId),
          {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(this.rules)});
        if(!r.ok){
          window.dispatchEvent(new CustomEvent('toast',{detail:{msg:(await r.text())||'Failed to save rules',type:'error'}}));
        } else {
          window.dispatchEvent(new CustomEvent('toast',{detail:{msg:'Rules saved',type:'ok'}}));
        }
      }
```

- [ ] **Step 5: Add the toast root + store to backend.gohtml**

In `backend.gohtml`, add before `</body>` (after the `<footer>` line 63) the same toast root and ensure the script defining `toastHost` is present. Since `backend.gohtml` is a standalone document (does not include `base`), add a `<script>` block before `</body>`:

```gotemplate
<div class="toast-root" x-data="toastHost()" x-init="init()">
  <template x-for="t in items" :key="t.id">
    <div class="toast" :class="t.type" x-text="t.msg"></div>
  </template>
</div>
<script>
function toastHost(){return{
  items:[],
  init(){var self=this;window.addEventListener('toast',function(e){self.push(e.detail.msg,e.detail.type||'info');});},
  push(msg,type){var id=Date.now()+Math.random();this.items.push({id:id,msg:msg,type:type});var self=this;setTimeout(function(){self.items=self.items.filter(function(x){return x.id!==id});},2600);}
}}
</script>
```

- [ ] **Step 6: Build and run existing configweb tests (no logic regressions)**

Run: `cd /home/diogo/dev/model-loader && make build && go test ./internal/service/configweb/ -v`
Expected: builds; all tests PASS (templates still contain `/customize/flag`, `Cross-field rules`).

- [ ] **Step 7: Commit**

```bash
git add internal/service/configweb/assets/templates/base.gohtml internal/service/configweb/assets/templates/backend.gohtml internal/service/configweb/assets/templates/customize.gohtml
git commit -m "feat(configweb): toast feedback for customize actions; remove-flag drops card; drop alert()"
```

---

## Task 7: Keyboard shortcuts, English done page, and final visual pass

Add `Ctrl/Cmd+S`, `Esc`, `/` shortcuts with dirty-tracking; replace the Portuguese done page with an English auto-close-or-fallback page; final review.

**Files:**
- Modify: `internal/service/configweb/assets/templates/base.gohtml` (shortcuts + dirty flag)
- Modify: `internal/service/configweb/handlers.go:150-153` (`handleClosed`)
- Modify: `internal/service/configweb/backend_handlers.go` (`handleBackendClosed` — verify language first)

- [ ] **Step 1: Add keyboard shortcuts + dirty tracking to base.gohtml**

In `base.gohtml`, extend the `x-data` on `<body>` (line 11) to track dirtiness. Change:

```gotemplate
<body x-data="{mode:'configure',activeGroup:'{{if .Groups}}{{(index .Groups 0).Name}}{{end}}',search:''}">
```

to:

```gotemplate
<body x-data="{mode:'configure',activeGroup:'{{if .Groups}}{{(index .Groups 0).Name}}{{end}}',search:'',dirty:false}"
  @input="dirty=true"
  @keydown.window.prevent.ctrl.s="htmx.trigger(document.getElementById('profile-form'),'manualSave')"
  @keydown.window.prevent.meta.s="htmx.trigger(document.getElementById('profile-form'),'manualSave')"
  @keydown.window.escape="if(!dirty || confirm('Discard unsaved changes?')) htmx.ajax('POST','/cancel',{});"
  @keydown.window.slash="if(mode==='configure' && document.activeElement.tagName!=='INPUT' && document.activeElement.tagName!=='SELECT'){$event.preventDefault(); $refs && $refs.search && $refs.search.focus();}">
```

Wire the Save button + form to react to `Ctrl/Cmd+S`: in `configure.gohtml`, change the form open tag (line 16) to also accept the `manualSave` trigger by adding a second HTMX-driven button. Simpler: bind the shortcut to click the existing Save button. Replace the `@keydown...ctrl.s` / `meta.s` handlers above with:

```gotemplate
  @keydown.window.prevent.ctrl.s="document.querySelector('header .actions .primary').click()"
  @keydown.window.prevent.meta.s="document.querySelector('header .actions .primary').click()"
```

(The Save button already posts to `/save` with the validation-blocking from Task 5, so the shortcut inherits the block-on-error behavior.)

Add `x-ref="search"` to the search input in `configure.gohtml` (line 66). Change:

```gotemplate
    <input type="text" x-model="search" placeholder="🔍 Search parameter..." class="search-input">
```

to:

```gotemplate
    <input type="text" x-ref="search" x-model="search" placeholder="Search parameter…  ( / )" class="search-input">
```

- [ ] **Step 2: Replace the Portuguese done page with an English auto-close page**

In `handlers.go`, replace `handleClosed` (lines 150-153) with:

```go
func (s *Session) handleClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(donePageHTML))
}

const donePageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>Done · model-loader</title><link rel="stylesheet" href="/static/app.css"></head>` +
	`<body><div class="done-page"><div class="done-icon">✓</div>` +
	`<h2>All set</h2><p>You can close this tab.</p></div>` +
	`<script>setTimeout(function(){try{window.close();}catch(e){}},400);</script></body></html>`
```

- [ ] **Step 3: Verify and fix the backend done page language**

Run: `cd /home/diogo/dev/model-loader && grep -n "handleBackendClosed" -A6 internal/service/configweb/backend_handlers.go`
If the body is Portuguese or a bare string, replace its written body with `donePageHTML` (now exported within the package) so both done pages share the English auto-close page. If it already serves English, leave it but switch it to `donePageHTML` for consistency.

- [ ] **Step 4: Build and run the full package test suite**

Run: `cd /home/diogo/dev/model-loader && make build && go test ./internal/service/configweb/... -v`
Expected: builds; all tests PASS.

- [ ] **Step 5: Manual end-to-end visual verification**

Launch the editor from the TUI (Profiles tab → create and edit a profile) and confirm:
- Dark theme reads cleanly; env-vars section is styled; Essentials group shows a star.
- Clearing Model shows an inline error + statusbar entry; Save is blocked and a toast appears.
- `/` focuses search; `Ctrl/Cmd+S` saves; `Esc` cancels (confirms when dirty).
- Customize: saving/removing a flag shows a toast; removed card disappears; saving rules shows a toast (no `alert()`).
- After save/cancel the English done page appears (and the tab auto-closes if the browser allows).

- [ ] **Step 6: Run the whole project test suite and commit**

Run: `cd /home/diogo/dev/model-loader && make tests`
Expected: PASS (note: `TestParseHelp_Golden` in `llamahelp` may fail locally due to an installed llama-server — that is a known, unrelated env failure, not a regression from this work).

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): keyboard shortcuts, English auto-close done page, final UX polish"
```

---

## Self-Review (against the 11 decisions)

1. All three surfaces touched — Configure (Tasks 3-5,7), Customize (Task 6), Backend editor (Tasks 2,6,7). ✓
2. Polish (Tasks 1-2), functional UX (Tasks 4-7), structural refinement (sidebar/env-vars/layout). ✓
3. No pipeline — only CSS + templates + Go; no Node/bundler added. ✓
4. Refined existing dark theme; no light mode introduced. ✓
5. Master-detail kept; sidebar refined (star marker, responsive collapse). ✓
6. Inline field errors + statusbar summary (Task 4). ✓
7. Save blocks on errors, warnings pass (Task 5, with test). ✓
8. Alpine toast store for Customize feedback (Task 6). ✓
9. `Ctrl/Cmd+S`, `Esc` (confirm-if-dirty), `/` (Task 7). ✓
10. English auto-close-or-fallback done page (Task 7). ✓
11. This plan written first. ✓

**Open verification flagged in-plan:** Task 5 Step 2 requires confirming which input the `validator` actually errors on (missing model vs out-of-range int) before finalizing the failing test — read `internal/service/validator/` to pick the guaranteed trigger.

**Test markers preserved:** `name="arg.ctx-size"`, `value="4096"`, `Essentials`, `Cross-field rules`, `/customize/flag` all remain present after edits.
