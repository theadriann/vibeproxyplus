package proxy

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	MaxThinkingBudget = 32768
	ThinkingSuffix    = "-thinking-"
)

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

	// Cap budget
	if budget > MaxThinkingBudget {
		budget = MaxThinkingBudget
	}

	// For gemini-claude-* models, keep "-thinking" in the name
	cleanModel := model[:idx]
	if strings.HasPrefix(model, "gemini-claude-") {
		cleanModel = model[:idx] + "-thinking"
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

// TransformRequestBody modifies the JSON body if model has thinking suffix.
// Returns: transformedBody, needsBetaHeader, error
// needsBetaHeader is true if either:
// - Body was transformed with thinking parameter
// - Model has a thinking pattern that backend will handle (needs beta header)
func TransformRequestBody(body []byte) ([]byte, bool, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body, false, err
	}

	model, ok := data["model"].(string)
	if !ok {
		return body, false, nil
	}

	// Only process Claude models (including gemini-claude variants)
	if !strings.HasPrefix(model, "claude-") && !strings.HasPrefix(model, "gemini-claude-") {
		return body, false, nil
	}

	// Check for -thinking-NUMBER suffix that we handle ourselves
	cleanModel, budget, hasThinkingSuffix := ParseThinkingSuffix(model)
	if hasThinkingSuffix {
		// Update model name
		data["model"] = cleanModel

		// Add thinking parameter
		data["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": budget,
		}

		// Ensure max_tokens > budget
		minMaxTokens := budget + 1024
		if minMaxTokens > MaxThinkingBudget {
			minMaxTokens = MaxThinkingBudget
		}

		if maxTokens, ok := data["max_tokens"].(float64); !ok || int(maxTokens) <= budget {
			data["max_tokens"] = minMaxTokens
		}

		output, err := json.Marshal(data)
		return output, true, err
	}

	// Check for other thinking patterns that backend handles
	// but still need the beta header (e.g., -thinking, -thinking(budget))
	if HasThinkingPattern(model) {
		return body, true, nil
	}

	return body, false, nil
}
