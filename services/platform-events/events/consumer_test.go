package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

type fakeReader struct {
	mu        sync.Mutex
	queue     []kafka.Message
	committed []kafka.Message
	closed    bool
	fetchErr  error
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	for {
		f.mu.Lock()
		if f.fetchErr != nil {
			f.mu.Unlock()
			return kafka.Message{}, f.fetchErr
		}
		if len(f.queue) > 0 {
			m := f.queue[0]
			f.queue = f.queue[1:]
			f.mu.Unlock()
			return m, nil
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return kafka.Message{}, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (f *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = append(f.committed, msgs...)
	return nil
}

func (f *fakeReader) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeReader) push(t *testing.T, key string, msg proto.Message) {
	t.Helper()
	b, err := proto.Marshal(msg)
	require.NoError(t, err)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, kafka.Message{Key: []byte(key), Value: b})
}

func (f *fakeReader) pushRaw(value []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, kafka.Message{Key: []byte("k"), Value: value})
}

func TestConsumer_DispatchesAndCommits(t *testing.T) {
	t.Parallel()

	r := &fakeReader{}
	r.push(t, "abc", &eventsv1.ShortURLCreated{Code: "abc", LongUrl: "https://x"})

	var got *eventsv1.ShortURLCreated
	var gotKey string
	doneCh := make(chan struct{})

	c := NewConsumer(r,
		func() *eventsv1.ShortURLCreated { return &eventsv1.ShortURLCreated{} },
		func(_ context.Context, key string, msg *eventsv1.ShortURLCreated) error {
			gotKey = key
			got = msg
			close(doneCh)
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx, nil) }()

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("handler not invoked")
	}

	cancel()
	require.NotNil(t, got)
	assert.Equal(t, "abc", got.GetCode())
	assert.Equal(t, "abc", gotKey)

	// give the goroutine a moment to commit then exit
	time.Sleep(20 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Len(t, r.committed, 1)
}

func TestConsumer_HandlerErrorStopsRunWithoutCommit(t *testing.T) {
	t.Parallel()

	r := &fakeReader{}
	r.push(t, "abc", &eventsv1.ShortURLCreated{Code: "abc"})

	c := NewConsumer(r,
		func() *eventsv1.ShortURLCreated { return &eventsv1.ShortURLCreated{} },
		func(context.Context, string, *eventsv1.ShortURLCreated) error {
			return errors.New("handler boom")
		},
	)

	err := c.Run(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler boom")

	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Empty(t, r.committed)
}

func TestConsumer_PoisonMessageIsCommittedAndSkipped(t *testing.T) {
	t.Parallel()

	r := &fakeReader{}
	r.pushRaw([]byte{0xFF, 0xFF, 0xFF}) // garbage that won't unmarshal as proto
	r.push(t, "good", &eventsv1.ShortURLCreated{Code: "good"})

	var decodeErrs int
	var handled int
	doneCh := make(chan struct{})

	c := NewConsumer(r,
		func() *eventsv1.ShortURLCreated { return &eventsv1.ShortURLCreated{} },
		func(_ context.Context, _ string, msg *eventsv1.ShortURLCreated) error {
			handled++
			if msg.GetCode() == "good" {
				close(doneCh)
			}
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx, func(error) { decodeErrs++ }) }()

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("good message not handled")
	}
	cancel()
	time.Sleep(20 * time.Millisecond)

	assert.Equal(t, 1, decodeErrs)
	assert.Equal(t, 1, handled)
	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Len(t, r.committed, 2, "both poison and good message must be committed")
}

func TestConsumer_ContextCancelExitsCleanly(t *testing.T) {
	t.Parallel()

	r := &fakeReader{}
	c := NewConsumer(r,
		func() *eventsv1.ShortURLCreated { return &eventsv1.ShortURLCreated{} },
		func(context.Context, string, *eventsv1.ShortURLCreated) error { return nil },
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, c.Run(ctx, nil))
}
