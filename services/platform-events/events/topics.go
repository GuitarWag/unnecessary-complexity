// Package events defines the publish/subscribe contract used by all services.
package events

// Kafka topic names.
const (
	TopicShortURLCreated = "short_urls.created"
	TopicClickRecorded   = "clicks.recorded"
)
