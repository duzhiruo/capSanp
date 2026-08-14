package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"capsnap/internal/embedding"
	"capsnap/internal/vectorstore"
)

const EmbeddingToolName = "embedding.generate"

type EmbeddingTool struct {
	embedder embedding.Provider
	vectors  vectorstore.VectorStore
}

func NewEmbeddingTool(embedder embedding.Provider, vectors vectorstore.VectorStore) *EmbeddingTool {
	return &EmbeddingTool{embedder: embedder, vectors: vectors}
}

func (t *EmbeddingTool) Name() string {
	return EmbeddingToolName
}

func (t *EmbeddingTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	if t.embedder == nil || t.vectors == nil {
		return nil, fmt.Errorf("embedding 未配置")
	}

	screenshotID, _ := input["screenshot_id"].(string)
	if screenshotID == "" {
		return nil, fmt.Errorf("screenshot_id 不能为空")
	}
	deviceID, _ := input["device_id"].(string)
	ocrText := asString(input["ocr_text"])
	summary := asString(input["summary"])
	tags := tagsToString(input["tags"])

	text := buildEmbeddingText(summary, tags, ocrText)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("无可嵌入文本")
	}

	vec, err := t.embedder.Embed(ctx, text, embedding.TextTypeDocument)
	if err != nil {
		return nil, fmt.Errorf("生成 embedding 失败: %w", err)
	}

	payload := map[string]any{
		"screenshot_id": screenshotID,
		"device_id":     deviceID,
		"text_preview":  truncateRunes(text, 200),
	}
	if err := t.vectors.Upsert(ctx, screenshotID, vec, payload); err != nil {
		return nil, fmt.Errorf("写入向量库失败: %w", err)
	}

	return map[string]any{
		"embedded":      true,
		"dimension":     t.embedder.Dimension(),
		"screenshot_id": screenshotID,
	}, nil
}

func buildEmbeddingText(summary, tags, ocrText string) string {
	parts := make([]string, 0, 3)
	if s := strings.TrimSpace(summary); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(tags); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(ocrText); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

func tagsToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if tags, ok := v.([]string); ok {
		return strings.Join(tags, " ")
	}
	if tags, ok := v.([]any); ok {
		parts := make([]string, 0, len(tags))
		for _, tag := range tags {
			if s, ok := tag.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
