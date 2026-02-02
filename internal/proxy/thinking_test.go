package proxy

import (
	"strings"
	"testing"
)

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
			name:            "budget capped at 32768",
			model:           "claude-opus-4-5-20251101-thinking-50000",
			wantModel:       "claude-opus-4-5-20251101",
			wantBudget:      32768,
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

	output, transformed, err := TransformRequestBody([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !transformed {
		t.Fatal("expected transformed=true")
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

	output, transformed, err := TransformRequestBody([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transformed {
		t.Fatal("expected transformed=false for non-thinking model")
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

	output, needsHeader, err := TransformRequestBody([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needsHeader {
		t.Fatal("expected needsHeader=true for -thinking suffix model")
	}
	if string(output) != input {
		t.Errorf("body should be unchanged for -thinking suffix (backend handles)")
	}
}
