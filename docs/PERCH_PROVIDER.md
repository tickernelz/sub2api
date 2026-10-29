# Perch provider adapter

Perch has no API keys: the official CLI, Desktop app and web app all authenticate with a per-account Supabase session and then call a single model proxy endpoint. `internal/pkg/perch` implements that protocol so sub2api can expose Perch as an OpenAI-compatible provider.

## Adding an account in the admin UI

1. Accounts -> Create account -> pick `Perch` in the platform row.
2. Choose the sign-in provider the Perch account uses (Google or GitHub) and press **Next**.
3. Press **Generate Login URL**. The admin API asks Perch for its auth config, builds the same authorization URL the official CLI builds and returns it; the flow panel shows it.
4. Open the URL and sign in. The browser lands on `http://127.0.0.1:45999/callback?code=…`. That page shows a connection error on purpose: nothing listens on that port outside the CLI. The authorization code is in the address bar.
5. Copy the whole address, paste it into **Callback URL**, press **Complete Authorization**. The server exchanges the code with PKCE, creates the account and shows it in the account list.

The credential map stored on the account is `access_token`, `refresh_token`, `app_url`, `expires_at`; the adapter rewrites `refresh_token`/`expires_at` whenever Perch rotates the session. No file from the Perch CLI, and no token typed by hand, is part of this flow: the form carries no token, session-JSON or app-URL field. `app_url` is a server setting (`PERCH_APP_URL`, default `https://app.perchai.app`).

Groups work exactly like the other OAuth platforms: the account joins whatever groups the form selects, and nothing is created implicitly.

### Editing an account

Edit on a Perch row carries no token or app-URL field: the session stays exactly as stored (the backend preserves sensitive credential keys the payload does not carry), so name, group, concurrency, priority and status save without touching credentials. To replace a session, use **Re-authorize** in the account action menu, which runs the same login steps and writes the new credentials onto that account instead of creating a second one.

Editing never writes `api_key`, `base_url`, `upstream_billing_probe_enabled` or `upstream_billing_rate_sync_enabled` for a Perch account, because the adapter reads none of them and a probe without an api key would reschedule forever.

The account answers `/v1/chat/completions` only. `/v1/responses` returns `unsupported_protocol` with HTTP 400, and the model name in a request is a Perch tier alias or catalogue id, not an OpenAI model name.

Perch accounts are never upstream-billing-probe targets. They are stored as `type=apikey` but hold no `credentials.api_key`, so a probe can only record `missing_api_key` and reschedule forever; `IsUpstreamBillingProbeIdentity` rejects the platform, the admin rate cell and the bulk-edit probe toggle hide it, and `249_clear_perch_upstream_billing_probe_flags.sql` clears flags left on older rows.

### Re-authorizing an existing account

Accounts created from an older session file keep working untouched: the adapter only needs the stored credentials. When the session is revoked upstream, open the account's **Re-authorize** action, run the same login steps, and the account's credentials are replaced instead of a second account being created.

### Why the callback is pasted instead of caught

Measured 2026-10-29 against the live Supabase project Perch uses (`GET {appUrl}/api/perch-terminal/cli-auth/config`):

| redirect_to | accepted |
| --- | --- |
| `http://127.0.0.1:<port>/callback` (random or fixed port) | yes, the code arrives in the address bar |
| `https://<sub2api host>/...` | no, Supabase silently falls back to the Perch site URL |

The redirect allowlist belongs to Perch's Supabase project, so sub2api cannot add its own host. Loopback is the only destination that receives the code, which is why the flow asks for the pasted callback URL — the same shape the OpenAI/Codex loopback flow uses. The port is fixed at 45999 so the address the operator has to recognise does not change between logins.

### Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `perch_credentials_missing` | account row has no `access_token` | run the login steps again for that account |
| 401 `not_authenticated` twice in a row | session revoked upstream, or refreshed by another client with the same token | re-authorize the account |
| 403 `perch_surface_required` | the model call did not lead with the surface system prompt | adapter bug, not a credential problem: check `surfacePromptText` and its floor test |
| 429 `usage_limit_reached` / `turn_rate_limited` | Perch plan or ticket rate limit | wait for `Retry-After`, or use another perch account in the group |
| `PERCH_OAUTH_SESSION_NOT_FOUND` | the login session expired (10 min) or was already used | start the login again |
| `PERCH_OAUTH_SESSION_FORBIDDEN` | the login was started by another admin | start your own login; sessions are bound to the admin who began them |
| `PERCH_OAUTH_CALLBACK_ERROR` | the provider returned `error=access_denied` (consent cancelled) | sign in again and allow access |
| Perch web app answers `Access blocked` | Perch banned the account for serving another app or tool | the ban is vendor-side: the login still completes, but `turn-ticket` answers `401 not_authenticated` until Perch lifts it |

## Account credentials

The adapter owns the session: it refreshes and writes rotated tokens back to the account row.

| Field | Meaning |
| --- | --- |
| `appUrl` | Perch app origin, from `PERCH_APP_URL`, default `https://app.perchai.app` |
| `accessToken` | Supabase access token (JWT) |
| `refreshToken` | Supabase refresh token, rotated on every refresh |
| `expiresAt` | Unix seconds; refresh when less than 90s remain |
| `userId`, `email` | Account identity for logging and attribution |

Sessions come from the login endpoints below, not from a file on disk. Keep any exported session out of the repository, a migration and a log.

## Login endpoints

`POST /api/v1/admin/perch/auth-url` (admin) — body `{provider?}`, response `{auth_url, session_id, provider, providers, app_url, redirect_uri, expires_in}`. The provider must be one Perch advertises in `cli-auth/config` (`google`, `github`); the default is the first advertised one. App URL and redirect target come from the server (`PERCH_APP_URL` and the loopback redirect), never from the request. `code_verifier` stays server-side, in a session store with a 10-minute TTL that is single-use and bound to the admin who started it; with Redis configured (`redissession`) the consume is authoritative in Redis, and the process map is only a fallback for sessions whose Redis write failed.

`POST /api/v1/admin/perch/exchange-code` (admin) — body `{session_id, callback_url | code}`, response `{access_token, refresh_token, app_url, expires_at, user_id, email}`. The callback URL or a bare code is accepted; `error`/`error_description` in the callback is reported as `oauth_callback_error` before any network call. The verifier is consumed before the exchange, so a second attempt with the same `session_id` answers `PERCH_OAUTH_SESSION_NOT_FOUND`.

Wire shape, pinned byte for byte by `login_fixture_test.go` against `testdata/login/*`:

```
GET {supabaseUrl}/auth/v1/authorize?provider=<google|github>&redirect_to=<encoded>&code_challenge=<S256>&code_challenge_method=s256[&prompt=select_account]
POST {supabaseUrl}/auth/v1/token?grant_type=pkce   {"auth_code":"…","code_verifier":"…"}
    host, connection: keep-alive
    Content-Type: application/json;charset=UTF-8
    Authorization: Bearer <publishable key>
    apikey: <publishable key>
    X-Client-Info: supabase-js-node/2.106.0
    X-Supabase-Api-Version: 2024-01-01
    accept, accept-language, sec-fetch-mode, user-agent: node, accept-encoding, content-length
```

The header set and its order come from the CLI capture (`~/re/perchai/captures/raw/flows.jsonl`, flow 2); `wire.go` emits that order through `supabaseTokenWireHeaders`, and the login client dials Supabase through `NewWireDoer(FingerprintDialer(nil))` exactly like the gateway and refresh paths, so the TLS hello and header order match the CLI instead of Go's defaults. The captured `X-Client-Info` value is redacted in the flow capture, but it is a public library tag: `perchai-cli` 2.4.120 bundles supabase-js and builds the header as `supabase-js-${runtime}/${version}` with runtime `node` and version `2.106.0` (`~/re/perchai/artifacts/package/dist/perch.mjs`), which is 24 characters, the length the capture records. The constant carries that value.

`prompt=select_account` is sent for Google only, matching the CLI. The publishable key is fetched per login from `cli-auth/config`, never hardcoded. Upstream 401/403/5xx are reported to the admin API as 502 with a `PERCH_OAUTH_*` reason, so the panel's own 401 handling never treats an upstream refusal as an expired admin session.

## Flow

1. `GET {appUrl}/api/perch-terminal/cli-auth/config` returns `supabaseUrl` and the publishable `supabaseAnonKey` used for refresh.
2. Refresh: `POST {supabaseUrl}/auth/v1/token?grant_type=refresh_token` with `apikey: <anonKey>`.
3. `GET {appUrl}/api/perch-terminal/public-model-default` returns `selection.chatModelId`, the model the CLI itself would run. Cached ten minutes; its id becomes `preferredModelId` whenever the caller names no model or a tier alias.
4. `GET {appUrl}/api/perchai/account` supplies identity and plan for attribution, cached ten minutes. It is fetched before the ticket so the on-wire order matches the CLI, which asks for defaults, then the account, then a ticket.
5. `POST {appUrl}/api/perch-terminal/turn-ticket` `{"surface":"cli","profile":"standard"}` mints a short-lived run ticket; tickets are cached and renewed 30s before expiry.
6. `POST {appUrl}/api/perch-terminal/model-call` with `Authorization: Bearer <access token>` and `x-perch-turn-ticket`. A 401 refreshes the session, re-mints the ticket and retries once.
7. The upstream answer is an SSE stream of `answer_delta` / `reasoning_delta` / `tool_call_delta` / `done` events; `chat.go` converts them into `chat.completion.chunk` frames and aggregates them for non-streaming callers.

Usage is camelCase (`inputTokens`, `outputTokens`, `cacheReadInputTokens`, `cacheCreationInputTokens`). `inputTokens` excludes cache traffic, so `prompt_tokens` is the sum of the three input counters and `cached_tokens` carries the cache-read share.

## Wiring

The platform is registered: `domain.PlatformPerch` ("perch", display "Perch") sits in `domain.platformList` with `PlatformGatewayOpenAI`, and `service.PlatformPerch` mirrors it for the service layer. Registration is what makes a perch account reachable: the OpenAI gateway handler selects accounts by `spec.Gateway`, so removing the `platformList` row silently removes the platform from every list and from the scheduler.

Dispatch: `forwardAsChatCompletions` hands a perch account to `OpenAIGatewayService.forwardPerchChatCompletions` before any Codex or protocol-routing work. That path builds the Perch model call from the client's Chat Completions body, streams `perch.StreamChat` chunks straight into the client's SSE response, and returns `OpenAIForwardResult` with Perch's usage so the normal billing path applies. `/v1/responses` is rejected with `unsupported_protocol` because Perch exposes no Responses endpoint.

Account credentials (all optional except the access token):

| Key | Meaning |
| --- | --- |
| `app_url` | Perch app origin; defaults to `https://app.perchai.app` |
| `access_token` | Supabase access token (required) |
| `refresh_token` | Rotated on refresh and written back to the account row |
| `expires_at` | Unix seconds; decides whether a cached client is refreshed |
| `user_id`, `email` | Session identity, used for the client cache key |

`perch.NewGatewayClient` builds the wire control and stream doers over standard TLS with ALPN `http/1.1`; refreshed credentials are persisted through `persistAccountCredentials`, so a rotated refresh token never lives only in memory. The client cache is keyed by account id, and a newer `expires_at` replaces the cached session. `Client.Health` remains the account health check (`/api/perchai/account` -> plan, entitlements, allowed models).

## Model selection

Perch does not take an OpenAI model name. `chat.go` sends a catalogue id such as `wandb-kimi-k2-6` as `manualModelOptionId` with `strictManual: true`, which is exactly what the recorded CLI does when a model is pinned. When the caller names no model, or names the `standard`/`standardMax` tier aliases, the adapter takes `selection.chatModelId` from `public-model-default` and sends it as `preferredModelId` with `strictManual: false` — the CLI's own default path. `pro` and `proMax` still travel as `roostModelChoice`, a field no recorded CLI request carries; that mapping is a convenience this gateway keeps and the live gate has not been measured for it. Roost reroutes server-side, so the model that answers is taken from `done.model`; non-streaming responses report it as `model` and echo it in `x-perch-upstream-model` when it differs from the request. Attribution (`userId`, `workspaceId`, `runId`, `lane`, `source`) is filled from `/api/perchai/account` and cached for ten minutes.

## Error mapping

| Upstream | Client status | Retried |
| --- | --- | --- |
| `not_authenticated` | 401 | once, after a session refresh |
| `perch_surface_required` | 403 | never |
| `usage_limit_reached`, `turn_rate_limited`, `limitKind.roost_*` | 429 with `Retry-After` from `limit.resetAt` | never |
| `context_overflow` | 400 | never |
| `plan_required` | 402 | never |
| `model_blocked` | 403 | never |
| `provider_not_configured` | 400 | never |
| `stream_interrupted`, transport failures, upstream 5xx | 502 | up to 3 attempts on the same account, 1s and 3s apart |

A stream that closes without a `done` event is reported as `stream_interrupted` and never closed with `[DONE]`. A failed turn-ticket mint refreshes the session and mints again instead of calling the model proxy without a ticket.

Failover: only a dead credential (`not_authenticated`, `invalid_grant`) leaves the account, because a broken session cannot serve anyone. Quota (`usage_limit_reached`), rate limiting (`turn_rate_limited`), the surface refusal, plan and model blocks, and upstream 5xx stay on the account that produced them and are returned to the client. The CLI has no notion of accounts at all; moving one client request across accounts is a pattern it never emits, and it is the pattern a multi-account quota bypass leaves behind.

## Transport

`fingerprint.go` reproduces the CLI ClientHello through `internal/pkg/tlsfingerprint`: 52 cipher suites in the recorded order, the captured 12-extension list (server_name second, ending 43/45/51), curves `4588,29,23,30,24,25,256,257`, point formats `0,1,2`, 26 signature algorithms starting at 2309, key-share groups X25519MLKEM768 and X25519, and ALPN `http/1.1` only. `wire.go` writes the HTTP/1.1 bytes over that connection. The JA3 of the hostname dial, the one production uses, is `505556100e8b8604b580554ebf5aaed5` (extensions `65281-0-11-10-35-16-22-23-13-43-45-51`); `PerchJA3` (`fe1a22af39881f8613c4834b2a568a00`) is the same profile dialled by IP, where uTLS sends no server_name.

The transport first failed every handshake (`remote error: tls: handshake failure`) because the profile omitted `server_name`; Cloudflare rejects a hello without SNI before the application sees it. With the extension list and the signature algorithms taken from a captured CLI hello the handshake completes and the request reaches Perch. The surface gate below is still content-based, so this layer is parity, not the gate.

The per-turn sequence follows the CLI: `public-model-default`, `account`, `turn-ticket`, `model-call`, all on one keep-alive connection. Two CLI calls stay out: `cli-context` (three per turn) and `cli-turn`. Both carry the CLI's own thread state, and replaying them would send fabricated telemetry; measured 2026-10-29, a model call that skips them still answers 200. `~/re/perchai/tools/parity.sh` is what keeps the rest honest.

## Wire layer

`net/http` always sorts request headers, so the adapter cannot reproduce the CLI's wire order through `http.Client`. `wire.go` therefore writes HTTP/1.1 itself over the TLS connection and parses the response with `http.ReadResponse`.

Header order is the one the CLI's own client (undici, as shipped in `perchai-cli`'s dependency tree) puts on the wire: request line, `host`, `connection: keep-alive`, the headers the CLI sets in its own insertion order (`Content-Type`, `x-perch-call-delta`, `Accept`, `Authorization`, `User-Agent`, `x-perch-locale`, `x-perch-turn-ticket` for model calls; `Content-Type`, `Authorization`, `User-Agent`, `x-perch-locale` for JSON control calls), then the defaults undici appends (`accept: */*` when absent, `accept-language: *`, `sec-fetch-mode: cors`, `accept-encoding: br, gzip, deflate`), and finally `content-length`. undici picks `accept-encoding` by scheme: the TLS recordings carry `br, gzip, deflate` on all 15 captured CLI requests, while `gzip, deflate` shows up only on the plain-HTTP loopback captures used for offline testing. Production Perch is HTTPS, so the adapter always sends `br, gzip, deflate`.

Kept from `net/http`: response parsing, chunked decoding, connection reuse. Response bodies are decompressed for `gzip`, `deflate` and `br` because the request advertises two of them and Perch may answer compressed with any of the three; without it the JSON and SSE readers see binary. A connection returns to the pool only when the body was read to EOF, the response allows reuse and no buffered bytes remain; an SSE stream closed early drops its connection. Before an idle connection is reused it is peeked without consuming bytes (`recvfrom` with `MSG_PEEK|MSG_DONTWAIT` on unix; the TLS wrapper is unwrapped with `NetConn` first, and platforms without the probe never reuse), so a pending byte or a queued EOF drops the connection instead of being read as the next response. A body settles exactly once through `sync.Once`, so a concurrent `Read` and `Close` cannot release and close the same socket. Header names and values go to the socket verbatim, so `checkWireHeader` rejects any name or value carrying a control byte (`errWireHeader`) before a byte leaves: a token, ticket or credential containing a newline cannot inject a header line. Two doers cover the two deadlines: control calls get a 60s whole-exchange timeout, model calls get a response-header timeout and no total timeout (an idle reader still trips the 60s stream guard). `WireDoer.Share` hands both doers one idle pool, so a turn keeps a single TCP connection across control and model calls, like the CLI.

Evidence:

- `testdata/wire/model-call.http` and `testdata/wire/turn-ticket.http` are the bytes the CLI's undici stack produced for the same requests; `TestWireDoerMatchesRecordedModelCallBytes` and `TestWireDoerMatchesRecordedTurnTicketBytes` compare them byte for byte.
- Measured 2026-10-29 through one forwarding proxy, the official CLI and the Go adapter emit the same request line, the same 13 headers in the same order with the same casing and values. The only remaining differences are inside the body, and one of them gates the request (see the surface gate).

## Request parity

Every layer below is measured by `~/re/perchai/tools/parity.sh`, which drives this package against a loopback mock through the production wire doer and compares the captured bytes with the CLI 2.4.120 recording in `~/re/perchai/golden/`. It exits 0 only when every layer matches or is one of the accepted differences.

| Layer | CLI 2.4.120 (recorded) | Adapter | Status |
| --- | --- | --- | --- |
| TLS ClientHello | JA3 `fe1a22af39881f8613c4834b2a568a00`, 52 suites, ALPN `http/1.1` | identical | match |
| request sequence per turn | public-model-default, account, turn-ticket, model-call | identical | match |
| keep-alive | one connection carries the turn | identical | match |
| request line, header order and casing (model-call, turn-ticket) | exact | identical | match |
| control-plane headers (account, public-model-default) | two CLI stacks: `node` UA on the fetch path, `perchai-cli/2.4.120` on the node:https path | `perchai-cli/2.4.120`, same header set | match inside the CLI's own variation |
| accept-encoding | `br, gzip, deflate` | identical | match |
| turn-ticket body | `{"surface":"cli","profile":"standard"}` | identical | match |
| model-call key order, top level and `request` | exact | identical | match |
| `temperature`, `maxTokens`, `toolChoice` | 0.6 / 32000 / `"auto"` | same defaults when the caller omits them | match |
| `attribution` keys | userId, workspaceId, runId, lane, source, billingMultiplier | identical | match |
| `lane`, `clientSurface`, `resendAfterOutput`, `strictManual` | chat / cli / true / false | identical | match |
| pacing | tightest observed gap 314 ms | 300 ms floor | match |
| concurrent model calls | at most 2 (`race2`) | at most 2, spaced | match |
| retry budget | 3 attempts, 1 s and 3 s backoff, one account | identical | match |
| `messages` | 53,561-char vendor persona + one user turn | own surface prompt + caller messages | accepted: the vendor prompt is not reproducible, and the gate only needs about 202 characters |
| `tools` | 45 built-in schemas the CLI can execute | caller's tools, `[]` when none | accepted: advertising tools this gateway cannot execute would return tool calls it cannot answer |
| `tools` + `toolChoice` together | 45 tools with `"auto"` | `[]` with `"auto"` | accepted but unmeasured: the CLI never sends an empty list, so the server's reading of that pair is unknown (see below) |
| `avoidModelIds` | the CLI's saved avoid list (28 ids) | `[]` | accepted: the list belongs to the CLI's local model state |
| `preferredModelId` source | the CLI's saved model | `selection.chatModelId` from public-model-default | accepted: same field, different source |
| `cli-context` (3 per turn) | memory retrieval for the CLI's own thread | not sent | accepted: it reports the CLI's context assembly |
| `cli-turn` | uploads the finished transcript (136 KB) | not sent | accepted: re-uploading the conversation is a privacy cost with no protocol role |

Not measurable offline. Each needs one live turn, and none is in the table until then:

- whether `perch_surface_required` reads anything past the ~202-character system prompt floor (messages, tools, model id);
- whether an empty `tools` list combined with `toolChoice: "auto"` is treated like the CLI's 45 tools plus `"auto"`. The CLI always ships tools with `auto`, and the last recorded provider turn used `toolChoice: null`, so this exact combination has never been observed on the wire;
- whether the server scores the presence of `cli-context` and `cli-turn`, or reads a missing pair as a client mismatch;
- how a `race2` overlap compares with a single call in whatever signal the server keeps;
- whether a login from the admin UI produces the same Supabase session shape as `perch login` (device, `signup_source`, token family).

## Client identity

Perch only serves hosted models to its own surfaces, so the adapter presents the same client identity the official CLI does, the way the codex and claude providers already do for their upstreams:

| Piece | Value |
| --- | --- |
| `User-Agent` | `perchai-cli/<version>`; `PERCH_CLI_VERSION` overrides it, strict semver only |
| `x-perch-locale` | `en` |
| `x-perch-call-delta` | `1` on model calls only |
| `accept-language`, `sec-fetch-mode` | `*`, `cors` |
| `Accept-Encoding` | `br, gzip, deflate`, the value every recorded TLS CLI request carries, decoded by `encoding.go` (brotli included) |
| turn context | a `<perch_turn_context>` user message with today's date, inserted after any system messages |
| system prompt | `surfacePromptText` as the first message, ≥ `surfacePromptFloor` (205) characters, ahead of any caller system message (see the surface gate) |

`identity.go` resolves the version once per process, so the header cannot change between requests. All outbound Perch calls go through one header applier; the turn ticket request deliberately omits delta mode. `wire.go` decides where each header lands on the wire, so no call site can reorder them.

## Turn context

`chat.go` prepends a synthetic `user` message wrapped in `<perch_turn_context>` that states today's date and that its content carries system-prompt authority, matching what the official CLI sends. That changes the meaning of the caller's own prompt, so it is a switch: `perch.WithTurnContext(false)` on the handler disables it and passes the conversation through untouched. Default is on, because parity with the CLI is the goal.

## Body shape

The model-call body follows the captured CLI byte shape rather than the spec table: `request` always carries `messages`, `tools`, `temperature`, `maxTokens` and `toolChoice`. An empty tool list stays `[]`; an unset `toolChoice` becomes `"auto"`; an omitted `temperature` or `maxTokens` takes the CLI's 0.6 and 32000. `preferredModelId` is `null` only when no public default could be read, `attribution` always carries the CLI's `billingMultiplier` key, and `resendAfterOutput: true` is sent on every call. `TestModelCallBodyKeyOrderMatchesRecordedCLI` decodes the marshalled JSON and checks the key order, so a struct reorder cannot silently change the wire shape.

## Concurrency

Ticket minting is single-flight through a one-slot gate, so a caller waiting behind another goroutine's mint is released by its own context instead of the control-plane deadline. The per-ticket budget leaves a tombstone: a spent ticket keeps its counter until it expires, so workers that still hold it cannot re-acquire budget, while a rotation mints exactly one replacement that the other workers share.

Control-plane calls (`cli-auth/config`, `turn-ticket`, `account`) keep a 60s total deadline even when the wire transport is active; only the model-call client runs without a total timeout and relies on the 60s idle read instead.

Every request an account sends passes one gate: starts are serialised with a 300 ms floor between them, and at most two exchanges are in flight. Both numbers come from the recording rather than taste — the CLI's tightest observed gap in a turn is 314 ms, and `raceMode "race2"` in `perch.mjs` fires at most two model calls in parallel. Retries follow the CLI budget as well: three attempts with 1 s and 3 s backoff, always on the same account.

One `Client` (and one `Session`) exists per account: the handler creates it under its own mutex, so two cold-start requests cannot mint two sessions that refresh with the same rotated token. Cached clients are reference-counted while a request uses them, and eviction only removes unpinned entries. The turn-ticket budget is reserved under the client mutex, so the 40-call limit and the reuse counter cannot be raced.

## Streaming limits

Model calls get their own HTTP client with no total timeout: `net/http` measures `Client.Timeout` across the body, which would cut any turn longer than a minute. The 60s budget is applied per read instead — an idle body is closed and reported as `stream_interrupted` — and the transport keeps a 60s `ResponseHeaderTimeout` for the header phase. When Perch restarts a stream after output already reached the client, the response ends as `stream_interrupted` rather than replaying the resent text.

A turn ticket is reusable for up to 40 model calls on the `standard` profile; the client mints a new one after that instead of running into the vendor limit. The vectors above were measured in six rounds of four to six model calls, each round on one freshly minted ticket.

## Session ownership

Do not point this provider at the session file the CLI is using. Supabase refresh tokens rotate, and a second client refreshing the same token can revoke the token family: after the adapter refreshed `~/.perch/cli-auth-session.json` once more, both the adapter and the official CLI started answering `401 not_authenticated` / `Not authorized` on that session. Import a snapshot of the session at account creation (copy the file, then let the adapter own the copy) and keep the CLI session separate.

The revoked family is recoverable without a browser: `POST {supabaseUrl}/auth/v1/token?grant_type=refresh_token` with `apikey: <publishable key from cli-auth/config>`, then write the rotated pair back to the session file. Verified 2026-10-29 04:33Z: refresh rotated, `GET /api/perchai/account` returned the account, and the official CLI completed a turn on the same session minutes later.

## Surface gate

`POST /api/perch-terminal/model-call` answers 403 `perch_surface_required` when the request carries no substantial system prompt. That is the whole check: it is a length floor, not a persona check and not a transport check.

Measured 2026-10-29 against one account with one turn ticket, every variant sent through the same forwarding proxy with identical headers:

| Variant | System prompt | Result |
| --- | --- | --- |
| CLI turn | 53,561 chars | 200 |
| provider body as shipped before this fix | none (user messages only) | 403 |
| first 200 chars of the CLI prompt | 200 chars | 403 |
| first 205 chars of the CLI prompt | 205 chars | 200 |
| 3,000 `x` characters, no persona text | 3,000 chars | 200 |
| full CLI prompt, different user message | 53,561 chars | 200 |
| caller system message only, no injected prompt | short | 403 |

The boundary sits between 200 and 205 characters. Content, key order, tools, `preferredModelId`, `attribution` and the wire transport made no difference to the gate: a CLI body replayed from Go with a Go-minted ticket returned 200, and a provider body truncated to a 200-character system prompt returned 403.

`chat.go` therefore sends `surfacePromptText` (898 chars, `surfacePromptFloor` 205 in `surface_test.go`) as the first message of every model call and keeps the caller's system messages behind it. `TestSurfacePromptSatisfiedByProviderText` fails if that text ever drops below the floor; `TestBuildModelCallRequestLeadsWithSurfacePrompt` asserts the first message on the wire is that system message and that the body starts with it. Negative control: truncating `surfacePromptText` to 22 characters fails both tests, and restoring it reproduces the byte-identical file.

## Live proof

Measured 2026-10-29 through the `sub2api` container serving `/v1/chat/completions` with a Perch account and a `perch` group:

| Call | Status | TTFB | Total | Answer | Upstream model | Usage |
| --- | --- | --- | --- | --- | --- | --- |
| `stream:false` | 200 | 3.035 s | 3.035 s | `pong` | `deepseek/deepseek-v4.1-flash` | 293 prompt / 4 completion / 297 total |
| `stream:true` | 200 | 0.981 s | 1.127 s | `pong` in `chat.completion.chunk` frames, ends with `[DONE]` | reported by the `done` event | same turn path |

The upstream model that answered comes from Perch's `done` event, not from the request: the request named `wandb-kimi-k2-6` and Perch routed the turn to `deepseek/deepseek-v4.1-flash`.

## Login proof status

The login endpoints are covered by fixtures and a local Supabase mock, not by a live call:

| Check | Where it is proven |
| --- | --- |
| authorize URL shape (parameters, order, `prompt=select_account` only for Google) | `login_fixture_test.go` compares against `testdata/login/authorize-{google,github}.txt` byte for byte |
| PKCE exchange request (method, path, `grant_type`, header names/order/values, body) | `testdata/login/exchange-pkce.txt` compared against `buildWireRequest` output, byte for byte |
| used code, expired code | local mock answers `invalid flow state` / `invalid_grant`; the service maps them and no token reaches the message |
| single use, admin binding, TTL | store and service tests, plus a mutation control on the delete |

Not measured live: a real browser login through Perch and one gateway request with credentials from this flow. The two accounts used for the earlier rounds were banned by the vendor (`Access blocked` in the Perch web app), so the last mile needs an unbanned account. Procedure when one exists: start the login from the account form, complete it in a browser, confirm the account row carries `access_token`/`refresh_token`/`expires_at`, then call `/v1/chat/completions` twice (stream and non-stream) through a `perch` group and record status, TTFB, total and the model from the `done` event.

## Tests

```bash
cd backend && go test -tags unit ./internal/pkg/perch/   # fixtures captured from the real proxy
cd backend && PERCH_SESSION_JSON="$(cat /path/to/session.json)" go test -tags 'unit perchlive' -run TestLivePerchGatewayModelCall -v ./internal/pkg/perch/   # one live turn through the production wiring
```

Fixtures under `internal/pkg/perch/testdata` keep the captured wire shapes with synthetic content; no session token, ticket or vendor prompt text is stored.

The byte-for-byte differential against the CLI recording is outside the repository (it needs the capture set):

```bash
bash ~/re/perchai/tools/parity.sh    # exits 0 only when every measured layer matches
```

It runs the `perchparity` harness against a loopback mock, captures the production wire bytes, and compares TLS ClientHello, request order, headers, body skeleton, pacing and keep-alive with `~/re/perchai/golden/`. No request leaves the box.
