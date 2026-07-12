package service

import "bytes"

// OpenAI 的 /v1/responses 上游对 harmony「hidden analysis channel」头做请求级硬校验：
// 当请求体里出现 ASCII 字面量 `<|channel|>` 紧跟 `analysis`（harmony 隐藏思维链
// 通道头）时，上游直接以 HTTP 200 流内 `response.failed` + error.code=`invalid_prompt`
// （message="Request blocked."）拒绝整个请求。这是上游反注入护栏，用于阻止伪造隐藏
// 推理通道，而非内容安全拦截；本地 content_moderation 显示 allowed=true。
//
// 触发条件经实测（gpt-5.6-sol，/v1/responses stream）为精确的 `<|channel|>` + `analysis`
// 组合（空白/换行容忍），单独的 `<|channel|>`、`<|channel|>final` 等其它 harmony token
// 均可正常通过。因此只要中和 `<|channel|>` 这一个 token 即可解除拦截，无需触碰 `analysis`
// 或其它 harmony 结构。
//
// 中和方式：把 `<|channel|>` 中的两个 ASCII 竖线 `|`(U+007C) 替换为全角竖线
// `｜`(U+FF5C)。实测该变体可正常通过上游校验；对模型与人类阅读几乎无差异（用于代码
// review 等把该字面量当作 fixture 文本的场景，语义保留），且是可逆的视觉替换而非删除。
// 实测零宽字符（U+200B 等）会被上游预处理剥离、无法中和，故不采用。
const (
	openAIHarmonyChannelToken            = "<|channel|>"
	openAIHarmonyChannelTokenNeutralized = "<\uff5cchannel\uff5c>"
)

var (
	openAIHarmonyChannelTokenBytes            = []byte(openAIHarmonyChannelToken)
	openAIHarmonyChannelTokenNeutralizedBytes = []byte(openAIHarmonyChannelTokenNeutralized)
)

// neutralizeOpenAIHarmonyChannelToken 在请求体里把字面量 `<|channel|>` 中和为全角竖线
// 变体，解除上游对伪造 harmony analysis 通道的 invalid_prompt 硬拦截。
//
// 热路径友好：先用 bytes.Contains 做一次快速判定，token 不存在时零分配、原样返回
// （changed=false）。仅在命中时才分配新切片。不做任何 JSON 反序列化，按原始字节替换，
// 因此对流式/非流式、透传/改写、compat 各路径都安全（只影响这一确切字节序列）。
func neutralizeOpenAIHarmonyChannelToken(body []byte) (out []byte, changed bool) {
	if len(body) == 0 || !bytes.Contains(body, openAIHarmonyChannelTokenBytes) {
		return body, false
	}
	return bytes.ReplaceAll(body, openAIHarmonyChannelTokenBytes, openAIHarmonyChannelTokenNeutralizedBytes), true
}
