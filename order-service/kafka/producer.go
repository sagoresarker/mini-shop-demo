package kafka

import (
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
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

func PublishOrderEvent(producer sarama.SyncProducer, topic string, event any, logger *zap.Logger) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(eventJSON),
	}

	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	logger.Info("Event published",
		zap.String("topic", topic),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
	)

	return nil
}
