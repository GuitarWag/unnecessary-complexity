package service

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/yld/url-shortener/services/analytics/internal/repo"
	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
)

func dialBuf(t *testing.T, srv *Server) analyticsv1.AnalyticsServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	analyticsv1.RegisterAnalyticsServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	t.Cleanup(gsrv.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return analyticsv1.NewAnalyticsServiceClient(conn)
}

func newTestServer(t *testing.T) (*Server, *repo.Repository) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))
	r := repo.New(db)
	return New(r), r
}

func TestStats_ReturnsAggregates(t *testing.T) {
	t.Parallel()
	srv, r := newTestServer(t)
	c := dialBuf(t, srv)

	day1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	for _, ts := range []time.Time{day1, day1.Add(time.Hour), day2} {
		require.NoError(t, r.InsertClick(context.Background(), repo.Click{Code: "abc", ClickedAt: ts}))
	}

	resp, err := c.Stats(context.Background(), &analyticsv1.StatsRequest{Code: "abc"})
	require.NoError(t, err)
	assert.Equal(t, "abc", resp.GetCode())
	assert.Equal(t, int64(3), resp.GetTotal())
	require.NotNil(t, resp.GetLastClickedAt())
	assert.True(t, resp.GetLastClickedAt().AsTime().Equal(day2))
	require.Len(t, resp.GetDaily(), 2)
	assert.Equal(t, "2026-05-01", resp.GetDaily()[0].GetDate())
	assert.Equal(t, int64(2), resp.GetDaily()[0].GetCount())
	assert.Equal(t, "2026-05-02", resp.GetDaily()[1].GetDate())
	assert.Equal(t, int64(1), resp.GetDaily()[1].GetCount())
}

func TestStats_HonorsDateFilter(t *testing.T) {
	t.Parallel()
	srv, r := newTestServer(t)
	c := dialBuf(t, srv)

	for _, ts := range []time.Time{
		time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
	} {
		require.NoError(t, r.InsertClick(context.Background(), repo.Click{Code: "abc", ClickedAt: ts}))
	}

	resp, err := c.Stats(context.Background(), &analyticsv1.StatsRequest{
		Code: "abc", FromDate: "2026-05-01", ToDate: "2026-05-02",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.GetTotal())
}

func TestStats_RejectsEmptyCode(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	c := dialBuf(t, srv)

	_, err := c.Stats(context.Background(), &analyticsv1.StatsRequest{Code: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStats_RejectsBadDateFormat(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	c := dialBuf(t, srv)
	for _, bad := range []string{"yesterday", "26-05-01", "2026/05/01"} {
		_, err := c.Stats(context.Background(), &analyticsv1.StatsRequest{Code: "abc", FromDate: bad})
		require.Errorf(t, err, "input: %q", bad)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "input: %q", bad)
	}
}

func TestStats_EmptyForUnknownCode(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	c := dialBuf(t, srv)
	resp, err := c.Stats(context.Background(), &analyticsv1.StatsRequest{Code: "missing"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.GetTotal())
	assert.Nil(t, resp.GetLastClickedAt())
	assert.Empty(t, resp.GetDaily())
}
