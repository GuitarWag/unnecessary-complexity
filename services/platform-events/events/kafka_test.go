package events

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

type fakeWriter struct {
	mu      sync.Mutex
	written []kafka.Message
	err     error
	closed  bool
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, msgs...)
	return nil
}

func (f *fakeWriter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestKafkaPublisher_WritesProtoMarshalledMessage(t *testing.T) {
	t.Parallel()

	w := &fakeWriter{}
	p := NewKafkaPublisherWithWriter(w)

	evt := &eventsv1.ShortURLCreated{Code: "abc", LongUrl: "https://x"}
	require.NoError(t, p.Publish(context.Background(), TopicShortURLCreated, "abc", evt))

	require.Len(t, w.written, 1)
	assert.Equal(t, TopicShortURLCreated, w.written[0].Topic)
	assert.Equal(t, []byte("abc"), w.written[0].Key)

	var got eventsv1.ShortURLCreated
	require.NoError(t, proto.Unmarshal(w.written[0].Value, &got))
	assert.Equal(t, "abc", got.GetCode())
}

func TestKafkaPublisher_WrapsWriterError(t *testing.T) {
	t.Parallel()

	w := &fakeWriter{err: errors.New("net down")}
	p := NewKafkaPublisherWithWriter(w)
	err := p.Publish(context.Background(), TopicShortURLCreated, "k", &eventsv1.ShortURLCreated{Code: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "net down")
}

func TestKafkaPublisher_RejectsNil(t *testing.T) {
	t.Parallel()

	p := NewKafkaPublisherWithWriter(&fakeWriter{})
	assert.Error(t, p.Publish(context.Background(), TopicShortURLCreated, "k", nil))
}

func TestKafkaPublisher_CloseDelegates(t *testing.T) {
	t.Parallel()

	w := &fakeWriter{}
	p := NewKafkaPublisherWithWriter(w)
	require.NoError(t, p.Close())
	assert.True(t, w.closed)
}
