package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func InitProducer(logger *zap.Logger) (sarama.SyncProducer, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 5

	brokers := []string{getEnv("KAFKA_BROKER", "localhost:9092")}

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	logger.Info("Kafka producer initialized")
	return producer, nil
}

func PublishOrderEvent(ctx context.Context, producer sarama.SyncProducer, topic string, event any, logger *zap.Logger) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic:   topic,
		Value:   sarama.StringEncoder(eventJSON),
		Headers: []sarama.RecordHeader{},
	}

	// Inject trace context into Kafka message headers
	propagator := otel.GetTextMapPropagator()
	carrier := make(saramaHeaderCarrier, 0)
	propagator.Inject(ctx, &carrier)
	msg.Headers = []sarama.RecordHeader(carrier)

	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Extract trace ID for logging
	span := trace.SpanFromContext(ctx)
	traceID := ""
	if span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
	}

	logger.Info("Event published",
		zap.String("trace_id", traceID),
		zap.String("topic", topic),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
	)

	return nil
}

// saramaHeaderCarrier implements the TextMapCarrier interface for Kafka headers (for producer)
type saramaHeaderCarrier []sarama.RecordHeader

func (c saramaHeaderCarrier) Get(key string) string {
	for _, h := range c {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *saramaHeaderCarrier) Set(key, value string) {
	*c = append(*c, sarama.RecordHeader{
		Key:   []byte(key),
		Value: []byte(value),
	})
}

func (c saramaHeaderCarrier) Keys() []string {
	keys := make([]string, len(c))
	for i, h := range c {
		keys[i] = string(h.Key)
	}
	return keys
}
