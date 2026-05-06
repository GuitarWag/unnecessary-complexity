// Package repo provides analytics-svc click persistence and aggregation.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Click is a single resolved-URL event.
type Click struct {
	Code      string
	ClickedAt time.Time
	UserAgent string
	Referer   string
	IP        string
}

// DailyCount is the daily aggregation row.
type DailyCount struct {
	Date  string // YYYY-MM-DD UTC
	Count int64
}

// Stats is the aggregated result returned to the gRPC service.
type Stats struct {
	Total         int64
	LastClickedAt time.Time
	Daily         []DailyCount
}

// Repository writes and queries the clicks fact table.
type Repository struct {
	db *sql.DB
}

// New returns a Repository.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

const schema = `
CREATE TABLE IF NOT EXISTS clicks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	code        TEXT NOT NULL,
	clicked_at  TIMESTAMP NOT NULL,
	user_agent  TEXT NOT NULL DEFAULT '',
	referer     TEXT NOT NULL DEFAULT '',
	ip          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_clicks_code_clicked_at ON clicks(code, clicked_at);
`

// Migrate applies the schema. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("repo: migrate: %w", err)
	}
	return nil
}

// InsertClick records a new click event.
func (r *Repository) InsertClick(ctx context.Context, c Click) error {
	if c.Code == "" {
		return errors.New("repo: code required")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO clicks(code, clicked_at, user_agent, referer, ip) VALUES(?, ?, ?, ?, ?)`,
		c.Code, c.ClickedAt.UTC(), c.UserAgent, c.Referer, c.IP,
	)
	if err != nil {
		return fmt.Errorf("repo: insert click: %w", err)
	}
	return nil
}

// Stats aggregates clicks for code within [from, to] (inclusive). Zero values mean "no bound".
func (r *Repository) Stats(ctx context.Context, code string, from, to time.Time) (Stats, error) {
	if code == "" {
		return Stats{}, errors.New("repo: code required")
	}

	args := []any{code}
	where := "code = ?"
	if !from.IsZero() {
		where += " AND clicked_at >= ?"
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		// inclusive of to-day: extend to 23:59:59.999...
		end := to.UTC().Add(24*time.Hour - time.Nanosecond)
		where += " AND clicked_at <= ?"
		args = append(args, end)
	}

	// #nosec G202 -- where is built from fixed string literals, not user input.
	totalQ := "SELECT COUNT(*), COALESCE(MAX(clicked_at), '') FROM clicks WHERE " + where
	var total int64
	var lastStr string
	if err := r.db.QueryRowContext(ctx, totalQ, args...).Scan(&total, &lastStr); err != nil {
		return Stats{}, fmt.Errorf("repo: stats total: %w", err)
	}

	out := Stats{Total: total}
	if lastStr != "" {
		// SQLite returns timestamps as RFC3339 with sub-second precision; try multiple layouts.
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, lastStr); err == nil {
				out.LastClickedAt = t.UTC()
				break
			}
		}
	}
	if total == 0 {
		return out, nil
	}

	// #nosec G202 -- where is built from fixed string literals, not user input.
	dailyQ := "SELECT strftime('%Y-%m-%d', clicked_at) AS d, COUNT(*) FROM clicks WHERE " + where + " GROUP BY d ORDER BY d"
	rows, err := r.db.QueryContext(ctx, dailyQ, args...)
	if err != nil {
		return Stats{}, fmt.Errorf("repo: stats daily: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var dc DailyCount
		if err := rows.Scan(&dc.Date, &dc.Count); err != nil {
			return Stats{}, fmt.Errorf("repo: stats daily scan: %w", err)
		}
		out.Daily = append(out.Daily, dc)
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("repo: stats daily rows: %w", err)
	}
	return out, nil
}
