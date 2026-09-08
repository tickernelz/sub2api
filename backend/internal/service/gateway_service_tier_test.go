package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyGatewayServiceTierRule_DisabledLeavesBodyUnchanged(t *testing.T) {
	body := []byte(`{"model":"gpt-5","service_tier":"flex"}`)

	updated, changed, err := applyGatewayServiceTierRule(body, GatewayServiceTierRule{
		Mode:        GatewayServiceTierModeDisabled,
		ServiceTier: "priority",
	}, openAIGatewayServiceTierValues)

	require.NoError(t, err)
	require.False(t, changed)
	require.JSONEq(t, string(body), string(updated))
}

func TestApplyGatewayServiceTierRule_FillMissingAddsConfiguredTier(t *testing.T) {
	updated, changed, err := applyGatewayServiceTierRule(
		[]byte(`{"model":"gpt-5"}`),
		GatewayServiceTierRule{Mode: GatewayServiceTierModeFillMissing, ServiceTier: "priority"},
		openAIGatewayServiceTierValues,
	)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "priority", gatewayServiceTierValue(updated))
}

func TestApplyGatewayServiceTierRule_FillMissingPreservesClientTier(t *testing.T) {
	body := []byte(`{"model":"gpt-5","service_tier":"flex"}`)

	updated, changed, err := applyGatewayServiceTierRule(
		body,
		GatewayServiceTierRule{Mode: GatewayServiceTierModeFillMissing, ServiceTier: "priority"},
		openAIGatewayServiceTierValues,
	)

	require.NoError(t, err)
	require.False(t, changed)
	require.JSONEq(t, string(body), string(updated))
}

func TestApplyGatewayServiceTierRule_ForceOverridesClientTier(t *testing.T) {
	updated, changed, err := applyGatewayServiceTierRule(
		[]byte(`{"model":"gpt-5","service_tier":"flex"}`),
		GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "priority"},
		openAIGatewayServiceTierValues,
	)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "priority", gatewayServiceTierValue(updated))
}

func TestApplyGatewayServiceTierRule_NullAndEmptyAreMissing(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5","service_tier":null}`,
		`{"model":"gpt-5","service_tier":""}`,
		`{"model":"gpt-5","service_tier":"   "}`,
	} {
		updated, changed, err := applyGatewayServiceTierRule(
			[]byte(body),
			GatewayServiceTierRule{Mode: GatewayServiceTierModeFillMissing, ServiceTier: "priority"},
			openAIGatewayServiceTierValues,
		)

		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "priority", gatewayServiceTierValue(updated), body)
	}
}

func TestValidateGatewayServiceTierSettingsUsesProviderSpecificValues(t *testing.T) {
	valid := &GatewayServiceTierSettings{
		OpenAI:    GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "fast"},
		Anthropic: GatewayServiceTierRule{Mode: GatewayServiceTierModeFillMissing, ServiceTier: "standard_only"},
	}
	require.NoError(t, ValidateGatewayServiceTierSettings(valid))
	require.Equal(t, "priority", valid.OpenAI.ServiceTier)

	invalidAnthropic := &GatewayServiceTierSettings{
		Anthropic: GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "priority"},
	}
	require.EqualError(t, ValidateGatewayServiceTierSettings(invalidAnthropic), `anthropic: invalid service_tier "priority"`)
}

func TestDefaultGatewayServiceTierSettingsAreDisabled(t *testing.T) {
	settings := DefaultGatewayServiceTierSettings()
	require.Equal(t, GatewayServiceTierModeDisabled, settings.OpenAI.Mode)
	require.Equal(t, "auto", settings.OpenAI.ServiceTier)
	require.Equal(t, GatewayServiceTierModeDisabled, settings.Anthropic.Mode)
	require.Equal(t, "auto", settings.Anthropic.ServiceTier)
}

func TestApplyConfiguredOpenAIServiceTierAppliesToOAuth(t *testing.T) {
	body := []byte(`{"model":"gpt-5"}`)
	repo := &gatewayServiceTierSettingRepoStub{values: map[string]string{
		SettingKeyGatewayServiceTierSettings: `{"openai":{"mode":"force","service_tier":"priority"}}`,
	}}
	svc := &OpenAIGatewayService{settingService: NewSettingService(repo, nil)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	updated, err := svc.applyConfiguredOpenAIServiceTier(context.TODO(), account, body)

	require.NoError(t, err)
	require.Equal(t, "priority", extractJSONServiceTier(updated))
}

func TestConfiguredOpenAIServiceTierAppliesToOAuth(t *testing.T) {
	repo := &gatewayServiceTierSettingRepoStub{values: map[string]string{
		SettingKeyGatewayServiceTierSettings: `{"openai":{"mode":"force","service_tier":"priority"}}`,
	}}
	svc := &OpenAIGatewayService{settingService: NewSettingService(repo, nil)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	tier, shouldApply := svc.configuredOpenAIServiceTier(context.TODO(), account, "")

	require.True(t, shouldApply)
	require.Equal(t, "priority", tier)
}

func TestApplyConfiguredOpenAIServiceTierSkipsUnsupportedAccountType(t *testing.T) {
	body := []byte(`{"model":"gpt-5"}`)
	repo := &gatewayServiceTierSettingRepoStub{values: map[string]string{
		SettingKeyGatewayServiceTierSettings: `{"openai":{"mode":"force","service_tier":"priority"}}`,
	}}
	svc := &OpenAIGatewayService{settingService: NewSettingService(repo, nil)}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken}

	updated, err := svc.applyConfiguredOpenAIServiceTier(context.TODO(), account, body)

	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestApplyConfiguredAnthropicServiceTierSkipsServiceAccounts(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-5","max_tokens":32}`)
	svc := &GatewayService{}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeServiceAccount}

	updated, err := svc.applyConfiguredAnthropicServiceTier(context.TODO(), account, body)

	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestGatewayServiceTierSettingsRoundTripThroughSettingService(t *testing.T) {
	repo := &gatewayServiceTierSettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, nil)
	want := &GatewayServiceTierSettings{
		OpenAI:    GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "fast"},
		Anthropic: GatewayServiceTierRule{Mode: GatewayServiceTierModeFillMissing, ServiceTier: "auto"},
	}

	require.NoError(t, svc.SetGatewayServiceTierSettings(context.Background(), want))
	got, err := svc.GetGatewayServiceTierSettings(context.Background())

	require.NoError(t, err)
	require.Equal(t, "priority", got.OpenAI.ServiceTier)
	require.Equal(t, "auto", got.Anthropic.ServiceTier)
}

type gatewayServiceTierSettingRepoStub struct {
	values map[string]string
}

func (r *gatewayServiceTierSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *gatewayServiceTierSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *gatewayServiceTierSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *gatewayServiceTierSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *gatewayServiceTierSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *gatewayServiceTierSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *gatewayServiceTierSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func gatewayServiceTierValue(body []byte) string {
	return extractJSONServiceTier(body)
}

// --- Change A: widened OpenAI service_tier value list ---

func TestOpenAIGatewayServiceTierAcceptsUltrafast(t *testing.T) {
	settings := &GatewayServiceTierSettings{
		OpenAI: GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "ultrafast"},
	}
	require.NoError(t, ValidateGatewayServiceTierSettings(settings))
	require.Equal(t, "ultrafast", settings.OpenAI.ServiceTier)

	updated, changed, err := applyGatewayServiceTierRule(
		[]byte(`{"model":"gpt-5"}`),
		GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "ultrafast"},
		openAIGatewayServiceTierValues,
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "ultrafast", gatewayServiceTierValue(updated))
}

func TestOpenAIGatewayServiceTierFastStillNormalizesToPriority(t *testing.T) {
	settings := &GatewayServiceTierSettings{
		OpenAI: GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "fast"},
	}
	require.NoError(t, ValidateGatewayServiceTierSettings(settings))
	require.Equal(t, "priority", settings.OpenAI.ServiceTier)

	// applyGatewayServiceTierRule passes openAI=false, so "fast" must be accepted verbatim.
	updated, changed, err := applyGatewayServiceTierRule(
		[]byte(`{"model":"gpt-5"}`),
		GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "fast"},
		openAIGatewayServiceTierValues,
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "fast", gatewayServiceTierValue(updated))
}

func TestOpenAIGatewayServiceTierRejectsInvalidValue(t *testing.T) {
	settings := &GatewayServiceTierSettings{
		OpenAI: GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "turbo"},
	}
	require.EqualError(t, ValidateGatewayServiceTierSettings(settings), `openai: invalid service_tier "turbo"`)
}

// --- Change B: Anthropic speed control ---

func TestDefaultAnthropicSpeedRuleIsDisabled(t *testing.T) {
	settings := DefaultGatewayServiceTierSettings()
	require.Equal(t, GatewayServiceTierModeDisabled, settings.AnthropicSpeed.Mode)
}

func TestValidateAnthropicSpeedRejectsInvalidValue(t *testing.T) {
	settings := DefaultGatewayServiceTierSettings()
	settings.AnthropicSpeed = GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "priority"}
	require.EqualError(t, ValidateGatewayServiceTierSettings(settings), `anthropic_speed: invalid service_tier "priority"`)

	valid := DefaultGatewayServiceTierSettings()
	valid.AnthropicSpeed = GatewayServiceTierRule{Mode: GatewayServiceTierModeForce, ServiceTier: "fast"}
	require.NoError(t, ValidateGatewayServiceTierSettings(valid))
}

func newAnthropicSpeedService(t *testing.T, mode, speed string) *GatewayService {
	t.Helper()
	repo := &gatewayServiceTierSettingRepoStub{values: map[string]string{
		SettingKeyGatewayServiceTierSettings: `{"anthropic_speed":{"mode":"` + mode + `","service_tier":"` + speed + `"}}`,
	}}
	return &GatewayService{settingService: NewSettingService(repo, nil)}
}

func anthropicSpeedValue(body []byte) string {
	return strings.TrimSpace(gjson.GetBytes(body, "speed").String())
}

func TestApplyConfiguredAnthropicSpeed_DisabledIsNoOp(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeDisabled, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestApplyConfiguredAnthropicSpeed_FillMissingPreservesClientSpeed(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5","speed":"standard"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeFillMissing, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, "standard", anthropicSpeedValue(updated))
}

func TestApplyConfiguredAnthropicSpeed_FillMissingSetsSpeed(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeFillMissing, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, "fast", anthropicSpeedValue(updated))
}

func TestApplyConfiguredAnthropicSpeed_ForceOverwritesSpeed(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-8","speed":"standard"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeForce, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-4-8")
	require.NoError(t, err)
	require.Equal(t, "fast", anthropicSpeedValue(updated))
}

func TestApplyConfiguredAnthropicSpeed_FastSkippedForUnsupportedModel(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeForce, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestApplyConfiguredAnthropicSpeed_FastSkippedForBedrockAccount(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeForce, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))

	// And the gate itself must refuse fast for a Bedrock account directly.
	require.False(t, anthropicSpeedAllowsFast(account, "claude-opus-5"))
	require.True(t, anthropicSpeedAllowsFast(&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, "claude-opus-5"))
	require.False(t, anthropicSpeedAllowsFast(&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, "claude-sonnet-4-5"))
}

func TestApplyConfiguredAnthropicSpeed_StandardAppliedRegardlessOfModel(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeForce, "standard")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, "standard", anthropicSpeedValue(updated))
}

func TestApplyConfiguredAnthropicSpeed_SkipsNonAPIKeyAccounts(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5"}`)
	svc := newAnthropicSpeedService(t, GatewayServiceTierModeForce, "fast")
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeServiceAccount}

	updated, err := svc.applyConfiguredAnthropicSpeed(context.TODO(), account, body, "claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}
