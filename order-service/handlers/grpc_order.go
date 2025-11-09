package handlers

import (
	"context"
	"database/sql"

	"order-svc/grpc"
	"order-svc/kafka"
	"order-svc/models"
	order "order-svc/proto"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type OrderService struct {
	order.UnimplementedOrderServiceServer

	db            *sql.DB
	producer      sarama.SyncProducer
	productClient *grpc.ProductClient
	logger        *zap.Logger
}

func NewOrderService(
	db *sql.DB,
	producer sarama.SyncProducer,
	productClient *grpc.ProductClient,
	logger *zap.Logger,
) *OrderService {
	return &OrderService{
		db:            db,
		producer:      producer,
		productClient: productClient,
		logger:        logger,
	}
}

func (s *OrderService) CreateOrder(
	ctx context.Context,
	req *order.CreateOrderRequest,
) (*order.CreateOrderResponse, error) {
	ctx, span := otel.Tracer("order-service").Start(ctx, "CreateOrder_gRPC")
	defer span.End()

	span.SetAttributes(
		attribute.Int("user_id", int(req.GetUserId())),
		attribute.Int("product_id", int(req.GetProductId())),
		attribute.Int("quantity", int(req.GetQuantity())),
	)

	// Check product availability
	available, stock, err := s.productClient.CheckAvailability(ctx, req.GetProductId(), req.GetQuantity())
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if !available {
		span.SetAttributes(
			attribute.Bool("available", false),
			attribute.Int("stock", int(stock)),
		)
		return &order.CreateOrderResponse{
			Success: false,
			Message: "Product not available",
		}, nil
	}

	// Get product details
	productResp, err := s.productClient.GetProduct(ctx, req.GetProductId())
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	totalPrice := float64(req.GetQuantity()) * float64(productResp.GetPrice())

	// Create order
	var orderModel models.Order
	err = s.db.QueryRowContext(
		ctx,
		"INSERT INTO orders (user_id, product_id, quantity, status, total_price) VALUES ($1, $2, $3, $4, $5) RETURNING id, user_id, product_id, quantity, status, total_price, created_at, updated_at",
		req.GetUserId(),
		req.GetProductId(),
		req.GetQuantity(),
		models.OrderStatusPending,
		totalPrice,
	).Scan(&orderModel.ID, &orderModel.UserID, &orderModel.ProductID, &orderModel.Quantity, &orderModel.Status, &orderModel.TotalPrice, &orderModel.CreatedAt, &orderModel.UpdatedAt)

	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.Int("order.id", orderModel.ID))

	// Publish event
	event := models.OrderEvent{
		OrderID:    orderModel.ID,
		UserID:     orderModel.UserID,
		ProductID:  orderModel.ProductID,
		Quantity:   orderModel.Quantity,
		Status:     orderModel.Status,
		TotalPrice: orderModel.TotalPrice,
		EventType:  "order_created",
	}

	if err := kafka.PublishOrderEvent(s.producer, "order_events", event, s.logger); err != nil {
		s.logger.Error("Failed to publish order_created event", zap.Error(err))
		// Don't fail the request, but log the error
	}

	return &order.CreateOrderResponse{
		Success: true,
		OrderId: int32(orderModel.ID),
		Message: "Order created successfully",
	}, nil
}

func (s *OrderService) GetOrder(ctx context.Context, req *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	ctx, span := otel.Tracer("order-service").Start(ctx, "GetOrder_gRPC")
	defer span.End()

	span.SetAttributes(attribute.Int("order.id", int(req.GetOrderId())))

	var orderModel models.Order
	err := s.db.QueryRowContext(ctx,
		"SELECT id, user_id, product_id, quantity, status, total_price FROM orders WHERE id = $1",
		req.GetOrderId(),
	).Scan(&orderModel.ID, &orderModel.UserID, &orderModel.ProductID, &orderModel.Quantity, &orderModel.Status, &orderModel.TotalPrice)

	if err != nil {
		if err == sql.ErrNoRows {
			span.RecordError(err)
			return nil, err
		}
		span.RecordError(err)
		return nil, err
	}

	return &order.GetOrderResponse{
		Id:         int32(orderModel.ID),
		UserId:     int32(orderModel.UserID),
		ProductId:  int32(orderModel.ProductID),
		Quantity:   int32(orderModel.Quantity),
		Status:     string(orderModel.Status),
		TotalPrice: float32(orderModel.TotalPrice),
	}, nil
}
