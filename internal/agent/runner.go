package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"capsnap/internal/llm"
	"capsnap/internal/reqctx"
	"capsnap/internal/store"
	"capsnap/internal/tools"
)

// AgentRunner 是通用 Agent 执行器，按注册的 AgentType 声明式执行 Pipeline。
type AgentRunner struct {
	store    store.Repository
	registry *tools.Registry
	types    map[string]*AgentType
}

func NewAgentRunner(s store.Repository, r *tools.Registry) *AgentRunner {
	return &AgentRunner{
		store:    s,
		registry: r,
		types:    make(map[string]*AgentType),
	}
}

func (ar *AgentRunner) RegisterType(at *AgentType) {
	ar.types[at.Name] = at
}

// Execute 执行一次 Agent Run，由 Worker 在后台调用。
// 调用方负责在 Execute 前已创建好 agent_run 记录（status=queued）。
func (ar *AgentRunner) Execute(ctx context.Context, runID, agentType string, input map[string]any) (*ExecuteResult, error) {
	at, ok := ar.types[agentType]
	if !ok {
		return nil, fmt.Errorf("未注册的 Agent 类型: %s", agentType)
	}

	start := time.Now()
	_ = ar.store.FinishAgentRun(ctx, runID, "running", nil, "", 0)

	rc := &RunContext{
		RunID:       runID,
		Input:       input,
		StepOutputs: make(map[string]map[string]any),
	}
	tracer := NewTracer(ar.store, runID)
	result := &ExecuteResult{}

	for _, step := range at.Steps {
		sr := ar.executeStep(ctx, tracer, rc, step)
		result.Steps = append(result.Steps, sr)

		if sr.Status == "failed" {
			result.Status = "failed"
			result.FinalOutput = collectFinalOutput(rc)
			_ = ar.store.FinishAgentRun(ctx, runID, "failed", result.FinalOutput, sr.Error, time.Since(start))
			return result, fmt.Errorf("步骤 %s 失败: %s", sr.Name, sr.Error)
		}
	}

	if rc.Degraded {
		result.Status = "partial_success"
	} else {
		result.Status = "completed"
	}
	result.FinalOutput = collectFinalOutput(rc)
	_ = ar.store.FinishAgentRun(ctx, runID, result.Status, result.FinalOutput, "", time.Since(start))
	return result, nil
}

func (ar *AgentRunner) executeStep(ctx context.Context, tracer *Tracer, rc *RunContext, step StepDefinition) StepResult {
	tool, err := ar.registry.Get(step.ToolName)
	if err != nil {
		return ar.handleStepFailure(rc, step, err, 0, 0)
	}

	stepInput := step.BuildInput(rc)
	var output map[string]any
	var lastErr error
	stepStart := time.Now()
	attempts := 0

	for attempt := 0; attempt <= step.MaxRetries; attempt++ {
		if attempt > 0 {
			slog.Info("步骤重试", "step", step.Name, "attempt", attempt, "run_id", rc.RunID, "request_id", reqctx.Get(ctx))
			time.Sleep(step.RetryDelay)
		}
		attempts = attempt + 1

		stepCtx, cancel := stepTimeoutContext(ctx, step.Timeout)
		output, lastErr = tool.Execute(stepCtx, stepInput)
		cancel()

		if lastErr == nil {
			break
		}
	}

	duration := time.Since(stepStart)

	if lastErr != nil {
		sr := ar.handleStepFailure(rc, step, lastErr, attempts, duration)
		ar.recordStepTrace(ctx, tracer, step, stepInput, rc.StepOutputs[step.Name], sr, duration)
		return sr
	}

	rc.StepOutputs[step.Name] = output
	sr := StepResult{
		Name:     step.Name,
		Status:   "completed",
		Output:   output,
		Attempts: attempts,
		Duration: duration,
	}
	ar.recordStepTrace(ctx, tracer, step, stepInput, output, sr, duration)
	return sr
}

func (ar *AgentRunner) handleStepFailure(rc *RunContext, step StepDefinition, err error, attempts int, duration time.Duration) StepResult {
	switch step.OnFailure {
	case FailSkip:
		rc.StepOutputs[step.Name] = map[string]any{}
		return StepResult{
			Name: step.Name, Status: "skipped", Error: err.Error(),
			Attempts: attempts, Duration: duration,
		}
	case FailContinueDegraded:
		var fallback map[string]any
		if step.FallbackOutput != nil {
			fallback = step.FallbackOutput(rc, err)
		} else {
			fallback = map[string]any{}
		}
		rc.StepOutputs[step.Name] = fallback
		rc.Degraded = true
		return StepResult{
			Name: step.Name, Status: "degraded", Output: fallback, Error: err.Error(),
			Attempts: attempts, Duration: duration,
		}
	default: // FailAbort
		return StepResult{
			Name: step.Name, Status: "failed", Error: err.Error(),
			Attempts: attempts, Duration: duration,
		}
	}
}

func (ar *AgentRunner) recordStepTrace(ctx context.Context, tracer *Tracer, step StepDefinition, input map[string]any, output map[string]any, sr StepResult, duration time.Duration) {
	stepID, err := tracer.RecordStep(ctx, sr.Name, sr.Status, input, output, sr.Error, sr.Duration)
	if err != nil {
		slog.Error("记录步骤追踪失败", "step", sr.Name, "error", err, "request_id", reqctx.Get(ctx))
		return
	}
	_ = tracer.RecordTool(ctx, stepID, step.ToolName, input, output, sr.Error, sr.Duration)

	if llmResult, ok := output["_llm_result"].(llm.Result); ok {
		ocrText, _ := input["ocr_text"].(string)
		_ = tracer.RecordLLM(ctx, llmResult, llm.BuildInsightPrompt(ocrText))
	}
}

func collectFinalOutput(rc *RunContext) map[string]any {
	result := make(map[string]any)
	for stepName, output := range rc.StepOutputs {
		for k, v := range output {
			if len(k) == 0 || k[0] == '_' {
				continue
			}
			result[stepName+"."+k] = v
		}
	}
	return result
}
