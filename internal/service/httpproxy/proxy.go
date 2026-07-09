package httpproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// newReverseProxy builds an httputil.ReverseProxy pointed at a backend
// listening on 127.0.0.1:<port>. When authToken is non-empty it injects
// "Authorization: Bearer <authToken>" on every UPSTREAM request — this is
// outbound auth (proxy → backend, e.g. Unsloth Studio), NOT inbound client
// auth; the proxy itself remains unauthenticated on the client side.
func newReverseProxy(port int, authToken string, maxBodyBuffer int64) *httputil.ReverseProxy {
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
	rp.ModifyResponse = func(resp *http.Response) error {
		if resp == nil || resp.Request == nil {
			return nil
		}
		if !isChatCompletionsPath(resp.Request.URL.Path) {
			return nil
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(ct, "text/event-stream") {
			// Streaming chat completions: mirror a reasoning-only stream into a
			// content delta so OpenAI clients that ignore reasoning fields still
			// see a message. Frames otherwise pass through untouched.
			if resp.StatusCode == http.StatusOK {
				resp.Body = mirrorReasoningStream(resp.Body)
			}
			return nil
		}
		if !shouldNormalizeChatResponse(resp) {
			return nil
		}
		// Fast path: upstream already declares an oversized body — pass it
		// through without buffering 8 MiB just to stream it back (N-P7).
		if resp.ContentLength > maxBodyBuffer {
			return nil
		}
		newBody, n, err := wrapChatCompletionResponseBody(resp.Body, maxBodyBuffer)
		if err != nil {
			return err
		}
		resp.Body = newBody
		if n < 0 {
			// Oversized body streamed through untruncated; length unknown.
			resp.ContentLength = -1
			resp.Header.Del("Content-Length")
		} else {
			resp.ContentLength = n
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", n))
		}
		resp.Header.Del("Content-Encoding")
		return nil
	}
	return rp
}
