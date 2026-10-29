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

`perch.Handler` serves `POST /v1/chat/completions` and `GET /v1/models` from a `CredentialSource`. Mount it behind the platform router once the platform is registered; `Client.Health` is the account health check (`/api/perchai/account` -> plan, entitlements, allowed models).

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

`identity.go` resolves the version once per process, so the header cannot change between requests. All outbound Perch calls go through one header applier; the turn ticket request deliberately omits delta mode.

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
