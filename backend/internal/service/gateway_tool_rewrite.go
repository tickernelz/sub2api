package service

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// toolNameRewriteKey 是 gin.Context 上存 ToolNameRewrite 映射的 key。
// 请求阶段写入，响应阶段读取，用于 bytes 级逆向还原假名 → 真名。
const toolNameRewriteKey = "claude_tool_name_rewrite"

// staticToolNameRewrites 是"静态前缀映射"，与 Parrot src/transform/cc_mimicry.py
// TOOL_NAME_REWRITES 完全一致。只有以这些前缀开头的工具会被重写。
var staticToolNameRewrites = map[string]string{
	"sessions_": "cc_sess_",
	"session_":  "cc_ses_",
}

// fakeToolNamePrefixes 是"动态映射"的前缀池，与 Parrot _FAKE_PREFIXES 一致。
// 当 tools 数量 > dynamicToolMapThreshold 时随机选用其中前缀生成可读假名。
var fakeToolNamePrefixes = []string{
	"analyze_", "compute_", "fetch_", "generate_", "lookup_", "modify_",
	"process_", "query_", "render_", "resolve_", "sync_", "update_",
	"validate_", "convert_", "extract_", "manage_", "monitor_", "parse_",
	"review_", "search_", "transform_", "handle_", "invoke_", "notify_",
}

// dynamicToolMapThreshold 与 Parrot 一致：tools 数量超过 5 才启用动态映射。
// 少量工具不需要混淆（一般是 Claude Code 自己的核心工具 bash/edit/read 等）。
const dynamicToolMapThreshold = 5

// ToolNameRewrite 是单次请求内的工具名混淆映射。
//   - Forward: real → fake，请求阶段在 body 上应用。
//   - Reverse: fake → real，响应阶段对每个 chunk 做 bytes.Replace 还原。
//
// ReverseOrdered 是按假名长度倒序的 (fake, real) 列表，用于防止短假名是长假名的
// 子串时 bytes.Replace 先被吃掉（对齐 Parrot _restore_tool_names_in_chunk 的
// `sorted(..., key=lambda x: len(x[1]), reverse=True)`）。
type ToolNameRewrite struct {
	Forward        map[string]string
	Reverse        map[string]string
	ReverseOrdered [][2]string
}

// buildDynamicToolMap 构造 tools 的动态假名映射。
//
// 与 Parrot _build_dynamic_tool_map 语义等价：
//   - tools 数量 ≤ dynamicToolMapThreshold 时返回 nil（不做动态映射，走静态 fallback）
//   - 同一组 tool_names 在同进程内映射稳定（保证 cache 命中）
//
// Parrot 用 `random.Random(hash(tuple(tool_names)))` 作 seed + shuffle 前缀池；
// Go 无法字节级复刻 Python hash，但"稳定性"和"前缀池打散"两个不变量都保留：
// 用 fnv64a(strings.Join(names, "\x00")) 作 seed 喂 math/rand.New。
// 字节级不同不影响上游判定（Anthropic 不会验证我们的随机种子算法）。
func buildDynamicToolMap(toolNames []string) map[string]string {
	if len(toolNames) <= dynamicToolMapThreshold {
		return nil
	}
	h := fnv.New64a()
	for i, n := range toolNames {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(n))
	}
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	available := make([]string, len(fakeToolNamePrefixes))
	copy(available, fakeToolNamePrefixes)
	rng.Shuffle(len(available), func(i, j int) { available[i], available[j] = available[j], available[i] })

	mapping := make(map[string]string, len(toolNames))
	for i, name := range toolNames {
		prefix := available[i%len(available)]
		headLen := 3
		if len(name) < 3 {
			headLen = len(name)
		}
		fake := fmt.Sprintf("%s%s%02d", prefix, name[:headLen], i)
		mapping[name] = fake
	}
	return mapping
}

// sanitizeToolName 把真名转成假名。
// 与 Parrot _sanitize_tool_name 语义一致：动态映射优先，再走静态前缀映射。
func sanitizeToolName(name string, dynamic map[string]string) string {
	if dynamic != nil {
		if fake, ok := dynamic[name]; ok {
			return fake
		}
	}
	for prefix, replacement := range staticToolNameRewrites {
		if strings.HasPrefix(name, prefix) {
			return replacement + name[len(prefix):]
		}
	}
	return name
}

// shouldMimicToolName 指示某个 tool 是否需要重命名。
// server tool（type != "" 且不是 "function" / "custom"）是 Anthropic 协议语义的一部分，
// 比如 "web_search_20250305" / "computer_20250124"；误改会导致上游拒绝。
func shouldMimicToolName(toolType string) bool {
	if toolType == "" || toolType == "function" || toolType == "custom" {
		return true
	}
	return false
}

// buildToolNameRewriteFromBody 扫描 body 的 tools[*].name，构造 ToolNameRewrite
// 并返回它。若不需要混淆（tools 数量不足 + 没有匹配静态前缀的工具）返回 nil。
//
// 注意：只扫描，不改 body。真正的 body 改写在 applyToolNameRewriteToBody。
func buildToolNameRewriteFromBody(body []byte) *ToolNameRewrite {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return nil
	}

	mimicableNames := make([]string, 0)
	toolsArr := tools.Array()
	for _, t := range toolsArr {
		if !shouldMimicToolName(t.Get("type").String()) {
			continue
		}
		name := t.Get("name").String()
		if name == "" {
			continue
		}
		mimicableNames = append(mimicableNames, name)
	}

	dynamic := buildDynamicToolMap(mimicableNames)

	rw := &ToolNameRewrite{
		Forward: make(map[string]string),
		Reverse: make(map[string]string),
	}
	for _, name := range mimicableNames {
		fake := sanitizeToolName(name, dynamic)
		if fake == name {
			continue
		}
		rw.Forward[name] = fake
		rw.Reverse[fake] = name
	}
	if len(rw.Forward) == 0 {
		return nil
	}

	rw.ReverseOrdered = make([][2]string, 0, len(rw.Reverse))
	for fake, real := range rw.Reverse {
		rw.ReverseOrdered = append(rw.ReverseOrdered, [2]string{fake, real})
	}
	sort.SliceStable(rw.ReverseOrdered, func(i, j int) bool {
		return len(rw.ReverseOrdered[i][0]) > len(rw.ReverseOrdered[j][0])
	})

	return rw
}

// applyToolNameRewriteToBody 把已构造的 ToolNameRewrite 应用到 body 上：
//
//   - 改写 $.tools[*].name（仅对 shouldMimicToolName 通过的 tool）
//   - 改写 $.tool_choice.name（仅当 $.tool_choice.type == "tool"）
//   - 改写 $.messages[*].content[*].name（仅当 type == "tool_use"）
//   - 在 $.tools[last].cache_control 上打 ephemeral 缓存断点
//
// 响应侧 bytes.Replace 会连带还原假名 → 真名。
func applyToolNameRewriteToBody(body []byte, rw *ToolNameRewrite) []byte {
	if rw == nil || len(rw.Forward) == 0 {
		body = applyToolsLastCacheBreakpoint(body)
		return body
	}

	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() {
		idx := -1
		tools.ForEach(func(_, t gjson.Result) bool {
			idx++
			if !shouldMimicToolName(t.Get("type").String()) {
				return true
			}
			name := t.Get("name").String()
			if name == "" {
				return true
			}
			fake, ok := rw.Forward[name]
			if !ok {
				return true
			}
			if next, err := sjson.SetBytes(body, fmt.Sprintf("tools.%d.name", idx), fake); err == nil {
				body = next
			}
			return true
		})
	}

	if tc := gjson.GetBytes(body, "tool_choice"); tc.Exists() && tc.Get("type").String() == "tool" {
		name := tc.Get("name").String()
		if fake, ok := rw.Forward[name]; ok {
			if next, err := sjson.SetBytes(body, "tool_choice.name", fake); err == nil {
				body = next
			}
		}
	}

	// 同步改写历史消息中的 tool_use.name，确保它和 tools[] 中的假名一致。
	// 否则 Anthropic 会因为 tool_use 引用了未声明的原始工具名而拒绝请求。
	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		messages.ForEach(func(msgKey, msg gjson.Result) bool {
			msgIdx := int(msgKey.Num)
			content := msg.Get("content")
			if !content.IsArray() {
				return true
			}
			content.ForEach(func(blkKey, blk gjson.Result) bool {
				blkIdx := int(blkKey.Num)
				if blk.Get("type").String() != "tool_use" {
					return true
				}
				name := blk.Get("name").String()
				if name == "" {
					return true
				}
				if fake, ok := rw.Forward[name]; ok {
					path := fmt.Sprintf("messages.%d.content.%d.name", msgIdx, blkIdx)
					if next, err := sjson.SetBytes(body, path, fake); err == nil {
						body = next
					}
				}
				return true
			})
			return true
		})
	}

	body = applyToolsLastCacheBreakpoint(body)
	return body
}

// applyToolsLastCacheBreakpoint 在最后一个非延迟加载工具上注入 cache_control
// 断点。Anthropic 不允许 defer_loading=true 的工具携带 cache_control，
// 因此会先清理所有延迟加载工具上的客户端断点。兼容官方顶层字段和
// Claude Code 使用的 custom.defer_loading 字段。其余行为对齐 Parrot
// `tools[-1]["cache_control"] = {"type":"ephemeral","ttl":"1h"}`，
// 但 ttl 按本仓规则：
//   - 客户端已为该 tool 显式设置 cache_control.ttl → 完全透传不覆盖
//   - 否则注入 {"type":"ephemeral","ttl": claude.DefaultCacheControlTTL}
//
// 纯副作用函数，tools 不存在或为空数组时 no-op。
func applyToolsLastCacheBreakpoint(body []byte) []byte {
	body = stripDeferredToolCacheControl(body)
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body
	}
	arr := tools.Array()
	if len(arr) == 0 {
		return body
	}
	lastIdx := -1
	for idx, tool := range arr {
		if isDeferredLoadingTool(tool) {
			continue
		}
		lastIdx = idx
	}
	if lastIdx == -1 {
		return body
	}

	existingCC := arr[lastIdx].Get("cache_control")

	if existingCC.Exists() && existingCC.Get("ttl").String() != "" {
		return body
	}

	if existingCC.Exists() {
		if next, err := sjson.SetBytes(body, fmt.Sprintf("tools.%d.cache_control.ttl", lastIdx), claude.DefaultCacheControlTTL); err == nil {
			body = next
		}
		return body
	}

	raw := fmt.Sprintf(`{"type":"ephemeral","ttl":%q}`, claude.DefaultCacheControlTTL)
	if next, err := sjson.SetRawBytes(body, fmt.Sprintf("tools.%d.cache_control", lastIdx), []byte(raw)); err == nil {
		body = next
	}
	return body
}

func isDeferredLoadingTool(tool gjson.Result) bool {
	return tool.Get("defer_loading").Type == gjson.True ||
		tool.Get("custom.defer_loading").Type == gjson.True
}

// stripDeferredToolCacheControl removes the cache marker Anthropic rejects on
// deferred tools. Only the literal JSON boolean true enables deferred loading.
func stripDeferredToolCacheControl(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body
	}
	for idx, tool := range tools.Array() {
		if !isDeferredLoadingTool(tool) || !tool.Get("cache_control").Exists() {
			continue
		}
		if next, err := sjson.DeleteBytes(body, fmt.Sprintf("tools.%d.cache_control", idx)); err == nil {
			body = next
		}
	}
	return body
}

// restoreToolNamesInBytes 对 bytes chunk 做逆向还原：假名 → 真名。
// 按 ReverseOrdered 的假名长度倒序逐个 bytes.Replace，防止子串冲突
// （与 Parrot _restore_tool_names_in_chunk 的 sorted(..., reverse=True) 等价）。
// 再做静态前缀还原（cc_sess_ → sessions_ / cc_ses_ → session_）。
//
// rw 可为 nil；nil 时仍会做静态前缀还原。
func restoreToolNamesInBytes(data []byte, rw *ToolNameRewrite) []byte {
	if rw != nil {
		for _, pair := range rw.ReverseOrdered {
			fake, real := pair[0], pair[1]
			if fake == "" || fake == real {
				continue
			}
			data = replaceAllBytes(data, fake, real)
		}
	}
	for prefix, replacement := range staticToolNameRewrites {
		data = replaceAllBytes(data, replacement, prefix)
	}
	return data
}

// replaceAllBytes 是 bytes.ReplaceAll 的便捷封装，避免每个调用点各自做 []byte 转换。
func replaceAllBytes(data []byte, from, to string) []byte {
	if len(data) == 0 || from == to || !strings.Contains(string(data), from) {
		return data
	}
	return []byte(strings.ReplaceAll(string(data), from, to))
}

// toolNameStreamRestorer 在 input_json_delta 分片之间还原假名。
//
// restoreToolNamesInBytes 按单行 SSE 做替换，因此写在 tool_use 顶层 name 上的假名
// 总能命中。但嵌套工具名（batch 的 tool_calls[*].tool）位于工具入参里，入参以
// input_json_delta 分片流式下发，一个假名会被切在两个分片中间，任一分片都不包含
// 完整假名，替换失效后假名原样到达客户端。
//
// 本类型按 content block 维度保留一小段尾巴：凡是可能构成某个假名前缀的后缀都暂不
// 下发，与下一个分片拼接后再还原。尾巴长度上界是最长假名减一，因此延迟有界。
// content_block_stop 前必须调用 Flush 取回残留，否则尾巴会被吞掉。
type toolNameStreamRestorer struct {
	rw     *ToolNameRewrite
	carry  map[int]string
	maxLen int
}

func newToolNameStreamRestorer(rw *ToolNameRewrite) *toolNameStreamRestorer {
	maxLen := 0
	if rw != nil {
		for fake := range rw.Reverse {
			if len(fake) > maxLen {
				maxLen = len(fake)
			}
		}
	}
	for _, replacement := range staticToolNameRewrites {
		if len(replacement) > maxLen {
			maxLen = len(replacement)
		}
	}
	return &toolNameStreamRestorer{rw: rw, carry: make(map[int]string), maxLen: maxLen}
}

// isPrefixOfAnyFakeName 判断 s 是否是某个假名的真前缀。
func (r *toolNameStreamRestorer) isPrefixOfAnyFakeName(s string) bool {
	if r.rw != nil {
		for fake := range r.rw.Reverse {
			if len(s) < len(fake) && strings.HasPrefix(fake, s) {
				return true
			}
		}
	}
	for _, replacement := range staticToolNameRewrites {
		if len(s) < len(replacement) && strings.HasPrefix(replacement, s) {
			return true
		}
	}
	return false
}

// splitEmittableTail 把已还原文本切成"可下发部分"和"需暂存的尾巴"。
func (r *toolNameStreamRestorer) splitEmittableTail(s string) (string, string) {
	limit := r.maxLen - 1
	if limit > len(s) {
		limit = len(s)
	}
	for n := limit; n > 0; n-- {
		if r.isPrefixOfAnyFakeName(s[len(s)-n:]) {
			return s[:len(s)-n], s[len(s)-n:]
		}
	}
	return s, ""
}

// RestoreFragment 处理单个 input_json_delta 分片，返回应当下发的文本。
func (r *toolNameStreamRestorer) RestoreFragment(blockIndex int, fragment string) string {
	combined := r.carry[blockIndex] + fragment
	restored := string(restoreToolNamesInBytes([]byte(combined), r.rw))
	emit, tail := r.splitEmittableTail(restored)
	if tail == "" {
		delete(r.carry, blockIndex)
	} else {
		r.carry[blockIndex] = tail
	}
	return emit
}

// Flush 取回并清空某个 content block 的残留尾巴。
func (r *toolNameStreamRestorer) Flush(blockIndex int) string {
	tail := r.carry[blockIndex]
	delete(r.carry, blockIndex)
	if tail == "" {
		return ""
	}
	return string(restoreToolNamesInBytes([]byte(tail), r.rw))
}

// RestoreSSELine 处理一整行 SSE。
//
// input_json_delta 行走跨分片还原：把 partial_json 交给 RestoreFragment，用还原后的
// 文本重写该字段。分片被完全暂存时返回 emit=false，调用方应丢弃该行（其内容会随
// 后续分片或 content_block_stop 的 flush 下发）。
//
// content_block_stop 行先取回残留尾巴，作为一条补充 input_json_delta 事件在 stop
// 之前下发，保证入参 JSON 完整。其余行按原有单行语义还原。
func (r *toolNameStreamRestorer) RestoreSSELine(line string) (out string, extraBefore string, emit bool) {
	const dataPrefix = "data: "
	if !strings.HasPrefix(line, dataPrefix) {
		return string(restoreToolNamesInBytes([]byte(line), r.rw)), "", true
	}
	payload := line[len(dataPrefix):]

	switch gjson.Get(payload, "type").String() {
	case "content_block_delta":
		if gjson.Get(payload, "delta.type").String() != "input_json_delta" {
			return string(restoreToolNamesInBytes([]byte(line), r.rw)), "", true
		}
		idx := int(gjson.Get(payload, "index").Int())
		restored := r.RestoreFragment(idx, gjson.Get(payload, "delta.partial_json").String())
		if restored == "" {
			return "", "", false
		}
		next, err := sjson.Set(payload, "delta.partial_json", restored)
		if err != nil {
			return string(restoreToolNamesInBytes([]byte(line), r.rw)), "", true
		}
		return dataPrefix + next, "", true

	case "content_block_stop":
		idx := int(gjson.Get(payload, "index").Int())
		tail := r.Flush(idx)
		if tail == "" {
			return string(restoreToolNamesInBytes([]byte(line), r.rw)), "", true
		}
		raw := fmt.Sprintf(
			`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
			idx, strconv.Quote(tail),
		)
		return string(restoreToolNamesInBytes([]byte(line), r.rw)),
			"event: content_block_delta\n" + dataPrefix + raw,
			true
	}

	return string(restoreToolNamesInBytes([]byte(line), r.rw)), "", true
}

// toolNameRewriteFromContext 从 gin.Context 取出请求阶段保存的工具名映射。
// 找不到（c==nil 或 key 不存在或类型不对）时返回 nil；调用方必须能处理 nil。
func toolNameRewriteFromContext(c interface {
	Get(string) (any, bool)
}) *ToolNameRewrite {
	if c == nil {
		return nil
	}
	raw, ok := c.Get(toolNameRewriteKey)
	if !ok || raw == nil {
		return nil
	}
	rw, _ := raw.(*ToolNameRewrite)
	return rw
}

// reverseToolNamesIfPresent 是响应侧 5 处注入点的统一封装：从 c 取出 mapping
// 并对 chunk 做 bytes 级假名→真名替换。c 没有 mapping 时仍会做静态前缀还原。
func reverseToolNamesIfPresent(c interface {
	Get(string) (any, bool)
}, chunk []byte) []byte {
	rw := toolNameRewriteFromContext(c)
	if rw == nil && len(staticToolNameRewrites) == 0 {
		return chunk
	}
	return restoreToolNamesInBytes(chunk, rw)
}
