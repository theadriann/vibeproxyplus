package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func hasBeta(betas []string, target string) bool {
	for _, beta := range betas {
		if beta == target {
			return true
		}
	}
	return false
}

func TestParseThinkingSuffix(t *testing.T) {
	tests := []struct {
		name            string
		model           string
		wantModel       string
		wantBudget      int
		wantHasThinking bool
	}{
		{
			name:            "claude with thinking suffix",
			model:           "claude-opus-4-5-20251101-thinking-10000",
			wantModel:       "claude-opus-4-5-20251101",
			wantBudget:      10000,
			wantHasThinking: true,
		},
		{
			name:            "claude sonnet with high budget",
			model:           "claude-sonnet-4-5-20250929-thinking-32000",
			wantModel:       "claude-sonnet-4-5-20250929",
			wantBudget:      32000,
			wantHasThinking: true,
		},
		{
			name:            "gemini-claude keeps -thinking in name",
			model:           "gemini-claude-opus-4-5-thinking-10000",
			wantModel:       "gemini-claude-opus-4-5-thinking",
			wantBudget:      10000,
			wantHasThinking: true,
		},
		{
			name:            "no thinking suffix",
			model:           "claude-opus-4-5-20251101",
			wantModel:       "claude-opus-4-5-20251101",
			wantBudget:      0,
			wantHasThinking: false,
		},
		{
			name:            "gpt model passthrough",
			model:           "gpt-5.1-codex",
			wantModel:       "gpt-5.1-codex",
			wantBudget:      0,
			wantHasThinking: false,
		},
		{
			name:            "budget preserved within model range",
			model:           "claude-opus-4-5-20251101-thinking-50000",
			wantModel:       "claude-opus-4-5-20251101",
			wantBudget:      50000,
			wantHasThinking: true,
		},
		{
			name:            "invalid budget ignored",
			model:           "claude-opus-4-5-20251101-thinking-abc",
			wantModel:       "claude-opus-4-5-20251101",
			wantBudget:      0,
			wantHasThinking: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, gotBudget, gotHasThinking := ParseThinkingSuffix(tt.model)
			if gotModel != tt.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tt.wantModel)
			}
			if gotBudget != tt.wantBudget {
				t.Errorf("budget = %d, want %d", gotBudget, tt.wantBudget)
			}
			if gotHasThinking != tt.wantHasThinking {
				t.Errorf("hasThinking = %v, want %v", gotHasThinking, tt.wantHasThinking)
			}
		})
	}
}

func TestTransformRequestBody(t *testing.T) {
	input := `{"model":"claude-opus-4-5-20251101-thinking-10000","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaInterleaved) {
		t.Fatalf("expected %q beta header, got %v", BetaInterleaved, betas)
	}

	// Check model was changed
	if !strings.Contains(string(output), `"model":"claude-opus-4-5-20251101"`) {
		t.Errorf("model not transformed: %s", output)
	}

	// Check thinking param added
	if !strings.Contains(string(output), `"thinking"`) {
		t.Errorf("thinking param not added: %s", output)
	}
	if !strings.Contains(string(output), `"budget_tokens":10000`) {
		t.Errorf("budget_tokens not set: %s", output)
	}
}

func TestTransformRequestBody_NoThinking(t *testing.T) {
	input := `{"model":"gpt-5.1-codex","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas for non-thinking model, got %v", betas)
	}
	if string(output) != input {
		t.Errorf("body should be unchanged")
	}
}

func TestHasThinkingPattern(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gemini-claude-opus-4-5-thinking", true},
		{"claude-sonnet-4-5-thinking", true},
		{"gemini-claude-opus-4-5-thinking(32768)", true},
		{"claude-opus-4-5-20251101", false},
		{"gpt-5.2-codex", false},
		{"gpt-5.2(high)", false}, // parentheses without -thinking prefix
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := HasThinkingPattern(tt.model)
			if got != tt.want {
				t.Errorf("HasThinkingPattern(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestTransformRequestBody_ThinkingPatternNeedsHeader(t *testing.T) {
	// Model with -thinking suffix should return needsBetaHeader=true
	// but body should not be transformed (backend handles it)
	input := `{"model":"gemini-claude-opus-4-5-thinking","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaInterleaved) {
		t.Fatalf("expected %q beta for -thinking suffix model, got %v", BetaInterleaved, betas)
	}
	if string(output) != input {
		t.Errorf("body should be unchanged for -thinking suffix (backend handles)")
	}
}

func TestTransformRequestBody_CodexResponsesInputStringNormalized(t *testing.T) {
	input := `{"model":"gpt-5.2-codex","input":"hello"}` // compact/summarize path sends input as string

	output, betas, err := TransformRequestBody("/v1/responses", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas for codex normalization, got %v", betas)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(output, &body); err != nil {
		t.Fatalf("invalid output json: %v", err)
	}

	inputField, ok := body["input"].([]interface{})
	if !ok || len(inputField) != 1 {
		t.Fatalf("expected input array with one item, got: %v", body["input"])
	}

	msg, ok := inputField[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected message object, got: %T", inputField[0])
	}

	if msg["role"] != "user" || msg["type"] != "message" {
		t.Fatalf("unexpected message shape: %+v", msg)
	}

	content, ok := msg["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected content array with one item, got: %v", msg["content"])
	}

	contentItem, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected content object, got: %T", content[0])
	}
	if contentItem["type"] != "input_text" || contentItem["text"] != "hello" {
		t.Fatalf("unexpected content item: %+v", contentItem)
	}
}

func TestTransformRequestBody_CodexNormalizationOnlyOnResponsesPath(t *testing.T) {
	input := `{"model":"gpt-5.2-codex","input":"hello"}`

	output, betas, err := TransformRequestBody("/v1/chat/completions", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas, got %v", betas)
	}
	if string(output) != input {
		t.Errorf("body should stay unchanged on non-responses path: %s", output)
	}
}

func TestTransformRequestBody_CodexFastAliasAddsPriorityServiceTier(t *testing.T) {
	input := `{"model":"gpt-5.5(fast)","input":"hello"}`

	output, betas, err := TransformRequestBody("/v1/responses", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas for codex fast alias, got %v", betas)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(output, &body); err != nil {
		t.Fatalf("invalid output json: %v", err)
	}
	if got := body["model"]; got != "gpt-5.5" {
		t.Fatalf("model = %v, want %q", got, "gpt-5.5")
	}
	if got := body["service_tier"]; got != "priority" {
		t.Fatalf("service_tier = %v, want %q", got, "priority")
	}
	if _, ok := body["input"].([]interface{}); !ok {
		t.Fatalf("expected codex string input to be normalized, got %T", body["input"])
	}
}

func TestTransformRequestBody_CodexFastReasoningAliasKeepsReasoningSuffix(t *testing.T) {
	input := `{"model":"gpt-5.5(high-fast)","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/responses", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas for codex fast reasoning alias, got %v", betas)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(output, &body); err != nil {
		t.Fatalf("invalid output json: %v", err)
	}
	if got := body["model"]; got != "gpt-5.5(high)" {
		t.Fatalf("model = %v, want %q", got, "gpt-5.5(high)")
	}
	if got := body["service_tier"]; got != "priority" {
		t.Fatalf("service_tier = %v, want %q", got, "priority")
	}
}

func TestTransformRequestBody_ClaudeOpus47AdaptiveSuffix(t *testing.T) {
	input := `{"model":"claude-opus-4-7(xhigh)","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(betas) != 0 {
		t.Fatalf("expected no betas for adaptive thinking, got %v", betas)
	}
	if !strings.Contains(string(output), `"model":"claude-opus-4-7"`) {
		t.Fatalf("expected transformed model id, got %s", output)
	}
	if !strings.Contains(string(output), `"type":"adaptive"`) {
		t.Fatalf("expected adaptive thinking, got %s", output)
	}
	if !strings.Contains(string(output), `"effort":"xhigh"`) {
		t.Fatalf("expected xhigh effort, got %s", output)
	}
	if strings.Contains(string(output), `"budget_tokens"`) {
		t.Fatalf("did not expect manual budget tokens, got %s", output)
	}
}

func TestTransformRequestBody_ClaudeOpus45LevelUsesManualBudgetAndEffort(t *testing.T) {
	input := `{"model":"claude-opus-4-5-20251101(high)","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaInterleaved) {
		t.Fatalf("expected %q beta for manual thinking, got %v", BetaInterleaved, betas)
	}
	if !strings.Contains(string(output), `"model":"claude-opus-4-5-20251101"`) {
		t.Fatalf("expected transformed model id, got %s", output)
	}
	if !strings.Contains(string(output), `"type":"enabled"`) {
		t.Fatalf("expected manual thinking enabled, got %s", output)
	}
	if !strings.Contains(string(output), `"budget_tokens":24576`) {
		t.Fatalf("expected mapped manual budget, got %s", output)
	}
	if !strings.Contains(string(output), `"effort":"high"`) {
		t.Fatalf("expected high effort, got %s", output)
	}
}

func TestTransformRequestBody_ClaudeOpus47TaskBudgetAddsBetaHeader(t *testing.T) {
	input := `{"model":"claude-opus-4-7","thinking":{"type":"adaptive"},"output_config":{"effort":"high","task_budget":{"type":"tokens","total":64000}},"messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaTaskBudgets) {
		t.Fatalf("expected %q beta for task budgets, got %v", BetaTaskBudgets, betas)
	}
	if string(output) != input {
		t.Fatalf("expected body passthrough for explicit task budget request, got %s", output)
	}
}

func TestTransformRequestBody_GeminiClaudeOpus47LegacyBudgetUsesAdaptiveProfile(t *testing.T) {
	input := `{"model":"gemini-claude-opus-4-7-thinking-10000","messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaInterleaved) {
		t.Fatalf("expected %q beta header for gemini-claude thinking alias, got %v", BetaInterleaved, betas)
	}
	if !strings.Contains(string(output), `"model":"gemini-claude-opus-4-7-thinking"`) {
		t.Fatalf("expected transformed gemini-claude model id, got %s", output)
	}
	if !strings.Contains(string(output), `"type":"adaptive"`) {
		t.Fatalf("expected adaptive thinking for gemini-claude opus 4.7, got %s", output)
	}
	if !strings.Contains(string(output), `"effort":"medium"`) {
		t.Fatalf("expected 10000 budget alias to map to medium effort, got %s", output)
	}
	if strings.Contains(string(output), `"budget_tokens"`) {
		t.Fatalf("did not expect budget_tokens for gemini-claude opus 4.7, got %s", output)
	}
}

func TestTransformRequestBody_ManualThinkingClampsBudgetAndMaxTokensToModelLimit(t *testing.T) {
	input := `{"model":"claude-opus-4-5-20251101-thinking-100000","max_tokens":200000,"messages":[{"role":"user","content":"hi"}]}`

	output, betas, err := TransformRequestBody("/v1/messages", []byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBeta(betas, BetaInterleaved) {
		t.Fatalf("expected %q beta header, got %v", BetaInterleaved, betas)
	}
	if !strings.Contains(string(output), `"budget_tokens":63999`) {
		t.Fatalf("expected manual thinking budget to clamp to 63999, got %s", output)
	}
	if !strings.Contains(string(output), `"max_tokens":64000`) {
		t.Fatalf("expected max_tokens to clamp to 64000, got %s", output)
	}
}
