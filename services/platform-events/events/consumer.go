package events

import (
	"context"
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
	giveUp  time.Duration
	newMsg  func() T
	handler Handler[T]
}

// NewConsumer wires a reader, a factory for the proto message type, and a handler.
func NewConsumer[T proto.Message](r KafkaReader, newMsg func() T, handler Handler[T]) *Consumer[T] {
	c := &Consumer[T]{r: r, newMsg: newMsg, handler: handler, giveUp: defaultGiveUp}
	if rc, ok := r.(interface{ Config() kafka.ReaderConfig }); ok {
		c.topic = rc.Config().Topic
	}
	return c
}

// Backoff bounds for retrying fetch and commit errors.
const (
	retryMin = 200 * time.Millisecond
	retryMax = 10 * time.Second
	// defaultGiveUp ends Run after this long of continuous failure. That is
	// longer than a broker restart or a laptop wake, and short enough that a
	// permanent fault (missing topic, bad auth) restarts the pod instead of
	// leaving it Ready while it consumes nothing.
	defaultGiveUp = 5 * time.Minute
	// finalCommitTimeout bounds the one commit attempt made during shutdown.
	finalCommitTimeout = 5 * time.Second
)

// Run consumes messages until ctx is cancelled, the handler fails, or Kafka
// stays unreachable for longer than the give-up window.
//
// Fetch and commit errors are retried with capped exponential backoff: a broker
// restart, a coordinator election or a laptop waking from sleep all produce
// them, and returning on the first one turned each blip into a crash loop. A
// commit is retried rather than skipped, and makes one last attempt during
// shutdown, because a skipped commit redelivers the message and the handler
// would apply it twice. onErr, if non-nil, sees every retried error and every
// undecodable (skipped) message.
func (c *Consumer[T]) Run(ctx context.Context, onErr func(error)) error {
	report := func(err error) {
		if onErr != nil {
			onErr(err)
		}
	}
	for {
		var msg kafka.Message
		err := retry(ctx, c.giveUp, func() (err error) {
			msg, err = c.r.FetchMessage(ctx)
			return err
		}, func(err error) {
			metrics.ObserveEvent(c.topic, "fetch_error", 0)
			report(fmt.Errorf("consumer: fetch (retrying): %w", err))
		})
		if done(ctx) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("consumer: fetch: %w", err)
		}

		decoded := c.newMsg()
		if err := proto.Unmarshal(msg.Value, decoded); err != nil {
			metrics.ObserveEvent(msg.Topic, "decode_error", 0)
			report(fmt.Errorf("consumer: unmarshal: %w", err))
			// Skip undecodable messages by committing; they are poison and would block the partition otherwise.
			if err := c.commit(ctx, msg, report); err != nil {
				return err
			}
			if done(ctx) {
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
		if err := c.commit(ctx, msg, report); err != nil {
			return err
		}
		if done(ctx) {
			return nil
		}
	}
}

// done reports whether ctx is cancelled: a clean shutdown, not an error.
func done(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// commit retries until it succeeds, gives up, or ctx ends. If ctx ends first,
// it makes one last attempt on a fresh deadline, because the handler has
// already applied this message.
func (c *Consumer[T]) commit(ctx context.Context, msg kafka.Message, report func(error)) error {
	err := retry(ctx, c.giveUp, func() error { return c.r.CommitMessages(ctx, msg) }, func(err error) {
		report(fmt.Errorf("consumer: commit (retrying): %w", err))
	})
	if err == nil {
		return nil
	}
	if ctx.Err() == nil {
		return fmt.Errorf("consumer: commit: %w", err)
	}
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalCommitTimeout)
	defer cancel()
	if ferr := c.r.CommitMessages(fctx, msg); ferr != nil {
		report(fmt.Errorf("consumer: final commit at shutdown failed, message will be redelivered: %w", ferr))
	}
	return nil
}

// retry runs op until it succeeds, ctx ends, or op has failed without a break
// for giveUp. Between attempts it waits retryMin, doubling up to retryMax. It
// returns nil on success, ctx.Err() when ctx ended, and the last error when it
// gave up. Only the caller's ctx counts as shutdown: an op error that merely
// wraps a deadline of its own is retried like any other.
func retry(ctx context.Context, giveUp time.Duration, op func() error, onFail func(error)) error {
	wait := retryMin
	start := time.Now()
	for {
		err := op()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) >= giveUp {
			return err
		}
		onFail(err)
		select {
		case <-ctx.Done():
			return ctx.Err()
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
