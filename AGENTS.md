# AGENTS.md — Maintainer Guide for the `tickernelz/sub2api` Fork

This repository is a deliberately small fork of `Wei-Shaw/sub2api`. The default rule is to follow upstream closely and preserve only the fork-specific behavior listed below.

Read this file before synchronizing with upstream. The goal is a repeatable, low-conflict replay—not a large manual merge.

## 1. Repository contract

| Item | Value |
| --- | --- |
| Fork remote | `origin` → `git@github.com:tickernelz/sub2api.git` |
| Upstream remote | `wei-shaw` → `git@github.com:Wei-Shaw/sub2api.git` |
| Go module path | `github.com/Wei-Shaw/sub2api` — keep the upstream path |
| Main branch | `main` |
| Product version | `backend/cmd/server/VERSION` |

The Go module path intentionally remains `github.com/Wei-Shaw/sub2api`. Any fork commit that introduces `github.com/tickernelz/sub2api` imports must be adapted before it is retained.

Check the upstream remote before a sync:

```bash
git remote get-url wei-shaw || git remote add wei-shaw git@github.com:Wei-Shaw/sub2api.git
git fetch wei-shaw
```

## 2. Fork keep-set

These behaviors are intentional fork features. Preserve their semantics during upstream updates. Do not treat old commit SHAs as the source of truth; locate the current semantic anchors and re-implement the behavior when upstream has moved or refactored the code.

### A. Keep OpenAI OAuth accounts schedulable after `refresh_token_reused`

When an OpenAI OAuth refresh reports `refresh_token_reused`, do not immediately mark the account failed or unschedule it. The error means that the refresh token was consumed or rotated; it does not by itself prove that the current access token is unusable.

Required behavior:

- Keep the account schedulable.
- Mark the account as requiring re-authentication.
- Expose the warning in the admin account UI.
- Preserve upstream privacy and scheduler-state behavior.
- Keep `openai_requires_reauth` and the `openai_refresh_token_` scheduler-neutral state handling.

Replay anchors:

- `backend/internal/service/openai_refresh_token_state.go`
- `backend/internal/service/token_refresh_service.go`
- `backend/internal/service/openai_token_provider.go`
- `backend/internal/service/token_refresher.go`
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/service/admin_account.go`
- `frontend/src/components/admin/account/AccountActionMenu.vue`
- `frontend/src/views/admin/AccountsView.vue`
- OpenAI refresh-state tests and the re-auth warning test

Known replay hazards:

- Preserve upstream `logredact.RedactText` handling when combining the soft-handle branch.
- Keep upstream shadow-aware account and privacy logic.
- If upstream already owns `updateExtraCalls`, `lastExtraUpdates`, or `UpdateExtra`, do not replay duplicate test declarations.
- After upstream introduced `postRefreshStateSync`, clear the reused-token marker after that sync and before privacy calls.
- OAuth refresh fixtures that assert active state must explicitly use `StatusActive`.

### B. Stream stale detection and failover

The fork keeps a shared stream watchdog for active SSE loops. It detects:

- first-token timeout;
- soft inter-event gap warnings; and
- hard inter-event gap timeouts that can fail over before output is committed.

Required invariants:

- Use the shared `StreamWatchdog` and layered `StreamRetrySettings`; do not copy timer logic into individual loops.
- Reset the gap timer on every valid upstream event.
- Retry only before client output is committed.
- Preserve the existing `StreamTimeoutSettings` behavior.
- Keep coverage for the 11 active SSE loops.

Replay anchors:

- `backend/internal/service/stream_stale_watchdog.go`
- `backend/internal/service/stream_retry_settings.go`
- `backend/internal/service/stream_watchdog_integration.go`
- `backend/internal/handler/admin/setting_handler_runtime.go`
- `backend/internal/handler/dto/settings.go`
- `backend/internal/server/routes/admin.go`
- the frontend settings API, view, and translations

Audit after a replay:

- 11 watchdog initializations;
- 11 event resets; and
- 33 `decideStreamStall` calls.

Do not apply the generic SSE watchdog to these paths:

- native OpenAI `/v1/responses`, which has its own first-semantic-output staging and retry lifecycle;
- OpenAI Live, which has a separate WebSocket/session retry lifecycle.

### C. Harmony channel-token neutralization

Before sending an OpenAI Responses request upstream, neutralize the literal harmony channel token that can trigger the upstream `invalid_prompt` request guard. Replace the two ASCII pipes inside the exact token with fullwidth pipes. Do not rewrite unrelated harmony tokens or ordinary `analysis` text.

Required invariants:

- Keep the byte fast path and the JSON-aware fallback.
- Preserve large JSON numbers with `json.Decoder.UseNumber()`.
- Use copy-on-write behavior for decoded maps; do not mutate caller-owned maps.
- Keep the default enabled when configuration is absent.
- Preserve `invalid_prompt` observability without changing response or failover semantics.

HTTP replay anchors:

- `backend/internal/service/openai_gateway_forward.go` — rewrite builder
- `backend/internal/service/openai_gateway_passthrough.go` — passthrough builder

WebSocket replay anchors:

- `backend/internal/service/openai_gateway_request_body.go`
- `backend/internal/service/openai_ws_forwarder_payload.go`
- `backend/internal/service/openai_ws_forwarder_ingress.go`
- `backend/internal/service/openai_ws_forwarder_v2.go`
- `backend/internal/service/openai_ws_v2_passthrough_adapter.go`

Keep the `NeutralizeHarmonyChannelToken` configuration field and its default. Audit every current HTTP and WebSocket `response.create` boundary after an upstream transport refactor.

### D. Provider-aware gateway `service_tier` controls

This feature is separate from the existing `openai_fast_policy_settings`. It controls the outbound request field, while the existing fast policy still performs its own filter/block/force processing afterward.

Scope:

- OpenAI API-key and OAuth gateway routes.
- Anthropic API-key gateway routes.
- Modes: `disabled`, `fill_missing`, and `force`.
- Provider-native values only.

Accepted values:

- OpenAI: `auto`, `default`, `flex`, `priority`, `scale`; `fast` normalizes to `priority`.
- Anthropic: `auto`, `standard_only`.

Deliberate exclusions:

- OpenAI account types outside API-key and OAuth.
- Vertex, Bedrock, Antigravity, Gemini, and custom-compatible providers.
- Anthropic `count_tokens` requests.
- The existing `openai_fast_policy_settings` behavior and storage key.

Required invariants:

- `disabled` is a true no-op.
- `fill_missing` preserves a non-empty client value.
- `force` replaces the client value.
- Invalid provider values are rejected by backend validation.
- Settings read failures fail open and preserve the request.
- The usage record must be derived from the **final outbound body**, not from a pre-policy request struct. Otherwise a forced `priority` request can be billed correctly while the UI incorrectly displays `Standard`.

Setting key and replay anchors:

- `gateway_service_tier_settings`
- `backend/internal/service/gateway_service_tier.go`
- `backend/internal/service/gateway_service_tier_test.go`
- `backend/internal/service/openai_gateway_request_body.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_gateway_passthrough.go`
- `backend/internal/service/openai_gateway_chat_completions.go`
- `backend/internal/service/openai_gateway_responses_chat_fallback.go`
- `backend/internal/service/gateway_upstream_request.go`
- `backend/internal/service/gateway_anthropic_passthrough.go`
- `backend/internal/service/domain_constants.go`
- `backend/internal/handler/dto/settings.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/admin/setting_handler_update.go`
- `frontend/src/api/admin/settings.ts`
- `frontend/src/views/admin/SettingsView.vue`
- the English and Chinese admin settings translations

Transport audit:

1. OpenAI HTTP request-body and patch/request-view paths.
2. OpenAI passthrough request builder.
3. OpenAI Chat Completions → Responses compatibility path.
4. OpenAI WebSocket `response.create` frames.
5. Anthropic generic request body before provider sanitization or signing.
6. Usage metadata propagation from each final body into `OpenAIForwardResult`/`UsageLog`.

The feature is fork-only until upstream provides an equivalent implementation. If upstream moves a function, port the behavior to the new choke point instead of preserving a stale file-local patch.

### E. OpenAI Responses input metadata compatibility

Some clients and providers attach `status` to input items when replaying a conversation. The OpenAI Responses upstream routes used by this fork do not accept that output-oriented metadata on `input[N]`, and return errors such as `Unknown parameter: 'input[57].status'`.

Required behavior:

- Apply the compatibility sanitizer to OpenAI `/v1/responses` HTTP forwarding for both API-key and OAuth accounts.
- Remove only the top-level `status` key from each object in the request `input` array.
- Preserve input order, non-object items, every other item field, IDs, call IDs, and nested `status` values inside content or tool payloads.
- Keep the sanitizer copy-on-write and single-pass over the input array so large multi-turn requests remain bounded.
- Keep the OAuth/Codex map-level filter as defense in depth for items created or transformed after raw-body sanitization.
- Do not apply this behavior to Anthropic, Grok, or other provider routes that may support the field.
- Preserve the existing invalid replayed-ID sanitization in the same traversal.

Replay anchors:

- `backend/internal/service/openai_responses_item_id.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_codex_transform.go`
- `backend/internal/service/openai_codex_input_status_test.go`
- `backend/internal/service/openai_gateway_apikey_item_id_test.go`

Replay hazards:

- Keep the condition provider-scoped to `PlatformOpenAI`; do not broaden it to generic-compatible providers.
- Delete only `input` item `status`; never delete top-level request fields or nested content metadata.
- Preserve copy-on-write behavior because callers may reuse decoded input maps.
- Re-audit both API-key and OAuth paths if upstream changes the Responses request builder or moves Codex normalization.

### F. Preserve `max` reasoning effort for OpenAI API-key Chat Completions

Direct OpenAI-compatible API-key Chat Completions upstreams can support `reasoning_effort: "max"` for models outside the built-in GPT-5.6 catalog, such as DeepSeek models. The raw request path already forwards the client value unchanged; usage metadata must not relabel it as `xhigh`.

Required behavior:

- Preserve explicit `max` as `max` in usage metadata for the raw OpenAI API-key `/v1/chat/completions` path.
- Keep the request body pass-through unchanged; do not map outbound `max` to `xhigh`.
- Keep Responses and OAuth/Codex normalization semantics separate unless their upstream contract changes.
- Preserve existing normalization for `minimal`, separator variants, and other known effort values.

Replay anchors:

- `backend/internal/service/openai_gateway_chat_completions_raw.go`
- `backend/internal/service/openai_gateway_request_body.go`
- `backend/internal/service/openai_gateway_chat_completions_raw_test.go`

The fork-specific normalizer exists because model catalogs cannot enumerate every OpenAI-compatible provider's supported effort values. Upstream still decides whether the forwarded value is accepted.

## 3. Upstream and workflow divergence

The fork follows upstream `.github/` workflows except for these intentional changes:

| File | Fork-only difference | Reason |
| --- | --- | --- |
| `.github/workflows/cla.yml` | References `tickernelz/sub2api` for the repository guard and fork CLA link | The CLA job and link belong to this fork |
| `.github/workflows/release.yml` | `continue-on-error: true` on `Update DockerHub description` | Docker Hub can return `403` even when image publication succeeds |
| All other `.github/` files | No intentional divergence | Follow upstream |

Do not restore an old fork snapshot of `.github/`. Start from the current upstream workflows and re-apply only the two differences above.

The upstream `.gitignore` may ignore `AGENTS.md` and `CLAUDE.md`. This repository intentionally tracks `AGENTS.md`; keep that rule removed or replaced with a comment, then stage the file explicitly.

## 4. Safe upstream synchronization

Use a full rewrite from upstream rather than `git merge wei-shaw/main`. Large upstream refactors make broad merges prone to duplicate declarations, stale file bodies, and broken wiring.

The following procedure is a maintainer runbook; do not execute destructive steps without explicit authorization for the current task.

```bash
# 0. Inspect the current state and fetch upstream
git status
git fetch wei-shaw

# 1. Create a recovery branch before rewriting history
git branch backup/pre-rewrite-$(date +%Y%m%d-%H%M%S) HEAD

# 2. Reset the working branch to the current upstream tree
git reset --hard wei-shaw/main

# 3. Check for fork-only leftovers
git status --porcelain

# 4. Find current feature commits by subject, then replay or re-implement them
git log --oneline --all --grep="keep oauth accounts schedulable"
git log --oneline --all --grep="stream stale"
git log --oneline --all --grep="harmony"
git log --oneline --all --grep="service tier"

# 5. Re-apply only the two .github divergences and retain AGENTS.md
# 6. Update VERSION, run the verification gates, and inspect the complete diff
```

When a cherry-pick conflicts with a major upstream file split:

1. Keep the current upstream file structure.
2. Re-home the fork behavior at the semantic replay anchor.
3. Re-apply the relevant tests and invariants.
4. Search for duplicate declarations and stale imports.
5. Verify runtime call-site cardinality, not just patch application.

Before staging a replay, verify that no fork module path leaked into the code:

```bash
git diff --cached | grep 'tickernelz/sub2api'   # expected: no Go import matches
```

## 5. Verification gates

Run from the repository root. Do not push a synchronization or release commit while a required gate is red or unverified.

### Backend

```bash
cd backend
export OPENAI_API_KEY=
export PATH="$(go env GOPATH)/bin:$PATH"
go build ./...
go vet ./...
go test -tags=unit ./...
go test -tags=integration ./...
govulncheck ./...
```

Use the same `golangci-lint` version as CI, currently v2.9.0:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0
"$(go env GOPATH)/bin/golangci-lint" run --timeout=30m
```

`go test ./...` alone is not the CI unit gate because it skips tests guarded by `//go:build unit`.

### Frontend

```bash
cd frontend
export CI=true
./node_modules/.bin/eslint . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts
./node_modules/.bin/vue-tsc --noEmit
./node_modules/.bin/vitest run
./node_modules/.bin/vue-tsc -b
./node_modules/.bin/vite build
```

Use the repository's existing frontend toolchain. Avoid upgrading pnpm or rewriting the lockfile just to run verification. If a tool creates `frontend/pnpm-workspace.yaml` or modifies the lockfile as an artifact, remove the artifact and restore the intentional lockfile state before reporting the result.

Also run focused tests for the feature being replayed. For service-tier work, include the gateway service-tier tests and the OpenAI gateway metadata-propagation regression test.

Always finish with:

```bash
git diff --check
git status --short
```

A timeout, killed process, or environment-blocked command is not passing evidence. Report it as unverified.

## 6. Versioning and release

- The product version is tracked in `backend/cmd/server/VERSION`.
- The release workflow derives the final version from the pushed tag.
- Release tags use `vX.Y.Z`.
- The release workflow builds and publishes multi-architecture images to GHCR and Docker Hub; it does not SSH into or redeploy production.
- CI and Security Scan also run for tag pushes. The tag must point to a commit whose required main-branch gates are already green.

Example release shape:

```bash
echo "0.1.xxx" > backend/cmd/server/VERSION
git add backend/cmd/server/VERSION
git commit -m "chore: release v0.1.xxx"
git tag -a v0.1.xxx -m "Release v0.1.xxx"
git push origin v0.1.xxx
```

Verify published artifacts from workflow output and registry digests. Do not infer publication from a successful local build. A Docker Hub description `403` is non-blocking only for the documented soft-fail step; image publication still needs independent verification.

## 7. Maintainer standards

- Follow upstream for product code unless a behavior is listed in the keep-set.
- Keep fork features separate from unrelated policies and providers.
- Prefer small, semantic replays over giant conflict resolutions.
- Do not add compatibility code for removed historical fork features unless explicitly requested.
- Do not use a commit SHA as the only documentation of a fork feature.
- Do not claim a test, build, release, or publication succeeded without fresh command or workflow evidence.
- Do not commit, push, force-push, change permissions, or modify production as part of routine maintenance unless the user explicitly authorizes that side effect.
- Keep `FORK_KEEP.md` as a pointer only; `AGENTS.md` is the canonical keep/replay document.
