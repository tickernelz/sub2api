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

### E. OpenAI Responses input metadata compatibility (SUPERSEDED BY UPSTREAM)

Upstream adopted a reactive mechanism (`normalizeOpenAIResponsesRejectedFieldRetryBody`) that strips `input[N].status` and other rejected fields only after the upstream returns an explicit rejection, then retries once. This supersedes the fork's earlier proactive status stripping, which conflicted with upstream's retry tests and semantics.

Current required behavior:

- Do NOT re-introduce proactive top-level `status` stripping of `input[]` items; upstream's reactive rejected-field retry owns that behavior.
- Preserve upstream's invalid replayed-ID sanitization (`sanitizeOpenAIResponsesInputItemIDs`, ID/call_id namespace checks) — the fork keeps this function but must not extend it to strip `status`.
- The OAuth/Codex map-level filter must NOT strip `status` anymore; upstream handles it reactively.

Replay anchors:

- `backend/internal/service/openai_responses_item_id.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_responses_rejected_field_retry.go`

Replay hazards:

- If upstream changes its rejected-field retry semantics, re-evaluate whether proactive stripping is needed again.
- Do not replay old fork commits `fix(openai): strip unsupported Responses input status` verbatim; their proactive stripping now breaks upstream tests (`TestOpenAIGatewayService_OAuthRetriesExactRejectedStatus`).

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

### G. Restore mimicked tool names across streamed `input_json_delta` fragments

Claude Code mimicry renames client tools to fake names before the request goes upstream (`bash` → `extract_bas03`, `session_x` → `cc_ses_x`) and restores the real names on the way back. Restoration ran as a per-SSE-line `bytes.Replace`, which only works when a fake name is wholly contained in one line.

A fake name written into a tool's **arguments** is not. Anthropic streams tool input as `input_json_delta` fragments, so a nested tool name (for example `batch`'s `tool_calls[*].tool`) is split across two `content_block_delta` lines. Neither line contains the whole fake name, no replacement fires, and the fake name reaches the client verbatim. The client then calls a tool that does not exist and the turn fails.

`toolNameStreamRestorer` closes the gap: it withholds the trailing bytes of each fragment that could still begin a fake name, prepends them to the next fragment, restores there, and releases whatever remains at `content_block_stop` as one extra `input_json_delta` event emitted before the stop line.

Required behavior:

- No fake tool name may reach the client, including one split across `input_json_delta` fragments.
- The reassembled tool-argument JSON must be byte-identical to the upstream JSON with fake names replaced by real names. Nothing is dropped, duplicated, or reordered.
- Only `delta.partial_json` on `content_block_delta` / `input_json_delta` lines may be withheld. Every other line passes through with the existing single-line restore.
- `content_block_stop` flushes the withheld tail for its own block before the stop line is written.
- Restoration stays a response-side concern. It must not depend on whether the *request* installed a mapping.

Required invariants:

- Withheld bytes are bounded by the longest fake name minus one. The bound is derived from both `ToolNameRewrite.Reverse` and `staticToolNameRewrites`, so it stays correct when either source grows.
- Carry state is keyed **per content-block index**, never global. Anthropic interleaves blocks; a global carry corrupts arguments by splicing one block's tail into another block's fragment.
- A nil `*ToolNameRewrite` is legal. Static prefix restoration (`cc_sess_` → `sessions_`, `cc_ses_` → `session_`) must still work, so `newToolNameStreamRestorer(nil)` must still withhold and still restore.
- `restoreToolNamesInBytes` stays a single-fragment helper. It is deliberately unable to span fragments; do not "fix" it, and do not delete it — the streaming restorer calls it on each joined buffer, and the five non-streaming and whole-body call sites still need it.
- Longest-fake-name-first replacement order (`ReverseOrdered`) must survive, so a short fake name that is a prefix of a longer one is not consumed first.
- The disguise is installed only on the **OAuth** paths (`account.IsOAuth()`, meaning `AccountTypeOAuth` or `AccountTypeSetupToken`), never on API-key requests. Restoration is intentionally wider than installation: it runs unconditionally on the passthrough stream and is a no-op when the context has no mapping.

Replay anchors:

- `backend/internal/service/gateway_tool_rewrite.go` — `toolNameStreamRestorer`, `newToolNameStreamRestorer`, `isPrefixOfAnyFakeName`, `splitEmittableTail`, `RestoreFragment`, `Flush`, `RestoreSSELine`; also the pre-existing `restoreToolNamesInBytes` / `reverseToolNamesIfPresent`
- `backend/internal/service/gateway_anthropic_passthrough.go` — `handleStreamingResponseAnthropicAPIKeyPassthrough` constructs the restorer and consumes the `(out, extraBefore, emit)` triple
- `backend/internal/service/gateway_claude_oauth_body.go` — `applyClaudeCodeOAuthMimicryToBody` installs the mapping into `gin.Context` (OAuth only)
- `backend/internal/service/gateway_forward.go` — the other install site, under `shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCode`
- `backend/internal/service/gateway_count_tokens.go` — applies the rewrite but deliberately does **not** publish the mapping to the context
- `backend/internal/service/gateway_tool_rewrite_test.go` — restorer unit tests, including the negative control that pins the single-fragment limitation
- `backend/internal/service/gateway_anthropic_passthrough_toolname_test.go` — end-to-end split-fragment test through `Forward`

Single-line restore call sites that were **not** converted and still use `reverseToolNamesIfPresent`:

- `backend/internal/service/gateway_upstream_response.go` (`handleStreamingResponse`, plus the whole-body path)
- `backend/internal/service/openai_gateway_messages_anthropic_native.go`
- `backend/internal/service/gateway_forward_as_chat_completions.go`
- `backend/internal/service/gateway_forward_as_responses.go`
- `backend/internal/service/openai_gateway_chat_completions_anthropic_native.go`
- `backend/internal/service/openai_gateway_responses_anthropic_native.go`

Known replay hazards:

- **The tail bound is `maxLen - 1`, and `maxLen` must consider both mapping sources.** Measured: `newToolNameStreamRestorer(nil)` yields `maxLen=8` from `cc_sess_`; a dynamic map containing `extract_bas03` yields `maxLen=13`. Seeding `maxLen` from `Reverse` alone regresses the static-prefix case to zero withholding and silently reopens the bug for `session_*` tools.
- **`Flush` at `content_block_stop` is mandatory.** Without it the withheld tail is swallowed and the tool-argument JSON arrives truncated — a worse failure than the original bug, because the JSON no longer parses. Verified: a stream whose last fragment ends mid-prefix (`revi`) only survives because the stop branch emits a synthetic `content_block_delta` carrying `"revi"`.
- **Carry must be per block index, not global.** `TestToolNameStreamRestorer_KeepsBlocksIndependent` is the guard: block 1's fragment must not consume block 0's carry.
- **Only `partial_json` may be withheld.** `content_block_start` carries the top-level `tool_use.name` in one piece and must keep passing through the single-line restore, otherwise the tool call itself loses its name.
- **`restoreToolNamesInBytes` cannot span fragments and must stay that way.** `TestRestoreToolNamesInBytes_CannotRestoreNameSplitAcrossFragments` asserts the limitation deliberately. If upstream refactors that helper, do not make it stateful; keep the state in the restorer.
- **Fragment boundaries on the wire are re-cut, not preserved.** A fragment can be emitted shorter than upstream sent it, longer (when a carry is prepended), or dropped entirely when it is wholly withheld. Verified wire output: upstream fragments `"review_bas00"` + `"{\"x\":1}"` are re-emitted as `"bas"` + `"h{\"x\":1}"`. Any consumer or test that asserts fragment-for-fragment equality with upstream will fail; only the **concatenation** is contractual.
- **A fully withheld `data:` line is skipped while its `event:` line is not.** The `emit=false` branch `continue`s after the `event: content_block_delta` header has already been written, producing a header with no data line, and the `extraBefore` flush is written before the `content_block_stop` data line rather than before its `event:` header. Verified on the wire. Clients tolerate it because SSE dispatches on blank-line boundaries, but a future rewrite that couples header and payload must handle both, and the keepalive/`inPartialEvent` bookkeeping in the same loop must not be desynchronized by the skipped line.
- **The stale-stream watchdog and usage parsing must stay upstream of the restorer.** `staleWatchdog.OnUpstreamEvent()` and `parseSSEUsagePassthrough` run on the raw line before restoration. Moving restoration earlier, or skipping those calls on a withheld line, would let a withheld fragment look like a stalled stream or drop usage.
- **`clientDisconnected` must not stop draining.** The `extraBefore` write sets `clientDisconnected` on failure but the loop keeps consuming upstream for usage accounting. Preserve that; an early `return` here under-bills.
- **The mapping lives on `gin.Context` and is never cleared.** The failover loop in `backend/internal/handler/gateway_handler.go` reuses one `gin.Context` across attempts, so a mapping installed by an OAuth attempt survives into a later attempt on a different account. Restoration is idempotent for unrelated text, so this is currently harmless, but a future change that makes restoration lossy or that keys behavior off mapping presence must clear `claude_tool_name_rewrite` between attempts.
- **Only the Anthropic API-key passthrough loop was converted.** The OAuth `/v1/messages` loop in `gateway_upstream_response.go` still restores per output block via `reverseToolNamesIfPresent`. It writes whole re-serialized SSE event blocks rather than raw fragments, so it is not obviously exposed, but it is the path where the disguise is actually installed. If a nested-name leak is reported against OAuth streaming, convert that loop next rather than re-deriving the fix.

Verification for this entry:

```bash
cd backend
go test -tags=unit ./internal/service/ -run 'ToolName|RestoreSSELine|RestoreToolNames|AnthropicPassthrough_NestedToolName' -count=1
```

Observed: 20 tests, all passing. Run the full `go test -tags=unit ./...` gate from section 5 before shipping; the focused run above is a feature check, not the CI gate.

### H. Perch provider platform (Perch AI over its own model-call protocol)

The fork ships a Perch provider: accounts authenticate with a Perch Supabase session and requests are translated to Perch's own `model-call` SSE protocol. It is not an OpenAI-compatible upstream; the package owns the wire format, the ClientHello and header order the official CLI presents, and the translation back to `chat.completion.chunk`.

Required behavior:

- `domain.PlatformPerch` (`perch`) stays in `domain.platformList` with `PlatformGatewayOpenAI`, and `service.PlatformPerch` mirrors it. That row is the only thing that makes perch accounts selectable: removing it silently drops the platform from the gateway, the admin lists and the quota lists while the package still compiles.
- `forwardAsChatCompletions` routes perch accounts to `forwardPerchChatCompletions` before any Codex, protocol or Grok handling.
- Perch usage (`inputTokens`, `outputTokens`, `cacheCreationInputTokens`, `cacheReadInputTokens`) must reach `OpenAIForwardResult`; `prompt_tokens` is the sum of the three input counters, never `inputTokens` alone.
- Refreshed sessions are written back to the account row through `persistAccountCredentials`; a rotated refresh token must never live only in memory.
- Every OpenAI-gateway entry that Perch cannot serve (`/v1/responses`, `/v1/messages`, embeddings, images, input_tokens and count_tokens, alpha search) must fail closed with 400 `unsupported_protocol` before touching the Perch upstream; a perch account reaching one of them silently answered HTTP 200 with an empty body before the guard.
- Account-scoped Perch refusals (401, 429, 5xx, transport, 403 `perch_surface_required`) must become `*UpstreamFailoverError` with the upstream status, and SSE headers must stay unwritten until the first chunk, so they fail over instead of sticking to the same account. Request-scoped refusals (400 `context_overflow` or `invalid_request`, `plan_required`, `model_blocked`) are the opposite: write them to the client with their own status and never fail over, or every account burns a ticket and the client receives 502 instead of 400.
- Usage must be asserted on the billed buckets (`openAIUsageBilledBuckets`: input minus cache read/write), not on Perch's summed `inputTokens`.
- The wire fixtures stay CRLF (`.gitattributes` `-text`) because the tests compare raw bytes.
- A perch account is never an upstream-billing-probe target. It is stored as `type=apikey` but holds no `credentials.api_key`, so a probe there can only record `missing_api_key` and reschedule forever. `IsUpstreamBillingProbeIdentity` excludes the platform for every caller (create, edit, bulk edit, CRS sync, scheduler), the admin rate cell and the bulk-edit probe toggle refuse it, and `249_clear_perch_upstream_billing_probe_flags.sql` clears flags left on rows created before the guard.

Replay anchors:

- `backend/internal/pkg/perch/` — handler, client, `wire.go`, `fingerprint.go`, chat translation, `surfacePromptText`, `testdata/wire/*.http`
- `backend/internal/service/openai_gateway_perch.go`
- `backend/internal/service/openai_gateway_chat_completions.go` — the `PlatformPerch` branch
- `backend/internal/service/openai_gateway_forward.go` — the Responses guard
- `backend/internal/domain/constants.go`, `backend/internal/domain/platforms.go`
- `backend/internal/service/domain_constants.go`
- `docs/PERCH_PROVIDER.md`, the `.gitignore` allowlist row, and the `.gitattributes` CRLF rule
- `backend/internal/service/upstream_billing_probe.go` — the `PlatformPerch` exclusion inside `IsUpstreamBillingProbeIdentity`
- `backend/migrations/249_clear_perch_upstream_billing_probe_flags.sql` — one-shot cleanup of probe flags stored on perch rows
- `frontend/src/utils/upstreamBillingProbe.ts` — the matching `isUpstreamBillingProbeCapable` guard used by the rate cell and bulk edit
- `frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts` — carries the `perch` entry in the platform quota map; a replay that starts from upstream drops the entry and the spec fails
- `backend/go.mod` — `golang.org/x/sys` direct, already in the module graph

Known replay hazards:

- Perch refuses a model call whose conversation carries no substantial system prompt (`403 perch_surface_required`). The gate is a length floor near 202 characters, measured with one account and one ticket (200 chars refused, 205 accepted, 3,000 filler characters accepted); it is not a persona check and not a transport check. `chat.go` therefore prepends `surfacePromptText` as the first message of every call, and `surface_test.go` fails if that text drops below `surfacePromptFloor`. Do not remove it or move it behind the caller's system messages.
- The wire layer writes HTTP/1.1 itself because `net/http` always sorts headers. If upstream refactors transports, keep the byte fixtures and their CRLF attribute, or the round-trip tests pass in a warm worktree and fail in CI.
- Idle connection reuse needs the TLS wrapper unwrapped with `NetConn` before the peek; a bare `*net.TCPConn` assertion silently disables reuse (five requests, five handshakes).
- Perch refresh tokens rotate within one family: the adapter must own a copy of the session, never the CLI's live file.

## 3. Upstream and workflow divergence

The fork follows upstream `.github/` workflows except for these intentional changes:

| File | Fork-only difference | Reason |
| --- | --- | --- |
| `.github/workflows/release.yml` | `continue-on-error: true` on `Update DockerHub description` | Docker Hub can return `403` even when image publication succeeds |
| All other `.github/` files | No intentional divergence; `cla.yml` is deleted, see below | Follow upstream |

Do not restore an old fork snapshot of `.github/`. Start from the current upstream workflows and re-apply only the one difference above. The fork runs no CLA workflow: `.github/workflows/cla.yml` is deleted here on purpose, and every upstream reset brings it back, so section 4 deletes it again.

### Fork carry-overs that live OUTSIDE the replayed keep-set

A `git reset --hard wei-shaw/main` plus a keep-set replay restores only what the replayed commits contain. Anything the fork fixed inside a `chore: sync fork with upstream` housekeeping commit is **not** replayed and silently reverts to its pre-fix state on the next sync. Restore these explicitly from the previous release before running the gates:

| Path | Why it is carried | Restore with |
| --- | --- | --- |
| `.github/workflows/cla.yml` | Deleted on purpose. Upstream still ships it, so a reset reinstates it; the upstream guard (`github.repository == 'Wei-Shaw/sub2api'`) makes it inert in the fork, yet it still queues a skipped `CLA Assistant` run on every PR | `git rm .github/workflows/cla.yml` |
| `AGENTS.md` | Fork-only doc, untracked and `.gitignore`d after the reset | `git checkout main -- AGENTS.md` then `git add -f AGENTS.md` |
| `frontend/package.json` + `frontend/pnpm-lock.yaml` | `vue-i18n` is `^11.4.14` here while upstream still pins `^9.14.5`; 9.x carries GHSA-83rm-6c8r-g728 with no patch inside that major, so a replay would turn the `frontend-security` job red | Re-apply the two specifiers onto upstream's manifest instead of restoring the file, because upstream bumped `frontend/package.json` six times since 2026-07-29 and four of those were security updates a wholesale checkout would revert: `cd frontend && npm pkg set 'dependencies.vue-i18n=^11.4.14' 'devDependencies.@intlify/message-compiler=^11.4.14' && pnpm install --lockfile-only`, then `pnpm audit --prod`. `pnpm add` also works but writes `^11.4.16`/`11.4.16`, four manifest lines different from the committed state; the `npm pkg set` form reproduces it byte-identically. Delete this row once upstream pins `^11.4.14`. |
| `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts` | Asserts the rendered plan strings; v11 resolves real messages where v9 returned raw keys, so the replayed upstream version fails 3 of its 9 assertions | `git checkout main -- <path>` |
| `frontend/src/views/admin/__tests__/AccountsView.openaiReauthWarning.spec.ts` | Adapted to upstream `df64b5f36`'s `anchorRect` prop contract in a housekeeping commit, so the replay reinstates the stale `position:` version and the CI-critical spec fails | `git checkout main -- <path>` |

Symptom when this is missed: the keep-set replays 15/15 with identical patch IDs, every backend gate is green, and `make test-frontend` still fails on a fork spec. Before re-deriving the fix, `grep` the probe's copy for the current upstream prop names — if it has the old ones, the file simply was not carried.

Prefer folding a recurring adaptation into the product commit it belongs to; only use the carry-over table when the fix has no natural home in the keep-set.

The upstream `.gitignore` ignores `AGENTS.md` (line 130) and `CLAUDE.md`. This fork keeps `.gitignore` byte-identical to upstream and does **not** remove the rule: ignore patterns do not apply to already-tracked files, so the rule is inert while `AGENTS.md` stays tracked. It only bites after a `git reset --hard wei-shaw/main`, which makes the file untracked again; from that point it must be re-added with `git add -f AGENTS.md`. `FORK_KEEP.md` is not ignored and needs no force flag.

## 4. Safe upstream synchronization

Use a full rewrite from upstream rather than `git merge wei-shaw/main`. Large upstream refactors make broad merges prone to duplicate declarations, stale file bodies, and broken wiring.

The following procedure is a maintainer runbook; do not execute destructive steps without explicit authorization for the current task.

```bash
# 0. Inspect the current state and fetch upstream
git status
git fetch wei-shaw

# 1. Create a recovery branch before rewriting history
git branch backup/pre-rewrite-$(date +%Y%m%d-%H%M%S) HEAD

# 1b. Preserve the fork-only docs. Upstream tracks neither AGENTS.md nor
#     FORK_KEEP.md, so step 2 deletes both, and AGENTS.md is matched by
#     .gitignore, so step 3 cannot report its loss.
FORK_DOCS=$(mktemp -d)
cp AGENTS.md FORK_KEEP.md "$FORK_DOCS"/

# 2. Reset the working branch to the current upstream tree
git reset --hard wei-shaw/main

# 3. Check for fork-only leftovers, then restore the fork docs
git status --porcelain
cp "$FORK_DOCS"/AGENTS.md "$FORK_DOCS"/FORK_KEEP.md .

# 4. Find current feature commits by subject, then replay or re-implement them
git log --oneline --all --grep="keep oauth accounts schedulable"
git log --oneline --all --grep="stream stale"
git log --oneline --all --grep="harmony"
git log --oneline --all --grep="service tier"

# 5. Re-apply the one .github divergence, delete the upstream CLA workflow,
#     then re-add the fork docs. AGENTS.md is untracked again after step 2 and
#     is ignored, so it needs -f.
#     git rm .github/workflows/cla.yml
#     git add -f AGENTS.md && git add FORK_KEEP.md
# 6. Update VERSION, run the verification gates, and inspect the complete diff
```

When a cherry-pick conflicts with a major upstream file split:

1. Keep the current upstream file structure.
2. Re-home the fork behavior at the semantic replay anchor.
3. Re-apply the relevant tests and invariants.
4. Search for duplicate declarations and stale imports.
5. Verify runtime call-site cardinality, not just patch application.

Before staging a replay, verify that no fork module path leaked into the code. Scope the check to Go sources and module files: `AGENTS.md` contains the fork name on purpose, so an unscoped grep reports an expected match and gives no signal.

```bash
git diff --cached -- '*.go' backend/go.mod backend/go.sum | grep 'tickernelz/sub2api'   # expected: no output
```

## 5. Verification gates

Run from the repository root. Do not push a synchronization or release commit while a required gate is red or unverified.

Four CI jobs assert the Go toolchain with a hard-failing `go version | grep -q 'go1.27.0'` (`backend-ci.yml` twice, `security-scan.yml`, `release.yml`). `backend/go.mod` declares `go 1.27.0` and carries no `toolchain` directive, so local gates must run on that exact version or their results do not represent CI.

### Backend

```bash
cd backend
export OPENAI_API_KEY=
export PATH="$(go env GOPATH)/bin:$PATH"
go build ./...
go vet ./...
go test -tags=unit ./...
go test -tags=integration ./...
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Use the same `golangci-lint` version as CI, currently v2.13 (see `version:` under the `golangci-lint` job in `.github/workflows/backend-ci.yml`):

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0
"$(go env GOPATH)/bin/golangci-lint" run --timeout=30m
```

Older linter releases cannot analyse this module: v2.9.0 fails every package with `could not load export data ... version 4 is greater than maximum supported version 2` against the Go toolchain pinned in `backend/go.mod`.

`go test ./...` alone is not the CI unit gate because it skips tests guarded by `//go:build unit`.

### Frontend

CI pins pnpm 9 and Node 20 (`pnpm/action-setup@v6` with `version: 9`, `node-version: '20'` in `backend-ci.yml`). Newer pnpm releases no longer read the `pnpm.overrides` field this repo uses, so `pnpm install --frozen-lockfile` fails with `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH` on pnpm 10+. Run the gate on pnpm 9 rather than regenerating the lockfile.

`make test-frontend` calls bare `pnpm`, so installing with pnpm 9 is not enough on its own: the whole gate must resolve `pnpm` to 9. When the system pnpm is newer, put a pnpm 9 shim first on `PATH` for both steps.

```bash
export CI=true
mkdir -p /tmp/pnpm9 && printf '#!/bin/sh\nexec npx -y pnpm@9 "$@"\n' > /tmp/pnpm9/pnpm && chmod +x /tmp/pnpm9/pnpm
export PATH="/tmp/pnpm9:$PATH"
(cd frontend && pnpm install --frozen-lockfile)
make test-frontend
```

`CI=true` is required: without a TTY, pnpm otherwise aborts with `ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY`. Verified green: 14 test files, 171 tests.

`make test-frontend` is `lint:check` + `typecheck` + `test-frontend-critical`, a subset of the suite. When replaying frontend work, also run the full suite and the release-only build steps, which belong to the release job rather than CI:

```bash
cd frontend
export CI=true
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