package httpproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// profileIDPattern restricts request-provided model IDs to the slug shape
// produced by domain.Slugify. Defense in depth against path traversal —
// profilestore.FSStore builds filesystem paths via filepath.Join(dir, id+".json")
// and only rejects empty IDs, so we filter here before invoking Get.
var profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// extractProfileID resolves the desired profile ID for a request, in order:
//  1. JSON body `"model"` field (when Content-Type is application/json and
//     the body fits within maxBody).
//  2. `?model=` query parameter.
//  3. Empty string — caller falls back to the currently-loaded profile.
//
// When the body is consumed for extraction, it is fully buffered and the
// request body is replaced with a new ReadCloser so the reverse proxy can
// forward it verbatim downstream.
func extractProfileID(r *http.Request, maxBody int64) string {
	if id := extractFromBody(r, maxBody); id != "" {
		return id
	}
	if id := r.URL.Query().Get("model"); id != "" {
		return id
	}
	return ""
}

// extractFromBody buffers the JSON body (when applicable) and pulls the
// top-level `model` field. The original body is replaced with a reader over
// the buffered bytes so downstream proxying is transparent.
func extractFromBody(r *http.Request, maxBody int64) string {
	if r.Body == nil || r.Body == http.NoBody {
		return ""
	}
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json") {
		return ""
	}
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBuffer
	}
	// Reject bodies larger than the cap: buffering them would balloon RAM.
	// Caller falls through to query/current-profile resolution; the request
	// still proxies because we leave r.Body untouched on this path.
	if r.ContentLength > maxBody {
		return ""
	}

	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	limited := io.LimitReader(r.Body, maxBody+1)
	n, err := io.Copy(buf, limited)
	_ = r.Body.Close()
	if err != nil || n > maxBody {
		// Restore an empty body — original was already partially drained.
		r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		r.ContentLength = int64(buf.Len())
		return ""
	}

	r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	r.ContentLength = int64(buf.Len())

	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(buf.Bytes(), &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.Model)
}

// validProfileID reports whether id is safe to look up in profilestore.
func validProfileID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	return profileIDPattern.MatchString(id)
}
