package events

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"
)

// Publisher emits proto events to a topic. Implementations must be safe for concurrent use.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, msg proto.Message) error
	Close() error
}

// Message is a topic/key/value tuple captured by FakePublisher.
type Message struct {
	Topic string
	Key   string
	Value []byte
}

// FakePublisher is an in-memory Publisher for tests.
type FakePublisher struct {
	mu       sync.Mutex
	byTop    map[string][]Message
	failWith error
}

// NewFakePublisher returns an empty FakePublisher.
func NewFakePublisher() *FakePublisher {
	return &FakePublisher{byTop: map[string][]Message{}}
}

// Publish stores the marshalled message under topic.
func (f *FakePublisher) Publish(_ context.Context, topic, key string, msg proto.Message) error {
	if msg == nil {
		return errors.New("events: message is nil")
	}
	value, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("events: marshal: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.byTop[topic] = append(f.byTop[topic], Message{Topic: topic, Key: key, Value: value})
	return nil
}

// Close is a no-op for FakePublisher.
func (f *FakePublisher) Close() error { return nil }

// Messages returns the messages captured for topic.
func (f *FakePublisher) Messages(topic string) []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Message, len(f.byTop[topic]))
	copy(out, f.byTop[topic])
	return out
}

// SetError makes subsequent Publish calls return err.
func (f *FakePublisher) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWith = err
}
