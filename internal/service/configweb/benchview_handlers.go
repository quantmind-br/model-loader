package configweb

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/configweb/assets"
)

// benchTmpl holds the read-only benchmark viewer templates. Kept separate from
// the editor's `tmpl` so the two template sets never collide on block names.
var benchTmpl = template.Must(template.New("bench").ParseFS(assets.FS,
	"templates/benchview_base.gohtml",
	"templates/benchview_list.gohtml",
	"templates/benchview_run.gohtml",
	"templates/benchview_compare.gohtml",
	"templates/benchview_live.gohtml",
))

// routes registers the viewer's read-only routes. Mirrors Session.routes: a
// FileServer for /static, /healthz, and a /closed done page + shutdown.
func (v *BenchViewer) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", v.handleList)
	mux.HandleFunc("/run/{id}", v.handleRun)
	mux.HandleFunc("/compare", v.handleCompare)
	mux.HandleFunc("/live", v.handleLive)
	mux.HandleFunc("/live/fragment", v.handleLiveFragment)
	mux.Handle("/static/", http.FileServer(http.FS(assets.FS)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/closed", v.handleClosed)
}

func (v *BenchViewer) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := benchTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// handleList renders the run list at "/". Any other unmatched path is a 404
// (the "/" pattern is a catch-all subtree).
func (v *BenchViewer) handleList(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	runs, err := v.deps.Runs.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v.render(w, "benchlist", buildListVM(runs))
}

// handleRun renders one run's detail. Unknown id → 404.
func (v *BenchViewer) handleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := v.deps.Runs.Load(id)
	if err != nil {
		if errors.Is(err, benchmarkstore.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allRuns, _ := v.deps.Runs.List() // ceilings degrade to defaults on error
	tr, terr := v.deps.Runs.LoadTranscript(id)
	if terr != nil {
		tr = nil // transcript not saved — detail still renders
	}
	v.render(w, "benchrun", buildDetailVM(run, allRuns, tr))
}

func (v *BenchViewer) handleCompare(w http.ResponseWriter, r *http.Request) {
	runs, err := v.deps.Runs.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v.render(w, "benchcompare", buildCompareVM(runs))
}

func (v *BenchViewer) handleLive(w http.ResponseWriter, r *http.Request) {
	v.render(w, "benchlivepage", benchBaseVM{Title: "Live run", Active: "live"})
}

// handleLiveFragment is polled by /live every second. A nil Live accessor OR a
// nil feed renders "no run in progress"; otherwise it renders the snapshot.
func (v *BenchViewer) handleLiveFragment(w http.ResponseWriter, r *http.Request) {
	vm := benchLiveVM{benchBaseVM: benchBaseVM{Active: "live"}}
	if v.deps.Live != nil {
		if feed := v.deps.Live(); feed != nil {
			vm = buildLiveVM(feed.Snapshot(), time.Now())
		}
	}
	v.render(w, "benchlivefragment", vm)
}

// handleClosed serves the shared done page and requests shutdown so the session
// does not linger the full grace period.
func (v *BenchViewer) handleClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(donePageHTML))
	v.requestShutdown()
}
