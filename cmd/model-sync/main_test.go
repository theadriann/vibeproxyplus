package main

import (
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
