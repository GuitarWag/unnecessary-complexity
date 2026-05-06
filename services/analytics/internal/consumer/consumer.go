// Package consumer wires Kafka click events to the analytics fact table.
package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yld/url-shortener/services/analytics/internal/repo"
	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

// ClickInserter is the slice of repo.Repository used by the consumer.
type ClickInserter interface {
	InsertClick(ctx context.Context, c repo.Click) error
}

// NewClickRecordedHandler returns a handler that persists ClickRecorded events.
func NewClickRecordedHandler(r ClickInserter) events.Handler[*eventsv1.ClickRecorded] {
	return func(ctx context.Context, key string, msg *eventsv1.ClickRecorded) error {
		code := msg.GetCode()
		if code == "" {
			code = key
		}
		if code == "" {
			return errors.New("consumer: ClickRecorded has empty code")
		}
		clicked := time.Now().UTC()
		if ts := msg.GetClickedAt(); ts != nil {
			clicked = ts.AsTime().UTC()
		}
		if err := r.InsertClick(ctx, repo.Click{
			Code:      code,
			ClickedAt: clicked,
			UserAgent: msg.GetUserAgent(),
			Referer:   msg.GetReferer(),
			IP:        msg.GetIp(),
		}); err != nil {
			return fmt.Errorf("consumer: insert click: %w", err)
		}
		return nil
	}
}
