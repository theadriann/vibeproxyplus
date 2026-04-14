package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRealtimeEndpointNonUpgradeReturns426(t *testing.T) {
	tp := NewThinkingProxy(8318)
	req := httptest.NewRequest(http.MethodGet, realtimeEndpointPath, nil)
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUpgradeRequired)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json payload: %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("error payload missing: %s", rec.Body.String())
	}
}

func TestRealtimeEndpointExactPathOnly(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	_, portText, err := netSplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("split backend host/port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("atoi backend port: %v", err)
	}

	tp := NewThinkingProxy(port)
	req := httptest.NewRequest(http.MethodGet, realtimeEndpointPath+"/extra", nil)
	rec := httptest.NewRecorder()

	tp.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRealtimeWebsocketRelayAndDefaultModel(t *testing.T) {
	type upstreamSeen struct {
		Auth  string
		Model string
	}
	seenCh := make(chan upstreamSeen, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/realtime" {
			http.NotFound(w, r)
			return
		}

		seenCh <- upstreamSeen{
			Auth:  r.Header.Get("Authorization"),
			Model: r.URL.Query().Get("model"),
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_ = conn.WriteMessage(websocket.TextMessage, []byte("upstream-ready"))
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(msgType, append([]byte("echo:"), payload...))
	}))
	defer upstream.Close()

	tp := NewThinkingProxy(8318)
	tp.resolveRealtimeAPIKey = func() (string, error) { return "cfg-realtime-key", nil }
	tp.realtimeUpstreamURL = "ws" + strings.TrimPrefix(upstream.URL, "http") + "/v1/realtime"

	proxyServer := httptest.NewServer(tp)
	defer proxyServer.Close()

	proxyWSURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + realtimeEndpointPath
	clientConn, _, err := websocket.DefaultDialer.Dial(proxyWSURL, nil)
	if err != nil {
		t.Fatalf("dial proxy websocket: %v", err)
	}
	defer clientConn.Close()
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read upstream ready message: %v", err)
	}
	if string(msg) != "upstream-ready" {
		t.Fatalf("first message = %q, want %q", string(msg), "upstream-ready")
	}

	if err := clientConn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write ping message: %v", err)
	}
	_, msg, err = clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read echo message: %v", err)
	}
	if string(msg) != "echo:ping" {
		t.Fatalf("echo message = %q, want %q", string(msg), "echo:ping")
	}

	select {
	case seen := <-seenCh:
		if seen.Auth != "Bearer cfg-realtime-key" {
			t.Fatalf("authorization header = %q, want %q", seen.Auth, "Bearer cfg-realtime-key")
		}
		if seen.Model != defaultRealtimeModel {
			t.Fatalf("model query = %q, want %q", seen.Model, defaultRealtimeModel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request capture")
	}
}

func netSplitHostPort(hostport string) (string, string, error) {
	parts := strings.Split(hostport, ":")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid hostport: %s", hostport)
	}
	host := strings.Join(parts[:len(parts)-1], ":")
	port := parts[len(parts)-1]
	return host, port, nil
}
