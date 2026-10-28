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

## Access restriction

Perch plans grant hosted models for the official surfaces only. Calling the model proxy from a third-party gateway is rejected with HTTP 403 `perch_surface_required`: "Your plan includes Perch-hosted models for use in Perch AI Web, Desktop, and CLI only. Direct API access is not included. Repeated direct-access attempts may result in account suspension." Do not ship this provider to end users without written access from Perch; keep the adapter for research use until then.

## Tests

```bash
cd backend && go test ./internal/pkg/perch/            # fixtures captured from the real proxy
cd backend && PERCH_SESSION_FILE=/path/to/session.json go test -tags perchlive ./internal/pkg/perch/   # live account
```

Fixtures under `internal/pkg/perch/testdata` keep the captured wire shapes with synthetic content; no session token, ticket or vendor prompt text is stored.
