package tools

import (
	"context"
	"fmt"
	"strings"

	"capsnap/internal/ocr"
	"capsnap/internal/store"
)

const OCRToolName = "ocr.extract_text"

type OCRTool struct {
	store store.Repository
	ocr   ocr.Provider
}

func NewOCRTool(store store.Repository, provider ocr.Provider) *OCRTool {
	return &OCRTool{store: store, ocr: provider}
}

func (t *OCRTool) Name() string {
	return OCRToolName
}

func (t *OCRTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	screenshotID, _ := input["screenshot_id"].(string)
	ocrHint, _ := input["ocr_hint"].(string)

	shot, err := t.store.GetScreenshot(ctx, screenshotID)
	if err != nil {
		return nil, err
	}

	text := strings.TrimSpace(ocrHint)
	if text == "" {
		text = strings.TrimSpace(shot.OCRText)
	}

	ocrErr := ""
	if text == "" && t.ocr != nil {
		result, err := t.ocr.Extract(ctx, shot.StoragePath)
		if err == nil {
			text = result.Text
		} else {
			ocrErr = err.Error()
		}
	}
	if text == "" {
		text = fmt.Sprintf("OCR 未能识别文字，当前使用文件名兜底：%s", shot.OriginalFilename)
	}

	return map[string]any{
		"ocr_text":  text,
		"ocr_error": ocrErr,
	}, nil
}
