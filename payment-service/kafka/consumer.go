package kafka

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"payment-svc/models"

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

func StartConsumer(consumer sarama.Consumer, db *sql.DB, producer sarama.SyncProducer, logger *zap.Logger) error {
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
			if err := handleMessage(message, db, producer, logger); err != nil {
				logger.Error("Failed to handle message", zap.Error(err))
			}
		case err := <-partitionConsumer.Errors():
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}
}

func handleMessage(message *sarama.ConsumerMessage, db *sql.DB, producer sarama.SyncProducer, logger *zap.Logger) error {
	// Extract trace context from Kafka message headers
	var propagator propagation.TextMapPropagator = otel.GetTextMapPropagator()
	carrier := saramaHeaderCarrierConsumer(message.Headers)
	ctx := propagator.Extract(context.Background(), carrier)

	var tracer trace.Tracer = otel.Tracer("payment-service")
	ctx, span := tracer.Start(ctx, "ProcessPayment")
	defer span.End()

	// Extract trace ID for logging
	traceID := ""
	if span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
	}

	var orderEvent map[string]interface{}
	if err := json.Unmarshal(message.Value, &orderEvent); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	eventType, ok := orderEvent["event_type"].(string)
	if !ok || eventType != "order_created" {
		// Skip non-order_created events
		span.End()
		return nil
	}

	span.SetAttributes(attribute.String("event.type", eventType))

	orderID, ok := orderEvent["order_id"].(float64)
	if !ok {
		return fmt.Errorf("invalid order_id in event")
	}

	userID, ok := orderEvent["user_id"].(float64)
	if !ok {
		return fmt.Errorf("invalid user_id in event")
	}

	totalPrice, ok := orderEvent["total_price"].(float64)
	if !ok {
		return fmt.Errorf("invalid total_price in event")
	}

	span.SetAttributes(
		attribute.Int("order.id", int(orderID)),
		attribute.Int("user.id", int(userID)),
		attribute.Float64("amount", totalPrice),
	)

	logger.Info("Processing payment for order",
		zap.String("trace_id", traceID),
		zap.Float64("order_id", orderID),
		zap.Float64("user_id", userID),
		zap.Float64("amount", totalPrice),
	)

	// Simulate payment processing with random success/failure (80% success rate)
	rand.Seed(time.Now().UnixNano())
	success := rand.Float32() > 0.2

	span.SetAttributes(attribute.Bool("payment.success", success))

	// Create payment record
	var paymentID int
	var status models.PaymentStatus
	var transactionID string

	if success {
		status = models.PaymentStatusSuccess
		transactionID = fmt.Sprintf("TXN-%d-%d", int(orderID), time.Now().Unix())
	} else {
		status = models.PaymentStatusFailed
		transactionID = ""
	}

	err := db.QueryRowContext(ctx,
		"INSERT INTO payments (order_id, user_id, amount, status, transaction_id) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		int(orderID), int(userID), totalPrice, status, transactionID,
	).Scan(&paymentID)

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create payment record: %w", err)
	}

	span.SetAttributes(attribute.Int("payment.id", paymentID))

	// Publish payment event
	paymentEvent := models.PaymentEvent{
		PaymentID:     paymentID,
		OrderID:       int(orderID),
		UserID:        int(userID),
		Amount:        totalPrice,
		Status:        status,
		TransactionID: transactionID,
	}

	if success {
		paymentEvent.EventType = "payment_success"
		logger.Info("Payment successful", zap.String("trace_id", traceID), zap.Int("payment_id", paymentID), zap.String("transaction_id", transactionID))
	} else {
		paymentEvent.EventType = "payment_failed"
		logger.Info("Payment failed", zap.String("trace_id", traceID), zap.Int("payment_id", paymentID))
	}

	// Publish to Kafka
	if err := PublishPaymentEvent(ctx, producer, "order_events", paymentEvent, logger); err != nil {
		span.RecordError(err)
		logger.Error("Failed to publish payment event", zap.String("trace_id", traceID), zap.Error(err))
		// Don't fail the whole process, but log the error
	}

	// Record metrics (would need to import middleware, but for now just log)
	logger.Info("Payment processed", zap.String("trace_id", traceID), zap.String("status", string(status)))

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
