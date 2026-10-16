package repo

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	"github.com/yld/url-shortener/services/shortener/internal/pgtest"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := pgtest.New(t)
	require.NoError(t, Migrate(context.Background(), db))
	return db
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	require.NoError(t, Migrate(context.Background(), db))
	require.NoError(t, Migrate(context.Background(), db))
}

func TestInsert_AndGetByCode(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	r := New(db)
	ctx := context.Background()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	err := r.Insert(ctx, ShortURL{
		Code:      "abc123",
		LongURL:   "https://example.com/foo",
		CreatedAt: now,
	})
	require.NoError(t, err)

	got, err := r.GetByCode(ctx, "abc123")
	require.NoError(t, err)
	assert.Equal(t, "abc123", got.Code)
	assert.Equal(t, "https://example.com/foo", got.LongURL)
	assert.True(t, got.CreatedAt.Equal(now))
}

func TestGetByCode_NotFound(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	_, err := r.GetByCode(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestInsert_DuplicateCode(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	row := ShortURL{Code: "dup", LongURL: "https://a", CreatedAt: time.Now().UTC()}

	require.NoError(t, r.Insert(ctx, row))

	err := r.Insert(ctx, row)
	assert.ErrorIs(t, err, ErrDuplicateCode)
}

func TestInsert_AllowsSameLongURL_DifferentCodes(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	long := "https://example.com/duplicates-allowed"

	require.NoError(t, r.Insert(ctx, ShortURL{Code: "one", LongURL: long, CreatedAt: time.Now().UTC()}))
	require.NoError(t, r.Insert(ctx, ShortURL{Code: "two", LongURL: long, CreatedAt: time.Now().UTC()}))
}

func TestGetByIdempotencyKey_RoundTrip(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	row := ShortURL{
		Code:           "kx1",
		LongURL:        "https://example.com",
		OwnerID:        "alice",
		IdempotencyKey: "uuid-1",
		CreatedAt:      time.Now().UTC(),
	}
	require.NoError(t, r.Insert(ctx, row))

	got, err := r.GetByIdempotencyKey(ctx, "alice", "uuid-1")
	require.NoError(t, err)
	assert.Equal(t, "kx1", got.Code)
	assert.Equal(t, "alice", got.OwnerID)
	assert.Equal(t, "uuid-1", got.IdempotencyKey)
}

func TestGetByIdempotencyKey_EmptyKeyReturnsNotFound(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	_, err := r.GetByIdempotencyKey(context.Background(), "alice", "")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetByIdempotencyKey_ScopedByOwner(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "a", LongURL: "u", OwnerID: "alice", IdempotencyKey: "k", CreatedAt: time.Now().UTC(),
	}))

	_, err := r.GetByIdempotencyKey(ctx, "bob", "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestInsert_DuplicateIdempotencyKeyPerOwner(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "first", LongURL: "u", OwnerID: "alice", IdempotencyKey: "same-key",
		CreatedAt: time.Now().UTC(),
	}))

	err := r.Insert(ctx, ShortURL{
		Code: "second", LongURL: "u", OwnerID: "alice", IdempotencyKey: "same-key",
		CreatedAt: time.Now().UTC(),
	})
	assert.ErrorIs(t, err, ErrDuplicateIdempotencyKey)
}

func TestInsert_DifferentOwnersMayShareIdempotencyKey(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "x1", LongURL: "u", OwnerID: "alice", IdempotencyKey: "k", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "x2", LongURL: "u", OwnerID: "bob", IdempotencyKey: "k", CreatedAt: time.Now().UTC(),
	}))
}

func TestInsert_NullKeysDoNotCollide(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "n1", LongURL: "u", OwnerID: "alice", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, r.Insert(ctx, ShortURL{
		Code: "n2", LongURL: "u", OwnerID: "alice", CreatedAt: time.Now().UTC(),
	}))
}

func TestInsert_ContextCancelled(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Insert(ctx, ShortURL{Code: "c", LongURL: "u", CreatedAt: time.Now()})
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestInsertBatch_PerRowConflicts(t *testing.T) {
	t.Parallel()
	r := New(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()

	result, err := r.InsertBatch(ctx, []ShortURL{
		{Code: "ok", LongURL: "u", OwnerID: "alice", IdempotencyKey: "k", CreatedAt: now},
		{Code: "ok", LongURL: "u", CreatedAt: now},
		{Code: "other", LongURL: "u", OwnerID: "alice", IdempotencyKey: "k", CreatedAt: now},
	})
	require.NoError(t, err)
	require.Len(t, result.Errors, 3)
	assert.NoError(t, result.Errors[0])
	assert.ErrorIs(t, result.Errors[1], ErrDuplicateCode)
	assert.ErrorIs(t, result.Errors[2], ErrDuplicateIdempotencyKey)

	_, err = r.GetByCode(ctx, "ok")
	require.NoError(t, err, "the successful row must commit despite its neighbours failing")
}

var outboxInsert = regexp.MustCompile(
	`table public\.outbox: INSERT: .*aggregatetype\[text\]:'([^']*)' aggregateid\[text\]:'([^']*)' .*payload\[bytea\]:'\\x([0-9a-f]*)'`)

// Reads the WAL through test_decoding, which is what Debezium sees. The outbox
// table itself is always empty after commit.
func TestInsert_WritesOutboxEventToWAL(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	var slot string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&slot))
	_, err := db.ExecContext(ctx, `SELECT pg_create_logical_replication_slot($1, 'test_decoding')`, slot)
	require.NoError(t, err)

	r := New(db)
	require.NoError(t, r.Insert(ctx, ShortURL{Code: "wal1", LongURL: "https://example.com/wal", CreatedAt: time.Now().UTC()}))

	var outboxRows int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxRows))
	assert.Zero(t, outboxRows)

	rows, err := db.QueryContext(ctx, `SELECT data FROM pg_logical_slot_get_changes($1, NULL, NULL)`, slot)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var inserts [][]string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		if m := outboxInsert.FindStringSubmatch(line); m != nil {
			inserts = append(inserts, m[1:])
		}
	}
	require.NoError(t, rows.Err())
	require.Len(t, inserts, 1)

	assert.Equal(t, events.TopicShortURLCreated, inserts[0][0])
	assert.Equal(t, "wal1", inserts[0][1])
	payload, err := hex.DecodeString(inserts[0][2])
	require.NoError(t, err)
	var evt eventsv1.ShortURLCreated
	require.NoError(t, proto.Unmarshal(payload, &evt))
	assert.Equal(t, "wal1", evt.GetCode())
	assert.Equal(t, "https://example.com/wal", evt.GetLongUrl())
}

// init.sql creates the publication empty, before the slot. Migrate must then add
// the outbox table instead of skipping the existing publication.
func TestMigrate_AddsOutboxToPrecreatedPublication(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE PUBLICATION `+Publication+` WITH (publish = 'insert')`)
	require.NoError(t, err)

	require.NoError(t, Migrate(ctx, db))
	require.NoError(t, Migrate(ctx, db))

	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication_tables WHERE pubname = $1 AND tablename = 'outbox'`, Publication,
	).Scan(&n))
	assert.Equal(t, 1, n)
}
