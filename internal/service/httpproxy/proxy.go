package httpproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// newReverseProxy builds an httputil.ReverseProxy pointed at a backend
// listening on 127.0.0.1:<port>. When authToken is non-empty it injects
// "Authorization: Bearer <authToken>" on every UPSTREAM request — this is
// outbound auth (proxy → backend, e.g. Unsloth Studio), NOT inbound client
// auth; the proxy itself remains unauthenticated on the client side.
func newReverseProxy(port int, authToken string) *httputil.ReverseProxy {
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", port),
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	if authToken != "" {
		orig := rp.Director
		rp.Director = func(req *http.Request) {
			orig(req)
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
	}
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
