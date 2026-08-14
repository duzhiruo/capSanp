package vectorstore

import (
	"context"
	"math"
	"sort"
	"sync"
)

type memPoint struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

type Memory struct {
	mu     sync.Mutex
	points map[string]memPoint
}

func NewMemory() *Memory {
	return &Memory{points: make(map[string]memPoint)}
}

func (m *Memory) EnsureCollection(_ context.Context) error { return nil }
func (m *Memory) Close() error                             { return nil }

func (m *Memory) Upsert(_ context.Context, id string, vector []float32, payload map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["screenshot_id"]; !ok {
		payload["screenshot_id"] = id
	}
	m.points[id] = memPoint{ID: id, Vector: vector, Payload: payload}
	return nil
}

func (m *Memory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.points, id)
	return nil
}

func (m *Memory) Search(_ context.Context, vector []float32, limit int, filter map[string]any) ([]SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	deviceID, _ := filter["device_id"].(string)

	type scored struct {
		id      string
		score   float32
		payload map[string]any
	}
	var items []scored
	for _, p := range m.points {
		if deviceID != "" {
			if d, _ := p.Payload["device_id"].(string); d != deviceID {
				continue
			}
		}
		s := cosineSimilarity(vector, p.Vector)
		items = append(items, scored{id: p.ID, score: s, payload: p.Payload})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	if limit > len(items) {
		limit = len(items)
	}
	results := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		results[i] = SearchResult{ID: items[i].id, Score: items[i].score, Payload: items[i].payload}
	}
	return results, nil
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
