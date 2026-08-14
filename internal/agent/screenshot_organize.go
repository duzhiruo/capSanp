package agent

import (
	"time"

	"capsnap/internal/llm"
	"capsnap/internal/tools"
)

// NewScreenshotOrganizeType 注册截图整理 Agent 类型。
// 固定 4 步工作流：OCR → LLM Insight → Save Memory → Embedding
func NewScreenshotOrganizeType() *AgentType {
	return &AgentType{
		Name: "screenshot_organize",
		Steps: []StepDefinition{
			{
				Name:     "ocr",
				ToolName: tools.OCRToolName,
				BuildInput: func(rc *RunContext) map[string]any {
					return map[string]any{
						"screenshot_id": rc.Input["screenshot_id"],
						"ocr_hint":      rc.Input["ocr_hint"],
					}
				},
				Timeout:   60 * time.Second,
				OnFailure: FailAbort,
			},
			{
				Name:     "llm_insight",
				ToolName: tools.LLMInsightToolName,
				BuildInput: func(rc *RunContext) map[string]any {
					ocrText, _ := rc.StepOutputs["ocr"]["ocr_text"].(string)
					return map[string]any{"ocr_text": ocrText}
				},
				Timeout:    45 * time.Second,
				MaxRetries: 1,
				RetryDelay: 2 * time.Second,
				OnFailure:  FailContinueDegraded,
				FallbackOutput: func(rc *RunContext, err error) map[string]any {
					ocrText, _ := rc.StepOutputs["ocr"]["ocr_text"].(string)
					mock := llm.MockInsight(ocrText)
					return map[string]any{
						"summary":     mock.Insight.Summary,
						"category":    mock.Insight.Category,
						"tags":        mock.Insight.Tags,
						"explanation": "LLM 调用失败，使用降级结果: " + err.Error(),
						"_llm_result": mock,
					}
				},
			},
			{
				Name:     "save_memory",
				ToolName: tools.MemoryToolName,
				BuildInput: func(rc *RunContext) map[string]any {
					llmOutput := rc.StepOutputs["llm_insight"]
					return map[string]any{
						"screenshot_id": rc.Input["screenshot_id"],
						"ocr_text":      rc.StepOutputs["ocr"]["ocr_text"],
						"summary":       llmOutput["summary"],
						"category":      llmOutput["category"],
						"tags":          llmOutput["tags"],
						"explanation":   llmOutput["explanation"],
					}
				},
				Timeout:    10 * time.Second,
				MaxRetries: 1,
				RetryDelay: 1 * time.Second,
				OnFailure:  FailAbort,
			},
			{
				Name:     "embedding",
				ToolName: tools.EmbeddingToolName,
				BuildInput: func(rc *RunContext) map[string]any {
					llmOutput := rc.StepOutputs["llm_insight"]
					return map[string]any{
						"screenshot_id": rc.Input["screenshot_id"],
						"device_id":     rc.Input["device_id"],
						"ocr_text":      rc.StepOutputs["ocr"]["ocr_text"],
						"summary":       llmOutput["summary"],
						"tags":          llmOutput["tags"],
					}
				},
				Timeout:   30 * time.Second,
				OnFailure: FailSkip,
			},
		},
	}
}
