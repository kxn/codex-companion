# Design for Codex Companion Proxy

## Overview
Codex Companion is a Go application that lets a single user route all Codex/OpenAI API calls through a local proxy. The proxy owns a pool of accounts and swaps the client key for an upstream credential so the user can consume quota from multiple accounts without switching manually.

Two account types are supported:

* **API key accounts** – traditional Codex/OpenAI keys. Each account can
  optionally specify its own upstream `BaseURL`; if omitted the proxy uses the
  default OpenAI host `https://api.openai.com` and forwards client paths like
  `/v1/responses` as-is.
* **ChatGPT-login accounts** – accounts authenticated via ChatGPT's OAuth flow that yield both an access token and a refresh token. When importing an account the proxy stores the existing access token and continues using it until 28 days after the last refresh, only exchanging the refresh token at that point (see the OAuth flow in <https://github.com/openai/codex> for reference). These requests are sent to `https://chatgpt.com/backend-api/codex` with the leading `/v1` stripped from the client path. The upstream repository defines the OAuth client ID `app_EMoamEEZ73f0CkXaXp7hrann` and uses scopes `openid profile email offline_access` for the initial login; refresh requests reuse the same client ID with scope `openid profile email`.

A single HTTP server binds to `127.0.0.1:8080`. Requests not starting with `/admin` are proxied to the upstream Codex service. The Web UI and management API live under `/admin` on the same port. Because the server only listens on localhost, the Web UI does not implement authentication.

## Project Layout
```
cmd/
  companion/
    main.go          # program entry; parses env, starts HTTP server
internal/
  account/
    manager.go       # CRUD operations on accounts stored in SQLite
  auth/
    oauth.go         # exchange & refresh ChatGPT OAuth tokens
  scheduler/
    scheduler.go     # selects which account to use
  proxy/
    handler.go       # reverse proxy logic
  webui/
    handler.go       # serves /admin pages and REST API
    static/          # embedded HTML templates and JS
  log/
    store.go         # persistence for request logs
```

## Implementation Steps
1. Run `go mod init codex-companion`.
2. Implement `internal/account` and `internal/auth`:
  - `Account` struct stores type, API key or OAuth tokens, priority, exhaustion status, and reset time.
  - Account table includes columns for `type`, `api_key`, `refresh_token`, `access_token`, and `token_expires_at` (used as the "last refresh" time plus 28 days).
  - CRUD functions: `List`, `AddAPIKey`, `AddChatGPT`, `Update`, `Delete`, `MarkExhausted`, `Reactivate`.
  - ChatGPT accounts require a refresh token obtained via the `codex login` CLI from the upstream repository or manual OAuth steps; the proxy does **not** implement the interactive login flow.
  - `auth.ExchangeRefreshToken(rt string)` posts to `https://auth.openai.com/oauth/token` with `client_id=app_EMoamEEZ73f0CkXaXp7hrann`, `grant_type=refresh_token`, `scope=openid profile email`, and returns `{access_token, refresh_token, expires_in}`.
  - `auth.Refresh(a *Account)` only runs when the stored token is older than 28 days. On success it stores the new access token, optional rotated refresh token, and sets `token_expires_at` to 28 days in the future.
3. Implement `internal/log` for request log table with `Insert` and `List` functions.
4. Implement `internal/scheduler`:
   - maintain slice of active accounts ordered by `Priority`.
   - method `Next(ctx context.Context) (*Account, error)` returns first non-exhausted account, refreshing ChatGPT tokens via `auth.Refresh` before returning.
   - background goroutine checks `ResetAt` and reactivates accounts.
5. Implement `internal/proxy`:
   - `ServeHTTP(w http.ResponseWriter, r *http.Request)` chooses account via scheduler.
  - forward only Codex API calls. Based on the upstream CLI implementation,
    valid paths are `/v1/responses`, `/v1/chat/completions`, and `/v1/models`.
    Any other path should return `404` without hitting the upstream service.
  - for API key accounts replace `Authorization` header with `Bearer <account.APIKey>`
    and allow an optional account‑specific `BaseURL` to override the default
    upstream when forwarding requests.
  - for ChatGPT accounts use `Bearer <account.AccessToken>`, forward to
    `https://chatgpt.com/backend-api/codex`, and strip the leading `/v1` from
    the request path.
  - normalize requests based on auth mode:
    - ChatGPT accounts add a `chatgpt-account-id` header, set the JSON field
      `store` to `false`, and add `include: ["reasoning.encrypted_content"]`
      so encrypted reasoning is returned instead of stored server-side.
    - API key accounts omit `chatgpt-account-id`, set `store` to `true`, and do
      not send an `include` field, allowing the server to store reasoning items
      referenced by ID.
  - forward the request using `http.Transport`.
  - log request and response through the log package.
6. Implement `internal/webui`:
   - `AdminHandler` registers routes on `/admin`.
   - static file server for `GET /admin` showing forms to add/remove accounts and view logs.
   - REST API under `/admin/api` called by simple JavaScript:
        - `GET /admin/api/accounts`
        - `POST /admin/api/accounts` (supports `type="api_key"` or `type="chatgpt"` with `refresh_token`, optional `access_token`, and optional `last_refresh`)
        - `POST /admin/api/accounts/import` to read `$CODEX_HOME/auth.json` (default `~/.codex/auth.json`) and create a ChatGPT account from its refresh and access tokens, using `last_refresh` to delay future refreshes
        - `PUT /admin/api/accounts/{id}`
        - `DELETE /admin/api/accounts/{id}`
        - `GET /admin/api/logs`
7. In `cmd/companion/main.go`:
   - open SQLite database file `companion.db`.
   - construct account manager, auth helper, log store, scheduler.
   - create `http.ServeMux` with `/admin` mapped to `webui.AdminHandler` and fallback to `proxy.Handler`.
   - start server with `http.ListenAndServe("127.0.0.1:8080", mux)`.

## Components
1. **Account Manager**
   - Stores account type, display name, priority, OAuth or API key credentials,
     optional API key `BaseURL`, exhaustion status, and reset time.
   - Persists data using SQLite via the pure Go driver `modernc.org/sqlite`.
   - Provides CRUD operations for both API key and ChatGPT-login accounts.

 2. **Auth (OAuth Token Refresher)**
    - Exchanges ChatGPT refresh tokens for access tokens using the shared client ID `app_EMoamEEZ73f0CkXaXp7hrann`.
    - Initial login (outside the proxy) must request scopes `openid profile email offline_access`; refresh requests use scope `openid profile email`.
    - Stores both `access_token` and `refresh_token` and records the time of the last refresh. Tokens are refreshed only every 28 days, updating the stored `refresh_token` if the server rotates it.

3. **Scheduler**
   - Keeps ordered list of active accounts by priority (lower number = higher priority).
   - Calls `auth.Refresh` for ChatGPT-login accounts before use.
   - On quota exhaustion (HTTP 429 or specific error codes), marks the account as unavailable and records the next reset time.
   - Background task periodically reactivates accounts whose reset time has passed.

4. **Proxy Handler**
   - Accepts Codex requests, selects an account from the scheduler, rewrites the `Authorization` header, and streams the request to the upstream Codex endpoint.
   - Uses `APIKey` for API key accounts or `AccessToken` for ChatGPT-login accounts.
   - Adjusts headers and body based on the authentication mode:
     - ChatGPT accounts include a `chatgpt-account-id` header, set `store` to `false`, and request `include: ["reasoning.encrypted_content"]`, which yields an encrypted reasoning payload in the response.
     - API key accounts omit that header, send `store` as `true`, and skip the `include` field so reasoning content is stored server-side and referenced by ID.
   - Streams the response back to the client.
   - On failures, retries with the next available account when possible.

5. **Request Logger**
   - Records timestamp, account used, request method/URL, headers, bodies, status, and error message.
   - Saves entries in the database and supports simple queries for the Web UI.

6. **Web UI & Management API**
   - Served at `/admin` on the same port as the proxy.
   - Uses static HTML with basic JavaScript `fetch` calls; no front-end framework.
   - Provides forms to manage accounts, import `auth.json`, and view recent logs.
   - REST endpoints under `/admin/api` implement JSON input/output.

7. **Codex API Reference**
   - The proxy mirrors Codex's REST endpoints and request formats but does not vendor any of the upstream repository's code.

### Codex `auth.json` format

The Web UI import expects the file generated by the official client at `$CODEX_HOME/auth.json`:

```
{
  "OPENAI_API_KEY": "optional",
  "tokens": {
    "id_token": "JWT with email/plan claims",
    "access_token": "short-lived access token",
    "refresh_token": "long-lived refresh token",
    "account_id": "optional"
  },
  "last_refresh": "RFC3339 timestamp"
}
```

## Data Structures
```go
// AccountType distinguishes how credentials are handled.
type AccountType int

const (
    APIKeyAccount AccountType = iota
    ChatGPTAccount
)

// Account represents an upstream Codex account.
type Account struct {
    ID             int64
    Name           string
    Type           AccountType
    APIKey         string    // for APIKeyAccount
    RefreshToken   string    // for ChatGPTAccount
    AccessToken    string    // cached access token
    TokenExpiresAt time.Time // last refresh time plus 28 days
    Priority       int       // smaller value = higher priority
    Exhausted      bool
    ResetAt        time.Time // next time quota is expected to reset
}

// RequestLog records a proxied request.
type RequestLog struct {
    ID         int64
    Time       time.Time
    AccountID  int64
    Method     string
    URL        string
    ReqHeader  http.Header
    ReqBody    string
    ReqSize    int
    RespHeader http.Header
    RespBody   string
    RespSize   int
    Status     int
    DurationMs int64
    Error      string
}
```

## Flow of a Proxied Request
1. Client sends an HTTP request to the local proxy with a simple API key for identification.
2. Proxy authenticates the client if needed (simple static key) and retrieves the next usable account from the scheduler.
3. For ChatGPT-login accounts the scheduler ensures a fresh `AccessToken`, refreshing via `auth.Refresh` only when the stored token is more than 28 days old.
4. Request headers and body are logged.
5. Proxy sets `Authorization: Bearer <credential>` where `<credential>` is the account's API key or access token and forwards the request to Codex.
6. Response is logged and streamed back to the client.
7. Scheduler updates the account status based on the response (marking exhausted accounts).

## Concurrency & Error Handling
- Use mutexes around shared account state.
- Handle network errors and upstream timeouts gracefully, retrying with the next account when appropriate.
- Background tasks should use `context.Context` for cancellation.

## Security & Deployment Considerations
- Server binds only to `127.0.0.1` and is intended for local use.
- Web UI has no authentication; do not expose the port to untrusted networks.
- API keys, refresh tokens, and access tokens are stored without encryption in the SQLite database.
- Logged bodies may contain sensitive data; provide options to redact or disable body logging.
- Refresh tokens grant long‑term access; ensure filesystem permissions restrict the database file to the local user.
