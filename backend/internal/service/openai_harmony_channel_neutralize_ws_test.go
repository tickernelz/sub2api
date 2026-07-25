package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyOpenAIFastPolicyToWSResponseCreateNeutralizesHarmonyTokenWhenEnabled(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	frame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":[{"type":"input_text","text":"` + openAIHarmonyChannelToken + `analysis"}]}`)

	got, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), account, "gpt-5.6-sol", frame)

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NotContains(t, string(got), openAIHarmonyChannelToken)
	require.Contains(t, string(got), openAIHarmonyChannelTokenNeutralized)
}

func TestApplyOpenAIFastPolicyToWSResponseCreateNeutralizesJSONEscapedHarmonyToken(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	frame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"<\u007cchannel\u007c>analysis","max_output_tokens":9007199254740993}`)

	got, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), account, "gpt-5.6-sol", frame)

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, openAIHarmonyChannelTokenNeutralized+"analysis", gjson.GetBytes(got, "input").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "max_output_tokens").Raw, "JSON-aware rewrite must preserve integer precision")
}

func TestNeutralizeOpenAIHarmonyChannelTokenJSONPreservesEscapedNoOpBytes(t *testing.T) {
	in := []byte(`{"input":"hello \u263a","max_output_tokens":9007199254740993}`)

	out, changed := neutralizeOpenAIHarmonyChannelTokenJSON(in)

	require.False(t, changed)
	require.Equal(t, in, out)
	require.True(t, &in[0] == &out[0], "no-op must return the original backing array")
}

func TestApplyOpenAIFastPolicyToWSResponseCreatePreservesHarmonyTokenWhenDisabled(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(false)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	frame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":[{"type":"input_text","text":"` + openAIHarmonyChannelToken + `analysis"}]}`)

	got, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), account, "gpt-5.6-sol", frame)

	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, frame, got)
}

func TestBuildOpenAIWSCreatePayloadNeutralizesHarmonyTokenWithoutMutatingRequest(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	reqBody := map[string]any{
		"model": "gpt-5.6-sol",
		"input": []any{
			map[string]any{"type": "input_text", "text": openAIHarmonyChannelToken + "analysis"},
		},
	}

	got := svc.buildOpenAIWSCreatePayload(reqBody, account)

	gotText := gjson.GetBytes(payloadAsJSONBytes(got), "input.0.text").String()
	require.NotContains(t, gotText, openAIHarmonyChannelToken)
	require.Contains(t, gotText, openAIHarmonyChannelTokenNeutralized)
	originalText := gjson.GetBytes(payloadAsJSONBytes(reqBody), "input.0.text").String()
	require.Contains(t, originalText, openAIHarmonyChannelToken, "builder must not mutate the original request map")
}

func TestBuildOpenAIWSCreatePayloadNeutralizesHarmonyTokenInObjectKey(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	unsafeKey := openAIHarmonyChannelToken + "analysis"
	safeKey := openAIHarmonyChannelTokenNeutralized + "analysis"
	reqBody := map[string]any{
		"model": "gpt-5.6-sol",
		"metadata": map[string]any{
			unsafeKey: "fixture",
		},
	}

	got := svc.buildOpenAIWSCreatePayload(reqBody, account)
	gotMetadata, ok := got["metadata"].(map[string]any)
	require.True(t, ok)
	originalMetadata, ok := reqBody["metadata"].(map[string]any)
	require.True(t, ok)

	require.Equal(t, "fixture", gotMetadata[safeKey])
	require.NotContains(t, gotMetadata, unsafeKey)
	require.Equal(t, "fixture", originalMetadata[unsafeKey], "builder must not mutate original object keys")
}

func TestNeutralizeOpenAIHarmonyChannelTokenJSONObjectKeepsExistingSafeKeyOnCollision(t *testing.T) {
	unsafeKey := openAIHarmonyChannelToken + "analysis"
	safeKey := openAIHarmonyChannelTokenNeutralized + "analysis"
	input := map[string]any{
		unsafeKey: "unsafe-value",
		safeKey:   "safe-value",
	}

	got, changed := neutralizeOpenAIHarmonyChannelTokenJSONObject(input)

	require.True(t, changed)
	require.NotContains(t, got, unsafeKey)
	require.Equal(t, "safe-value", got[safeKey], "an explicitly safe key wins deterministic collision handling")
	require.Equal(t, "unsafe-value", input[unsafeKey], "copy-on-write must preserve the original map")
}

func TestBuildOpenAIWSCreatePayloadPreservesHarmonyTokenWhenDisabled(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(false)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	reqBody := map[string]any{
		"model": "gpt-5.6-sol",
		"input": []any{
			map[string]any{"type": "input_text", "text": openAIHarmonyChannelToken + "analysis"},
		},
	}

	got := svc.buildOpenAIWSCreatePayload(reqBody, account)

	gotText := gjson.GetBytes(payloadAsJSONBytes(got), "input.0.text").String()
	require.Contains(t, gotText, openAIHarmonyChannelToken)
}
