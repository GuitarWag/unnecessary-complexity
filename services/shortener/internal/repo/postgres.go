// Package repo provides Postgres persistence for the shortener service.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

// ErrNotFound indicates the requested record does not exist.
var ErrNotFound = errors.New("repo: not found")

// ErrDuplicateCode indicates a primary-key collision on the short code.
var ErrDuplicateCode = errors.New("repo: duplicate code")

// ErrDuplicateIdempotencyKey indicates the (owner_id, idempotency_key) pair already exists.
var ErrDuplicateIdempotencyKey = errors.New("repo: duplicate idempotency key")

// Publication is the logical replication publication Debezium reads the outbox from.
const Publication = "shortener_outbox"

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

// The outbox column names are the Debezium EventRouter defaults. The publication
// only carries INSERTs, so the DELETE that empties the outbox in the same
// transaction never reaches Debezium.
const schema = `
CREATE TABLE IF NOT EXISTS short_urls (
	code             TEXT PRIMARY KEY,
	long_url         TEXT NOT NULL,
	owner_id         TEXT NOT NULL DEFAULT '',
	idempotency_key  TEXT,
	created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_short_urls_long_url ON short_urls(long_url);
CREATE UNIQUE INDEX IF NOT EXISTS idx_short_urls_owner_idem
	ON short_urls(owner_id, idempotency_key)
	WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS outbox (
	id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	aggregatetype  TEXT NOT NULL,
	aggregateid    TEXT NOT NULL,
	type           TEXT NOT NULL,
	payload        BYTEA NOT NULL
);

-- k8s/base/postgres/init.sql creates the publication empty, before the
-- replication slot, so it already exists in every change the slot decodes.
-- Here it only gains the outbox table.
DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = '` + Publication + `') THEN
		CREATE PUBLICATION ` + Publication + ` FOR TABLE outbox WITH (publish = 'insert');
	ELSIF NOT EXISTS (SELECT 1 FROM pg_publication_tables
	                  WHERE pubname = '` + Publication + `' AND tablename = 'outbox') THEN
		ALTER PUBLICATION ` + Publication + ` ADD TABLE outbox;
	END IF;
END $$;
`

// Migrate applies the schema. Safe to call repeatedly.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("repo: migrate: %w", err)
	}
	return nil
}

// aggregatetype is the routing field for the EventRouter, which uses it as the
// Kafka topic name.
const insertWithEvent = `
WITH url AS (
	INSERT INTO short_urls(code, long_url, owner_id, idempotency_key, created_at)
	VALUES($1, $2, $3, $4, $5)
	ON CONFLICT DO NOTHING
	RETURNING code
)
INSERT INTO outbox(aggregatetype, aggregateid, type, payload)
SELECT $6, code, 'ShortURLCreated', $7 FROM url
RETURNING id`

// Insert persists a new mapping and its ShortURLCreated outbox event. Returns
// ErrDuplicateCode on PK collision and ErrDuplicateIdempotencyKey when the
// (owner_id, idempotency_key) pair already exists.
func (r *Repository) Insert(ctx context.Context, row ShortURL) error {
	result, err := r.InsertBatch(ctx, []ShortURL{row})
	if err != nil {
		return err
	}
	return result.Errors[0]
}

// BatchResult carries the per-row outcome of InsertBatch. Errors[i] is nil on
// success, or one of ErrDuplicateCode / ErrDuplicateIdempotencyKey if that
// specific row failed inside the shared transaction.
type BatchResult struct {
	Errors []error
}

// InsertBatch runs all inserts and their outbox events inside one transaction,
// so one commit covers many writes. Conflicts use ON CONFLICT DO NOTHING
// rather than a savepoint per row: a failed statement aborts a Postgres
// transaction, and more than 64 savepoints overflow the subtransaction cache.
// The returned error is non-nil only on a transaction-level failure, in which
// case Errors is unset.
func (r *Repository) InsertBatch(ctx context.Context, rows []ShortURL) (BatchResult, error) {
	if len(rows) == 0 {
		return BatchResult{}, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchResult{}, fmt.Errorf("repo: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	errs := make([]error, len(rows))
	ids := make([]string, 0, len(rows))
	for i, row := range rows {
		id, err := insertOne(ctx, tx, row)
		if err != nil {
			return BatchResult{}, err
		}
		if id == "" {
			taken, err := codeTaken(ctx, tx, row.Code)
			if err != nil {
				return BatchResult{}, err
			}
			errs[i] = ErrDuplicateIdempotencyKey
			if taken {
				errs[i] = ErrDuplicateCode
			}
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return BatchResult{Errors: errs}, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE id = ANY($1)`, ids); err != nil {
		return BatchResult{}, fmt.Errorf("repo: clear outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return BatchResult{}, fmt.Errorf("repo: commit: %w", err)
	}
	return BatchResult{Errors: errs}, nil
}

// insertOne returns the outbox id, or "" when the row hit a unique constraint.
func insertOne(ctx context.Context, tx *sql.Tx, row ShortURL) (string, error) {
	payload, err := proto.Marshal(&eventsv1.ShortURLCreated{
		Code:      row.Code,
		LongUrl:   row.LongURL,
		CreatedAt: timestamppb.New(row.CreatedAt),
	})
	if err != nil {
		return "", fmt.Errorf("repo: marshal event: %w", err)
	}
	var key any
	if row.IdempotencyKey != "" {
		key = row.IdempotencyKey
	}
	var id string
	err = tx.QueryRowContext(ctx, insertWithEvent,
		row.Code, row.LongURL, row.OwnerID, key, row.CreatedAt.UTC(),
		events.TopicShortURLCreated, payload,
	).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("repo: insert: %w", err)
	}
	return id, nil
}

// codeTaken tells which unique constraint a skipped insert hit. Under READ
// COMMITTED the conflicting row is visible to the next statement.
func codeTaken(ctx context.Context, tx *sql.Tx, code string) (bool, error) {
	var taken bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM short_urls WHERE code = $1)`, code,
	).Scan(&taken); err != nil {
		return false, fmt.Errorf("repo: conflict lookup: %w", err)
	}
	return taken, nil
}

// GetByCode looks up a mapping by its short code.
func (r *Repository) GetByCode(ctx context.Context, code string) (ShortURL, error) {
	row := r.db.QueryRowContext(ctx, selectColumns+` WHERE code = $1`, code)
	return scanShortURL(row)
}

// GetByIdempotencyKey returns the mapping previously created for (ownerID, key), if any.
// An empty key always returns ErrNotFound: empty keys do not participate in dedup.
func (r *Repository) GetByIdempotencyKey(ctx context.Context, ownerID, key string) (ShortURL, error) {
	if key == "" {
		return ShortURL{}, ErrNotFound
	}
	row := r.db.QueryRowContext(ctx,
		selectColumns+` WHERE owner_id = $1 AND idempotency_key = $2 LIMIT 1`,
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
