package ocr

import "context"

// Provider 抽象 OCR 能力，便于后续替换实现。
type Provider interface {
	Extract(ctx context.Context, imagePath string) (Result, error)
}
