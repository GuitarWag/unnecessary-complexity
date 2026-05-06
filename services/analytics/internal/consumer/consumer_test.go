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

	"github.com/yld/url-shortener/services/analytics/internal/repo"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

func newRepo(t *testing.T) *repo.Repository {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))
	return repo.New(db)
}

func TestHandleClickRecorded_InsertsRow(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	h := NewClickRecordedHandler(r)

	when := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	require.NoError(t, h(context.Background(), "abc", &eventsv1.ClickRecorded{
		Code: "abc", ClickedAt: timestamppb.New(when), UserAgent: "ua", Referer: "ref", Ip: "1.2.3.4",
	}))

	stats, err := r.Stats(context.Background(), "abc", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Total)
	assert.True(t, stats.LastClickedAt.Equal(when))
}

func TestHandleClickRecorded_UsesKeyWhenCodeMissing(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	h := NewClickRecordedHandler(r)

	require.NoError(t, h(context.Background(), "abc", &eventsv1.ClickRecorded{
		ClickedAt: timestamppb.New(time.Now().UTC()),
	}))

	stats, err := r.Stats(context.Background(), "abc", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Total)
}

func TestHandleClickRecorded_RejectsEmptyCodeAndKey(t *testing.T) {
	t.Parallel()
	h := NewClickRecordedHandler(newRepo(t))
	err := h(context.Background(), "", &eventsv1.ClickRecorded{ClickedAt: timestamppb.New(time.Now().UTC())})
	assert.Error(t, err)
}

func TestHandleClickRecorded_DefaultsClickedAtToNow(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	h := NewClickRecordedHandler(r)
	before := time.Now().UTC().Add(-time.Second)
	require.NoError(t, h(context.Background(), "abc", &eventsv1.ClickRecorded{Code: "abc"}))
	stats, err := r.Stats(context.Background(), "abc", time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Total)
	assert.True(t, stats.LastClickedAt.After(before))
}
