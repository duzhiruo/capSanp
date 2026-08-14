package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Insight struct {
	Summary     string   `json:"summary"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Explanation string   `json:"explanation"`
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	EstimatedCostUSD float64
}

type Result struct {
	Insight  Insight
	Raw      string
	Usage    Usage
	Provider string
	Model    string
	Duration time.Duration
}

type Provider interface {
	GenerateInsight(ctx context.Context, ocrText string) (Result, error)
}

type OpenAICompatible struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	inputCost  float64
	outputCost float64
}

func NewOpenAICompatible(baseURL, apiKey, model string, timeout time.Duration, inputCost, outputCost float64) *OpenAICompatible {
	return &OpenAICompatible{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: timeout},
		inputCost:  inputCost,
		outputCost: outputCost,
	}
}

func (p *OpenAICompatible) GenerateInsight(ctx context.Context, ocrText string) (Result, error) {
	if p.apiKey == "" {
		return MockInsight(ocrText), nil
	}
	prompt := BuildInsightPrompt(ocrText)
	body := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是 CapSnap 的截图整理 Agent。请只输出 JSON，不要输出 Markdown。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Result{}, err
	}
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("LLM 请求失败: %s", decoded.Error.Message)
	}
	raw := ""
	if len(decoded.Choices) > 0 {
		raw = decoded.Choices[0].Message.Content
	}
	insight, err := parseInsight(raw, ocrText)
	if err != nil {
		return Result{}, err
	}
	usage := Usage{
		PromptTokens:     decoded.Usage.PromptTokens,
		CompletionTokens: decoded.Usage.CompletionTokens,
	}
	usage.EstimatedCostUSD = float64(usage.PromptTokens)/1_000_000*p.inputCost + float64(usage.CompletionTokens)/1_000_000*p.outputCost
	return Result{
		Insight:  insight,
		Raw:      raw,
		Usage:    usage,
		Provider: "openai_compatible",
		Model:    p.model,
		Duration: time.Since(start),
	}, nil
}

func parseInsight(raw, ocrText string) (Insight, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	var insight Insight
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &insight); err != nil {
		fallback := MockInsight(ocrText)
		fallback.Raw = raw
		return fallback.Insight, nil
	}
	if insight.Summary == "" {
		insight.Summary = "这是一张需要进一步确认的截图"
	}
	if insight.Category == "" {
		insight.Category = "暂未分类"
	}
	if len(insight.Tags) == 0 {
		insight.Tags = []string{"待确认", "截图"}
	}
	return insight, nil
}

// MockInsight 生成降级用的 mock 结果（无真实 LLM 调用时使用）。
func MockInsight(ocrText string) Result {
	text := strings.TrimSpace(ocrText)
	if text == "" {
		text = "未识别到明确文字"
	}
	summary := text
	if len([]rune(summary)) > 30 {
		summary = string([]rune(summary)[:30]) + "..."
	}
	return Result{
		Insight: Insight{
			Summary:     "原型摘要：" + summary,
			Category:    "暂未分类",
			Tags:        []string{"原型", "待确认", "截图"},
			Explanation: "当前未配置真实 LLM，系统使用 Mock Provider 生成可观察的 Agent 结果。",
		},
		Raw:      `{"summary":"原型摘要","category":"暂未分类","tags":["原型","待确认","截图"],"explanation":"Mock Provider 结果"}`,
		Provider: "mock",
		Model:    "mock-insight",
	}
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
