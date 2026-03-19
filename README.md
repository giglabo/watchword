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
- **Bearer token and JWT/JWKS authentication**
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

### Authentication

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.enabled` | `WORDSTORE_AUTH_ENABLED` | `true` | Enable/disable authentication |
| `auth.tokens` | `WORDSTORE_AUTH_TOKENS` | | Comma-separated bearer tokens |
| | `WORDSTORE_AUTH_TOKEN` | | Token for stdio transport validation |

#### JWT / JWKS

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `auth.jwt.jwks_url` | `WORDSTORE_AUTH_JWT_JWKS_URL` | | JWKS endpoint for public key discovery (required when `jwt` block is present) |
| `auth.jwt.issuer` | `WORDSTORE_AUTH_JWT_ISSUER` | | Expected `iss` claim |
| `auth.jwt.audience` | `WORDSTORE_AUTH_JWT_AUDIENCE` | | Expected `aud` claim (recommended for OAuth — RFC 8707) |

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

### S3 file storage (optional)

When configured, Watchword registers `upload_file` and `download_file` tools. Files are stored in S3 (or any S3-compatible service like Cloudflare R2) and transferred via presigned URLs — the MCP server never touches file data.

| Setting | Env var | Default | Description |
|---------|---------|---------|-------------|
| `s3.endpoint` | `WORDSTORE_S3_ENDPOINT` | *(empty = AWS)* | Custom endpoint URL (required for R2, MinIO) |
| `s3.region` | `WORDSTORE_S3_REGION` | | AWS region (e.g. `eu-central-1`) |
| `s3.bucket` | `WORDSTORE_S3_BUCKET` | | S3 bucket name |
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

If `s3` is not configured, only the original text-based tools are registered — no S3 dependency.

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
