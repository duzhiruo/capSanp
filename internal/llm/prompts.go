package llm

import "fmt"

const InsightPromptVersion = "v1"

// BuildInsightPrompt 生成截图整理提示词。
func BuildInsightPrompt(ocrText string) string {
	return fmt.Sprintf(`请根据下面的截图 OCR 文本，生成截图整理结果。

要求：
1. summary 不超过 60 个中文字符。
2. category 从这些类别中选择一个：聊天记录、文档资料、产品参考、购物消费、数据报表、灵感收藏、暂未分类。
3. tags 返回 2 到 6 个短标签。
4. explanation 用一句话解释为什么这样分类。
5. 如果 OCR 文本里出现“未接入 OCR”“未识别”等描述，请把它当作截图内容本身，不要误判为当前系统状态。
6. 只输出 JSON，格式为：
{"summary":"...","category":"...","tags":["..."],"explanation":"..."}

OCR 文本：
%s`, ocrText)
}
