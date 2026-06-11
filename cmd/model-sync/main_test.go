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
			want:     []string{"router-for-me/CLIProxyAPI", "cli-proxy-api", "darwin_aarch64", "linux_aarch64", "curl -fL"},
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

func TestParseAndEnrichModels_IncludesClaudeFable5Supplement(t *testing.T) {
	models := parseAndEnrichModels("", "", nil)
	claudeModels := models["claude"]

	var fable *Model
	for i := range claudeModels {
		if claudeModels[i].ID == "claude-fable-5" {
			fable = &claudeModels[i]
			break
		}
	}

	if fable == nil {
		t.Fatal("expected supplemental claude-fable-5 model")
	}
	if fable.DisplayName != "Claude Fable 5" {
		t.Fatalf("display name = %q, want %q", fable.DisplayName, "Claude Fable 5")
	}
	if fable.ContextLength != 1000000 {
		t.Fatalf("context length = %d, want 1000000", fable.ContextLength)
	}
	if fable.MaxCompletionTokens != 128000 {
		t.Fatalf("max completion tokens = %d, want 128000", fable.MaxCompletionTokens)
	}
	if fable.Thinking == nil || !fable.Thinking.Supported {
		t.Fatalf("expected adaptive thinking support, got %#v", fable.Thinking)
	}
	if got := strings.Join(fable.Thinking.Levels, ","); got != "low,medium,high,xhigh,max" {
		t.Fatalf("thinking levels = %q, want low,medium,high,xhigh,max", got)
	}
	if fable.Cost == nil || fable.Cost.Input != 10 || fable.Cost.Output != 50 {
		t.Fatalf("cost = %#v, want input 10 output 50", fable.Cost)
	}
}

func TestParseAndEnrichModels_IncludesClaudeMythos5Supplement(t *testing.T) {
	models := parseAndEnrichModels("", "", nil)
	claudeModels := models["claude"]

	var mythos *Model
	for i := range claudeModels {
		if claudeModels[i].ID == "claude-mythos-5" {
			mythos = &claudeModels[i]
			break
		}
	}

	if mythos == nil {
		t.Fatal("expected supplemental claude-mythos-5 model")
	}
	if mythos.DisplayName != "Claude Mythos 5" {
		t.Fatalf("display name = %q, want %q", mythos.DisplayName, "Claude Mythos 5")
	}
	if mythos.ContextLength != 1000000 {
		t.Fatalf("context length = %d, want 1000000", mythos.ContextLength)
	}
	if mythos.MaxCompletionTokens != 128000 {
		t.Fatalf("max completion tokens = %d, want 128000", mythos.MaxCompletionTokens)
	}
	if mythos.Thinking == nil || !mythos.Thinking.Supported {
		t.Fatalf("expected adaptive thinking support, got %#v", mythos.Thinking)
	}
	if got := strings.Join(mythos.Thinking.Levels, ","); got != "low,medium,high,xhigh,max" {
		t.Fatalf("thinking levels = %q, want low,medium,high,xhigh,max", got)
	}
	if mythos.Cost == nil || mythos.Cost.Input != 10 || mythos.Cost.Output != 50 {
		t.Fatalf("cost = %#v, want input 10 output 50", mythos.Cost)
	}
}

func TestMakeAuthCopilotDoesNotUseRemovedCLIProxyFlag(t *testing.T) {
	data, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	removedCommand := "./bin/cli-proxy-api -config config/cliproxy.yaml -github-copilot-login"
	if strings.Contains(string(data), removedCommand) {
		t.Fatal("Makefile auth-copilot executes removed CLIProxyAPI -github-copilot-login flag")
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

func TestBuildCodexClientMetadataIndex(t *testing.T) {
	source := `{
  "models": [
    {
      "slug": "gpt-5.5",
      "support_verbosity": true,
      "default_verbosity": "low",
      "default_reasoning_summary": "none",
      "supports_reasoning_summaries": true,
      "service_tiers": [{"id": "priority", "name": "Fast"}],
      "additional_speed_tiers": ["fast"],
      "supported_reasoning_levels": [{"effort": "low"}, {"effort": "medium"}, {"effort": "high"}]
    },
    {
      "slug": "gpt-5.3-codex",
      "support_verbosity": false,
      "service_tiers": [],
      "supported_reasoning_levels": [{"effort": "low"}]
    }
  ]
}`

	index, err := buildCodexClientMetadataIndex(source)
	if err != nil {
		t.Fatalf("unexpected metadata parse error: %v", err)
	}

	gpt55 := index["gpt-5.5"]
	if gpt55 == nil {
		t.Fatal("missing gpt-5.5 metadata")
	}
	if !gpt55.SupportsPriorityServiceTier {
		t.Fatal("expected gpt-5.5 to support priority service tier")
	}
	if !gpt55.SupportsVerbosity || gpt55.DefaultVerbosity != "low" {
		t.Fatalf("unexpected verbosity metadata: %#v", gpt55)
	}
	if !gpt55.SupportsReasoningSummaries || gpt55.DefaultReasoningSummary != "none" {
		t.Fatalf("unexpected reasoning summary metadata: %#v", gpt55)
	}
	if got := strings.Join(gpt55.ReasoningLevels, ","); got != "low,medium,high" {
		t.Fatalf("reasoning levels = %q, want %q", got, "low,medium,high")
	}

	codex53 := index["gpt-5.3-codex"]
	if codex53 == nil {
		t.Fatal("missing gpt-5.3-codex metadata")
	}
	if codex53.SupportsPriorityServiceTier {
		t.Fatal("did not expect gpt-5.3-codex to support priority service tier")
	}
	if codex53.SupportsVerbosity {
		t.Fatal("did not expect gpt-5.3-codex to support verbosity")
	}
}

func TestBuildCodexClientMetadataIndex_ReturnsErrorForInvalidJSON(t *testing.T) {
	index, err := buildCodexClientMetadataIndex(`{"models":`)
	if err == nil {
		t.Fatal("expected invalid Codex client metadata JSON to return an error")
	}
	if index != nil {
		t.Fatalf("expected nil index on invalid JSON, got %#v", index)
	}
}

func TestCodexDefaultReasoningSummaryOmitsNone(t *testing.T) {
	metadata := &CodexClientMetadata{
		SupportsReasoningSummaries: true,
		DefaultReasoningSummary:    "none",
	}

	if got := codexDefaultReasoningSummary(metadata); got != "" {
		t.Fatalf("reasoning summary = %q, want empty string", got)
	}
}

func TestGenerateOpenCodeConfig_OmitsUnsupportedNoneReasoningSummary(t *testing.T) {
	models := map[string][]Model{
		"codex": {
			{
				ID:          "gpt-5.5",
				Provider:    "codex",
				DisplayName: "GPT 5.5",
				Type:        "openai",
				OwnedBy:     "openai",
				Thinking:    &Thinking{Supported: true, Levels: []string{"high"}},
				Modalities:  &Modalities{Input: []string{"text"}, Output: []string{"text"}},
			},
		},
	}
	metadata := CodexClientMetadataIndex{
		"gpt-5.5": {
			SupportsVerbosity:          true,
			DefaultVerbosity:           "low",
			SupportsReasoningSummaries: true,
			DefaultReasoningSummary:    "none",
			ReasoningLevels:            []string{"high"},
		},
	}

	config := generateOpenCodeConfig(models, metadata)
	variant := config.Provider["ai-proxy-openai"].Models["gpt-5.5"].Variants["high"]
	if variant == nil {
		t.Fatal("missing high variant")
	}
	if variant.ReasoningSummary != "" {
		t.Fatalf("reasoning summary = %q, want omitted empty value", variant.ReasoningSummary)
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

	config := generateFactoryConfig(models, nil)
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

	if got := len(models["claude"]); got != 3 {
		t.Fatalf("expected 3 claude models including supplemental Claude Fable 5 and Claude Mythos 5, got %d", got)
	}

	if got := len(models["codex"]); got != 2 {
		t.Fatalf("expected 2 deduplicated codex models, got %d", got)
	}

	config := generateFactoryConfig(models, nil)

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
				ID:                  "claude-opus-4-8",
				Provider:            "claude",
				DisplayName:         "Claude Opus 4.8",
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

	config := generateFactoryConfig(models, nil)

	var foundAdaptiveAuto, foundAdaptiveXHigh, foundOpus48Auto, foundOpus48LegacyBudget, foundManualHigh, foundLegacyBudget bool
	for _, model := range config.CustomModels {
		switch model.Model {
		case "claude-opus-4-7(auto)":
			foundAdaptiveAuto = true
		case "claude-opus-4-7(xhigh)":
			foundAdaptiveXHigh = true
		case "claude-opus-4-8(auto)":
			foundOpus48Auto = true
		case "claude-opus-4-8-thinking-10000":
			foundOpus48LegacyBudget = true
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
	if !foundOpus48Auto {
		t.Fatal("expected Claude Opus 4.8 auto adaptive Factory variant")
	}
	if foundOpus48LegacyBudget {
		t.Fatal("did not expect Claude Opus 4.8 legacy budget Factory variant")
	}
	if !foundManualHigh {
		t.Fatal("expected Claude Opus 4.5 high Factory variant")
	}
	if !foundLegacyBudget {
		t.Fatal("expected Claude Opus 4.5 legacy budget Factory variant")
	}
}

func TestGenerateFactoryConfig_CodexFastVariants(t *testing.T) {
	models := map[string][]Model{
		"codex": {
			{
				ID:                  "gpt-5.5",
				Provider:            "codex",
				DisplayName:         "GPT 5.5",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh"},
				},
			},
			{
				ID:                  "gpt-5.3-codex",
				Provider:            "codex",
				DisplayName:         "GPT 5.3 Codex",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh"},
				},
			},
		},
	}

	metadata := CodexClientMetadataIndex{
		"gpt-5.5": {
			SupportsPriorityServiceTier: true,
			SupportsVerbosity:           true,
			DefaultVerbosity:            "low",
		},
		"gpt-5.3-codex": {
			SupportsPriorityServiceTier: false,
			SupportsVerbosity:           false,
		},
	}

	config := generateFactoryConfig(models, metadata)

	wantModels := map[string]bool{
		"gpt-5.5(fast)":         false,
		"gpt-5.5(low-fast)":     false,
		"gpt-5.5(medium-fast)":  false,
		"gpt-5.5(high-fast)":    false,
		"gpt-5.5(xhigh-fast)":   false,
		"gpt-5.5(verbose)":      false,
		"gpt-5.5(high-verbose)": false,
	}
	unwantedModels := map[string]bool{
		"gpt-5.3-codex(fast)":    true,
		"gpt-5.3-codex(verbose)": true,
	}
	for _, model := range config.CustomModels {
		if _, ok := wantModels[model.Model]; ok {
			wantModels[model.Model] = true
		}
		if _, ok := unwantedModels[model.Model]; ok {
			t.Fatalf("did not expect Factory config to include %q", model.Model)
		}
	}
	for model, found := range wantModels {
		if !found {
			t.Fatalf("expected Factory config to include %q", model)
		}
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

	config := generateOpenCodeConfig(models, nil)
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

func TestGenerateOpenCodeConfig_CodexFastAliasModel(t *testing.T) {
	models := map[string][]Model{
		"codex": {
			{
				ID:                  "gpt-5.5",
				Provider:            "codex",
				DisplayName:         "GPT 5.5",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh"},
				},
			},
			{
				ID:                  "gpt-5.3-codex",
				Provider:            "codex",
				DisplayName:         "GPT 5.3 Codex",
				MaxCompletionTokens: 128000,
				Thinking: &Thinking{
					Supported: true,
					Levels:    []string{"low", "medium", "high", "xhigh"},
				},
			},
		},
	}

	metadata := CodexClientMetadataIndex{
		"gpt-5.5": {
			SupportsPriorityServiceTier: true,
			SupportsVerbosity:           true,
			DefaultVerbosity:            "low",
			SupportsReasoningSummaries:  true,
			DefaultReasoningSummary:     "none",
		},
		"gpt-5.3-codex": {
			SupportsPriorityServiceTier: false,
			SupportsVerbosity:           false,
			SupportsReasoningSummaries:  true,
			DefaultReasoningSummary:     "auto",
		},
	}

	config := generateOpenCodeConfig(models, metadata)
	openaiProvider := config.Provider["ai-proxy-openai"]
	if openaiProvider == nil {
		t.Fatal("expected OpenAI provider in OpenCode config")
	}

	fastModel := openaiProvider.Models["gpt-5.5(fast)"]
	if fastModel == nil {
		t.Fatal("expected OpenCode config to include gpt-5.5(fast)")
	}
	if fastModel.Name != "GPT 5.5 (Fast)" {
		t.Fatalf("fast model name = %q, want %q", fastModel.Name, "GPT 5.5 (Fast)")
	}
	if fastModel.Variants["high"] == nil || fastModel.Variants["high"].ReasoningEffort != "high" || fastModel.Variants["high"].ReasoningSummary != "" {
		t.Fatalf("expected fast OpenCode model to keep high reasoning variant, got %#v", fastModel.Variants["high"])
	}
	if fastModel.Variants["verbose"] == nil || fastModel.Variants["verbose"].TextVerbosity != "high" {
		t.Fatalf("expected fast OpenCode model to include verbose variant, got %#v", fastModel.Variants["verbose"])
	}
	if openaiProvider.Models["gpt-5.3-codex(fast)"] != nil {
		t.Fatal("did not expect OpenCode config to include gpt-5.3-codex(fast)")
	}
}
