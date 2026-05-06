// Package repo provides resolver-svc's local SQLite cache, hydrated from Kafka.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates the requested code is not in the local cache.
var ErrNotFound = errors.New("repo: not found")

// ShortURL is the locally-cached projection of a short→long mapping.
type ShortURL struct {
	Code      string
	LongURL   string
	CreatedAt time.Time
}

// Repository provides read/upsert access to short_urls.
type Repository struct {
	db *sql.DB
}

// New returns a Repository.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

const schema = `
CREATE TABLE IF NOT EXISTS short_urls (
	code        TEXT PRIMARY KEY,
	long_url    TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL
);
`

// Migrate applies the schema. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("repo: migrate: %w", err)
	}
	return nil
}

// Upsert inserts or replaces a code mapping. Used by the Kafka consumer.
func (r *Repository) Upsert(ctx context.Context, s ShortURL) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO short_urls(code, long_url, created_at) VALUES(?, ?, ?)
		 ON CONFLICT(code) DO UPDATE SET long_url=excluded.long_url, created_at=excluded.created_at`,
		s.Code, s.LongURL, s.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("repo: upsert: %w", err)
	}
	return nil
}

// Lookup returns the long URL for a code.
func (r *Repository) Lookup(ctx context.Context, code string) (ShortURL, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT code, long_url, created_at FROM short_urls WHERE code = ?`,
		code,
	)
	var s ShortURL
	switch err := row.Scan(&s.Code, &s.LongURL, &s.CreatedAt); {
	case errors.Is(err, sql.ErrNoRows):
		return ShortURL{}, ErrNotFound
	case err != nil:
		return ShortURL{}, fmt.Errorf("repo: scan: %w", err)
	}
	s.CreatedAt = s.CreatedAt.UTC()
	return s, nil
}
