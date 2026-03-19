package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/migrations"
)

type pgQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresRepo struct {
	pool *pgxpool.Pool
	q    pgQuerier // either pool or tx
}

func NewPostgresRepo(ctx context.Context, dsn string) (*PostgresRepo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &PostgresRepo{pool: pool, q: pool}, nil
}

func (r *PostgresRepo) Migrate(ctx context.Context) error {
	// Create migration tracking table
	_, err := r.q.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	migrationFiles := []string{"001_init.sql", "002_add_entry_type.sql"}
	for _, name := range migrationFiles {
		var count int
		err := r.q.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, name).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		data, err := migrations.PostgresFS.ReadFile("postgres/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		if _, err := r.q.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		if _, err := r.q.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("recording migration %s: %w", name, err)
		}
	}
	return nil
}

func (r *PostgresRepo) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepo) Close() error {
	if r.pool != nil {
		r.pool.Close()
	}
	return nil
}

func (r *PostgresRepo) Store(ctx context.Context, entry *domain.Entry) (*domain.Entry, error) {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	entry.Status = domain.StatusActive
	if entry.EntryType == "" {
		entry.EntryType = domain.EntryTypeText
	}

	_, err := r.q.Exec(ctx,
		`INSERT INTO entries (id, word, payload, status, entry_type, created_at, updated_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID, entry.Word, entry.Payload, string(entry.Status), string(entry.EntryType),
		entry.CreatedAt, entry.UpdatedAt, entry.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting entry: %w", err)
	}
	return entry, nil
}

func (r *PostgresRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Entry, error) {
	row := r.q.QueryRow(ctx,
		`SELECT id, word, payload, status, entry_type, created_at, updated_at, expires_at FROM entries WHERE id = $1`, id,
	)
	return scanPgEntry(row)
}

func (r *PostgresRepo) GetByWord(ctx context.Context, word string, includeExpired bool) (*domain.Entry, error) {
	query := `SELECT id, word, payload, status, entry_type, created_at, updated_at, expires_at FROM entries WHERE word = $1`
	if !includeExpired {
		query += ` AND status = 'active'`
	}
	query += ` ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END LIMIT 1`
	row := r.q.QueryRow(ctx, query, word)
	return scanPgEntry(row)
}

func (r *PostgresRepo) SearchByLike(ctx context.Context, pattern string, status string, limit int, offset int) ([]*domain.Entry, int, error) {
	where := `WHERE word LIKE $1`
	args := []any{pattern}
	idx := 2
	if status != "all" {
		where += fmt.Sprintf(` AND status = $%d`, idx)
		args = append(args, status)
		idx++
	}

	var total int
	err := r.q.QueryRow(ctx, `SELECT COUNT(*) FROM entries `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, entry_type, created_at, updated_at, expires_at FROM entries %s ORDER BY word ASC LIMIT $%d OFFSET $%d`,
		where, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("searching entries: %w", err)
	}
	defer rows.Close()

	entries, err := scanPgEntries(rows)
	return entries, total, err
}

func (r *PostgresRepo) List(ctx context.Context, status string, limit int, offset int, sortBy string, sortOrder string) ([]*domain.Entry, int, error) {
	sortBy = ValidateSortBy(sortBy)
	sortOrder = ValidateSortOrder(sortOrder)

	where := ""
	var args []any
	idx := 1
	if status != "all" {
		where = fmt.Sprintf(`WHERE status = $%d`, idx)
		args = append(args, status)
		idx++
	}

	var total int
	err := r.q.QueryRow(ctx, `SELECT COUNT(*) FROM entries `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, entry_type, created_at, updated_at, expires_at FROM entries %s ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		where, sortBy, sortOrder, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing entries: %w", err)
	}
	defer rows.Close()

	entries, err := scanPgEntries(rows)
	return entries, total, err
}

func (r *PostgresRepo) UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, newWord string, expiresAt *time.Time) error {
	now := time.Now().UTC()
	ct, err := r.q.Exec(ctx,
		`UPDATE entries SET status = $1, word = $2, updated_at = $3, expires_at = $4 WHERE id = $5`,
		newStatus, newWord, now, expiresAt, id,
	)
	if err != nil {
		return fmt.Errorf("updating entry status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresRepo) Delete(ctx context.Context, id uuid.UUID) error {
	ct, err := r.q.Exec(ctx, `DELETE FROM entries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting entry: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresRepo) MarkExpiredBatch(ctx context.Context, batchSize int) (int, error) {
	now := time.Now().UTC()
	ct, err := r.q.Exec(ctx,
		`UPDATE entries SET status = 'expired', updated_at = $1
		 WHERE id IN (
			SELECT id FROM entries
			WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= $2
			LIMIT $3
		 )`, now, now, batchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("marking expired: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

func (r *PostgresRepo) WordExistsActive(ctx context.Context, word string) (bool, error) {
	var count int
	err := r.q.QueryRow(ctx, `SELECT COUNT(*) FROM entries WHERE word = $1 AND status = 'active'`, word).Scan(&count)
	return count > 0, err
}

func (r *PostgresRepo) WithTx(ctx context.Context, fn func(Repository) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txRepo := &PostgresRepo{pool: r.pool, q: tx}
	if err := fn(txRepo); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Scanning helpers

func scanPgEntry(row pgx.Row) (*domain.Entry, error) {
	var e domain.Entry
	var status, entryType string
	err := row.Scan(&e.ID, &e.Word, &e.Payload, &status, &entryType, &e.CreatedAt, &e.UpdatedAt, &e.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scanning entry: %w", err)
	}
	e.Status = domain.EntryStatus(status)
	e.EntryType = domain.EntryType(entryType)
	return &e, nil
}

func scanPgEntries(rows pgx.Rows) ([]*domain.Entry, error) {
	var entries []*domain.Entry
	for rows.Next() {
		var e domain.Entry
		var status, entryType string
		if err := rows.Scan(&e.ID, &e.Word, &e.Payload, &status, &entryType, &e.CreatedAt, &e.UpdatedAt, &e.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		e.Status = domain.EntryStatus(status)
		e.EntryType = domain.EntryType(entryType)
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
