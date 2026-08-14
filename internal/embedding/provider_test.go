package embedding

import (
	"context"
	"testing"
)

func TestMock_Embed(t *testing.T) {
	p := NewMock(1024)
	if p.Dimension() != 1024 {
		t.Errorf("Dimension() = %d, want 1024", p.Dimension())
	}

	vec, err := p.Embed(context.Background(), "hello world", TextTypeDocument)
	if err != nil {
		t.Fatalf("Embed 失败: %v", err)
	}
	if len(vec) != 1024 {
		t.Errorf("向量维度 = %d, want 1024", len(vec))
	}

	// 相同文本应产生相同向量
	vec2, _ := p.Embed(context.Background(), "hello world", TextTypeDocument)
	for i := range vec {
		if vec[i] != vec2[i] {
			t.Fatalf("相同文本的向量不一致，index %d: %f != %f", i, vec[i], vec2[i])
		}
	}
}

func TestMock_EmbedBatch(t *testing.T) {
	p := NewMock(512)
	texts := []string{"text1", "text2", "text3"}
	results, err := p.EmbedBatch(context.Background(), texts, TextTypeDocument)
	if err != nil {
		t.Fatalf("EmbedBatch 失败: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("结果数 = %d, want 3", len(results))
	}
	for i, vec := range results {
		if len(vec) != 512 {
			t.Errorf("results[%d] 维度 = %d, want 512", i, len(vec))
		}
	}
}

func TestMock_DifferentTextsDifferentVectors(t *testing.T) {
	p := NewMock(128)
	v1, _ := p.Embed(context.Background(), "截图整理", TextTypeDocument)
	v2, _ := p.Embed(context.Background(), "竞品定价", TextTypeDocument)

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("不同文本应产生不同向量")
	}
}

func TestDashScope_NoAPIKey_ReturnsMock(t *testing.T) {
	p := NewDashScope("https://example.com/v1", "", "text-embedding-v3", 1024, 0)
	vec, err := p.Embed(context.Background(), "test", TextTypeDocument)
	if err != nil {
		t.Fatalf("无 API Key 应 fallback 到 mock: %v", err)
	}
	if len(vec) != 1024 {
		t.Errorf("向量维度 = %d, want 1024", len(vec))
	}
}
