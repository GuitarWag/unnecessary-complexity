//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
)

func TestMain(m *testing.M) {
	base := envOr(envGatewayURL, defaultBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout()+5*time.Second)
	defer cancel()
	if err := waitForGatewayReady(ctx, base, readyTimeout()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestHealthz_OK(t *testing.T) {
	c := newClients(t)
	require.NoError(t, waitForGatewayReady(context.Background(), c.baseURL, 5*time.Second))
}

func TestShorten_RoundTrip(t *testing.T) {
	c := newClients(t)
	fx := seedShortURL(t, c)

	resp, err := c.shortener.Get(context.Background(), connect.NewRequest(&shortenerv1.GetRequest{
		Code: fx.Code,
	}))
	require.NoError(t, err)
	assert.Equal(t, fx.Code, resp.Msg.GetUrl().GetCode())
	assert.Equal(t, fx.LongURL, resp.Msg.GetUrl().GetLongUrl())
}

func TestShorten_NoKeyAlwaysCreatesFreshRow(t *testing.T) {
	c := newClients(t)
	long := uniqueLongURL(t)
	ctx := context.Background()

	first, err := c.shortener.Shorten(ctx, connect.NewRequest(&shortenerv1.ShortenRequest{LongUrl: long}))
	require.NoError(t, err)
	assert.True(t, first.Msg.GetCreated())

	second, err := c.shortener.Shorten(ctx, connect.NewRequest(&shortenerv1.ShortenRequest{LongUrl: long}))
	require.NoError(t, err)
	assert.True(t, second.Msg.GetCreated())
	assert.NotEqual(t, first.Msg.GetUrl().GetCode(), second.Msg.GetUrl().GetCode(),
		"without an idempotency key, repeated shortens must produce distinct codes")
}

func TestShorten_IdempotentWithKey(t *testing.T) {
	c := newClients(t)
	long := uniqueLongURL(t)
	key := fmt.Sprintf("it-%d", time.Now().UnixNano())
	ctx := context.Background()

	req := &shortenerv1.ShortenRequest{
		LongUrl:        long,
		OwnerId:        "alice",
		IdempotencyKey: key,
	}
	first, err := c.shortener.Shorten(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	assert.True(t, first.Msg.GetCreated())

	second, err := c.shortener.Shorten(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	assert.False(t, second.Msg.GetCreated())
	assert.Equal(t, first.Msg.GetUrl().GetCode(), second.Msg.GetUrl().GetCode())
}

func TestShorten_KeyScopedByOwner(t *testing.T) {
	c := newClients(t)
	long := uniqueLongURL(t)
	key := fmt.Sprintf("it-%d", time.Now().UnixNano())
	ctx := context.Background()

	alice, err := c.shortener.Shorten(ctx, connect.NewRequest(&shortenerv1.ShortenRequest{
		LongUrl: long, OwnerId: "alice", IdempotencyKey: key,
	}))
	require.NoError(t, err)
	bob, err := c.shortener.Shorten(ctx, connect.NewRequest(&shortenerv1.ShortenRequest{
		LongUrl: long, OwnerId: "bob", IdempotencyKey: key,
	}))
	require.NoError(t, err)

	assert.NotEqual(t, alice.Msg.GetUrl().GetCode(), bob.Msg.GetUrl().GetCode())
}

func TestShorten_RejectsInvalidURL(t *testing.T) {
	c := newClients(t)
	for _, bad := range []string{"", "javascript:alert(1)", "not a url", "ftp://x"} {
		t.Run(fmt.Sprintf("input=%q", bad), func(t *testing.T) {
			_, err := c.shortener.Shorten(context.Background(), connect.NewRequest(&shortenerv1.ShortenRequest{
				LongUrl: bad,
			}))
			require.Error(t, err)
			var ce *connect.Error
			require.True(t, errors.As(err, &ce), "expected connect.Error, got %T", err)
			assert.Equal(t, connect.CodeInvalidArgument, ce.Code())
		})
	}
}

func TestGet_NotFound(t *testing.T) {
	c := newClients(t)
	_, err := c.shortener.Get(context.Background(), connect.NewRequest(&shortenerv1.GetRequest{
		Code: "nonexistent-code-xyz",
	}))
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeNotFound, ce.Code())
}

func TestResolver_PropagatesViaKafka(t *testing.T) {
	c := newClients(t)
	fx := seedShortURL(t, c)

	require.Eventually(t, func() bool {
		resp, err := c.resolver.Resolve(context.Background(), connect.NewRequest(&resolverv1.ResolveRequest{
			Code: fx.Code,
		}))
		if err != nil {
			return false
		}
		return resp.Msg.GetLongUrl() == fx.LongURL
	}, propagationTimeout(), 500*time.Millisecond, "resolver did not see code %q in time", fx.Code)
}

func TestResolver_NotFoundForUnknownCode(t *testing.T) {
	c := newClients(t)
	_, err := c.resolver.Resolve(context.Background(), connect.NewRequest(&resolverv1.ResolveRequest{
		Code: "nope-xyz",
	}))
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeNotFound, ce.Code())
}

func TestAnalytics_TracksClicks(t *testing.T) {
	c := newClients(t)
	fx := seedShortURL(t, c)

	require.Eventually(t, func() bool {
		_, err := c.resolver.Resolve(context.Background(), connect.NewRequest(&resolverv1.ResolveRequest{
			Code: fx.Code,
		}))
		return err == nil
	}, propagationTimeout(), 500*time.Millisecond, "resolver never resolved %q", fx.Code)

	const clicks = 3
	for i := 0; i < clicks; i++ {
		_, err := c.resolver.Resolve(context.Background(), connect.NewRequest(&resolverv1.ResolveRequest{
			Code:      fx.Code,
			UserAgent: fmt.Sprintf("integration-test/%d", i),
		}))
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		resp, err := c.analytics.Stats(context.Background(), connect.NewRequest(&analyticsv1.StatsRequest{
			Code: fx.Code,
		}))
		if err != nil {
			return false
		}
		return resp.Msg.GetTotal() >= int64(clicks)
	}, propagationTimeout(), 500*time.Millisecond, "analytics never saw %d clicks for %q", clicks, fx.Code)
}

func TestAnalytics_EmptyForUnknownCode(t *testing.T) {
	c := newClients(t)
	resp, err := c.analytics.Stats(context.Background(), connect.NewRequest(&analyticsv1.StatsRequest{
		Code: "no-such-code-zzz",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.GetTotal())
	assert.Empty(t, resp.Msg.GetDaily())
}

func TestAnalytics_RejectsBadDateFormat(t *testing.T) {
	c := newClients(t)
	_, err := c.analytics.Stats(context.Background(), connect.NewRequest(&analyticsv1.StatsRequest{
		Code:     "any",
		FromDate: "not-a-date",
	}))
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeInvalidArgument, ce.Code())
}
