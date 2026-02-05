package main

import "testing"

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

