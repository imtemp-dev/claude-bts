package metrics

import (
	"fmt"
	"strings"
)

// ModelPricing holds per-million-token pricing for a model family.
type ModelPricing struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64
}

// CostBreakdown holds itemised cost in USD.
type CostBreakdown struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Total      float64 `json:"total"`
}

// modelPricing maps model-name prefixes to their pricing.
// Longest prefix match is used so "claude-opus-4" matches "claude-opus-4-6".
var modelPricing = map[string]ModelPricing{
	"claude-opus-4":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25},
	"claude-sonnet-4": {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"claude-haiku-4":  {InputPerMTok: 1.0, OutputPerMTok: 5.0, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25},
}

// LookupPricing returns the pricing for a model using longest-prefix match.
func LookupPricing(model string) (ModelPricing, bool) {
	model = strings.ToLower(model)
	var best string
	for prefix := range modelPricing {
		if strings.HasPrefix(model, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return ModelPricing{}, false
	}
	return modelPricing[best], true
}

// CalculateCost computes cost from token counts and model name.
// Returns zero CostBreakdown for unknown models.
func CalculateCost(tokens TokenSnapshot, model string) CostBreakdown {
	pricing, ok := LookupPricing(model)
	if !ok {
		return CostBreakdown{}
	}

	cb := CostBreakdown{
		Input:      float64(tokens.InputTokens) * pricing.InputPerMTok / 1_000_000,
		Output:     float64(tokens.OutputTokens) * pricing.OutputPerMTok / 1_000_000,
		CacheRead:  float64(tokens.CacheReadTokens) * pricing.CacheReadPerMTok / 1_000_000,
		CacheWrite: float64(tokens.CacheCreationTokens) * pricing.CacheWritePerMTok / 1_000_000,
	}
	cb.Total = cb.Input + cb.Output + cb.CacheRead + cb.CacheWrite
	return cb
}

// FormatCost formats a USD cost for display.
func FormatCost(cost float64) string {
	if cost <= 0 {
		return "$0.00"
	}
	if cost < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", cost)
}

// AddCost adds two CostBreakdowns.
func AddCost(a, b CostBreakdown) CostBreakdown {
	return CostBreakdown{
		Input:      a.Input + b.Input,
		Output:     a.Output + b.Output,
		CacheRead:  a.CacheRead + b.CacheRead,
		CacheWrite: a.CacheWrite + b.CacheWrite,
		Total:      a.Total + b.Total,
	}
}
