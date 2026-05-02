# Watchword

An [MCP](https://modelcontextprotocol.io/) server that lets AI assistants store and retrieve prompts, snippets, and arbitrary text keyed by short, human-readable code words. Think of it as a shared clipboard between you and your LLM — say "save this as *falcon*", then later "show me *falcon*".

Built in Go. Supports SQLite and PostgreSQL. Runs over stdio, SSE, or Streamable HTTP.

## Why

LLM conversations are ephemeral. Watchword gives your assistant persistent, named storage so it can save useful prompts, templates, code snippets, or any text under memorable keywords and recall them across sessions.

## Features

- **10 MCP tools** for storing, retrieving, searching, listing, restoring, deleting entries, and file upload/download
- **S3 file storage** — upload/download files up to 1GB via presigned URLs (works with AWS S3 and Cloudflare R2)
- **Collision resolution** — if a keyword is taken, the server auto-appends a suffix (`rabbit` -> `rabbit2`)
- **SQLite or PostgreSQL** backends
- **Automatic expiration** — entries expire after a configurable TTL (or never, with `ttl_hours: 0`)
- **Bearer token and JWT/JWKS authentication** — with optional named tokens for service accounts
- **Per-entry `created_by` tracking** — populated from a JWT identity claim or a named static token, surfaced on read/list/search responses
- **Health endpoints** for Kubernetes liveness/readiness probes
- **Customizable tool descriptions** — tune the prompts your LLM sees via `config.yaml`
- **Transports**: stdio, SSE, Streamable HTTP, or combined HTTP mode

## Quick start

### Build

```bash
go build -o watchword ./cmd/server
```

### Run with SQLite (stdio)

```bash
WORDSTORE_AUTH_TOKEN=secret ./watchword --config config.yaml
```

### Run with Docker + PostgreSQL

```bash
docker compose up -d
```

This starts PostgreSQL and Watchword with HTTP transport on port 8001:
- Streamable HTTP: `http://localhost:8001/mcp`
- SSE: `http://localhost:8001/sse`

### Run PostgreSQL only (for local stdio development)

```bash
docker compose -f docker-compose.postgres.yml up -d
```

Then run the binary locally against the database on port 5434.

## Connecting to an MCP client

### Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "watchword": {
      "command": "/path/to/watchword",
      "args": ["--config", "/path/to/config.yaml"],
      "env": {
        "WORDSTORE_AUTH_TOKEN": "your-secret-token"
      }
    }
  }
}
```

### Claude Code (stdio)

```bash
claude mcp add watchword /path/to/watchword -- --config /path/to/config.yaml
```

### Claude Code (Streamable HTTP via Docker)

```bash
claude mcp add-json watchword '{"type":"http","url":"http://localhost:8001/mcp"}'
```

Or add to `~/.mcp.json`:

```json
{
  "mcpServers": {
    "watchword": {
      "type": "http",
      "url": "http://localhost:8001/mcp"
    }
  }
}
```

## MCP tools

### Text entries

| Tool | Description |
|------|-------------|
| `store_entry` | Store a payload under a keyword. Auto-resolves collisions by appending a number suffix. |
| `get_entry` | Retrieve an entry by its UUID. Returns full payload. |
| `get_entry_by_word` | Retrieve an entry by its exact keyword. Returns full payload. |
| `search_entries` | Search entries with a SQL LIKE pattern (e.g. `%cat%`). Returns compact summaries (no payload). |
| `search_words` | Lightweight keyword search — returns only word, ID, status, and type. Ideal for browsing. |
| `list_entries` | List entries with filtering, sorting, and pagination. Returns compact summaries (no payload). |
| `restore_entry` | Restore an expired entry back to active status. |
| `delete_entry` | Permanently delete an entry by UUID **or keyword**. |

> **Token-saving design**: `list_entries`, `search_entries`, and `search_words` intentionally omit payload content to keep responses small. Use `get_entry` or `get_entry_by_word` to retrieve the full content of a specific entry.

### File entries (requires S3)

These tools are only available when S3 is configured. File data never passes through the MCP server — only presigned URLs are exchanged.

| Tool | Description |
|------|-------------|
| `upload_file` | Create a file entry and get a presigned PUT URL. Upload with `curl -X PUT -T file '<url>'`. |
| `download_file` | Get a presigned GET URL for a file entry. Download with `curl -o file '<url>'`. |

When a file entry is fetched via `get_entry` or `get_entry_by_word`, the response includes a hint to use `download_file` instead of returning raw file content.

## Configuration

All settings live in `config.yaml`. Every value can be overridden with environment variables prefixed `WORDSTORE_`.

### Server

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `server.transport` | `WORDSTORE_SERVER_TRANSPORT` | `stdio` | `stdio`, `sse`, `streamable-http`, or `http` |
| `server.sse_port` | `WORDSTORE_SERVER_SSE_PORT` | `8080` | Port for SSE-only transport |
| `server.http_port` | `WORDSTORE_SERVER_HTTP_PORT` | `8080` | Port for HTTP/Streamable HTTP transport |
| `server.health_port` | `WORDSTORE_SERVER_HEALTH_PORT` | `8081` | Health endpoint port (0 to disable) |

### Database

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `database.driver` | `WORDSTORE_DATABASE_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `database.sqlite.path` | `WORDSTORE_DATABASE_SQLITE_PATH` | `./data/word-store.db` | SQLite file path |
| `database.postgres.dsn` | `WORDSTORE_DATABASE_POSTGRES_DSN` | | PostgreSQL connection string |

#### SQLite concurrency

The SQLite backend is configured for safe concurrent reads and writes:

- **WAL journal mode** — multiple readers can run at the same time as one writer.
- **`busy_timeout=5000`** — writers wait up to 5 s on lock contention instead of failing immediately.
- **`BEGIN IMMEDIATE` for every transaction** — prevents busy-snapshot in read-then-write flows (collision resolution, file ops).
- **`synchronous=NORMAL` + `foreign_keys=1`** applied to every pooled connection.
- **Bounded connection pool** sized from CPU count.

SQLite still serializes writers globally — that is a SQLite invariant — but readers run in parallel and write contention is absorbed by the busy timeout. For most MCP workloads this is more than sufficient; reach for PostgreSQL only if you need cross-process writers or a centralized DB.

#### Switching from PostgreSQL to SQLite

Driver selection is a config flip; there is no automatic data migration between backends.

**1. Update config** — either edit `config.yaml`:

```yaml
database:
  driver: "sqlite"
  sqlite:
    path: "./data/word-store.db"
```

…or override via env var (takes precedence over `config.yaml`):

```bash
export WORDSTORE_DATABASE_DRIVER=sqlite
export WORDSTORE_DATABASE_SQLITE_PATH=./data/word-store.db
./watchword
```

The directory in `path` is created on startup, and migrations run on first boot.

**2. Docker** — the default `docker-compose.yml` launches PostgreSQL alongside watchword. To run on SQLite, stop that compose stack (`docker compose down`) and either run the binary directly or use a compose override that drops the `postgres` service, sets `WORDSTORE_DATABASE_DRIVER=sqlite` plus `WORDSTORE_DATABASE_SQLITE_PATH=/data/word-store.db`, and mounts a named volume at `/data` so the DB file survives container restarts.

**3. Migrating data (optional)** — switching driver starts from an empty database. If you need to carry entries across, dump the `entries` table from PostgreSQL (`COPY entries TO STDOUT (FORMAT csv, HEADER)`) and load it into SQLite with `.import`; the schemas are equivalent, but PostgreSQL `timestamptz` columns must be converted to RFC3339 strings for SQLite during the dump.

### Authentication

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.enabled` | `WORDSTORE_AUTH_ENABLED` | `true` | Enable/disable authentication |
| `auth.tokens` | `WORDSTORE_AUTH_TOKENS` | | Comma-separated bearer tokens (anonymous — `created_by` left null) |
| `auth.named_tokens` | | | List of `{name, token}` pairs. Requests using a named token record `created_by = name`. See [Tracking who created an entry](#tracking-who-created-an-entry). |
| | `WORDSTORE_AUTH_TOKEN` | | Token for stdio transport validation |

#### JWT / JWKS

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.jwt.jwks_url` | `WORDSTORE_AUTH_JWT_JWKS_URL` | | JWKS endpoint for public key discovery (required when `jwt` block is present) |
| `auth.jwt.issuer` | `WORDSTORE_AUTH_JWT_ISSUER` | | Expected `iss` claim (exact match — Auth0 emits a trailing slash, Keycloak does not) |
| `auth.jwt.audience` | `WORDSTORE_AUTH_JWT_AUDIENCE` | | Expected `aud` claim (recommended for OAuth — RFC 8707) |
| `auth.jwt.required_scopes` | `WORDSTORE_AUTH_JWT_REQUIRED_SCOPES` | | Comma-separated scopes that must all be present; checked against the `scope` claim (space-delimited string) and `scp` claim (array). If unset, signature + iss + aud is enough. |
| `auth.jwt.identity_claim` | `WORDSTORE_AUTH_JWT_IDENTITY_CLAIM` | `sub` | Claim used to populate `created_by` on stored entries. Common alternatives: `email`, `preferred_username`. |

#### Protected Resource Metadata (RFC 9728)

Required for MCP OAuth with the draft spec. Serves `/.well-known/oauth-protected-resource` so MCP clients (like Claude.ai) can discover your authorization server.

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.resource_metadata.resource` | `WORDSTORE_AUTH_RESOURCE` | | Canonical URI of this MCP server (e.g. `https://watchword.example.com`) |
| `auth.resource_metadata.authorization_servers` | `WORDSTORE_AUTH_AUTHORIZATION_SERVERS` | | Comma-separated authorization server issuer URIs |
| `auth.resource_metadata.bearer_methods_supported` | `WORDSTORE_AUTH_BEARER_METHODS` | `header` | Comma-separated bearer methods |
| `auth.resource_metadata.scopes_supported` | `WORDSTORE_AUTH_SCOPES_SUPPORTED` | | Comma-separated scopes (optional) |

When configured, 401 responses include a `WWW-Authenticate` header with the `resource_metadata` URL per the MCP spec.

#### Legacy Authorization Server Metadata

For backward compatibility with the 2025-03-26 MCP spec, Watchword can also serve `/.well-known/oauth-authorization-server`. This is only needed when Watchword itself acts as the authorization server. Requires both `jwt` and `oauth_metadata` blocks.

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.oauth_metadata.authorization_endpoint` | `WORDSTORE_AUTH_OAUTH_AUTHORIZATION_ENDPOINT` | | Authorization endpoint URL |
| `auth.oauth_metadata.token_endpoint` | `WORDSTORE_AUTH_OAUTH_TOKEN_ENDPOINT` | | Token endpoint URL |

### MCP OAuth with Claude.ai

To connect Claude.ai to Watchword using OAuth, you need:

1. **An authorization server** (e.g. Auth0, Keycloak, Cloudflare Workers) that issues JWT access tokens
2. **Watchword configured as a resource server** with RFC 9728 metadata

Example config for OAuth:

```yaml
auth:
  enabled: true
  resource_metadata:
    resource: "https://watchword.example.com"
    authorization_servers:
      - "https://auth.example.com"
  jwt:
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    issuer: "https://auth.example.com/"
    audience: "https://watchword.example.com"
```

The OAuth flow works as follows:

1. Claude sends an unauthenticated request to Watchword
2. Watchword returns `401` with `WWW-Authenticate: Bearer resource_metadata="https://watchword.example.com/.well-known/oauth-protected-resource"`
3. Claude fetches the protected resource metadata to discover the authorization server
4. Claude authenticates with the authorization server and obtains a JWT
5. Claude sends requests with `Authorization: Bearer <jwt>`
6. Watchword validates the JWT signature (via JWKS), issuer, and audience

Register these redirect URIs in your authorization server for Claude:
- `https://claude.ai/api/mcp/auth_callback`
- `https://claude.com/api/mcp/auth_callback`
- `http://localhost:6274/oauth/callback` (Claude Code)

#### Provider-specific notes

Watchword validates one IdP at a time. Pick Keycloak **or** Auth0 (or a Cloudflare Workers OAuth provider); ready-made configs live in [`examples/keycloak.yaml`](examples/keycloak.yaml) and [`examples/auth0.yaml`](examples/auth0.yaml).

**Keycloak**

- `jwks_url`: `{base}/realms/{realm}/protocol/openid-connect/certs`
- `issuer`: `{base}/realms/{realm}` (no trailing slash)
- `audience`: by default Keycloak puts `account` in `aud`. Treating that as a valid audience accepts any realm user — it does not bind tokens to this API. Create an *Audience* client scope that emits a custom value (e.g. `watchword`), assign it to your client, and set `audience` to that value.
- Scopes: define client scopes (e.g. `watchword:read`, `watchword:write`) and require them with `required_scopes`. Keycloak sends them in the `scope` claim.

**Auth0**

- `jwks_url`: `https://{tenant}.auth0.com/.well-known/jwks.json`
- `issuer`: `https://{tenant}.auth0.com/` (trailing slash — Auth0 emits it and `iss` is matched exactly)
- `audience`: the API Identifier from the Auth0 API (e.g. `https://watchword.example.com/api`)
- Scopes: define them on the Auth0 API and grant to the app/user; Auth0 sends them in the `scope` claim.

**Cloudflare**

Two distinct Cloudflare products show up here — they behave differently:

- **Cloudflare Workers OAuth Provider** (the pattern in [`docs/cloudflare-worker-oauth-proxy.md`](docs/cloudflare-worker-oauth-proxy.md)) — fits Watchword's bearer-token flow. The Worker issues its own JWTs; `aud` is whatever the Worker stamps. Set `audience` to that value, point `jwks_url`/`issuer` at the Worker's well-known endpoints, and treat it the same as Auth0/Keycloak.
- **Cloudflare Access** (zero-trust app gating) — does *not* fit cleanly. Access delivers its JWT in the `Cf-Access-Jwt-Assertion` header (and cookie), not `Authorization: Bearer`. `aud` is the per-application *Application AUD* tag (a hex string from the Access dashboard); JWKS is at `https://<team>.cloudflareaccess.com/cdn-cgi/access/certs` and issuer is `https://<team>.cloudflareaccess.com`. Best used as a front door layered in front of normal MCP auth, not as the MCP auth itself.

#### How `aud` is shaped per provider

`aud` is enforced via `jwt.WithAudience` (exact match), which accepts either a string or an array — only one entry has to match. Per-provider gotchas:

| Provider | Where `aud` comes from | Shape | What to put in `audience` |
|----------|-----------------------|-------|----------------------------|
| Keycloak | Audience protocol mapper on a client scope | string or array (often includes `"account"`) | The custom value emitted by your Audience mapper (e.g. `"watchword"`). Don't set this to `"account"` — that accepts every realm user. |
| Auth0    | The `audience` query param sent to `/authorize` | array, typically `[<API Identifier>, "https://{tenant}.auth0.com/userinfo"]` | The API Identifier (matches the first array entry). If clients omit `audience` at `/authorize`, Auth0 returns an opaque token (not a JWT) — those won't validate. |
| Cloudflare Worker OAuth | Whatever the Worker code stamps | depends on the Worker | The exact value the Worker uses. |
| Cloudflare Access | The Application AUD tag | string (hex) | The Application AUD from the Access dashboard. Note Access uses a non-`Authorization` header, so swapping it in requires middleware changes. |

#### Tracking who created an entry

Watchword records the creator's identity on each new entry in a nullable `created_by` column. The column is surfaced on `get_entry`, `get_entry_by_word`, `list_entries`, `search_entries`, `search_words`, `store_entry`, `restore_entry`, and `upload_file` responses. It stays `null` for anonymous calls.

Where `created_by` comes from:

- **JWT requests:** the value of `auth.jwt.identity_claim` (default `sub`). Set it to `email` or `preferred_username` if you want a human-readable label.
- **Named static tokens:** the `name` of the matching `auth.named_tokens` entry.
- **Plain `auth.tokens`:** anonymous (`created_by` is null) — these tokens have no associated identity.
- **Auth disabled:** anonymous.

```yaml
auth:
  enabled: true
  named_tokens:
    - name: ci-bot
      token: "ci-secret-xyz"
    - name: alice
      token: "alice-secret-xyz"
  jwt:
    jwks_url: "https://{tenant}.auth0.com/.well-known/jwks.json"
    issuer:   "https://{tenant}.auth0.com/"
    audience: "https://watchword.example.com/api"
    identity_claim: "email"
```

Restoring an expired entry preserves the original `created_by`; it is not overwritten by the restorer.

#### Required scopes

`auth.jwt.required_scopes` enforces fine-grained access on top of issuer/audience. Every listed scope must appear in either the `scope` claim (space-delimited string, used by Auth0 and Keycloak) or the `scp` claim (array). Without this, any valid token from the configured issuer/audience is accepted.

```yaml
auth:
  jwt:
    jwks_url: "https://{tenant}.auth0.com/.well-known/jwks.json"
    issuer:   "https://{tenant}.auth0.com/"
    audience: "https://watchword.example.com/api"
    required_scopes:
      - "watchword:read"
      - "watchword:write"
```

### S3 file storage (optional)

When configured, Watchword registers `upload_file` and `download_file` tools. Files are stored in S3 (or any S3-compatible service like Cloudflare R2) and transferred via presigned URLs — the MCP server never touches file data.

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `s3.enabled` | `WORDSTORE_S3_ENABLED` | *(unset)* | Set to `false` to force-disable S3 even if other `s3.*` / `WORDSTORE_S3_*` values are present. Useful for environments that may leak partial S3 env vars. |
| `s3.endpoint` | `WORDSTORE_S3_ENDPOINT` | *(empty = AWS)* | Custom endpoint URL (required for R2, MinIO) |
| `s3.region` | `WORDSTORE_S3_REGION` | | AWS region (e.g. `eu-central-1`) |
| `s3.bucket` | `WORDSTORE_S3_BUCKET` | | S3 bucket name |
| `s3.key_prefix` | `WORDSTORE_S3_KEY_PREFIX` | *(empty)* | Optional folder/prefix prepended to every new object key (e.g. `tenants/acme`). Existing entries keep their stored key. |
| `s3.presign_ttl_minutes` | `WORDSTORE_S3_PRESIGN_TTL_MINUTES` | `15` | How long presigned URLs remain valid |
| `s3.max_file_size_bytes` | `WORDSTORE_S3_MAX_FILE_SIZE_BYTES` | `1073741824` | Max file size (default 1GB) |
| | `WORDSTORE_S3_ACCESS_KEY_ID` | | S3 access key (env var only — never in config file) |
| | `WORDSTORE_S3_SECRET_ACCESS_KEY` | | S3 secret key (env var only — never in config file) |

Example config for Cloudflare R2:

```yaml
s3:
  endpoint: "https://<account-id>.r2.cloudflarestorage.com"
  region: "auto"
  bucket: "watchword-files"
  presign_ttl_minutes: 15
  max_file_size_bytes: 1073741824
```

```bash
export WORDSTORE_S3_ACCESS_KEY_ID="your-r2-access-key"
export WORDSTORE_S3_SECRET_ACCESS_KEY="your-r2-secret-key"
```

Example config for AWS S3:

```yaml
s3:
  region: "eu-central-1"
  bucket: "watchword-files"
```

**S3 object cleanup**: Expired file entries do not auto-delete S3 objects. Use [S3 lifecycle rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html) for garbage collection.

If `s3` is not configured, only the original text-based tools are registered — no S3 dependency. Partial S3 config (e.g. region set but no bucket, or missing credentials) does **not** fail startup — Watchword logs a warning and continues with the file tools disabled. To explicitly disable S3 in environments where partial `WORDSTORE_S3_*` env vars may leak in (e.g. shared k8s ConfigMaps), set `s3.enabled: false` (or `WORDSTORE_S3_ENABLED=false`) — the entire S3 block is then discarded after config load.

### Expiration

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `expiration.enabled` | `WORDSTORE_EXPIRATION_ENABLED` | `true` | Run background expiration worker |
| `expiration.interval_hours` | `WORDSTORE_EXPIRATION_INTERVAL_HOURS` | `24` | How often the worker checks for expired entries |
| `expiration.ttl_hours` | `WORDSTORE_EXPIRATION_TTL_HOURS` | `168` | Default TTL for new entries (7 days). `0` = never expires |

To disable expiration entirely, set `expiration.enabled: false` and `expiration.ttl_hours: 0`. Individual entries can override the default TTL by passing `ttl_hours` when storing (set to `0` for no expiration).

### Logging

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `logging.level` | `WORDSTORE_LOGGING_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `logging.format` | `WORDSTORE_LOGGING_FORMAT` | `json` | `json` or `text` |

## Health endpoints

Available on a separate port (default `8081`) for monitoring and Kubernetes probes.

| Endpoint | Purpose | Response |
|----------|---------|----------|
| `GET /healthz/live` | Liveness probe | `200` if the process is running |
| `GET /healthz/ready` | Readiness probe | `200` if the database is reachable, `503` otherwise |
| `GET /status` | Detailed status | `200` with version, uptime, DB status, memory, goroutine count |

## Deployment

### Docker

```bash
docker build -t watchword:latest .
docker compose up -d
```

### Kubernetes

Use the `http` transport for Kubernetes — it serves both Streamable HTTP (`/mcp`) and SSE (`/sse`) on one port.

```yaml
# ConfigMap
apiVersion: v1
kind: ConfigMap
metadata:
  name: watchword-config
data:
  config.yaml: |
    server:
      transport: "http"
      http_port: 8080
      health_port: 8081
    database:
      driver: "postgres"
    auth:
      enabled: true
    expiration:
      enabled: true
      interval_hours: 24
      ttl_hours: 168
    logging:
      level: "info"
      format: "json"
---
# Secret — auth settings via env vars
apiVersion: v1
kind: Secret
metadata:
  name: watchword-secret
type: Opaque
stringData:
  WORDSTORE_AUTH_TOKENS: "your-token-here"
  WORDSTORE_DATABASE_POSTGRES_DSN: "postgres://watchword:changeme@postgres:5432/watchword?sslmode=require"
  # MCP OAuth (RFC 9728) — uncomment to enable
  # WORDSTORE_AUTH_RESOURCE: "https://watchword.example.com"
  # WORDSTORE_AUTH_AUTHORIZATION_SERVERS: "https://auth.example.com"
  # WORDSTORE_AUTH_JWT_JWKS_URL: "https://auth.example.com/.well-known/jwks.json"
  # WORDSTORE_AUTH_JWT_ISSUER: "https://auth.example.com/"
  # WORDSTORE_AUTH_JWT_AUDIENCE: "https://watchword.example.com"
---
# Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: watchword
spec:
  replicas: 1
  selector:
    matchLabels:
      app: watchword
  template:
    metadata:
      labels:
        app: watchword
    spec:
      containers:
        - name: watchword
          image: your-registry/watchword:1.0.0
          args: ["--config", "/etc/watchword/config.yaml"]
          ports:
            - name: http
              containerPort: 8080
            - name: health
              containerPort: 8081
          envFrom:
            - secretRef:
                name: watchword-secret
          volumeMounts:
            - name: config
              mountPath: /etc/watchword
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz/live
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /healthz/ready
              port: health
            initialDelaySeconds: 3
            periodSeconds: 5
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
      volumes:
        - name: config
          configMap:
            name: watchword-config
---
# Service
apiVersion: v1
kind: Service
metadata:
  name: watchword
spec:
  selector:
    app: watchword
  ports:
    - name: http
      port: 8080
      targetPort: http
    - name: health
      port: 8081
      targetPort: health
```

Notes:
- **Replicas**: Safe to run multiple replicas against the same PostgreSQL — collision resolution uses database-level unique constraints.
- **Migrations**: Run automatically on startup (tracked via `schema_migrations` table).
- **Secrets**: Never put tokens in ConfigMap. Use Kubernetes Secrets or an external secret manager.

## Testing

```bash
go test ./...
```

## Architecture

```
cmd/server/main.go        Entry point, config loading, DI wiring, graceful shutdown
internal/domain/          Entry struct, validation, sentinel errors
internal/config/          YAML + env var config loading
internal/repository/      Repository interface + SQLite/PostgreSQL implementations
internal/service/         Business logic (collision resolution, store, restore, search, file ops)
internal/s3/              S3 presigned URL client (AWS SDK v2)
internal/auth/            Bearer token and JWT/JWKS validation
internal/mcp/             MCP server setup and tool handlers
internal/worker/          Background expiration goroutine
internal/health/          Health/status HTTP endpoints
migrations/               Embedded SQL migrations (SQLite + PostgreSQL)
```

## License

[MIT](LICENSE)
