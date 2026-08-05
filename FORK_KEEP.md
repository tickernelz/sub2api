# Fork keep set

This file records fork-only behavior that must survive upstream synchronization.

## Gateway provider service-tier settings

Feature: provider-aware outbound `service_tier` defaults and overrides.

Approved phase-1 scope:

- OpenAI API-key gateway routes
- Anthropic API-key gateway routes
- modes: `disabled`, `fill_missing`, `force`
- provider-native values only
- existing OpenAI Fast/Flex policy remains a separate setting and runs after configured tier injection

Intentionally unchanged:

- OAuth, Vertex, Bedrock, Antigravity, and other unsupported account routes
- Gemini and custom-compatible providers
- Anthropic `count_tokens` requests, whose token-counting schema is not a message service-tier request
- the existing `openai_fast_policy_settings` behavior

## Replay/keep checklist during upstream sync

Do not blindly resolve this feature away when replaying fork commits or adapting changed upstream code. Verify each item after the sync:

1. Keep the dedicated setting key `gateway_service_tier_settings`.
2. Keep backend validation and normalization for OpenAI (`auto`, `default`, `flex`, `priority`, `scale`; `fast` aliases to `priority`) and Anthropic (`auto`, `standard_only`).
3. Keep admin GET/PUT serialization and the Settings UI controls/translations.
4. Re-apply the provider-specific outbound transforms at the current upstream choke points:
   - OpenAI HTTP request body and patch/request-view paths
   - OpenAI WebSocket `response.create`
   - Anthropic generic upstream request body before provider sanitization/signing
5. Preserve account-type guards and fail-open behavior when the setting cannot be read.
6. Run the service-tier tests plus the normal backend and frontend gates before considering the sync complete.

## Current implementation map

- `backend/internal/service/gateway_service_tier.go`
- `backend/internal/service/gateway_service_tier_test.go`
- `backend/internal/service/domain_constants.go`
- `backend/internal/handler/dto/settings.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/admin/setting_handler_update.go`
- `backend/internal/service/openai_gateway_request_body.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/gateway_upstream_request.go`
- `frontend/src/api/admin/settings.ts`
- `frontend/src/views/admin/SettingsView.vue`
- `frontend/src/i18n/locales/en/admin/settings.ts`
- `frontend/src/i18n/locales/zh/admin/settings.ts`

If upstream moves any listed function, replay the behavior at its new equivalent rather than preserving a stale context-only patch.
