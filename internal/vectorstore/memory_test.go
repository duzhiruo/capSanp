package vectorstore

import (
	"context"
	"math"
	"testing"
)

func TestMemory_UpsertAndSearch(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	_ = m.Upsert(ctx, "a", []float32{1, 0, 0, 0}, map[string]any{"label": "right"})
	_ = m.Upsert(ctx, "b", []float32{0, 1, 0, 0}, map[string]any{"label": "up"})
	_ = m.Upsert(ctx, "c", []float32{0.9, 0.1, 0, 0}, map[string]any{"label": "near-right"})

	results, err := m.Search(ctx, []float32{1, 0, 0, 0}, 2, nil)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("最相似应为 'a'，实际 '%s'", results[0].ID)
	}
	if results[1].ID != "c" {
		t.Errorf("第二相似应为 'c'，实际 '%s'", results[1].ID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("完全匹配 score 应接近 1.0，实际 %f", results[0].Score)
	}
}

func TestMemory_FilterByDevice(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.Upsert(ctx, "a", []float32{1, 0}, map[string]any{"device_id": "d1"})
	_ = m.Upsert(ctx, "b", []float32{0.99, 0.01}, map[string]any{"device_id": "d2"})

	results, err := m.Search(ctx, []float32{1, 0}, 10, map[string]any{"device_id": "d1"})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("应按 device_id 过滤，实际 %#v", results)
	}
}

func TestMemory_Delete(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	_ = m.Upsert(ctx, "x", []float32{1, 0}, nil)
	_ = m.Delete(ctx, "x")

	results, _ := m.Search(ctx, []float32{1, 0}, 10, nil)
	if len(results) != 0 {
		t.Errorf("删除后应无结果，实际 %d", len(results))
	}
}

func TestMemory_SearchEmpty(t *testing.T) {
	m := NewMemory()
	results, err := m.Search(context.Background(), []float32{1, 0}, 10, nil)
	if err != nil {
		t.Fatalf("空库搜索不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("空库应返回 0 结果，实际 %d", len(results))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.want)) > 0.001 {
				t.Errorf("cosineSimilarity = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestPointUUID_Deterministic(t *testing.T) {
	a := PointUUID("shot_abc")
	b := PointUUID("shot_abc")
	c := PointUUID("shot_xyz")
	if a != b {
		t.Error("相同 ID 应生成相同 UUID")
	}
	if a == c {
		t.Error("不同 ID 应生成不同 UUID")
	}
	if len(a) != 36 {
		t.Errorf("UUID 长度应为 36，实际 %d (%s)", len(a), a)
	}
}
