package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
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

func TestInsert_DifferentOwnersMaySharaeIdempotencyKey(t *testing.T) {
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
