package kafka

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"order-svc/models"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func InitConsumer(logger *zap.Logger) (sarama.Consumer, error) {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true

	brokers := []string{getEnv("KAFKA_BROKER", "localhost:9092")}

	consumer, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	logger.Info("Kafka consumer initialized")
	return consumer, nil
}

func StartConsumer(consumer sarama.Consumer, db *sql.DB, logger *zap.Logger) error {
	topic := getEnv("KAFKA_TOPIC", "order_events")
	partitionConsumer, err := consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
	if err != nil {
		return fmt.Errorf("failed to consume partition: %w", err)
	}
	defer partitionConsumer.Close()

	logger.Info("Kafka consumer started", zap.String("topic", topic))

	for {
		select {
		case message := <-partitionConsumer.Messages():
			if err := handleMessage(message, db, logger); err != nil {
				logger.Error("Failed to handle message", zap.Error(err))
			}
		case err := <-partitionConsumer.Errors():
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}
}

func handleMessage(message *sarama.ConsumerMessage, db *sql.DB, logger *zap.Logger) error {
	// Extract trace context from Kafka message headers
	var propagator propagation.TextMapPropagator = otel.GetTextMapPropagator()
	carrier := saramaHeaderCarrierConsumer(message.Headers)
	ctx := propagator.Extract(context.Background(), carrier)

	var tracer trace.Tracer = otel.Tracer("order-service")
	ctx, span := tracer.Start(ctx, "ProcessOrderEvent")
	defer span.End()

	// Extract trace ID for logging
	traceID := ""
	if span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
	}

	var event models.OrderEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	span.SetAttributes(
		attribute.String("event.type", event.EventType),
		attribute.Int("order.id", event.OrderID),
	)

	logger.Info("Received event",
		zap.String("trace_id", traceID),
		zap.String("event_type", event.EventType),
		zap.Int("order_id", event.OrderID),
	)

	// Handle different event types for Saga pattern
	switch event.EventType {
	case "order_failed", "payment_failed":
		// Rollback order status
		_, err := db.ExecContext(ctx,
			"UPDATE orders SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			models.OrderStatusFailed, event.OrderID,
		)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update order status: %w", err)
		}
		logger.Info("Order status updated to failed", zap.String("trace_id", traceID), zap.Int("order_id", event.OrderID))
	case "order_paid", "payment_success":
		// Update order status to paid
		_, err := db.ExecContext(ctx,
			"UPDATE orders SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			models.OrderStatusPaid, event.OrderID,
		)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update order status: %w", err)
		}
		logger.Info("Order status updated to paid", zap.String("trace_id", traceID), zap.Int("order_id", event.OrderID))
	}

	return nil
}

// saramaHeaderCarrierConsumer implements the TextMapCarrier interface for Kafka headers (for consumer)
type saramaHeaderCarrierConsumer []*sarama.RecordHeader

func (c saramaHeaderCarrierConsumer) Get(key string) string {
	for _, h := range c {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c saramaHeaderCarrierConsumer) Set(key, value string) {
	// Not needed for extraction
}

func (c saramaHeaderCarrierConsumer) Keys() []string {
	keys := make([]string, len(c))
	for i, h := range c {
		keys[i] = string(h.Key)
	}
	return keys
}
