package tools

import "fmt"

// Registry 维护 Agent 可用工具集合。
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(items ...Tool) *Registry {
	reg := &Registry{tools: make(map[string]Tool, len(items))}
	for _, item := range items {
		reg.tools[item.Name()] = item
	}
	return reg
}

func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("工具不存在: %s", name)
	}
	return tool, nil
}
