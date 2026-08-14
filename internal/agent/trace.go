package agent

import (
	"context"
	"time"

	"capsnap/internal/llm"
	"capsnap/internal/store"
)

// Tracer 负责记录 Agent 运行轨迹。
type Tracer struct {
	store store.Repository
	runID string
}

func NewTracer(store store.Repository, runID string) *Tracer {
	return &Tracer{store: store, runID: runID}
}

func (t *Tracer) RecordStep(ctx context.Context, name, status string, input, output any, errMessage string, duration time.Duration) (int64, error) {
	return t.store.AddStep(ctx, t.runID, name, status, input, output, errMessage, duration)
}

func (t *Tracer) RecordTool(ctx context.Context, stepID int64, toolName string, input, output any, errMessage string, duration time.Duration) error {
	return t.store.AddToolCall(ctx, t.runID, stepID, toolName, input, output, errMessage, duration)
}

func (t *Tracer) RecordLLM(ctx context.Context, result llm.Result, prompt string) error {
	if result.Provider == "" {
		return nil
	}
	return t.store.AddLLMCall(ctx, t.runID, result.Provider, result.Model, prompt, result.Raw, result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.EstimatedCostUSD, result.Duration)
}
