// Package consumer wires Kafka events to the resolver's local cache.
package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	"github.com/yld/url-shortener/services/resolver/internal/repo"
)

// Upserter is the slice of repo.Repository used by the consumer.
type Upserter interface {
	Upsert(ctx context.Context, s repo.ShortURL) error
}

// NewShortURLCreatedHandler returns an events.Handler that upserts ShortURLCreated events
// into the resolver's local cache.
func NewShortURLCreatedHandler(u Upserter) events.Handler[*eventsv1.ShortURLCreated] {
	return func(ctx context.Context, key string, msg *eventsv1.ShortURLCreated) error {
		code := msg.GetCode()
		if code == "" {
			code = key
		}
		if code == "" {
			return errors.New("consumer: ShortURLCreated has empty code")
		}
		var created time.Time
		if ts := msg.GetCreatedAt(); ts != nil {
			created = ts.AsTime()
		}
		if err := u.Upsert(ctx, repo.ShortURL{
			Code:      code,
			LongURL:   msg.GetLongUrl(),
			CreatedAt: created,
		}); err != nil {
			return fmt.Errorf("consumer: upsert: %w", err)
		}
		return nil
	}
}
