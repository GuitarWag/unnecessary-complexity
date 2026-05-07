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
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
	"github.com/yld/url-shortener/services/shortener/internal/codegen"
	"github.com/yld/url-shortener/services/shortener/internal/repo"
)

func dialBuf(t *testing.T, srv *Server) shortenerv1.ShortenerServiceClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	gsrv := grpc.NewServer()
	shortenerv1.RegisterShortenerServiceServer(gsrv, srv)
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
	return shortenerv1.NewShortenerServiceClient(conn)
}

func newTestServer(t *testing.T) (*Server, *events.FakePublisher) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))

	gen, err := codegen.NewGenerator(8, nil)
	require.NoError(t, err)

	pub := events.NewFakePublisher()
	return New(repo.New(db), gen, pub, 5), pub
}

func newTestClient(t *testing.T) (shortenerv1.ShortenerServiceClient, *Server, *events.FakePublisher) {
	t.Helper()
	srv, pub := newTestServer(t)
	return dialBuf(t, srv), srv, pub
}

func TestShorten_CreatesNew(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	resp, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/foo",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetCreated())
	assert.Len(t, resp.GetUrl().GetCode(), 8)
	assert.Equal(t, "https://example.com/foo", resp.GetUrl().GetLongUrl())
	assert.NotNil(t, resp.GetUrl().GetCreatedAt())
}

func TestShorten_NoKeyAlwaysCreatesFreshRow(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	long := "https://example.com/no-dedup"

	first, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: long})
	require.NoError(t, err)

	second, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: long})
	require.NoError(t, err)

	assert.NotEqual(t, first.GetUrl().GetCode(), second.GetUrl().GetCode(),
		"without an idempotency key, repeated shortens must produce distinct codes")
	assert.True(t, first.GetCreated())
	assert.True(t, second.GetCreated())
}

func TestShorten_SameKeyReturnsSameCode(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	req := &shortenerv1.ShortenRequest{
		LongUrl:        "https://example.com/dedup",
		OwnerId:        "alice",
		IdempotencyKey: "uuid-1",
	}
	first, err := c.Shorten(context.Background(), req)
	require.NoError(t, err)
	second, err := c.Shorten(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, first.GetUrl().GetCode(), second.GetUrl().GetCode())
	assert.True(t, first.GetCreated())
	assert.False(t, second.GetCreated())
}

func TestShorten_DifferentKeysProduceDifferentCodes(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	first, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/k", OwnerId: "alice", IdempotencyKey: "k1",
	})
	require.NoError(t, err)
	second, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/k", OwnerId: "alice", IdempotencyKey: "k2",
	})
	require.NoError(t, err)

	assert.NotEqual(t, first.GetUrl().GetCode(), second.GetUrl().GetCode())
}

func TestShorten_KeyIsScopedByOwner(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	alice, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/scoped", OwnerId: "alice", IdempotencyKey: "shared",
	})
	require.NoError(t, err)
	bob, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/scoped", OwnerId: "bob", IdempotencyKey: "shared",
	})
	require.NoError(t, err)

	assert.NotEqual(t, alice.GetUrl().GetCode(), bob.GetUrl().GetCode(),
		"the same key from different owners must not collide")
}

func TestShorten_RejectsEmptyURL(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestShorten_RejectsNonHTTPURL(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	for _, bad := range []string{"javascript:alert(1)", "ftp://x", "not a url", "//example.com"} {
		_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: bad})
		require.Errorf(t, err, "input: %q", bad)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "input: %q", bad)
	}
}

func TestGet_ReturnsExisting(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	created, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/get",
	})
	require.NoError(t, err)

	got, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: created.GetUrl().GetCode()})
	require.NoError(t, err)
	assert.Equal(t, created.GetUrl().GetCode(), got.GetUrl().GetCode())
	assert.Equal(t, "https://example.com/get", got.GetUrl().GetLongUrl())
}

func TestGet_NotFound(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	_, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: "missing"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGet_RejectsEmptyCode(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	_, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestShorten_RetriesOnCollision(t *testing.T) {
	t.Parallel()

	// Force the generator to emit the same code twice in a row, then a fresh one.
	scripted := &scriptedGen{codes: []string{"AAAAAAAA", "AAAAAAAA", "BBBBBBBB"}}
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))

	srv := New(repo.New(db), scripted, events.NewFakePublisher(), 5)
	c := dialBuf(t, srv)

	first, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/1"})
	require.NoError(t, err)
	assert.Equal(t, "AAAAAAAA", first.GetUrl().GetCode())

	second, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/2"})
	require.NoError(t, err)
	assert.Equal(t, "BBBBBBBB", second.GetUrl().GetCode())
}

func TestShorten_GivesUpAfterMaxRetries(t *testing.T) {
	t.Parallel()

	scripted := &scriptedGen{codes: []string{"X", "X", "X"}, repeat: true}
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))

	srv := New(repo.New(db), scripted, events.NewFakePublisher(), 3)
	c := dialBuf(t, srv)

	_, err = c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/first"})
	require.NoError(t, err)

	_, err = c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/second"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestServer_RoundTripPreservesCreatedAt(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestClient(t)
	before := time.Now().Add(-1 * time.Second).UTC()

	created, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/ts",
	})
	require.NoError(t, err)
	got, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: created.GetUrl().GetCode()})
	require.NoError(t, err)

	ts := got.GetUrl().GetCreatedAt().AsTime()
	assert.True(t, ts.After(before) || ts.Equal(before),
		"created_at %v should be after %v", ts, before)
}

func TestShorten_PublishesShortURLCreated(t *testing.T) {
	t.Parallel()

	c, srv, pub := newTestClient(t)
	resp, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/published",
	})
	require.NoError(t, err)
	srv.Wait()

	msgs := pub.Messages(events.TopicShortURLCreated)
	require.Len(t, msgs, 1)
	assert.Equal(t, resp.GetUrl().GetCode(), msgs[0].Key)

	var got eventsv1.ShortURLCreated
	require.NoError(t, proto.Unmarshal(msgs[0].Value, &got))
	assert.Equal(t, resp.GetUrl().GetCode(), got.GetCode())
	assert.Equal(t, "https://example.com/published", got.GetLongUrl())
	assert.NotNil(t, got.GetCreatedAt())
}

func TestShorten_DoesNotPublishOnIdempotentReturn(t *testing.T) {
	t.Parallel()

	c, srv, pub := newTestClient(t)
	req := &shortenerv1.ShortenRequest{
		LongUrl:        "https://example.com/no-double-publish",
		OwnerId:        "alice",
		IdempotencyKey: "k-once",
	}

	_, err := c.Shorten(context.Background(), req)
	require.NoError(t, err)
	_, err = c.Shorten(context.Background(), req)
	require.NoError(t, err)
	srv.Wait()

	assert.Len(t, pub.Messages(events.TopicShortURLCreated), 1,
		"second Shorten with the same idempotency key must not republish")
}

func TestShorten_PublishesEachCreateWhenNoKey(t *testing.T) {
	t.Parallel()

	c, srv, pub := newTestClient(t)
	long := "https://example.com/each-publishes"

	_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: long})
	require.NoError(t, err)
	_, err = c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: long})
	require.NoError(t, err)
	srv.Wait()

	assert.Len(t, pub.Messages(events.TopicShortURLCreated), 2,
		"each new row must publish its own ShortURLCreated event")
}

func TestShorten_StillSucceedsWhenPublishFails(t *testing.T) {
	t.Parallel()

	srv, pub := newTestServer(t)
	pub.SetError(assert.AnError)
	c := dialBuf(t, srv)

	resp, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{
		LongUrl: "https://example.com/publish-fail",
	})
	require.NoError(t, err, "publish failure must not fail the RPC")
	assert.True(t, resp.GetCreated())
	assert.NotEmpty(t, resp.GetUrl().GetCode())
}

func TestShorten_NilPublisherIsAllowed(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repo.Migrate(context.Background(), db))

	gen, err := codegen.NewGenerator(8, nil)
	require.NoError(t, err)

	srv := New(repo.New(db), gen, nil, 5)
	c := dialBuf(t, srv)

	_, err = c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://x.example"})
	require.NoError(t, err)
}

// scriptedGen returns predetermined codes in order.
type scriptedGen struct {
	codes  []string
	pos    int
	repeat bool
}

func (s *scriptedGen) Generate() (string, error) {
	c := s.codes[s.pos]
	if !s.repeat {
		s.pos++
	}
	return c, nil
}
