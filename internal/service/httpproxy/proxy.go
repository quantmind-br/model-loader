package httpproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// newReverseProxy builds an httputil.ReverseProxy pointed at a backend
// listening on 127.0.0.1:<port>. Tuned for SSE streaming (no buffering, no
// compression, no header timeout) so chat-completion streams flush
// chunk-by-chunk back to the client.
func newReverseProxy(port int) *httputil.ReverseProxy {
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", port),
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Transport = &http.Transport{
		DisableCompression:    true,
		ResponseHeaderTimeout: 0,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
	}
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeOpenAIError(w, http.StatusBadGateway, "backend_error", "upstream_unavailable", err.Error())
	}
	return rp
}
