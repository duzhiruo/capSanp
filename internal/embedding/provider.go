package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type TextType string

const (
	TextTypeDocument TextType = "document"
	TextTypeQuery    TextType = "query"
)

type Provider interface {
	Embed(ctx context.Context, text string, textType TextType) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string, textType TextType) ([][]float32, error)
	Dimension() int
}

// DashScope 通过 OpenAI 兼容 /v1/embeddings 端点生成向量。
type DashScope struct {
	baseURL    string
	apiKey     string
	model      string
	dimension  int
	httpClient *http.Client
}

func NewDashScope(baseURL, apiKey, model string, dimension int, timeout time.Duration) *DashScope {
	return &DashScope{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		dimension:  dimension,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (d *DashScope) Dimension() int { return d.dimension }

func (d *DashScope) Embed(ctx context.Context, text string, textType TextType) ([]float32, error) {
	results, err := d.EmbedBatch(ctx, []string{text}, textType)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding 返回空结果")
	}
	return results[0], nil
}

func (d *DashScope) EmbedBatch(ctx context.Context, texts []string, textType TextType) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if d.apiKey == "" {
		return mockBatchEmbed(texts, d.dimension), nil
	}

	body := map[string]any{
		"model":      d.model,
		"input":      texts,
		"dimensions": d.dimension,
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	var decoded embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("embedding 响应解析失败: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embedding API 错误 (%d): %s", resp.StatusCode, decoded.Error.Message)
	}

	results := make([][]float32, len(decoded.Data))
	for _, item := range decoded.Data {
		if item.Index < len(results) {
			vec := make([]float32, len(item.Embedding))
			for i, v := range item.Embedding {
				vec[i] = float32(v)
			}
			results[item.Index] = vec
		}
	}
	return results, nil
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Mock 是测试用的 Embedding Provider，返回确定性伪向量。
type Mock struct {
	dim int
}

func NewMock(dimension int) *Mock {
	return &Mock{dim: dimension}
}

func (m *Mock) Dimension() int { return m.dim }

func (m *Mock) Embed(_ context.Context, text string, _ TextType) ([]float32, error) {
	return deterministicVector(text, m.dim), nil
}

func (m *Mock) EmbedBatch(_ context.Context, texts []string, _ TextType) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		results[i] = deterministicVector(t, m.dim)
	}
	return results, nil
}

// deterministicVector 基于文本哈希生成确定性伪向量，相同文本始终返回相同向量。
func deterministicVector(text string, dim int) []float32 {
	vec := make([]float32, dim)
	h := uint32(0)
	for _, c := range text {
		h = h*31 + uint32(c)
	}
	for i := range vec {
		h ^= h << 13
		h ^= h >> 17
		h ^= h << 5
		vec[i] = float32(h%1000) / 1000.0
	}
	return vec
}

func mockBatchEmbed(texts []string, dim int) [][]float32 {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		results[i] = deterministicVector(t, dim)
	}
	return results
}
