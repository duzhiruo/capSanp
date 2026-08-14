package agent

import (
	"context"
	"time"
)

// FailurePolicy 定义步骤失败时的处理策略。
type FailurePolicy int

const (
	FailAbort            FailurePolicy = iota // 中止整个 Run
	FailSkip                                  // 跳过此步骤继续
	FailContinueDegraded                      // 降级继续（使用 fallback 输出）
)

// RunContext 在步骤之间传递状态。
type RunContext struct {
	RunID       string
	Input       map[string]any
	StepOutputs map[string]map[string]any
	Degraded    bool
}

// StepResult 记录单个步骤的执行结果。
type StepResult struct {
	Name     string
	Status   string // completed / failed / skipped / degraded
	Output   map[string]any
	Error    string
	Attempts int
	Duration time.Duration
}

// StepDefinition 声明 Pipeline 中的一个步骤。
type StepDefinition struct {
	Name           string
	ToolName       string
	BuildInput     func(rc *RunContext) map[string]any
	Timeout        time.Duration
	MaxRetries     int
	RetryDelay     time.Duration
	OnFailure      FailurePolicy
	FallbackOutput func(rc *RunContext, err error) map[string]any
}

// AgentType 是一种已注册的 Agent 类型。
type AgentType struct {
	Name  string
	Steps []StepDefinition
}

// ExecuteResult 是 AgentRunner.Execute 的返回值。
type ExecuteResult struct {
	Status      string // completed / partial_success / failed
	Steps       []StepResult
	FinalOutput map[string]any
}

// stepTimeoutContext 为步骤创建超时 context（如果配置了 Timeout）。
func stepTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(parent, timeout)
	}
	return parent, func() {}
}
