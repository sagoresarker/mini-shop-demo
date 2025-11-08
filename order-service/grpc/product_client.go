package grpc

import (
	"context"
	"fmt"
	"os"
	"time"

	"order-svc/circuitbreaker"
	"order-svc/proto/product"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ProductClient struct {
	conn           *grpc.ClientConn
	client         product.ProductServiceClient
	circuitBreaker *circuitbreaker.CircuitBreaker
	logger         *zap.Logger
}

func InitProductClient(logger *zap.Logger) (*ProductClient, error) {
	address := getEnv("PRODUCT_SERVICE_GRPC", "localhost:50052")

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Product Service: %w", err)
	}

	client := product.NewProductServiceClient(conn)

	return &ProductClient{
		conn:           conn,
		client:         client,
		circuitBreaker: circuitbreaker.NewCircuitBreaker(5, 30*time.Second),
		logger:         logger,
	}, nil
}

func (pc *ProductClient) CheckAvailability(ctx context.Context, productID int32, quantity int32) (bool, int32, error) {
	var available bool
	var stock int32

	err := pc.circuitBreaker.Execute(ctx, func() error {
		resp, err := pc.client.CheckAvailability(ctx, &product.CheckAvailabilityRequest{
			ProductId: productID,
			Quantity:  quantity,
		})
		if err != nil {
			return err
		}
		available = resp.Available
		stock = resp.Stock
		return nil
	})

	if err != nil {
		return false, 0, err
	}

	return available, stock, nil
}

func (pc *ProductClient) GetProduct(ctx context.Context, productID int32) (*product.GetProductResponse, error) {
	var resp *product.GetProductResponse

	err := pc.circuitBreaker.Execute(ctx, func() error {
		var err error
		resp, err = pc.client.GetProduct(ctx, &product.GetProductRequest{
			ProductId: productID,
		})
		return err
	})

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (pc *ProductClient) Close() error {
	return pc.conn.Close()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
