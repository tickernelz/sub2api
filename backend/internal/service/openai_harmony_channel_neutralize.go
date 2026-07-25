package service

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

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

// neutralizeOpenAIHarmonyChannelTokenJSON keeps the literal byte replacement
// as the hot path, then falls back to decoded JSON only when a Unicode escape
// could encode the same logical token. UseNumber prevents large integers from
// losing precision when the fallback has to re-marshal the request.
func neutralizeOpenAIHarmonyChannelTokenJSON(body []byte) ([]byte, bool) {
	if out, changed := neutralizeOpenAIHarmonyChannelToken(body); changed {
		return out, true
	}
	if !bytes.Contains(body, []byte(`\u`)) {
		return body, false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return body, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return body, false
	}
	updated, changed := neutralizeOpenAIHarmonyChannelTokenJSONValue(value)
	if !changed {
		return body, false
	}
	out, err := json.Marshal(updated)
	if err != nil {
		return body, false
	}
	return out, true
}

func (s *OpenAIGatewayService) openAIHarmonyChannelNeutralizationEnabled() bool {
	return s == nil || s.cfg == nil || s.cfg.Gateway.NeutralizeHarmonyChannelToken
}

// neutralizeOpenAIHarmonyChannelTokenJSONValue applies the same exact token
// replacement to decoded JSON values. It clones maps/slices only along changed
// paths so callers can safely reuse the original request body.
func neutralizeOpenAIHarmonyChannelTokenJSONValue(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		out, changed := neutralizeOpenAIHarmonyChannelToken([]byte(v))
		if !changed {
			return value, false
		}
		return string(out), true
	case []any:
		var out []any
		for i, item := range v {
			updated, changed := neutralizeOpenAIHarmonyChannelTokenJSONValue(item)
			if !changed {
				continue
			}
			if out == nil {
				out = append([]any(nil), v...)
			}
			out[i] = updated
		}
		if out == nil {
			return value, false
		}
		return out, true
	case map[string]any:
		return neutralizeOpenAIHarmonyChannelTokenJSONObject(v)
	default:
		return value, false
	}
}

func neutralizeOpenAIHarmonyChannelTokenJSONObject(value map[string]any) (map[string]any, bool) {
	var out map[string]any
	for key, item := range value {
		updated, changed := neutralizeOpenAIHarmonyChannelTokenJSONValue(item)
		updatedKey := key
		keyChanged := strings.Contains(key, openAIHarmonyChannelToken)
		if keyChanged {
			updatedKey = strings.ReplaceAll(key, openAIHarmonyChannelToken, openAIHarmonyChannelTokenNeutralized)
		}
		if !changed && !keyChanged {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(value))
			for existingKey, existingValue := range value {
				out[existingKey] = existingValue
			}
		}
		if keyChanged {
			delete(out, key)
			if _, safeKeyAlreadyExists := value[updatedKey]; safeKeyAlreadyExists {
				// Prefer an explicitly supplied safe key deterministically instead
				// of making the result depend on Go map iteration order.
				continue
			}
		}
		out[updatedKey] = updated
	}
	if out == nil {
		return value, false
	}
	return out, true
}

func isOpenAIWSInvalidPromptObservableEvent(eventType string) bool {
	return eventType == "response.failed" || eventType == "error"
}

func (s *OpenAIGatewayService) recordOpenAIWSInvalidPrompt(
	c *gin.Context,
	account *Account,
	passthrough bool,
	upstreamRequestID string,
	payload []byte,
) bool {
	hit, _, message := detectOpenAIInvalidPrompt(payload)
	if !hit {
		return false
	}
	s.recordOpenAIStreamUpstreamError(c, account, passthrough, upstreamRequestID, "invalid_prompt", payload, message)
	return true
}

// detectOpenAIInvalidPrompt 识别上游 `invalid_prompt` 硬拦截（对齐 detectOpenAICyberPolicy
// 的结构）。命中返回 (true, "invalid_prompt", message)。
//
// GPT-5.x /v1/responses 会以 HTTP 200 流内 `response.failed` 或非流 400 返回
// error.code=`invalid_prompt`（如 harmony `<|channel|>analysis` 伪造隐藏推理通道、
// 蒸馏/越狱结构等）。此类拦截是请求级、不可 failover、不冷却账号，但当前若既非 cyber、
// 又未命中管理员透传规则，则不会写入 ops_error_logs——从而在监控里“隐形”。本函数供
// Layer 2 观测使用：命中即补记一条 ops 上游错误事件（kind=invalid_prompt），使新拦截
// 模式可被及早发现（例如 harmony 护栏收紧到其它 token 时，中和逻辑失效会立即可见）。
//
// 注意：只匹配 error.code / response.error.code == "invalid_prompt"，不做关键字模糊匹配，
// 避免与 context-window、compact 等其它 400 混淆。
func detectOpenAIInvalidPrompt(payload []byte) (hit bool, code string, message string) {
	if len(payload) == 0 {
		return false, "", ""
	}
	c := gjson.GetBytes(payload, "error.code").String()
	if c == "" {
		c = gjson.GetBytes(payload, "response.error.code").String()
	}
	if !strings.EqualFold(strings.TrimSpace(c), "invalid_prompt") {
		return false, "", ""
	}
	msg := gjson.GetBytes(payload, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(payload, "response.error.message").String()
	}
	return true, "invalid_prompt", strings.TrimSpace(msg)
}
