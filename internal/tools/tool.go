package tools

import "context"

// Tool 表示 Agent 可调用的一个工具。
type Tool interface {
	Name() string
	Execute(ctx context.Context, input map[string]any) (map[string]any, error)
}
