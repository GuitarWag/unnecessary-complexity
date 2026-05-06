package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

func TestFakePublisher_RecordsMessages(t *testing.T) {
	t.Parallel()

	f := NewFakePublisher()

	evt := &eventsv1.ShortURLCreated{
		Code:      "abc123",
		LongUrl:   "https://example.com",
		CreatedAt: timestamppb.New(time.Now().UTC()),
	}

	require.NoError(t, f.Publish(context.Background(), TopicShortURLCreated, evt.GetCode(), evt))

	got := f.Messages(TopicShortURLCreated)
	require.Len(t, got, 1)
	assert.Equal(t, "abc123", got[0].Key)

	var decoded eventsv1.ShortURLCreated
	require.NoError(t, proto.Unmarshal(got[0].Value, &decoded))
	assert.Equal(t, "abc123", decoded.GetCode())
	assert.Equal(t, "https://example.com", decoded.GetLongUrl())
}

func TestFakePublisher_PartitionsByTopic(t *testing.T) {
	t.Parallel()

	f := NewFakePublisher()
	require.NoError(t, f.Publish(context.Background(), "topic-a", "k1", &eventsv1.ShortURLCreated{Code: "1"}))
	require.NoError(t, f.Publish(context.Background(), "topic-b", "k2", &eventsv1.ShortURLCreated{Code: "2"}))

	assert.Len(t, f.Messages("topic-a"), 1)
	assert.Len(t, f.Messages("topic-b"), 1)
	assert.Empty(t, f.Messages("topic-c"))
}

func TestFakePublisher_FailsWhenConfigured(t *testing.T) {
	t.Parallel()

	want := errors.New("kafka down")
	f := NewFakePublisher()
	f.SetError(want)

	err := f.Publish(context.Background(), TopicShortURLCreated, "k", &eventsv1.ShortURLCreated{Code: "x"})
	assert.ErrorIs(t, err, want)
	assert.Empty(t, f.Messages(TopicShortURLCreated))
}

func TestFakePublisher_RejectsNilMessage(t *testing.T) {
	t.Parallel()

	f := NewFakePublisher()
	err := f.Publish(context.Background(), TopicShortURLCreated, "k", nil)
	assert.Error(t, err)
}

func TestFakePublisher_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	f := NewFakePublisher()
	const n = 100
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			_ = f.Publish(context.Background(), TopicShortURLCreated, "k", &eventsv1.ShortURLCreated{Code: "x"})
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	assert.Len(t, f.Messages(TopicShortURLCreated), n)
}
