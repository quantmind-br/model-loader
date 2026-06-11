package configweb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"sort"
	"strconv"
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
			final := d.ApplyTo(existing, fs)
			if lookup != final.ID {
				// The user changed the ID: move the file (write new, drop old)
				// instead of leaving an orphaned copy under the original id.
				perr = s.deps.Profiles.Rename(lookup, final)
			} else {
				perr = s.deps.Profiles.Save(final)
			}
		}
	}
	if perr != nil {
		renderIssueError(w, perr.Error())
		return
	}
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, ProfileID: d.ID})
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

// loadSchemaRef returns the schema plus its store ref for a backend.
func (s *Session) loadSchemaRef(backendID string) (domain.BackendValidationSchema, string, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, "", err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
			sch, err := s.deps.Schemas.Load(ref)
			return sch, ref, err
		}
	}
	return domain.BackendValidationSchema{}, "", backendcatalog.ErrBackendNotFound
}

func atoiPtr(s string) *int {
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return &n
	}
	return nil
}

// persistEditable marks the schema as a manual edit. RefreshSchema preserves
// schemas with Source.Editable=true.
func persistEditable(schema *domain.BackendValidationSchema) {
	schema.Source.Editable = true
}

// handleCustomizeFlag updates one flag's editable constraints and marks the
// schema as user-edited so RefreshSchema preserves it.
func (s *Session) handleCustomizeFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	backendID := r.FormValue("backendId")
	flag := r.FormValue("flag")
	schema, ref, err := s.loadSchemaRef(backendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	spec, ok := schema.Flags[flag]
	if !ok {
		http.Error(w, "unknown flag", http.StatusBadRequest)
		return
	}
	spec.Min = atoiPtr(r.FormValue("min"))
	spec.Max = atoiPtr(r.FormValue("max"))
	spec.Required = r.FormValue("required") == "on"
	if dv := r.FormValue("default"); dv != "" {
		spec.Default = dv
	}
	if ev := r.FormValue("enumValues"); ev != "" {
		spec.EnumValues = splitCSV(ev)
	}
	schema.Flags[flag] = spec
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func splitCSV(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// handleCustomizeAddFlag adds a new user-defined flag to the schema.
func (s *Session) handleCustomizeAddFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	if long == "" {
		http.Error(w, "flag name required", http.StatusBadRequest)
		return
	}
	if schema.Flags == nil {
		schema.Flags = map[string]domain.FlagSpec{}
	}
	schema.Flags[long] = domain.FlagSpec{
		Long:     long,
		Type:     parseFlagType(r.FormValue("type")),
		HelpText: r.FormValue("help"),
		Group:    "custom",
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRemoveFlag deletes a flag and any presentation reference to it.
func (s *Session) handleCustomizeRemoveFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	delete(schema.Flags, long)
	if schema.Presentation != nil {
		for gi := range schema.Presentation.Groups {
			schema.Presentation.Groups[gi].Flags = removeStr(schema.Presentation.Groups[gi].Flags, long)
		}
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizePresentation overwrites the presentation from a JSON body.
func (s *Session) handleCustomizePresentation(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var pres domain.Presentation
	if err := jsonDecode(r, &pres); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema.Presentation = &pres
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRules overwrites the cross-field rules from a JSON body.
func (s *Session) handleCustomizeRules(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var rules []domain.CrossFieldRule
	if err := jsonDecode(r, &rules); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg := validateRules(rules, schema); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	schema.Rules = rules
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func parseFlagType(s string) domain.FlagType {
	switch s {
	case "int":
		return domain.FlagTypeInt
	case "float":
		return domain.FlagTypeFloat
	case "bool":
		return domain.FlagTypeBool
	case "enum":
		return domain.FlagTypeEnum
	default:
		return domain.FlagTypeString
	}
}

func removeStr(xs []string, s string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func jsonDecode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func validateRules(rules []domain.CrossFieldRule, schema domain.BackendValidationSchema) string {
	for _, rule := range rules {
		if _, ok := schema.Flags[rule.When.Flag]; !ok {
			return "rule references unknown flag: " + rule.When.Flag
		}
		if rule.Then.Flag != "" {
			if _, ok := schema.Flags[rule.Then.Flag]; !ok {
				return "rule effect references unknown flag: " + rule.Then.Flag
			}
		}
		if rule.Severity != "warning" && rule.Severity != "error" {
			return "rule severity must be warning or error"
		}
	}
	return ""
}

// handleSwitchBackend re-renders the form for a new backend, carrying over
// every configured arg the new schema supports. Args the new backend does NOT
// support are never dropped silently: the first POST re-renders the old form
// (select reverted, args intact) with a confirmation banner, and only a second
// POST carrying confirmSwitch performs the switch and removes them.
func (s *Session) handleSwitchBackend(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
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
	if len(vm.Groups) > 0 {
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
