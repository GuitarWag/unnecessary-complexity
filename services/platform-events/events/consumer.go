package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
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
	newMsg  func() T
	handler Handler[T]
}

// NewConsumer wires a reader, a factory for the proto message type, and a handler.
func NewConsumer[T proto.Message](r KafkaReader, newMsg func() T, handler Handler[T]) *Consumer[T] {
	return &Consumer[T]{r: r, newMsg: newMsg, handler: handler}
}

// Run consumes messages until ctx is cancelled or a non-recoverable error occurs.
// Decode errors are logged via the supplied onDecodeErr callback if non-nil; otherwise dropped.
func (c *Consumer[T]) Run(ctx context.Context, onDecodeErr func(error)) error {
	for {
		msg, err := c.r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("consumer: fetch: %w", err)
		}

		decoded := c.newMsg()
		if err := proto.Unmarshal(msg.Value, decoded); err != nil {
			if onDecodeErr != nil {
				onDecodeErr(fmt.Errorf("consumer: unmarshal: %w", err))
			}
			// Skip undecodable messages by committing — they're poison and would block the partition otherwise.
			if err := c.r.CommitMessages(ctx, msg); err != nil {
				return fmt.Errorf("consumer: commit poison: %w", err)
			}
			continue
		}

		if err := c.handler(ctx, string(msg.Key), decoded); err != nil {
			return fmt.Errorf("consumer: handler: %w", err)
		}
		if err := c.r.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("consumer: commit: %w", err)
		}
	}
}

// Close closes the underlying reader.
func (c *Consumer[T]) Close() error {
	if err := c.r.Close(); err != nil {
		return fmt.Errorf("consumer: close: %w", err)
	}
	return nil
}
