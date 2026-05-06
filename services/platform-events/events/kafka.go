package events

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

// KafkaWriter is the subset of *kafka.Writer used by KafkaPublisher.
// Defined as an interface so tests can substitute a fake.
type KafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// KafkaPublisher publishes proto messages to Kafka.
type KafkaPublisher struct {
	w KafkaWriter
}

// NewKafkaPublisher constructs a KafkaPublisher writing to the given brokers.
// Topic is set per-Publish, not bound to the writer.
func NewKafkaPublisher(brokers []string) *KafkaPublisher {
	return &KafkaPublisher{
		w: &kafka.Writer{
			Addr:                   kafka.TCP(brokers...),
			Balancer:               &kafka.Hash{},
			RequiredAcks:           kafka.RequireAll,
			AllowAutoTopicCreation: true,
			Async:                  false,
		},
	}
}

// NewKafkaPublisherWithWriter is for tests injecting a fake KafkaWriter.
func NewKafkaPublisherWithWriter(w KafkaWriter) *KafkaPublisher {
	return &KafkaPublisher{w: w}
}

// Publish marshals msg and writes a single Kafka message to topic.
func (p *KafkaPublisher) Publish(ctx context.Context, topic, key string, msg proto.Message) error {
	if msg == nil {
		return fmt.Errorf("events: message is nil")
	}
	value, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("events: marshal: %w", err)
	}
	if err := p.w.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	}); err != nil {
		return fmt.Errorf("events: write %s: %w", topic, err)
	}
	return nil
}

// Close flushes and closes the underlying writer.
func (p *KafkaPublisher) Close() error {
	if err := p.w.Close(); err != nil {
		return fmt.Errorf("events: close writer: %w", err)
	}
	return nil
}
