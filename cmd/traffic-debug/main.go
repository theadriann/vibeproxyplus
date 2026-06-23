package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type debugLog struct {
	Timestamp            string         `json:"timestamp"`
	RequestID            string         `json:"request_id"`
	Direction            string         `json:"direction"`
	Method               string         `json:"method"`
	Path                 string         `json:"path"`
	Status               string         `json:"status"`
	StatusCode           int            `json:"status_code"`
	Headers              map[string]any `json:"headers"`
	Body                 any            `json:"body"`
	ForwardedBodyChanged bool           `json:"forwarded_body_changed"`
	ForwardedBody        any            `json:"forwarded_body"`
}

type sseEvent struct {
	Event string
	Data  any
	Raw   string
}

type toolCallSummary struct {
	Name      string
	Arguments any
}

type captureSummary struct {
	Dir             string
	RequestID       string
	Timestamp       string
	Method          string
	Path            string
	Model           string
	ReasoningEffort string
	StatusCode      int
	ResponseText    string
	ResponseEvents  []sseEvent
	ToolCalls       []toolCallSummary
	Request         debugLog
	Response        debugLog
}

type renderOptions struct {
	IncludeBody bool
	MaxString   int
	MaxArray    int
}

func main() {
	defaultRoot := filepath.Join(userHomeDir(), ".vibeproxyplus", "logs", "traffic-debug")
	root := flag.String("root", defaultRoot, "traffic-debug root directory")
	capture := flag.String("capture", "", "single capture directory to parse")
	limit := flag.Int("limit", 25, "maximum captures to render when parsing a root")
	includeBody := flag.Bool("include-body", false, "include request/response bodies and SSE event details")
	full := flag.Bool("full", false, "do not truncate strings or arrays in markdown output")
	maxString := flag.Int("max-string", 500, "maximum string length in markdown JSON blocks; use -1 for unlimited")
	maxArray := flag.Int("max-array", 30, "maximum array length in markdown JSON blocks; use -1 for unlimited")
	exportDir := flag.String("export-dir", "", "write explorable artifacts to this directory without modifying captures")
	out := flag.String("out", "", "write markdown report to this path instead of stdout")
	flag.Parse()

	captures, err := loadCaptures(*root, *capture)
	if err != nil {
		fmt.Fprintf(os.Stderr, "traffic-debug: %v\n", err)
		os.Exit(1)
	}
	sort.Slice(captures, func(i, j int) bool {
		return captures[i].Timestamp > captures[j].Timestamp
	})
	if *capture == "" && *limit > 0 && len(captures) > *limit {
		captures = captures[:*limit]
	}

	if *exportDir != "" {
		if err := exportCaptures(captures, *exportDir); err != nil {
			fmt.Fprintf(os.Stderr, "traffic-debug: export: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported %d capture(s) to %s\n", len(captures), *exportDir)
	}

	options := renderOptions{IncludeBody: *includeBody, MaxString: *maxString, MaxArray: *maxArray}
	if *full {
		options.MaxString = -1
		options.MaxArray = -1
	}
	report := renderMarkdown(captures, options)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "traffic-debug: write report: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %s\n", *out)
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}

func loadCaptures(root, capture string) ([]captureSummary, error) {
	if strings.TrimSpace(capture) != "" {
		loaded, err := loadCapture(capture)
		if err != nil {
			return nil, err
		}
		return []captureSummary{loaded}, nil
	}

	var captures []captureSummary
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "request.json")); err != nil {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "response.json")); err != nil {
			return nil
		}
		loaded, err := loadCapture(path)
		if err != nil {
			return err
		}
		captures = append(captures, loaded)
		return nil
	})
	return captures, err
}

func loadCapture(dir string) (captureSummary, error) {
	request, err := readDebugLog(filepath.Join(dir, "request.json"))
	if err != nil {
		return captureSummary{}, err
	}
	response, err := readDebugLog(filepath.Join(dir, "response.json"))
	if err != nil {
		return captureSummary{}, err
	}

	events, text := responseBodySummary(response.Body)
	return captureSummary{
		Dir:             dir,
		RequestID:       firstNonEmpty(request.RequestID, response.RequestID),
		Timestamp:       firstNonEmpty(request.Timestamp, response.Timestamp),
		Method:          request.Method,
		Path:            request.Path,
		Model:           modelFromBody(request.Body),
		ReasoningEffort: reasoningEffortFromBody(request.Body),
		StatusCode:      response.StatusCode,
		ResponseText:    text,
		ResponseEvents:  events,
		ToolCalls:       extractToolCalls(events),
		Request:         request,
		Response:        response,
	}, nil
}

func readDebugLog(path string) (debugLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return debugLog{}, err
	}
	var log debugLog
	if err := json.Unmarshal(data, &log); err != nil {
		return debugLog{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return log, nil
}

func responseBodySummary(body any) ([]sseEvent, string) {
	switch typed := body.(type) {
	case string:
		events, text := parseSSEBody(typed)
		if len(events) > 0 || text != "" {
			return events, text
		}
		return nil, typed
	case map[string]any:
		return nil, textFromJSONBody(typed)
	default:
		return nil, ""
	}
}

func parseSSEBody(body string) ([]sseEvent, string) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 1024), 16*1024*1024)

	var events []sseEvent
	var eventName string
	var dataLines []string
	flush := func() {
		if eventName == "" && len(dataLines) == 0 {
			return
		}
		raw := strings.Join(dataLines, "\n")
		evt := sseEvent{Event: eventName, Raw: raw}
		if raw != "" {
			var value any
			if json.Unmarshal([]byte(raw), &value) == nil {
				evt.Data = value
			} else {
				evt.Data = raw
			}
		}
		events = append(events, evt)
		eventName = ""
		dataLines = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()

	var text strings.Builder
	for _, event := range events {
		text.WriteString(textDeltaFromEvent(event))
	}
	return events, text.String()
}

func textDeltaFromEvent(event sseEvent) string {
	if !strings.Contains(event.Event, "output_text") && !strings.Contains(event.Event, "content_block_delta") {
		return ""
	}
	value, ok := event.Data.(map[string]any)
	if !ok {
		return ""
	}
	if delta, ok := value["delta"].(string); ok {
		return delta
	}
	if delta, ok := value["delta"].(map[string]any); ok {
		if text, ok := delta["text"].(string); ok {
			return text
		}
	}
	if text, ok := value["text"].(string); ok {
		return text
	}
	return ""
}

func textFromJSONBody(body map[string]any) string {
	if outputText, ok := body["output_text"].(string); ok {
		return outputText
	}
	var out strings.Builder
	output, _ := body["output"].([]any)
	for _, item := range output {
		itemMap, _ := item.(map[string]any)
		content, _ := itemMap["content"].([]any)
		for _, c := range content {
			contentMap, _ := c.(map[string]any)
			if text, ok := contentMap["text"].(string); ok {
				out.WriteString(text)
			}
		}
	}
	return out.String()
}

func extractToolCalls(events []sseEvent) []toolCallSummary {
	var calls []toolCallSummary
	for _, event := range events {
		if event.Event != "response.output_item.done" {
			continue
		}
		data, ok := event.Data.(map[string]any)
		if !ok {
			continue
		}
		item, ok := data["item"].(map[string]any)
		if !ok || item["type"] != "function_call" {
			continue
		}
		name, _ := item["name"].(string)
		argsRaw, _ := item["arguments"].(string)
		var args any = argsRaw
		if argsRaw != "" {
			var decoded any
			if json.Unmarshal([]byte(argsRaw), &decoded) == nil {
				args = decoded
			}
		}
		calls = append(calls, toolCallSummary{Name: name, Arguments: args})
	}
	return calls
}

func modelFromBody(body any) string {
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	model, _ := bodyMap["model"].(string)
	return model
}

func reasoningEffortFromBody(body any) string {
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	if reasoning, ok := bodyMap["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok {
			return effort
		}
	}
	if effort, ok := bodyMap["reasoning_effort"].(string); ok {
		return effort
	}
	if outputConfig, ok := bodyMap["output_config"].(map[string]any); ok {
		if effort, ok := outputConfig["effort"].(string); ok {
			return effort
		}
	}
	return ""
}

func renderMarkdown(captures []captureSummary, options renderOptions) string {
	var out bytes.Buffer
	fmt.Fprintln(&out, "# VibeProxyPlus Traffic Debug")
	fmt.Fprintln(&out)
	fmt.Fprintf(&out, "Captures: %d\n\n", len(captures))
	for _, capture := range captures {
		title := capture.Timestamp
		if parsed, err := time.Parse(time.RFC3339Nano, capture.Timestamp); err == nil {
			title = parsed.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(&out, "## %s `%s`\n\n", title, capture.RequestID)
		fmt.Fprintf(&out, "- Directory: `%s`\n", capture.Dir)
		fmt.Fprintf(&out, "- Request: `%s %s`\n", capture.Method, capture.Path)
		fmt.Fprintf(&out, "- Model: `%s`\n", emptyDash(capture.Model))
		fmt.Fprintf(&out, "- Reasoning effort: `%s`\n", emptyDash(capture.ReasoningEffort))
		fmt.Fprintf(&out, "- Status: `%d`\n", capture.StatusCode)
		fmt.Fprintf(&out, "- SSE events: `%d`\n", len(capture.ResponseEvents))
		if len(capture.ToolCalls) > 0 {
			fmt.Fprintf(&out, "- Tool calls: `%d`", len(capture.ToolCalls))
			for _, call := range capture.ToolCalls {
				fmt.Fprintf(&out, " `%s`", emptyDash(call.Name))
			}
			fmt.Fprintln(&out)
		}
		if capture.ResponseText != "" {
			fmt.Fprintf(&out, "- Response text preview: %s\n", markdownInline(capture.ResponseText, 300))
		}
		fmt.Fprintln(&out)

		if !options.IncludeBody {
			continue
		}
		fmt.Fprintln(&out, "### Request body")
		fmt.Fprintln(&out)
		writeJSONBlock(&out, capture.Request.Body, options)
		fmt.Fprintln(&out)
		if capture.Request.ForwardedBodyChanged {
			fmt.Fprintln(&out, "### Forwarded request body")
			fmt.Fprintln(&out)
			writeJSONBlock(&out, capture.Request.ForwardedBody, options)
			fmt.Fprintln(&out)
		}
		if len(capture.ToolCalls) > 0 {
			fmt.Fprintln(&out, "### Tool calls")
			fmt.Fprintln(&out)
			for i, call := range capture.ToolCalls {
				fmt.Fprintf(&out, "#### %03d `%s`\n\n", i+1, emptyDash(call.Name))
				writeJSONBlock(&out, call.Arguments, options)
				fmt.Fprintln(&out)
			}
		}
		fmt.Fprintln(&out, "### Response text")
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, "```text")
		fmt.Fprintln(&out, capture.ResponseText)
		fmt.Fprintln(&out, "```")
		if len(capture.ResponseEvents) > 0 {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "### SSE events")
			fmt.Fprintln(&out)
			for i, event := range capture.ResponseEvents {
				fmt.Fprintf(&out, "#### %03d `%s`\n\n", i+1, emptyDash(event.Event))
				writeJSONBlock(&out, event.Data, options)
				fmt.Fprintln(&out)
			}
		}
	}
	return out.String()
}

func writeJSONBlock(out *bytes.Buffer, value any, options renderOptions) {
	data, err := marshalReadableJSON(compactDebugValue(value, options))
	if err != nil {
		fmt.Fprintln(out, "```text")
		fmt.Fprintln(out, value)
		fmt.Fprintln(out, "```")
		return
	}
	fmt.Fprintln(out, "```json")
	fmt.Fprintln(out, string(bytes.TrimSpace(data)))
	fmt.Fprintln(out, "```")
}

func marshalReadableJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func compactDebugValue(value any, options renderOptions) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = compactDebugValue(nested, options)
		}
		return out
	case []any:
		limit := len(typed)
		truncated := 0
		if options.MaxArray >= 0 && limit > options.MaxArray {
			truncated = limit - options.MaxArray
			limit = options.MaxArray
		}
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, compactDebugValue(typed[i], options))
		}
		if truncated > 0 {
			out = append(out, fmt.Sprintf("... [%d more items truncated]", truncated))
		}
		return out
	case string:
		if options.MaxString < 0 || len(typed) <= options.MaxString {
			return typed
		}
		return typed[:options.MaxString] + fmt.Sprintf(" ... [%d chars truncated]", len(typed)-options.MaxString)
	default:
		return typed
	}
}

func exportCaptures(captures []captureSummary, exportRoot string) error {
	for _, capture := range captures {
		dir := filepath.Join(exportRoot, exportDirName(capture))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		summary := map[string]any{
			"request_id":       capture.RequestID,
			"timestamp":        capture.Timestamp,
			"source_dir":       capture.Dir,
			"method":           capture.Method,
			"path":             capture.Path,
			"model":            capture.Model,
			"reasoning_effort": capture.ReasoningEffort,
			"status_code":      capture.StatusCode,
			"sse_event_count":  len(capture.ResponseEvents),
			"tool_call_count":  len(capture.ToolCalls),
		}
		if err := writeJSONFile(filepath.Join(dir, "summary.json"), summary); err != nil {
			return err
		}
		if err := writeJSONFile(filepath.Join(dir, "request.body.json"), capture.Request.Body); err != nil {
			return err
		}
		if capture.Request.ForwardedBodyChanged {
			if err := writeJSONFile(filepath.Join(dir, "request.forwarded-body.json"), capture.Request.ForwardedBody); err != nil {
				return err
			}
		}
		if err := writeResponseArtifacts(dir, capture); err != nil {
			return err
		}
		if len(capture.ToolCalls) > 0 {
			if err := writeJSONFile(filepath.Join(dir, "tool-calls.json"), capture.ToolCalls); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeResponseArtifacts(dir string, capture captureSummary) error {
	switch body := capture.Response.Body.(type) {
	case string:
		if err := os.WriteFile(filepath.Join(dir, "response.sse.raw.txt"), []byte(body), 0o600); err != nil {
			return err
		}
		if len(capture.ResponseEvents) > 0 {
			if err := writeJSONFile(filepath.Join(dir, "response.sse.events.json"), capture.ResponseEvents); err != nil {
				return err
			}
		}
	case map[string]any:
		if err := writeJSONFile(filepath.Join(dir, "response.body.json"), body); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "response.text.txt"), []byte(capture.ResponseText+"\n"), 0o600); err != nil {
		return err
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := marshalReadableJSON(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func exportDirName(capture captureSummary) string {
	if parsed, err := time.Parse(time.RFC3339Nano, capture.Timestamp); err == nil {
		return parsed.Format("2006-01-02-150405.000000000Z") + "-" + capture.RequestID
	}
	base := filepath.Base(capture.Dir)
	if base != "." && base != string(filepath.Separator) {
		return base
	}
	return capture.RequestID
}

func markdownInline(value string, limit int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > limit {
		value = value[:limit] + "..."
	}
	return "`" + strings.ReplaceAll(value, "`", "'") + "`"
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
