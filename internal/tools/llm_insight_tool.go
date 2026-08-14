package tools

import (
	"context"

	"capsnap/internal/llm"
)

const LLMInsightToolName = "llm.generate_insight"

type LLMInsightTool struct {
	llm llm.Provider
}

func NewLLMInsightTool(provider llm.Provider) *LLMInsightTool {
	return &LLMInsightTool{llm: provider}
}

func (t *LLMInsightTool) Name() string {
	return LLMInsightToolName
}

func (t *LLMInsightTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	ocrText, _ := input["ocr_text"].(string)
	result, err := t.llm.GenerateInsight(ctx, ocrText)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"summary":     result.Insight.Summary,
		"category":    result.Insight.Category,
		"tags":        result.Insight.Tags,
		"explanation": result.Insight.Explanation,
		"_llm_result": result,
	}, nil
}
