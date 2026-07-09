package configweb

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// draftFromForm parses an editor form POST into a Draft.
// Arg fields are named "arg.<flag>"; meta fields use their plain names.
func draftFromForm(r *http.Request) Draft {
	_ = r.ParseForm()
	d := Draft{
		ID:          r.FormValue("id"),
		OrigID:      r.FormValue("origId"),
		IsNew:       r.FormValue("isNew") == "true",
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Model:       r.FormValue("model"),
		BackendID:   r.FormValue("backendId"),
		Args:        map[string]string{},
	}
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		for t := range strings.SplitSeq(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				d.Tags = append(d.Tags, t)
			}
		}
	}
	for k, vs := range r.Form {
		// Empty arg values mean "not configured" — drop them so untouched flags
		// (their inputs default to empty) never get persisted into the profile.
		if strings.HasPrefix(k, "arg.") && len(vs) > 0 && strings.TrimSpace(vs[0]) != "" {
			d.Args[strings.TrimPrefix(k, "arg.")] = vs[0]
		}
	}
	// Parse env vars from envKey_N / envValue_N pairs.
	for i := 0; ; i++ {
		key := strings.TrimSpace(r.FormValue(fmt.Sprintf("envKey_%d", i)))
		val := strings.TrimSpace(r.FormValue(fmt.Sprintf("envValue_%d", i)))
		if key == "" {
			break
		}
		d.Env = append(d.Env, domain.EnvVar{Key: key, Value: val})
	}
	return d
}

func (s *Session) loadSchema(backendID string) (domain.BackendValidationSchema, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			return s.deps.Schemas.Load(backendcatalog.SchemaStoreRef(b.SchemaRef))
		}
	}
	return domain.BackendValidationSchema{}, backendcatalog.ErrBackendNotFound
}

func (s *Session) handleValidate(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		renderIssueError(w, err.Error())
		return
	}
	fs := schema.ToFlagSchema()
	p := d.ToProfile(fs)
	rep := downgradeModelExistence(validator.New(nil).Validate(p, fs, schema.BackendKind))
	renderIssues(w, rep)
}

// renderIssues writes an HTMX partial listing errors and warnings. It emits
// inner content only — the persistent #issues element in the page carries the
// aria-live attributes, and replacing it (e.g. via an oob outerHTML swap)
// would silence screen-reader announcements.
func renderIssues(w http.ResponseWriter, rep validator.Report) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	if len(rep.Errors) == 0 && len(rep.Warnings) == 0 {
		b.WriteString(`<span class="ok">✓ valid</span>`)
	}
	for _, e := range rep.Errors {
		b.WriteString(`<div class="issue error" data-field="` + htmlEscape(e.Field) + `">` + htmlEscape(e.Field) + `: ` + htmlEscape(e.Message) + `</div>`)
	}
	for _, wn := range rep.Warnings {
		b.WriteString(`<div class="issue warn" data-field="` + htmlEscape(wn.Field) + `">` + htmlEscape(wn.Field) + `: ` + htmlEscape(wn.Message) + `</div>`)
	}
	_, _ = w.Write([]byte(b.String()))
}

// renderIssueError writes a single blocking issue into the #issues status
// region (200, not http.Error) so failures surface in the UI instead of a
// silently-ignored 4xx response.
func renderIssueError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<div class="issue error">` + htmlEscape(msg) + `</div>`))
}

func htmlEscape(s string) string {
	return html.EscapeString(s)
}

// modelExistenceErr reports whether a validator issue is one of the model-file
// existence errors (file missing / permission / stat failure). These are the
// only errors applyExistenceRules emits and all carry Field "model".
func modelExistenceErr(it validator.FieldIssue) bool {
	if it.Field != "model" {
		return false
	}
	return it.Message == "model file does not exist" ||
		it.Message == "permission denied for model path" ||
		strings.HasPrefix(it.Message, "model path stat failed: ")
}

// downgradeModelExistence moves model-existence errors out of rep.Errors into
// rep.Warnings so the web editor never blocks Save on a not-yet-present model
// (configure now, download later). Real config errors keep blocking.
func downgradeModelExistence(rep validator.Report) validator.Report {
	var kept []validator.FieldIssue
	for _, e := range rep.Errors {
		if modelExistenceErr(e) {
			e.Severity = validator.SeverityWarning
			rep.Warnings = append(rep.Warnings, e)
			continue
		}
		kept = append(kept, e)
	}
	rep.Errors = kept
	return rep
}

func (s *Session) handleSave(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		renderIssueError(w, err.Error())
		return
	}
	fs := schema.ToFlagSchema()

	// Block persistence when the profile has validation errors. Warnings pass.
	// Model-existence errors are downgraded to warnings so a profile can be
	// configured before the model file is downloaded (or for HF repo IDs).
	rep := downgradeModelExistence(validator.New(nil).Validate(d.ToProfile(fs), fs, schema.BackendKind))
	if len(rep.Errors) > 0 {
		renderIssues(w, rep)
		return
	}

	if err := s.doPersist(s.decidePersistOp(d, fs, surfacedFlags(schema))); err != nil {
		renderIssueError(w, err.Error())
		return
	}
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, ProfileID: d.ID})
}

// persistOp is the resolved persistence action for a saved draft: a brand-new
// create, an in-place save, or a rename (the user changed the profile ID).
type persistOp struct {
	kind    string // "create" | "save" | "rename"
	profile domain.Profile
	oldID   string // rename only — the file to drop after writing profile
}

// decidePersistOp resolves which persistence action a saved draft needs. New
// drafts (and ones whose original file vanished) are creates; otherwise the
// draft is applied onto the existing profile and becomes a rename when its ID
// changed, else a plain save. The schema is needed to project the draft into a
// domain.Profile, and the lookup of the existing profile needs Session deps —
// hence this is a method rather than a free function.
func (s *Session) decidePersistOp(d Draft, fs domain.FlagSchema, surfaced map[string]bool) persistOp {
	if d.IsNew {
		return persistOp{kind: "create", profile: d.ToProfile(fs)}
	}
	lookup := d.OrigID
	if lookup == "" {
		lookup = d.ID
	}
	existing, err := s.deps.Profiles.Get(lookup)
	if err != nil {
		// The original file is gone — fall back to a create so the edit is not lost.
		return persistOp{kind: "create", profile: d.ToProfile(fs)}
	}
	final := d.ApplyTo(existing, fs, surfaced)
	if lookup != final.ID {
		return persistOp{kind: "rename", profile: final, oldID: lookup}
	}
	return persistOp{kind: "save", profile: final}
}

// doPersist executes a persistOp against the profile store.
func (s *Session) doPersist(op persistOp) error {
	switch op.kind {
	case "create":
		return s.deps.Profiles.Create(op.profile)
	case "rename":
		if s.deps.InstanceInUse != nil && s.deps.InstanceInUse(op.oldID) {
			return fmt.Errorf("%w %q — stop the running instance/benchmark first, then rename", ErrProfileInUse, op.oldID)
		}
		// Move the file (write new, drop old) instead of leaving an orphaned
		// copy under the original id.
		return s.deps.Profiles.Rename(op.oldID, op.profile)
	default: // "save"
		return s.deps.Profiles.Save(op.profile)
	}
}

func (s *Session) handleCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: false})
}

const donePageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>Done · model-loader</title><link rel="stylesheet" href="/static/app.css"></head>` +
	`<body><div class="done-page"><div class="done-icon">✓</div>` +
	`<h2>All set</h2><p>You can close this tab.</p></div>` +
	`<script>setTimeout(function(){try{window.close();}catch(e){}},400);</script></body></html>`

func (s *Session) handleClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(donePageHTML))
	s.requestShutdown()
}

// handleSwitchBackend re-renders the form for a new backend, carrying over
// every configured arg the new schema supports. Args the new backend does NOT
// support are never dropped silently: the first POST re-renders the old form
// (select reverted, args intact) with a confirmation banner, and only a second
// POST carrying confirmSwitch performs the switch and removes them.
func (s *Session) handleSwitchBackend(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	// confirmSwitch does double duty: its presence marks the confirmed re-POST
	// and its value carries the target backend (the re-posted form's backendId
	// is the reverted OLD backend, so the select can't be the source then).
	confirmed := r.FormValue("confirmSwitch") != ""
	newID := r.FormValue("confirmSwitch")
	if newID == "" {
		newID = d.BackendID
	}
	prevID := r.FormValue("prevBackendId")
	if newID == "" {
		http.Error(w, "backendId required", http.StatusBadRequest)
		return
	}

	// Errors here intentionally use http.Error, not renderIssueError: the swap
	// target is #profile-form (outerHTML), so a 200 issue partial would replace
	// the whole form. A 4xx leaves the form alone and surfaces through the
	// htmx:responseError toast.
	newSchema, err := s.loadSchema(newID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	backends, _ := s.loadBackends()
	kept, dropped := partitionArgs(d.Args, newSchema.ToFlagSchema())

	if len(dropped) > 0 && !confirmed {
		// Render the OLD form back (args fully intact, select reverted) plus the
		// confirmation banner; the switch only happens on the confirmed re-POST.
		if prevID == "" {
			// Never empty via the rendered form (hidden field); guard so a
			// hand-crafted POST can't silently render the default backend.
			http.Error(w, "prevBackendId required", http.StatusBadRequest)
			return
		}
		oldSchema, err := s.loadSchema(prevID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.BackendID = prevID
		vm := BuildViewModel(d, oldSchema, backends)
		vm.SwitchConfirm = &SwitchConfirmVM{
			NewBackendID:   newID,
			NewBackendName: backendName(backends, newID),
			Dropped:        dropped,
		}
		s.renderSwitchResponse(w, vm)
		return
	}

	d.BackendID = newID
	d.Args = kept
	vm := BuildViewModel(d, newSchema, backends)
	s.renderSwitchResponse(w, vm)
}

// partitionArgs splits draft args into those the schema knows (kept) and those
// it does not (dropped, sorted by flag for stable rendering).
func partitionArgs(args map[string]string, fs domain.FlagSchema) (map[string]string, []DroppedArg) {
	kept := map[string]string{}
	var dropped []DroppedArg
	for flag, val := range args {
		if _, ok := fs.Lookup(domain.CanonicalFlag(flag)); ok {
			kept[flag] = val
		} else {
			dropped = append(dropped, DroppedArg{Flag: flag, Value: val})
		}
	}
	sort.Slice(dropped, func(i, j int) bool { return dropped[i].Flag < dropped[j].Flag })
	return kept, dropped
}

func backendName(backends []domain.Backend, id string) string {
	for _, b := range backends {
		if b.ID == id && b.Name != "" {
			return b.Name
		}
	}
	return id
}

// renderSwitchResponse writes the swapped #profile-form (outerHTML target of
// the backend select) followed by an out-of-band innerHTML refresh of the
// #sidebar-groups tablist so the group tabs always match the rendered schema.
// The oob fragment leads with a one-shot hidden x-init div that resets Alpine's
// activeGroup to the first group of the rendered schema (Alpine initializes
// htmx-inserted nodes via its MutationObserver).
func (s *Session) renderSwitchResponse(w http.ResponseWriter, vm ViewModel) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "configure", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteString(`<div id="sidebar-groups" hx-swap-oob="innerHTML">`)
	if len(vm.Groups) > 0 && vm.SwitchConfirm == nil {
		// Reset the active group only on a real switch — the confirmation
		// response re-renders the OLD schema, so yanking the user's current
		// group selection there would be gratuitous.
		expr := "activeGroup='" + template.JSEscapeString(vm.Groups[0].Name) + "'"
		buf.WriteString(`<div x-init="` + htmlEscape(expr) + `" hidden></div>`)
	}
	if err := tmpl.ExecuteTemplate(&buf, "sidebar-tabs", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteString(`</div>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
