package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func InitRedis(logger *zap.Logger) (*redis.Client, error) {
	host := getEnv("REDIS_HOST", "localhost")
	port := getEnv("REDIS_PORT", "6379")
	password := getEnv("REDIS_PASSWORD", "")

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("Redis connection established")
	return rdb, nil
}

func GetProduct(ctx context.Context, rdb *redis.Client, id string) ([]byte, error) {
	key := fmt.Sprintf("product:%s", id)
	return rdb.Get(ctx, key).Bytes()
}

func SetProduct(ctx context.Context, rdb *redis.Client, id string, product interface{}, ttl time.Duration) error {
	key := fmt.Sprintf("product:%s", id)
	data, err := json.Marshal(product)
	if err != nil {
		return err
	}
	return rdb.Set(ctx, key, data, ttl).Err()
}

func DeleteProduct(ctx context.Context, rdb *redis.Client, id string) error {
	key := fmt.Sprintf("product:%s", id)
	return rdb.Del(ctx, key).Err()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
