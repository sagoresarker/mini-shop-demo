package kafka

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"payment-svc/models"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	rng                = rand.New(rand.NewSource(time.Now().UnixNano()))
	paymentSuccessRate = loadSuccessRate()
	minProcessingDelay = 200 * time.Millisecond
	maxAdditionalDelay = 800 * time.Millisecond
)

type orderCreatedEvent struct {
	EventType  string  `json:"event_type"`
	OrderID    int     `json:"order_id"`
	UserID     int     `json:"user_id"`
	ProductID  int     `json:"product_id"`
	Quantity   int     `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

func InitConsumer(logger *zap.Logger) (sarama.ConsumerGroup, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V2_8_0_0
	config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Consumer.Return.Errors = true

	brokers := []string{getEnv("KAFKA_BROKER", "localhost:9092")}
	groupID := getEnv("KAFKA_CONSUMER_GROUP", "payment-service")

	consumerGroup, err := sarama.NewConsumerGroup(brokers, groupID, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer group: %w", err)
	}

	logger.Info("Kafka consumer group initialized",
		zap.Strings("brokers", brokers),
		zap.String("group_id", groupID),
		zap.Float64("payment_success_rate", paymentSuccessRate),
	)

	return consumerGroup, nil
}

func StartConsumer(ctx context.Context, consumerGroup sarama.ConsumerGroup, db *sql.DB, producer sarama.SyncProducer, logger *zap.Logger) error {
	topics := []string{getEnv("KAFKA_TOPIC", "order_events")}
	handler := &paymentConsumerGroupHandler{
		db:       db,
		producer: producer,
		logger:   logger,
	}

	logger.Info("Kafka consumer loop started", zap.Strings("topics", topics))

	// Handle errors in a separate goroutine
	go func() {
		for err := range consumerGroup.Errors() {
			logger.Error("Kafka consumer group error", zap.Error(err))
		}
	}()

	for {
		if err := consumerGroup.Consume(ctx, topics, handler); err != nil {
			return fmt.Errorf("failed to consume topic: %w", err)
		}

		if ctx.Err() != nil {
			logger.Info("Kafka consumer context cancelled")
			return nil
		}
	}
}

type paymentConsumerGroupHandler struct {
	db       *sql.DB
	producer sarama.SyncProducer
	logger   *zap.Logger
}

func (h *paymentConsumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *paymentConsumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *paymentConsumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for message := range claim.Messages() {
		if err := handleMessage(message, h.db, h.producer, h.logger); err != nil {
			h.logger.Error("Failed to handle message", zap.Error(err))
		} else {
			session.MarkMessage(message, "")
		}
	}

	return nil
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

	var orderEvent orderCreatedEvent
	if err := json.Unmarshal(message.Value, &orderEvent); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	if orderEvent.EventType != "order_created" {
		// Skip non-order_created events
		return nil
	}

	span.SetAttributes(
		attribute.String("event.type", orderEvent.EventType),
		attribute.Int("order.id", orderEvent.OrderID),
		attribute.Int("user.id", orderEvent.UserID),
		attribute.Int("product.id", orderEvent.ProductID),
		attribute.Int("order.quantity", orderEvent.Quantity),
		attribute.Float64("amount", orderEvent.TotalPrice),
	)

	logger.Info("Processing payment for order",
		zap.String("trace_id", traceID),
		zap.Int("order_id", orderEvent.OrderID),
		zap.Int("user_id", orderEvent.UserID),
		zap.Float64("amount", orderEvent.TotalPrice),
	)

	status, transactionID, processingDelay, simErr := simulatePayment(orderEvent.OrderID, orderEvent.TotalPrice)
	span.SetAttributes(attribute.Bool("payment.success", status == models.PaymentStatusSuccess))
	if simErr != nil {
		span.RecordError(simErr)
	}

	paymentID, err := persistPayment(ctx, db, orderEvent, status, transactionID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create payment record: %w", err)
	}

	span.SetAttributes(attribute.Int("payment.id", paymentID))

	paymentEvent := models.PaymentEvent{
		PaymentID:     paymentID,
		OrderID:       orderEvent.OrderID,
		UserID:        orderEvent.UserID,
		Amount:        orderEvent.TotalPrice,
		Status:        status,
		TransactionID: transactionID,
	}

	if status == models.PaymentStatusSuccess {
		paymentEvent.EventType = "payment_success"
		logger.Info("Payment successful",
			zap.String("trace_id", traceID),
			zap.Int("payment_id", paymentID),
			zap.String("transaction_id", transactionID),
			zap.Duration("processing_time", processingDelay),
		)
	} else {
		paymentEvent.EventType = "payment_failed"
		logger.Warn("Payment failed",
			zap.String("trace_id", traceID),
			zap.Int("payment_id", paymentID),
			zap.Duration("processing_time", processingDelay),
		)
	}

	if err := PublishPaymentEvent(ctx, producer, "order_events", paymentEvent, logger); err != nil {
		span.RecordError(err)
		logger.Error("Failed to publish payment event", zap.String("trace_id", traceID), zap.Error(err))
	}

	logger.Info("Payment processed",
		zap.String("trace_id", traceID),
		zap.Int("payment_id", paymentID),
		zap.String("status", string(status)),
		zap.Duration("processing_time", processingDelay),
	)

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

func persistPayment(ctx context.Context, db *sql.DB, evt orderCreatedEvent, status models.PaymentStatus, transactionID string) (int, error) {
	var paymentID int
	err := db.QueryRowContext(ctx,
		"INSERT INTO payments (order_id, user_id, amount, status, transaction_id) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		evt.OrderID, evt.UserID, evt.TotalPrice, status, transactionID,
	).Scan(&paymentID)

	if err != nil {
		return 0, err
	}

	return paymentID, nil
}

func simulatePayment(orderID int, amount float64) (models.PaymentStatus, string, time.Duration, error) {
	delay := minProcessingDelay
	if maxAdditionalDelay > 0 {
		delay += time.Duration(rng.Int63n(int64(maxAdditionalDelay)))
	}

	if amount <= 0 {
		return models.PaymentStatusFailed, "", delay, errors.New("invalid payment amount")
	}

	time.Sleep(delay)

	if rng.Float64() <= paymentSuccessRate {
		transactionID := fmt.Sprintf("TXN-%d-%d", orderID, time.Now().UnixNano())
		return models.PaymentStatusSuccess, transactionID, delay, nil
	}

	return models.PaymentStatusFailed, "", delay, errors.New("payment authorization declined")
}

func loadSuccessRate() float64 {
	raw := getEnv("PAYMENT_SUCCESS_RATE", "")
	if raw == "" {
		return 0.8
	}

	rate, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0.8
	}

	if rate < 0 {
		return 0
	}

	if rate > 1 {
		return 1
	}

	return rate
}

func (c saramaHeaderCarrierConsumer) Keys() []string {
	keys := make([]string, len(c))
	for i, h := range c {
		keys[i] = string(h.Key)
	}
	return keys
}
