package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	modelDefsURL       = "https://raw.githubusercontent.com/router-for-me/CLIProxyAPIPlus/main/internal/registry/model_definitions.go"
	modelDefsStaticURL = "https://raw.githubusercontent.com/router-for-me/CLIProxyAPIPlus/main/internal/registry/model_definitions_static_data.go"
	modelsDevURL       = "https://models.dev/api.json"
)

// Canonical model with merged metadata
type Model struct {
	ID                  string      `json:"id"`
	Provider            string      `json:"provider"`
	DisplayName         string      `json:"display_name"`
	Description         string      `json:"description,omitempty"`
	Family              string      `json:"family,omitempty"`
	Type                string      `json:"type"`
	OwnedBy             string      `json:"owned_by"`
	ContextLength       int         `json:"context_length,omitempty"`
	MaxCompletionTokens int         `json:"max_completion_tokens,omitempty"`
	Thinking            *Thinking   `json:"thinking,omitempty"`
	Modalities          *Modalities `json:"modalities,omitempty"`
	Capabilities        *Capabilities `json:"capabilities,omitempty"`
	Cost                *Cost       `json:"cost,omitempty"`
}

type Thinking struct {
	Supported    bool     `json:"supported"`
	Min          int      `json:"min,omitempty"`
	Max          int      `json:"max,omitempty"`
	ZeroAllowed  bool     `json:"zero_allowed,omitempty"`
	Levels       []string `json:"levels,omitempty"`
}

type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type Capabilities struct {
	Reasoning        bool `json:"reasoning,omitempty"`
	ToolCall         bool `json:"tool_call,omitempty"`
	StructuredOutput bool `json:"structured_output,omitempty"`
	Attachment       bool `json:"attachment,omitempty"`
	Temperature      bool `json:"temperature,omitempty"`
}

type Cost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type CanonicalConfig struct {
	Version   string             `json:"version"`
	Sources   []string           `json:"sources"`
	Models    map[string][]Model `json:"models"`
}

// models.dev types
type ModelsDevAPI map[string]*ModelsDevProvider

type ModelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models map[string]*ModelsDevModel `json:"models"`
}

type ModelsDevModel struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Family           string                 `json:"family"`
	Attachment       bool                   `json:"attachment"`
	Reasoning        bool                   `json:"reasoning"`
	ToolCall         bool                   `json:"tool_call"`
	StructuredOutput bool                   `json:"structured_output"`
	Temperature      bool                   `json:"temperature"`
	Modalities       *Modalities            `json:"modalities"`
	Cost             map[string]float64     `json:"cost"`
	Limit            map[string]int         `json:"limit"`
}

// Factory config types (settings.json format - camelCase)
type FactoryModel struct {
	Model           string                 `json:"model"`
	DisplayName     string                 `json:"displayName,omitempty"`
	BaseURL         string                 `json:"baseUrl"`
	APIKey          string                 `json:"apiKey"`
	Provider        string                 `json:"provider"`
	MaxOutputTokens int                    `json:"maxOutputTokens,omitempty"`
	SupportsImages  bool                   `json:"supportsImages,omitempty"`
	ExtraArgs       map[string]interface{} `json:"extraArgs,omitempty"`
	ExtraHeaders    map[string]string      `json:"extraHeaders,omitempty"`
}

type FactoryConfig struct {
	CustomModels []FactoryModel `json:"customModels"`
}

func main() {
	outputFile := flag.String("output", "models.json", "Output file for canonical config")
	factoryFile := flag.String("factory", "", "Generate Factory CLI config file")
	opencodeFile := flag.String("opencode", "", "Generate OpenCode CLI config file")
	localModelDefs := flag.String("local-modeldefs", "", "Use local model_definitions.go")
	localModelsDev := flag.String("local-modelsdev", "", "Use local models.dev api.json")
	flag.Parse()

	// Download/load CLIProxyAPIPlus model definitions (both files)
	var modelDefsSource string
	if *localModelDefs != "" {
		data, err := os.ReadFile(*localModelDefs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading local model_definitions.go: %v\n", err)
			os.Exit(1)
		}
		modelDefsSource = string(data)
		fmt.Printf("Using local model_definitions.go: %s\n", *localModelDefs)
	} else {
		fmt.Printf("Downloading CLIProxyAPIPlus model definitions...\n")
		
		// Download main model_definitions.go
		resp, err := http.Get(modelDefsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading model_definitions.go: %v\n", err)
			os.Exit(1)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		modelDefsSource = string(data)
		
		// Download model_definitions_static_data.go (contains Claude, OpenAI, Gemini, etc.)
		fmt.Printf("Downloading CLIProxyAPIPlus static model definitions...\n")
		resp2, err := http.Get(modelDefsStaticURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading model_definitions_static_data.go: %v\n", err)
			os.Exit(1)
		}
		data2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		modelDefsSource += "\n" + string(data2)
	}

	// Download/load models.dev API
	var modelsDevData ModelsDevAPI
	if *localModelsDev != "" {
		data, err := os.ReadFile(*localModelsDev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading local models.dev api.json: %v\n", err)
			os.Exit(1)
		}
		json.Unmarshal(data, &modelsDevData)
		fmt.Printf("Using local models.dev api.json: %s\n", *localModelsDev)
	} else {
		fmt.Printf("Downloading models.dev API...\n")
		resp, err := http.Get(modelsDevURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading models.dev API: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		json.Unmarshal(data, &modelsDevData)
	}

	// Build models.dev lookup index
	modelsDevIndex := buildModelsDevIndex(modelsDevData)
	fmt.Printf("Indexed %d models from models.dev\n", len(modelsDevIndex))

	// Parse CLIProxyAPIPlus models and enrich with models.dev
	models := parseAndEnrichModels(modelDefsSource, modelsDevIndex)

	config := CanonicalConfig{
		Version: "2.0",
		Sources: []string{modelDefsURL, modelsDevURL},
		Models:  models,
	}

	// Write canonical config
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(*outputFile, data, 0644)
	fmt.Printf("Written canonical config to: %s\n", *outputFile)

	// Print summary
	total := 0
	for provider, providerModels := range models {
		fmt.Printf("  %s: %d models\n", provider, len(providerModels))
		total += len(providerModels)
	}
	fmt.Printf("  Total: %d models\n", total)

	// Generate Factory config
	if *factoryFile != "" {
		factoryConfig := generateFactoryConfig(models)
		data, _ := json.MarshalIndent(factoryConfig, "", "  ")
		os.WriteFile(*factoryFile, data, 0644)
		fmt.Printf("Written Factory config to: %s (%d models)\n", *factoryFile, len(factoryConfig.CustomModels))
	}

	// Generate OpenCode config
	if *opencodeFile != "" {
		opencodeConfig := generateOpenCodeConfig(models)
		data, _ := json.MarshalIndent(opencodeConfig, "", "  ")
		os.WriteFile(*opencodeFile, data, 0644)
		fmt.Printf("Written OpenCode config to: %s\n", *opencodeFile)
	}
}

// buildModelsDevIndex creates a lookup map by model ID across all providers
func buildModelsDevIndex(api ModelsDevAPI) map[string]*ModelsDevModel {
	index := make(map[string]*ModelsDevModel)
	
	for _, provider := range api {
		if provider.Models == nil {
			continue
		}
		for modelID, model := range provider.Models {
			// Index by exact ID
			index[modelID] = model
			// Also index by normalized ID (lowercase, no version suffix)
			normalized := normalizeModelID(modelID)
			if _, exists := index[normalized]; !exists {
				index[normalized] = model
			}
		}
	}
	return index
}

func normalizeModelID(id string) string {
	// Remove date suffixes like -20250929
	re := regexp.MustCompile(`-\d{8}$`)
	normalized := re.ReplaceAllString(id, "")
	return strings.ToLower(normalized)
}

func parseAndEnrichModels(source string, modelsDevIndex map[string]*ModelsDevModel) map[string][]Model {
	models := make(map[string][]Model)

	parsers := []struct {
		funcName string
		provider string
	}{
		{"GetClaudeModels", "claude"},
		{"GetOpenAIModels", "codex"},
		{"GetGeminiModels", "gemini"},
		{"GetGeminiCLIModels", "gemini-cli"},
		{"GetGeminiVertexModels", "vertex"},
		{"GetAIStudioModels", "aistudio"},
		{"GetQwenModels", "qwen"},
		{"GetIFlowModels", "iflow"},
		{"GetGitHubCopilotModels", "github-copilot"},
		{"GetKiroModels", "kiro"},
		{"GetAmazonQModels", "amazonq"},
	}

	for _, p := range parsers {
		funcModels := parseFunctionModels(source, p.funcName, p.provider, modelsDevIndex)
		if len(funcModels) > 0 {
			models[p.provider] = funcModels
		}
	}

	// Parse Antigravity
	antigravityModels := parseAntigravityModels(source, modelsDevIndex)
	if len(antigravityModels) > 0 {
		models["antigravity"] = antigravityModels
	}

	return models
}

func parseFunctionModels(source, funcName, provider string, modelsDevIndex map[string]*ModelsDevModel) []Model {
	var models []Model

	funcPattern := regexp.MustCompile(`func\s+` + funcName + `\s*\(\)\s*\[\]\*ModelInfo\s*\{([\s\S]*?)\nfunc\s`)
	funcMatch := funcPattern.FindStringSubmatch(source)
	if funcMatch == nil {
		funcPattern = regexp.MustCompile(`func\s+` + funcName + `\s*\(\)\s*\[\]\*ModelInfo\s*\{([\s\S]*)$`)
		funcMatch = funcPattern.FindStringSubmatch(source)
		if funcMatch == nil {
			return models
		}
	}
	funcBody := funcMatch[1]

	idPattern := regexp.MustCompile(`ID:\s*"([^"]+)"`)
	idMatches := idPattern.FindAllStringSubmatchIndex(funcBody, -1)

	for _, idxMatch := range idMatches {
		id := funcBody[idxMatch[2]:idxMatch[3]]

		start := idxMatch[0]
		for start > 0 && funcBody[start] != '{' {
			start--
		}

		end := idxMatch[1]
		braceCount := 1
		for end < len(funcBody) && braceCount > 0 {
			if funcBody[end] == '{' {
				braceCount++
			} else if funcBody[end] == '}' {
				braceCount--
			}
			end++
		}

		if start >= end || start < 0 {
			continue
		}

		block := funcBody[start:end]

		model := Model{
			ID:       id,
			Provider: provider,
			Type:     extractField(block, "Type"),
			OwnedBy:  extractField(block, "OwnedBy"),
		}

		displayName := extractField(block, "DisplayName")
		if displayName != "" {
			model.DisplayName = displayName
		} else {
			model.DisplayName = formatDisplayName(id)
		}

		model.Description = extractField(block, "Description")
		model.ContextLength = extractIntField(block, "ContextLength")
		if model.ContextLength == 0 {
			model.ContextLength = extractIntField(block, "InputTokenLimit")
		}
		model.MaxCompletionTokens = extractIntField(block, "MaxCompletionTokens")
		if model.MaxCompletionTokens == 0 {
			model.MaxCompletionTokens = extractIntField(block, "OutputTokenLimit")
		}

		// Parse thinking support from CLIProxyAPIPlus
		if strings.Contains(block, "Thinking:") && !strings.Contains(block, "// Thinking: not supported") {
			model.Thinking = &Thinking{Supported: true}
			if min := extractIntField(block, "Min"); min > 0 {
				model.Thinking.Min = min
			}
			if max := extractIntField(block, "Max"); max > 0 {
				model.Thinking.Max = max
			}
			if strings.Contains(block, "ZeroAllowed: true") {
				model.Thinking.ZeroAllowed = true
			}
			levelsPattern := regexp.MustCompile(`Levels:\s*\[\]string\{([^}]+)\}`)
			if levelsMatch := levelsPattern.FindStringSubmatch(block); levelsMatch != nil {
				levelStrings := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(levelsMatch[1], -1)
				for _, l := range levelStrings {
					model.Thinking.Levels = append(model.Thinking.Levels, l[1])
				}
			}
		}

		// Enrich with models.dev data
		enrichFromModelsDev(&model, modelsDevIndex)

		models = append(models, model)
	}

	return models
}

func enrichFromModelsDev(model *Model, index map[string]*ModelsDevModel) {
	// Try exact match first
	mdModel := index[model.ID]
	
	// Try normalized match
	if mdModel == nil {
		mdModel = index[normalizeModelID(model.ID)]
	}
	
	// Try partial matches for common patterns
	if mdModel == nil {
		// claude-opus-4-5-20251101 -> claude-opus-4-5
		parts := strings.Split(model.ID, "-")
		if len(parts) > 2 {
			for i := len(parts) - 1; i >= 2; i-- {
				partial := strings.Join(parts[:i], "-")
				if m := index[partial]; m != nil {
					mdModel = m
					break
				}
			}
		}
	}

	if mdModel == nil {
		// Set defaults
		model.Modalities = inferModalities(model.ID, model.Type)
		model.Capabilities = &Capabilities{
			ToolCall:    true,
			Temperature: true,
		}
		return
	}

	// Enrich from models.dev
	if model.DisplayName == "" || model.DisplayName == model.ID {
		model.DisplayName = mdModel.Name
	}
	model.Family = mdModel.Family

	if mdModel.Modalities != nil {
		model.Modalities = mdModel.Modalities
	} else {
		model.Modalities = inferModalities(model.ID, model.Type)
	}

	model.Capabilities = &Capabilities{
		Reasoning:        mdModel.Reasoning,
		ToolCall:         mdModel.ToolCall,
		StructuredOutput: mdModel.StructuredOutput,
		Attachment:       mdModel.Attachment,
		Temperature:      mdModel.Temperature,
	}

	if mdModel.Limit != nil {
		if ctx, ok := mdModel.Limit["context"]; ok && model.ContextLength == 0 {
			model.ContextLength = ctx
		}
		if out, ok := mdModel.Limit["output"]; ok && model.MaxCompletionTokens == 0 {
			model.MaxCompletionTokens = out
		}
	}

	if mdModel.Cost != nil {
		model.Cost = &Cost{
			Input:      mdModel.Cost["input"],
			Output:     mdModel.Cost["output"],
			CacheRead:  mdModel.Cost["cache_read"],
			CacheWrite: mdModel.Cost["cache_write"],
		}
	}
}

func parseAntigravityModels(source string, modelsDevIndex map[string]*ModelsDevModel) []Model {
	var models []Model

	funcPattern := regexp.MustCompile(`func\s+GetAntigravityModelConfig\s*\(\)\s*map\[string\]\*AntigravityModelConfig\s*\{([\s\S]*?)\n\}`)
	funcMatch := funcPattern.FindStringSubmatch(source)
	if funcMatch == nil {
		return models
	}

	modelPattern := regexp.MustCompile(`"([^"]+)":\s*\{`)
	matches := modelPattern.FindAllStringSubmatch(funcMatch[1], -1)

	for _, match := range matches {
		id := match[1]
		model := Model{
			ID:          id,
			Provider:    "antigravity",
			DisplayName: formatDisplayName(id),
			Type:        "antigravity",
			OwnedBy:     "antigravity",
		}

		blockPattern := regexp.MustCompile(`"` + regexp.QuoteMeta(id) + `":\s*\{([^}]+)\}`)
		if blockMatch := blockPattern.FindStringSubmatch(funcMatch[1]); blockMatch != nil {
			if strings.Contains(blockMatch[1], "Thinking:") {
				model.Thinking = &Thinking{Supported: true}
			}
		}

		enrichFromModelsDev(&model, modelsDevIndex)
		models = append(models, model)
	}

	return models
}

func extractField(block, field string) string {
	pattern := regexp.MustCompile(field + `:\s*"([^"]*)"`)
	if match := pattern.FindStringSubmatch(block); match != nil {
		return match[1]
	}
	return ""
}

func extractIntField(block, field string) int {
	pattern := regexp.MustCompile(field + `:\s*(\d+)`)
	if match := pattern.FindStringSubmatch(block); match != nil {
		var val int
		fmt.Sscanf(match[1], "%d", &val)
		return val
	}
	return 0
}

func formatDisplayName(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func inferModalities(modelID, modelType string) *Modalities {
	m := &Modalities{
		Input:  []string{"text"},
		Output: []string{"text"},
	}

	if strings.Contains(modelID, "vision") || strings.Contains(modelID, "-vl-") ||
		strings.Contains(modelID, "image") || strings.Contains(modelID, "gemini") ||
		strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "gpt-4") ||
		strings.Contains(modelID, "claude") {
		m.Input = []string{"text", "image"}
	}

	if strings.Contains(modelID, "imagen") || strings.Contains(modelID, "image-generate") {
		m.Output = []string{"image"}
	}

	return m
}

func generateFactoryConfig(models map[string][]Model) FactoryConfig {
	var factoryModels []FactoryModel

	// Provider config: provider value must be "anthropic", "openai", or "generic-chat-completion-api"
	// - anthropic: for Anthropic Messages API (Claude via direct anthropic endpoint)
	// - openai: for OpenAI Responses API (GPT-5, Codex - newest models)
	// - generic-chat-completion-api: for OpenAI Chat Completions API (most other providers)
	providerConfig := map[string]struct {
		baseURL  string
		provider string
		include  bool
	}{
		"claude":         {baseURL: "http://localhost:8317", provider: "anthropic", include: true},
		"codex":          {baseURL: "http://localhost:8317/v1", provider: "openai", include: true},
		"gemini":         {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: true},
		"gemini-cli":     {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: false},
		"antigravity":    {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: true},
		"qwen":           {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: true},
		"github-copilot": {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: true},
		"kiro":           {baseURL: "http://localhost:8317/v1", provider: "generic-chat-completion-api", include: true},
	}

	// Human-readable prefixes for display names
	displayPrefixes := map[string]string{
		"claude":         "Claude",
		"codex":          "OpenAI",
		"gemini":         "Gemini",
		"gemini-cli":     "Gemini",
		"antigravity":    "AG",
		"qwen":           "Qwen",
		"github-copilot": "Copilot",
		"kiro":           "Kiro",
	}

	for providerKey, providerModels := range models {
		cfg, ok := providerConfig[providerKey]
		if !ok || !cfg.include {
			continue
		}

		prefix := displayPrefixes[providerKey]
		if prefix == "" {
			prefix = providerKey
		}

		for _, m := range providerModels {

			// Check if model supports images
			supportsImages := false
			if m.Modalities != nil {
				for _, mod := range m.Modalities.Input {
					if mod == "image" {
						supportsImages = true
						break
					}
				}
			}

			fm := FactoryModel{
				Model:           m.ID,
				DisplayName:     fmt.Sprintf("[%s] %s", prefix, m.DisplayName),
				BaseURL:         cfg.baseURL,
				APIKey:          "dummy",
				Provider:        cfg.provider,
				MaxOutputTokens: m.MaxCompletionTokens,
				SupportsImages:  supportsImages,
			}
			factoryModels = append(factoryModels, fm)

			// Add thinking variants for Claude models
			if m.Provider == "claude" && m.Thinking != nil && m.Thinking.Supported {
				for _, budget := range []int{4000, 10000, 32000} {
					fm := FactoryModel{
						Model:           fmt.Sprintf("%s-thinking-%d", m.ID, budget),
						DisplayName:     fmt.Sprintf("[%s] %s (Thinking %dk)", prefix, m.DisplayName, budget/1000),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					}
					factoryModels = append(factoryModels, fm)
				}
			}

			// Add reasoning effort variants for Codex/OpenAI models with thinking levels
			if m.Provider == "codex" && m.Thinking != nil && len(m.Thinking.Levels) > 0 {
				for _, level := range m.Thinking.Levels {
					// Skip "none" level as it's the default/base model
					if level == "none" {
						continue
					}
					fm := FactoryModel{
						Model:           fmt.Sprintf("%s(%s)", m.ID, level),
						DisplayName:     fmt.Sprintf("[%s] %s (%s)", prefix, m.DisplayName, strings.Title(level)),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					}
					factoryModels = append(factoryModels, fm)
				}
			}
		}
	}

	sort.Slice(factoryModels, func(i, j int) bool {
		return factoryModels[i].DisplayName < factoryModels[j].DisplayName
	})

	return FactoryConfig{CustomModels: factoryModels}
}

// OpenCode config types
type OpenCodeConfig struct {
	Schema   string                       `json:"$schema"`
	Provider map[string]*OpenCodeProvider `json:"provider"`
}

type OpenCodeProvider struct {
	Name    string                    `json:"name,omitempty"`
	Type    string                    `json:"type,omitempty"`
	BaseURL string                    `json:"baseURL,omitempty"`
	APIKey  string                    `json:"apiKey,omitempty"`
	Models  map[string]*OpenCodeModel `json:"models"`
}

type OpenCodeModel struct {
	Name       string                      `json:"name,omitempty"`
	Options    *OpenCodeOptions            `json:"options,omitempty"`
	Variants   map[string]*OpenCodeVariant `json:"variants,omitempty"`
	Modalities *Modalities                 `json:"modalities,omitempty"`
}

type OpenCodeOptions struct {
	Thinking         *OpenCodeThinking `json:"thinking,omitempty"`
	ReasoningEffort  string            `json:"reasoningEffort,omitempty"`
	TextVerbosity    string            `json:"textVerbosity,omitempty"`
	ReasoningSummary string            `json:"reasoningSummary,omitempty"`
}

type OpenCodeThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budgetTokens,omitempty"`
}

type OpenCodeVariant struct {
	ReasoningEffort  string            `json:"reasoningEffort,omitempty"`
	TextVerbosity    string            `json:"textVerbosity,omitempty"`
	ReasoningSummary string            `json:"reasoningSummary,omitempty"`
	Thinking         *OpenCodeThinking `json:"thinking,omitempty"`
}

func generateOpenCodeConfig(models map[string][]Model) OpenCodeConfig {
	config := OpenCodeConfig{
		Schema:   "https://opencode.ai/config.json",
		Provider: make(map[string]*OpenCodeProvider),
	}

	claudeProvider := &OpenCodeProvider{
		Name:    "AI Proxy (Claude)",
		Type:    "anthropic",
		BaseURL: "http://localhost:8317",
		APIKey:  "dummy",
		Models:  make(map[string]*OpenCodeModel),
	}

	openaiProvider := &OpenCodeProvider{
		Name:    "AI Proxy (OpenAI)",
		Type:    "openai",
		BaseURL: "http://localhost:8317/v1",
		APIKey:  "dummy",
		Models:  make(map[string]*OpenCodeModel),
	}

	// Process Claude models
	if claudeModels, ok := models["claude"]; ok {
		for _, m := range claudeModels {
			ocModel := &OpenCodeModel{
				Name:       m.DisplayName,
				Modalities: m.Modalities,
			}

			if m.Thinking != nil && m.Thinking.Supported {
				ocModel.Variants = map[string]*OpenCodeVariant{
					"low":    {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 4000}},
					"medium": {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 10000}},
					"high":   {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 32000}},
					"max":    {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 64000}},
				}
			}

			claudeProvider.Models[m.ID] = ocModel
		}
	}

	// Process Codex/OpenAI models
	if codexModels, ok := models["codex"]; ok {
		for _, m := range codexModels {
			ocModel := &OpenCodeModel{
				Name:       m.DisplayName,
				Modalities: m.Modalities,
			}

			if m.Thinking != nil && len(m.Thinking.Levels) > 0 {
				ocModel.Variants = make(map[string]*OpenCodeVariant)
				for _, level := range m.Thinking.Levels {
					ocModel.Variants[level] = &OpenCodeVariant{
						ReasoningEffort:  level,
						TextVerbosity:    "low",
						ReasoningSummary: "auto",
					}
				}
			}

			openaiProvider.Models[m.ID] = ocModel
		}
	}

	// Process other providers
	for _, providerKey := range []string{"gemini", "antigravity", "kiro", "github-copilot", "qwen"} {
		if providerModels, ok := models[providerKey]; ok {
			for _, m := range providerModels {
				ocModel := &OpenCodeModel{
					Name:       m.DisplayName,
					Modalities: m.Modalities,
				}

				if m.Thinking != nil && m.Thinking.Supported {
					ocModel.Variants = map[string]*OpenCodeVariant{
						"low":    {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 4000}},
						"medium": {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 10000}},
						"high":   {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 32000}},
					}
				}

				openaiProvider.Models[m.ID] = ocModel
			}
		}
	}

	if len(claudeProvider.Models) > 0 {
		config.Provider["ai-proxy-claude"] = claudeProvider
	}
	if len(openaiProvider.Models) > 0 {
		config.Provider["ai-proxy-openai"] = openaiProvider
	}

	return config
}
