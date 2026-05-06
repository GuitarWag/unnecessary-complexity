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
	"google.golang.org/protobuf/proto"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	"github.com/yld/url-shortener/services/resolver/internal/repo"
)

func dialBuf(t *testing.T, srv *Server) resolverv1.ResolverServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	resolverv1.RegisterResolverServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	t.Cleanup(gsrv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return resolverv1.NewResolverServiceClient(conn)
}

func newTestServer(t *testing.T, fixed time.Time) (*Server, *repo.Repository, *events.FakePublisher) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))

	r := repo.New(db)
	pub := events.NewFakePublisher()

	srv := New(r, pub, WithNow(func() time.Time { return fixed }))
	return srv, r, pub
}

func TestResolve_ReturnsLongURLAndPublishesClick(t *testing.T) {
	t.Parallel()

	clickedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	srv, r, pub := newTestServer(t, clickedAt)
	require.NoError(t, r.Upsert(context.Background(), repo.ShortURL{
		Code: "abc", LongURL: "https://example.com/x", CreatedAt: clickedAt.Add(-time.Hour),
	}))

	c := dialBuf(t, srv)
	resp, err := c.Resolve(context.Background(), &resolverv1.ResolveRequest{
		Code: "abc", UserAgent: "ua", Referer: "ref", Ip: "1.2.3.4",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/x", resp.GetLongUrl())

	require.Eventually(t, func() bool {
		return len(pub.Messages(events.TopicClickRecorded)) == 1
	}, time.Second, 10*time.Millisecond, "click event must be published")

	msgs := pub.Messages(events.TopicClickRecorded)
	require.Len(t, msgs, 1)
	assert.Equal(t, "abc", msgs[0].Key)

	var click eventsv1.ClickRecorded
	require.NoError(t, proto.Unmarshal(msgs[0].Value, &click))
	assert.Equal(t, "abc", click.GetCode())
	assert.Equal(t, "ua", click.GetUserAgent())
	assert.Equal(t, "ref", click.GetReferer())
	assert.Equal(t, "1.2.3.4", click.GetIp())
	assert.True(t, click.GetClickedAt().AsTime().Equal(clickedAt))
}

func TestResolve_NotFound(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t, time.Now().UTC())
	c := dialBuf(t, srv)

	_, err := c.Resolve(context.Background(), &resolverv1.ResolveRequest{Code: "nope"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestResolve_RejectsEmptyCode(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t, time.Now().UTC())
	c := dialBuf(t, srv)

	_, err := c.Resolve(context.Background(), &resolverv1.ResolveRequest{Code: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestResolve_DoesNotPublishOnNotFound(t *testing.T) {
	t.Parallel()

	srv, _, pub := newTestServer(t, time.Now().UTC())
	c := dialBuf(t, srv)

	_, _ = c.Resolve(context.Background(), &resolverv1.ResolveRequest{Code: "nope"})
	// give async publish a moment in case of regression
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, pub.Messages(events.TopicClickRecorded))
}

func TestResolve_PublishFailureDoesNotFailRPC(t *testing.T) {
	t.Parallel()

	srv, r, pub := newTestServer(t, time.Now().UTC())
	require.NoError(t, r.Upsert(context.Background(), repo.ShortURL{
		Code: "abc", LongURL: "https://x", CreatedAt: time.Now().UTC(),
	}))
	pub.SetError(assert.AnError)

	c := dialBuf(t, srv)
	resp, err := c.Resolve(context.Background(), &resolverv1.ResolveRequest{Code: "abc"})
	require.NoError(t, err)
	assert.Equal(t, "https://x", resp.GetLongUrl())
}
