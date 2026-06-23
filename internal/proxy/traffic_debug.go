package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	trafficDebugEnabledEnv   = "VIBEPROXYPLUS_TRAFFIC_DEBUG"
	trafficDebugDirEnv       = "VIBEPROXYPLUS_TRAFFIC_DEBUG_DIR"
	trafficDebugBodyLimitEnv = "VIBEPROXYPLUS_TRAFFIC_DEBUG_BODY_LIMIT"
	defaultTrafficDebugLimit = 4 * 1024 * 1024
)

type trafficDebugContextKey struct{}

var trafficDebugIndexMu sync.Mutex

type trafficDebugRecord struct {
	Enabled   bool
	ID        string
	Dir       string
	BodyLimit int64
	StartedAt time.Time
}

func newTrafficDebugRecord(_ *http.Request) trafficDebugRecord {
	if !trafficDebugEnabled() {
		return trafficDebugRecord{}
	}

	return trafficDebugRecord{
		Enabled:   true,
		ID:        newTrafficDebugID(),
		Dir:       trafficDebugDir(),
		BodyLimit: trafficDebugBodyLimit(),
		StartedAt: time.Now().UTC(),
	}
}

func trafficDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(trafficDebugEnabledEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func trafficDebugDir() string {
	if dir := strings.TrimSpace(os.Getenv(trafficDebugDirEnv)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "traffic-debug")
	}
	return filepath.Join(home, ".vibeproxyplus", "logs", "traffic-debug")
}

func trafficDebugBodyLimit() int64 {
	raw := strings.TrimSpace(os.Getenv(trafficDebugBodyLimitEnv))
	if raw == "" {
		return defaultTrafficDebugLimit
	}
	limit, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || limit < 0 {
		return defaultTrafficDebugLimit
	}
	return limit
}

func newTrafficDebugID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (d trafficDebugRecord) Attach(r *http.Request) *http.Request {
	if !d.Enabled {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), trafficDebugContextKey{}, d))
}

func trafficDebugFromContext(ctx context.Context) (trafficDebugRecord, bool) {
	record, ok := ctx.Value(trafficDebugContextKey{}).(trafficDebugRecord)
	return record, ok && record.Enabled
}

func (d trafficDebugRecord) LogRequest(r *http.Request, originalBody, forwardedBody []byte) {
	if !d.Enabled {
		return
	}

	entry := map[string]any{
		"timestamp":              time.Now().UTC().Format(time.RFC3339Nano),
		"request_id":             d.ID,
		"direction":              "request",
		"method":                 r.Method,
		"path":                   r.URL.Path,
		"query":                  r.URL.RawQuery,
		"content_length":         r.ContentLength,
		"headers":                redactHeaders(r.Header),
		"body_bytes":             len(originalBody),
		"body_truncated":         int64(len(originalBody)) > d.BodyLimit,
		"body":                   trafficDebugBodyValue(originalBody, d.BodyLimit),
		"forwarded_body_changed": !bytes.Equal(originalBody, forwardedBody),
	}
	if !bytes.Equal(originalBody, forwardedBody) {
		entry["forwarded_body_bytes"] = len(forwardedBody)
		entry["forwarded_body_truncated"] = int64(len(forwardedBody)) > d.BodyLimit
		entry["forwarded_body"] = trafficDebugBodyValue(forwardedBody, d.BodyLimit)
	}

	d.write("request", entry)
}

func wrapTrafficDebugResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil || resp.Request == nil {
		return
	}
	record, ok := trafficDebugFromContext(resp.Request.Context())
	if !ok {
		return
	}
	resp.Body = &trafficDebugResponseBody{
		ReadCloser: resp.Body,
		record:     record,
		resp:       resp,
	}
}

type trafficDebugResponseBody struct {
	io.ReadCloser
	record    trafficDebugRecord
	resp      *http.Response
	buf       []byte
	truncated bool
	once      sync.Once
}

func (b *trafficDebugResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.capture(p[:n])
	}
	if err == io.EOF {
		b.log()
	}
	return n, err
}

func (b *trafficDebugResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.log()
	return err
}

func (b *trafficDebugResponseBody) capture(chunk []byte) {
	if int64(len(b.buf)) >= b.record.BodyLimit {
		if len(chunk) > 0 {
			b.truncated = true
		}
		return
	}

	remaining := int(b.record.BodyLimit - int64(len(b.buf)))
	if len(chunk) > remaining {
		b.buf = append(b.buf, chunk[:remaining]...)
		b.truncated = true
		return
	}
	b.buf = append(b.buf, chunk...)
}

func (b *trafficDebugResponseBody) log() {
	b.once.Do(func() {
		entry := map[string]any{
			"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
			"request_id":     b.record.ID,
			"direction":      "response",
			"status_code":    b.resp.StatusCode,
			"status":         b.resp.Status,
			"content_length": b.resp.ContentLength,
			"headers":        redactHeaders(b.resp.Header),
			"body_bytes":     len(b.buf),
			"body_truncated": b.truncated,
			"body":           trafficDebugBodyValue(b.buf, b.record.BodyLimit),
		}
		b.record.write("response", entry)
	})
}

func (d trafficDebugRecord) write(kind string, entry map[string]any) {
	captureDir := d.captureDir()
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		log.Printf("traffic debug: create log dir: %v", err)
		return
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		log.Printf("traffic debug: marshal %s log: %v", kind, err)
		return
	}
	data = append(data, '\n')

	name := kind + ".json"
	path := filepath.Join(captureDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Printf("traffic debug: write %s log: %v", kind, err)
		return
	}
	d.appendIndex(kind, name, entry)
}

func (d trafficDebugRecord) dayDir() string {
	return filepath.Join(d.Dir, d.StartedAt.Format("2006-01-02"))
}

func (d trafficDebugRecord) captureDir() string {
	name := d.StartedAt.Format("150405.000000000Z") + "-" + d.ID
	return filepath.Join(d.dayDir(), name)
}

func (d trafficDebugRecord) appendIndex(kind, fileName string, entry map[string]any) {
	summary := map[string]any{
		"timestamp":   entry["timestamp"],
		"request_id":  d.ID,
		"direction":   kind,
		"capture_dir": filepath.ToSlash(filepath.Base(d.captureDir())),
	}
	if method, ok := entry["method"].(string); ok && method != "" {
		summary["method"] = method
	}
	if path, ok := entry["path"].(string); ok && path != "" {
		summary["path"] = path
	}
	if statusCode, ok := entry["status_code"].(int); ok {
		summary["status_code"] = statusCode
	}
	if model := trafficDebugModelFromEntry(entry); model != "" {
		summary["model"] = model
	}
	switch kind {
	case "request":
		summary["request_file"] = filepath.ToSlash(filepath.Join(filepath.Base(d.captureDir()), fileName))
	case "response":
		summary["response_file"] = filepath.ToSlash(filepath.Join(filepath.Base(d.captureDir()), fileName))
	default:
		summary["file"] = filepath.ToSlash(filepath.Join(filepath.Base(d.captureDir()), fileName))
	}

	data, err := json.Marshal(summary)
	if err != nil {
		log.Printf("traffic debug: marshal index entry: %v", err)
		return
	}
	data = append(data, '\n')

	trafficDebugIndexMu.Lock()
	defer trafficDebugIndexMu.Unlock()

	indexPath := filepath.Join(d.dayDir(), "index.ndjson")
	file, err := os.OpenFile(indexPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("traffic debug: open index: %v", err)
		return
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		log.Printf("traffic debug: write index: %v", err)
	}
}

func trafficDebugModelFromEntry(entry map[string]any) string {
	body, ok := entry["body"].(map[string]any)
	if !ok {
		return ""
	}
	model, _ := body["model"].(string)
	return model
}

func redactHeaders(headers http.Header) map[string][]string {
	redacted := make(map[string][]string, len(headers))
	for key, values := range headers {
		if isSensitiveName(key) {
			redacted[key] = []string{"[REDACTED]"}
			continue
		}
		redacted[key] = append([]string(nil), values...)
	}
	return redacted
}

func trafficDebugBodyValue(body []byte, limit int64) any {
	if limit < 0 {
		limit = 0
	}
	if int64(len(body)) > limit {
		body = body[:limit]
	}

	var value any
	if len(body) > 0 && json.Unmarshal(body, &value) == nil {
		return redactJSONValue(value)
	}
	return string(body)
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveName(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactJSONValue(nested)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = redactJSONValue(nested)
		}
		return out
	default:
		return typed
	}
}

func isSensitiveName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
	for _, marker := range []string{"authorization", "api-key", "access-token", "refresh-token", "bearer", "cookie", "password", "secret"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "token" || normalized == "key"
}
