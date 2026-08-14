package tools

import (
	"context"
	"strings"

	"capsnap/internal/llm"
	"capsnap/internal/store"
)

const MemoryToolName = "memory.save_screenshot_insight"

type MemoryTool struct {
	store store.Repository
}

func NewMemoryTool(store store.Repository) *MemoryTool {
	return &MemoryTool{store: store}
}

func (t *MemoryTool) Name() string {
	return MemoryToolName
}

func (t *MemoryTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	screenshotID, _ := input["screenshot_id"].(string)
	ocrText, _ := input["ocr_text"].(string)

	insight := llm.Insight{
		Summary:     asString(input["summary"]),
		Category:    asString(input["category"]),
		Explanation: asString(input["explanation"]),
	}
	if tags, ok := input["tags"].([]string); ok {
		insight.Tags = tags
	} else if tagsAny, ok := input["tags"].([]any); ok {
		for _, tag := range tagsAny {
			if value, ok := tag.(string); ok {
				insight.Tags = append(insight.Tags, value)
			}
		}
	}

	tagsText := strings.Join(insight.Tags, " ")
	if err := t.store.UpdateScreenshotInsight(ctx, screenshotID, "organized", ocrText, insight.Summary, insight.Category, tagsText, insight.Explanation); err != nil {
		return nil, err
	}
	return map[string]any{
		"screenshot_id": screenshotID,
		"category":      insight.Category,
		"tags_text":     tagsText,
	}, nil
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
