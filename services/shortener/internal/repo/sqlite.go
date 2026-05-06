// Package repo provides SQLite persistence for the shortener service.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mattn/go-sqlite3"
)

// ErrNotFound indicates the requested record does not exist.
var ErrNotFound = errors.New("repo: not found")

// ErrDuplicateCode indicates a primary-key collision on the short code.
var ErrDuplicateCode = errors.New("repo: duplicate code")

// ErrDuplicateIdempotencyKey indicates the (owner_id, idempotency_key) pair already exists.
var ErrDuplicateIdempotencyKey = errors.New("repo: duplicate idempotency key")

// ShortURL is a persisted short→long URL mapping.
type ShortURL struct {
	Code           string
	LongURL        string
	OwnerID        string
	IdempotencyKey string
	CreatedAt      time.Time
}

// Repository persists ShortURL records.
type Repository struct {
	db *sql.DB
}

// New returns a Repository backed by db. Caller owns db lifecycle.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

const schema = `
CREATE TABLE IF NOT EXISTS short_urls (
	code             TEXT PRIMARY KEY,
	long_url         TEXT NOT NULL,
	owner_id         TEXT NOT NULL DEFAULT '',
	idempotency_key  TEXT,
	created_at       TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_short_urls_long_url ON short_urls(long_url);
CREATE UNIQUE INDEX IF NOT EXISTS idx_short_urls_owner_idem
	ON short_urls(owner_id, idempotency_key)
	WHERE idempotency_key IS NOT NULL;
`

// Migrate applies the schema and brings legacy databases forward. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("repo: migrate: %w", err)
	}
	if err := ensureColumn(ctx, db, "short_urls", "owner_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "short_urls", "idempotency_key", "TEXT"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_short_urls_owner_idem
		ON short_urls(owner_id, idempotency_key) WHERE idempotency_key IS NOT NULL`); err != nil {
		return fmt.Errorf("repo: migrate index: %w", err)
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, col, decl string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("repo: pragma %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("repo: pragma scan: %w", err)
		}
		if name == col {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("repo: pragma rows: %w", err)
	}

	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, decl) //nolint:gosec // table/col are package constants
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("repo: add column %s: %w", col, err)
	}
	return nil
}

// Insert persists a new mapping. Returns ErrDuplicateCode on PK collision and
// ErrDuplicateIdempotencyKey when the (owner_id, idempotency_key) pair already exists.
func (r *Repository) Insert(ctx context.Context, row ShortURL) error {
	var key any
	if row.IdempotencyKey != "" {
		key = row.IdempotencyKey
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO short_urls(code, long_url, owner_id, idempotency_key, created_at) VALUES(?, ?, ?, ?, ?)`,
		row.Code, row.LongURL, row.OwnerID, key, row.CreatedAt.UTC(),
	)
	if err != nil {
		var sqlErr sqlite3.Error
		if errors.As(err, &sqlErr) && sqlErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
			return ErrDuplicateCode
		}
		if errors.As(err, &sqlErr) && sqlErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("repo: insert: %w", err)
	}
	return nil
}

// GetByCode looks up a mapping by its short code.
func (r *Repository) GetByCode(ctx context.Context, code string) (ShortURL, error) {
	row := r.db.QueryRowContext(ctx, selectColumns+` WHERE code = ?`, code)
	return scanShortURL(row)
}

// GetByIdempotencyKey returns the mapping previously created for (ownerID, key), if any.
// An empty key always returns ErrNotFound — empty keys do not participate in dedup.
func (r *Repository) GetByIdempotencyKey(ctx context.Context, ownerID, key string) (ShortURL, error) {
	if key == "" {
		return ShortURL{}, ErrNotFound
	}
	row := r.db.QueryRowContext(ctx,
		selectColumns+` WHERE owner_id = ? AND idempotency_key = ? LIMIT 1`,
		ownerID, key,
	)
	return scanShortURL(row)
}

const selectColumns = `SELECT code, long_url, owner_id, COALESCE(idempotency_key, ''), created_at FROM short_urls`

func scanShortURL(row *sql.Row) (ShortURL, error) {
	var s ShortURL
	err := row.Scan(&s.Code, &s.LongURL, &s.OwnerID, &s.IdempotencyKey, &s.CreatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ShortURL{}, ErrNotFound
	case err != nil:
		return ShortURL{}, fmt.Errorf("repo: scan: %w", err)
	}
	s.CreatedAt = s.CreatedAt.UTC()
	return s, nil
}
