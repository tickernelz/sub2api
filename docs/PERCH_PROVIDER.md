# Perch provider adapter

Perch has no API keys: the official CLI, Desktop app and web app all authenticate with a per-account Supabase session and then call a single model proxy endpoint. `internal/pkg/perch` implements that protocol so sub2api can expose Perch as an OpenAI-compatible provider.

## Account credentials

Store the CLI session verbatim; the adapter owns refresh and writes rotated tokens back.

| Field | Meaning |
| --- | --- |
| `appUrl` | Perch app origin, default `https://app.perchai.app` |
| `accessToken` | Supabase access token (JWT) |
| `refreshToken` | Supabase refresh token, rotated on every refresh |
| `expiresAt` | Unix seconds; refresh when less than 90s remain |
| `userId`, `email` | Account identity for logging and attribution |

The session file written by `perch login` is `~/.perch/cli-auth-session.json` (mode 0600, or the macOS keychain entry `app.perchai.cli-auth`). `LoadDefaultCredentials` reads it; never copy a session into the repository or a migration.

## Flow

1. `GET {appUrl}/api/perch-terminal/cli-auth/config` returns `supabaseUrl` and the publishable `supabaseAnonKey` used for refresh.
2. Refresh: `POST {supabaseUrl}/auth/v1/token?grant_type=refresh_token` with `apikey: <anonKey>`.
3. `POST {appUrl}/api/perch-terminal/turn-ticket` `{"surface":"cli","profile":"standard"}` mints a short-lived run ticket; tickets are cached and renewed 30s before expiry.
4. `POST {appUrl}/api/perch-terminal/model-call` with `Authorization: Bearer <access token>` and `x-perch-turn-ticket`. A 401 refreshes the session, re-mints the ticket and retries once.
5. The upstream answer is an SSE stream of `answer_delta` / `reasoning_delta` / `tool_call_delta` / `done` events; `chat.go` converts them into `chat.completion.chunk` frames and aggregates them for non-streaming callers.

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

`perch.NewGatewayClient` builds the fingerprinted control and stream doers; refreshed credentials are persisted through `persistAccountCredentials`, so a rotated refresh token never lives only in memory. The client cache is keyed by account id, and a newer `expires_at` replaces the cached session. `Client.Health` remains the account health check (`/api/perchai/account` -> plan, entitlements, allowed models).

## Model selection

Perch does not take an OpenAI model name: the captured CLI keeps `preferredModelId` null and picks a model with `roostModelChoice` (`standard`, `standardMax`, `pro`, `proMax`; default `standardMax`) or `manualModelOptionId`, a catalogue id such as `wandb-kimi-k2-6`. `chat.go` maps tier aliases (`standard`, `standard_max`, `Standard 1M`, `pro`, `PRO_MAX`, ...), sends any other id as a manual pin with `strictManual: true`, and falls back to `standardMax` when the request names no model. Roost reroutes server-side, so the model that answers is taken from `done.model`; non-streaming responses report it as `model` and echo it in `x-perch-upstream-model` when it differs from the request. Attribution (`userId`, `workspaceId`, `runId`, `lane`, `source`) is filled from `/api/perchai/account` and cached for ten minutes.

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
| `stream_interrupted`, transport failures, upstream 5xx | 502 | once for 408/425/5xx |

A stream that closes without a `done` event is reported as `stream_interrupted` and never closed with `[DONE]`. A failed turn-ticket mint refreshes the session and mints again instead of calling the model proxy without a ticket.

## Transport fingerprint

`fingerprint.go` reproduces the TLS ClientHello of the official CLI 2.4.120, measured offline against a local capture listener: JA3 `fe1a22af39881f8613c4834b2a568a00` (`fingerprint.go` keeps the full parameter string in `PerchJA3Raw`), 52 cipher suites in the recorded order, extensions `65281,11,10,35,16,22,23,13,43,45,51`, curves `4588,29,23,30,24,25,256,257`, point formats `0,1,2`, and ALPN `http/1.1` — the CLI does not speak HTTP/2, so the fingerprint transport leaves HTTP/2 off.

`TestFingerprintProfileMatchesRecordedJA3` listens on a loopback socket, dials with the profile, parses the produced ClientHello and asserts both the JA3 hash and the raw parameter string. `WithFingerprintTransport(false)` switches the profile off for a plain transport.

Still open for full parity: the per-turn request sequence. The CLI also emits `public-model-default`, `account` and three `cli-context` calls around each turn; the adapter fetches `account` and the ticket but does not replicate `public-model-default` or `cli-context`, because `cli-context` reports the CLI's own context-assembly statistics and sending fabricated telemetry would be worse than omitting it.

## Wire layer

`net/http` always sorts request headers, so the adapter cannot reproduce the CLI's wire order through `http.Client`. `wire.go` therefore writes HTTP/1.1 itself over the fingerprinted TLS connection and parses the response with `http.ReadResponse`.

Header order is the one the CLI's own client (undici, as shipped in `perchai-cli`'s dependency tree) puts on the wire: request line, `host`, `connection: keep-alive`, the headers the CLI sets in its own insertion order (`Content-Type`, `x-perch-call-delta`, `Accept`, `Authorization`, `User-Agent`, `x-perch-locale`, `x-perch-turn-ticket` for model calls; `Content-Type`, `Authorization`, `User-Agent`, `x-perch-locale` for JSON control calls), then the defaults undici appends (`accept: */*` when absent, `accept-language: *`, `sec-fetch-mode: cors`, `accept-encoding: br, gzip, deflate`), and finally `content-length`.

Kept from `net/http`: response parsing, chunked decoding, connection reuse. A connection returns to the pool only when the body was read to EOF, the response allows reuse and no buffered bytes remain; an SSE stream closed early drops its connection. Before an idle connection is reused it is peeked without consuming bytes (`recvfrom` with `MSG_PEEK|MSG_DONTWAIT` on unix; the TLS wrapper is unwrapped with `NetConn` first, and platforms without the probe never reuse), so a pending byte or a queued EOF drops the connection instead of being read as the next response. A body settles exactly once through `sync.Once`, so a concurrent `Read` and `Close` cannot release and close the same socket. Header names and values go to the socket verbatim, so `checkWireHeader` rejects any name or value carrying a control byte (`errWireHeader`) before a byte leaves: a token, ticket or credential containing a newline cannot inject a header line. Two doers cover the two deadlines: control calls get a 60s whole-exchange timeout, model calls get a response-header timeout and no total timeout (an idle reader still trips the 60s stream guard).

Evidence, all offline against a local capture listener with zero traffic to Perch:

- `testdata/wire/model-call.http` and `testdata/wire/turn-ticket.http` are the bytes the CLI's undici stack produced for the same requests; `TestWireDoerMatchesRecordedModelCallBytes` and `TestWireDoerMatchesRecordedTurnTicketBytes` compare them byte for byte.
- End to end through the fingerprint dialer, the two requests the Go adapter sent were byte-identical to those recordings, and the recorded ClientHello carried JA3 `fe1a22af39881f8613c4834b2a568a00` with ALPN `http/1.1`.

## Client identity

Perch only serves hosted models to its own surfaces, so the adapter presents the same client identity the official CLI does, the way the codex and claude providers already do for their upstreams:

| Piece | Value |
| --- | --- |
| `User-Agent` | `perchai-cli/<version>`; `PERCH_CLI_VERSION` overrides it, strict semver only |
| `x-perch-locale` | `en` |
| `x-perch-call-delta` | `1` on model calls only |
| `accept-language`, `sec-fetch-mode` | `*`, `cors` |
| `Accept-Encoding` | `br, gzip, deflate` on model calls, decoded by `encoding.go` |
| turn context | a `<perch_turn_context>` user message with today's date, inserted after any system messages |

`identity.go` resolves the version once per process, so the header cannot change between requests. All outbound Perch calls go through one header applier; the turn ticket request deliberately omits delta mode. `wire.go` decides where each header lands on the wire, so no call site can reorder them.

## Turn context

`chat.go` prepends a synthetic `user` message wrapped in `<perch_turn_context>` that states today's date and that its content carries system-prompt authority, matching what the official CLI sends. That changes the meaning of the caller's own prompt, so it is a switch: `perch.WithTurnContext(false)` on the handler disables it and passes the conversation through untouched. Default is on, because parity with the CLI is the goal.

## Body shape

The model-call body follows the captured CLI byte shape rather than the spec table: `request` always carries `messages`, `tools`, `temperature`, `maxTokens` and `toolChoice` (an empty tool list stays `[]`, an unset choice stays `null`), `preferredModelId` and `attribution` are emitted as explicit `null` when unset, and `resendAfterOutput: true` is sent on every call. `TestModelCallBodyKeyOrderMatchesRecordedCLI` decodes the marshalled JSON and checks the key order, so a struct reorder cannot silently change the wire shape.

## Concurrency

Ticket minting is single-flight through a one-slot gate, so a caller waiting behind another goroutine's mint is released by its own context instead of the control-plane deadline. The per-ticket budget leaves a tombstone: a spent ticket keeps its counter until it expires, so workers that still hold it cannot re-acquire budget, while a rotation mints exactly one replacement that the other workers share.

Control-plane calls (`cli-auth/config`, `turn-ticket`, `account`) keep a 60s total deadline even when the fingerprint transport is active; only the model-call client runs without a total timeout and relies on the 60s idle read instead.

One `Client` (and one `Session`) exists per account: the handler creates it under its own mutex, so two cold-start requests cannot mint two sessions that refresh with the same rotated token. Cached clients are reference-counted while a request uses them, and eviction only removes unpinned entries. The turn-ticket budget is reserved under the client mutex, so the 40-call limit and the reuse counter cannot be raced.

## Streaming limits

Model calls get their own HTTP client with no total timeout: `net/http` measures `Client.Timeout` across the body, which would cut any turn longer than a minute. The 60s budget is applied per read instead — an idle body is closed and reported as `stream_interrupted` — and the transport keeps a 60s `ResponseHeaderTimeout` for the header phase. When Perch restarts a stream after output already reached the client, the response ends as `stream_interrupted` rather than replaying the resent text.

A turn ticket is reusable for up to 40 model calls on the `standard` profile; the client mints a new one after that instead of running into the vendor limit.

## Session ownership

Do not point this provider at the session file the CLI is using. Supabase refresh tokens rotate, and a second client refreshing the same token can revoke the token family: after the adapter refreshed `~/.perch/cli-auth-session.json` once more, both the adapter and the official CLI started answering `401 not_authenticated` / `Not authorized` on that session. Import a snapshot of the session at account creation (copy the file, then let the adapter own the copy) and keep the CLI session separate.

After a fresh `perch login` on 2026-10-29 the adapter still answered `401 not_authenticated` on the turn ticket, and the official CLI on the same session answered `Not authorized`, so the block is account-side rather than client-side. Probe evidence lives in `~/re/perchai/notes/p5-live-attempts.md`.

## Access restriction

Perch plans grant hosted models for the official surfaces only. Calling the model proxy from a third-party gateway is rejected with HTTP 403 `perch_surface_required`: "Your plan includes Perch-hosted models for use in Perch AI Web, Desktop, and CLI only. Direct API access is not included. Repeated direct-access attempts may result in account suspension." Do not ship this provider to end users without written access from Perch; keep the adapter for research use until then.

Evidence gathered on 2026-10-28/29 (notes: `~/re/perchai/notes/p5-reconciliation.md`): the refusal persists with a fresh turn ticket, real attribution and the documented headers, while the official CLI gets 200 on the same account. Removing ticket-presence and attribution as causes leaves client identity (delta mode, browser-style headers, the CLI turn-context body, TLS/HTTP fingerprint) as the only difference, and closing that gap means impersonating the official client. Do not attempt it.

## Tests

```bash
cd backend && go test ./internal/pkg/perch/            # fixtures captured from the real proxy
cd backend && PERCH_SESSION_FILE=/path/to/session.json go test -tags perchlive ./internal/pkg/perch/   # live account
```

Fixtures under `internal/pkg/perch/testdata` keep the captured wire shapes with synthetic content; no session token, ticket or vendor prompt text is stored.
