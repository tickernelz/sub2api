package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayService_AnthropicPassthrough_NestedToolNameSurvivesSplitDeltas(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/1.0.0")

	toolNames := []string{"bash", "read", "write", "edit", "batch", "agentgrep", "todo"}
	toolDecls := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		toolDecls = append(toolDecls, `{"name":"`+name+`","input_schema":{"type":"object"}}`)
	}
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":true,"tools":[` +
		strings.Join(toolDecls, ",") +
		`],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)

	rw := buildToolNameRewriteFromBody(body)
	require.NotNil(t, rw, "more than five tools must trigger dynamic tool-name mimicry")
	fakeBash := rw.Forward["bash"]
	require.NotEmpty(t, fakeBash)
	require.NotEqual(t, "bash", fakeBash)
	c.Set(toolNameRewriteKey, rw)

	parsed := &ParsedRequest{
		Body:   NewRequestBodyRef(applyToolNameRewriteToBody(body, rw)),
		Model:  "claude-3-7-sonnet-20250219",
		Stream: true,
	}

	nested := `{"tool_calls":[{"tool":"` + fakeBash + `"}]}`
	nameAt := strings.Index(nested, fakeBash)
	splitAt := nameAt + len(fakeBash)/2
	head, tail := nested[:splitAt], nested[splitAt:]

	upstreamSSE := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"` + rw.Forward["batch"] + `","input":{}}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":` + strconv.Quote(head) + `}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":` + strconv.Quote(tail) + `}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	upstream := &anthropicHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
		},
	}

	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
	svc.settingService = NewSettingService(&gatewayServiceTierSettingRepoStub{values: map[string]string{}}, nil)

	account := &Account{
		ID:          301,
		Name:        "anthropic-passthrough-toolname",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "upstream-anthropic-key",
			"base_url": "https://api.anthropic.com",
		},
		Extra:       map[string]any{"anthropic_passthrough": true},
		Status:      StatusActive,
		Schedulable: true,
	}

	result, err := svc.Forward(t.Context(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)

	wire := rec.Body.String()
	require.NotContains(t, wire, fakeBash,
		"no mimicked tool name may reach the client, even split across input_json_delta fragments")

	assembled := ""
	for _, line := range strings.Split(wire, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if gjson.Get(payload, "delta.type").String() != "input_json_delta" {
			continue
		}
		assembled += gjson.Get(payload, "delta.partial_json").String()
	}
	require.Equal(t, `{"tool_calls":[{"tool":"bash"}]}`, assembled,
		"the client must receive the real nested tool name")
}
