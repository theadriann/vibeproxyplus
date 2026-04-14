package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	realtimeEndpointPath       = "/realtime/v1"
	defaultRealtimeUpstreamURL = "wss://api.openai.com/v1/realtime"
	defaultRealtimeModel       = "gpt-realtime"
)

func (tp *ThinkingProxy) handleRealtimeWebsocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeRealtimeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		writeRealtimeHTTPError(w, http.StatusUpgradeRequired, "websocket upgrade is required for /realtime/v1")
		return
	}

	if tp.resolveRealtimeAPIKey == nil {
		writeRealtimeHTTPError(w, http.StatusBadGateway, "realtime API key resolver is not configured")
		return
	}

	apiKey, err := tp.resolveRealtimeAPIKey()
	if err != nil {
		writeRealtimeHTTPError(w, http.StatusBadGateway, fmt.Sprintf("resolve upstream API key failed: %v", err))
		return
	}

	upstreamURL, err := tp.buildRealtimeUpstreamURL(r.URL)
	if err != nil {
		writeRealtimeHTTPError(w, http.StatusBadRequest, fmt.Sprintf("invalid realtime request: %v", err))
		return
	}

	dialer := websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  30 * time.Second,
		EnableCompression: true,
		Subprotocols:      websocket.Subprotocols(r),
	}

	upstreamConn, upstreamResp, err := dialer.DialContext(r.Context(), upstreamURL, buildRealtimeUpstreamHeaders(r, apiKey))
	if err != nil {
		defer closeHTTPBody(upstreamResp)
		status := http.StatusBadGateway
		message := fmt.Sprintf("dial realtime upstream failed: %v", err)
		if upstreamResp != nil {
			if upstreamResp.StatusCode > 0 {
				status = upstreamResp.StatusCode
			}
			if body, readErr := io.ReadAll(upstreamResp.Body); readErr == nil {
				trimmed := strings.TrimSpace(string(body))
				if trimmed != "" {
					message = trimmed
				}
			}
		}
		writeRealtimeHTTPError(w, status, message)
		return
	}
	closeHTTPBody(upstreamResp)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	upgradeHeaders := http.Header{}
	if protocol := strings.TrimSpace(upstreamConn.Subprotocol()); protocol != "" {
		upgradeHeaders.Set("Sec-WebSocket-Protocol", protocol)
	}

	downstreamConn, err := upgrader.Upgrade(w, r, upgradeHeaders)
	if err != nil {
		_ = upstreamConn.Close()
		return
	}

	tunnelWebsocketConnections(downstreamConn, upstreamConn)
}

func (tp *ThinkingProxy) buildRealtimeUpstreamURL(reqURL *url.URL) (string, error) {
	base := strings.TrimSpace(tp.realtimeUpstreamURL)
	if base == "" {
		base = defaultRealtimeUpstreamURL
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid realtime upstream URL %q: %w", base, err)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "wss"
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid realtime upstream URL: missing host")
	}

	query := parsed.Query()
	if reqURL != nil {
		for key, values := range reqURL.Query() {
			query[key] = values
		}
	}
	if strings.TrimSpace(query.Get("model")) == "" {
		query.Set("model", defaultRealtimeModel)
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func buildRealtimeUpstreamHeaders(r *http.Request, apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))

	if r == nil {
		return headers
	}

	forwardKeys := []string{
		"OpenAI-Beta",
		"OpenAI-Organization",
		"OpenAI-Project",
		"User-Agent",
	}
	for i := range forwardKeys {
		key := forwardKeys[i]
		for _, value := range r.Header.Values(key) {
			headers.Add(key, value)
		}
	}

	return headers
}

func tunnelWebsocketConnections(downstream, upstream *websocket.Conn) {
	errCh := make(chan error, 2)

	go func() {
		errCh <- relayWebsocketMessages(upstream, downstream)
	}()
	go func() {
		errCh <- relayWebsocketMessages(downstream, upstream)
	}()

	<-errCh
	_ = downstream.Close()
	_ = upstream.Close()
}

func relayWebsocketMessages(dst, src *websocket.Conn) error {
	for {
		msgType, payload, err := src.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok {
				deadline := time.Now().Add(2 * time.Second)
				_ = dst.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeErr.Code, closeErr.Text), deadline)
				return nil
			}
			return err
		}
		if err := dst.WriteMessage(msgType, payload); err != nil {
			return err
		}
	}
}

func writeRealtimeHTTPError(w http.ResponseWriter, statusCode int, message string) {
	if statusCode <= 0 {
		statusCode = http.StatusInternalServerError
	}
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(statusCode)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func closeHTTPBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_ = resp.Body.Close()
}
