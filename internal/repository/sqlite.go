package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/migrations"
)

type SQLiteRepo struct {
	db *sql.DB
}

func NewSQLiteRepo(dbPath string) (*SQLiteRepo, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}
	return &SQLiteRepo{db: db}, nil
}

func (r *SQLiteRepo) Migrate(ctx context.Context) error {
	// Create migration tracking table
	_, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	migrationFiles := []string{"001_init.sql"}
	for _, name := range migrationFiles {
		var count int
		err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		data, err := migrations.SQLiteFS.ReadFile("sqlite/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		if _, err := r.db.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := r.db.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, name, now); err != nil {
			return fmt.Errorf("recording migration %s: %w", name, err)
		}
	}
	return nil
}

func (r *SQLiteRepo) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *SQLiteRepo) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepo) Store(ctx context.Context, entry *domain.Entry) (*domain.Entry, error) {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	entry.Status = domain.StatusActive

	var expiresAt *string
	if entry.ExpiresAt != nil {
		s := entry.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}

	_, err := r.execContext(ctx,
		`INSERT INTO entries (id, word, payload, status, created_at, updated_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(), entry.Word, entry.Payload, string(entry.Status),
		now.Format(time.RFC3339), now.Format(time.RFC3339), expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting entry: %w", err)
	}
	return entry, nil
}

func (r *SQLiteRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Entry, error) {
	row := r.queryRowContext(ctx,
		`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries WHERE id = ?`,
		id.String(),
	)
	return scanSQLiteEntry(row)
}

func (r *SQLiteRepo) GetByWord(ctx context.Context, word string, includeExpired bool) (*domain.Entry, error) {
	query := `SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries WHERE word = ?`
	if !includeExpired {
		query += ` AND status = 'active'`
	}
	query += ` ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END LIMIT 1`
	row := r.queryRowContext(ctx, query, word)
	return scanSQLiteEntry(row)
}

func (r *SQLiteRepo) SearchByLike(ctx context.Context, pattern string, status string, limit int, offset int) ([]*domain.Entry, int, error) {
	where := `WHERE word LIKE ?`
	args := []any{pattern}
	if status != "all" {
		where += ` AND status = ?`
		args = append(args, status)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := r.queryRowContext(ctx, `SELECT COUNT(*) FROM entries `+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries %s ORDER BY word ASC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)

	rows, err := r.queryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("searching entries: %w", err)
	}
	defer rows.Close()

	entries, err := scanSQLiteEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (r *SQLiteRepo) List(ctx context.Context, status string, limit int, offset int, sortBy string, sortOrder string) ([]*domain.Entry, int, error) {
	sortBy = ValidateSortBy(sortBy)
	sortOrder = ValidateSortOrder(sortOrder)

	where := ""
	var args []any
	if status != "all" {
		where = `WHERE status = ?`
		args = append(args, status)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	countQuery := `SELECT COUNT(*) FROM entries ` + where
	err := r.queryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries %s ORDER BY %s %s LIMIT ? OFFSET ?`,
		where, sortBy, sortOrder)
	args = append(args, limit, offset)

	rows, err := r.queryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing entries: %w", err)
	}
	defer rows.Close()

	entries, err := scanSQLiteEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (r *SQLiteRepo) UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, newWord string, expiresAt *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var expiresStr *string
	if expiresAt != nil {
		s := expiresAt.UTC().Format(time.RFC3339)
		expiresStr = &s
	}

	result, err := r.execContext(ctx,
		`UPDATE entries SET status = ?, word = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
		newStatus, newWord, now, expiresStr, id.String(),
	)
	if err != nil {
		return fmt.Errorf("updating entry status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SQLiteRepo) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.execContext(ctx, `DELETE FROM entries WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("deleting entry: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SQLiteRepo) MarkExpiredBatch(ctx context.Context, batchSize int) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.execContext(ctx,
		`UPDATE entries SET status = 'expired', updated_at = ?
		 WHERE id IN (
			SELECT id FROM entries
			WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= ?
			LIMIT ?
		 )`, now, now, batchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("marking expired: %w", err)
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (r *SQLiteRepo) WordExistsActive(ctx context.Context, word string) (bool, error) {
	var count int
	err := r.queryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE word = ? AND status = 'active'`, word).Scan(&count)
	return count > 0, err
}

func (r *SQLiteRepo) WithTx(ctx context.Context, fn func(Repository) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	txRepo := &sqliteTxRepo{tx: tx}
	if err := fn(txRepo); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DB accessor methods that work with either *sql.DB or *sql.Tx
func (r *SQLiteRepo) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return r.db.ExecContext(ctx, query, args...)
}

func (r *SQLiteRepo) queryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.db.QueryRowContext(ctx, query, args...)
}

func (r *SQLiteRepo) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.db.QueryContext(ctx, query, args...)
}

// sqliteTxRepo wraps a transaction
type sqliteTxRepo struct {
	tx *sql.Tx
}

func (r *sqliteTxRepo) Store(ctx context.Context, entry *domain.Entry) (*domain.Entry, error) {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	entry.Status = domain.StatusActive

	var expiresAt *string
	if entry.ExpiresAt != nil {
		s := entry.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}

	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO entries (id, word, payload, status, created_at, updated_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(), entry.Word, entry.Payload, string(entry.Status),
		now.Format(time.RFC3339), now.Format(time.RFC3339), expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting entry: %w", err)
	}
	return entry, nil
}

func (r *sqliteTxRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Entry, error) {
	row := r.tx.QueryRowContext(ctx,
		`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries WHERE id = ?`,
		id.String(),
	)
	return scanSQLiteEntry(row)
}

func (r *sqliteTxRepo) GetByWord(ctx context.Context, word string, includeExpired bool) (*domain.Entry, error) {
	query := `SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries WHERE word = ?`
	if !includeExpired {
		query += ` AND status = 'active'`
	}
	query += ` ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END LIMIT 1`
	row := r.tx.QueryRowContext(ctx, query, word)
	return scanSQLiteEntry(row)
}

func (r *sqliteTxRepo) SearchByLike(ctx context.Context, pattern string, status string, limit int, offset int) ([]*domain.Entry, int, error) {
	where := `WHERE word LIKE ?`
	args := []any{pattern}
	if status != "all" {
		where += ` AND status = ?`
		args = append(args, status)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := r.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries `+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries %s ORDER BY word ASC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanSQLiteEntries(rows)
	return entries, total, err
}

func (r *sqliteTxRepo) List(ctx context.Context, status string, limit int, offset int, sortBy string, sortOrder string) ([]*domain.Entry, int, error) {
	sortBy = ValidateSortBy(sortBy)
	sortOrder = ValidateSortOrder(sortOrder)

	where := ""
	var args []any
	if status != "all" {
		where = `WHERE status = ?`
		args = append(args, status)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := r.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries `+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`SELECT id, word, payload, status, created_at, updated_at, expires_at FROM entries %s ORDER BY %s %s LIMIT ? OFFSET ?`,
		where, sortBy, sortOrder)
	args = append(args, limit, offset)

	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanSQLiteEntries(rows)
	return entries, total, err
}

func (r *sqliteTxRepo) UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, newWord string, expiresAt *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var expiresStr *string
	if expiresAt != nil {
		s := expiresAt.UTC().Format(time.RFC3339)
		expiresStr = &s
	}

	result, err := r.tx.ExecContext(ctx,
		`UPDATE entries SET status = ?, word = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
		newStatus, newWord, now, expiresStr, id.String(),
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *sqliteTxRepo) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.tx.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *sqliteTxRepo) MarkExpiredBatch(ctx context.Context, batchSize int) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.tx.ExecContext(ctx,
		`UPDATE entries SET status = 'expired', updated_at = ?
		 WHERE id IN (
			SELECT id FROM entries
			WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= ?
			LIMIT ?
		 )`, now, now, batchSize,
	)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (r *sqliteTxRepo) WordExistsActive(ctx context.Context, word string) (bool, error) {
	var count int
	err := r.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE word = ? AND status = 'active'`, word).Scan(&count)
	return count > 0, err
}

func (r *sqliteTxRepo) WithTx(_ context.Context, _ func(Repository) error) error {
	return fmt.Errorf("nested transactions not supported")
}

func (r *sqliteTxRepo) Ping(_ context.Context) error {
	return fmt.Errorf("cannot ping within transaction")
}

func (r *sqliteTxRepo) Migrate(_ context.Context) error {
	return fmt.Errorf("cannot migrate within transaction")
}

func (r *sqliteTxRepo) Close() error {
	return fmt.Errorf("cannot close within transaction")
}

// Scanning helpers

type scannable interface {
	Scan(dest ...any) error
}

func scanSQLiteEntry(row scannable) (*domain.Entry, error) {
	var e domain.Entry
	var idStr, status, createdStr, updatedStr string
	var expiresStr *string

	err := row.Scan(&idStr, &e.Word, &e.Payload, &status, &createdStr, &updatedStr, &expiresStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scanning entry: %w", err)
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing UUID: %w", err)
	}
	e.ID = id
	e.Status = domain.EntryStatus(status)

	if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
		e.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		e.UpdatedAt = t
	}
	if expiresStr != nil {
		if t, err := time.Parse(time.RFC3339, *expiresStr); err == nil {
			e.ExpiresAt = &t
		}
	}

	return &e, nil
}

func scanSQLiteEntries(rows *sql.Rows) ([]*domain.Entry, error) {
	var entries []*domain.Entry
	for rows.Next() {
		var e domain.Entry
		var idStr, status, createdStr, updatedStr string
		var expiresStr *string

		if err := rows.Scan(&idStr, &e.Word, &e.Payload, &status, &createdStr, &updatedStr, &expiresStr); err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parsing UUID: %w", err)
		}
		e.ID = id
		e.Status = domain.EntryStatus(status)

		if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			e.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedStr); err == nil {
			e.UpdatedAt = t
		}
		if expiresStr != nil {
			if t, err := time.Parse(time.RFC3339, *expiresStr); err == nil {
				e.ExpiresAt = &t
			}
		}

		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
