package main

import (
	"os"
	"strings"
	"testing"
)

func TestBuildModelsDevIndex_PrefersAuthoritativeProvider(t *testing.T) {
	api := ModelsDevAPI{
		"github-copilot": {
			ID: "github-copilot",
			Models: map[string]*ModelsDevModel{
				"gpt-5.2-codex": {
					ID:   "gpt-5.2-codex",
					Name: "Copilot GPT-5.2 Codex",
					Cost: map[string]float64{
						"input": 999,
					},
				},
			},
		},
		"openai": {
			ID: "openai",
			Models: map[string]*ModelsDevModel{
				"gpt-5.2-codex": {
					ID:   "gpt-5.2-codex",
					Name: "OpenAI GPT-5.2 Codex",
					Cost: map[string]float64{
						"input": 1.75,
					},
				},
			},
		},
	}

	index := buildModelsDevIndex(api)
	got := index["gpt-5.2-codex"]
	if got == nil {
		t.Fatal("missing index entry for gpt-5.2-codex")
	}
	if got.Name != "OpenAI GPT-5.2 Codex" {
		t.Fatalf("got model name %q, want %q", got.Name, "OpenAI GPT-5.2 Codex")
	}
}

func TestRepositoryTargetsCLIProxyAPIInsteadOfPlus(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		want     []string
		unwanted []string
	}{
		{
			name: "model sync sources",
			path: "main.go",
			want: []string{
				"raw.githubusercontent.com/router-for-me/CLIProxyAPI/",
				"raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json",
			},
			unwanted: []string{"CLIProxyAPIPlus", "internal/registry/models/models.json"},
		},
		{
			name:     "makefile download/update",
			path:     "../../Makefile",
			want:     []string{"router-for-me/CLIProxyAPI", "cli-proxy-api"},
			unwanted: []string{"CLIProxyAPIPlus", "cli-proxy-api-plus"},
		},
		{
			name:     "unix start script",
			path:     "../../scripts/start.sh",
			want:     []string{"bin/cli-proxy-api", "Starting CLIProxyAPI"},
			unwanted: []string{"CLIProxyAPIPlus", "cli-proxy-api-plus"},
		},
		{
			name:     "windows start script",
			path:     "../../scripts/start.bat",
			want:     []string{"bin\\cli-proxy-api.exe", "Starting CLIProxyAPI"},
			unwanted: []string{"CLIProxyAPIPlus", "cli-proxy-api-plus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			content := string(data)
			for _, want := range tt.want {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", tt.path, want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(content, unwanted) {
					t.Fatalf("expected %s not to contain %q", tt.path, unwanted)
				}
			}
		})
	}
}

func TestBuildModelsDevIndex_PopulatesNormalizedID(t *testing.T) {
	api := ModelsDevAPI{
		"anthropic": {
			ID: "anthropic",
			Models: map[string]*ModelsDevModel{
				"claude-sonnet-4-5-20250929": {
					ID:   "claude-sonnet-4-5-20250929",
					Name: "Claude Sonnet 4.5",
				},
			},
		},
	}

	index := buildModelsDevIndex(api)
	got := index["claude-sonnet-4-5"]
	if got == nil {
		t.Fatal("missing normalized index entry for claude-sonnet-4-5")
	}
	if got.ID != "claude-sonnet-4-5-20250929" {
		t.Fatalf("got normalized model id %q, want %q", got.ID, "claude-sonnet-4-5-20250929")
	}
}

func TestValidateFactoryModelObject_RejectsCustomFields(t *testing.T) {
	obj := map[string]interface{}{
		"model":       "gpt-5-codex",
		"baseUrl":     "http://localhost:8317/v1",
		"apiKey":      "${OPENAI_API_KEY}",
		"provider":    "openai",
		"displayName": "GPT-5 Codex",
		"id":          "custom-id",
	}

	err := validateFactoryModelObject(obj)
	if err == nil {
		t.Fatal("expected error for unsupported custom field")
	}
	if !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("expected unsupported field error, got: %v", err)
	}
}

func TestValidateFactoryModelObject_RejectsIndexField(t *testing.T) {
	obj := map[string]interface{}{
		"model":    "gpt-5-codex",
		"baseUrl":  "http://localhost:8317/v1",
		"apiKey":   "${OPENAI_API_KEY}",
		"provider": "openai",
		"index":    1,
	}

	err := validateFactoryModelObject(obj)
	if err == nil {
		t.Fatal("expected error for unsupported index field")
	}
	if !strings.Contains(err.Error(), "index") {
		t.Fatalf("expected index field error, got: %v", err)
	}
}

func TestValidateFactoryModelObject_AllowsSupportedFieldsOnly(t *testing.T) {
	obj := map[string]interface{}{
		"model":           "claude-sonnet-4-5-20250929",
		"displayName":     "Claude Sonnet 4.5",
		"baseUrl":         "http://localhost:8317",
		"apiKey":          "${ANTHROPIC_API_KEY}",
		"provider":        "anthropic",
		"maxOutputTokens": 64000,
		"supportsImages":  true,
		"extraArgs": map[string]interface{}{
			"temperature": 0.2,
		},
		"extraHeaders": map[string]interface{}{
			"X-Test": "1",
		},
	}

	if err := validateFactoryModelObject(obj); err != nil {
		t.Fatalf("expected object to validate, got: %v", err)
	}
}

func TestGenerateFactoryConfig_SortsDeterministicallyWhenDisplayNamesMatch(t *testing.T) {
	models := map[string][]Model{
		"codex": {
			{ID: "z-model", Provider: "codex", DisplayName: "Same Name", MaxCompletionTokens: 1},
			{ID: "a-model", Provider: "codex", DisplayName: "Same Name", MaxCompletionTokens: 1},
		},
	}

	config := generateFactoryConfig(models)
	if len(config.CustomModels) != 2 {
		t.Fatalf("expected 2 custom models, got %d", len(config.CustomModels))
	}

	if got := config.CustomModels[0].Model; got != "a-model" {
		t.Fatalf("expected first model to be a-model, got %q", got)
	}
	if got := config.CustomModels[1].Model; got != "z-model" {
		t.Fatalf("expected second model to be z-model, got %q", got)
	}
}

func TestParseAndEnrichModels_UsesStaticModelsJSONForGetterBackedProviders(t *testing.T) {
	source := `
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

func GetCodexFreeModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexFree)
}

func GetCodexProModels() []*ModelInfo {
	return cloneModelInfos(getModels().CodexPro)
}
`

	staticModelsJSON := `{
  "claude": [
    {
      "id": "claude-sonnet-4-5-20250929",
      "owned_by": "anthropic",
      "type": "claude",
      "display_name": "Claude Sonnet 4.5",
      "max_completion_tokens": 64000,
      "thinking": {
        "min": 1024,
        "max": 128000,
        "zero_allowed": true
      }
    }
  ],
  "codex-free": [
    {
      "id": "gpt-5",
      "owned_by": "openai",
      "type": "codex",
      "display_name": "GPT-5",
      "max_completion_tokens": 32768
    }
  ],
  "codex-pro": [
    {
      "id": "gpt-5",
      "owned_by": "openai",
      "type": "codex",
      "display_name": "GPT-5",
      "max_completion_tokens": 32768
    },
    {
      "id": "gpt-5.4",
      "owned_by": "openai",
      "type": "codex",
      "display_name": "GPT-5.4",
      "max_completion_tokens": 32768,
      "thinking": {
        "levels": ["none", "low", "medium", "high"]
      }
    }
  ]
}`

	models := parseAndEnrichModels(source, staticModelsJSON, nil)

	if got := len(models["claude"]); got != 1 {
		t.Fatalf("expected 1 claude model, got %d", got)
	}

	if got := len(models["codex"]); got != 2 {
		t.Fatalf("expected 2 deduplicated codex models, got %d", got)
	}

	config := generateFactoryConfig(models)

	var anthropicCount, openaiCount int
	for _, model := range config.CustomModels {
		switch model.Provider {
		case "anthropic":
			anthropicCount++
		case "openai":
			openaiCount++
		}
	}

	if anthropicCount == 0 {
		t.Fatal("expected generated Factory config to include anthropic models")
	}
	if openaiCount == 0 {
		t.Fatal("expected generated Factory config to include openai models")
	}
}

func TestGenerateFactoryConfig_ClaudeAdaptiveAndManualVariants(t *testing.T) {
	models := map[string][]Model{
		"claude": {
			{
				ID:                  "claude-opus-4-7",
				Provider:            "claude",
				DisplayName:         "Claude Opus 4.7",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh", "max"},
				},
			},
			{
				ID:                  "claude-opus-4-5-20251101",
				Provider:            "claude",
				DisplayName:         "Claude 4.5 Opus",
				MaxCompletionTokens: 64000,
				Thinking: &Thinking{
					Supported:   true,
					Min:         1024,
					Max:         128000,
					ZeroAllowed: true,
				},
			},
		},
	}

	config := generateFactoryConfig(models)

	var foundAdaptiveAuto, foundAdaptiveXHigh, foundManualHigh, foundLegacyBudget bool
	for _, model := range config.CustomModels {
		switch model.Model {
		case "claude-opus-4-7(auto)":
			foundAdaptiveAuto = true
		case "claude-opus-4-7(xhigh)":
			foundAdaptiveXHigh = true
		case "claude-opus-4-5-20251101(high)":
			foundManualHigh = true
		case "claude-opus-4-5-20251101-thinking-10000":
			foundLegacyBudget = true
		}
	}

	if !foundAdaptiveAuto {
		t.Fatal("expected Claude Opus 4.7 auto adaptive Factory variant")
	}
	if !foundAdaptiveXHigh {
		t.Fatal("expected Claude Opus 4.7 xhigh adaptive Factory variant")
	}
	if !foundManualHigh {
		t.Fatal("expected Claude Opus 4.5 high Factory variant")
	}
	if !foundLegacyBudget {
		t.Fatal("expected Claude Opus 4.5 legacy budget Factory variant")
	}
}

func TestGenerateOpenCodeConfig_ClaudeVariantsMatchThinkingMode(t *testing.T) {
	models := map[string][]Model{
		"claude": {
			{
				ID:                  "claude-opus-4-7",
				Provider:            "claude",
				DisplayName:         "Claude Opus 4.7",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh", "max"},
				},
			},
			{
				ID:                  "claude-opus-4-5-20251101",
				Provider:            "claude",
				DisplayName:         "Claude 4.5 Opus",
				MaxCompletionTokens: 64000,
				Thinking: &Thinking{
					Supported:   true,
					Min:         1024,
					Max:         128000,
					ZeroAllowed: true,
				},
			},
		},
	}

	config := generateOpenCodeConfig(models)
	claudeProvider := config.Provider["ai-proxy-claude"]
	if claudeProvider == nil {
		t.Fatal("expected anthropic provider in OpenCode config")
	}

	opus47 := claudeProvider.Models["claude-opus-4-7"]
	if opus47 == nil {
		t.Fatal("expected Claude Opus 4.7 model")
	}
	if opus47.Variants["auto"] == nil || opus47.Variants["auto"].Thinking == nil || opus47.Variants["auto"].Thinking.Type != "adaptive" {
		t.Fatalf("expected Claude Opus 4.7 auto variant to use adaptive thinking, got %#v", opus47.Variants["auto"])
	}
	if opus47.Variants["xhigh"] == nil || opus47.Variants["xhigh"].ReasoningEffort != "xhigh" {
		t.Fatalf("expected Claude Opus 4.7 xhigh effort variant, got %#v", opus47.Variants["xhigh"])
	}

	opus45 := claudeProvider.Models["claude-opus-4-5-20251101"]
	if opus45 == nil {
		t.Fatal("expected Claude Opus 4.5 model")
	}
	if opus45.Variants["high"] == nil || opus45.Variants["high"].Thinking == nil {
		t.Fatalf("expected Claude Opus 4.5 high variant, got %#v", opus45.Variants["high"])
	}
	if got := opus45.Variants["high"].Thinking.Type; got != "enabled" {
		t.Fatalf("expected Claude Opus 4.5 high variant to use manual thinking, got %q", got)
	}
	if got := opus45.Variants["high"].Thinking.BudgetTokens; got != 24576 {
		t.Fatalf("expected Claude Opus 4.5 high variant to map to 24576 budget tokens, got %d", got)
	}
	if got := opus45.Variants["high"].ReasoningEffort; got != "high" {
		t.Fatalf("expected Claude Opus 4.5 high variant to keep high effort, got %q", got)
	}
}
