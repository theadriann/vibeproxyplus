package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
)

const (
	BetaHeader      = "anthropic-beta"
	BetaInterleaved = "interleaved-thinking-2025-05-14"
)

type ThinkingProxy struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
}

func NewThinkingProxy(targetPort int) *ThinkingProxy {
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(targetPort))

	tp := &ThinkingProxy{target: target}
	tp.proxy = &httputil.ReverseProxy{
		Director: tp.director,
	}
	return tp
}

func (tp *ThinkingProxy) director(req *http.Request) {
	req.URL.Scheme = tp.target.Scheme
	req.URL.Host = tp.target.Host
	req.Host = tp.target.Host
}

func (tp *ThinkingProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Health check endpoint
	if r.URL.Path == "/health" {
		tp.handleHealth(w, r)
		return
	}

	// Only transform POST requests with body
	if r.Method != http.MethodPost || r.Body == nil {
		tp.proxy.ServeHTTP(w, r)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Transform if needed
	newBody, needsBetaHeader, err := TransformRequestBody(r.URL.Path, body)
	if err != nil {
		log.Printf("Warning: failed to transform body: %v", err)
		newBody = body
	}

	// Add beta header when Claude thinking is enabled
	if needsBetaHeader {
		existing := r.Header.Get(BetaHeader)
		if existing == "" {
			r.Header.Set(BetaHeader, BetaInterleaved)
		} else if !contains(existing, BetaInterleaved) {
			r.Header.Set(BetaHeader, existing+","+BetaInterleaved)
		}
		log.Printf("Transformed request: thinking enabled")
	}

	// Update request
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))

	tp.proxy.ServeHTTP(w, r)
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func (tp *ThinkingProxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check if backend is reachable
	resp, err := http.Get(tp.target.String() + "/health")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy","error":"backend unreachable"}`))
		return
	}
	resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}
