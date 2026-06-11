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
	modelDefsURL         = "https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/internal/registry/model_definitions.go"
	registryModelsURL    = "https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json"
	codexClientModelsURL = "https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/internal/registry/models/codex_client_models.json"
	modelsDevURL         = "https://models.dev/api.json"
)

// Canonical model with merged metadata
type Model struct {
	ID                  string        `json:"id"`
	Provider            string        `json:"provider"`
	DisplayName         string        `json:"display_name"`
	Description         string        `json:"description,omitempty"`
	Family              string        `json:"family,omitempty"`
	Type                string        `json:"type"`
	OwnedBy             string        `json:"owned_by"`
	ContextLength       int           `json:"context_length,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Thinking            *Thinking     `json:"thinking,omitempty"`
	Modalities          *Modalities   `json:"modalities,omitempty"`
	Capabilities        *Capabilities `json:"capabilities,omitempty"`
	Cost                *Cost         `json:"cost,omitempty"`
}

type Thinking struct {
	Supported   bool     `json:"supported"`
	Min         int      `json:"min,omitempty"`
	Max         int      `json:"max,omitempty"`
	ZeroAllowed bool     `json:"zero_allowed,omitempty"`
	Levels      []string `json:"levels,omitempty"`
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
	Version string             `json:"version"`
	Sources []string           `json:"sources"`
	Models  map[string][]Model `json:"models"`
}

// models.dev types
type ModelsDevAPI map[string]*ModelsDevProvider

type ModelsDevProvider struct {
	ID     string                     `json:"id"`
	Name   string                     `json:"name"`
	Models map[string]*ModelsDevModel `json:"models"`
}

type ModelsDevModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Family           string             `json:"family"`
	Attachment       bool               `json:"attachment"`
	Reasoning        bool               `json:"reasoning"`
	ToolCall         bool               `json:"tool_call"`
	StructuredOutput bool               `json:"structured_output"`
	Temperature      bool               `json:"temperature"`
	Modalities       *Modalities        `json:"modalities"`
	Cost             map[string]float64 `json:"cost"`
	Limit            map[string]int     `json:"limit"`
}

type indexedModelsDevModel struct {
	Provider string
	Model    *ModelsDevModel
}

type CodexClientMetadataIndex map[string]*CodexClientMetadata

type CodexClientMetadata struct {
	SupportsPriorityServiceTier bool
	SupportsVerbosity           bool
	DefaultVerbosity            string
	SupportsReasoningSummaries  bool
	DefaultReasoningSummary     string
	ReasoningLevels             []string
}

type codexClientModelsPayload struct {
	Models []codexClientModel `json:"models"`
}

type codexClientModel struct {
	Slug                       string                      `json:"slug"`
	SupportsVerbosity          bool                        `json:"support_verbosity"`
	DefaultVerbosity           string                      `json:"default_verbosity"`
	SupportsReasoningSummaries bool                        `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary    string                      `json:"default_reasoning_summary"`
	ServiceTiers               []codexClientServiceTier    `json:"service_tiers"`
	AdditionalSpeedTiers       []string                    `json:"additional_speed_tiers"`
	SupportedReasoningLevels   []codexClientReasoningLevel `json:"supported_reasoning_levels"`
}

type codexClientServiceTier struct {
	ID string `json:"id"`
}

type codexClientReasoningLevel struct {
	Effort string `json:"effort"`
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

var supportedFactoryModelFields = map[string]struct{}{
	"model":           {},
	"displayName":     {},
	"baseUrl":         {},
	"apiKey":          {},
	"provider":        {},
	"maxOutputTokens": {},
	"supportsImages":  {},
	"extraArgs":       {},
	"extraHeaders":    {},
}

var supportedFactoryProviders = map[string]struct{}{
	"anthropic":                   {},
	"openai":                      {},
	"generic-chat-completion-api": {},
}

func main() {
	outputFile := flag.String("output", "models.json", "Output file for canonical config")
	factoryFile := flag.String("factory", "", "Generate Factory CLI config file")
	opencodeFile := flag.String("opencode", "", "Generate OpenCode CLI config file")
	localModelDefs := flag.String("local-modeldefs", "", "Use local model_definitions.go")
	localRegistryModels := flag.String("local-registry-models", "", "Use local model catalog models.json")
	localCodexClientModels := flag.String("local-codex-client-models", "", "Use local Codex client models JSON")
	localModelsDev := flag.String("local-modelsdev", "", "Use local models.dev api.json")
	flag.Parse()

	// Download/load CLIProxyAPI model definitions
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
		fmt.Printf("Downloading CLIProxyAPI model definitions...\n")

		resp, err := http.Get(modelDefsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading model_definitions.go: %v\n", err)
			os.Exit(1)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		modelDefsSource = string(data)
	}

	var registryModelsSource string
	if *localRegistryModels != "" {
		data, err := os.ReadFile(*localRegistryModels)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading local registry models.json: %v\n", err)
			os.Exit(1)
		}
		registryModelsSource = string(data)
		fmt.Printf("Using local model catalog models.json: %s\n", *localRegistryModels)
	} else {
		fmt.Printf("Downloading shared model catalog...\n")
		resp, err := http.Get(registryModelsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading model catalog models.json: %v\n", err)
			os.Exit(1)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		registryModelsSource = string(data)
	}

	var codexClientModelsSource string
	if *localCodexClientModels != "" {
		data, err := os.ReadFile(*localCodexClientModels)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading local Codex client models JSON: %v\n", err)
			os.Exit(1)
		}
		codexClientModelsSource = string(data)
		fmt.Printf("Using local Codex client models JSON: %s\n", *localCodexClientModels)
	} else {
		fmt.Printf("Downloading Codex client models catalog...\n")
		resp, err := http.Get(codexClientModelsURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading Codex client models catalog: %v\n", err)
			os.Exit(1)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		codexClientModelsSource = string(data)
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

	// Parse CLIProxyAPI models and enrich with models.dev
	models := parseAndEnrichModels(modelDefsSource, registryModelsSource, modelsDevIndex)
	codexMetadata, err := buildCodexClientMetadataIndex(codexClientModelsSource)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing Codex client models catalog: %v\n", err)
		os.Exit(1)
	}

	config := CanonicalConfig{
		Version: "2.0",
		Sources: []string{modelDefsURL, registryModelsURL, codexClientModelsURL, modelsDevURL},
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
		factoryConfig := generateFactoryConfig(models, codexMetadata)
		data, err := marshalFactoryConfig(factoryConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating Factory config: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*factoryFile, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing Factory config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Written Factory config to: %s (%d models)\n", *factoryFile, len(factoryConfig.CustomModels))
	}

	// Generate OpenCode config
	if *opencodeFile != "" {
		opencodeConfig := generateOpenCodeConfig(models, codexMetadata)
		data, _ := json.MarshalIndent(opencodeConfig, "", "  ")
		os.WriteFile(*opencodeFile, data, 0644)
		fmt.Printf("Written OpenCode config to: %s\n", *opencodeFile)
	}
}

// buildModelsDevIndex creates a lookup map by model ID across all providers
func buildModelsDevIndex(api ModelsDevAPI) map[string]*ModelsDevModel {
	index := make(map[string]indexedModelsDevModel)

	providerIDs := make([]string, 0, len(api))
	for providerID := range api {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Slice(providerIDs, func(i, j int) bool {
		pi := modelsDevProviderPriority(providerIDs[i])
		pj := modelsDevProviderPriority(providerIDs[j])
		if pi != pj {
			return pi < pj
		}
		return providerIDs[i] < providerIDs[j]
	})

	for _, providerID := range providerIDs {
		provider := api[providerID]
		if provider == nil || provider.Models == nil {
			continue
		}

		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)

		for _, modelID := range modelIDs {
			model := provider.Models[modelID]
			if model == nil {
				continue
			}

			upsertIndexedModel(index, modelID, providerID, model)

			// Also index by normalized ID (lowercase, no version suffix)
			normalized := normalizeModelID(modelID)
			upsertIndexedModel(index, normalized, providerID, model)
		}
	}

	flat := make(map[string]*ModelsDevModel, len(index))
	for key, item := range index {
		flat[key] = item.Model
	}
	return flat
}

func upsertIndexedModel(index map[string]indexedModelsDevModel, key, providerID string, model *ModelsDevModel) {
	current, exists := index[key]
	if !exists {
		index[key] = indexedModelsDevModel{
			Provider: providerID,
			Model:    model,
		}
		return
	}

	if shouldReplaceIndexedModel(current.Provider, current.Model, providerID, model) {
		index[key] = indexedModelsDevModel{
			Provider: providerID,
			Model:    model,
		}
	}
}

func shouldReplaceIndexedModel(currentProvider string, currentModel *ModelsDevModel, candidateProvider string, candidateModel *ModelsDevModel) bool {
	currentPriority := modelsDevProviderPriority(currentProvider)
	candidatePriority := modelsDevProviderPriority(candidateProvider)

	if candidatePriority != currentPriority {
		return candidatePriority < currentPriority
	}

	currentScore := modelsDevModelQualityScore(currentModel)
	candidateScore := modelsDevModelQualityScore(candidateModel)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}

	// Final deterministic tie-breaker.
	return candidateProvider < currentProvider
}

func modelsDevModelQualityScore(model *ModelsDevModel) int {
	if model == nil {
		return 0
	}

	score := 0
	if model.Name != "" {
		score++
	}
	if model.Family != "" {
		score++
	}
	if model.Modalities != nil {
		score += 2
		score += len(model.Modalities.Input)
		score += len(model.Modalities.Output)
	}
	if model.Limit != nil && len(model.Limit) > 0 {
		score += 2 + len(model.Limit)
	}
	if model.Cost != nil && len(model.Cost) > 0 {
		score += 2 + len(model.Cost)
	}
	if model.Attachment {
		score++
	}
	if model.Reasoning {
		score++
	}
	if model.ToolCall {
		score++
	}
	if model.StructuredOutput {
		score++
	}
	if model.Temperature {
		score++
	}
	return score
}

func modelsDevProviderPriority(providerID string) int {
	switch providerID {
	case "openai":
		return 10
	case "anthropic":
		return 20
	case "google", "google-vertex":
		return 30
	case "google-vertex-anthropic":
		return 40
	case "github-copilot", "github-models":
		return 50
	case "amazon-bedrock":
		return 60
	case "alibaba", "alibaba-cn":
		return 70
	case "iflowcn":
		return 80
	default:
		return 1000
	}
}

func normalizeModelID(id string) string {
	// Remove date suffixes like -20250929
	re := regexp.MustCompile(`-\d{8}$`)
	normalized := re.ReplaceAllString(id, "")
	return strings.ToLower(normalized)
}

func buildCodexClientMetadataIndex(source string) (CodexClientMetadataIndex, error) {
	var payload codexClientModelsPayload
	if err := json.Unmarshal([]byte(source), &payload); err != nil {
		return nil, err
	}

	index := make(CodexClientMetadataIndex, len(payload.Models))
	for _, model := range payload.Models {
		slug := strings.TrimSpace(model.Slug)
		if slug == "" {
			continue
		}

		metadata := &CodexClientMetadata{
			SupportsVerbosity:          model.SupportsVerbosity,
			DefaultVerbosity:           normalizeCodexOption(model.DefaultVerbosity),
			SupportsReasoningSummaries: model.SupportsReasoningSummaries,
			DefaultReasoningSummary:    normalizeCodexOption(model.DefaultReasoningSummary),
			ReasoningLevels:            normalizeCodexReasoningLevels(model.SupportedReasoningLevels),
		}

		for _, tier := range model.ServiceTiers {
			if strings.EqualFold(strings.TrimSpace(tier.ID), "priority") {
				metadata.SupportsPriorityServiceTier = true
				break
			}
		}
		if !metadata.SupportsPriorityServiceTier {
			for _, tier := range model.AdditionalSpeedTiers {
				if strings.EqualFold(strings.TrimSpace(tier), "fast") {
					metadata.SupportsPriorityServiceTier = true
					break
				}
			}
		}

		index[slug] = metadata
	}

	return index, nil
}

func normalizeCodexReasoningLevels(levels []codexClientReasoningLevel) []string {
	out := make([]string, 0, len(levels))
	for _, rawLevel := range levels {
		level := normalizeCodexOption(rawLevel.Effort)
		if level == "" {
			continue
		}
		out = append(out, level)
	}
	return out
}

func normalizeCodexOption(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseAndEnrichModels(source, registryModelsSource string, modelsDevIndex map[string]*ModelsDevModel) map[string][]Model {
	models := parseRegistryModelsJSON(registryModelsSource, modelsDevIndex)

	parsers := []struct {
		funcName string
		provider string
	}{
		{"GetClaudeModels", "claude"},
		{"GetOpenAIModels", "codex"},
		{"GetCodexFreeModels", "codex"},
		{"GetCodexTeamModels", "codex"},
		{"GetCodexPlusModels", "codex"},
		{"GetCodexProModels", "codex"},
		{"GetGeminiModels", "gemini"},
		{"GetGeminiCLIModels", "gemini-cli"},
		{"GetGeminiVertexModels", "vertex"},
		{"GetAIStudioModels", "aistudio"},
		{"GetQwenModels", "qwen"},
		{"GetIFlowModels", "iflow"},
		{"GetAntigravityModels", "antigravity"},
		{"GetGitHubCopilotModels", "github-copilot"},
		{"GetKiroModels", "kiro"},
		{"GetAmazonQModels", "amazonq"},
	}

	for _, p := range parsers {
		funcModels := parseFunctionModels(source, p.funcName, p.provider, modelsDevIndex)
		mergeModels(models, p.provider, funcModels)
	}

	// Parse Antigravity
	antigravityModels := parseAntigravityModels(source, modelsDevIndex)
	mergeModels(models, "antigravity", antigravityModels)
	mergeModels(models, "claude", supplementalAnthropicModels())

	for provider := range models {
		sort.Slice(models[provider], func(i, j int) bool {
			return models[provider][i].ID < models[provider][j].ID
		})
	}

	return models
}

func supplementalAnthropicModels() []Model {
	return []Model{
		{
			ID:                  "claude-fable-5",
			Provider:            "claude",
			DisplayName:         "Claude Fable 5",
			Description:         "Anthropic's most capable widely released model, for the most demanding reasoning and long-horizon agentic work.",
			Family:              "claude-fable",
			Type:                "anthropic",
			OwnedBy:             "anthropic",
			ContextLength:       1000000,
			MaxCompletionTokens: 128000,
			Thinking: &Thinking{
				Supported: true,
				Levels:    []string{"low", "medium", "high", "xhigh", "max"},
			},
			Modalities: &Modalities{
				Input:  []string{"text", "image"},
				Output: []string{"text"},
			},
			Capabilities: &Capabilities{
				Reasoning:   true,
				ToolCall:    true,
				Attachment:  true,
				Temperature: true,
			},
			Cost: &Cost{
				Input:  10,
				Output: 50,
			},
		},
		{
			ID:                  "claude-mythos-5",
			Provider:            "claude",
			DisplayName:         "Claude Mythos 5",
			Description:         "Limited-availability Project Glasswing model, successor to Claude Mythos Preview.",
			Family:              "claude-mythos",
			Type:                "anthropic",
			OwnedBy:             "anthropic",
			ContextLength:       1000000,
			MaxCompletionTokens: 128000,
			Thinking: &Thinking{
				Supported: true,
				Levels:    []string{"low", "medium", "high", "xhigh", "max"},
			},
			Modalities: &Modalities{
				Input:  []string{"text", "image"},
				Output: []string{"text"},
			},
			Capabilities: &Capabilities{
				Reasoning:   true,
				ToolCall:    true,
				Attachment:  true,
				Temperature: true,
			},
			Cost: &Cost{
				Input:  10,
				Output: 50,
			},
		},
	}
}

func parseRegistryModelsJSON(source string, modelsDevIndex map[string]*ModelsDevModel) map[string][]Model {
	models := make(map[string][]Model)
	if strings.TrimSpace(source) == "" {
		return models
	}

	var registryModels map[string][]Model
	if err := json.Unmarshal([]byte(source), &registryModels); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse registry models.json: %v\n", err)
		return models
	}

	providerGroups := []struct {
		group    string
		provider string
	}{
		{"claude", "claude"},
		{"gemini", "gemini"},
		{"vertex", "vertex"},
		{"gemini-cli", "gemini-cli"},
		{"aistudio", "aistudio"},
		{"codex-pro", "codex"},
		{"codex-plus", "codex"},
		{"codex-team", "codex"},
		{"codex-free", "codex"},
		{"qwen", "qwen"},
		{"iflow", "iflow"},
		{"kimi", "kimi"},
		{"antigravity", "antigravity"},
	}

	for _, group := range providerGroups {
		groupModels := registryModels[group.group]
		if len(groupModels) == 0 {
			continue
		}

		normalized := make([]Model, 0, len(groupModels))
		for _, model := range groupModels {
			model.Provider = group.provider
			if model.DisplayName == "" {
				model.DisplayName = formatDisplayName(model.ID)
			}
			if model.Thinking != nil {
				model.Thinking.Supported = true
			}
			enrichFromModelsDev(&model, modelsDevIndex)
			normalized = append(normalized, model)
		}

		mergeModels(models, group.provider, normalized)
	}

	return models
}

func mergeModels(models map[string][]Model, provider string, incoming []Model) {
	if len(incoming) == 0 {
		return
	}

	merged := make(map[string]Model, len(models[provider])+len(incoming))
	for _, model := range models[provider] {
		merged[model.ID] = model
	}

	for _, candidate := range incoming {
		current, exists := merged[candidate.ID]
		if !exists || shouldReplaceMergedModel(current, candidate) {
			merged[candidate.ID] = candidate
		}
	}

	result := make([]Model, 0, len(merged))
	for _, model := range merged {
		result = append(result, model)
	}
	models[provider] = result
}

func shouldReplaceMergedModel(current, candidate Model) bool {
	currentScore := mergedModelQualityScore(current)
	candidateScore := mergedModelQualityScore(candidate)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}

	return false
}

func mergedModelQualityScore(model Model) int {
	score := 0
	if model.DisplayName != "" {
		score++
	}
	if model.Description != "" {
		score++
	}
	if model.Family != "" {
		score++
	}
	if model.Type != "" {
		score++
	}
	if model.OwnedBy != "" {
		score++
	}
	if model.ContextLength > 0 {
		score++
	}
	if model.MaxCompletionTokens > 0 {
		score++
	}
	if model.Thinking != nil {
		score += 1 + len(model.Thinking.Levels)
		if model.Thinking.Min > 0 {
			score++
		}
		if model.Thinking.Max > 0 {
			score++
		}
		if model.Thinking.ZeroAllowed {
			score++
		}
	}
	if model.Modalities != nil {
		score += 1 + len(model.Modalities.Input) + len(model.Modalities.Output)
	}
	if model.Capabilities != nil {
		score++
	}
	if model.Cost != nil {
		score++
	}
	return score
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

		// Parse thinking support from CLIProxyAPI
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

func generateFactoryConfig(models map[string][]Model, codexMetadata CodexClientMetadataIndex) FactoryConfig {
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

			if m.Provider == "claude" && m.Thinking != nil && m.Thinking.Supported {
				if levels := claudeAdaptiveLevels(m); len(levels) > 0 {
					factoryModels = append(factoryModels, FactoryModel{
						Model:           fmt.Sprintf("%s(auto)", m.ID),
						DisplayName:     fmt.Sprintf("[%s] %s (Auto)", prefix, m.DisplayName),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					})
					for _, level := range levels {
						factoryModels = append(factoryModels, FactoryModel{
							Model:           fmt.Sprintf("%s(%s)", m.ID, level),
							DisplayName:     fmt.Sprintf("[%s] %s (%s)", prefix, m.DisplayName, strings.Title(level)),
							BaseURL:         cfg.baseURL,
							APIKey:          "dummy",
							Provider:        cfg.provider,
							MaxOutputTokens: m.MaxCompletionTokens,
							SupportsImages:  supportsImages,
						})
					}
				}

				for _, level := range claudeManualEffortLevels(m) {
					factoryModels = append(factoryModels, FactoryModel{
						Model:           fmt.Sprintf("%s(%s)", m.ID, level),
						DisplayName:     fmt.Sprintf("[%s] %s (%s)", prefix, m.DisplayName, strings.Title(level)),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					})
				}

				for _, budget := range claudeLegacyBudgetVariants(m) {
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

			if codexSupportsPriorityServiceTier(m, codexMetadata) {
				factoryModels = append(factoryModels, FactoryModel{
					Model:           fmt.Sprintf("%s(fast)", m.ID),
					DisplayName:     fmt.Sprintf("[%s] %s (Fast)", prefix, m.DisplayName),
					BaseURL:         cfg.baseURL,
					APIKey:          "dummy",
					Provider:        cfg.provider,
					MaxOutputTokens: m.MaxCompletionTokens,
					SupportsImages:  supportsImages,
				})

				for _, level := range codexFastReasoningLevels(m) {
					factoryModels = append(factoryModels, FactoryModel{
						Model:           fmt.Sprintf("%s(%s-fast)", m.ID, level),
						DisplayName:     fmt.Sprintf("[%s] %s (%s Fast)", prefix, m.DisplayName, strings.Title(level)),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					})
				}
			}

			if codexSupportsVerbosity(m, codexMetadata) {
				factoryModels = append(factoryModels, FactoryModel{
					Model:           fmt.Sprintf("%s(verbose)", m.ID),
					DisplayName:     fmt.Sprintf("[%s] %s (Verbose)", prefix, m.DisplayName),
					BaseURL:         cfg.baseURL,
					APIKey:          "dummy",
					Provider:        cfg.provider,
					MaxOutputTokens: m.MaxCompletionTokens,
					SupportsImages:  supportsImages,
				})

				for _, level := range codexFastReasoningLevels(m) {
					factoryModels = append(factoryModels, FactoryModel{
						Model:           fmt.Sprintf("%s(%s-verbose)", m.ID, level),
						DisplayName:     fmt.Sprintf("[%s] %s (%s Verbose)", prefix, m.DisplayName, strings.Title(level)),
						BaseURL:         cfg.baseURL,
						APIKey:          "dummy",
						Provider:        cfg.provider,
						MaxOutputTokens: m.MaxCompletionTokens,
						SupportsImages:  supportsImages,
					})
				}
			}
		}
	}

	sort.Slice(factoryModels, func(i, j int) bool {
		if factoryModels[i].DisplayName == factoryModels[j].DisplayName {
			return factoryModels[i].Model < factoryModels[j].Model
		}
		return factoryModels[i].DisplayName < factoryModels[j].DisplayName
	})

	return FactoryConfig{CustomModels: factoryModels}
}

func isClaudeAdaptiveOnlyModel(model Model) bool {
	switch model.ID {
	case "claude-fable-5", "claude-mythos-5", "claude-opus-4-7", "claude-opus-4-8":
		return true
	default:
		return false
	}
}

func claudeAdaptiveLevels(model Model) []string {
	if model.Provider != "claude" || model.Thinking == nil {
		return nil
	}
	return append([]string(nil), model.Thinking.Levels...)
}

func claudeManualEffortLevels(model Model) []string {
	switch model.ID {
	case "claude-opus-4-5-20251101":
		return []string{"low", "medium", "high"}
	default:
		return nil
	}
}

func claudeLegacyBudgetVariants(model Model) []int {
	if model.Provider != "claude" || model.Thinking == nil || !model.Thinking.Supported || isClaudeAdaptiveOnlyModel(model) {
		return nil
	}
	return []int{4000, 10000, 32000}
}

func claudeManualBudgetForLevel(level string) int {
	switch level {
	case "low":
		return 1024
	case "medium":
		return 8192
	case "high":
		return 24576
	case "xhigh":
		return 32768
	case "max":
		return 64000
	default:
		return 0
	}
}

func codexFastReasoningLevels(model Model) []string {
	if model.Provider != "codex" || model.Thinking == nil {
		return nil
	}

	levels := make([]string, 0, len(model.Thinking.Levels))
	for _, level := range model.Thinking.Levels {
		level = strings.TrimSpace(strings.ToLower(level))
		if level == "" || level == "none" {
			continue
		}
		levels = append(levels, level)
	}
	return levels
}

func codexSupportsPriorityServiceTier(model Model, metadata CodexClientMetadataIndex) bool {
	if model.Provider != "codex" {
		return false
	}
	if modelMetadata := metadata[model.ID]; modelMetadata != nil {
		return modelMetadata.SupportsPriorityServiceTier
	}
	return false
}

func codexSupportsVerbosity(model Model, metadata CodexClientMetadataIndex) bool {
	if model.Provider != "codex" {
		return false
	}
	if modelMetadata := metadata[model.ID]; modelMetadata != nil {
		return modelMetadata.SupportsVerbosity
	}
	return false
}

func marshalFactoryConfig(config FactoryConfig) ([]byte, error) {
	if err := validateFactoryConfig(config); err != nil {
		return nil, err
	}

	return json.MarshalIndent(config, "", "  ")
}

func validateFactoryConfig(config FactoryConfig) error {
	for i, model := range config.CustomModels {
		data, err := json.Marshal(model)
		if err != nil {
			return fmt.Errorf("marshal custom model %d: %w", i, err)
		}

		var obj map[string]interface{}
		if err := json.Unmarshal(data, &obj); err != nil {
			return fmt.Errorf("unmarshal custom model %d: %w", i, err)
		}

		if err := validateFactoryModelObject(obj); err != nil {
			return fmt.Errorf("customModels[%d]: %w", i, err)
		}
	}

	return nil
}

func validateFactoryModelObject(obj map[string]interface{}) error {
	for key := range obj {
		if _, ok := supportedFactoryModelFields[key]; !ok {
			return fmt.Errorf("unsupported field %q", key)
		}
	}

	required := []string{"model", "baseUrl", "apiKey", "provider"}
	for _, field := range required {
		value, ok := obj[field]
		if !ok {
			return fmt.Errorf("missing required field %q", field)
		}

		str, ok := value.(string)
		if !ok || strings.TrimSpace(str) == "" {
			return fmt.Errorf("required field %q must be a non-empty string", field)
		}
	}

	provider := obj["provider"].(string)
	if _, ok := supportedFactoryProviders[provider]; !ok {
		return fmt.Errorf("invalid provider %q", provider)
	}

	if value, ok := obj["supportsImages"]; ok {
		if _, isBool := value.(bool); !isBool {
			return fmt.Errorf("field %q must be a boolean", "supportsImages")
		}
	}

	if value, ok := obj["maxOutputTokens"]; ok {
		switch n := value.(type) {
		case float64:
			if n <= 0 {
				return fmt.Errorf("field %q must be a positive number", "maxOutputTokens")
			}
		case int:
			if n <= 0 {
				return fmt.Errorf("field %q must be a positive number", "maxOutputTokens")
			}
		case int32:
			if n <= 0 {
				return fmt.Errorf("field %q must be a positive number", "maxOutputTokens")
			}
		case int64:
			if n <= 0 {
				return fmt.Errorf("field %q must be a positive number", "maxOutputTokens")
			}
		default:
			return fmt.Errorf("field %q must be a positive number", "maxOutputTokens")
		}
	}

	if value, ok := obj["extraArgs"]; ok {
		if _, isObject := value.(map[string]interface{}); !isObject {
			return fmt.Errorf("field %q must be an object", "extraArgs")
		}
	}

	if value, ok := obj["extraHeaders"]; ok {
		headers, isObject := value.(map[string]interface{})
		if !isObject {
			return fmt.Errorf("field %q must be an object", "extraHeaders")
		}
		for header, headerValue := range headers {
			if _, ok := headerValue.(string); !ok {
				return fmt.Errorf("extraHeaders[%q] must be a string", header)
			}
		}
	}

	return nil
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

func generateOpenCodeConfig(models map[string][]Model, codexMetadata CodexClientMetadataIndex) OpenCodeConfig {
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

			if levels := claudeAdaptiveLevels(m); len(levels) > 0 {
				ocModel.Variants = map[string]*OpenCodeVariant{
					"auto": {Thinking: &OpenCodeThinking{Type: "adaptive"}},
				}
				for _, level := range levels {
					ocModel.Variants[level] = &OpenCodeVariant{
						Thinking:        &OpenCodeThinking{Type: "adaptive"},
						ReasoningEffort: level,
					}
				}
			} else if levels := claudeManualEffortLevels(m); len(levels) > 0 {
				ocModel.Variants = make(map[string]*OpenCodeVariant)
				for _, level := range levels {
					ocModel.Variants[level] = &OpenCodeVariant{
						Thinking:        &OpenCodeThinking{Type: "enabled", BudgetTokens: claudeManualBudgetForLevel(level)},
						ReasoningEffort: level,
					}
				}
			} else if m.Thinking != nil && m.Thinking.Supported {
				ocModel.Variants = map[string]*OpenCodeVariant{
					"low":    {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 4000}},
					"medium": {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 10000}},
					"high":   {Thinking: &OpenCodeThinking{Type: "enabled", BudgetTokens: 32000}},
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

			metadata := codexMetadata[m.ID]
			ocModel.Variants = codexOpenCodeVariants(m, metadata)

			openaiProvider.Models[m.ID] = ocModel

			if codexSupportsPriorityServiceTier(m, codexMetadata) {
				openaiProvider.Models[fmt.Sprintf("%s(fast)", m.ID)] = &OpenCodeModel{
					Name:       fmt.Sprintf("%s (Fast)", m.DisplayName),
					Variants:   codexOpenCodeVariants(m, metadata),
					Modalities: m.Modalities,
				}
			}
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

func codexOpenCodeVariants(model Model, metadata *CodexClientMetadata) map[string]*OpenCodeVariant {
	levels := codexOpenCodeReasoningLevels(model, metadata)
	if len(levels) == 0 && (metadata == nil || !metadata.SupportsVerbosity) {
		return nil
	}

	textVerbosity := codexDefaultVerbosity(metadata)
	reasoningSummary := codexDefaultReasoningSummary(metadata)

	variants := make(map[string]*OpenCodeVariant)
	for _, level := range levels {
		variants[level] = &OpenCodeVariant{
			ReasoningEffort:  level,
			TextVerbosity:    textVerbosity,
			ReasoningSummary: reasoningSummary,
		}
		if metadata != nil && metadata.SupportsVerbosity {
			variants[level+"-verbose"] = &OpenCodeVariant{
				ReasoningEffort:  level,
				TextVerbosity:    "high",
				ReasoningSummary: reasoningSummary,
			}
		}
	}
	if metadata != nil && metadata.SupportsVerbosity {
		variants["verbose"] = &OpenCodeVariant{
			TextVerbosity:    "high",
			ReasoningSummary: reasoningSummary,
		}
	}
	return variants
}

func codexOpenCodeReasoningLevels(model Model, metadata *CodexClientMetadata) []string {
	if metadata != nil && len(metadata.ReasoningLevels) > 0 {
		return append([]string(nil), metadata.ReasoningLevels...)
	}
	if model.Thinking == nil || len(model.Thinking.Levels) == 0 {
		return nil
	}
	return append([]string(nil), model.Thinking.Levels...)
}

func codexDefaultVerbosity(metadata *CodexClientMetadata) string {
	if metadata != nil && metadata.DefaultVerbosity != "" {
		return metadata.DefaultVerbosity
	}
	return "low"
}

func codexDefaultReasoningSummary(metadata *CodexClientMetadata) string {
	if metadata != nil && metadata.SupportsReasoningSummaries && metadata.DefaultReasoningSummary != "" {
		if metadata.DefaultReasoningSummary == "none" {
			return ""
		}
		return metadata.DefaultReasoningSummary
	}
	return "auto"
}
