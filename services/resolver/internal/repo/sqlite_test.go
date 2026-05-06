package repo

import (
	"context"
	"database/sql"
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

func TestUpsert_InsertsAndReplaces(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	ctx := context.Background()

	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, r.Upsert(ctx, ShortURL{Code: "abc", LongURL: "https://a", CreatedAt: first}))

	got, err := r.Lookup(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, "https://a", got.LongURL)
	assert.True(t, got.CreatedAt.Equal(first))

	second := first.Add(time.Hour)
	require.NoError(t, r.Upsert(ctx, ShortURL{Code: "abc", LongURL: "https://b", CreatedAt: second}))

	got, err = r.Lookup(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, "https://b", got.LongURL)
	assert.True(t, got.CreatedAt.Equal(second))
}

func TestLookup_NotFound(t *testing.T) {
	t.Parallel()

	r := New(newTestDB(t))
	_, err := r.Lookup(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}
