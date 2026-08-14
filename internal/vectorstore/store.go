package vectorstore

import "context"

type SearchResult struct {
	ID      string
	Score   float32
	Payload map[string]any
}

type VectorStore interface {
	Upsert(ctx context.Context, id string, vector []float32, payload map[string]any) error
	Search(ctx context.Context, vector []float32, limit int, filter map[string]any) ([]SearchResult, error)
	Delete(ctx context.Context, id string) error
	EnsureCollection(ctx context.Context) error
	Close() error
}
