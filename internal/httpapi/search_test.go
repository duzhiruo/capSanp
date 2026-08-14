package httpapi

import (
	"testing"
)

func TestRRFMerge(t *testing.T) {
	kw := []rankedID{
		{ID: "a", Rank: 1},
		{ID: "b", Rank: 2},
		{ID: "c", Rank: 3},
	}
	sem := []rankedID{
		{ID: "c", Rank: 1},
		{ID: "d", Rank: 2},
		{ID: "a", Rank: 3},
	}
	merged := rrfMerge(kw, sem, 60, 10)
	if len(merged) != 4 {
		t.Fatalf("期望 4 个结果，实际 %d: %v", len(merged), merged)
	}
	// a 在两边都靠前，c 在语义第一，通常 a 或 c 应排最前
	if merged[0] != "a" && merged[0] != "c" {
		t.Errorf("Top1 应为 a 或 c，实际 %s", merged[0])
	}
	seen := map[string]bool{}
	for _, id := range merged {
		seen[id] = true
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if !seen[id] {
			t.Errorf("缺少 ID %s", id)
		}
	}
}

func TestRRFMerge_Limit(t *testing.T) {
	a := []rankedID{{ID: "x", Rank: 1}, {ID: "y", Rank: 2}}
	b := []rankedID{{ID: "z", Rank: 1}}
	merged := rrfMerge(a, b, 60, 2)
	if len(merged) != 2 {
		t.Fatalf("limit=2 应返回 2，实际 %d", len(merged))
	}
}

func TestRRFMerge_Empty(t *testing.T) {
	merged := rrfMerge(nil, nil, 60, 10)
	if len(merged) != 0 {
		t.Errorf("空输入应返回空，实际 %v", merged)
	}
}
