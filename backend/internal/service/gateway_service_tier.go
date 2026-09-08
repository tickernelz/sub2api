package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	GatewayServiceTierModeDisabled    = "disabled"
	GatewayServiceTierModeFillMissing = "fill_missing"
	GatewayServiceTierModeForce       = "force"
)

// GatewayServiceTierRule controls how the gateway applies one provider's
// service_tier request field. The zero value is intentionally disabled.
type GatewayServiceTierRule struct {
	Mode        string `json:"mode"`
	ServiceTier string `json:"service_tier"`
}

// GatewayServiceTierSettings stores provider-aware service-tier behavior. It
// is separate from OpenAIFastPolicySettings because override/default intent is
// not the same as block/filter policy.
type GatewayServiceTierSettings struct {
	OpenAI    GatewayServiceTierRule `json:"openai"`
	Anthropic GatewayServiceTierRule `json:"anthropic"`
	// AnthropicSpeed controls the SEPARATE top-level Anthropic `speed` request
	// field, which is what actually selects Anthropic Fast mode. Anthropic's
	// `service_tier` only accepts auto/standard_only, so fast mode cannot be
	// expressed through the Anthropic rule above.
	//
	// NOTE: this rule reuses GatewayServiceTierRule for shape/UI consistency;
	// its ServiceTier field carries a SPEED value ("fast" / "standard"), not a
	// service tier.
	AnthropicSpeed GatewayServiceTierRule `json:"anthropic_speed"`
}

var openAIGatewayServiceTierValues = map[string]struct{}{
	"auto": {}, "default": {}, "fast": {}, "flex": {}, "priority": {}, "scale": {}, "ultrafast": {},
}

var anthropicGatewayServiceTierValues = map[string]struct{}{
	"auto": {}, "standard_only": {},
}

// anthropicGatewaySpeedValues are the accepted values of the Anthropic `speed`
// request field.
var anthropicGatewaySpeedValues = map[string]struct{}{
	"fast": {}, "standard": {},
}

func DefaultGatewayServiceTierSettings() *GatewayServiceTierSettings {
	return &GatewayServiceTierSettings{
		OpenAI:    GatewayServiceTierRule{Mode: GatewayServiceTierModeDisabled, ServiceTier: "auto"},
		Anthropic: GatewayServiceTierRule{Mode: GatewayServiceTierModeDisabled, ServiceTier: "auto"},
		// Disabled by default: fast mode is billed at 2x.
		AnthropicSpeed: GatewayServiceTierRule{Mode: GatewayServiceTierModeDisabled, ServiceTier: "standard"},
	}
}

func ValidateGatewayServiceTierSettings(settings *GatewayServiceTierSettings) error {
	if settings == nil {
		return errors.New("settings cannot be nil")
	}
	if err := normalizeAndValidateGatewayServiceTierRule("openai", &settings.OpenAI, openAIGatewayServiceTierValues, true); err != nil {
		return err
	}
	if err := normalizeAndValidateGatewayServiceTierRule("anthropic", &settings.Anthropic, anthropicGatewayServiceTierValues, false); err != nil {
		return err
	}
	return normalizeAndValidateGatewayServiceTierRule("anthropic_speed", &settings.AnthropicSpeed, anthropicGatewaySpeedValues, false)
}

func normalizeAndValidateGatewayServiceTierRule(
	provider string,
	rule *GatewayServiceTierRule,
	allowed map[string]struct{},
	openAI bool,
) error {
	mode := strings.ToLower(strings.TrimSpace(rule.Mode))
	if mode == "" {
		mode = GatewayServiceTierModeDisabled
	}
	switch mode {
	case GatewayServiceTierModeDisabled, GatewayServiceTierModeFillMissing, GatewayServiceTierModeForce:
	default:
		return fmt.Errorf("%s: invalid mode %q", provider, rule.Mode)
	}

	tier := strings.ToLower(strings.TrimSpace(rule.ServiceTier))
	if openAI && tier == "fast" {
		tier = "priority"
	}
	if tier != "" {
		if _, ok := allowed[tier]; !ok {
			return fmt.Errorf("%s: invalid service_tier %q", provider, rule.ServiceTier)
		}
	}
	if mode != GatewayServiceTierModeDisabled && tier == "" {
		return fmt.Errorf("%s: service_tier is required when mode=%s", provider, mode)
	}

	rule.Mode = mode
	rule.ServiceTier = tier
	return nil
}

func applyGatewayServiceTierRule(body []byte, rule GatewayServiceTierRule, allowed map[string]struct{}) ([]byte, bool, error) {
	if err := normalizeAndValidateGatewayServiceTierRule("provider", &rule, allowed, false); err != nil {
		return body, false, err
	}
	if rule.Mode == GatewayServiceTierModeDisabled {
		return body, false, nil
	}

	existing := gjson.GetBytes(body, "service_tier")
	hasExisting := existing.Exists() && existing.Type != gjson.Null && strings.TrimSpace(existing.String()) != ""
	if rule.Mode == GatewayServiceTierModeFillMissing && hasExisting {
		return body, false, nil
	}

	updated, err := sjson.SetBytes(body, "service_tier", rule.ServiceTier)
	if err != nil {
		return body, false, fmt.Errorf("set service_tier: %w", err)
	}
	return updated, true, nil
}

func extractJSONServiceTier(body []byte) string {
	return strings.TrimSpace(gjson.GetBytes(body, "service_tier").String())
}

func (s *SettingService) GetGatewayServiceTierSettings(ctx context.Context) (*GatewayServiceTierSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyGatewayServiceTierSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultGatewayServiceTierSettings(), nil
		}
		return nil, fmt.Errorf("get gateway service tier settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return DefaultGatewayServiceTierSettings(), nil
	}

	settings := DefaultGatewayServiceTierSettings()
	if err := json.Unmarshal([]byte(value), settings); err != nil {
		slog.Warn("failed to unmarshal gateway service tier settings, falling back to disabled defaults", "error", err)
		return DefaultGatewayServiceTierSettings(), nil
	}
	if err := ValidateGatewayServiceTierSettings(settings); err != nil {
		slog.Warn("invalid gateway service tier settings, falling back to disabled defaults", "error", err)
		return DefaultGatewayServiceTierSettings(), nil
	}
	return settings, nil
}

func (s *SettingService) SetGatewayServiceTierSettings(ctx context.Context, settings *GatewayServiceTierSettings) error {
	if err := ValidateGatewayServiceTierSettings(settings); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal gateway service tier settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyGatewayServiceTierSettings, string(data))
}

func (s *OpenAIGatewayService) configuredOpenAIServiceTier(ctx context.Context, account *Account, rawTier string) (string, bool) {
	if s == nil || s.settingService == nil || !supportsOpenAIGatewayServiceTier(account) {
		return rawTier, false
	}
	settings, err := s.settingService.GetGatewayServiceTierSettings(ctx)
	if err != nil || settings == nil {
		if err != nil {
			slog.Warn("gateway service tier settings unavailable; preserving OpenAI request", "error", err)
		}
		return rawTier, false
	}
	return resolveGatewayServiceTier(settings.OpenAI, rawTier), shouldApplyGatewayServiceTier(settings.OpenAI, rawTier)
}

func supportsOpenAIGatewayServiceTier(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI {
		return false
	}
	return account.Type == AccountTypeAPIKey || account.Type == AccountTypeOAuth
}

func resolveGatewayServiceTier(rule GatewayServiceTierRule, rawTier string) string {
	if rule.Mode == GatewayServiceTierModeForce {
		return rule.ServiceTier
	}
	if rule.Mode == GatewayServiceTierModeFillMissing && strings.TrimSpace(rawTier) == "" {
		return rule.ServiceTier
	}
	return rawTier
}

func shouldApplyGatewayServiceTier(rule GatewayServiceTierRule, rawTier string) bool {
	if rule.Mode == GatewayServiceTierModeForce {
		return true
	}
	return rule.Mode == GatewayServiceTierModeFillMissing && strings.TrimSpace(rawTier) == ""
}

func (s *OpenAIGatewayService) applyConfiguredOpenAIServiceTier(ctx context.Context, account *Account, body []byte) ([]byte, error) {
	if s == nil || s.settingService == nil || !supportsOpenAIGatewayServiceTier(account) {
		return body, nil
	}
	settings, err := s.settingService.GetGatewayServiceTierSettings(ctx)
	if err != nil || settings == nil {
		if err != nil {
			slog.Warn("gateway service tier settings unavailable; preserving OpenAI request", "error", err)
		}
		return body, nil
	}
	updated, _, err := applyGatewayServiceTierRule(body, settings.OpenAI, openAIGatewayServiceTierValues)
	return updated, err
}

func (s *GatewayService) applyConfiguredAnthropicServiceTier(ctx context.Context, account *Account, body []byte) ([]byte, error) {
	if s == nil || s.settingService == nil || account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return body, nil
	}
	settings, err := s.settingService.GetGatewayServiceTierSettings(ctx)
	if err != nil || settings == nil {
		if err != nil {
			slog.Warn("gateway service tier settings unavailable; preserving Anthropic request", "error", err)
		}
		return body, nil
	}
	updated, _, err := applyGatewayServiceTierRule(body, settings.Anthropic, anthropicGatewayServiceTierValues)
	return updated, err
}

// anthropicSpeedAllowsFast is the hard safety gate for forcing Anthropic Fast
// mode. Fast mode is billed at 2x and unsupported models/platforms hard-error
// upstream, so we only ever send speed=fast where it genuinely exists.
func anthropicSpeedAllowsFast(account *Account, model string) bool {
	if account == nil || account.IsBedrock() {
		return false
	}
	return modelSupportsAnthropicFastMode(model)
}

// applyGatewayAnthropicSpeedRule mirrors applyGatewayServiceTierRule but writes
// the top-level `speed` field, and silently skips (no error) when the resolved
// speed is "fast" on an account/model that does not support fast mode.
func applyGatewayAnthropicSpeedRule(body []byte, rule GatewayServiceTierRule, account *Account, model string) ([]byte, bool, error) {
	if err := normalizeAndValidateGatewayServiceTierRule("anthropic_speed", &rule, anthropicGatewaySpeedValues, false); err != nil {
		return body, false, err
	}
	if rule.Mode == GatewayServiceTierModeDisabled {
		return body, false, nil
	}

	existing := gjson.GetBytes(body, "speed")
	hasExisting := existing.Exists() && existing.Type != gjson.Null && strings.TrimSpace(existing.String()) != ""
	if rule.Mode == GatewayServiceTierModeFillMissing && hasExisting {
		return body, false, nil
	}

	// Safety gate: never force fast where the upstream cannot honor it.
	if rule.ServiceTier == "fast" && !anthropicSpeedAllowsFast(account, model) {
		return body, false, nil
	}

	updated, err := sjson.SetBytes(body, "speed", rule.ServiceTier)
	if err != nil {
		return body, false, fmt.Errorf("set speed: %w", err)
	}
	return updated, true, nil
}

// applyConfiguredAnthropicSpeed applies the configured Anthropic speed rule to
// Anthropic API-key gateway requests. Settings failures fail open.
func (s *GatewayService) applyConfiguredAnthropicSpeed(ctx context.Context, account *Account, body []byte, model string) ([]byte, error) {
	if s == nil || s.settingService == nil || account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return body, nil
	}
	settings, err := s.settingService.GetGatewayServiceTierSettings(ctx)
	if err != nil || settings == nil {
		if err != nil {
			slog.Warn("gateway service tier settings unavailable; preserving Anthropic request", "error", err)
		}
		return body, nil
	}
	updated, _, err := applyGatewayAnthropicSpeedRule(body, settings.AnthropicSpeed, account, model)
	return updated, err
}
