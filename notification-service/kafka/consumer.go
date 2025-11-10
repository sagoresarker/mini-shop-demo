package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"notification-svc/middleware"

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
	config.Consumer.Retry.Backoff = 1 * time.Second

	brokers := []string{getEnv("KAFKA_BROKER", "localhost:9092")}

	consumer, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	logger.Info("Kafka consumer initialized")
	return consumer, nil
}

func StartConsumer(consumer sarama.Consumer, logger *zap.Logger) error {
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
			if err := handleMessageWithRetry(message, logger, 3); err != nil {
				logger.Error("Failed to handle message after retries", zap.Error(err))
			}
		case err := <-partitionConsumer.Errors():
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}
}

func handleMessageWithRetry(message *sarama.ConsumerMessage, logger *zap.Logger, maxRetries int) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := handleMessage(message, logger)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries {
			backoff := time.Duration(attempt) * time.Second
			logger.Warn("Retrying message handling",
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
				zap.Error(err),
			)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func handleMessage(message *sarama.ConsumerMessage, logger *zap.Logger) error {
	// Extract trace context from Kafka message headers
	var propagator propagation.TextMapPropagator = otel.GetTextMapPropagator()
	carrier := saramaHeaderCarrierConsumer(message.Headers)
	ctx := propagator.Extract(context.Background(), carrier)

	var tracer trace.Tracer = otel.Tracer("notification-service")
	ctx, span := tracer.Start(ctx, "ProcessNotification")
	defer span.End()

	// traceID will be extracted in handler functions using middleware.GetTraceID
	_ = span

	var event map[string]interface{}
	if err := json.Unmarshal(message.Value, &event); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	eventType, ok := event["event_type"].(string)
	if !ok {
		span.End()
		return fmt.Errorf("missing event_type in event")
	}

	span.SetAttributes(attribute.String("event.type", eventType))

	// Handle different event types
	switch eventType {
	case "order_created":
		handleOrderCreated(ctx, event, logger, span)
	case "payment_success":
		handlePaymentSuccess(ctx, event, logger, span)
	case "payment_failed":
		handlePaymentFailed(ctx, event, logger, span)
	default:
		logger.Debug("Unknown event type", zap.String("event_type", eventType))
	}

	return nil
}

func handleOrderCreated(ctx context.Context, event map[string]interface{}, logger *zap.Logger, span trace.Span) {
	middleware.RecordNotificationSent("order_created")
	orderID, _ := event["order_id"].(float64)
	userID, _ := event["user_id"].(float64)

	span.SetAttributes(
		attribute.Int("order.id", int(orderID)),
		attribute.Int("user.id", int(userID)),
	)

	message := fmt.Sprintf("Your order #%.0f has been placed successfully! We'll notify you once it's confirmed.", orderID)
	traceID := middleware.GetTraceID(ctx)
	logger.Info("Order notification sent",
		zap.String("trace_id", traceID),
		zap.Float64("order_id", orderID),
		zap.Float64("user_id", userID),
		zap.String("message", message),
	)

	// Simulate email sending
	fmt.Printf("[EMAIL] To: user_%.0f@example.com\n", userID)
	fmt.Printf("[EMAIL] Subject: Order Confirmation\n")
	fmt.Printf("[EMAIL] Body: %s\n\n", message)
}

func handlePaymentSuccess(ctx context.Context, event map[string]interface{}, logger *zap.Logger, span trace.Span) {
	middleware.RecordNotificationSent("payment_success")
	orderID, _ := event["order_id"].(float64)
	userID, _ := event["user_id"].(float64)
	transactionID, _ := event["transaction_id"].(string)

	span.SetAttributes(
		attribute.Int("order.id", int(orderID)),
		attribute.Int("user.id", int(userID)),
		attribute.String("transaction.id", transactionID),
	)

	message := fmt.Sprintf("Payment for order #%.0f was successful! Transaction ID: %s", orderID, transactionID)
	traceID := middleware.GetTraceID(ctx)
	logger.Info("Payment success notification sent",
		zap.String("trace_id", traceID),
		zap.Float64("order_id", orderID),
		zap.Float64("user_id", userID),
		zap.String("transaction_id", transactionID),
		zap.String("message", message),
	)

	// Simulate email sending
	fmt.Printf("[EMAIL] To: user_%.0f@example.com\n", userID)
	fmt.Printf("[EMAIL] Subject: Payment Successful\n")
	fmt.Printf("[EMAIL] Body: %s\n\n", message)
}

func handlePaymentFailed(ctx context.Context, event map[string]interface{}, logger *zap.Logger, span trace.Span) {
	middleware.RecordNotificationSent("payment_failed")
	orderID, _ := event["order_id"].(float64)
	userID, _ := event["user_id"].(float64)

	span.SetAttributes(
		attribute.Int("order.id", int(orderID)),
		attribute.Int("user.id", int(userID)),
	)

	message := fmt.Sprintf("Payment for order #%.0f failed. Please try again or contact support.", orderID)
	traceID := middleware.GetTraceID(ctx)
	logger.Info("Payment failure notification sent",
		zap.String("trace_id", traceID),
		zap.Float64("order_id", orderID),
		zap.Float64("user_id", userID),
		zap.String("message", message),
	)

	// Simulate email sending
	fmt.Printf("[EMAIL] To: user_%.0f@example.com\n", userID)
	fmt.Printf("[EMAIL] Subject: Payment Failed\n")
	fmt.Printf("[EMAIL] Body: %s\n\n", message)
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
