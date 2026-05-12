package proxy

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	MaxThinkingBudget = 128000
	ThinkingSuffix    = "-thinking-"
)

type claudeThinkingMode string

const (
	claudeThinkingModeNone    claudeThinkingMode = "none"
	claudeThinkingModeAuto    claudeThinkingMode = "auto"
	claudeThinkingModeLevel   claudeThinkingMode = "level"
	claudeThinkingModeBudget  claudeThinkingMode = "budget"
	claudeThinkingModeUnknown claudeThinkingMode = ""
)

type claudeThinkingConfig struct {
	Mode   claudeThinkingMode
	Level  string
	Budget int
}

type claudeThinkingProfile struct {
	MaxOutputTokens    int
	AdaptiveOnly       bool
	SupportsAdaptive   bool
	AdaptiveLevels     []string
	ManualEffortLevels []string
}

// ParseThinkingSuffix extracts thinking budget from model name.
// Returns: cleanModel, budgetTokens, hasThinking
func ParseThinkingSuffix(model string) (string, int, bool) {
	idx := strings.LastIndex(model, ThinkingSuffix)
	if idx == -1 {
		return model, 0, false
	}

	budgetStr := model[idx+len(ThinkingSuffix):]
	budget, err := strconv.Atoi(budgetStr)
	if err != nil || budget <= 0 {
		// Invalid budget - strip suffix but don't enable thinking
		return model[:idx], 0, false
	}

	// For gemini-claude-* models, keep "-thinking" in the name
	cleanModel := model[:idx]
	if strings.HasPrefix(model, "gemini-claude-") {
		cleanModel = model[:idx] + "-thinking"
	}

	if budget > MaxThinkingBudget {
		budget = MaxThinkingBudget
	}

	return cleanModel, budget, true
}

// HasThinkingPattern checks if a model name has any thinking pattern that
// should trigger the beta header, even if we don't transform the body.
// Patterns: -thinking suffix, -thinking(budget) syntax
func HasThinkingPattern(model string) bool {
	// Check for -thinking suffix (e.g., gemini-claude-opus-4-5-thinking)
	if strings.HasSuffix(model, "-thinking") {
		return true
	}
	// Check for -thinking(budget) syntax (e.g., gemini-claude-opus-4-5-thinking(32768))
	if strings.Contains(model, "-thinking(") {
		return true
	}
	return false
}

func isCodexResponsesPath(path string) bool {
	switch path {
	case "/v1/responses", "/v1/responses/compact":
		return true
	default:
		return false
	}
}

func isCodexModel(model string) bool {
	baseModel := model
	if cleanModel, _, ok := splitParentheticalSuffix(model); ok {
		baseModel = cleanModel
	}
	return strings.HasPrefix(baseModel, "gpt-")
}

func normalizeCodexResponsesInput(data map[string]interface{}, path string) bool {
	if !isCodexResponsesPath(path) {
		return false
	}

	model, ok := data["model"].(string)
	if !ok || !isCodexModel(model) {
		return false
	}

	input, ok := data["input"].(string)
	if !ok {
		return false
	}

	data["input"] = []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type": "input_text",
					"text": input,
				},
			},
		},
	}
	return true
}

func splitParentheticalSuffix(model string) (string, string, bool) {
	lastOpen := strings.LastIndex(model, "(")
	if lastOpen == -1 || !strings.HasSuffix(model, ")") {
		return model, "", false
	}

	cleanModel := model[:lastOpen]
	rawSuffix := strings.ToLower(strings.TrimSpace(model[lastOpen+1 : len(model)-1]))
	if cleanModel == "" || rawSuffix == "" {
		return model, "", false
	}
	return cleanModel, rawSuffix, true
}

func parseParentheticalThinkingSuffix(model string) (string, claudeThinkingConfig, bool) {
	cleanModel, rawSuffix, ok := splitParentheticalSuffix(model)
	if !ok {
		return model, claudeThinkingConfig{}, false
	}

	switch rawSuffix {
	case "none":
		return cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeNone}, true
	case "auto", "-1":
		return cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeAuto}, true
	case "low", "medium", "high", "xhigh", "max":
		return cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeLevel, Level: rawSuffix}, true
	}

	budget, err := strconv.Atoi(rawSuffix)
	if err != nil {
		return model, claudeThinkingConfig{}, false
	}
	if budget == 0 {
		return cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeNone}, true
	}
	if budget > 0 {
		if budget > MaxThinkingBudget {
			budget = MaxThinkingBudget
		}
		return cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeBudget, Budget: budget}, true
	}

	return model, claudeThinkingConfig{}, false
}

func isCodexFastReasoningLevel(level string) bool {
	switch level {
	case "minimal", "none", "auto", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func parseCodexFastAlias(model string) (string, string, bool) {
	cleanModel, rawSuffix, ok := splitParentheticalSuffix(model)
	if !ok || !isCodexModel(cleanModel) {
		return model, "", false
	}

	switch rawSuffix {
	case "fast", "priority":
		return cleanModel, "priority", true
	}

	for _, tierSuffix := range []string{"-fast", "-priority"} {
		if strings.HasSuffix(rawSuffix, tierSuffix) {
			level := strings.TrimSuffix(rawSuffix, tierSuffix)
			if isCodexFastReasoningLevel(level) {
				return cleanModel + "(" + level + ")", "priority", true
			}
		}
	}

	for _, tierPrefix := range []string{"fast-", "priority-"} {
		if strings.HasPrefix(rawSuffix, tierPrefix) {
			level := strings.TrimPrefix(rawSuffix, tierPrefix)
			if isCodexFastReasoningLevel(level) {
				return cleanModel + "(" + level + ")", "priority", true
			}
		}
	}

	return model, "", false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isClaudeModel(model string) bool {
	return strings.HasPrefix(model, "claude-") || strings.HasPrefix(model, "gemini-claude-")
}

func claudeProfileForModel(model string) claudeThinkingProfile {
	if strings.HasPrefix(model, "gemini-claude-") {
		model = strings.TrimPrefix(model, "gemini-")
	}

	switch {
	case strings.HasPrefix(model, "claude-opus-4-7"):
		return claudeThinkingProfile{
			MaxOutputTokens:  128000,
			AdaptiveOnly:     true,
			SupportsAdaptive: true,
			AdaptiveLevels:   []string{"low", "medium", "high", "xhigh", "max"},
		}
	case strings.HasPrefix(model, "claude-opus-4-6"):
		return claudeThinkingProfile{
			MaxOutputTokens:  128000,
			SupportsAdaptive: true,
			AdaptiveLevels:   []string{"low", "medium", "high", "max"},
		}
	case strings.HasPrefix(model, "claude-sonnet-4-6"):
		return claudeThinkingProfile{
			MaxOutputTokens:  64000,
			SupportsAdaptive: true,
			AdaptiveLevels:   []string{"low", "medium", "high"},
		}
	case strings.HasPrefix(model, "claude-opus-4-5-20251101"):
		return claudeThinkingProfile{
			MaxOutputTokens:    64000,
			ManualEffortLevels: []string{"low", "medium", "high"},
		}
	case strings.HasPrefix(model, "claude-sonnet-4-5-20250929"), strings.HasPrefix(model, "claude-haiku-4-5-20251001"):
		return claudeThinkingProfile{MaxOutputTokens: 64000}
	case strings.HasPrefix(model, "claude-opus-4-20250514"), strings.HasPrefix(model, "claude-sonnet-4-20250514"):
		return claudeThinkingProfile{MaxOutputTokens: 32000}
	case strings.HasPrefix(model, "claude-3-7-sonnet-20250219"), strings.HasPrefix(model, "claude-3-5-haiku-20241022"):
		return claudeThinkingProfile{MaxOutputTokens: 8192}
	default:
		return claudeThinkingProfile{MaxOutputTokens: 64000}
	}
}

func mapLevelToLegacyBudget(level string) int {
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
		return 128000
	default:
		return 0
	}
}

func mapBudgetToAdaptiveLevel(budget int, levels []string) string {
	switch {
	case containsString(levels, "xhigh") && containsString(levels, "max"):
		switch {
		case budget <= 4000:
			return "low"
		case budget <= 10000:
			return "medium"
		case budget <= 32000:
			return "high"
		case budget <= 64000:
			return "xhigh"
		default:
			return "max"
		}
	case containsString(levels, "max"):
		switch {
		case budget <= 4000:
			return "low"
		case budget <= 10000:
			return "medium"
		case budget <= 32000:
			return "high"
		default:
			return "max"
		}
	default:
		switch {
		case budget <= 4000:
			return "low"
		case budget <= 10000:
			return "medium"
		default:
			return "high"
		}
	}
}

func deletePath(data map[string]interface{}, path ...string) {
	if len(path) == 0 {
		return
	}
	current := data
	for i := 0; i < len(path)-1; i++ {
		next, ok := current[path[i]].(map[string]interface{})
		if !ok {
			return
		}
		current = next
	}
	delete(current, path[len(path)-1])
}

func setOutputConfigField(data map[string]interface{}, key string, value interface{}) {
	outputConfig, _ := data["output_config"].(map[string]interface{})
	if outputConfig == nil {
		outputConfig = make(map[string]interface{})
		data["output_config"] = outputConfig
	}
	outputConfig[key] = value
}

func cleanupOutputConfig(data map[string]interface{}) {
	outputConfig, _ := data["output_config"].(map[string]interface{})
	if outputConfig != nil && len(outputConfig) == 0 {
		delete(data, "output_config")
	}
}

func applyDisabledThinking(data map[string]interface{}) {
	data["thinking"] = map[string]interface{}{"type": "disabled"}
	deletePath(data, "thinking", "budget_tokens")
	deletePath(data, "output_config", "effort")
	cleanupOutputConfig(data)
}

func applyAdaptiveThinking(data map[string]interface{}, level string) {
	data["thinking"] = map[string]interface{}{"type": "adaptive"}
	deletePath(data, "thinking", "budget_tokens")
	if level == "" {
		deletePath(data, "output_config", "effort")
	} else {
		setOutputConfigField(data, "effort", level)
	}
	cleanupOutputConfig(data)
}

func applyManualThinking(data map[string]interface{}, budget, maxOutputTokens int, effort string) {
	if budget <= 0 {
		applyDisabledThinking(data)
		return
	}

	if budget > MaxThinkingBudget {
		budget = MaxThinkingBudget
	}

	if maxOutputTokens > 0 && budget >= maxOutputTokens {
		budget = maxOutputTokens - 1
	}
	if budget <= 0 {
		applyDisabledThinking(data)
		return
	}

	requestMaxTokens := 0
	if maxTokens, ok := data["max_tokens"].(float64); ok && int(maxTokens) > 0 {
		requestMaxTokens = int(maxTokens)
	}

	effectiveMaxTokens := requestMaxTokens
	if effectiveMaxTokens == 0 || effectiveMaxTokens <= budget {
		effectiveMaxTokens = budget + 1024
	}
	if maxOutputTokens > 0 && effectiveMaxTokens > maxOutputTokens {
		effectiveMaxTokens = maxOutputTokens
	}
	if effectiveMaxTokens <= budget {
		budget = effectiveMaxTokens - 1
	}
	if budget <= 0 {
		applyDisabledThinking(data)
		return
	}

	data["thinking"] = map[string]interface{}{
		"type":          "enabled",
		"budget_tokens": budget,
	}
	if effort == "" {
		deletePath(data, "output_config", "effort")
	} else {
		setOutputConfigField(data, "effort", effort)
	}

	if requestMaxTokens != effectiveMaxTokens {
		data["max_tokens"] = effectiveMaxTokens
	}
	cleanupOutputConfig(data)
}

func applyClaudeThinkingConfig(data map[string]interface{}, model string, config claudeThinkingConfig) {
	profile := claudeProfileForModel(model)

	switch config.Mode {
	case claudeThinkingModeNone:
		applyDisabledThinking(data)
	case claudeThinkingModeAuto:
		if profile.SupportsAdaptive {
			applyAdaptiveThinking(data, "")
			return
		}
		applyManualThinking(data, 0, profile.MaxOutputTokens, "")
	case claudeThinkingModeLevel:
		if profile.SupportsAdaptive {
			level := config.Level
			if !containsString(profile.AdaptiveLevels, level) {
				level = profile.AdaptiveLevels[len(profile.AdaptiveLevels)-1]
			}
			applyAdaptiveThinking(data, level)
			return
		}

		effort := ""
		if containsString(profile.ManualEffortLevels, config.Level) {
			effort = config.Level
		}
		applyManualThinking(data, mapLevelToLegacyBudget(config.Level), profile.MaxOutputTokens, effort)
	case claudeThinkingModeBudget:
		if profile.AdaptiveOnly {
			applyAdaptiveThinking(data, mapBudgetToAdaptiveLevel(config.Budget, profile.AdaptiveLevels))
			return
		}
		applyManualThinking(data, config.Budget, profile.MaxOutputTokens, "")
	}
}

func getThinkingType(data map[string]interface{}) string {
	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		return ""
	}
	value, _ := thinking["type"].(string)
	return strings.ToLower(value)
}

func hasTaskBudget(data map[string]interface{}) bool {
	outputConfig, ok := data["output_config"].(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = outputConfig["task_budget"].(map[string]interface{})
	return ok
}

func requiredAnthropicBetas(model string, data map[string]interface{}) []string {
	var betas []string

	if HasThinkingPattern(model) {
		betas = append(betas, BetaInterleaved)
	}

	thinkingType := getThinkingType(data)
	if thinkingType == "enabled" {
		betas = append(betas, BetaInterleaved)
	}
	if hasTaskBudget(data) {
		betas = append(betas, BetaTaskBudgets)
	}

	deduped := make([]string, 0, len(betas))
	seen := make(map[string]struct{}, len(betas))
	for _, beta := range betas {
		if _, ok := seen[beta]; ok {
			continue
		}
		seen[beta] = struct{}{}
		deduped = append(deduped, beta)
	}
	return deduped
}

// TransformRequestBody modifies the JSON body when needed.
// Returns: transformedBody, requiredAnthropicBetas, error.
func TransformRequestBody(path string, body []byte) ([]byte, []string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body, nil, err
	}

	modified := normalizeCodexResponsesInput(data, path)

	model, ok := data["model"].(string)
	if !ok {
		if !modified {
			return body, nil, nil
		}
		output, err := json.Marshal(data)
		return output, nil, err
	}

	if cleanModel, serviceTier, hasFastAlias := parseCodexFastAlias(model); hasFastAlias {
		data["model"] = cleanModel
		data["service_tier"] = serviceTier
		model = cleanModel
		modified = true
	}

	// Only process Claude models (including gemini-claude variants)
	if !isClaudeModel(model) {
		if !modified {
			return body, nil, nil
		}
		output, err := json.Marshal(data)
		return output, nil, err
	}

	if strings.HasPrefix(model, "claude-") {
		if cleanModel, config, hasConfigSuffix := parseParentheticalThinkingSuffix(model); hasConfigSuffix {
			data["model"] = cleanModel
			applyClaudeThinkingConfig(data, cleanModel, config)
			output, err := json.Marshal(data)
			return output, requiredAnthropicBetas(cleanModel, data), err
		}
	}

	if cleanModel, budget, hasThinkingSuffix := ParseThinkingSuffix(model); hasThinkingSuffix {
		data["model"] = cleanModel
		applyClaudeThinkingConfig(data, cleanModel, claudeThinkingConfig{Mode: claudeThinkingModeBudget, Budget: budget})
		output, err := json.Marshal(data)
		return output, requiredAnthropicBetas(cleanModel, data), err
	}

	// Check for other thinking patterns that backend handles
	// but still need the beta header (e.g., -thinking, -thinking(budget))
	if HasThinkingPattern(model) {
		return body, requiredAnthropicBetas(model, data), nil
	}

	if modified {
		output, err := json.Marshal(data)
		return output, requiredAnthropicBetas(model, data), err
	}

	return body, requiredAnthropicBetas(model, data), nil
}
