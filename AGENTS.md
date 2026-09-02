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

## 3. Upstream and workflow divergence

The fork follows upstream `.github/` workflows except for these intentional changes:

| File | Fork-only difference | Reason |
| --- | --- | --- |
| `.github/workflows/cla.yml` | References `tickernelz/sub2api` for the repository guard and fork CLA link | The CLA job and link belong to this fork |
| `.github/workflows/release.yml` | `continue-on-error: true` on `Update DockerHub description` | Docker Hub can return `403` even when image publication succeeds |
| All other `.github/` files | No intentional divergence | Follow upstream |

Do not restore an old fork snapshot of `.github/`. Start from the current upstream workflows and re-apply only the two differences above.

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

# 5. Re-apply only the two .github divergences, then re-add the fork docs.
#     AGENTS.md is untracked again after step 2 and is ignored, so it needs -f.
#     git add -f AGENTS.md && git add FORK_KEEP.md
# 6. Update VERSION, run the verification gates, and inspect the complete diff
```

When a cherry-pick conflicts with a major upstream file split:

1. Keep the current upstream file structure.
2. Re-home the fork behavior at the semantic replay anchor.
3. Re-apply the relevant tests and invariants.
4. Search for duplicate declarations and stale imports.
5. Verify runtime call-site cardinality, not just patch application.

Before staging a replay, verify that no fork module path leaked into the code. Scope the check to Go sources and module files: `.github/workflows/cla.yml` and `AGENTS.md` contain the fork name on purpose, so an unscoped grep reports expected matches and gives no signal.

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

The CI `frontend` job then runs one command. Match it first:

```bash
cd frontend
export CI=true
pnpm install --frozen-lockfile
cd .. && make test-frontend
```

`CI=true` is required: without a TTY, pnpm otherwise aborts with `ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY`.

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