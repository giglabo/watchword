# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Watchword — an MCP server in Go that stores prompts/data keyed by human-readable words (code words). Supports SQLite and PostgreSQL backends, background expiration, bearer token auth, S3 presigned file upload/download, and 10 MCP tools over stdio/SSE/Streamable HTTP.

## Build & Test

```bash
go build ./...          # Build all packages
go test ./...           # Run all tests
go build -o watchword ./cmd/server   # Build binary
docker compose up -d    # Run with PostgreSQL + Streamable HTTP (via supergateway)
```

## Architecture

- `cmd/server/main.go` — Entry point, config loading, DI wiring, graceful shutdown
- `internal/domain/` — Entry struct, validation, sentinel errors
- `internal/config/` — YAML + env var config loading
- `internal/repository/` — Repository interface + SQLite/PostgreSQL implementations
- `internal/service/` — Business logic (collision resolution, store, restore, search, file ops)
- `internal/s3/` — S3 presigned URL client (aws-sdk-go-v2), Presigner interface
- `internal/auth/` — Bearer token + JWT/JWKS validation, RFC 9728 Protected Resource Metadata, WWW-Authenticate headers
- `internal/mcp/` — MCP server setup + tool handlers (text + file)
- `internal/worker/` — Background expiration goroutine
- `migrations/` — Embedded SQL migrations for SQLite and PostgreSQL

## Key Design Decisions

- **WithTx on Repository interface** — enables transactional collision resolution from service layer
- **SQLite timestamps as RFC3339 strings** — consistent, parseable, sortable
- **Domain errors → tool result errors** (not transport errors) — MCP client gets structured messages
- **Collision resolution** — base word, then word2..word999, all within a transaction
- **Auth per-transport** — stdio validates WORDSTORE_AUTH_TOKEN once at startup; SSE uses HTTP middleware
- **S3 presigned URLs** — file tools generate presigned PUT/GET URLs; MCP server never touches file data. Conditionally registered only when S3 is configured
- **Compact list/search responses** — `list_entries`, `search_entries`, `search_words` omit payload to reduce token usage; `get_entry`/`get_entry_by_word` return full content
- **delete_entry accepts UUID or word** — tries UUID parse first, falls back to word lookup
- **entry_type column** — `text` (default) or `file`; file entries store JSON metadata in payload field
- **Streamable HTTP via supergateway** — Docker image wraps the stdio binary with [supergateway](https://github.com/supercorp-ai/supergateway) to expose Streamable HTTP on `/mcp`. Uses `--stateful` mode to avoid the child-process OOM leak ([PR #111](https://github.com/supercorp-ai/supergateway/pull/111)). Built from the PR #111 branch for the stateless leak fix as well.

## Commit Guidelines

- Never add Claude as a co-author in commit messages (no `Co-Authored-By: Claude` lines).

## MCP Library

Uses `github.com/mark3labs/mcp-go` for MCP protocol support (stdio + SSE transports).

## Docker

The Dockerfile is a multi-stage build:
1. Go builder — compiles the `watchword` binary
2. Supergateway builder — clones and builds supergateway from the PR #111 fork (OOM fix)
3. Final `node:22-alpine` image — runs supergateway wrapping the watchword binary

`docker-compose.yml` runs PostgreSQL + watchword with Streamable HTTP on port 8001.
`docker-compose.postgres.yml` runs PostgreSQL only (for local stdio development on port 5434).
