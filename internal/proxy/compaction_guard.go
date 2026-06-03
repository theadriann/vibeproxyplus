package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type compactionNoTextGuardContextKey struct{}

func attachCompactionNoTextGuard(r *http.Request, body []byte) *http.Request {
	if r == nil || !isCodexResponsesPath(r.URL.Path) || len(body) == 0 {
		return r
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return r
	}
	if !looksLikeDroidSummarizerRequest(data) {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), compactionNoTextGuardContextKey{}, true))
}

func compactionNoTextGuardEnabled(ctx context.Context) bool {
	enabled, _ := ctx.Value(compactionNoTextGuardContextKey{}).(bool)
	return enabled
}

func guardCompactionNoTextResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil || resp.Request == nil || !compactionNoTextGuardEnabled(resp.Request.Context()) {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	if responseBodyHasUsableText(body) {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		return nil
	}

	log.Printf("compaction no-text guard: converting empty successful response to 502")
	replacement := []byte(`{"error":{"message":"VibeProxyPlus detected a successful compaction response with no text output","type":"bad_gateway","code":"compaction_no_text"}}` + "\n")
	resp.StatusCode = http.StatusBadGateway
	resp.Status = http.StatusText(http.StatusBadGateway)
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Set("Content-Length", strconv.Itoa(len(replacement)))
	resp.Body = io.NopCloser(bytes.NewReader(replacement))
	resp.ContentLength = int64(len(replacement))
	return nil
}

func looksLikeDroidSummarizerRequest(data map[string]interface{}) bool {
	instructions, _ := data["instructions"].(string)
	instructions = strings.ToLower(instructions)
	return strings.Contains(instructions, "creating and maintaining summaries") ||
		strings.Contains(instructions, "return your final summary") ||
		strings.Contains(instructions, "<summary>")
}

func responseBodyHasUsableText(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}

	var value interface{}
	if json.Unmarshal(trimmed, &value) == nil {
		return jsonValueHasUsableText(value)
	}

	return sseBodyHasUsableText(string(body))
}

func sseBodyHasUsableText(body string) bool {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	for _, frame := range strings.Split(normalized, "\n\n") {
		var payloadLines []string
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payloadLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payloadLine == "" || payloadLine == "[DONE]" {
				continue
			}
			payloadLines = append(payloadLines, payloadLine)
		}
		if len(payloadLines) == 0 {
			continue
		}
		payload := strings.Join(payloadLines, "\n")
		var value interface{}
		if json.Unmarshal([]byte(payload), &value) == nil && jsonValueHasUsableText(value) {
			return true
		}
	}
	return false
}

func jsonValueHasUsableText(value interface{}) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			if textValueKey(key) {
				if text, ok := nested.(string); ok && strings.TrimSpace(text) != "" {
					return true
				}
			}
			if jsonValueHasUsableText(nested) {
				return true
			}
		}
	case []interface{}:
		for _, nested := range typed {
			if jsonValueHasUsableText(nested) {
				return true
			}
		}
	}
	return false
}

func textValueKey(key string) bool {
	switch key {
	case "output_text", "text", "delta":
		return true
	default:
		return false
	}
}
