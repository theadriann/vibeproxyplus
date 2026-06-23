package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSSEBodyExtractsEventsAndText(t *testing.T) {
	body := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"file_path\\\":\"}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"

	events, text := parseSSEBody(body)

	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
	if events[2].Event != "response.output_text.delta" {
		t.Fatalf("event[2] = %q", events[2].Event)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want hello", text)
	}
}

func TestLoadCaptureBuildsSummaryWithoutModifyingSource(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "2026-06-14", "120000.000000000Z-abc")
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	requestPath := filepath.Join(captureDir, "request.json")
	responsePath := filepath.Join(captureDir, "response.json")
	request := `{
  "timestamp": "2026-06-14T12:00:00Z",
  "request_id": "abc",
  "direction": "request",
  "method": "POST",
  "path": "/v1/responses",
  "body": {"model": "gpt-5.5", "reasoning": {"effort": "high"}, "input": [{"content": [{"text": "<system-reminder>hello</system-reminder>"}]}]},
  "forwarded_body_changed": true,
  "forwarded_body": {"model": "gpt-5.5", "reasoning": {"effort": "high"}}
}`
	response := `{
  "timestamp": "2026-06-14T12:00:01Z",
  "request_id": "abc",
  "direction": "response",
  "status_code": 200,
  "body": "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\nevent: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"name\":\"Read\",\"arguments\":\"{\\\"file_path\\\":\\\"/tmp/a\\\"}\"}}\n\n"
}`
	if err := os.WriteFile(requestPath, []byte(request), 0o600); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := os.WriteFile(responsePath, []byte(response), 0o600); err != nil {
		t.Fatalf("write response: %v", err)
	}
	before, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatalf("read response before: %v", err)
	}

	capture, err := loadCapture(captureDir)
	if err != nil {
		t.Fatalf("load capture: %v", err)
	}
	if capture.Model != "gpt-5.5" || capture.ReasoningEffort != "high" || capture.StatusCode != 200 {
		t.Fatalf("unexpected capture summary: %#v", capture)
	}
	if capture.ResponseText != "ok" {
		t.Fatalf("response text = %q, want ok", capture.ResponseText)
	}
	if len(capture.ToolCalls) != 1 || capture.ToolCalls[0].Name != "Read" {
		t.Fatalf("tool calls = %#v, want Read", capture.ToolCalls)
	}
	rendered := renderMarkdown([]captureSummary{capture}, renderOptions{IncludeBody: true, MaxString: -1, MaxArray: -1})
	for _, want := range []string{"# VibeProxyPlus Traffic Debug", "gpt-5.5", "high", "response.output_text.delta", "Read", "/tmp/a", "ok"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, `\u003c`) {
		t.Fatalf("rendered markdown escaped angle brackets:\n%s", rendered)
	}
	if !strings.Contains(rendered, "<system-reminder>hello</system-reminder>") {
		t.Fatalf("rendered markdown missing unescaped system reminder:\n%s", rendered)
	}

	after, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatalf("read response after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("source response file was modified")
	}
}

func TestExportCaptureArtifactsWritesReadableFilesWithoutModifyingSource(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "2026-06-14", "120000.000000000Z-abc")
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	requestPath := filepath.Join(captureDir, "request.json")
	responsePath := filepath.Join(captureDir, "response.json")
	request := `{"timestamp":"2026-06-14T12:00:00Z","request_id":"abc","method":"POST","path":"/v1/responses","body":{"model":"gpt-5.5","input":[{"content":[{"text":"<system-reminder>hello</system-reminder>"}]}]}}`
	response := `{"timestamp":"2026-06-14T12:00:01Z","request_id":"abc","status_code":200,"body":"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"}`
	if err := os.WriteFile(requestPath, []byte(request), 0o600); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := os.WriteFile(responsePath, []byte(response), 0o600); err != nil {
		t.Fatalf("write response: %v", err)
	}
	before, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read request before: %v", err)
	}
	capture, err := loadCapture(captureDir)
	if err != nil {
		t.Fatalf("load capture: %v", err)
	}
	exportDir := filepath.Join(root, "export")

	if err := exportCaptures([]captureSummary{capture}, exportDir); err != nil {
		t.Fatalf("export captures: %v", err)
	}

	requestBody, err := os.ReadFile(filepath.Join(exportDir, "2026-06-14-120000.000000000Z-abc", "request.body.json"))
	if err != nil {
		t.Fatalf("read exported request body: %v", err)
	}
	if strings.Contains(string(requestBody), `\u003c`) || !strings.Contains(string(requestBody), "<system-reminder>hello</system-reminder>") {
		t.Fatalf("exported request body is not readable:\n%s", requestBody)
	}
	responseText, err := os.ReadFile(filepath.Join(exportDir, "2026-06-14-120000.000000000Z-abc", "response.text.txt"))
	if err != nil {
		t.Fatalf("read exported response text: %v", err)
	}
	if string(responseText) != "ok\n" {
		t.Fatalf("response text = %q, want ok newline", responseText)
	}
	after, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read request after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("source request file was modified")
	}
}
