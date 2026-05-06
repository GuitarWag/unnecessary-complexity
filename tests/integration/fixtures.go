//go:build integration

package integration

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
)

var fixtureCounter atomic.Uint64

type fixture struct {
	Code    string
	LongURL string
}

func uniqueLongURL(t *testing.T) string {
	t.Helper()
	n := fixtureCounter.Add(1)
	return fmt.Sprintf("https://example.com/it/%d/%d/%s", time.Now().UnixNano(), n, t.Name())
}

func seedShortURL(t *testing.T, c clients) fixture {
	t.Helper()
	long := uniqueLongURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.shortener.Shorten(ctx, connect.NewRequest(&shortenerv1.ShortenRequest{
		LongUrl: long,
	}))
	require.NoError(t, err, "fixture: Shorten %s", long)
	require.NotNil(t, resp.Msg.GetUrl())
	require.NotEmpty(t, resp.Msg.GetUrl().GetCode())

	return fixture{Code: resp.Msg.GetUrl().GetCode(), LongURL: long}
}

func seedShortURLs(t *testing.T, c clients, n int) []fixture {
	t.Helper()
	out := make([]fixture, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, seedShortURL(t, c))
	}
	return out
}
