package service

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
	"github.com/yld/url-shortener/services/shortener/internal/codegen"
	"github.com/yld/url-shortener/services/shortener/internal/pgtest"
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

func newTestRepo(t *testing.T) *repo.Repository {
	t.Helper()
	db := pgtest.New(t)
	require.NoError(t, repo.Migrate(context.Background(), db))
	return repo.New(db)
}

func newTestClient(t *testing.T) shortenerv1.ShortenerServiceClient {
	t.Helper()
	gen, err := codegen.NewGenerator(8, nil)
	require.NoError(t, err)
	return dialBuf(t, New(newTestRepo(t), gen, 5))
}

func TestShorten_CreatesNew(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
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

	c := newTestClient(t)
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

	c := newTestClient(t)
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

	c := newTestClient(t)
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

	c := newTestClient(t)
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

	c := newTestClient(t)
	_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestShorten_RejectsNonHTTPURL(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	for _, bad := range []string{"javascript:alert(1)", "ftp://x", "not a url", "//example.com"} {
		_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: bad})
		require.Errorf(t, err, "input: %q", bad)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "input: %q", bad)
	}
}

func TestGet_ReturnsExisting(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
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

	c := newTestClient(t)
	_, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: "missing"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGet_RejectsEmptyCode(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	_, err := c.Get(context.Background(), &shortenerv1.GetRequest{Code: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestShorten_RetriesOnCollision(t *testing.T) {
	t.Parallel()

	// Force the generator to emit the same code twice in a row, then a fresh one.
	scripted := &scriptedGen{codes: []string{"AAAAAAAA", "AAAAAAAA", "BBBBBBBB"}}
	srv := New(newTestRepo(t), scripted, 5)
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
	srv := New(newTestRepo(t), scripted, 3)
	c := dialBuf(t, srv)

	_, err := c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/first"})
	require.NoError(t, err)

	_, err = c.Shorten(context.Background(), &shortenerv1.ShortenRequest{LongUrl: "https://a.example/second"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestServer_RoundTripPreservesCreatedAt(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
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

func TestShorten_RejectsBadOwnerOrKey(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	long := strings.Repeat("k", maxOwnerKeyLen+1)
	for _, req := range []*shortenerv1.ShortenRequest{
		{LongUrl: "https://example.com", OwnerId: "a\x00b"},
		{LongUrl: "https://example.com", IdempotencyKey: "k\x00"},
		{LongUrl: "https://example.com", IdempotencyKey: long},
		{LongUrl: "https://example.com", OwnerId: long},
	} {
		_, err := c.Shorten(context.Background(), req)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}
