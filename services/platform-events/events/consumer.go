package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"

	"github.com/yld/url-shortener/services/platform-events/metrics"
)

// KafkaReader is the subset of *kafka.Reader used by Consumer.
type KafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Handler processes a decoded event. Returning an error prevents the offset commit.
type Handler[T proto.Message] func(ctx context.Context, key string, msg T) error

// Consumer is a generic Kafka → proto consumer that commits offsets only after the handler succeeds.
type Consumer[T proto.Message] struct {
	r       KafkaReader
	topic   string // for metrics; empty when the reader does not say
	newMsg  func() T
	handler Handler[T]
}

// NewConsumer wires a reader, a factory for the proto message type, and a handler.
func NewConsumer[T proto.Message](r KafkaReader, newMsg func() T, handler Handler[T]) *Consumer[T] {
	c := &Consumer[T]{r: r, newMsg: newMsg, handler: handler}
	if rc, ok := r.(interface{ Config() kafka.ReaderConfig }); ok {
		c.topic = rc.Config().Topic
	}
	return c
}

// Backoff bounds for retrying fetch and commit errors.
const (
	retryMin = 200 * time.Millisecond
	retryMax = 10 * time.Second
)

// Run consumes messages until ctx is cancelled or the handler fails.
//
// Fetch and commit errors are retried with capped exponential backoff, not
// returned: a broker restart, a coordinator election or a laptop waking from
// sleep all produce them, and exiting turned each blip into a crash loop. A
// commit is retried, never skipped, because a skipped commit redelivers the
// message and the handler would apply it twice. onErr, if non-nil, sees every
// retried error and every undecodable (skipped) message.
func (c *Consumer[T]) Run(ctx context.Context, onErr func(error)) error {
	report := func(err error) {
		if onErr != nil {
			onErr(err)
		}
	}
	for {
		var msg kafka.Message
		if !retry(ctx, func() (err error) {
			msg, err = c.r.FetchMessage(ctx)
			return err
		}, func(err error) {
			metrics.ObserveEvent(c.topic, "fetch_error", 0)
			report(fmt.Errorf("consumer: fetch (retrying): %w", err))
		}) {
			return nil
		}

		decoded := c.newMsg()
		if err := proto.Unmarshal(msg.Value, decoded); err != nil {
			metrics.ObserveEvent(msg.Topic, "decode_error", 0)
			report(fmt.Errorf("consumer: unmarshal: %w", err))
			// Skip undecodable messages by committing; they are poison and would block the partition otherwise.
			if !c.commit(ctx, msg, report) {
				return nil
			}
			continue
		}

		start := time.Now()
		if err := c.handler(ctx, string(msg.Key), decoded); err != nil {
			metrics.ObserveEvent(msg.Topic, "handler_error", time.Since(start))
			return fmt.Errorf("consumer: handler: %w", err)
		}
		metrics.ObserveEvent(msg.Topic, "ok", time.Since(start))
		if !c.commit(ctx, msg, report) {
			return nil
		}
	}
}

func (c *Consumer[T]) commit(ctx context.Context, msg kafka.Message, report func(error)) bool {
	return retry(ctx, func() error { return c.r.CommitMessages(ctx, msg) }, func(err error) {
		report(fmt.Errorf("consumer: commit (retrying): %w", err))
	})
}

// retry runs op until it succeeds or ctx ends, waiting retryMin, doubling up
// to retryMax, between attempts. It reports each failure and returns false
// only when ctx ended.
func retry(ctx context.Context, op func() error, onFail func(error)) bool {
	wait := retryMin
	for {
		err := op()
		if err == nil {
			return true
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		onFail(err)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait):
		}
		wait = min(wait*2, retryMax)
	}
}

// Close closes the underlying reader.
func (c *Consumer[T]) Close() error {
	if err := c.r.Close(); err != nil {
		return fmt.Errorf("consumer: close: %w", err)
	}
	return nil
}
