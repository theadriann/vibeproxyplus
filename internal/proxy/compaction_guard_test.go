package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const droidSummaryInstructions = "You excel at creating and maintaining summaries that capture the most salient details from technical conversations. Return your final summary with the following wrapped in <summary> tags."

func TestCompactionNoTextGuardRejectsEmptyStreamingSummaryResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"output\":[]}}\n\n"))
	}))
	defer upstream.Close()

	tp := newTestThinkingProxy(t, upstream.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"instructions":"`+droidSummaryInstructions+`","input":[{"role":"user","content":"large session"}]}`))
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no text") {
		t.Fatalf("expected no-text diagnostic, got %s", rec.Body.String())
	}
}

func TestCompactionNoTextGuardAllowsStreamingSummaryText(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"summary\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"summary\"}]}]}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	tp := newTestThinkingProxy(t, upstream.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"instructions":"`+droidSummaryInstructions+`","input":[{"role":"user","content":"large session"}]}`))
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != body {
		t.Fatalf("response body changed:\ngot  %q\nwant %q", rec.Body.String(), body)
	}
}

func TestCompactionNoTextGuardIgnoresNonSummarizerResponses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"resp-1","output":[]}`)
	}))
	defer upstream.Close()

	tp := newTestThinkingProxy(t, upstream.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","input":"hello"}`))
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"id":"resp-1","output":[]}` {
		t.Fatalf("response body changed: %s", rec.Body.String())
	}
}

func newTestThinkingProxy(t *testing.T, upstreamURL string) *ThinkingProxy {
	t.Helper()

	target, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	return newThinkingProxyWithTarget(target)
}
