package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrafficDebugLoggingWritesRedactedRequestAndResponse(t *testing.T) {
	t.Setenv("VIBEPROXYPLUS_TRAFFIC_DEBUG", "1")
	debugDir := t.TempDir()
	t.Setenv("VIBEPROXYPLUS_TRAFFIC_DEBUG_DIR", debugDir)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		if !strings.Contains(string(body), `"model":"gpt-5.5(high)"`) {
			t.Fatalf("upstream received unexpected body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","output_text":"summary"}`))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	tp := newThinkingProxyWithTarget(target)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5(high)","api_key":"secret","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"id":"resp-1","output_text":"summary"}` {
		t.Fatalf("response body was not preserved: %s", rec.Body.String())
	}

	requestLog := readTrafficDebugLog(t, debugDir, "-request.json")
	if strings.Contains(string(requestLog), "secret") {
		t.Fatalf("request log leaked sensitive data: %s", requestLog)
	}
	if !strings.Contains(string(requestLog), `"Authorization":`) || !strings.Contains(string(requestLog), `"api_key":`) {
		t.Fatalf("request log missing redacted fields: %s", requestLog)
	}

	responseLog := readTrafficDebugLog(t, debugDir, "-response.json")
	if !strings.Contains(string(responseLog), `"status_code": 200`) || !strings.Contains(string(responseLog), `"output_text": "summary"`) {
		t.Fatalf("response log missing response details: %s", responseLog)
	}
}

func TestTrafficDebugLoggingDisabledByDefault(t *testing.T) {
	debugDir := t.TempDir()
	t.Setenv("VIBEPROXYPLUS_TRAFFIC_DEBUG_DIR", debugDir)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	tp := newThinkingProxyWithTarget(target)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5(high)","input":"hello"}`))
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	entries, err := os.ReadDir(debugDir)
	if err != nil {
		t.Fatalf("read debug dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("debug logs were written while disabled: %v", entries)
	}
}

func readTrafficDebugLog(t *testing.T, dir, suffix string) []byte {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	if err != nil {
		t.Fatalf("glob debug logs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches for %s = %v, want one", suffix, matches)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("debug log is not valid JSON: %s", data)
	}
	return data
}
