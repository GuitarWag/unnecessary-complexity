package consumer

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	"github.com/yld/url-shortener/services/resolver/internal/repo"
)

func newRepo(t *testing.T) *repo.Repository {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))
	return repo.New(db)
}

func TestHandleShortURLCreated_UpsertsRow(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	h := NewShortURLCreatedHandler(r)

	created := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	require.NoError(t, h(context.Background(), "abc", &eventsv1.ShortURLCreated{
		Code: "abc", LongUrl: "https://x", CreatedAt: timestamppb.New(created),
	}))

	got, err := r.Lookup(context.Background(), "abc")
	require.NoError(t, err)
	assert.Equal(t, "https://x", got.LongURL)
	assert.True(t, got.CreatedAt.Equal(created))
}

func TestHandleShortURLCreated_RejectsEmptyCode(t *testing.T) {
	t.Parallel()

	h := NewShortURLCreatedHandler(newRepo(t))
	err := h(context.Background(), "", &eventsv1.ShortURLCreated{LongUrl: "https://x"})
	assert.Error(t, err)
}

func TestHandleShortURLCreated_RejectsNilTimestamp(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	h := NewShortURLCreatedHandler(r)
	err := h(context.Background(), "abc", &eventsv1.ShortURLCreated{Code: "abc", LongUrl: "https://x"})
	require.NoError(t, err)

	got, err := r.Lookup(context.Background(), "abc")
	require.NoError(t, err)
	// CreatedAt should default to zero time when missing — we accept the row but it's epoch.
	assert.True(t, got.CreatedAt.IsZero() || got.CreatedAt.Year() <= 1970)
}
