package kafka

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"order-svc/models"

	"github.com/IBM/sarama"
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
	var event models.OrderEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	logger.Info("Received event",
		zap.String("event_type", event.EventType),
		zap.Int("order_id", event.OrderID),
	)

	// Handle different event types for Saga pattern
	switch event.EventType {
	case "order_failed", "payment_failed":
		// Rollback order status
		_, err := db.Exec(
			"UPDATE orders SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			models.OrderStatusFailed, event.OrderID,
		)
		if err != nil {
			return fmt.Errorf("failed to update order status: %w", err)
		}
		logger.Info("Order status updated to failed", zap.Int("order_id", event.OrderID))
	case "order_paid":
		// Update order status to paid
		_, err := db.Exec(
			"UPDATE orders SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			models.OrderStatusPaid, event.OrderID,
		)
		if err != nil {
			return fmt.Errorf("failed to update order status: %w", err)
		}
		logger.Info("Order status updated to paid", zap.Int("order_id", event.OrderID))
	}

	return nil
}
