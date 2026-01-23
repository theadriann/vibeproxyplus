package proxy

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	MaxThinkingBudget = 32000
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

// TransformRequestBody modifies the JSON body if model has thinking suffix.
// Returns: transformedBody, wasTransformed, error
func TransformRequestBody(body []byte) ([]byte, bool, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body, false, err
	}

	model, ok := data["model"].(string)
	if !ok {
		return body, false, nil
	}

	cleanModel, budget, hasThinking := ParseThinkingSuffix(model)
	if !hasThinking {
		return body, false, nil
	}

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
