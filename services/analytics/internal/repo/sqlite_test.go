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

func TestInsertClick_PersistsRow(t *testing.T) {
	t.Parallel()
	r := New(newTestDB(t))
	ctx := context.Background()

	when := time.Date(2026, 5, 6, 12, 30, 0, 0, time.UTC)
	require.NoError(t, r.InsertClick(ctx, Click{
		Code: "abc", ClickedAt: when, UserAgent: "ua", Referer: "ref", IP: "1.2.3.4",
	}))

	stats, err := r.Stats(ctx, "abc", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Total)
	require.False(t, stats.LastClickedAt.IsZero())
	assert.True(t, stats.LastClickedAt.Equal(when))
}

func TestStats_AggregatesByDay(t *testing.T) {
	t.Parallel()
	r := New(newTestDB(t))
	ctx := context.Background()

	day1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	for _, ts := range []time.Time{day1, day1.Add(time.Hour), day1.Add(2 * time.Hour), day2, day2.Add(30 * time.Minute)} {
		require.NoError(t, r.InsertClick(ctx, Click{Code: "abc", ClickedAt: ts}))
	}
	require.NoError(t, r.InsertClick(ctx, Click{Code: "other", ClickedAt: day1}))

	stats, err := r.Stats(ctx, "abc", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(5), stats.Total)
	require.Len(t, stats.Daily, 2)
	assert.Equal(t, "2026-05-01", stats.Daily[0].Date)
	assert.Equal(t, int64(3), stats.Daily[0].Count)
	assert.Equal(t, "2026-05-02", stats.Daily[1].Date)
	assert.Equal(t, int64(2), stats.Daily[1].Count)
	assert.True(t, stats.LastClickedAt.Equal(day2.Add(30*time.Minute)))
}

func TestStats_FiltersByDateRange(t *testing.T) {
	t.Parallel()
	r := New(newTestDB(t))
	ctx := context.Background()

	require.NoError(t, r.InsertClick(ctx, Click{Code: "abc", ClickedAt: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, r.InsertClick(ctx, Click{Code: "abc", ClickedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, r.InsertClick(ctx, Click{Code: "abc", ClickedAt: time.Date(2026, 5, 2, 23, 59, 59, 0, time.UTC)}))
	require.NoError(t, r.InsertClick(ctx, Click{Code: "abc", ClickedAt: time.Date(2026, 5, 3, 0, 0, 1, 0, time.UTC)}))

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	stats, err := r.Stats(ctx, "abc", from, to)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Total, "expects clicks on 5/1 and 5/2 (inclusive)")
}

func TestStats_EmptyForUnknownCode(t *testing.T) {
	t.Parallel()
	r := New(newTestDB(t))
	stats, err := r.Stats(context.Background(), "missing", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.Total)
	assert.True(t, stats.LastClickedAt.IsZero())
	assert.Empty(t, stats.Daily)
}
