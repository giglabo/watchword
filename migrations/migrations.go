package migrations

import "embed"

//go:embed sqlite/*.sql
var SQLiteFS embed.FS

//go:embed postgres/*.sql
var PostgresFS embed.FS
